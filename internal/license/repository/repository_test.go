package repository_test

import (
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/license/domain"
	"github.com/viettechno/vietgara-backend/internal/license/repository"
	"github.com/viettechno/vietgara-backend/internal/license/usecase"
	"github.com/viettechno/vietgara-backend/internal/platform/db/dbtest"
)

var (
	errBoom = errors.New("boom")
	now     = time.Date(2026, 9, 25, 9, 0, 0, 0, time.UTC)
)

func planRow() []any {
	return []any{"p-1", "pro", "Pro", "", int64(599000), "VND", "monthly", 2, 20, 100, []string{"quotes", "reports"}, true, true, 20, now, now}
}

func subRow() []any {
	return append([]any{"s-1", "acc-1", "p-1", "trial", now, now, now, nil, true, nil, now, now}, planRow()...)
}

func TestPlanRepository(t *testing.T) {
	t.Parallel()

	repo := repository.NewPostgresPlanRepository(nil)
	plan := &domain.Plan{Code: "x", Features: []domain.Feature{domain.FeatureQuotes}}

	tx := dbtest.New(t).
		ExpectQuery("p.is_public AND p.is_active", dbtest.NewRows(planRow()), nil).
		ExpectQuery("FROM plans p  ORDER", nil, errBoom).
		ExpectQuery("FROM plans p  ORDER", dbtest.NewRows([]any{1}), nil).
		ExpectQueryRow("p.id = $1", dbtest.ErrRow(pgx.ErrNoRows)).
		ExpectQueryRow("p.code = $1", dbtest.ErrRow(errBoom)).
		ExpectQueryRow("INSERT INTO plans", dbtest.Row("p-2", now, now)).WithArgs(func(args []any) {
		assert.Equal(t, []string{"quotes"}, args[9])
	}).
		ExpectQueryRow("INSERT INTO plans", dbtest.ErrRow(dbtest.UniqueViolation("plans_code_key"))).
		ExpectQueryRow("UPDATE plans", dbtest.Row(now)).
		ExpectQueryRow("UPDATE plans", dbtest.ErrRow(pgx.ErrNoRows))
	ctx := tx.Context(t.Context())

	plans, err := repo.ListPublic(ctx)
	require.NoError(t, err)
	require.Len(t, plans, 1)
	assert.Equal(t, []domain.Feature{domain.FeatureQuotes, domain.FeatureReports}, plans[0].Features)
	assert.Equal(t, domain.BillingCycleMonthly, plans[0].BillingCycle)

	_, err = repo.ListAll(ctx)
	require.ErrorIs(t, err, errBoom)
	_, err = repo.ListAll(ctx)
	require.Error(t, err)

	_, err = repo.FindByID(ctx, "p")
	require.ErrorIs(t, err, domain.ErrPlanNotFound)
	_, err = repo.FindByCode(ctx, "p")
	require.ErrorIs(t, err, errBoom)

	require.NoError(t, repo.Create(ctx, plan))
	require.ErrorIs(t, repo.Create(ctx, plan), domain.ErrPlanCodeTaken)
	require.NoError(t, repo.Update(ctx, plan))
	require.ErrorIs(t, repo.Update(ctx, plan), domain.ErrPlanNotFound)
}

func TestSubscriptionRepository(t *testing.T) {
	t.Parallel()

	repo := repository.NewPostgresRepository(nil)
	sub := &domain.Subscription{AccountID: "acc-1", PlanID: "p-1", Status: domain.StatusTrial}

	tx := dbtest.New(t).
		ExpectQueryRow("ON CONFLICT (account_id) DO NOTHING", dbtest.Row("s-1", now, now)).
		ExpectQueryRow("ON CONFLICT (account_id) DO NOTHING", dbtest.ErrRow(pgx.ErrNoRows)).
		ExpectQueryRow("ON CONFLICT (account_id) DO NOTHING", dbtest.ErrRow(errBoom)).
		ExpectQueryRow("s.account_id = $1", dbtest.Row(subRow()...)).
		ExpectQueryRow("FOR UPDATE OF s", dbtest.ErrRow(pgx.ErrNoRows)).
		ExpectQueryRow("s.id = $1 FOR UPDATE", dbtest.ErrRow(errBoom)).
		ExpectQueryRow("UPDATE subscriptions", dbtest.Row(now)).
		ExpectQuery("current_period_end < $1", dbtest.NewRows([]any{"acc-1"}), nil).
		ExpectQuery("current_period_end < $1", nil, errBoom).
		ExpectQuery("current_period_end < $1", dbtest.NewRows([]any{"a", "b"}), nil).
		ExpectExec("INSERT INTO entitlements", dbtest.Tag(7), nil)
	ctx := tx.Context(t.Context())

	inserted, err := repo.CreateIfAbsent(ctx, sub)
	require.NoError(t, err)
	assert.True(t, inserted)
	inserted, err = repo.CreateIfAbsent(ctx, sub)
	require.NoError(t, err)
	assert.False(t, inserted)

	_, err = repo.CreateIfAbsent(ctx, sub)
	require.ErrorIs(t, err, errBoom)

	got, err := repo.FindByAccountID(ctx, "acc-1")
	require.NoError(t, err)
	assert.Equal(t, domain.StatusTrial, got.Status)
	assert.Equal(t, "pro", got.Plan.Code)

	_, err = repo.LockByAccountID(ctx, "acc-1")
	require.ErrorIs(t, err, domain.ErrSubscriptionNotFound)
	_, err = repo.LockByID(ctx, "s-1")
	require.ErrorIs(t, err, errBoom)

	require.NoError(t, repo.Update(ctx, got))

	ids, err := repo.ListAccountIDsDueBefore(ctx, now)
	require.NoError(t, err)
	assert.Equal(t, []string{"acc-1"}, ids)

	_, err = repo.ListAccountIDsDueBefore(ctx, now)
	require.ErrorIs(t, err, errBoom)
	_, err = repo.ListAccountIDsDueBefore(ctx, now)
	require.Error(t, err)

	require.NoError(t, repo.Replace(ctx, "acc-1", map[domain.Feature]bool{domain.FeatureQuotes: true}))
}

func TestSearch(t *testing.T) {
	t.Parallel()

	repo := repository.NewPostgresRepository(nil)

	tx := dbtest.New(t).
		ExpectQueryRow("SELECT count(*)", dbtest.Row(1)).WithArgs(func(args []any) {
		assert.Equal(t, "%owner%", args[0])
		assert.Equal(t, "active", args[1])
	}).
		ExpectQuery("LIMIT $3 OFFSET $4", dbtest.NewRows(append(subRow(), "owner@example.com", "Minh")), nil).WithArgs(func(args []any) {
		assert.Equal(t, 10, args[2])
		assert.Equal(t, 10, args[3])
	}).
		ExpectQueryRow("SELECT count(*)", dbtest.ErrRow(errBoom)).
		ExpectQueryRow("SELECT count(*)", dbtest.Row(1)).
		ExpectQuery("LIMIT $3 OFFSET $4", nil, errBoom).
		ExpectQueryRow("SELECT count(*)", dbtest.Row(1)).
		ExpectQuery("LIMIT $3 OFFSET $4", dbtest.NewRows([]any{1}), nil)
	ctx := tx.Context(t.Context())

	page, err := repo.Search(ctx, usecase.SubscriptionFilter{Query: " Owner ", Status: domain.StatusActive, Page: 2, PageSize: 10})
	require.NoError(t, err)
	assert.Equal(t, 1, page.Total)
	require.Len(t, page.Items, 1)
	assert.Equal(t, "owner@example.com", page.Items[0].Email)

	for range 3 {
		_, err = repo.Search(ctx, usecase.SubscriptionFilter{Page: 1, PageSize: 10})
		require.Error(t, err)
	}
}

func TestCachesAndCounters(t *testing.T) {
	t.Parallel()

	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	t.Cleanup(func() { _ = client.Close() })

	ctx := t.Context()
	e := &domain.Entitlements{AccountID: "acc-1", Status: domain.StatusActive, PlanCode: "pro", Features: []domain.Feature{domain.FeatureQuotes}}

	redisCache := repository.NewRedisEntitlementCache(client, time.Minute)
	memoryCache := repository.NewMemoryEntitlementCache(time.Minute)

	for _, c := range []struct {
		get func(string) (*domain.Entitlements, error)
		set func(*domain.Entitlements) error
		del func(string) error
	}{
		{func(id string) (*domain.Entitlements, error) { return redisCache.Get(ctx, id) }, func(e *domain.Entitlements) error { return redisCache.Set(ctx, e) }, func(id string) error { return redisCache.Delete(ctx, id) }},
		{func(id string) (*domain.Entitlements, error) { return memoryCache.Get(ctx, id) }, func(e *domain.Entitlements) error { return memoryCache.Set(ctx, e) }, func(id string) error { return memoryCache.Delete(ctx, id) }},
	} {
		got, err := c.get("acc-1")
		require.NoError(t, err)
		assert.Nil(t, got)

		require.NoError(t, c.set(e))
		got, err = c.get("acc-1")
		require.NoError(t, err)
		assert.Equal(t, e.Features, got.Features)

		require.NoError(t, c.del("acc-1"))
		got, err = c.get("acc-1")
		require.NoError(t, err)
		assert.Nil(t, got)
	}

	require.NoError(t, client.Set(ctx, repository.CacheKey("bad"), "not json", 0).Err())
	_, err := redisCache.Get(ctx, "bad")
	require.Error(t, err)

	down := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", MaxRetries: -1, DialTimeout: 50 * time.Millisecond})

	t.Cleanup(func() { _ = down.Close() })

	_, err = repository.NewRedisEntitlementCache(down, time.Minute).Get(ctx, "x")
	require.Error(t, err)

	redisCounter := repository.NewRedisTransactionCounter(client)
	memoryCounter := repository.NewMemoryTransactionCounter()

	for _, c := range []struct {
		inc func() (int, error)
		get func() (int, error)
	}{
		{func() (int, error) { return redisCounter.Increment(ctx, "acc", "20260925") }, func() (int, error) { return redisCounter.Get(ctx, "acc", "20260925") }},
		{func() (int, error) { return memoryCounter.Increment(ctx, "acc", "20260925") }, func() (int, error) { return memoryCounter.Get(ctx, "acc", "20260925") }},
	} {
		n, err := c.get()
		require.NoError(t, err)
		assert.Zero(t, n)

		for want := 1; want <= 2; want++ {
			n, err = c.inc()
			require.NoError(t, err)
			assert.Equal(t, want, n)
		}

		n, err = c.get()
		require.NoError(t, err)
		assert.Equal(t, 2, n)
	}

	_, err = repository.NewRedisTransactionCounter(down).Increment(ctx, "a", "d")
	require.Error(t, err)
}
