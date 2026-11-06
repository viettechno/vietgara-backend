package handler_test

import (
	"testing"
	"time"

	billingv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/billing/v1"
	receivablev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/receivable/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/viettechno/vietgara-backend/internal/mocks"
	"github.com/viettechno/vietgara-backend/internal/platform/audit"
	"github.com/viettechno/vietgara-backend/internal/platform/outbox"
	"github.com/viettechno/vietgara-backend/internal/receivable/domain"
	"github.com/viettechno/vietgara-backend/internal/receivable/handler"
	"github.com/viettechno/vietgara-backend/internal/receivable/usecase"
	"github.com/viettechno/vietgara-backend/pkg/issuer"
)

const (
	supplierID = "77777777-7777-7777-7777-777777777777"
	payableID  = "99999999-9999-9999-9999-999999999999"
	voucherID  = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
)

type payableEnv struct {
	srv       *handler.PayableServer
	repo      *mocks.MockReceivablePayableRepository
	suppliers *mocks.MockReceivableSupplierReader
	sequences *mocks.MockReceivableSequencer
	audit     *mocks.MockReceivableAuditLogger
}

func newPayableEnv(t *testing.T) *payableEnv {
	t.Helper()

	e := &payableEnv{
		repo: mocks.NewMockReceivablePayableRepository(t), suppliers: mocks.NewMockReceivableSupplierReader(t),
		sequences: mocks.NewMockReceivableSequencer(t), audit: mocks.NewMockReceivableAuditLogger(t),
	}
	garages := mocks.NewMockReceivableGarageReader(t)
	garages.EXPECT().Timezone(mock.Anything, garageID).Return("Asia/Ho_Chi_Minh", nil).Maybe()

	issuers := mocks.NewMockReceivableIssuerReader(t)
	issuers.EXPECT().Issuer(mock.Anything, garageID).Return(issuer.Issuer{Name: "Gara"}, nil).Maybe()

	staff := mocks.NewMockReceivableStaffReader(t)
	staff.EXPECT().FullName(mock.Anything, actorID).Return("Binh", nil).Maybe()

	e.srv = handler.NewPayableServer(usecase.NewPayableUseCase(usecase.PayableDeps{
		Repo: e.repo, Suppliers: e.suppliers, Garages: garages, Issuers: issuers, Staff: staff, Sequences: e.sequences, Audit: e.audit,
		Now: func() time.Time { return now },
	}))

	return e
}

func payable() *domain.Payable {
	return &domain.Payable{
		ID: payableID, GarageID: garageID, GoodsReceiptID: "gr-1", GoodsReceiptNumber: "NK-0001", SupplierID: supplierID,
		SupplierName: "Sao Mai", ReceiptDate: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), TotalAmount: 700_000, PaidAmount: 200_000,
		Status: domain.PayableOpen, CreatedAt: now,
	}
}

func storedVoucher() *domain.PaymentOutVoucher {
	voided := now

	return &domain.PaymentOutVoucher{
		ID: voucherID, GarageID: garageID, Number: "PC-2026-00001", SupplierID: supplierID, SupplierName: "Sao Mai", PayeeName: "Sao Mai",
		Amount: 1_000_000, Method: "bank_transfer", Reason: "Thanh toán", IssuerName: "Binh", VoidReason: "sai", VoidedAt: &voided, CreatedAt: now,
		Allocations: []domain.Allocation{{PayableID: payableID, GoodsReceiptNumber: "NK-0001", Amount: 700_000}},
	}
}

func TestPayablePolicies(t *testing.T) {
	t.Parallel()

	p := handler.PayablePolicies()
	assert.Len(t, p, 6)
	assert.NotNil(t, p[receivablev1.PayableService_CreatePaymentOutVoucher_FullMethodName].Garage)
}

func TestSubscribePayables(t *testing.T) {
	t.Parallel()

	b := bus{}
	handler.SubscribePayables(b, outbox.NewProcessor(nil, nil), usecase.NewPayableUseCase(usecase.PayableDeps{}))

	assert.Len(t, b, 2)
	assert.Contains(t, b, "goods_receipt.completed")
	assert.Contains(t, b, "goods_receipt.cancelled")
}

func TestListSupplierPayables(t *testing.T) {
	t.Parallel()

	e := newPayableEnv(t)
	e.repo.EXPECT().ListPayables(mock.Anything, garageID, usecase.PayableFilter{SupplierID: supplierID, IncludePaid: true, Limit: 10, Offset: 10}).
		Return([]*domain.Payable{payable()}, 11, usecase.PayableSummary{OpenCount: 4, OutstandingAmount: 900}, nil).Once()

	resp, err := e.srv.ListSupplierPayables(signedIn(t), &receivablev1.ListSupplierPayablesRequest{
		GarageId: garageID, SupplierId: supplierID, IncludePaid: true, Page: 2, PageSize: 10,
	})
	require.NoError(t, err)
	require.Len(t, resp.GetData(), 1)
	assert.EqualValues(t, 11, resp.GetPagination().GetTotal())
	assert.EqualValues(t, 900, resp.GetSummary().GetOutstandingAmount())

	got := resp.GetData()[0]
	assert.Equal(t, "2026-10-01", got.GetReceiptDate())
	assert.EqualValues(t, 500_000, got.GetRemainingAmount())
	assert.EqualValues(t, 50, got.GetAgeDays())
	assert.Equal(t, receivablev1.PayableStatus_PAYABLE_STATUS_OPEN, got.GetStatus())

	e.repo.EXPECT().ListPayables(mock.Anything, garageID, mock.Anything).Return(nil, 0, usecase.PayableSummary{}, errBoom).Once()
	_, err = e.srv.ListSupplierPayables(signedIn(t), &receivablev1.ListSupplierPayablesRequest{GarageId: garageID})
	require.Equal(t, codes.Internal, status.Code(err))
}

func TestPayableStatusMapping(t *testing.T) {
	t.Parallel()

	e := newPayableEnv(t)
	paid, cancelled, odd := payable(), payable(), payable()
	paid.Status, cancelled.Status, odd.Status = domain.PayablePaid, domain.PayableCancelled, domain.PayableStatus("?")
	e.repo.EXPECT().ListPayables(mock.Anything, garageID, mock.Anything).
		Return([]*domain.Payable{paid, cancelled, odd}, 3, usecase.PayableSummary{}, nil).Once()

	resp, err := e.srv.ListSupplierPayables(signedIn(t), &receivablev1.ListSupplierPayablesRequest{GarageId: garageID})
	require.NoError(t, err)
	assert.Equal(t, receivablev1.PayableStatus_PAYABLE_STATUS_PAID, resp.GetData()[0].GetStatus())
	assert.Equal(t, receivablev1.PayableStatus_PAYABLE_STATUS_CANCELLED, resp.GetData()[1].GetStatus())
	assert.Equal(t, receivablev1.PayableStatus_PAYABLE_STATUS_UNSPECIFIED, resp.GetData()[2].GetStatus())
}

func TestGetSupplierPayablesAging(t *testing.T) {
	t.Parallel()

	e := newPayableEnv(t)
	e.repo.EXPECT().Aging(mock.Anything, garageID, mock.Anything).Return([]domain.Aging{
		{SupplierID: supplierID, SupplierName: "Sao Mai", Total: 10, Current: 1, Days31to60: 2, Days61to90: 3, Over90: 4},
	}, nil).Once()

	resp, err := e.srv.GetSupplierPayablesAging(signedIn(t), &receivablev1.GetSupplierPayablesAgingRequest{GarageId: garageID})
	require.NoError(t, err)
	require.Len(t, resp.GetSuppliers(), 1)
	assert.EqualValues(t, 2, resp.GetSuppliers()[0].GetDays_31_60())
	assert.EqualValues(t, 4, resp.GetTotals().GetOver_90())

	e.repo.EXPECT().Aging(mock.Anything, garageID, mock.Anything).Return(nil, errBoom).Once()
	_, err = e.srv.GetSupplierPayablesAging(signedIn(t), &receivablev1.GetSupplierPayablesAgingRequest{GarageId: garageID})
	require.Equal(t, codes.Internal, status.Code(err))
}

func TestPaymentOutVoucherReads(t *testing.T) {
	t.Parallel()

	e := newPayableEnv(t)
	e.repo.EXPECT().ListVouchers(mock.Anything, garageID, usecase.VoucherFilter{SupplierID: supplierID, IncludeVoided: true, Limit: 20, Offset: 0}).
		Return([]*domain.PaymentOutVoucher{storedVoucher()}, 1, nil).Once()

	list, err := e.srv.ListPaymentOutVouchers(signedIn(t), &receivablev1.ListPaymentOutVouchersRequest{
		GarageId: garageID, SupplierId: supplierID, IncludeVoided: true,
	})
	require.NoError(t, err)
	require.Len(t, list.GetData(), 1)

	got := list.GetData()[0]
	assert.True(t, got.GetVoided())
	assert.NotNil(t, got.GetVoidedAt())
	assert.Equal(t, billingv1.PaymentMethod_PAYMENT_METHOD_BANK_TRANSFER, got.GetMethod())
	assert.EqualValues(t, 300_000, got.GetUnallocatedAmount())
	require.Len(t, got.GetAllocations(), 1)
	assert.Equal(t, "NK-0001", got.GetAllocations()[0].GetGoodsReceiptNumber())

	e.repo.EXPECT().ListVouchers(mock.Anything, garageID, mock.Anything).Return(nil, 0, errBoom).Once()
	_, err = e.srv.ListPaymentOutVouchers(signedIn(t), &receivablev1.ListPaymentOutVouchersRequest{GarageId: garageID})
	require.Equal(t, codes.Internal, status.Code(err))

	e.repo.EXPECT().FindVoucher(mock.Anything, garageID, voucherID).Return(storedVoucher(), nil).Once()

	one, err := e.srv.GetPaymentOutVoucher(signedIn(t), &receivablev1.GetPaymentOutVoucherRequest{GarageId: garageID, VoucherId: voucherID})
	require.NoError(t, err)
	assert.Equal(t, "PC-2026-00001", one.GetNumber())

	e.repo.EXPECT().FindVoucher(mock.Anything, garageID, voucherID).Return(nil, domain.ErrVoucherNotFound).Once()
	_, err = e.srv.GetPaymentOutVoucher(signedIn(t), &receivablev1.GetPaymentOutVoucherRequest{GarageId: garageID, VoucherId: voucherID})
	require.Equal(t, codes.NotFound, status.Code(err))
}

func TestCreatePaymentOutVoucher(t *testing.T) {
	t.Parallel()

	e := newPayableEnv(t)
	e.suppliers.EXPECT().Get(mock.Anything, garageID, supplierID).Return(usecase.SupplierView{Name: "Sao Mai"}, nil).Maybe()
	e.repo.EXPECT().LockPayables(mock.Anything, garageID, []string{payableID}).Return([]*domain.Payable{payable()}, nil).Once()
	e.sequences.EXPECT().Next(mock.Anything, garageID, "payment_out", "PC-2026-{SEQ:5}").Return(1, nil).Once()
	e.repo.EXPECT().AddVoucher(mock.Anything, mock.Anything).Return(nil).Once()
	e.repo.EXPECT().SavePayable(mock.Anything, mock.Anything).Return(nil).Once()
	e.audit.EXPECT().Record(mock.Anything, mock.MatchedBy(func(en audit.Entry) bool { return en.Action == usecase.ActionVoucherIssued })).Return(nil).Once()

	voucher := &receivablev1.PaymentOutVoucher{
		SupplierId: supplierID, Amount: 500_000, Method: billingv1.PaymentMethod_PAYMENT_METHOD_CASH,
		Allocations: []*receivablev1.PaymentOutAllocation{{PayableId: payableID, Amount: 500_000}},
	}

	resp, err := e.srv.CreatePaymentOutVoucher(signedIn(t), &receivablev1.CreatePaymentOutVoucherRequest{GarageId: garageID, Voucher: voucher})
	require.NoError(t, err)
	assert.Equal(t, "PC-2026-00001", resp.GetNumber())
	assert.Equal(t, "Binh", resp.GetIssuerName())
	assert.Equal(t, billingv1.PaymentMethod_PAYMENT_METHOD_CASH, resp.GetMethod())

	voucher.Method = billingv1.PaymentMethod_PAYMENT_METHOD_UNSPECIFIED
	_, err = e.srv.CreatePaymentOutVoucher(signedIn(t), &receivablev1.CreatePaymentOutVoucherRequest{GarageId: garageID, Voucher: voucher})
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	_, err = e.srv.CreatePaymentOutVoucher(signedIn(t), &receivablev1.CreatePaymentOutVoucherRequest{GarageId: garageID})
	require.Equal(t, codes.NotFound, status.Code(err), "no voucher means no supplier")
}

func TestVoidPaymentOutVoucher(t *testing.T) {
	t.Parallel()

	e := newPayableEnv(t)
	e.repo.EXPECT().VoidVoucher(mock.Anything, garageID, voucherID, "sai nha cung cap", now).Return(nil).Once()
	e.repo.EXPECT().FindVoucher(mock.Anything, garageID, voucherID).Return(storedVoucher(), nil).Once()
	e.repo.EXPECT().LockPayables(mock.Anything, garageID, []string{payableID}).Return([]*domain.Payable{payable()}, nil).Once()
	e.repo.EXPECT().SavePayable(mock.Anything, mock.Anything).Return(nil).Once()
	e.audit.EXPECT().Record(mock.Anything, mock.Anything).Return(nil).Once()

	resp, err := e.srv.VoidPaymentOutVoucher(signedIn(t), &receivablev1.VoidPaymentOutVoucherRequest{
		GarageId: garageID, VoucherId: voucherID, Reason: "sai nha cung cap",
	})
	require.NoError(t, err)
	assert.True(t, resp.GetVoided())

	_, err = e.srv.VoidPaymentOutVoucher(signedIn(t), &receivablev1.VoidPaymentOutVoucherRequest{GarageId: garageID, VoucherId: voucherID})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}
