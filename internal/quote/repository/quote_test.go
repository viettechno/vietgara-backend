package repository_test

import (
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/platform/db/dbtest"
	"github.com/viettechno/vietgara-backend/internal/quote/domain"
	"github.com/viettechno/vietgara-backend/internal/quote/repository"
	"github.com/viettechno/vietgara-backend/internal/quote/usecase"
)

func testQuote() *domain.Quote {
	return &domain.Quote{
		ID: quoteID, GarageID: garageID, CustomerID: customerID, VehicleID: vehicleID, CustomerName: "An Nguyen",
		VehicleLicensePlate: "51F-123.45", Number: "BG-202609-0001", Status: domain.StatusDraft, ApprovalMode: domain.ApprovalModeLink,
		Lines: []domain.Line{
			{ID: lineID, Position: 1, ItemType: domain.ItemTypeLabor, Description: "Oil change", Quantity: "1", UnitPrice: 250_000, LineTotal: 250_000, Decision: domain.DecisionPending},
		},
		CreatedBy: new(actorID), UpdatedBy: new(actorID),
	}
}

func TestPostgresRepository_Create(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		q := testQuote()

		tx := dbtest.New(t).
			ExpectQueryRow("INSERT INTO quotes", dbtest.Row(now, now)).
			ExpectExec("INSERT INTO quote_lines", dbtest.Tag(1), nil)
		ctx := tx.Context(t.Context())

		require.NoError(t, repo.Create(ctx, q))
		require.Equal(t, now, q.CreatedAt)
	})

	t.Run("insert failure", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).ExpectQueryRow("INSERT INTO quotes", dbtest.ErrRow(errBoom))
		ctx := tx.Context(t.Context())

		require.ErrorIs(t, repo.Create(ctx, testQuote()), errBoom)
	})

	t.Run("line insert failure", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).
			ExpectQueryRow("INSERT INTO quotes", dbtest.Row(now, now)).
			ExpectExec("INSERT INTO quote_lines", dbtest.Tag(0), errBoom)
		ctx := tx.Context(t.Context())

		require.ErrorIs(t, repo.Create(ctx, testQuote()), errBoom)
	})
}

func TestPostgresRepository_FindByID(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).
			ExpectQueryRow("FROM quotes WHERE garage_id", dbtest.Row(quoteRow()...)).
			ExpectQuery("FROM quote_lines", dbtest.NewRows(lineRow()), nil)
		ctx := tx.Context(t.Context())

		q, err := repo.FindByID(ctx, garageID, quoteID)
		require.NoError(t, err)
		require.Equal(t, domain.StatusDraft, q.Status)
		require.Len(t, q.Lines, 1)
		require.Equal(t, "Oil change", q.Lines[0].Description)
	})

	t.Run("not found", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).ExpectQueryRow("FROM quotes WHERE garage_id", dbtest.ErrRow(pgx.ErrNoRows))
		ctx := tx.Context(t.Context())

		_, err := repo.FindByID(ctx, garageID, "missing")
		require.ErrorIs(t, err, domain.ErrQuoteNotFound)
	})

	t.Run("lines query failure", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).
			ExpectQueryRow("FROM quotes WHERE garage_id", dbtest.Row(quoteRow()...)).
			ExpectQuery("FROM quote_lines", nil, errBoom)
		ctx := tx.Context(t.Context())

		_, err := repo.FindByID(ctx, garageID, quoteID)
		require.ErrorIs(t, err, errBoom)
	})

	t.Run("line scan failure", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).
			ExpectQueryRow("FROM quotes WHERE garage_id", dbtest.Row(quoteRow()...)).
			ExpectQuery("FROM quote_lines", dbtest.NewRows(lineRow()).WithScanError(errBoom), nil)
		ctx := tx.Context(t.Context())

		_, err := repo.FindByID(ctx, garageID, quoteID)
		require.ErrorIs(t, err, errBoom)
	})
}

func TestPostgresRepository_ReplaceLines(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		q := testQuote()

		tx := dbtest.New(t).
			ExpectQueryRow("UPDATE quotes SET customer_id", dbtest.Row(now)).
			ExpectExec("DELETE FROM quote_lines", dbtest.Tag(1), nil).
			ExpectExec("INSERT INTO quote_lines", dbtest.Tag(1), nil)
		ctx := tx.Context(t.Context())

		require.NoError(t, repo.ReplaceLines(ctx, q))
		require.Equal(t, now, q.UpdatedAt)
	})

	t.Run("not found", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).ExpectQueryRow("UPDATE quotes SET customer_id", dbtest.ErrRow(pgx.ErrNoRows))
		ctx := tx.Context(t.Context())

		require.ErrorIs(t, repo.ReplaceLines(ctx, testQuote()), domain.ErrQuoteNotFound)
	})

	t.Run("delete failure", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).
			ExpectQueryRow("UPDATE quotes SET customer_id", dbtest.Row(now)).
			ExpectExec("DELETE FROM quote_lines", dbtest.Tag(0), errBoom)
		ctx := tx.Context(t.Context())

		require.ErrorIs(t, repo.ReplaceLines(ctx, testQuote()), errBoom)
	})
}

func TestPostgresRepository_Save(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		q := testQuote()

		tx := dbtest.New(t).
			ExpectQueryRow("UPDATE quotes SET customer_id", dbtest.Row(now)).
			ExpectExec("UPDATE quote_lines SET decision", dbtest.Tag(1), nil)
		ctx := tx.Context(t.Context())

		require.NoError(t, repo.Save(ctx, q))
	})

	t.Run("scalar update failure", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).ExpectQueryRow("UPDATE quotes SET customer_id", dbtest.ErrRow(errBoom))
		ctx := tx.Context(t.Context())

		require.ErrorIs(t, repo.Save(ctx, testQuote()), errBoom)
	})

	t.Run("line update failure", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).
			ExpectQueryRow("UPDATE quotes SET customer_id", dbtest.Row(now)).
			ExpectExec("UPDATE quote_lines SET decision", dbtest.Tag(0), errBoom)
		ctx := tx.Context(t.Context())

		require.ErrorIs(t, repo.Save(ctx, testQuote()), errBoom)
	})
}

func TestPostgresRepository_List(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).
			ExpectQueryRow("SELECT count(*) FROM quotes", dbtest.Row(1)).
			ExpectQuery("FROM quotes WHERE garage_id", dbtest.NewRows(quoteRow()), nil)
		ctx := tx.Context(t.Context())

		items, total, err := repo.List(ctx, garageID, usecase.ListFilter{Limit: 20, Offset: 0})
		require.NoError(t, err)
		require.Equal(t, 1, total)
		require.Len(t, items, 1)
	})

	t.Run("no matches", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).ExpectQueryRow("SELECT count(*) FROM quotes", dbtest.Row(0))
		ctx := tx.Context(t.Context())

		items, total, err := repo.List(ctx, garageID, usecase.ListFilter{Limit: 20})
		require.NoError(t, err)
		require.Equal(t, 0, total)
		require.Empty(t, items)
	})

	t.Run("count failure", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).ExpectQueryRow("SELECT count(*) FROM quotes", dbtest.ErrRow(errBoom))
		ctx := tx.Context(t.Context())

		_, _, err := repo.List(ctx, garageID, usecase.ListFilter{})
		require.ErrorIs(t, err, errBoom)
	})

	t.Run("list query failure", func(t *testing.T) {
		t.Parallel()

		repo := repository.NewPostgresRepository(nil)
		tx := dbtest.New(t).
			ExpectQueryRow("SELECT count(*) FROM quotes", dbtest.Row(1)).
			ExpectQuery("FROM quotes WHERE garage_id", nil, errBoom)
		ctx := tx.Context(t.Context())

		_, _, err := repo.List(ctx, garageID, usecase.ListFilter{Limit: 20})
		require.ErrorIs(t, err, errBoom)
	})
}

func TestPostgresRepository_SoftDelete(t *testing.T) {
	t.Parallel()

	repo := repository.NewPostgresRepository(nil)
	tx := dbtest.New(t).
		ExpectExec("UPDATE quotes SET deleted_at", dbtest.Tag(1), nil).
		ExpectExec("UPDATE quotes SET deleted_at", dbtest.Tag(0), nil).
		ExpectExec("UPDATE quotes SET deleted_at", dbtest.Tag(0), errBoom)
	ctx := tx.Context(t.Context())

	require.NoError(t, repo.SoftDelete(ctx, garageID, quoteID, actorID))
	require.ErrorIs(t, repo.SoftDelete(ctx, garageID, "missing", actorID), domain.ErrQuoteNotFound)
	require.ErrorIs(t, repo.SoftDelete(ctx, garageID, quoteID, actorID), errBoom)
}

func TestPostgresRepository_FindByPublicToken(t *testing.T) {
	t.Parallel()

	repo := repository.NewPostgresRepository(nil)
	tx := dbtest.New(t).
		ExpectQueryRow("find_quote_by_public_token", dbtest.Row(quoteID, garageID)).
		ExpectQueryRow("find_quote_by_public_token", dbtest.ErrRow(pgx.ErrNoRows))
	ctx := tx.Context(t.Context())

	gID, qID, err := repo.FindByPublicToken(ctx, "hash")
	require.NoError(t, err)
	require.Equal(t, garageID, gID)
	require.Equal(t, quoteID, qID)

	_, _, err = repo.FindByPublicToken(ctx, "bad-hash")
	require.ErrorIs(t, err, domain.ErrPublicTokenInvalid)
}

func TestPostgresRepository_ExistsLive(t *testing.T) {
	t.Parallel()

	repo := repository.NewPostgresRepository(nil)
	tx := dbtest.New(t).
		ExpectQueryRow("FROM quotes", dbtest.Row(true)).
		WithArgs(func(args []any) { require.Equal(t, []any{garageID, new(customerID), []string{vehicleID}}, args) }).
		ExpectQueryRow("FROM quotes", dbtest.Row(false)).
		WithArgs(func(args []any) { require.Equal(t, []any{garageID, (*string)(nil), []string{vehicleID}}, args) }).
		ExpectQueryRow("FROM quotes", dbtest.ErrRow(errBoom))
	ctx := tx.Context(t.Context())

	exists, err := repo.ExistsLive(ctx, garageID, customerID, []string{vehicleID})
	require.NoError(t, err)
	require.True(t, exists)

	exists, err = repo.ExistsLive(ctx, garageID, "", []string{vehicleID})
	require.NoError(t, err)
	require.False(t, exists)

	_, err = repo.ExistsLive(ctx, garageID, customerID, nil)
	require.ErrorIs(t, err, errBoom)

	exists, err = repo.ExistsLive(ctx, garageID, "", nil)
	require.NoError(t, err)
	require.False(t, exists, "nothing to look for")
}

func TestPostgresRepository_BilledToTaxProfile(t *testing.T) {
	t.Parallel()

	repo := repository.NewPostgresRepository(nil)
	tx := dbtest.New(t).
		ExpectQueryRow("billing_party->>'tax_profile_id'", dbtest.Row(true)).
		ExpectQueryRow("billing_party->>'tax_profile_id'", dbtest.ErrRow(errBoom))
	ctx := tx.Context(t.Context())

	used, err := repo.BilledToTaxProfile(ctx, garageID, "tp-1")
	require.NoError(t, err)
	require.True(t, used)

	_, err = repo.BilledToTaxProfile(ctx, garageID, "tp-1")
	require.ErrorIs(t, err, errBoom)
}

func TestPostgresRepository_LiveVehicleIDs(t *testing.T) {
	t.Parallel()

	repo := repository.NewPostgresRepository(nil)

	ids, err := repo.LiveVehicleIDs(t.Context(), garageID, nil)
	require.NoError(t, err)
	require.Empty(t, ids)

	tx := dbtest.New(t).
		ExpectQuery("FROM quotes", dbtest.NewRows([]any{vehicleID}), nil).
		ExpectQuery("FROM quotes", nil, errBoom).
		ExpectQuery("FROM quotes", dbtest.NewRows([]any{errBoom}), nil)
	ctx := tx.Context(t.Context())

	ids, err = repo.LiveVehicleIDs(ctx, garageID, []string{vehicleID})
	require.NoError(t, err)
	require.Equal(t, []string{vehicleID}, ids)

	_, err = repo.LiveVehicleIDs(ctx, garageID, []string{vehicleID})
	require.ErrorIs(t, err, errBoom)

	_, err = repo.LiveVehicleIDs(ctx, garageID, []string{vehicleID})
	require.Error(t, err)
}

func TestPostgresRepository_LinesReference(t *testing.T) {
	t.Parallel()

	repo := repository.NewPostgresRepository(nil)
	tx := dbtest.New(t).
		ExpectQueryRow("FROM quote_lines", dbtest.Row(true)).
		ExpectQueryRow("FROM quote_lines", dbtest.ErrRow(errBoom))
	ctx := tx.Context(t.Context())

	used, err := repo.LinesReference(ctx, garageID, "part-1", "")
	require.NoError(t, err)
	require.True(t, used)

	_, err = repo.LinesReference(ctx, garageID, "", "unit-1")
	require.ErrorIs(t, err, errBoom)
}
