//go:build integration

package repository_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/receivable/domain"
	"github.com/viettechno/vietgara-backend/internal/receivable/usecase"
)

func (w *world) newSupplier(t *testing.T, garage, name string) string {
	t.Helper()

	id := uuid.NewString()
	_, err := w.pg.Admin.ExecContext(t.Context(), `INSERT INTO suppliers (id, garage_id, name) VALUES ($1, $2, $3)`, id, garage, name)
	require.NoError(t, err)

	return id
}

func (w *world) newPayable(t *testing.T, garage, supplier string, total int64, receipt time.Time) *domain.Payable {
	t.Helper()

	p := &domain.Payable{
		GarageID: garage, GoodsReceiptID: uuid.NewString(), GoodsReceiptNumber: "NK-" + uuid.NewString()[:4], SupplierID: supplier,
		SupplierName: "Sao Mai", ReceiptDate: receipt, TotalAmount: total, Status: domain.PayableOpen,
	}

	w.in(t, garage, func(ctx context.Context) { require.NoError(t, w.repo.AddPayable(ctx, p)) })

	var id string
	require.NoError(t, w.pg.Admin.QueryRowContext(t.Context(),
		`SELECT id::text FROM supplier_payables WHERE goods_receipt_id = $1 AND supplier_id = $2`, p.GoodsReceiptID, supplier).Scan(&id))
	p.ID = id

	return p
}

func TestPayablesAddCancelListAndAging(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	sup, other := w.newSupplier(t, w.garageA, "Sao Mai"), w.newSupplier(t, w.garageA, "Bao An")
	today := time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)

	fresh := w.newPayable(t, w.garageA, sup, 100_000, today.AddDate(0, 0, -10))
	w.newPayable(t, w.garageA, sup, 100_000, today.AddDate(0, 0, -45))
	old := w.newPayable(t, w.garageA, sup, 100_000, today.AddDate(0, 0, -120))
	w.newPayable(t, w.garageA, other, 50_000, today.AddDate(0, 0, -20))

	// A redelivered event adds nothing and does not reset what was paid.
	w.in(t, w.garageA, func(ctx context.Context) {
		again := *fresh
		again.TotalAmount = 999
		require.NoError(t, w.repo.AddPayable(ctx, &again))

		old.PaidAmount = 100_000
		old.Derive()
		require.NoError(t, w.repo.SavePayable(ctx, old))

		open, total, summary, err := w.repo.ListPayables(ctx, w.garageA, usecase.PayableFilter{Limit: 10})
		require.NoError(t, err)
		require.Equal(t, 3, total, "the paid one is left out")
		require.Equal(t, usecase.PayableSummary{OpenCount: 3, OutstandingAmount: 250_000}, summary)
		require.EqualValues(t, 100_000, open[0].TotalAmount, "oldest receipt first")
		require.Equal(t, today.AddDate(0, 0, -45), open[0].ReceiptDate)

		all, total, _, err := w.repo.ListPayables(ctx, w.garageA, usecase.PayableFilter{IncludePaid: true, Limit: 2, Offset: 2})
		require.NoError(t, err)
		require.Equal(t, 4, total)
		require.Len(t, all, 2)

		mine, total, _, err := w.repo.ListPayables(ctx, w.garageA, usecase.PayableFilter{SupplierID: other, Limit: 10})
		require.NoError(t, err)
		require.Equal(t, 1, total)
		require.Equal(t, "Sao Mai", mine[0].SupplierName)

		aging, err := w.repo.Aging(ctx, w.garageA, today)
		require.NoError(t, err)
		require.Len(t, aging, 2)

		bySupplier := map[string]domain.Aging{aging[0].SupplierID: aging[0], aging[1].SupplierID: aging[1]}
		require.Equal(t, domain.Aging{SupplierID: other, SupplierName: "Sao Mai", Total: 50_000, Current: 50_000}, bySupplier[other])
		require.Equal(t, domain.Aging{SupplierID: sup, SupplierName: "Sao Mai", Total: 200_000, Current: 100_000, Days31to60: 100_000}, bySupplier[sup],
			"the paid payable is not counted")

		require.NoError(t, w.repo.CancelPayables(ctx, w.garageA, fresh.GoodsReceiptID))

		_, total, _, err = w.repo.ListPayables(ctx, w.garageA, usecase.PayableFilter{IncludePaid: true, Limit: 10})
		require.NoError(t, err)
		require.Equal(t, 3, total, "a cancelled payable is never listed")
	})

	// Another garage sees none of it.
	w.in(t, w.garageB, func(ctx context.Context) {
		_, total, _, err := w.repo.ListPayables(ctx, w.garageB, usecase.PayableFilter{IncludePaid: true, Limit: 10})
		require.NoError(t, err)
		require.Zero(t, total)
	})
}

func TestVoucherLifecycleAndConstraints(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	sup := w.newSupplier(t, w.garageA, "Sao Mai")
	payable := w.newPayable(t, w.garageA, sup, 1_000_000, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))

	voucher := func(number string, allocations ...domain.Allocation) *domain.PaymentOutVoucher {
		return &domain.PaymentOutVoucher{
			ID: uuid.NewString(), GarageID: w.garageA, Number: number, SupplierID: sup, SupplierName: "Sao Mai", PayeeName: "Sao Mai",
			Amount: 1_000_000, Method: "cash", Reason: "Thanh toán", Issuer: []byte(`{"name":"Gara"}`), IssuerName: "Binh", CreatedBy: w.owner,
			Allocations: allocations,
		}
	}

	v := voucher("PC-2026-00001", domain.Allocation{PayableID: payable.ID, Amount: 700_000})

	w.in(t, w.garageA, func(ctx context.Context) {
		require.NoError(t, w.repo.AddVoucher(ctx, v))

		got, err := w.repo.FindVoucher(ctx, w.garageA, v.ID)
		require.NoError(t, err)
		require.Equal(t, "PC-2026-00001", got.Number)
		require.Equal(t, w.owner, got.CreatedBy)
		require.JSONEq(t, `{"name":"Gara"}`, string(got.Issuer))
		require.Equal(t, []domain.Allocation{{PayableID: payable.ID, GoodsReceiptNumber: payable.GoodsReceiptNumber, Amount: 700_000}}, got.Allocations)

		list, total, err := w.repo.ListVouchers(ctx, w.garageA, usecase.VoucherFilter{Limit: 10})
		require.NoError(t, err)
		require.Equal(t, 1, total)
		require.Len(t, list[0].Allocations, 1)

		sum, err := w.repo.PaidOut(ctx, w.garageA, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
		require.NoError(t, err)
		require.EqualValues(t, 1_000_000, sum)

		// A cancelled payable shows its allocation as unallocated.
		require.NoError(t, w.repo.CancelPayables(ctx, w.garageA, payable.GoodsReceiptID))

		got, err = w.repo.FindVoucher(ctx, w.garageA, v.ID)
		require.NoError(t, err)
		require.True(t, got.Allocations[0].Cancelled)

		require.NoError(t, w.repo.VoidVoucher(ctx, w.garageA, v.ID, "wrong", time.Now()))
		require.ErrorIs(t, w.repo.VoidVoucher(ctx, w.garageA, v.ID, "again", time.Now()), domain.ErrPaymentOutAlreadyVoided)
		require.ErrorIs(t, w.repo.VoidVoucher(ctx, w.garageA, uuid.NewString(), "x", time.Now()), domain.ErrVoucherNotFound)

		_, total, err = w.repo.ListVouchers(ctx, w.garageA, usecase.VoucherFilter{Limit: 10})
		require.NoError(t, err)
		require.Zero(t, total, "voided vouchers are left out")

		_, total, err = w.repo.ListVouchers(ctx, w.garageA, usecase.VoucherFilter{IncludeVoided: true, Limit: 10})
		require.NoError(t, err)
		require.Equal(t, 1, total)

		sum, err = w.repo.PaidOut(ctx, w.garageA, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
		require.NoError(t, err)
		require.Zero(t, sum, "a voided voucher is not paid out")
	})

	w.in(t, w.garageB, func(ctx context.Context) {
		_, err := w.repo.FindVoucher(ctx, w.garageB, v.ID)
		require.ErrorIs(t, err, domain.ErrVoucherNotFound, "another garage cannot see the voucher")
	})

	// The database enforces what the domain says: a number is unique per
	// garage, an amount is positive, and a payable cannot be overpaid.
	for name, bad := range map[string]*domain.PaymentOutVoucher{
		"duplicate number": voucher("PC-2026-00001"),
		"zero amount":      func() *domain.PaymentOutVoucher { b := voucher("PC-2026-00002"); b.Amount = 0; return b }(),
	} {
		err := w.tx.WithTransaction(t.Context(), platformdb.Scope{UserID: w.owner, GarageID: w.garageA}, func(ctx context.Context) error {
			return w.repo.AddVoucher(ctx, bad)
		})
		require.Error(t, err, name)
	}

	over := &domain.Payable{ID: payable.ID, GarageID: w.garageA, PaidAmount: 1_000_001, Status: domain.PayablePaid}
	err := w.tx.WithTransaction(t.Context(), platformdb.Scope{UserID: w.owner, GarageID: w.garageA}, func(ctx context.Context) error {
		return w.repo.SavePayable(ctx, over)
	})
	require.Error(t, err, "a payable cannot be paid more than it is owed")
}

// Two vouchers that allocate the same remainder take turns on the payables'
// lock: the second sees what the first took.
func TestLockPayablesSerializesAllocations(t *testing.T) {
	t.Parallel()

	w := newWorld(t)
	sup := w.newSupplier(t, w.garageA, "Sao Mai")
	payable := w.newPayable(t, w.garageA, sup, 1_000_000, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))

	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		wins int
	)

	for range 5 {
		wg.Go(func() {
			_ = w.tx.WithTransaction(t.Context(), platformdb.Scope{UserID: w.owner, GarageID: w.garageA}, func(ctx context.Context) error {
				locked, err := w.repo.LockPayables(ctx, w.garageA, []string{payable.ID})
				if err != nil || len(locked) != 1 || locked[0].Remaining() < 700_000 {
					return err
				}

				locked[0].PaidAmount += 700_000
				locked[0].Derive()

				mu.Lock()
				wins++
				mu.Unlock()

				return w.repo.SavePayable(ctx, locked[0])
			})
		})
	}

	wg.Wait()
	require.Equal(t, 1, wins, "exactly one of the five found room for 700,000")

	var paid int64
	require.NoError(t, w.pg.Admin.QueryRowContext(t.Context(), `SELECT paid_amount FROM supplier_payables WHERE id = $1`, payable.ID).Scan(&paid))
	require.EqualValues(t, 700_000, paid)
}
