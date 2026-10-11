//go:build integration

package main

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dbpkg "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platform/db/pgtest"
	quoteRepository "github.com/viettechno/vietgara-backend/internal/quote/repository"
	repairorderRepository "github.com/viettechno/vietgara-backend/internal/repairorder/repository"
	repairorderUsecase "github.com/viettechno/vietgara-backend/internal/repairorder/usecase"
)

// TestCustomerUsage runs the delete-or-deactivate reference check against
// real quotes and repair orders (FR-CAT-07): a customer or vehicle that a
// live document refers to is "in use"; a deleted quote does not count.
func TestCustomerUsage(t *testing.T) {
	pg := pgtest.StartMigrated(t)
	userID, garageID := uuid.NewString(), uuid.NewString()
	customerID, vehicleID, otherCustomerID, otherVehicleID := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	quoteID := uuid.NewString()

	exec := func(query string, args ...any) {
		t.Helper()

		_, err := pg.Admin.ExecContext(t.Context(), query, args...)
		require.NoError(t, err)
	}

	exec(`INSERT INTO users (id, email, full_name) VALUES ($1, 'owner@example.com', 'Owner')`, userID)
	exec(`INSERT INTO garages (id, user_id, name) VALUES ($1, $2, 'G')`, garageID, userID)

	for _, c := range [][3]string{{customerID, "Used", "+84911000001"}, {otherCustomerID, "Unused", "+84911000002"}} {
		exec(`INSERT INTO customers (id, garage_id, full_name, phone) VALUES ($1, $2, $3, $4)`, c[0], garageID, c[1], c[2])
	}

	exec(`INSERT INTO vehicles (id, garage_id, customer_id, license_plate) VALUES ($1, $2, $3, '51F-111.11')`, vehicleID, garageID, customerID)
	exec(`INSERT INTO vehicles (id, garage_id, customer_id, license_plate) VALUES ($1, $2, $3, '51F-222.22')`, otherVehicleID, garageID, otherCustomerID)
	exec(`INSERT INTO quotes (id, garage_id, customer_id, vehicle_id, customer_name, vehicle_license_plate, number)
		VALUES ($1, $2, $3, $4, 'Used', '51F-111.11', 'BG-1')`, quoteID, garageID, customerID, vehicleID)

	pool := pg.AppPool(t)
	txm := dbpkg.NewTxManager(pool)
	usage := customerUsage{
		quotes: quoteRepository.NewPostgresRepository(pool),
		repairOrders: repairorderUsecase.NewRepairOrderUseCase(repairorderUsecase.Deps{
			RepairOrders: repairorderRepository.NewPostgresRepository(pool),
		}),
	}

	inGarage := func(fn func(ctx context.Context)) {
		t.Helper()

		require.NoError(t, txm.WithTransaction(t.Context(), dbpkg.GarageScope(garageID), func(ctx context.Context) error {
			fn(ctx)

			return nil
		}))
	}

	inGarage(func(ctx context.Context) {
		got, err := usage.CustomerInUse(ctx, garageID, customerID, []string{vehicleID})
		require.NoError(t, err)
		assert.True(t, got, "a live quote refers to the customer")

		got, err = usage.VehicleInUse(ctx, garageID, vehicleID)
		require.NoError(t, err)
		assert.True(t, got, "a live quote refers to the vehicle")

		got, err = usage.CustomerInUse(ctx, garageID, otherCustomerID, []string{otherVehicleID})
		require.NoError(t, err)
		assert.False(t, got, "nothing refers to the other customer")

		got, err = usage.VehicleInUse(ctx, garageID, otherVehicleID)
		require.NoError(t, err)
		assert.False(t, got)
	})

	// A quote on the other customer's vehicle makes that customer in use
	// through the vehicle, even though the quote names someone else.
	exec(`INSERT INTO quotes (id, garage_id, customer_id, vehicle_id, customer_name, vehicle_license_plate, number)
		VALUES ($1, $2, $3, $4, 'Used', '51F-222.22', 'BG-2')`, uuid.NewString(), garageID, customerID, otherVehicleID)

	inGarage(func(ctx context.Context) {
		got, err := usage.CustomerInUse(ctx, garageID, otherCustomerID, []string{otherVehicleID})
		require.NoError(t, err)
		assert.True(t, got)
	})

	// A deleted quote no longer counts.
	exec(`UPDATE quotes SET deleted_at = now() WHERE garage_id = $1 AND vehicle_id = ANY($2)`, garageID, []string{vehicleID, otherVehicleID})

	inGarage(func(ctx context.Context) {
		got, err := usage.VehicleInUse(ctx, garageID, vehicleID)
		require.NoError(t, err)
		assert.False(t, got)
	})

	// A repair order on the vehicle does.
	exec(`INSERT INTO repair_orders (id, garage_id, vehicle_id, customer_name, vehicle_license_plate, number)
		VALUES ($1, $2, $3, 'Used', '51F-111.11', 'LSC-1')`, uuid.NewString(), garageID, vehicleID)

	inGarage(func(ctx context.Context) {
		got, err := usage.CustomerInUse(ctx, garageID, customerID, []string{vehicleID})
		require.NoError(t, err)
		assert.True(t, got)
	})
}

func TestSupplierUsage(t *testing.T) {
	got, err := supplierUsage{}.SupplierInUse(t.Context(), "g", "s")
	require.NoError(t, err)
	assert.False(t, got, "nothing refers to suppliers before goods receipts")
}
