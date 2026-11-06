package receivable_test

import (
	"context"
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	billingv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/billing/v1"
	inventoryv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/inventory/v1"
	receivablev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/receivable/v1"
	supplierv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/supplier/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/client"
	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

// payables is a garage that buys oil and wipers from two suppliers.
type payables struct {
	*fixture.Business

	oil, wiper      string // part ids
	acme, bolt      *supplierv1.Supplier
	supplierAddress string
}

func newPayables(t *testing.T, label string) *payables {
	t.Helper()

	b := fixture.NewBusiness(t, label)
	ctx := t.Context()
	unit := fixture.NewUnit(t, b.Owner.API, b.GarageID, "Litre", "L")
	suffix := strconv.FormatInt(time.Now().UnixNano()%1_000_000, 10)

	p := &payables{Business: b, supplierAddress: "12 Le Loi, Quan 1"}

	var err error

	p.acme, err = b.Owner.API.CreateSupplier(ctx, b.GarageID, &supplierv1.Supplier{
		Name: "Acme " + suffix, Contact: &supplierv1.SupplierContact{Address: &p.supplierAddress},
	})
	require.NoError(t, err)

	p.bolt, err = b.Owner.API.CreateSupplier(ctx, b.GarageID, &supplierv1.Supplier{Name: "Bolt " + suffix})
	require.NoError(t, err)

	p.oil = fixture.NewPart(t, b.Owner.API, b.GarageID, "OIL-"+suffix, "Engine oil", unit).GetId()
	p.wiper = fixture.NewPart(t, b.Owner.API, b.GarageID, "WPR-"+suffix, "Wiper", unit).GetId()

	return p
}

// receipt completes a goods receipt dated daysAgo with one line per entry:
// the part, the supplier, the quantity and the cost per unit.
type receiptLine struct {
	part     string
	supplier *supplierv1.Supplier
	quantity string
	cost     int64
}

func (p *payables) receipt(t *testing.T, daysAgo int, lines ...receiptLine) *inventoryv1.GoodsReceipt {
	t.Helper()

	ctx := t.Context()

	var out []*inventoryv1.GoodsReceiptLine

	for _, l := range lines {
		line := &inventoryv1.GoodsReceiptLine{PartId: l.part, EnteredQuantity: l.quantity, SupplierId: l.supplier.GetId()}
		if l.cost > 0 {
			line.UnitCost = new(l.cost)
		}

		out = append(out, line)
	}

	draft, err := p.Owner.API.CreateGoodsReceipt(ctx, p.GarageID, &inventoryv1.GoodsReceipt{
		ReceiptDate: time.Now().AddDate(0, 0, -daysAgo).Format(time.DateOnly), Lines: out,
	})
	require.NoError(t, err)

	done, err := p.Owner.API.CompleteGoodsReceipt(ctx, p.GarageID, draft.GetId())
	require.NoError(t, err)

	return done
}

// payablesOf waits for the payables of a receipt (the event goes through the
// outbox) and returns them by supplier.
func (p *payables) payablesOf(t *testing.T, api *client.Client, receipt *inventoryv1.GoodsReceipt, count int) map[string]*receivablev1.SupplierPayable {
	t.Helper()

	var found map[string]*receivablev1.SupplierPayable

	eventually(t, arrives, "payables of "+receipt.GetNumber(), func(ctx context.Context) bool {
		list, err := api.ListSupplierPayables(ctx, p.GarageID, "", true)
		if err != nil {
			return false
		}

		found = map[string]*receivablev1.SupplierPayable{}

		for _, x := range list.GetData() {
			if x.GetGoodsReceiptId() == receipt.GetId() {
				found[x.GetSupplierId()] = x
			}
		}

		return len(found) == count
	})

	return found
}

func (p *payables) voucher(t *testing.T, supplier *supplierv1.Supplier, amount int64, method billingv1.PaymentMethod, allocations ...*receivablev1.PaymentOutAllocation) (*receivablev1.PaymentOutVoucher, error) {
	t.Helper()

	return p.Owner.API.WithIdempotencyKey(uuid.NewString()).CreatePaymentOutVoucher(t.Context(), p.GarageID, &receivablev1.PaymentOutVoucher{
		SupplierId: supplier.GetId(), Amount: amount, Method: method, Allocations: allocations,
	})
}

func allocation(payable *receivablev1.SupplierPayable, amount int64) *receivablev1.PaymentOutAllocation {
	return &receivablev1.PaymentOutAllocation{PayableId: payable.GetId(), Amount: amount}
}

func (p *payables) payable(t *testing.T, api *client.Client, id string) *receivablev1.SupplierPayable {
	t.Helper()

	list, err := api.ListSupplierPayables(t.Context(), p.GarageID, "", true)
	require.NoError(t, err)

	for _, x := range list.GetData() {
		if x.GetId() == id {
			return x
		}
	}

	return nil
}

// Scenario: E2E-PAY-001, Release 13.0, a completed goods receipt makes a payable per
// supplier of its lines; cancelling it cancels them (FR-INV-17).
func TestReceiptMakesPayablesAndCancelCancelsThem(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	p := newPayables(t, "pay-001")
	api := p.Owner.API

	receipt := p.receipt(t, 0,
		receiptLine{p.oil, p.acme, "10", 100_000}, receiptLine{p.wiper, p.bolt, "2", 200_000})
	found := p.payablesOf(t, api, receipt, 2)

	oil, wiper := found[p.acme.GetId()], found[p.bolt.GetId()]
	require.NotNil(t, oil)
	require.NotNil(t, wiper)
	assert.EqualValues(t, 1_000_000, oil.GetTotalAmount())
	assert.EqualValues(t, 400_000, wiper.GetTotalAmount())
	assert.Equal(t, receivablev1.PayableStatus_PAYABLE_STATUS_OPEN, oil.GetStatus())
	assert.Zero(t, oil.GetPaidAmount())
	assert.EqualValues(t, 1_000_000, oil.GetRemainingAmount())
	assert.Equal(t, receipt.GetNumber(), oil.GetGoodsReceiptNumber())
	assert.Equal(t, time.Now().Format(time.DateOnly), oil.GetReceiptDate())

	// A receipt whose lines record no cost makes no payable.
	free := p.receipt(t, 0, receiptLine{p.oil, p.acme, "1", 0})

	// Cancelling the receipt cancels its payables.
	_, err := api.CancelGoodsReceipt(ctx, p.GarageID, receipt.GetId())
	require.NoError(t, err)

	eventually(t, arrives, "the payables are cancelled", func(ctx context.Context) bool {
		list, err := api.ListSupplierPayables(ctx, p.GarageID, "", true)

		return err == nil && len(list.GetData()) == 0
	})

	aging, err := api.GetSupplierPayablesAging(ctx, p.GarageID)
	require.NoError(t, err)
	assert.Empty(t, aging.GetSuppliers())
	assert.Zero(t, aging.GetTotals().GetTotal())

	// A later receipt makes payables for itself only.
	next := p.receipt(t, 0, receiptLine{p.oil, p.acme, "1", 50_000})
	later := p.payablesOf(t, api, next, 1)
	assert.EqualValues(t, 50_000, later[p.acme.GetId()].GetTotalAmount())

	// Nothing was ever made for the receipt without costs.
	time.Sleep(absent)

	list, err := api.ListSupplierPayables(ctx, p.GarageID, p.acme.GetId(), true)
	require.NoError(t, err)
	assert.Len(t, list.GetData(), 1)
	assert.NotEqual(t, free.GetId(), list.GetData()[0].GetGoodsReceiptId())
}

// Scenario: E2E-PAY-002, Release 13.0, a payment-out voucher is numbered and
// settles payables (FR-BIL-07).
func TestVoucherIsNumberedAndSettlesPayables(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	p := newPayables(t, "pay-002")
	api := p.Owner.API
	year := strconv.Itoa(time.Now().Year())

	first := p.payablesOf(t, api, p.receipt(t, 3, receiptLine{p.oil, p.acme, "10", 100_000}), 1)[p.acme.GetId()]
	second := p.payablesOf(t, api, p.receipt(t, 1, receiptLine{p.wiper, p.acme, "3", 200_000}), 1)[p.acme.GetId()]

	v1, err := p.voucher(t, p.acme, 1_200_000, billingv1.PaymentMethod_PAYMENT_METHOD_BANK_TRANSFER, allocation(first, 1_000_000), allocation(second, 200_000))
	require.NoError(t, err)
	assert.Equal(t, "PC-"+year+"-00001", v1.GetNumber())
	assert.Equal(t, p.acme.GetName(), v1.GetPayeeName(), "the payee defaults to the supplier")
	assert.Equal(t, p.supplierAddress, v1.GetPayeeAddress())
	assert.NotEmpty(t, v1.GetReason())
	assert.NotEmpty(t, v1.GetIssuerName())
	assert.Zero(t, v1.GetUnallocatedAmount())
	require.Len(t, v1.GetAllocations(), 2)

	assert.Equal(t, receivablev1.PayableStatus_PAYABLE_STATUS_PAID, p.payable(t, api, first.GetId()).GetStatus())
	assert.EqualValues(t, 200_000, p.payable(t, api, second.GetId()).GetPaidAmount())

	v2, err := p.voucher(t, p.acme, 400_000, billingv1.PaymentMethod_PAYMENT_METHOD_CASH, allocation(second, 400_000))
	require.NoError(t, err)
	assert.Equal(t, "PC-"+year+"-00002", v2.GetNumber())
	assert.Equal(t, receivablev1.PayableStatus_PAYABLE_STATUS_PAID, p.payable(t, api, second.GetId()).GetStatus())

	// A voucher with no allocation is an advance.
	advance, err := p.voucher(t, p.bolt, 150_000, billingv1.PaymentMethod_PAYMENT_METHOD_CASH)
	require.NoError(t, err)
	assert.EqualValues(t, 150_000, advance.GetUnallocatedAmount())

	got, err := api.GetPaymentOutVoucher(ctx, p.GarageID, v1.GetId())
	require.NoError(t, err)
	assert.Equal(t, v1.GetNumber(), got.GetNumber())
	assert.Len(t, got.GetAllocations(), 2)

	list, err := api.ListPaymentOutVouchers(ctx, p.GarageID, "", false)
	require.NoError(t, err)
	require.Len(t, list.GetData(), 3)
	assert.Equal(t, advance.GetId(), list.GetData()[0].GetId(), "newest first")

	byBolt, err := api.ListPaymentOutVouchers(ctx, p.GarageID, p.bolt.GetId(), false)
	require.NoError(t, err)
	assert.Len(t, byBolt.GetData(), 1)

	// The paid payables leave the default list.
	open, err := api.ListSupplierPayables(ctx, p.GarageID, "", false)
	require.NoError(t, err)
	assert.Empty(t, open.GetData())
	assert.Zero(t, open.GetSummary().GetOutstandingAmount())
}

// Scenario: E2E-PAY-003, Release 13.0, wrong allocations are refused and two
// vouchers cannot take the same remainder (FR-BIL-07).
func TestWrongAllocationsAreRefusedAndRemaindersAreNotShared(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	p := newPayables(t, "pay-003")
	api := p.Owner.API

	big := p.payablesOf(t, api, p.receipt(t, 2, receiptLine{p.oil, p.acme, "10", 100_000}), 1)[p.acme.GetId()]
	other := p.payablesOf(t, api, p.receipt(t, 2, receiptLine{p.wiper, p.bolt, "2", 200_000}), 1)[p.bolt.GetId()]

	paidReceipt := p.receipt(t, 2, receiptLine{p.oil, p.acme, "1", 30_000})
	paid := p.payablesOf(t, api, paidReceipt, 1)[p.acme.GetId()]
	_, err := p.voucher(t, p.acme, 30_000, billingv1.PaymentMethod_PAYMENT_METHOD_CASH, allocation(paid, 30_000))
	require.NoError(t, err)

	cancelledReceipt := p.receipt(t, 2, receiptLine{p.oil, p.acme, "1", 20_000})
	cancelled := p.payablesOf(t, api, cancelledReceipt, 1)[p.acme.GetId()]
	_, err = api.CancelGoodsReceipt(ctx, p.GarageID, cancelledReceipt.GetId())
	require.NoError(t, err)

	eventually(t, arrives, "the payable is cancelled", func(ctx context.Context) bool {
		list, err := api.ListSupplierPayables(ctx, p.GarageID, p.acme.GetId(), true)

		return err == nil && len(list.GetData()) == 2
	})

	before, err := api.ListPaymentOutVouchers(ctx, p.GarageID, "", true)
	require.NoError(t, err)

	method := billingv1.PaymentMethod_PAYMENT_METHOD_CASH

	cases := map[string]struct {
		supplier *supplierv1.Supplier
		amount   int64
		method   billingv1.PaymentMethod
		allocs   []*receivablev1.PaymentOutAllocation
		status   int
		reason   string
	}{
		"another supplier's payable": {p.acme, 100, method, []*receivablev1.PaymentOutAllocation{allocation(other, 100)}, http.StatusUnprocessableEntity, "INVALID_ALLOCATION"},
		"more than remaining":        {p.acme, 2_000_000, method, []*receivablev1.PaymentOutAllocation{allocation(big, 1_000_001)}, http.StatusUnprocessableEntity, "INVALID_ALLOCATION"},
		"the same payable twice":     {p.acme, 2_000_000, method, []*receivablev1.PaymentOutAllocation{allocation(big, 100), allocation(big, 100)}, http.StatusUnprocessableEntity, "INVALID_ALLOCATION"},
		"a paid payable":             {p.acme, 100, method, []*receivablev1.PaymentOutAllocation{allocation(paid, 10)}, http.StatusUnprocessableEntity, "INVALID_ALLOCATION"},
		"a cancelled payable":        {p.acme, 100, method, []*receivablev1.PaymentOutAllocation{allocation(cancelled, 10)}, http.StatusUnprocessableEntity, "INVALID_ALLOCATION"},
		"less than allocated":        {p.acme, 100_000, method, []*receivablev1.PaymentOutAllocation{allocation(big, 200_000)}, http.StatusUnprocessableEntity, "INVALID_PAYMENT_OUT_AMOUNT"},
		"zero amount":                {p.acme, 0, method, nil, http.StatusUnprocessableEntity, "INVALID_PAYMENT_OUT_AMOUNT"},
		"negative amount":            {p.acme, -5, method, nil, http.StatusUnprocessableEntity, "INVALID_PAYMENT_OUT_AMOUNT"},
		"no method":                  {p.acme, 100, billingv1.PaymentMethod_PAYMENT_METHOD_UNSPECIFIED, nil, http.StatusUnprocessableEntity, "VALIDATION_ERROR"},
		"unknown supplier":           {&supplierv1.Supplier{Id: uuid.NewString()}, 100, method, nil, http.StatusNotFound, "SUPPLIER_NOT_FOUND"},
	}

	for _, tc := range cases {
		_, err := p.voucher(t, tc.supplier, tc.amount, tc.method, tc.allocs...)
		fixture.RequireAPIError(t, err, tc.status, tc.reason)
	}

	// Nothing was created and no number was used.
	after, err := api.ListPaymentOutVouchers(ctx, p.GarageID, "", true)
	require.NoError(t, err)
	assert.Len(t, after.GetData(), len(before.GetData()))
	assert.EqualValues(t, 1_000_000, p.payable(t, api, big.GetId()).GetRemainingAmount())

	// Two vouchers at once cannot both take 700,000 of 1,000,000.
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		wins    int
		refused []error
	)

	for range 2 {
		wg.Go(func() {
			_, err := p.voucher(t, p.acme, 700_000, method, allocation(big, 700_000))

			mu.Lock()
			defer mu.Unlock()

			if err == nil {
				wins++
			} else {
				refused = append(refused, err)
			}
		})
	}

	wg.Wait()
	assert.Equal(t, 1, wins)
	require.Len(t, refused, 1)
	fixture.RequireAPIError(t, refused[0], http.StatusUnprocessableEntity, "INVALID_ALLOCATION")
	assert.EqualValues(t, 700_000, p.payable(t, api, big.GetId()).GetPaidAmount())
}

// Scenario: E2E-PAY-004, Release 13.0, voiding a voucher reopens its payables
// and a voucher is never deleted (FR-BIL-07).
func TestVoidingAVoucherReopensItsPayables(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	p := newPayables(t, "pay-004")
	api := p.Owner.API

	receipt := p.receipt(t, 2, receiptLine{p.oil, p.acme, "10", 100_000})
	payable := p.payablesOf(t, api, receipt, 1)[p.acme.GetId()]

	v, err := p.voucher(t, p.acme, 1_000_000, billingv1.PaymentMethod_PAYMENT_METHOD_CASH, allocation(payable, 1_000_000))
	require.NoError(t, err)
	require.Equal(t, receivablev1.PayableStatus_PAYABLE_STATUS_PAID, p.payable(t, api, payable.GetId()).GetStatus())

	// Reasons that are missing or too long are refused.
	_, err = api.VoidPaymentOutVoucher(ctx, p.GarageID, v.GetId(), "")
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "VALIDATION_ERROR")

	long := make([]byte, 501)
	for i := range long {
		long[i] = 'x'
	}

	_, err = api.VoidPaymentOutVoucher(ctx, p.GarageID, v.GetId(), string(long))
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "VALIDATION_ERROR")

	voided, err := api.VoidPaymentOutVoucher(ctx, p.GarageID, v.GetId(), "paid the wrong supplier")
	require.NoError(t, err)
	assert.True(t, voided.GetVoided())
	assert.Equal(t, "paid the wrong supplier", voided.GetVoidReason())
	assert.NotNil(t, voided.GetVoidedAt())
	assert.Len(t, voided.GetAllocations(), 1, "its allocations are shown as before")

	reopened := p.payable(t, api, payable.GetId())
	assert.Equal(t, receivablev1.PayableStatus_PAYABLE_STATUS_OPEN, reopened.GetStatus())
	assert.Zero(t, reopened.GetPaidAmount())

	_, err = api.VoidPaymentOutVoucher(ctx, p.GarageID, v.GetId(), "again")
	fixture.RequireAPIError(t, err, http.StatusConflict, "PAYMENT_OUT_ALREADY_VOIDED")

	// A voided voucher stays on file, and is listed on request only.
	plain, err := api.ListPaymentOutVouchers(ctx, p.GarageID, "", false)
	require.NoError(t, err)
	assert.Empty(t, plain.GetData())

	all, err := api.ListPaymentOutVouchers(ctx, p.GarageID, "", true)
	require.NoError(t, err)
	assert.Len(t, all.GetData(), 1)

	// A voucher is never deleted.
	_, _, err = api.DownloadRaw(ctx, http.MethodDelete, "/api/v1/garages/"+p.GarageID+"/payment-out-vouchers/"+v.GetId())
	require.Error(t, err)

	// Voiding after the receipt was cancelled leaves the payable cancelled.
	second := p.receipt(t, 1, receiptLine{p.oil, p.acme, "5", 100_000})
	target := p.payablesOf(t, api, second, 1)[p.acme.GetId()]

	v2, err := p.voucher(t, p.acme, 500_000, billingv1.PaymentMethod_PAYMENT_METHOD_CASH, allocation(target, 500_000))
	require.NoError(t, err)

	_, err = api.CancelGoodsReceipt(ctx, p.GarageID, second.GetId())
	require.NoError(t, err)

	eventually(t, arrives, "the payable is cancelled", func(ctx context.Context) bool {
		list, err := api.ListSupplierPayables(ctx, p.GarageID, "", true)
		if err != nil {
			return false
		}

		for _, x := range list.GetData() {
			if x.GetId() == target.GetId() {
				return false
			}
		}

		return true
	})

	voided, err = api.VoidPaymentOutVoucher(ctx, p.GarageID, v2.GetId(), "cancelled receipt")
	require.NoError(t, err)
	assert.EqualValues(t, 500_000, voided.GetUnallocatedAmount(), "the allocation to a cancelled payable counts as unallocated")
}

// Scenario: E2E-PAY-005, Release 13.0, the payables by supplier and age
// (FR-INV-17).
func TestAgingBySupplierAndAge(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	p := newPayables(t, "pay-005")
	api := p.Owner.API

	byAge := map[int]*receivablev1.SupplierPayable{}

	for _, days := range []int{10, 45, 75, 120} {
		receipt := p.receipt(t, days, receiptLine{p.oil, p.acme, "1", 100_000})
		byAge[days] = p.payablesOf(t, api, receipt, 1)[p.acme.GetId()]
	}

	p.payablesOf(t, api, p.receipt(t, 20, receiptLine{p.wiper, p.bolt, "1", 50_000}), 1)

	_, err := p.voucher(t, p.acme, 100_000, billingv1.PaymentMethod_PAYMENT_METHOD_CASH, allocation(byAge[120], 100_000))
	require.NoError(t, err)

	aging, err := api.GetSupplierPayablesAging(ctx, p.GarageID)
	require.NoError(t, err)

	by := map[string]*receivablev1.SupplierAging{}
	for _, s := range aging.GetSuppliers() {
		by[s.GetSupplierId()] = s
	}

	acme := by[p.acme.GetId()]
	require.NotNil(t, acme)
	assert.EqualValues(t, 100_000, acme.GetCurrent())
	assert.EqualValues(t, 100_000, acme.GetDays_31_60())
	assert.EqualValues(t, 100_000, acme.GetDays_61_90())
	assert.Zero(t, acme.GetOver_90(), "the 120-day payable is paid")
	assert.EqualValues(t, 300_000, acme.GetTotal())

	bolt := by[p.bolt.GetId()]
	require.NotNil(t, bolt)
	assert.EqualValues(t, 50_000, bolt.GetCurrent())
	assert.EqualValues(t, 50_000, bolt.GetTotal())
	assert.EqualValues(t, 350_000, aging.GetTotals().GetTotal())

	list, err := api.ListSupplierPayables(ctx, p.GarageID, "", false)
	require.NoError(t, err)
	require.Len(t, list.GetData(), 4)
	assert.EqualValues(t, 75, list.GetData()[0].GetAgeDays(), "oldest first")
	assert.EqualValues(t, 350_000, list.GetSummary().GetOutstandingAmount())
	assert.EqualValues(t, 4, list.GetSummary().GetOpenCount())
}

// Scenario: E2E-PAY-007, Release 13.0, permissions and tenant isolation of
// payables and vouchers (FR-INV-17, FR-BIL-07, NFR-SE).
func TestPayablePermissionsAndTenantIsolation(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	p := newPayables(t, "pay-007")

	accountant := p.AddStaff(t, "Accountant")
	warehouse := p.AddStaff(t, "Inventory Staff")
	advisor := p.AddStaff(t, "Service Advisor")

	payable := p.payablesOf(t, p.Owner.API, p.receipt(t, 1, receiptLine{p.oil, p.acme, "10", 100_000}), 1)[p.acme.GetId()]

	// The accountant does all of it.
	_, err := accountant.API.ListSupplierPayables(ctx, p.GarageID, "", false)
	require.NoError(t, err)

	_, err = accountant.API.GetSupplierPayablesAging(ctx, p.GarageID)
	require.NoError(t, err)

	v, err := accountant.API.WithIdempotencyKey(uuid.NewString()).CreatePaymentOutVoucher(ctx, p.GarageID, &receivablev1.PaymentOutVoucher{
		SupplierId: p.acme.GetId(), Amount: 100_000, Method: billingv1.PaymentMethod_PAYMENT_METHOD_CASH,
		Allocations: []*receivablev1.PaymentOutAllocation{allocation(payable, 100_000)},
	})
	require.NoError(t, err)

	_, err = accountant.API.GetPaymentOutVoucher(ctx, p.GarageID, v.GetId())
	require.NoError(t, err)

	_, err = accountant.API.ListPaymentOutVouchers(ctx, p.GarageID, "", false)
	require.NoError(t, err)

	_, err = accountant.API.VoidPaymentOutVoucher(ctx, p.GarageID, v.GetId(), "test")
	require.NoError(t, err)

	// Without billing access every call is refused.
	for _, who := range []*fixture.Actor{warehouse, advisor} {
		_, err = who.API.ListSupplierPayables(ctx, p.GarageID, "", false)
		fixture.RequireAPIError(t, err, http.StatusForbidden, "PERMISSION_DENIED")

		_, err = who.API.GetSupplierPayablesAging(ctx, p.GarageID)
		fixture.RequireAPIError(t, err, http.StatusForbidden, "PERMISSION_DENIED")

		_, err = who.API.ListPaymentOutVouchers(ctx, p.GarageID, "", false)
		fixture.RequireAPIError(t, err, http.StatusForbidden, "PERMISSION_DENIED")

		_, err = who.API.GetPaymentOutVoucher(ctx, p.GarageID, v.GetId())
		fixture.RequireAPIError(t, err, http.StatusForbidden, "PERMISSION_DENIED")

		_, err = who.API.WithIdempotencyKey(uuid.NewString()).CreatePaymentOutVoucher(ctx, p.GarageID,
			&receivablev1.PaymentOutVoucher{SupplierId: p.acme.GetId(), Amount: 1, Method: billingv1.PaymentMethod_PAYMENT_METHOD_CASH})
		fixture.RequireAPIError(t, err, http.StatusForbidden, "PERMISSION_DENIED")

		_, err = who.API.VoidPaymentOutVoucher(ctx, p.GarageID, v.GetId(), "x")
		fixture.RequireAPIError(t, err, http.StatusForbidden, "PERMISSION_DENIED")
	}

	// Another tenant sees nothing of this garage.
	other := fixture.NewBusiness(t, "pay-007-other")

	_, err = other.Owner.API.GetPaymentOutVoucher(ctx, other.GarageID, v.GetId())
	fixture.RequireAPIError(t, err, http.StatusNotFound, "PAYMENT_OUT_VOUCHER_NOT_FOUND")

	_, err = other.Owner.API.GetPaymentOutVoucher(ctx, p.GarageID, v.GetId())
	fixture.RequireAPIError(t, err, http.StatusForbidden, "GARAGE_ACCESS_DENIED")

	// A payable of another garage cannot be allocated in one's own voucher.
	foreign, err := other.Owner.API.CreateSupplier(ctx, other.GarageID, &supplierv1.Supplier{Name: "Foreign " + strconv.FormatInt(time.Now().UnixNano()%1_000_000, 10)})
	require.NoError(t, err)

	_, err = other.Owner.API.WithIdempotencyKey(uuid.NewString()).CreatePaymentOutVoucher(ctx, other.GarageID, &receivablev1.PaymentOutVoucher{
		SupplierId: foreign.GetId(), Amount: 100, Method: billingv1.PaymentMethod_PAYMENT_METHOD_CASH,
		Allocations: []*receivablev1.PaymentOutAllocation{allocation(payable, 100)},
	})
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "INVALID_ALLOCATION")

	theirs, err := other.Owner.API.ListSupplierPayables(ctx, other.GarageID, "", true)
	require.NoError(t, err)
	assert.Empty(t, theirs.GetData())

	// Without a token nothing is served.
	_, err = fixture.Anonymous(t).ListSupplierPayables(ctx, p.GarageID, "", false)
	fixture.RequireAPIError(t, err, http.StatusUnauthorized, "")
}
