package repository_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/platform/db/dbtest"
	"github.com/viettechno/vietgara-backend/internal/receivable/domain"
	"github.com/viettechno/vietgara-backend/internal/receivable/usecase"
)

const (
	payableRowID = "p-1"
	voucherRowID = "v-1"
	supplierRow  = "sup-1"
)

var receiptDay = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

func payableRow() []any {
	return []any{payableRowID, garageID, "gr-1", "NK-0001", supplierRow, "Sao Mai", receiptDay, int64(700_000), int64(200_000), "open", now}
}

func voucherRow(voidedAt *time.Time) []any {
	return []any{
		voucherRowID, garageID, "PC-2026-00001", supplierRow, "Sao Mai", "Sao Mai", "12 Le Loi", int64(1_000_000), "cash",
		"Thanh toán", "", []byte(`{"name":"Gara"}`), "Binh", "", voidedAt, now, "u-1",
	}
}

func TestPayables_AddAndCancel(t *testing.T) {
	t.Parallel()

	p := &domain.Payable{
		GarageID: garageID, GoodsReceiptID: "gr-1", GoodsReceiptNumber: "NK-0001", SupplierID: supplierRow, SupplierName: "Sao Mai",
		ReceiptDate: receiptDay, TotalAmount: 700_000,
	}

	tx := dbtest.New(t).ExpectExec("INSERT INTO supplier_payables", dbtest.Tag(1), nil).WithArgs(func(args []any) {
		require.Equal(t, []any{garageID, "gr-1", "NK-0001", supplierRow, "Sao Mai", receiptDay, int64(700_000)}, args)
	})
	require.NoError(t, repo().AddPayable(tx.Context(t.Context()), p))

	tx = dbtest.New(t).ExpectExec("INSERT INTO supplier_payables", dbtest.Tag(0), errBoom)
	require.ErrorIs(t, repo().AddPayable(tx.Context(t.Context()), p), errBoom)

	tx = dbtest.New(t).ExpectExec("UPDATE supplier_payables p SET status = 'cancelled'", dbtest.Tag(2), nil)
	require.NoError(t, repo().CancelPayables(tx.Context(t.Context()), garageID, "gr-1"))

	tx = dbtest.New(t).ExpectExec("UPDATE supplier_payables p SET status = 'cancelled'", dbtest.Tag(0), errBoom)
	require.ErrorIs(t, repo().CancelPayables(tx.Context(t.Context()), garageID, "gr-1"), errBoom)
}

func TestPayables_List(t *testing.T) {
	t.Parallel()

	filter := usecase.PayableFilter{SupplierID: supplierRow, IncludePaid: true, Limit: 10, Offset: 10}

	t.Run("a page with its summary", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).
			ExpectQueryRow("count(*)", dbtest.Row(12, 5, int64(3_000_000))).
			ExpectQuery("FROM supplier_payables", dbtest.NewRows(payableRow()), nil).WithArgs(func(args []any) {
			require.Equal(t, []any{garageID, supplierRow, true, 10, 10}, args)
		})

		items, total, summary, err := repo().ListPayables(tx.Context(t.Context()), garageID, filter)
		require.NoError(t, err)
		require.Equal(t, 12, total)
		require.Equal(t, usecase.PayableSummary{OpenCount: 5, OutstandingAmount: 3_000_000}, summary)
		require.Len(t, items, 1)
		require.Equal(t, domain.PayableOpen, items[0].Status)
		require.EqualValues(t, 500_000, items[0].Remaining())
	})

	t.Run("an empty match reads no page", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).ExpectQueryRow("count(*)", dbtest.Row(0, 0, int64(0)))
		items, total, _, err := repo().ListPayables(tx.Context(t.Context()), garageID, filter)
		require.NoError(t, err)
		require.Zero(t, total)
		require.Empty(t, items)
	})

	t.Run("failures", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).ExpectQueryRow("count(*)", dbtest.ErrRow(errBoom))
		_, _, _, err := repo().ListPayables(tx.Context(t.Context()), garageID, filter)
		require.ErrorIs(t, err, errBoom)

		tx = dbtest.New(t).ExpectQueryRow("count(*)", dbtest.Row(1, 1, int64(1))).ExpectQuery("FROM supplier_payables", nil, errBoom)
		_, _, _, err = repo().ListPayables(tx.Context(t.Context()), garageID, filter)
		require.ErrorIs(t, err, errBoom)

		tx = dbtest.New(t).ExpectQueryRow("count(*)", dbtest.Row(1, 1, int64(1))).
			ExpectQuery("FROM supplier_payables", dbtest.NewRows(payableRow()).WithScanError(errBoom), nil)
		_, _, _, err = repo().ListPayables(tx.Context(t.Context()), garageID, filter)
		require.ErrorIs(t, err, errBoom)

		tx = dbtest.New(t).ExpectQueryRow("count(*)", dbtest.Row(1, 1, int64(1))).
			ExpectQuery("FROM supplier_payables", dbtest.NewRows().WithErr(errBoom), nil)
		_, _, _, err = repo().ListPayables(tx.Context(t.Context()), garageID, filter)
		require.ErrorIs(t, err, errBoom)
	})
}

func TestPayables_AgingLockSave(t *testing.T) {
	t.Parallel()

	tx := dbtest.New(t).ExpectQuery("GROUP BY supplier_id", dbtest.NewRows(
		[]any{supplierRow, "Sao Mai", int64(1_000), int64(100), int64(200), int64(300), int64(400)}), nil)
	aging, err := repo().Aging(tx.Context(t.Context()), garageID, now)
	require.NoError(t, err)
	require.Equal(t, []domain.Aging{{SupplierID: supplierRow, SupplierName: "Sao Mai", Total: 1_000, Current: 100, Days31to60: 200, Days61to90: 300, Over90: 400}}, aging)

	tx = dbtest.New(t).ExpectQuery("GROUP BY supplier_id", nil, errBoom)
	_, err = repo().Aging(tx.Context(t.Context()), garageID, now)
	require.ErrorIs(t, err, errBoom)

	tx = dbtest.New(t).ExpectQuery("GROUP BY supplier_id", dbtest.NewRows([]any{supplierRow}).WithScanError(errBoom), nil)
	_, err = repo().Aging(tx.Context(t.Context()), garageID, now)
	require.ErrorIs(t, err, errBoom)

	tx = dbtest.New(t).ExpectQuery("FOR UPDATE", dbtest.NewRows(payableRow()), nil)
	locked, err := repo().LockPayables(tx.Context(t.Context()), garageID, []string{payableRowID})
	require.NoError(t, err)
	require.Len(t, locked, 1)

	tx = dbtest.New(t).ExpectQuery("FOR UPDATE", nil, errBoom)
	_, err = repo().LockPayables(tx.Context(t.Context()), garageID, []string{payableRowID})
	require.ErrorIs(t, err, errBoom)

	tx = dbtest.New(t).ExpectQuery("FOR UPDATE", dbtest.NewRows(payableRow()).WithScanError(errBoom), nil)
	_, err = repo().LockPayables(tx.Context(t.Context()), garageID, []string{payableRowID})
	require.ErrorIs(t, err, errBoom)

	p := &domain.Payable{ID: payableRowID, GarageID: garageID, PaidAmount: 700_000, Status: domain.PayablePaid}

	tx = dbtest.New(t).ExpectExec("UPDATE supplier_payables SET paid_amount", dbtest.Tag(1), nil).WithArgs(func(args []any) {
		require.Equal(t, []any{garageID, payableRowID, int64(700_000), "paid"}, args)
	})
	require.NoError(t, repo().SavePayable(tx.Context(t.Context()), p))

	tx = dbtest.New(t).ExpectExec("UPDATE supplier_payables SET paid_amount", dbtest.Tag(0), errBoom)
	require.ErrorIs(t, repo().SavePayable(tx.Context(t.Context()), p), errBoom)
}

func TestVouchers_Add(t *testing.T) {
	t.Parallel()

	v := &domain.PaymentOutVoucher{
		ID: voucherRowID, GarageID: garageID, Number: "PC-2026-00001", SupplierID: supplierRow, SupplierName: "Sao Mai",
		PayeeName: "Sao Mai", Amount: 1_000_000, Method: "cash", Reason: "Thanh toán", Issuer: []byte(`{}`), IssuerName: "Binh", CreatedBy: "u-1",
		Allocations: []domain.Allocation{{PayableID: payableRowID, Amount: 700_000}, {PayableID: "p-2", Amount: 100_000}},
	}

	tx := dbtest.New(t).
		ExpectQueryRow("INSERT INTO payment_out_vouchers", dbtest.Row(now)).
		ExpectExec("INSERT INTO payment_out_allocations", dbtest.Tag(1), nil).
		ExpectExec("INSERT INTO payment_out_allocations", dbtest.Tag(1), nil)
	require.NoError(t, repo().AddVoucher(tx.Context(t.Context()), v))
	require.Equal(t, now, v.CreatedAt)

	tx = dbtest.New(t).ExpectQueryRow("INSERT INTO payment_out_vouchers", dbtest.ErrRow(errBoom))
	require.ErrorIs(t, repo().AddVoucher(tx.Context(t.Context()), v), errBoom)

	tx = dbtest.New(t).ExpectQueryRow("INSERT INTO payment_out_vouchers", dbtest.Row(now)).
		ExpectExec("INSERT INTO payment_out_allocations", dbtest.Tag(0), errBoom)
	require.ErrorIs(t, repo().AddVoucher(tx.Context(t.Context()), v), errBoom)
}

func TestVouchers_Find(t *testing.T) {
	t.Parallel()

	voided := now.Add(time.Hour)

	tx := dbtest.New(t).
		ExpectQueryRow("FROM payment_out_vouchers WHERE garage_id = $1 AND id = $2", dbtest.Row(voucherRow(&voided)...)).
		ExpectQuery("FROM payment_out_allocations", dbtest.NewRows(
			[]any{voucherRowID, payableRowID, "NK-0001", int64(700_000), false},
			[]any{voucherRowID, "p-2", "NK-0002", int64(100_000), true}), nil)

	v, err := repo().FindVoucher(tx.Context(t.Context()), garageID, voucherRowID)
	require.NoError(t, err)
	require.True(t, v.Voided())
	require.Equal(t, []domain.Allocation{
		{PayableID: payableRowID, GoodsReceiptNumber: "NK-0001", Amount: 700_000},
		{PayableID: "p-2", GoodsReceiptNumber: "NK-0002", Amount: 100_000, Cancelled: true},
	}, v.Allocations)
	require.EqualValues(t, 700_000, v.Allocated())

	tx = dbtest.New(t).ExpectQueryRow("FROM payment_out_vouchers WHERE", dbtest.ErrRow(pgxNoRows()))
	_, err = repo().FindVoucher(tx.Context(t.Context()), garageID, voucherRowID)
	require.ErrorIs(t, err, domain.ErrVoucherNotFound)

	tx = dbtest.New(t).ExpectQueryRow("FROM payment_out_vouchers WHERE", dbtest.ErrRow(errBoom))
	_, err = repo().FindVoucher(tx.Context(t.Context()), garageID, voucherRowID)
	require.ErrorIs(t, err, errBoom)

	tx = dbtest.New(t).ExpectQueryRow("FROM payment_out_vouchers WHERE", dbtest.Row(voucherRow(nil)...)).
		ExpectQuery("FROM payment_out_allocations", nil, errBoom)
	_, err = repo().FindVoucher(tx.Context(t.Context()), garageID, voucherRowID)
	require.ErrorIs(t, err, errBoom)

	tx = dbtest.New(t).ExpectQueryRow("FROM payment_out_vouchers WHERE", dbtest.Row(voucherRow(nil)...)).
		ExpectQuery("FROM payment_out_allocations", dbtest.NewRows([]any{voucherRowID}).WithScanError(errBoom), nil)
	_, err = repo().FindVoucher(tx.Context(t.Context()), garageID, voucherRowID)
	require.ErrorIs(t, err, errBoom)
}

func TestVouchers_List(t *testing.T) {
	t.Parallel()

	filter := usecase.VoucherFilter{SupplierID: supplierRow, IncludeVoided: true, Limit: 10, Offset: 0}

	tx := dbtest.New(t).
		ExpectQueryRow("SELECT count(*) FROM payment_out_vouchers", dbtest.Row(1)).
		ExpectQuery("FROM payment_out_vouchers", dbtest.NewRows(voucherRow(nil)), nil).
		ExpectQuery("FROM payment_out_allocations", dbtest.NewRows([]any{voucherRowID, payableRowID, "NK-0001", int64(700_000), false}), nil)

	items, total, err := repo().ListVouchers(tx.Context(t.Context()), garageID, filter)
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Len(t, items[0].Allocations, 1)

	tx = dbtest.New(t).ExpectQueryRow("SELECT count(*)", dbtest.Row(0))
	items, total, err = repo().ListVouchers(tx.Context(t.Context()), garageID, filter)
	require.NoError(t, err)
	require.Zero(t, total)
	require.Empty(t, items)

	tx = dbtest.New(t).ExpectQueryRow("SELECT count(*)", dbtest.ErrRow(errBoom))
	_, _, err = repo().ListVouchers(tx.Context(t.Context()), garageID, filter)
	require.ErrorIs(t, err, errBoom)

	tx = dbtest.New(t).ExpectQueryRow("SELECT count(*)", dbtest.Row(1)).ExpectQuery("FROM payment_out_vouchers", nil, errBoom)
	_, _, err = repo().ListVouchers(tx.Context(t.Context()), garageID, filter)
	require.ErrorIs(t, err, errBoom)

	tx = dbtest.New(t).ExpectQueryRow("SELECT count(*)", dbtest.Row(1)).
		ExpectQuery("FROM payment_out_vouchers", dbtest.NewRows(voucherRow(nil)).WithScanError(errBoom), nil)
	_, _, err = repo().ListVouchers(tx.Context(t.Context()), garageID, filter)
	require.ErrorIs(t, err, errBoom)

	tx = dbtest.New(t).ExpectQueryRow("SELECT count(*)", dbtest.Row(1)).
		ExpectQuery("FROM payment_out_vouchers", dbtest.NewRows().WithErr(errBoom), nil)
	_, _, err = repo().ListVouchers(tx.Context(t.Context()), garageID, filter)
	require.ErrorIs(t, err, errBoom)

	tx = dbtest.New(t).ExpectQueryRow("SELECT count(*)", dbtest.Row(1)).
		ExpectQuery("FROM payment_out_vouchers", dbtest.NewRows(voucherRow(nil)), nil).
		ExpectQuery("FROM payment_out_allocations", nil, errBoom)
	_, _, err = repo().ListVouchers(tx.Context(t.Context()), garageID, filter)
	require.ErrorIs(t, err, errBoom)
}

func TestVouchers_VoidAndPaidOut(t *testing.T) {
	t.Parallel()

	void := func(tx *dbtest.Tx) error {
		return repo().VoidVoucher(tx.Context(t.Context()), garageID, voucherRowID, "wrong", now)
	}

	require.NoError(t, void(dbtest.New(t).ExpectExec("UPDATE payment_out_vouchers SET void_reason", dbtest.Tag(1), nil)))
	require.ErrorIs(t, void(dbtest.New(t).ExpectExec("UPDATE payment_out_vouchers SET void_reason", dbtest.Tag(0), errBoom)), errBoom)

	require.ErrorIs(t, void(dbtest.New(t).
		ExpectExec("UPDATE payment_out_vouchers SET void_reason", dbtest.Tag(0), nil).
		ExpectQueryRow("SELECT EXISTS", dbtest.Row(false))), domain.ErrVoucherNotFound)
	require.ErrorIs(t, void(dbtest.New(t).
		ExpectExec("UPDATE payment_out_vouchers SET void_reason", dbtest.Tag(0), nil).
		ExpectQueryRow("SELECT EXISTS", dbtest.Row(true))), domain.ErrPaymentOutAlreadyVoided)
	require.ErrorIs(t, void(dbtest.New(t).
		ExpectExec("UPDATE payment_out_vouchers SET void_reason", dbtest.Tag(0), nil).
		ExpectQueryRow("SELECT EXISTS", dbtest.ErrRow(errBoom))), errBoom)

	tx := dbtest.New(t).ExpectQueryRow("FROM payment_out_vouchers\n\t\tWHERE garage_id = $1 AND voided_at IS NULL", dbtest.Row(int64(250_000)))
	sum, err := repo().PaidOut(tx.Context(t.Context()), garageID, now, now.Add(time.Hour))
	require.NoError(t, err)
	require.EqualValues(t, 250_000, sum)

	tx = dbtest.New(t).ExpectQueryRow("FROM payment_out_vouchers\n\t\tWHERE garage_id = $1 AND voided_at IS NULL", dbtest.ErrRow(errBoom))
	_, err = repo().PaidOut(tx.Context(t.Context()), garageID, now, now.Add(time.Hour))
	require.ErrorIs(t, err, errBoom)
}
