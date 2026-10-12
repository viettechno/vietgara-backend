package handler_test

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	billingv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/billing/v1"

	"github.com/viettechno/vietgara-backend/internal/billing/domain"
	"github.com/viettechno/vietgara-backend/internal/billing/usecase"
)

func TestListSettlementsFilters(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		status  billingv1.SettlementStatus
		outOnly bool
		filter  usecase.ListFilter
	}{
		{
			name:   "pending",
			status: billingv1.SettlementStatus_SETTLEMENT_STATUS_PENDING,
			filter: usecase.ListFilter{Status: domain.StatusPending, Limit: 20},
		},
		{
			name:   "partially paid",
			status: billingv1.SettlementStatus_SETTLEMENT_STATUS_PARTIALLY_PAID,
			filter: usecase.ListFilter{Status: domain.StatusPartiallyPaid, Limit: 20},
		},
		{
			name:   "paid",
			status: billingv1.SettlementStatus_SETTLEMENT_STATUS_PAID,
			filter: usecase.ListFilter{Status: domain.StatusPaid, Limit: 20},
		},
		{
			name:    "outstanding only",
			outOnly: true,
			filter:  usecase.ListFilter{OutstandingOnly: true, Limit: 20},
		},
		{
			name:   "unknown status lists everything",
			status: billingv1.SettlementStatus(99),
			filter: usecase.ListFilter{Limit: 20},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			e := newEnv(t)
			e.settlements.EXPECT().List(anyArg, garageID, c.filter).Return([]*domain.Settlement{openSettlement()}, 1, nil).Once()

			resp, err := e.srv.ListSettlements(signedIn(t), &billingv1.ListSettlementsRequest{
				GarageId: garageID, Status: c.status, OutstandingOnly: c.outOnly,
			})
			require.NoError(t, err)
			assert.Len(t, resp.GetData(), 1)
		})
	}
}

func TestListSettlementsFailure(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.settlements.EXPECT().List(anyArg, garageID, usecase.ListFilter{Limit: 20}).Return(nil, 0, domain.ErrSettlementNotFound).Once()

	_, err := e.srv.ListSettlements(signedIn(t), &billingv1.ListSettlementsRequest{GarageId: garageID})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestListSettlementsPaginationClamps(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.settlements.EXPECT().List(anyArg, garageID, usecase.ListFilter{Limit: 20}).Return(nil, int(math.MaxInt32)+1, nil).Once()

	resp, err := e.srv.ListSettlements(signedIn(t), &billingv1.ListSettlementsRequest{GarageId: garageID})
	require.NoError(t, err)
	assert.Equal(t, int32(math.MaxInt32), resp.GetPagination().GetTotal())

	e = newEnv(t)
	e.settlements.EXPECT().List(anyArg, garageID, usecase.ListFilter{Limit: 20}).Return(nil, int(math.MinInt32)-1, nil).Once()

	resp, err = e.srv.ListSettlements(signedIn(t), &billingv1.ListSettlementsRequest{GarageId: garageID})
	require.NoError(t, err)
	assert.Equal(t, int32(math.MinInt32), resp.GetPagination().GetTotal())
}

func TestCreateSettlementAlreadySettled(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.settlements.EXPECT().FindByRepairOrderID(anyArg, garageID, repairOrderID).Return(openSettlement(), nil).Once()

	_, err := e.srv.CreateSettlement(signedIn(t), &billingv1.CreateSettlementRequest{
		GarageId: garageID, RepairOrderId: repairOrderID,
	})
	require.Error(t, err)
	assert.Equal(t, codes.AlreadyExists, status.Code(err))
}

func TestRecordSettlementPaymentMethods(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		method billingv1.PaymentMethod
		want   billingv1.PaymentMethod
	}{
		{"bank transfer", billingv1.PaymentMethod_PAYMENT_METHOD_BANK_TRANSFER, billingv1.PaymentMethod_PAYMENT_METHOD_BANK_TRANSFER},
		{"online", billingv1.PaymentMethod_PAYMENT_METHOD_ONLINE, billingv1.PaymentMethod_PAYMENT_METHOD_ONLINE},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			e := newEnv(t)
			e.settlements.EXPECT().FindByID(anyArg, garageID, settlementID).Return(openSettlement(), nil).Once()
			e.garages.EXPECT().Get(anyArg, garageID).Return(defaultGarage(), nil).Once()
			e.sequences.EXPECT().Next(anyArg, garageID, "payment_voucher", "PT-{SEQ:4}").Return(1, nil).Once()
			e.collectors.EXPECT().FullName(anyArg, actorID).Return("Le Thu Ngan", nil).Once()

			updated := openSettlement()
			updated.PaidAmount = 1_000_000
			e.settlements.EXPECT().RecordPayment(anyArg, garageID, settlementID, anyArg).Return(updated, nil).Once()
			e.settlements.EXPECT().SaveStatus(anyArg, garageID, settlementID, domain.StatusPaid).Return(nil).Once()

			final := openSettlement()
			final.PaidAmount, final.Status = 1_000_000, domain.StatusPaid
			final.Payments = []domain.Payment{{ID: "p-1", Amount: 1_000_000, Method: domainMethod(c.method), VoucherNumber: "PT-0001"}}
			e.settlements.EXPECT().FindByID(anyArg, garageID, settlementID).Return(final, nil).Once()

			resp, err := e.srv.RecordSettlementPayment(signedIn(t), &billingv1.RecordSettlementPaymentRequest{
				GarageId: garageID, SettlementId: settlementID, Amount: 1_000_000, Method: c.method,
			})
			require.NoError(t, err)
			assert.Equal(t, c.want, resp.GetSettlement().GetPayments()[0].GetMethod())
		})
	}
}

func domainMethod(m billingv1.PaymentMethod) domain.PaymentMethod {
	if m == billingv1.PaymentMethod_PAYMENT_METHOD_BANK_TRANSFER {
		return domain.MethodBankTransfer
	}

	return domain.MethodOnline
}

func TestRecordSettlementPaymentInvalidMethod(t *testing.T) {
	t.Parallel()

	e := newEnv(t)

	_, err := e.srv.RecordSettlementPayment(signedIn(t), &billingv1.RecordSettlementPaymentRequest{
		GarageId: garageID, SettlementId: settlementID, Amount: 1_000_000,
		Method: billingv1.PaymentMethod_PAYMENT_METHOD_UNSPECIFIED,
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestRecordSettlementPaymentMissing(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.settlements.EXPECT().FindByID(anyArg, garageID, settlementID).Return(nil, domain.ErrSettlementNotFound).Once()

	_, err := e.srv.RecordSettlementPayment(signedIn(t), &billingv1.RecordSettlementPaymentRequest{
		GarageId: garageID, SettlementId: settlementID, Amount: 1_000_000, Method: billingv1.PaymentMethod_PAYMENT_METHOD_CASH,
	})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestUpdateSettlementWithoutMask(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.settlements.EXPECT().FindByID(anyArg, garageID, settlementID).Return(openSettlement(), nil).Once()
	e.repairOrders.EXPECT().Get(anyArg, garageID, repairOrderID).Return(usecase.RepairOrderInfo{VehicleID: "vehicle-1"}, nil).Once()
	e.settlements.EXPECT().SaveBilling(anyArg, anyArg).Return(nil).Once()

	resp, err := e.srv.UpdateSettlement(signedIn(t), &billingv1.UpdateSettlementRequest{
		GarageId: garageID, SettlementId: settlementID, Settlement: &billingv1.Settlement{},
	})
	require.NoError(t, err)
	assert.False(t, resp.GetSettlement().GetInvoiceRequested())
}

func TestGetSettlementUnknownStatus(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	unknown := openSettlement()
	unknown.Status = "archived"
	e.settlements.EXPECT().FindByID(anyArg, garageID, settlementID).Return(unknown, nil).Once()

	resp, err := e.srv.GetSettlement(signedIn(t), &billingv1.GetSettlementRequest{GarageId: garageID, SettlementId: settlementID})
	require.NoError(t, err)
	assert.Equal(t, billingv1.SettlementStatus_SETTLEMENT_STATUS_UNSPECIFIED, resp.GetSettlement().GetStatus())
}

func TestGetSettlementPartiallyPaid(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	partial := openSettlement()
	partial.PaidAmount, partial.Status = 400_000, domain.StatusPartiallyPaid
	partial.Payments = []domain.Payment{{ID: "p-1", Amount: 400_000, VoucherNumber: "PT-0001"}}
	e.settlements.EXPECT().FindByID(anyArg, garageID, settlementID).Return(partial, nil).Once()

	resp, err := e.srv.GetSettlement(signedIn(t), &billingv1.GetSettlementRequest{GarageId: garageID, SettlementId: settlementID})
	require.NoError(t, err)
	assert.Equal(t, billingv1.SettlementStatus_SETTLEMENT_STATUS_PARTIALLY_PAID, resp.GetSettlement().GetStatus())
	payment := resp.GetSettlement().GetPayments()[0]
	assert.Equal(t, billingv1.PaymentMethod_PAYMENT_METHOD_UNSPECIFIED, payment.GetMethod())
}
