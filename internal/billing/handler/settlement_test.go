package handler_test

import (
	"context"
	"testing"

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
	assert.Len(t, p, 4)
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
