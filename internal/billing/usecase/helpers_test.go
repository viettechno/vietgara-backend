package usecase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/billing/domain"
	"github.com/viettechno/vietgara-backend/internal/billing/usecase"
	"github.com/viettechno/vietgara-backend/internal/mocks"
	"github.com/viettechno/vietgara-backend/internal/platform/audit"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

const (
	garageID      = "11111111-1111-1111-1111-111111111111"
	repairOrderID = "22222222-2222-2222-2222-222222222222"
	settlementID  = "33333333-3333-3333-3333-333333333333"
	insurerID     = "44444444-4444-4444-4444-444444444444"
	actorID       = "55555555-5555-5555-5555-555555555555"
	ownerID       = "66666666-6666-6666-6666-666666666666"
)

var (
	errBoom = errors.New("boom")
	now     = time.Date(2026, time.September, 30, 10, 0, 0, 0, time.UTC)
)

// deps are the mocked dependencies of one SettlementUseCase under test.
type deps struct {
	Settlements  *mocks.MockBillingRepository
	RepairOrders *mocks.MockBillingRepairOrderReader
	Partners     *mocks.MockBillingPartnerReader
	Garages      *mocks.MockBillingGarageReader
	Limits       *mocks.MockBillingTransactionLimiter
	Sequences    *mocks.MockBillingSequencer
	Events       *mocks.MockBillingEventPublisher
	Audit        *mocks.MockBillingAuditLogger
}

func newUseCase(t *testing.T) (*usecase.SettlementUseCase, *deps) {
	t.Helper()

	d := &deps{
		Settlements: mocks.NewMockBillingRepository(t), RepairOrders: mocks.NewMockBillingRepairOrderReader(t),
		Partners: mocks.NewMockBillingPartnerReader(t), Garages: mocks.NewMockBillingGarageReader(t),
		Limits: mocks.NewMockBillingTransactionLimiter(t), Sequences: mocks.NewMockBillingSequencer(t),
		Events: mocks.NewMockBillingEventPublisher(t), Audit: mocks.NewMockBillingAuditLogger(t),
	}

	uc := usecase.NewSettlementUseCase(usecase.Deps{
		Settlements: d.Settlements, RepairOrders: d.RepairOrders, Partners: d.Partners, Garages: d.Garages,
		Limits: d.Limits, Sequences: d.Sequences, Events: d.Events, Audit: d.Audit, Now: func() time.Time { return now },
	})

	return uc, d
}

// expectAudit expects one audit entry and returns where it is captured.
func expectAudit(d *deps) *audit.Entry {
	var got audit.Entry

	d.Audit.EXPECT().Record(mock.Anything, mock.Anything).
		Run(func(_ context.Context, e audit.Entry) { got = e }).Return(nil).Once()

	return &got
}

func insurerIDPtr() *string {
	id := insurerID

	return &id
}

func defaultGarage() usecase.GarageInfo {
	return usecase.GarageInfo{
		OwnerUserID: ownerID, Timezone: "Asia/Ho_Chi_Minh", SettlementNumberTemplate: "QT-{YYYY}{MM}-{SEQ:4}",
	}
}

func closedRepairOrder() usecase.RepairOrderInfo {
	return usecase.RepairOrderInfo{
		CustomerName: "An Nguyen", VehicleLicensePlate: "51F-123.45", TotalAmount: 1_000_000, Closed: true,
	}
}

func openSettlement() *domain.Settlement {
	return &domain.Settlement{
		ID: settlementID, GarageID: garageID, RepairOrderID: repairOrderID, VehicleLicensePlate: "51F-123.45",
		CustomerName: "An Nguyen", Number: "QT-202609-0001", TotalAmount: 1_000_000, CustomerAmount: 1_000_000,
		Status: domain.StatusPending, CreatedAt: now, UpdatedAt: now,
	}
}

func assertAppError(t *testing.T, err error, code apperror.Code) {
	t.Helper()

	require.Error(t, err)

	appErr, ok := apperror.As(err)
	require.True(t, ok, "expected an *apperror.AppError, got %T: %v", err, err)
	require.Equal(t, code, appErr.Code)
}
