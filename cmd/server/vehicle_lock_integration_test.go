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
	"github.com/viettechno/vietgara-backend/internal/platform/audit"
	dbpkg "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platform/db/pgtest"
	quoteRepository "github.com/viettechno/vietgara-backend/internal/quote/repository"
	repairorderRepository "github.com/viettechno/vietgara-backend/internal/repairorder/repository"
	repairorderUsecase "github.com/viettechno/vietgara-backend/internal/repairorder/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// TestVehicleIdentityLock runs FR-CUS-02 / FR-CUS-09 through the real
// schema: a vehicle on a live quote or repair order has its identity locked,
// a soft-deleted quote does not count, a correction goes through with its
// audit entry, and an owner change keeps the vehicle's history.
//
//nolint:funlen // one scenario through the real schema, read top to bottom
func TestVehicleIdentityLock(t *testing.T) {
	pg := pgtest.StartMigrated(t)
	userID, garageID := uuid.NewString(), uuid.NewString()
	customerID, otherCustomerID, vehicleID, freeVehicleID := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	quoteID := uuid.NewString()

	exec := func(query string, args ...any) {
		t.Helper()

		_, err := pg.Admin.ExecContext(t.Context(), query, args...)
		require.NoError(t, err)
	}

	exec(`INSERT INTO users (id, email, full_name) VALUES ($1, 'owner@example.com', 'Owner')`, userID)
	exec(`INSERT INTO garages (id, user_id, name) VALUES ($1, $2, 'A')`, garageID, userID)
	exec(`INSERT INTO customers (id, garage_id, full_name) VALUES ($1, $3, 'Binh'), ($2, $3, 'Hoa')`, customerID, otherCustomerID, garageID)
	exec(`INSERT INTO vehicles (id, garage_id, customer_id, license_plate) VALUES ($1, $3, $4, '51F-111.11'), ($2, $3, $4, '51F-222.22')`,
		vehicleID, freeVehicleID, garageID, customerID)
	exec(`INSERT INTO quotes (id, garage_id, customer_id, vehicle_id, customer_name, vehicle_license_plate, number)
		VALUES ($1, $2, $3, $4, 'Binh', '51F-111.11', 'BG-1')`, quoteID, garageID, customerID, vehicleID)

	pool := pg.AppPool(t)
	txm := dbpkg.NewTxManager(pool)
	quotes := quoteRepository.NewPostgresRepository(pool)
	orders := repairorderUsecase.NewRepairOrderUseCase(repairorderUsecase.Deps{
		RepairOrders: repairorderRepository.NewPostgresRepository(pool),
	})
	usage := customerUsage{quotes: quotes, repairOrders: orders}

	vehicles := customerUsecase.NewVehicleUseCase(
		customerRepository.NewPostgresVehicleRepository(pool), customerRepository.NewPostgresCustomerRepository(pool), nil, usage,
		customerUsecase.WithAuditor(audit.NewLogger(pool)),
	)

	in := func(fn func(ctx context.Context) error) error {
		return txm.WithTransaction(t.Context(), dbpkg.Scope{GarageID: garageID, UserID: userID}, fn)
	}

	// The vehicle on a live quote is locked; the other is free.
	require.NoError(t, in(func(ctx context.Context) error {
		locked, err := vehicles.Get(ctx, garageID, vehicleID)
		require.NoError(t, err)
		assert.False(t, locked.Editable)
		assert.Len(t, locked.LockedFields(), 6)

		free, err := vehicles.Get(ctx, garageID, freeVehicleID)
		require.NoError(t, err)
		assert.True(t, free.Editable)

		page, err := vehicles.List(ctx, customerUsecase.ListVehiclesRequest{GarageID: garageID})
		require.NoError(t, err)

		state := map[string]bool{}
		for _, v := range page.Items {
			state[v.ID] = v.Editable
		}

		assert.Equal(t, map[string]bool{vehicleID: false, freeVehicleID: true}, state)

		return nil
	}))

	err := in(func(ctx context.Context) error {
		_, err := vehicles.Update(ctx, customerUsecase.UpdateVehicleRequest{
			GarageID: garageID, VehicleID: vehicleID, LicensePlate: new("30A-999.99"),
		})

		return err
	})
	require.Error(t, err)
	assert.Equal(t, customerDomain.ReasonVehicleFieldLocked, apperror.ReasonOf(err))

	// The free vehicle can be edited.
	require.NoError(t, in(func(ctx context.Context) error {
		_, err := vehicles.Update(ctx, customerUsecase.UpdateVehicleRequest{
			GarageID: garageID, VehicleID: freeVehicleID, LicensePlate: new("30A-888.88"),
		})

		return err
	}))

	// A correction goes through the lock and is audited with its reason.
	require.NoError(t, in(func(ctx context.Context) error {
		v, err := vehicles.Correct(ctx, customerUsecase.CorrectVehicleRequest{
			GarageID: garageID, VehicleID: vehicleID, ActorID: userID,
			Field: customerDomain.FieldLicensePlate, Value: "51F-333.33", Reason: "nhập nhầm",
		})
		require.NoError(t, err)
		assert.Equal(t, "51F-333.33", v.LicensePlate)
		assert.False(t, v.Editable)

		return nil
	}))

	var reason, action string

	require.NoError(t, pg.Admin.QueryRowContext(t.Context(),
		`SELECT action, after->>'reason' FROM audit_log WHERE entity_id = $1`, vehicleID).Scan(&action, &reason))
	assert.Equal(t, customerUsecase.ActionVehicleCorrected, action)
	assert.Equal(t, "nhập nhầm", reason)

	// The quote keeps the plate it snapshotted.
	var quotePlate string

	require.NoError(t, pg.Admin.QueryRowContext(t.Context(),
		`SELECT vehicle_license_plate FROM quotes WHERE id = $1`, quoteID).Scan(&quotePlate))
	assert.Equal(t, "51F-111.11", quotePlate)

	// A soft-deleted quote no longer locks the vehicle.
	exec(`UPDATE quotes SET deleted_at = now() WHERE id = $1`, quoteID)

	require.NoError(t, in(func(ctx context.Context) error {
		v, err := vehicles.Get(ctx, garageID, vehicleID)
		require.NoError(t, err)
		assert.True(t, v.Editable)

		return nil
	}))

	// Owner change keeps the vehicle, now with its new owner.
	require.NoError(t, in(func(ctx context.Context) error {
		v, err := vehicles.ChangeOwner(ctx, customerUsecase.ChangeVehicleOwnerRequest{
			GarageID: garageID, VehicleID: vehicleID, ActorID: userID, CustomerID: otherCustomerID, Reason: "đã bán",
		})
		require.NoError(t, err)
		assert.Equal(t, otherCustomerID, v.CustomerID)
		assert.Equal(t, "Hoa", v.CustomerName)

		return nil
	}))
}
