//go:build integration

package main

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	customerDomain "github.com/viettechno/vietgara-backend/internal/customer/domain"
	customerRepository "github.com/viettechno/vietgara-backend/internal/customer/repository"
	customerUsecase "github.com/viettechno/vietgara-backend/internal/customer/usecase"
	dbpkg "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platform/db/pgtest"
)

// TestTaxProfilePersistence runs the tax profile rules through the real
// schema: one personal profile and one default per customer, a tax id
// format check, garage isolation, and a default mark that follows the
// profile that last took it.
func TestTaxProfilePersistence(t *testing.T) {
	pg := pgtest.StartMigrated(t)
	userID, garageID, otherGarageID, customerID := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()

	exec := func(query string, args ...any) {
		t.Helper()

		_, err := pg.Admin.ExecContext(t.Context(), query, args...)
		require.NoError(t, err)
	}

	exec(`INSERT INTO users (id, email, full_name) VALUES ($1, 'owner@example.com', 'Owner')`, userID)
	exec(`INSERT INTO garages (id, user_id, name) VALUES ($1, $3, 'A'), ($2, $3, 'B')`, garageID, otherGarageID, userID)
	exec(`INSERT INTO customers (id, garage_id, full_name) VALUES ($1, $2, 'Binh')`, customerID, garageID)

	pool := pg.AppPool(t)
	txm := dbpkg.NewTxManager(pool)
	customers := customerRepository.NewPostgresCustomerRepository(pool)
	uc := customerUsecase.NewTaxProfileUseCase(customerRepository.NewPostgresTaxProfileRepository(pool), customers, taxProfileUsage{})

	in := func(garage string, fn func(ctx context.Context) error) error {
		return txm.WithTransaction(t.Context(), dbpkg.Scope{GarageID: garage, UserID: userID}, fn)
	}

	company := func(name, taxID string, isDefault bool) customerUsecase.CreateTaxProfileRequest {
		return customerUsecase.CreateTaxProfileRequest{
			GarageID: garageID, CustomerID: customerID, Kind: customerDomain.TaxProfileCompany,
			TaxProfileInput: customerUsecase.TaxProfileInput{
				LegalName: new(name), TaxID: new(taxID), Address: new("12 Le Loi"), IsDefault: new(isDefault),
			},
		}
	}

	var first, second *customerDomain.TaxProfile

	require.NoError(t, in(garageID, func(ctx context.Context) error {
		var err error

		first, err = uc.Create(ctx, company("Cong ty A", "0301234567", true))
		require.NoError(t, err)

		second, err = uc.Create(ctx, company("Cong ty B", "0301234567001", true))
		require.NoError(t, err)

		list, err := uc.List(ctx, garageID, customerID, false)
		require.NoError(t, err)
		require.Len(t, list, 2)
		assert.Equal(t, second.ID, list[0].ID, "the default comes first")
		assert.False(t, list[1].IsDefault, "the mark moved to the newer profile")

		return nil
	}))

	// One personal profile per customer.
	require.NoError(t, in(garageID, func(ctx context.Context) error {
		_, err := uc.Create(ctx, customerUsecase.CreateTaxProfileRequest{
			GarageID: garageID, CustomerID: customerID, Kind: customerDomain.TaxProfilePersonal,
			TaxProfileInput: customerUsecase.TaxProfileInput{LegalName: new("Tran Binh")},
		})

		return err
	}))

	err := in(garageID, func(ctx context.Context) error {
		_, err := uc.Create(ctx, customerUsecase.CreateTaxProfileRequest{
			GarageID: garageID, CustomerID: customerID, Kind: customerDomain.TaxProfilePersonal,
			TaxProfileInput: customerUsecase.TaxProfileInput{LegalName: new("Tran Binh Again")},
		})

		return err
	})
	require.Error(t, err)

	// An unused profile is deleted, whatever its default mark.
	require.NoError(t, in(garageID, func(ctx context.Context) error {
		outcome, err := uc.Delete(ctx, customerUsecase.DeleteTaxProfileRequest{
			GarageID: garageID, CustomerID: customerID, TaxProfileID: second.ID,
		})
		require.NoError(t, err)
		assert.Equal(t, "deleted", string(outcome))

		_, err = uc.Get(ctx, garageID, customerID, second.ID)
		require.Error(t, err)

		got, err := uc.Get(ctx, garageID, customerID, first.ID)
		require.NoError(t, err)
		assert.Equal(t, "0301234567", *got.TaxID)

		return nil
	}))

	// Another garage sees nothing of it.
	require.NoError(t, in(otherGarageID, func(ctx context.Context) error {
		_, err := uc.Get(ctx, otherGarageID, customerID, first.ID)
		require.Error(t, err)

		return nil
	}))

	// The database refuses a malformed tax id even if the application did not.
	_, err = pg.Admin.ExecContext(t.Context(), `
		INSERT INTO customer_tax_profiles (garage_id, customer_id, kind, legal_name, tax_id)
		VALUES ($1, $2, 'company', 'X', '123')`, garageID, customerID)
	require.Error(t, err)
}
