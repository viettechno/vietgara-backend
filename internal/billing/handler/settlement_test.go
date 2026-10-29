package handler_test

import (
	"context"
	"testing"
	"time"

	"github.com/viettechno/vietgara-backend/pkg/billingparty"
	commonv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/common/v1"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	billingv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/billing/v1"

	"github.com/viettechno/vietgara-backend/internal/billing/domain"
	"github.com/viettechno/vietgara-backend/internal/billing/handler"
	"github.com/viettechno/vietgara-backend/internal/billing/usecase"
)

func openSettlement() *domain.Settlement {
	return &domain.Settlement{
		ID: settlementID, GarageID: garageID, RepairOrderID: repairOrderID, VehicleLicensePlate: "51F-123.45",
		CustomerName: "An Nguyen", Number: "QT-0001", TotalAmount: 1_000_000,
		Status: domain.StatusPending,
	}
}

func TestPolicies(t *testing.T) {
	t.Parallel()

	p := handler.Policies()
	assert.Len(t, p, 6)
	assert.NotNil(t, p[billingv1.SettlementService_CreateSettlement_FullMethodName].Garage)
}

func TestListSettlements(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.settlements.EXPECT().
		List(anyArg, garageID, usecase.ListFilter{Limit: 20}).
		Return([]*domain.Settlement{openSettlement()}, 1, nil).Once()

	resp, err := e.srv.ListSettlements(signedIn(t), &billingv1.ListSettlementsRequest{GarageId: garageID})
	require.NoError(t, err)
	assert.Len(t, resp.GetData(), 1)
	assert.Equal(t, int32(1), resp.GetPagination().GetTotal())
}

func TestCreateSettlement(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.settlements.EXPECT().FindByRepairOrderID(anyArg, garageID, repairOrderID).Return(nil, domain.ErrSettlementNotFound).Once()
	e.repairOrders.EXPECT().Get(anyArg, garageID, repairOrderID).Return(usecase.RepairOrderInfo{
		CustomerName: "An Nguyen", VehicleLicensePlate: "51F-123.45", TotalAmount: 1_000_000, Closed: true,
	}, nil).Once()
	e.garages.EXPECT().Get(anyArg, garageID).Return(defaultGarage(), nil).Once()
	e.limits.EXPECT().ConsumeTransaction(anyArg, ownerID).Return(nil).Once()
	e.sequences.EXPECT().Next(anyArg, garageID, "settlement", "QT-{SEQ:4}").Return(1, nil).Once()
	e.settlements.EXPECT().Create(anyArg, anyArg).Return(nil).Once()
	e.events.EXPECT().Publish(anyArg, usecase.EventSettlementCreated, garageID, "", anyArg).Return(nil).Once()

	resp, err := e.srv.CreateSettlement(signedIn(t), &billingv1.CreateSettlementRequest{
		GarageId: garageID, RepairOrderId: repairOrderID,
	})
	require.NoError(t, err)
	assert.Equal(t, "QT-0001", resp.GetSettlement().GetNumber())
	assert.Equal(t, "An Nguyen", resp.GetSettlement().GetCustomerName())
}

func TestGetSettlement(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.settlements.EXPECT().FindByID(anyArg, garageID, settlementID).Return(openSettlement(), nil).Once()

	resp, err := e.srv.GetSettlement(signedIn(t), &billingv1.GetSettlementRequest{
		GarageId: garageID, SettlementId: settlementID,
	})
	require.NoError(t, err)
	assert.Equal(t, settlementID, resp.GetSettlement().GetId())

	e.settlements.EXPECT().FindByID(anyArg, garageID, "missing").Return(nil, domain.ErrSettlementNotFound).Once()

	_, err = e.srv.GetSettlement(signedIn(t), &billingv1.GetSettlementRequest{GarageId: garageID, SettlementId: "missing"})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestRecordSettlementPayment(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.settlements.EXPECT().FindByID(anyArg, garageID, settlementID).Return(openSettlement(), nil).Once()
	e.garages.EXPECT().Get(anyArg, garageID).Return(defaultGarage(), nil).Once()
	e.sequences.EXPECT().Next(anyArg, garageID, "payment_voucher", "PT-{SEQ:4}").Return(5, nil).Once()
	e.collectors.EXPECT().FullName(anyArg, actorID).Return("Le Thu Ngan", nil).Once()

	var issued *domain.Payment

	updated := openSettlement()
	updated.PaidAmount = 1_000_000
	e.settlements.EXPECT().RecordPayment(anyArg, garageID, settlementID, anyArg).
		Run(func(_ context.Context, _, _ string, p *domain.Payment) { issued = p }).Return(updated, nil).Once()
	e.settlements.EXPECT().SaveStatus(anyArg, garageID, settlementID, domain.StatusPaid).Return(nil).Once()
	e.expectPaymentEvents(true)

	final := openSettlement()
	final.PaidAmount, final.Status = 1_000_000, domain.StatusPaid
	final.Payments = []domain.Payment{{
		ID: "p-1", Amount: 1_000_000, Method: domain.MethodCash, VoucherNumber: "PT-0005", PayerName: "Cong ty A",
		PayerAddress: "5 Le Loi", Reason: "Thanh toán", CollectorName: "Le Thu Ngan",
	}}
	e.settlements.EXPECT().FindByID(anyArg, garageID, settlementID).Return(final, nil).Once()

	resp, err := e.srv.RecordSettlementPayment(signedIn(t), &billingv1.RecordSettlementPaymentRequest{
		GarageId: garageID, SettlementId: settlementID,
		Amount: 1_000_000, Method: billingv1.PaymentMethod_PAYMENT_METHOD_CASH,
		PayerName: "Cong ty A", PayerAddress: "5 Le Loi", Reason: "Thanh toán",
	})
	require.NoError(t, err)
	assert.Equal(t, billingv1.SettlementStatus_SETTLEMENT_STATUS_PAID, resp.GetSettlement().GetStatus())

	require.NotNil(t, issued)
	assert.Equal(t, "PT-0005", issued.VoucherNumber)
	assert.Equal(t, "Cong ty A", issued.PayerName)

	payment := resp.GetSettlement().GetPayments()[0]
	assert.Equal(t, "PT-0005", payment.GetVoucherNumber())
	assert.Equal(t, "Cong ty A", payment.GetPayerName())
	assert.Equal(t, "5 Le Loi", payment.GetPayerAddress())
	assert.Equal(t, "Thanh toán", payment.GetReason())
	assert.Equal(t, "Le Thu Ngan", payment.GetCollectorName())
	assert.False(t, payment.GetVoided())
}

func TestVoidSettlementPayment(t *testing.T) {
	t.Parallel()

	e := newEnv(t)

	paid := openSettlement()
	paid.PaidAmount, paid.Status = 400_000, domain.StatusPartiallyPaid
	paid.Payments = []domain.Payment{{ID: "p-1", Amount: 400_000, Method: domain.MethodCash, VoucherNumber: "PT-0001"}}
	e.settlements.EXPECT().FindByID(anyArg, garageID, settlementID).Return(paid, nil).Once()

	after := openSettlement()
	e.settlements.EXPECT().VoidPayment(anyArg, garageID, settlementID, "p-1", "typo").Return(after, nil).Once()
	e.settlements.EXPECT().SaveStatus(anyArg, garageID, settlementID, domain.StatusPending).Return(nil).Once()

	voidedAt := time.Now()
	final := openSettlement()
	final.Payments = []domain.Payment{{
		ID: "p-1", Amount: 400_000, Method: domain.MethodCash, VoucherNumber: "PT-0001", VoidReason: "typo", VoidedAt: &voidedAt,
	}}
	e.settlements.EXPECT().FindByID(anyArg, garageID, settlementID).Return(final, nil).Once()

	resp, err := e.srv.VoidSettlementPayment(signedIn(t), &billingv1.VoidSettlementPaymentRequest{
		GarageId: garageID, SettlementId: settlementID, PaymentId: "p-1", Reason: "typo",
	})
	require.NoError(t, err)
	assert.Equal(t, billingv1.SettlementStatus_SETTLEMENT_STATUS_PENDING, resp.GetSettlement().GetStatus())

	payment := resp.GetSettlement().GetPayments()[0]
	assert.True(t, payment.GetVoided())
	assert.Equal(t, "typo", payment.GetVoidReason())
	assert.NotNil(t, payment.GetVoidedAt())

	_, err = e.srv.VoidSettlementPayment(signedIn(t), &billingv1.VoidSettlementPaymentRequest{
		GarageId: garageID, SettlementId: settlementID, PaymentId: "p-1",
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestUpdateSettlement(t *testing.T) {
	t.Parallel()

	party := billingparty.Party{TaxProfileID: "tp-1", Kind: "company", LegalName: "Cong ty A", TaxID: "0301234567", Address: "12 Le Loi"}

	e := newEnv(t)
	e.parties.ExpectedCalls = nil
	e.parties.EXPECT().Resolve(anyArg, garageID, "vehicle-1", new("tp-1")).Return(party, nil).Once()
	e.settlements.EXPECT().FindByID(anyArg, garageID, settlementID).Return(openSettlement(), nil).Once()
	e.repairOrders.EXPECT().Get(anyArg, garageID, repairOrderID).Return(usecase.RepairOrderInfo{VehicleID: "vehicle-1"}, nil).Once()
	e.settlements.EXPECT().SaveBilling(anyArg, anyArg).Return(nil).Once()

	resp, err := e.srv.UpdateSettlement(signedIn(t), &billingv1.UpdateSettlementRequest{
		GarageId: garageID, SettlementId: settlementID,
		Settlement: &billingv1.Settlement{
			BillingParty: &commonv1.BillingParty{TaxProfileId: "tp-1"}, InvoiceRequested: true,
		},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"billing_party", "invoice_requested"}},
	})
	require.NoError(t, err)
	assert.Equal(t, "Cong ty A", resp.GetSettlement().GetBillingParty().GetLegalName())
	assert.Equal(t, commonv1.TaxProfileKind_TAX_PROFILE_KIND_COMPANY, resp.GetSettlement().GetBillingParty().GetKind())
	assert.True(t, resp.GetSettlement().GetInvoiceRequested())

	_, err = e.srv.UpdateSettlement(signedIn(t), &billingv1.UpdateSettlementRequest{
		GarageId: garageID, SettlementId: settlementID, Settlement: &billingv1.Settlement{},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"total_amount"}},
	})
	assert.Equal(t, codes.InvalidArgument, status.Code(err))

	// An invoice for a retail receipt is refused with its own reason.
	e = newEnv(t)
	e.settlements.EXPECT().FindByID(anyArg, garageID, settlementID).Return(openSettlement(), nil).Once()

	_, err = e.srv.UpdateSettlement(signedIn(t), &billingv1.UpdateSettlementRequest{
		GarageId: garageID, SettlementId: settlementID,
		Settlement: &billingv1.Settlement{InvoiceRequested: true},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"invoice_requested"}},
	})
	require.Error(t, err)
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
}
