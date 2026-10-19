package usecase_test

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/mocks"
	"github.com/viettechno/vietgara-backend/internal/repairorder/domain"
	"github.com/viettechno/vietgara-backend/internal/repairorder/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
	"github.com/viettechno/vietgara-backend/pkg/issuer"
)

const (
	garageID      = "11111111-1111-1111-1111-111111111111"
	vehicleID     = "22222222-2222-2222-2222-222222222222"
	repairOrderID = "33333333-3333-3333-3333-333333333333"
	quoteID       = "44444444-4444-4444-4444-444444444444"
	actorID       = "55555555-5555-5555-5555-555555555555"
	ownerID       = "66666666-6666-6666-6666-666666666666"
)

var (
	errBoom = errors.New("boom")
	now     = time.Date(2026, time.September, 30, 10, 0, 0, 0, time.UTC)
)

// deps are the mocked dependencies of one RepairOrderUseCase under test.
type deps struct {
	RepairOrders *mocks.MockRepairOrderRepository
	Vehicles     *mocks.MockRepairOrderVehicleReader
	Garages      *mocks.MockRepairOrderGarageReader
	Limits       *mocks.MockRepairOrderTransactionLimiter
	Sequences    *mocks.MockRepairOrderSequencer
	Events       *mocks.MockRepairOrderEventPublisher
	Parts        *mocks.MockPartQuantityResolver
	Issues       *mocks.MockRepairOrderStockIssues
}

func newUseCase(t *testing.T) (*usecase.RepairOrderUseCase, *deps) {
	t.Helper()

	d := &deps{
		RepairOrders: mocks.NewMockRepairOrderRepository(t), Vehicles: mocks.NewMockRepairOrderVehicleReader(t),
		Garages: mocks.NewMockRepairOrderGarageReader(t), Limits: mocks.NewMockRepairOrderTransactionLimiter(t),
		Sequences: mocks.NewMockRepairOrderSequencer(t), Events: mocks.NewMockRepairOrderEventPublisher(t),
		Parts: mocks.NewMockPartQuantityResolver(t), Issues: mocks.NewMockRepairOrderStockIssues(t),
	}

	uc := usecase.NewRepairOrderUseCase(usecase.Deps{
		RepairOrders: d.RepairOrders, Vehicles: d.Vehicles, Garages: d.Garages, Limits: d.Limits, Sequences: d.Sequences,
		Events: d.Events, Parts: d.Parts, StockIssues: d.Issues, Now: func() time.Time { return now },
	})

	return uc, d
}

func defaultGarage() usecase.GarageInfo {
	return usecase.GarageInfo{
		OwnerUserID: ownerID, Timezone: "Asia/Ho_Chi_Minh", RepairOrderNumberTemplate: "LSC-{YYYY}{MM}-{SEQ:4}",
		Issuer: issuer.Issuer{Name: "Cong ty TNHH An Phat", TaxCode: "0301234567"},
	}
}

func oneLine() []usecase.LineInput {
	return []usecase.LineInput{{ItemType: domain.ItemTypeLabor, Description: "Oil change", Quantity: "1", UnitPrice: 250_000}}
}

func openRepairOrder() *domain.RepairOrder {
	return &domain.RepairOrder{
		ID: repairOrderID, GarageID: garageID, VehicleID: vehicleID, VehicleLicensePlate: "51F-123.45",
		CustomerName: "An Nguyen", Number: "LSC-202609-0001", Status: domain.StatusOpen,
		Lines: []domain.Line{
			{
				ID: "line-1", Position: 1, ItemType: domain.ItemTypeLabor, Description: "Oil change", Quantity: "1",
				UnitPrice: 250_000, LineTotal: 250_000, Status: domain.LineNotStarted,
			},
		},
		TotalAmount: 250_000, CreatedAt: now, UpdatedAt: now,
	}
}

const (
	partID  = "77777777-7777-7777-7777-777777777777"
	unitID  = "88888888-8888-8888-8888-888888888888"
	jobID   = "99999999-9999-9999-9999-999999999999"
	partRef = "part-line"
)

// repairOrderWithPart is an open order with a labor line and a part line.
func repairOrderWithPart() *domain.RepairOrder {
	ro := openRepairOrder()
	ref := partID
	ro.Lines = append(ro.Lines, domain.Line{
		ID: partRef, Position: 2, ItemType: domain.ItemTypePart, ItemRefID: &ref, Description: "Oil", Quantity: "4",
		UnitPrice: 100_000, LineTotal: 400_000, Status: domain.LineNotStarted,
	})
	ro.TotalAmount = 650_000

	return ro
}

func assertAppError(t *testing.T, err error, code apperror.Code) {
	t.Helper()

	require.Error(t, err)

	appErr, ok := apperror.As(err)
	require.True(t, ok, "expected an *apperror.AppError, got %T: %v", err, err)
	require.Equal(t, code, appErr.Code)
}
