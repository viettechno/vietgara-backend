//go:build integration

package repository_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dbpkg "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platform/db/pgtest"
	"github.com/viettechno/vietgara-backend/internal/supplier/domain"
	"github.com/viettechno/vietgara-backend/internal/supplier/repository"
	"github.com/viettechno/vietgara-backend/internal/supplier/usecase"
)

// TestSupplierRepository walks the repository against a migrated Postgres
// bound to a vietgara_app login, so the FORCE ROW LEVEL SECURITY policy
// applies.
func TestSupplierRepository(t *testing.T) {
	pg := pgtest.StartMigrated(t)
	userID, garageA, garageB := uuid.NewString(), uuid.NewString(), uuid.NewString()

	_, err := pg.Admin.ExecContext(t.Context(), `INSERT INTO users (id, email, full_name) VALUES ($1, 'owner@example.com', 'Owner')`, userID)
	require.NoError(t, err)

	_, err = pg.Admin.ExecContext(t.Context(), `INSERT INTO garages (id, user_id, name) VALUES ($1, $3, 'A'), ($2, $3, 'B')`,
		garageA, garageB, userID)
	require.NoError(t, err)

	pool := pg.AppPool(t)
	txm := dbpkg.NewTxManager(pool)
	repo := repository.NewPostgresRepository(pool)

	inGarage := func(garageID string, fn func(ctx context.Context)) {
		t.Helper()

		require.NoError(t, txm.WithTransaction(t.Context(), dbpkg.GarageScope(garageID), func(ctx context.Context) error {
			fn(ctx)

			return nil
		}))
	}

	person, phone, bank := "Ms. Lan", "0281234567", "Vietcombank"
	supplier := &domain.Supplier{
		ID: uuid.NewString(), GarageID: garageA, Name: "Phu Tung Sai Gon",
		Contact: domain.Contact{ContactPerson: &person, Phone: &phone},
		Bank:    domain.BankDetails{BankName: &bank},
	}

	inGarage(garageA, func(ctx context.Context) {
		require.NoError(t, repo.Create(ctx, supplier))
		require.NoError(t, repo.Create(ctx, &domain.Supplier{ID: uuid.NewString(), GarageID: garageA, Name: "Bao Duong Xe"}))

		got, err := repo.FindByID(ctx, garageA, supplier.ID)
		require.NoError(t, err)
		assert.Equal(t, person, *got.Contact.ContactPerson)
		assert.Equal(t, bank, *got.Bank.BankName)

		list, total, err := repo.List(ctx, garageA, usecase.ListFilter{Limit: 10})
		require.NoError(t, err)
		require.Equal(t, 2, total)
		assert.Equal(t, "Bao Duong Xe", list[0].Name, "ordered by name")

		for _, query := range []string{"lan", "028 123", "sai gon"} {
			_, total, err := repo.List(ctx, garageA, usecase.ListFilter{Query: query, Limit: 10})
			require.NoError(t, err)
			assert.Equal(t, 1, total, query)
		}

		note := "Pays within 30 days"
		got.Note = &note
		require.NoError(t, repo.Update(ctx, got))

		got, err = repo.FindByID(ctx, garageA, supplier.ID)
		require.NoError(t, err)
		assert.Equal(t, note, *got.Note)
	})

	err = txm.WithTransaction(t.Context(), dbpkg.GarageScope(garageA), func(ctx context.Context) error {
		return repo.Create(ctx, &domain.Supplier{ID: uuid.NewString(), GarageID: garageA, Name: "phu tung SAI GON"})
	})
	require.ErrorIs(t, err, domain.ErrSupplierNameTaken)

	// The same name is free in another garage.
	inGarage(garageB, func(ctx context.Context) {
		require.NoError(t, repo.Create(ctx, &domain.Supplier{ID: uuid.NewString(), GarageID: garageB, Name: "Phu Tung Sai Gon"}))
	})

	inGarage(garageA, func(ctx context.Context) {
		require.NoError(t, repo.SoftDelete(ctx, garageA, supplier.ID, userID))
		_, err := repo.FindByID(ctx, garageA, supplier.ID)
		require.ErrorIs(t, err, domain.ErrSupplierNotFound)

		// A deleted supplier's name can be reused.
		require.NoError(t, repo.Create(ctx, &domain.Supplier{ID: uuid.NewString(), GarageID: garageA, Name: "Phu Tung Sai Gon"}))
	})

	// Row-level security: garage B cannot see garage A's suppliers.
	inGarage(garageB, func(ctx context.Context) {
		_, total, err := repo.List(ctx, garageA, usecase.ListFilter{Limit: 10})
		require.NoError(t, err)
		assert.Zero(t, total)
	})
}
