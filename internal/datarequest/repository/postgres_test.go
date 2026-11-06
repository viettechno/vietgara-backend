package repository_test

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/datarequest/domain"
	"github.com/viettechno/vietgara-backend/internal/datarequest/repository"
	"github.com/viettechno/vietgara-backend/internal/datarequest/usecase"
	"github.com/viettechno/vietgara-backend/internal/platform/db/dbtest"
)

const (
	garageID   = "11111111-1111-1111-1111-111111111111"
	customerID = "22222222-2222-2222-2222-222222222222"
	requestID  = "44444444-4444-4444-4444-444444444444"
)

var (
	errBoom = errors.New("boom")
	now     = time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)
)

func requestRow() []any {
	return []any{requestID, garageID, customerID, "correct", "completed", "public", "details", "outcome", nil, now, &now, new("staff-1")}
}

func newRequest() *domain.Request {
	return &domain.Request{
		ID: requestID, GarageID: garageID, CustomerID: customerID, Type: domain.TypeCorrect, Status: domain.StatusCompleted,
		Source: domain.SourcePublic, Outcome: "outcome", RequestedAt: now, CompletedAt: &now, CompletedBy: new("staff-1"),
	}
}

func TestPostgresRepository_Requests(t *testing.T) {
	t.Parallel()

	repo := repository.NewPostgresRepository(nil)

	tx := dbtest.New(t).
		ExpectExec("INSERT INTO customer_data_requests", dbtest.Tag(1), nil).
		ExpectExec("INSERT INTO customer_data_requests", dbtest.Tag(0), errBoom).
		ExpectQueryRow("FROM customer_data_requests WHERE garage_id", dbtest.Row(requestRow()...)).
		ExpectQueryRow("FOR UPDATE", dbtest.Row(requestRow()...)).
		ExpectQueryRow("FROM customer_data_requests WHERE garage_id", dbtest.ErrRow(pgx.ErrNoRows)).
		ExpectQueryRow("FROM customer_data_requests WHERE garage_id", dbtest.ErrRow(errBoom)).
		ExpectExec("UPDATE customer_data_requests SET status", dbtest.Tag(1), nil).
		ExpectExec("UPDATE customer_data_requests SET status", dbtest.Tag(0), nil).
		ExpectExec("UPDATE customer_data_requests SET status", dbtest.Tag(0), errBoom).
		ExpectExec("UPDATE customer_data_requests SET details", dbtest.Tag(2), nil).
		ExpectExec("UPDATE customer_data_requests SET details", dbtest.Tag(0), errBoom)
	ctx := tx.Context(t.Context())

	require.NoError(t, repo.Create(ctx, newRequest()))
	require.ErrorIs(t, repo.Create(ctx, newRequest()), errBoom)

	got, err := repo.Find(ctx, garageID, requestID)
	require.NoError(t, err)
	assert.Equal(t, domain.TypeCorrect, got.Type)
	assert.Equal(t, domain.SourcePublic, got.Source)
	assert.Equal(t, "details", got.Details)

	_, err = repo.FindForUpdate(ctx, garageID, requestID)
	require.NoError(t, err)

	_, err = repo.Find(ctx, garageID, requestID)
	require.ErrorIs(t, err, domain.ErrRequestNotFound)

	_, err = repo.Find(ctx, garageID, requestID)
	require.ErrorIs(t, err, errBoom)

	_, err = repo.Find(ctx, garageID, "not-an-id")
	require.ErrorIs(t, err, domain.ErrRequestNotFound)

	_, err = repo.Find(ctx, garageID, "44444444-4444-4444-4444-44444444444g")
	require.ErrorIs(t, err, domain.ErrRequestNotFound)

	_, err = repo.Find(ctx, garageID, "444444444-444-4444-4444-444444444444")
	require.ErrorIs(t, err, domain.ErrRequestNotFound)

	require.NoError(t, repo.Save(ctx, newRequest()))
	require.ErrorIs(t, repo.Save(ctx, newRequest()), domain.ErrRequestNotFound)
	require.ErrorIs(t, repo.Save(ctx, newRequest()), errBoom)

	require.NoError(t, repo.ClearDetails(ctx, garageID, customerID))
	require.ErrorIs(t, repo.ClearDetails(ctx, garageID, customerID), errBoom)
}

func TestPostgresRepository_List(t *testing.T) {
	t.Parallel()

	repo := repository.NewPostgresRepository(nil)

	tx := dbtest.New(t).
		ExpectQueryRow("SELECT count(*)", dbtest.Row(1)).
		ExpectQuery("ORDER BY requested_at DESC", dbtest.NewRows(requestRow()), nil).
		ExpectQueryRow("SELECT count(*)", dbtest.Row(0)).
		ExpectQueryRow("SELECT count(*)", dbtest.ErrRow(errBoom)).
		ExpectQueryRow("SELECT count(*)", dbtest.Row(1)).
		ExpectQuery("ORDER BY requested_at DESC", nil, errBoom).
		ExpectQueryRow("SELECT count(*)", dbtest.Row(1)).
		ExpectQuery("ORDER BY requested_at DESC", dbtest.NewRows(requestRow()).WithScanError(errBoom), nil)
	ctx := tx.Context(t.Context())

	items, total, err := repo.List(ctx, garageID, usecase.Filter{CustomerID: customerID, Type: domain.TypeCorrect, Limit: 20})
	require.NoError(t, err)
	require.Equal(t, 1, total)
	assert.Equal(t, requestID, items[0].ID)

	items, total, err = repo.List(ctx, garageID, usecase.Filter{Limit: 20})
	require.NoError(t, err)
	assert.Zero(t, total)
	assert.Empty(t, items)

	for range 3 {
		_, _, err = repo.List(ctx, garageID, usecase.Filter{Limit: 20})
		require.ErrorIs(t, err, errBoom)
	}
}

func TestPostgresRepository_Confirmations(t *testing.T) {
	t.Parallel()

	repo := repository.NewPostgresRepository(nil)
	c := &domain.Confirmation{ID: "c-1", Email: "a@b.vn", Type: domain.TypeView, Locale: "vi", TokenHash: "h", SourceHash: "s", ExpiresAt: now, CreatedAt: now}

	tx := dbtest.New(t).
		ExpectExec("INSERT INTO data_request_confirmations", dbtest.Tag(1), nil).
		ExpectQueryRow("lower(email) = lower", dbtest.Row(2)).
		ExpectQueryRow("source_hash = $1", dbtest.Row(5)).
		ExpectQueryRow("DELETE FROM data_request_confirmations WHERE token_hash", dbtest.Row("c-1", "a@b.vn", "delete", "", "en", "h", "s", now, now)).
		ExpectQueryRow("DELETE FROM data_request_confirmations WHERE token_hash", dbtest.ErrRow(pgx.ErrNoRows)).
		ExpectQueryRow("DELETE FROM data_request_confirmations WHERE token_hash", dbtest.ErrRow(errBoom)).
		ExpectExec("DELETE FROM data_request_confirmations WHERE expires_at", dbtest.Tag(3), nil).
		ExpectExec("DELETE FROM data_request_confirmations WHERE expires_at", dbtest.Tag(0), errBoom)
	ctx := tx.Context(t.Context())

	require.NoError(t, repo.CreateConfirmation(ctx, c))

	n, err := repo.CountByEmail(ctx, "a@b.vn", now)
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	n, err = repo.CountBySource(ctx, "s", now)
	require.NoError(t, err)
	assert.Equal(t, 5, n)

	got, err := repo.Consume(ctx, "h", now)
	require.NoError(t, err)
	assert.Equal(t, domain.TypeDelete, got.Type)
	assert.Equal(t, "en", got.Locale)

	_, err = repo.Consume(ctx, "h", now)
	require.ErrorIs(t, err, domain.ErrTokenInvalid)

	_, err = repo.Consume(ctx, "h", now)
	require.ErrorIs(t, err, errBoom)

	purged, err := repo.PurgeExpired(ctx, now)
	require.NoError(t, err)
	assert.Equal(t, 3, purged)

	_, err = repo.PurgeExpired(ctx, now)
	require.ErrorIs(t, err, errBoom)
}

func TestPostgresRepository_Numbers(t *testing.T) {
	t.Parallel()

	repo := repository.NewPostgresRepository(nil)

	tx := dbtest.New(t).
		ExpectQuery("FROM quotes", dbtest.NewRows([]any{"BG-1"}), nil).
		ExpectQuery("FROM repair_orders ro", dbtest.NewRows([]any{"LSC-1"}, []any{"LSC-2"}), nil).
		ExpectQuery("FROM settlements s", dbtest.NewRows(), nil).
		ExpectQuery("FROM settlement_payments p", dbtest.NewRows([]any{"PT-1"}), nil).
		ExpectQuery("FROM quotes", nil, errBoom).
		ExpectQuery("FROM quotes", dbtest.NewRows([]any{"BG-1"}).WithScanError(errBoom), nil)
	ctx := tx.Context(t.Context())

	got, err := repo.Numbers(ctx, garageID, customerID)
	require.NoError(t, err)
	assert.Equal(t, usecase.DocumentNumbers{
		Quotes: []string{"BG-1"}, RepairOrders: []string{"LSC-1", "LSC-2"}, Settlements: []string{}, PaymentVouchers: []string{"PT-1"},
	}, got)

	for range 2 {
		_, err = repo.Numbers(ctx, garageID, customerID)
		require.ErrorIs(t, err, errBoom)
	}
}
