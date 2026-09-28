package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/license/domain"
)

var start = time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)

func trialPlan() *domain.Plan {
	return &domain.Plan{
		ID: "plan-trial", Code: domain.TrialPlanCode, MaxGarages: 1, MaxStaff: 5, MaxTransactionsPerDay: 50,
		Features: []domain.Feature{domain.FeatureQuotes, domain.FeatureInventory}, Active: true,
	}
}

func proPlan() *domain.Plan {
	return &domain.Plan{
		ID: "plan-pro", Code: "pro", Name: "Pro", Currency: "VND", Public: true, Active: true,
		BillingCycle: domain.BillingCycleMonthly, MaxGarages: 2, MaxStaff: 20, MaxTransactionsPerDay: 100,
		Features: []domain.Feature{domain.FeatureReports, domain.FeatureQuotes},
	}
}

func TestSubscriptionLifecycleOverTime(t *testing.T) {
	t.Parallel()

	sub := domain.NewTrial("acc-1", trialPlan(), start)
	end := start.Add(domain.TrialPeriod)

	require.Equal(t, domain.StatusTrial, sub.Status)
	require.Equal(t, end, *sub.TrialEndAt)

	steps := []struct {
		name         string
		at           time.Time
		wantReminder bool
		wantChanged  bool
		wantStatus   domain.SubscriptionStatus
	}{
		{name: "day 1", at: start.Add(24 * time.Hour), wantStatus: domain.StatusTrial},
		{name: "8 days left", at: end.Add(-8 * 24 * time.Hour), wantStatus: domain.StatusTrial},
		{name: "7 days left: remind", at: end.Add(-7 * 24 * time.Hour), wantReminder: true, wantStatus: domain.StatusTrial},
		{name: "already reminded", at: end.Add(-24 * time.Hour), wantStatus: domain.StatusTrial},
		{name: "expired: grace", at: end, wantChanged: true, wantStatus: domain.StatusPastDue},
		{name: "still in grace", at: end.Add(6 * 24 * time.Hour), wantStatus: domain.StatusPastDue},
		{name: "grace over", at: end.Add(domain.GracePeriod), wantChanged: true, wantStatus: domain.StatusSuspended},
		{name: "suspended stays", at: end.Add(30 * 24 * time.Hour), wantStatus: domain.StatusSuspended},
	}

	for _, step := range steps {
		reminder := sub.NeedsExpiryReminder(step.at, domain.ExpiringSoonWindow)
		assert.Equal(t, step.wantReminder, reminder, step.name)

		if reminder {
			sub.MarkReminded()
		}

		assert.Equal(t, step.wantChanged, sub.Expire(step.at, domain.GracePeriod), step.name)
		assert.Equal(t, step.wantStatus, sub.Status, step.name)
	}

	renewedAt := end.Add(40 * 24 * time.Hour)
	require.NoError(t, sub.ChangePlan(proPlan(), renewedAt))
	assert.Equal(t, domain.StatusActive, sub.Status)
	assert.Equal(t, renewedAt.AddDate(0, 1, 0), sub.CurrentPeriodEnd)
	assert.Nil(t, sub.GracePeriodEnd)
	assert.Nil(t, sub.TrialEndAt)
}

func TestChangePlanAndExtend(t *testing.T) {
	t.Parallel()

	sub := domain.NewTrial("acc-1", trialPlan(), start)
	require.ErrorIs(t, sub.ChangePlan(trialPlan(), start), domain.ErrPlanNotAvailable)

	hidden := proPlan()
	hidden.Active = false
	require.ErrorIs(t, sub.ChangePlan(hidden, start), domain.ErrPlanNotAvailable)

	yearly := proPlan()
	yearly.BillingCycle = domain.BillingCycleYearly
	require.NoError(t, sub.ChangePlan(yearly, start))
	assert.Equal(t, start.AddDate(1, 0, 0), sub.CurrentPeriodEnd)

	trial := domain.NewTrial("acc-2", trialPlan(), start)
	require.NoError(t, trial.Extend(3, start))
	assert.Equal(t, start.Add(domain.TrialPeriod+72*time.Hour), trial.CurrentPeriodEnd)
	assert.Equal(t, trial.CurrentPeriodEnd, *trial.TrialEndAt)

	late := start.Add(60 * 24 * time.Hour)
	trial.Expire(late, domain.GracePeriod)
	require.Equal(t, domain.StatusSuspended, trial.Status)
	require.NoError(t, trial.Extend(10, late))
	assert.Equal(t, domain.StatusActive, trial.Status)
	assert.Equal(t, late.Add(10*24*time.Hour), trial.CurrentPeriodEnd)

	require.ErrorIs(t, trial.Extend(0, late), domain.ErrInvalidExtension)
	require.ErrorIs(t, trial.Extend(400, late), domain.ErrInvalidExtension)

	sub.Status = domain.StatusCancelled
	require.ErrorIs(t, sub.ChangePlan(proPlan(), start), domain.ErrSubscriptionCancelled)
	require.ErrorIs(t, sub.Extend(1, start), domain.ErrSubscriptionCancelled)
	assert.False(t, sub.Expire(start.AddDate(2, 0, 0), domain.GracePeriod))
	assert.False(t, sub.NeedsExpiryReminder(sub.CurrentPeriodEnd.Add(-time.Hour), domain.ExpiringSoonWindow))
}

func TestStatusAccess(t *testing.T) {
	t.Parallel()

	for status, want := range map[domain.SubscriptionStatus][2]bool{
		domain.StatusTrial: {true, true}, domain.StatusActive: {true, true}, domain.StatusPastDue: {true, false},
		domain.StatusSuspended: {false, false}, domain.StatusCancelled: {false, false},
	} {
		assert.Equal(t, want[0], status.AllowsReads(), status)
		assert.Equal(t, want[1], status.AllowsWrites(), status)
	}
}

func TestEntitlements(t *testing.T) {
	t.Parallel()

	sub := domain.NewTrial("acc-1", proPlan(), start)
	e := domain.NewEntitlements(sub)

	assert.Equal(t, []domain.Feature{domain.FeatureQuotes, domain.FeatureReports}, e.Features)
	assert.True(t, e.HasFeature(domain.FeatureReports))
	assert.False(t, e.ReadOnly())

	for limit, count := range map[domain.LimitType]int{domain.LimitGarages: 1, domain.LimitStaff: 19, domain.LimitTransactions: 99} {
		ok, err := e.WithinLimit(limit, count)
		require.NoError(t, err)
		assert.True(t, ok, limit)
		ok, _ = e.WithinLimit(limit, count+1)
		assert.False(t, ok, limit)
	}

	_, err := e.WithinLimit("max_bays", 1)
	require.ErrorIs(t, err, domain.ErrUnknownLimit)

	flags := domain.FeatureFlags(sub)
	assert.Len(t, flags, 7)
	assert.True(t, flags[domain.FeatureQuotes])
	assert.False(t, flags[domain.FeatureInventory])

	sub.Status = domain.StatusSuspended
	locked := domain.NewEntitlements(sub)
	assert.Empty(t, locked.Features)
	assert.True(t, locked.ReadOnly())
	assert.False(t, domain.FeatureFlags(sub)[domain.FeatureQuotes])
}

func TestPlanValidation(t *testing.T) {
	t.Parallel()

	require.NoError(t, proPlan().Validate())
	assert.True(t, proPlan().Selectable())
	assert.False(t, trialPlan().Selectable())
	assert.True(t, domain.BillingCycleYearly.IsValid())
	assert.False(t, domain.BillingCycle("weekly").IsValid())
	assert.False(t, domain.Feature("x").IsValid())

	mutations := map[string]func(p *domain.Plan){
		"code":                         func(p *domain.Plan) { p.Code = "Bad Code" },
		"name":                         func(p *domain.Plan) { p.Name = " " },
		"price":                        func(p *domain.Plan) { p.Price = -1 },
		"currency":                     func(p *domain.Plan) { p.Currency = "VN" },
		"billingCycle":                 func(p *domain.Plan) { p.BillingCycle = "weekly" },
		"limits.maxGarages":            func(p *domain.Plan) { p.MaxGarages = 0 },
		"limits.maxStaff":              func(p *domain.Plan) { p.MaxStaff = -1 },
		"limits.maxTransactionsPerDay": func(p *domain.Plan) { p.MaxTransactionsPerDay = 0 },
		"features":                     func(p *domain.Plan) { p.Features = []domain.Feature{"x"} },
	}

	for field, mutate := range mutations {
		p := proPlan()
		mutate(p)
		err := p.Validate()
		require.ErrorIs(t, err, domain.InvalidPlanError(field))
		assert.Contains(t, err.Error(), field)
	}

	assert.Equal(t, []domain.Feature{domain.FeatureCustomers, domain.FeatureReports},
		domain.NormalizeFeatures([]domain.Feature{domain.FeatureReports, "bogus", domain.FeatureCustomers, domain.FeatureReports}))
}
