//go:build integration

package main

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	billingDomain "github.com/viettechno/vietgara-backend/internal/billing/domain"
	billingRepository "github.com/viettechno/vietgara-backend/internal/billing/repository"
	dbpkg "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platform/db/pgtest"
)

// TestPaymentVoucherPersistence runs a payment voucher through the real
// repository and schema: the voucher's number, payer, reason, collector
// and the issuer snapshot are stored, read back, and a number is unique
// per garage.
func TestPaymentVoucherPersistence(t *testing.T) {
	pg := pgtest.StartMigrated(t)
	userID, garageID, otherGarageID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	settlementID, otherSettlementID := uuid.NewString(), uuid.NewString()

	exec := func(query string, args ...any) {
		t.Helper()

		_, err := pg.Admin.ExecContext(t.Context(), query, args...)
		require.NoError(t, err)
	}

	exec(`INSERT INTO users (id, email, full_name) VALUES ($1, 'owner@example.com', 'Owner')`, userID)
	exec(`INSERT INTO garages (id, user_id, name) VALUES ($1, $3, 'A'), ($2, $3, 'B')`, garageID, otherGarageID, userID)

	for _, g := range [][3]string{{garageID, settlementID, "QT-1"}, {otherGarageID, otherSettlementID, "QT-1"}} {
		roID := uuid.NewString()

		exec(`INSERT INTO customers (id, garage_id, full_name) VALUES ($1, $2, 'An')`, roID, g[0])
		exec(`INSERT INTO vehicles (id, garage_id, customer_id, license_plate) VALUES ($1, $2, $1, '51F-111.11')`, roID, g[0])
		exec(`INSERT INTO repair_orders (id, garage_id, vehicle_id, customer_name, vehicle_license_plate, number)
			VALUES ($1, $2, $1, 'An', '51F-111.11', 'LSC-1')`, roID, g[0])
		exec(`INSERT INTO settlements (id, garage_id, repair_order_id, customer_name, vehicle_license_plate, number, total_amount)
			VALUES ($1, $2, $3, 'An', '51F-111.11', $4, 1000000)`, g[1], g[0], roID, g[2])
	}

	pool := pg.AppPool(t)
	txm := dbpkg.NewTxManager(pool)
	repo := billingRepository.NewPostgresRepository(pool)

	inGarage := func(id string, fn func(ctx context.Context) error) error {
		return txm.WithTransaction(t.Context(), dbpkg.Scope{GarageID: id, UserID: userID}, fn)
	}

	payment := func(number string) *billingDomain.Payment {
		return &billingDomain.Payment{
			ID: uuid.NewString(), Amount: 300_000, Method: billingDomain.MethodCash, VoucherNumber: number,
			PayerName: "Cong ty Van Tai A", PayerAddress: "5 Le Loi", Reason: "Thanh toán quyết toán QT-1",
			CollectorName: "Le Thu Ngan", CreatedBy: &userID,
			Issuer: billingDomain.Issuer{Name: "Cong ty TNHH An Phat", TaxCode: "0301234567", BankName: "VCB", LogoKey: "k.png"},
		}
	}

	require.NoError(t, inGarage(garageID, func(ctx context.Context) error {
		_, err := repo.RecordPayment(ctx, garageID, settlementID, payment("PT-202610-0001"))
		require.NoError(t, err)

		s, err := repo.FindByID(ctx, garageID, settlementID)
		require.NoError(t, err)
		require.Len(t, s.Payments, 1)

		p := s.Payments[0]
		assert.Equal(t, "PT-202610-0001", p.VoucherNumber)
		assert.Equal(t, "Cong ty Van Tai A", p.PayerName)
		assert.Equal(t, "5 Le Loi", p.PayerAddress)
		assert.Equal(t, "Thanh toán quyết toán QT-1", p.Reason)
		assert.Equal(t, "Le Thu Ngan", p.CollectorName)
		assert.Equal(t, "Cong ty TNHH An Phat", p.Issuer.Name)
		assert.Equal(t, "0301234567", p.Issuer.TaxCode)
		assert.Equal(t, "k.png", p.Issuer.LogoKey)
		assert.Equal(t, int64(300_000), s.PaidAmount)

		return nil
	}))

	// A voucher number is unique per garage...
	err := inGarage(garageID, func(ctx context.Context) error {
		_, err := repo.RecordPayment(ctx, garageID, settlementID, payment("PT-202610-0001"))

		return err
	})
	require.ErrorContains(t, err, "settlement_payments_garage_voucher_unique")

	// ...and free in another garage.
	require.NoError(t, inGarage(otherGarageID, func(ctx context.Context) error {
		_, err := repo.RecordPayment(ctx, otherGarageID, otherSettlementID, payment("PT-202610-0001"))

		return err
	}))
}
