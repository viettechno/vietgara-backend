package usecase_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/license/domain"
	"github.com/viettechno/vietgara-backend/internal/license/licensetest"
	"github.com/viettechno/vietgara-backend/internal/license/usecase"
	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/platform/access"
	"github.com/viettechno/vietgara-backend/internal/platform/outbox"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

var errBoom = errors.New("boom")

func requireReason(t *testing.T, err error, reason string) {
	t.Helper()
	require.Error(t, err)
	require.Equal(t, reason, apperror.ReasonOf(err), err)
}

//go:fix inline

func TestTrialAndPlanChange(t *testing.T) {
	t.Parallel()

	e := licensetest.NewEnv()
	ctx := t.Context()

	data, _ := json.Marshal(map[string]string{"user_id": "user-1"})
	require.NoError(t, e.Subscriptions.HandleUserEmailVerified(ctx, outbox.Event{Data: data}))
	require.NoError(t, e.Subscriptions.HandleUserEmailVerified(ctx, outbox.Event{Data: data}), "idempotent")
	require.NoError(t, e.Subscriptions.HandleUserEmailVerified(ctx, outbox.Event{Data: []byte("{}")}))

	sub, usage, err := e.Subscriptions.GetMine(ctx, "user-1")
	require.NoError(t, err)
	assert.Equal(t, domain.StatusTrial, sub.Status)
	assert.Equal(t, usecase.Usage{}, usage)
	assert.True(t, e.Store.Flags["user-1"][domain.FeatureQuotes])
	assert.Contains(t, e.Store.Events, usecase.EventEntitlementUpdated)

	plans, err := e.Subscriptions.ListPlans(ctx)
	require.NoError(t, err)
	require.Len(t, plans, 2)
	assert.Equal(t, "starter", plans[0].Code)

	e.Store.Garages["user-1"], e.Store.Staff["user-1"] = 2, 3
	_, _, err = e.Subscriptions.ChangeMyPlan(ctx, "user-1", "p-starter")
	requireReason(t, err, domain.ReasonPlanLimitReached)

	e.Store.Garages["user-1"], e.Store.Staff["user-1"] = 1, 6
	_, _, err = e.Subscriptions.ChangeMyPlan(ctx, "user-1", "p-starter")
	requireReason(t, err, domain.ReasonPlanLimitReached)

	// The plan is scheduled for the end of the trial, not switched.
	scheduled, usage, err := e.Subscriptions.ChangeMyPlan(ctx, "user-1", "p-pro")
	require.NoError(t, err)
	assert.Equal(t, domain.StatusTrial, scheduled.Status)
	assert.Equal(t, domain.TrialPlanCode, scheduled.Plan.Code)
	assert.Equal(t, 6, usage.Staff)
	require.NotNil(t, scheduled.PendingChange)
	assert.Equal(t, "pro", scheduled.PendingChange.Plan.Code)
	assert.Equal(t, domain.PlanChangeScheduled, scheduled.PendingChange.Status)
	assert.Equal(t, scheduled.CurrentPeriodEnd, scheduled.PendingChange.EffectiveAt)
	assert.Contains(t, e.Store.Audits, "subscription.plan_change_scheduled")
	assert.NotContains(t, e.Store.Events, usecase.EventSubscriptionStatusChanged)

	_, _, err = e.Subscriptions.ChangeMyPlan(ctx, "user-1", "p-legacy")
	requireReason(t, err, domain.ReasonPlanNotAvailable)
	_, _, err = e.Subscriptions.ChangeMyPlan(ctx, "user-1", "missing")
	requireReason(t, err, apperror.ReasonNotFound)

	// GetMine starts the trial of an owner without a subscription.
	sub, _, err = e.Subscriptions.GetMine(ctx, "user-new")
	require.NoError(t, err)
	assert.Equal(t, domain.StatusTrial, sub.Status)
}

func TestSubscriptionFailures(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	for op, call := range map[string]func(e *licensetest.Env) error{
		"plans.code":   func(e *licensetest.Env) error { return e.Subscriptions.EnsureTrial(ctx, "a") },
		"subs.create":  func(e *licensetest.Env) error { return e.Subscriptions.EnsureTrial(ctx, "a") },
		"entitlements": func(e *licensetest.Env) error { return e.Subscriptions.EnsureTrial(ctx, "a") },
		"plans.list": func(e *licensetest.Env) error {
			_, err := e.Subscriptions.ListPlans(ctx)
			return err
		},
		"subs.find": func(e *licensetest.Env) error {
			_, _, err := e.Subscriptions.GetMine(ctx, "a")
			return err
		},
		"usage": func(e *licensetest.Env) error {
			_, _, err := e.Subscriptions.GetMine(ctx, "a")
			return err
		},
		"subs.lock": func(e *licensetest.Env) error {
			_, _, err := e.Subscriptions.ChangeMyPlan(ctx, "a", "p-pro")
			return err
		},
		"subs.update": func(e *licensetest.Env) error {
			_, _, err := e.Subscriptions.ChangeMyPlan(ctx, "a", "p-pro")
			return err
		},
		"events": func(e *licensetest.Env) error {
			_, _, err := e.Subscriptions.ChangeMyPlan(ctx, "a", "p-pro")
			return err
		},
	} {
		e := licensetest.NewEnv()
		if op != "plans.code" && op != "subs.create" && op != "entitlements" && op != "subs.find" {
			require.NoError(t, e.Subscriptions.EnsureTrial(ctx, "a"))
		}

		if op == "subs.update" || op == "events" {
			// Only a past-due owner switches at once and so saves the subscription.
			e.Now = e.Now.Add(domain.TrialPeriod + time.Hour)
			require.NoError(t, e.Expiry.Run(ctx))
		}

		e.Store.Fail[op] = errBoom
		require.Error(t, call(e), op)
	}

	e := licensetest.NewEnv()
	require.NoError(t, e.Subscriptions.EnsureTrial(ctx, "a"))
	e.Store.AuditErr = errBoom
	_, _, err := e.Subscriptions.ChangeMyPlan(ctx, "a", "p-pro")
	requireReason(t, err, apperror.ReasonInternal)

	e2 := licensetest.NewEnv()
	require.NoError(t, e2.Subscriptions.EnsureTrial(ctx, "a"))
	e2.Now = e2.Now.Add(domain.TrialPeriod + time.Hour)
	require.NoError(t, e2.Expiry.Run(ctx))
	e2.Store.Fail["entitlements"] = errBoom
	_, _, err = e2.Subscriptions.ChangeMyPlan(ctx, "a", "p-starter")
	requireReason(t, err, apperror.ReasonInternal)
}

func TestEntitlements(t *testing.T) {
	t.Parallel()

	e := licensetest.NewEnv()
	ctx := t.Context()
	e.Store.Owners["g-1"] = "owner"

	got, err := e.Entitlements.ForGarage(ctx, "g-1")
	require.NoError(t, err, "starts the owner's trial")
	assert.Equal(t, domain.StatusTrial, got.Status)

	cached, err := e.Entitlements.Get(ctx, "owner")
	require.NoError(t, err)
	assert.Equal(t, got, cached)

	enabled, err := e.Entitlements.IsFeatureEnabled(ctx, "owner", domain.FeatureReports)
	require.NoError(t, err)
	assert.True(t, enabled)

	require.NoError(t, e.Entitlements.CheckLimit(ctx, "owner", domain.LimitGarages, 0))
	requireReason(t, e.Entitlements.CheckLimit(ctx, "owner", domain.LimitGarages, 1), domain.ReasonPlanLimitReached)
	requireReason(t, e.Entitlements.CheckLimit(ctx, "owner", "max_bays", 0), apperror.ReasonInternal)
	require.NoError(t, e.Entitlements.CheckLimit(ctx, "other", domain.LimitStaff, 0), "starts the trial")

	require.NoError(t, e.Entitlements.ConsumeTransaction(ctx, "owner"))
	require.NoError(t, e.Entitlements.ConsumeTransaction(ctx, "owner"))
	requireReason(t, e.Entitlements.ConsumeTransaction(ctx, "owner"), domain.ReasonPlanLimitReached)

	_, usage, err := e.Subscriptions.GetMine(ctx, "owner")
	require.NoError(t, err)
	assert.Equal(t, 3, usage.TransactionsToday)

	require.NoError(t, e.Entitlements.Invalidate(ctx, "owner"))

	_, err = e.Entitlements.Get(ctx, "nobody")
	requireReason(t, err, apperror.ReasonNotFound)
	_, err = e.Entitlements.ForGarage(ctx, "unknown-garage")
	requireReason(t, err, apperror.ReasonNotFound)
	_, err = e.Entitlements.IsFeatureEnabled(ctx, "nobody", domain.FeatureQuotes)
	require.Error(t, err)

	e.Store.Fail["plans.code"] = errBoom
	require.Error(t, e.Entitlements.CheckLimit(ctx, "brand-new", domain.LimitStaff, 0))
	require.Error(t, e.Entitlements.ConsumeTransaction(ctx, "brand-new"))
}

func TestLicenseMode(t *testing.T) {
	t.Parallel()

	e := licensetest.NewEnv()
	ctx := t.Context()
	e.Store.Owners["g-1"] = "owner"

	mode, err := e.Entitlements.LicenseMode(ctx, "g-1")
	require.NoError(t, err)
	assert.Equal(t, middleware.LicenseFull, mode, "no subscription yet")

	require.NoError(t, e.Subscriptions.EnsureTrial(ctx, "owner"))

	// The authorized access in the context names the owner.
	actx := access.WithAccess(ctx, &access.Access{GarageID: "g-2", OwnerUserID: "owner"})
	mode, err = e.Entitlements.LicenseMode(actx, "g-2")
	require.NoError(t, err)
	assert.Equal(t, middleware.LicenseFull, mode)

	e.Now = e.Now.Add(domain.TrialPeriod + time.Hour)
	require.NoError(t, e.Expiry.Run(ctx))
	mode, err = e.Entitlements.LicenseMode(ctx, "g-1")
	require.NoError(t, err)
	assert.Equal(t, middleware.LicenseReadOnly, mode)

	e.Now = e.Now.Add(domain.GracePeriod)
	require.NoError(t, e.Expiry.Run(ctx))
	mode, err = e.Entitlements.LicenseMode(ctx, "g-1")
	require.NoError(t, err)
	assert.Equal(t, middleware.LicenseLocked, mode)

	_, err = e.Entitlements.LicenseMode(ctx, "unknown")
	require.Error(t, err)

	e.Store.Fail["subs.find"] = errBoom
	require.NoError(t, e.Entitlements.Invalidate(ctx, "owner"))
	_, err = e.Entitlements.LicenseMode(ctx, "g-1")
	require.Error(t, err)
}

func TestExpiryJob(t *testing.T) {
	t.Parallel()

	e := licensetest.NewEnv()
	ctx := t.Context()
	require.NoError(t, e.Subscriptions.EnsureTrial(ctx, "a"))

	e.Now = e.Now.Add(domain.TrialPeriod - 6*24*time.Hour)
	require.NoError(t, e.Expiry.Run(ctx))
	assert.Contains(t, e.Store.Events, usecase.EventSubscriptionExpiringSoon)

	events := len(e.Store.Events)
	require.NoError(t, e.Expiry.Run(ctx))
	assert.Len(t, e.Store.Events, events, "reminded once")

	e.Store.Fail["subs.due"] = errBoom
	require.Error(t, e.Expiry.Run(ctx))

	delete(e.Store.Fail, "subs.due")
	e.Now = e.Now.Add(7 * 24 * time.Hour)
	e.Store.Fail["subs.update"] = errBoom
	require.Error(t, e.Expiry.Run(ctx))

	delete(e.Store.Fail, "subs.update")
	e.Store.Fail["events"] = errBoom
	require.Error(t, e.Expiry.Run(ctx))

	delete(e.Store.Fail, "events")
	e.Store.Fail["subs.lock"] = errBoom
	require.Error(t, e.Expiry.Run(ctx))

	loop, cancel := context.WithCancel(ctx)
	done := make(chan struct{})

	go func() {
		e.Expiry.Schedule(loop, time.Hour)
		close(done)
	}()

	cancel()
	<-done
}

func TestAdmin(t *testing.T) {
	t.Parallel()

	e := licensetest.NewEnv()
	ctx := t.Context()

	plan, err := e.Subscriptions.CreatePlan(ctx, usecase.PlanFields{
		Code: new("chain"), Name: new(" Chain "), Price: new(int64(1490000)), Currency: new(" vnd "),
		BillingCycle: new(domain.BillingCycleMonthly), MaxGarages: new(10), MaxStaff: new(100), MaxTransactionsPerDay: new(1000),
		Features: &[]domain.Feature{domain.FeatureReports, domain.FeatureCustomers}, Public: new(true), Active: new(true), SortOrder: new(30),
		Description: new("desc"),
	})
	require.NoError(t, err)
	assert.Equal(t, "Chain", plan.Name)
	assert.Equal(t, "VND", plan.Currency)
	assert.Equal(t, []domain.Feature{domain.FeatureCustomers, domain.FeatureReports}, plan.Features)

	_, err = e.Subscriptions.CreatePlan(ctx, usecase.PlanFields{Code: new("chain"), Name: new("X"), MaxGarages: new(1), MaxTransactionsPerDay: new(1)})
	requireReason(t, err, domain.ReasonPlanCodeTaken)
	_, err = e.Subscriptions.CreatePlan(ctx, usecase.PlanFields{Code: new("Bad Code")})
	requireReason(t, err, apperror.ReasonValidation)

	updated, err := e.Subscriptions.UpdatePlan(ctx, plan.ID, usecase.PlanFields{Code: new("renamed"), Name: new("Chain+"), Active: new(false)})
	require.NoError(t, err)
	assert.Equal(t, "chain", updated.Code, "code is immutable")
	assert.False(t, updated.Active)

	_, err = e.Subscriptions.UpdatePlan(ctx, plan.ID, usecase.PlanFields{MaxGarages: new(0)})
	requireReason(t, err, apperror.ReasonValidation)
	_, err = e.Subscriptions.UpdatePlan(ctx, "missing", usecase.PlanFields{})
	requireReason(t, err, apperror.ReasonNotFound)

	all, err := e.Subscriptions.ListAllPlans(ctx)
	require.NoError(t, err)
	assert.Len(t, all, 5)

	require.NoError(t, e.Subscriptions.EnsureTrial(ctx, "user-1"))
	e.Store.Emails["user-1"] = "owner@example.com"

	page, err := e.Subscriptions.ListSubscriptions(ctx, usecase.SubscriptionFilter{Query: "owner", Page: 1, PageSize: 20})
	require.NoError(t, err)
	require.Equal(t, 1, page.Total)

	extended, err := e.Subscriptions.ExtendSubscription(ctx, page.Items[0].Subscription.ID, 30)
	require.NoError(t, err)
	assert.Equal(t, e.Now.Add(domain.TrialPeriod+30*24*time.Hour), extended.CurrentPeriodEnd)
	assert.Contains(t, e.Store.Audits, "subscription.extended")

	_, err = e.Subscriptions.ExtendSubscription(ctx, extended.ID, 0)
	requireReason(t, err, apperror.ReasonValidation)
	_, err = e.Subscriptions.ExtendSubscription(ctx, "missing", 1)
	requireReason(t, err, apperror.ReasonNotFound)

	e.Store.Fail["plans.list"] = errBoom
	_, err = e.Subscriptions.ListAllPlans(ctx)
	require.Error(t, err)

	e.Store.Fail["subs.search"] = errBoom
	_, err = e.Subscriptions.ListSubscriptions(ctx, usecase.SubscriptionFilter{})
	require.Error(t, err)

	e.Store.Fail["plans.update"] = errBoom
	_, err = e.Subscriptions.UpdatePlan(ctx, plan.ID, usecase.PlanFields{})
	require.Error(t, err)

	e.Store.AuditErr = errBoom
	_, err = e.Subscriptions.CreatePlan(ctx, usecase.PlanFields{Code: new("x1"), Name: new("X"), MaxGarages: new(1), MaxTransactionsPerDay: new(1)})
	requireReason(t, err, apperror.ReasonInternal)
}

func TestPlanChangeScheduling(t *testing.T) {
	t.Parallel()

	e := licensetest.NewEnv()
	ctx := t.Context()
	require.NoError(t, e.Subscriptions.EnsureTrial(ctx, "a"))

	// Nothing to cancel yet.
	_, _, err := e.Subscriptions.CancelMyPendingPlanChange(ctx, "a")
	requireReason(t, err, apperror.ReasonNotFound)

	sub, _, err := e.Subscriptions.ChangeMyPlan(ctx, "a", "p-pro")
	require.NoError(t, err)
	require.NotNil(t, sub.PendingChange)
	assert.Equal(t, "pro", sub.PendingChange.Plan.Code)

	// Choosing another plan replaces it.
	sub, _, err = e.Subscriptions.ChangeMyPlan(ctx, "a", "p-starter")
	require.NoError(t, err)
	assert.Equal(t, "starter", sub.PendingChange.Plan.Code)
	assert.Equal(t, "starter", e.Store.PendingChangeOf("a").Plan.Code, "one change is pending")

	// The reads carry it.
	got, _, err := e.Subscriptions.GetMine(ctx, "a")
	require.NoError(t, err)
	require.NotNil(t, got.PendingChange)

	// Choosing the plan the owner has cancels it.
	sub, _, err = e.Subscriptions.ChangeMyPlan(ctx, "a", "p-trial")
	requireReason(t, err, domain.ReasonPlanNotAvailable)
	assert.Nil(t, sub)

	cancelled, _, err := e.Subscriptions.CancelMyPendingPlanChange(ctx, "a")
	require.NoError(t, err)
	assert.Nil(t, cancelled.PendingChange)
	assert.Nil(t, e.Store.PendingChangeOf("a"))
	assert.Contains(t, e.Store.Audits, "subscription.plan_change_cancelled")

	_, _, err = e.Subscriptions.CancelMyPendingPlanChange(ctx, "a")
	requireReason(t, err, apperror.ReasonNotFound)
}

func TestChoosingTheCurrentPlanCancelsTheChange(t *testing.T) {
	t.Parallel()

	e := licensetest.NewEnv()
	ctx := t.Context()
	require.NoError(t, e.Subscriptions.EnsureTrial(ctx, "a"))

	// Move to the starter plan for good, then schedule pro and take it back.
	_, _, err := e.Subscriptions.ChangeMyPlan(ctx, "a", "p-starter")
	require.NoError(t, err)

	e.Now = e.Now.Add(domain.TrialPeriod + time.Hour)
	require.NoError(t, e.Expiry.Run(ctx))

	active, _, err := e.Subscriptions.GetMine(ctx, "a")
	require.NoError(t, err)
	require.Equal(t, domain.StatusActive, active.Status)
	require.Equal(t, "starter", active.Plan.Code)

	_, _, err = e.Subscriptions.ChangeMyPlan(ctx, "a", "p-pro")
	require.NoError(t, err)
	require.NotNil(t, e.Store.PendingChangeOf("a"))

	sub, _, err := e.Subscriptions.ChangeMyPlan(ctx, "a", "p-starter")
	require.NoError(t, err)
	assert.Nil(t, sub.PendingChange)
	assert.Nil(t, e.Store.PendingChangeOf("a"))

	// Without a pending change, choosing the current plan changes nothing.
	again, _, err := e.Subscriptions.ChangeMyPlan(ctx, "a", "p-starter")
	require.NoError(t, err)
	assert.Equal(t, active.CurrentPeriodEnd, again.CurrentPeriodEnd)
	assert.Equal(t, domain.StatusActive, again.Status)
}

func TestPastDueOwnerSwitchesAtOnce(t *testing.T) {
	t.Parallel()

	e := licensetest.NewEnv()
	ctx := t.Context()
	require.NoError(t, e.Subscriptions.EnsureTrial(ctx, "a"))

	_, _, err := e.Subscriptions.ChangeMyPlan(ctx, "a", "p-pro")
	require.NoError(t, err)

	// The period ends with the change due: the job applies it. A second
	// owner who chose nothing falls into the grace period.
	require.NoError(t, e.Subscriptions.EnsureTrial(ctx, "b"))

	e.Now = e.Now.Add(domain.TrialPeriod + time.Hour)
	require.NoError(t, e.Expiry.Run(ctx))

	b, _, err := e.Subscriptions.GetMine(ctx, "b")
	require.NoError(t, err)
	require.Equal(t, domain.StatusPastDue, b.Status)

	switched, _, err := e.Subscriptions.ChangeMyPlan(ctx, "b", "p-starter")
	require.NoError(t, err)
	assert.Equal(t, domain.StatusActive, switched.Status)
	assert.Equal(t, "starter", switched.Plan.Code)
	assert.Nil(t, switched.PendingChange)
	assert.Contains(t, e.Store.Audits, "subscription.plan_changed")
}

func TestExpiryJobAppliesDueChange(t *testing.T) {
	t.Parallel()

	e := licensetest.NewEnv()
	ctx := t.Context()
	require.NoError(t, e.Subscriptions.EnsureTrial(ctx, "a"))

	_, _, err := e.Subscriptions.ChangeMyPlan(ctx, "a", "p-pro")
	require.NoError(t, err)

	// Not due yet: nothing changes.
	trialEnd := e.Now.Add(domain.TrialPeriod)
	e.Now = e.Now.Add(time.Hour)
	require.NoError(t, e.Expiry.Run(ctx))
	require.NotNil(t, e.Store.PendingChangeOf("a"))

	e.Now = trialEnd.Add(time.Minute)
	require.NoError(t, e.Expiry.Run(ctx))

	sub, _, err := e.Subscriptions.GetMine(ctx, "a")
	require.NoError(t, err)
	assert.Equal(t, domain.StatusActive, sub.Status)
	assert.Equal(t, "pro", sub.Plan.Code)
	assert.Equal(t, trialEnd, sub.CurrentPeriodStart, "the new period starts when the old one ended")
	assert.Equal(t, trialEnd.AddDate(1, 0, 0), sub.CurrentPeriodEnd)
	assert.Nil(t, sub.PendingChange)
	assert.Contains(t, e.Store.Events, usecase.EventPlanChangeApplied)
	assert.Contains(t, e.Store.Events, usecase.EventSubscriptionStatusChanged)
	assert.True(t, e.Store.Flags["a"][domain.FeatureReports], "the entitlements follow the new plan")

	// It is applied once.
	events := len(e.Store.Events)
	require.NoError(t, e.Expiry.Run(ctx))
	assert.Len(t, e.Store.Events, events)
}

func TestExpiryJobPostponesWhenUsageExceedsThePlan(t *testing.T) {
	t.Parallel()

	e := licensetest.NewEnv()
	ctx := t.Context()
	require.NoError(t, e.Subscriptions.EnsureTrial(ctx, "a"))

	_, _, err := e.Subscriptions.ChangeMyPlan(ctx, "a", "p-starter")
	require.NoError(t, err)

	// The owner grows past the starter plan before the change is due.
	e.Store.Staff["a"] = 8
	trialEnd := e.Now.Add(domain.TrialPeriod)

	e.Now = trialEnd.Add(time.Minute)
	require.NoError(t, e.Expiry.Run(ctx))

	change := e.Store.PendingChangeOf("a")
	require.NotNil(t, change)
	assert.Equal(t, domain.PlanChangePostponed, change.Status)
	assert.Equal(t, 1, countOf(e.Store.Events, usecase.EventPlanChangePostponed))
	assert.NotContains(t, e.Store.Events, usecase.EventPlanChangeApplied)

	sub, _, err := e.Subscriptions.GetMine(ctx, "a")
	require.NoError(t, err)
	assert.Equal(t, domain.StatusPastDue, sub.Status, "the period still ended")
	assert.Equal(t, domain.TrialPlanCode, sub.Plan.Code)

	// Still exceeded: held back, and the owner is not told again.
	e.Now = e.Now.Add(time.Hour)
	require.NoError(t, e.Expiry.Run(ctx))
	assert.Equal(t, 1, countOf(e.Store.Events, usecase.EventPlanChangePostponed))

	// Once the usage fits, the next run applies it, from now.
	e.Store.Staff["a"] = 3
	e.Now = e.Now.Add(time.Hour)
	require.NoError(t, e.Expiry.Run(ctx))

	sub, _, err = e.Subscriptions.GetMine(ctx, "a")
	require.NoError(t, err)
	assert.Equal(t, domain.StatusActive, sub.Status)
	assert.Equal(t, "starter", sub.Plan.Code)
	assert.Equal(t, e.Now, sub.CurrentPeriodStart)
	assert.Nil(t, sub.PendingChange)
}

func TestExtensionMovesThePlanChange(t *testing.T) {
	t.Parallel()

	e := licensetest.NewEnv()
	ctx := t.Context()
	require.NoError(t, e.Subscriptions.EnsureTrial(ctx, "a"))
	e.Store.Emails["a"] = "a@example.com"

	_, _, err := e.Subscriptions.ChangeMyPlan(ctx, "a", "p-pro")
	require.NoError(t, err)

	page, err := e.Subscriptions.ListSubscriptions(ctx, usecase.SubscriptionFilter{Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.NotNil(t, page.Items[0].Subscription.PendingChange, "the admin list carries it")

	extended, err := e.Subscriptions.ExtendSubscription(ctx, page.Items[0].Subscription.ID, 10)
	require.NoError(t, err)

	change := e.Store.PendingChangeOf("a")
	require.NotNil(t, change)
	assert.Equal(t, extended.CurrentPeriodEnd, change.EffectiveAt)

	// Not due at the old date any more.
	e.Now = e.Now.Add(domain.TrialPeriod + time.Hour)
	require.NoError(t, e.Expiry.Run(ctx))

	sub, _, err := e.Subscriptions.GetMine(ctx, "a")
	require.NoError(t, err)
	assert.Equal(t, domain.StatusTrial, sub.Status)
	assert.NotNil(t, sub.PendingChange)
}

func TestExpiryJobPlanChangeFailures(t *testing.T) {
	t.Parallel()

	for _, op := range []string{"usage", "subs.change.save", "subs.update", "events"} {
		e := licensetest.NewEnv()
		ctx := t.Context()
		require.NoError(t, e.Subscriptions.EnsureTrial(ctx, "a"))
		_, _, err := e.Subscriptions.ChangeMyPlan(ctx, "a", "p-pro")
		require.NoError(t, err)

		e.Now = e.Now.Add(domain.TrialPeriod + time.Hour)
		e.Store.Fail[op] = errBoom
		require.Error(t, e.Expiry.Run(ctx), op)
	}
}

func countOf(events []string, event string) int {
	n := 0

	for _, e := range events {
		if e == event {
			n++
		}
	}

	return n
}
