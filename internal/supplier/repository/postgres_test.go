package repository_test

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/platform/db/dbtest"
	"github.com/viettechno/vietgara-backend/internal/supplier/domain"
	"github.com/viettechno/vietgara-backend/internal/supplier/repository"
	"github.com/viettechno/vietgara-backend/internal/supplier/usecase"
)

const (
	garageID   = "11111111-1111-4111-8111-111111111111"
	supplierID = "44444444-4444-4444-8444-444444444444"
	actorID    = "55555555-5555-4555-8555-555555555555"
)

var (
	now     = time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	errBoom = errors.New("boom")
)

func supplierRow(contact, bank string) []any {
	return []any{
		supplierID, garageID, "Phu Tung Sai Gon", nil, []byte(contact), []byte(bank), new("VIP"), nil, nil, now, now,
	}
}

func bytesArg(t *testing.T, v any) []byte {
	t.Helper()

	b, ok := v.([]byte)
	require.True(t, ok, "expected a JSON document argument, got %T", v)

	return b
}

func newSupplier() *domain.Supplier {
	return &domain.Supplier{
		ID: supplierID, GarageID: garageID, Name: "Phu Tung Sai Gon",
		Contact: domain.Contact{Phone: new("0281234567")},
		Bank:    domain.BankDetails{BankName: new("Vietcombank")},
	}
}

func TestPostgresRepository_CreateAndUpdate(t *testing.T) {
	t.Parallel()

	repo := repository.NewPostgresRepository(nil)

	t.Run("create stores the JSON blocks and scans the timestamps", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).
			ExpectQueryRow("INSERT INTO suppliers", dbtest.Row(now, now)).
			WithArgs(func(args []any) {
				require.JSONEq(t, `{"phone":"0281234567"}`, string(bytesArg(t, args[4])))
				require.JSONEq(t, `{"bank_name":"Vietcombank"}`, string(bytesArg(t, args[5])))
			})

		s := newSupplier()
		require.NoError(t, repo.Create(tx.Context(t.Context()), s))
		assert.Equal(t, now, s.CreatedAt)
		assert.Equal(t, now, s.UpdatedAt)
	})

	t.Run("create reports a taken name", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).ExpectQueryRow("INSERT INTO suppliers",
			dbtest.ErrRow(dbtest.UniqueViolation("idx_suppliers_garage_name_unique")))

		require.ErrorIs(t, repo.Create(tx.Context(t.Context()), newSupplier()), domain.ErrSupplierNameTaken)
	})

	t.Run("create returns other errors", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).ExpectQueryRow("INSERT INTO suppliers", dbtest.ErrRow(errBoom))

		require.ErrorIs(t, repo.Create(tx.Context(t.Context()), newSupplier()), errBoom)
	})

	t.Run("update", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).ExpectQueryRow("UPDATE suppliers SET name", dbtest.Row(now))

		s := newSupplier()
		require.NoError(t, repo.Update(tx.Context(t.Context()), s))
		assert.Equal(t, now, s.UpdatedAt)
	})

	t.Run("update of a missing supplier", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).ExpectQueryRow("UPDATE suppliers SET name", dbtest.ErrRow(pgx.ErrNoRows))

		require.ErrorIs(t, repo.Update(tx.Context(t.Context()), newSupplier()), domain.ErrSupplierNotFound)
	})

	t.Run("update reports a taken name", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).ExpectQueryRow("UPDATE suppliers SET name",
			dbtest.ErrRow(dbtest.UniqueViolation("idx_suppliers_garage_name_unique")))

		require.ErrorIs(t, repo.Update(tx.Context(t.Context()), newSupplier()), domain.ErrSupplierNameTaken)
	})
}

func TestPostgresRepository_FindByID(t *testing.T) {
	t.Parallel()

	repo := repository.NewPostgresRepository(nil)

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).ExpectQueryRow("FROM suppliers s WHERE",
			dbtest.Row(supplierRow(`{"contact_person":"Mr. Minh"}`, `{"account_number":"123"}`)...))

		s, err := repo.FindByID(tx.Context(t.Context()), garageID, supplierID)
		require.NoError(t, err)
		assert.Equal(t, "Mr. Minh", *s.Contact.ContactPerson)
		assert.Equal(t, "123", *s.Bank.AccountNumber)
		assert.Equal(t, "VIP", *s.Note)
	})

	t.Run("corrupt contact JSON", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).ExpectQueryRow("FROM suppliers s WHERE", dbtest.Row(supplierRow(`[`, `{}`)...))

		_, err := repo.FindByID(tx.Context(t.Context()), garageID, supplierID)
		require.Error(t, err)
	})

	t.Run("corrupt bank JSON", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).ExpectQueryRow("FROM suppliers s WHERE", dbtest.Row(supplierRow(`{}`, `[`)...))

		_, err := repo.FindByID(tx.Context(t.Context()), garageID, supplierID)
		require.Error(t, err)
	})

	t.Run("not found", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).ExpectQueryRow("FROM suppliers s WHERE", dbtest.ErrRow(pgx.ErrNoRows))

		_, err := repo.FindByID(tx.Context(t.Context()), garageID, supplierID)
		require.ErrorIs(t, err, domain.ErrSupplierNotFound)
	})

	t.Run("not a uuid", func(t *testing.T) {
		t.Parallel()

		_, err := repo.FindByID(t.Context(), garageID, "nope")
		require.ErrorIs(t, err, domain.ErrSupplierNotFound)
	})
}

func TestPostgresRepository_List(t *testing.T) {
	t.Parallel()

	repo := repository.NewPostgresRepository(nil)

	t.Run("a page of matches", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).
			ExpectQueryRow("SELECT count(*)", dbtest.Row(1)).
			WithArgs(func(args []any) { require.Equal(t, []any{garageID, "%sai 12%", "%SAI12%"}, args) }).
			ExpectQuery("ORDER BY lower(s.name)", dbtest.NewRows(supplierRow(`{}`, `{}`)), nil).
			WithArgs(func(args []any) { require.Equal(t, []any{garageID, "%sai 12%", "%SAI12%", 20, 0}, args) })

		list, total, err := repo.List(tx.Context(t.Context()), garageID, usecase.ListFilter{Query: "sai 12", Limit: 20})
		require.NoError(t, err)
		assert.Equal(t, 1, total)
		require.Len(t, list, 1)
		assert.Equal(t, "Phu Tung Sai Gon", list[0].Name)
	})

	t.Run("no matches", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).ExpectQueryRow("SELECT count(*)", dbtest.Row(0))

		list, total, err := repo.List(tx.Context(t.Context()), garageID, usecase.ListFilter{Limit: 20})
		require.NoError(t, err)
		assert.Zero(t, total)
		assert.Empty(t, list)
	})

	t.Run("count failure", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).ExpectQueryRow("SELECT count(*)", dbtest.ErrRow(errBoom))

		_, _, err := repo.List(tx.Context(t.Context()), garageID, usecase.ListFilter{Limit: 20})
		require.ErrorIs(t, err, errBoom)
	})

	t.Run("query failure", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).
			ExpectQueryRow("SELECT count(*)", dbtest.Row(1)).
			ExpectQuery("ORDER BY lower(s.name)", nil, errBoom)

		_, _, err := repo.List(tx.Context(t.Context()), garageID, usecase.ListFilter{Limit: 20})
		require.ErrorIs(t, err, errBoom)
	})

	t.Run("scan failure", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).
			ExpectQueryRow("SELECT count(*)", dbtest.Row(1)).
			ExpectQuery("ORDER BY lower(s.name)", dbtest.NewRows(supplierRow(`{}`, `{}`)).WithScanError(errBoom), nil)

		_, _, err := repo.List(tx.Context(t.Context()), garageID, usecase.ListFilter{Limit: 20})
		require.ErrorIs(t, err, errBoom)
	})
}

func TestPostgresRepository_SoftDelete(t *testing.T) {
	t.Parallel()

	repo := repository.NewPostgresRepository(nil)

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).ExpectExec("UPDATE suppliers SET deleted_at", dbtest.Tag(1), nil)

		require.NoError(t, repo.SoftDelete(tx.Context(t.Context()), garageID, supplierID, actorID))
	})

	t.Run("unknown actor stores NULL", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).
			ExpectExec("UPDATE suppliers SET deleted_at", dbtest.Tag(1), nil).
			WithArgs(func(args []any) { require.Equal(t, (*string)(nil), args[2]) })

		require.NoError(t, repo.SoftDelete(tx.Context(t.Context()), garageID, supplierID, ""))
	})

	t.Run("not found", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).ExpectExec("UPDATE suppliers SET deleted_at", dbtest.Tag(0), nil)

		require.ErrorIs(t, repo.SoftDelete(tx.Context(t.Context()), garageID, supplierID, actorID), domain.ErrSupplierNotFound)
	})

	t.Run("exec failure", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).ExpectExec("UPDATE suppliers SET deleted_at", dbtest.Tag(0), errBoom)

		require.ErrorIs(t, repo.SoftDelete(tx.Context(t.Context()), garageID, supplierID, actorID), errBoom)
	})

	t.Run("not a uuid", func(t *testing.T) {
		t.Parallel()

		require.ErrorIs(t, repo.SoftDelete(t.Context(), garageID, "nope", actorID), domain.ErrSupplierNotFound)
	})
}
