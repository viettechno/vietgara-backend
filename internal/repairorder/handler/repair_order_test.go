package handler_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	repairorderv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/repairorder/v1"

	"github.com/viettechno/vietgara-backend/internal/repairorder/domain"
	"github.com/viettechno/vietgara-backend/internal/repairorder/handler"
	"github.com/viettechno/vietgara-backend/internal/repairorder/usecase"
)

func openRepairOrder() *domain.RepairOrder {
	return &domain.RepairOrder{
		ID: repairOrderID, GarageID: garageID, VehicleID: vehicleID, VehicleLicensePlate: "51F-123.45",
		CustomerName: "An Nguyen", Number: "LSC-0001", Status: domain.StatusOpen,
		Lines: []domain.Line{
			{
				ID: "line-1", Position: 1, ItemType: domain.ItemTypeLabor, Description: "Oil change", Quantity: "1",
				UnitPrice: 250_000, LineTotal: 250_000, Status: domain.LineNotStarted,
			},
		},
		TotalAmount: 250_000,
	}
}

func TestPolicies(t *testing.T) {
	t.Parallel()

	p := handler.Policies()
	assert.Len(t, p, 5)
	assert.NotNil(t, p[repairorderv1.RepairOrderService_CreateRepairOrder_FullMethodName].Garage)
}

func TestListRepairOrders(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.repairOrders.EXPECT().
		List(anyArg, garageID, usecase.ListFilter{Limit: 20}).
		Return([]*domain.RepairOrder{openRepairOrder()}, 1, nil).Once()

	resp, err := e.srv.ListRepairOrders(signedIn(t), &repairorderv1.ListRepairOrdersRequest{GarageId: garageID})
	require.NoError(t, err)
	assert.Len(t, resp.GetData(), 1)
	assert.Equal(t, int32(1), resp.GetPagination().GetTotal())
}

func TestCreateRepairOrder(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.garages.EXPECT().Get(anyArg, garageID).Return(defaultGarage(), nil).Once()
	e.vehicles.EXPECT().Find(anyArg, garageID, vehicleID).Return("An Nguyen", "51F-123.45", nil).Once()
	e.limits.EXPECT().ConsumeTransaction(anyArg, ownerID).Return(nil).Once()
	e.sequences.EXPECT().Next(anyArg, garageID, "repair_order", "LSC-{SEQ:4}").Return(1, nil).Once()
	e.repairOrders.EXPECT().Create(anyArg, anyArg).Return(nil).Once()

	resp, err := e.srv.CreateRepairOrder(signedIn(t), &repairorderv1.CreateRepairOrderRequest{
		GarageId: garageID,
		RepairOrder: &repairorderv1.RepairOrder{
			VehicleId: vehicleID,
			Lines: []*repairorderv1.RepairOrderLine{{
				ItemType: repairorderv1.RepairOrderItemType_REPAIR_ORDER_ITEM_TYPE_LABOR, Description: "Oil change",
				Quantity: "1", UnitPrice: 250_000,
			}},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "LSC-0001", resp.GetRepairOrder().GetNumber())
	assert.Equal(t, "An Nguyen", resp.GetRepairOrder().GetCustomerName())
}

func TestGetRepairOrder(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.repairOrders.EXPECT().FindByID(anyArg, garageID, repairOrderID).Return(openRepairOrder(), nil).Once()

	resp, err := e.srv.GetRepairOrder(signedIn(t), &repairorderv1.GetRepairOrderRequest{
		GarageId: garageID, RepairOrderId: repairOrderID,
	})
	require.NoError(t, err)
	assert.Equal(t, repairOrderID, resp.GetRepairOrder().GetId())

	e.repairOrders.EXPECT().FindByID(anyArg, garageID, "missing").Return(nil, domain.ErrRepairOrderNotFound).Once()

	_, err = e.srv.GetRepairOrder(signedIn(t), &repairorderv1.GetRepairOrderRequest{GarageId: garageID, RepairOrderId: "missing"})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestUpdateRepairOrderLine(t *testing.T) {
	t.Parallel()

	t.Run("updates status and technicians", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)
		e.repairOrders.EXPECT().FindByID(anyArg, garageID, repairOrderID).Return(openRepairOrder(), nil).Once()
		e.repairOrders.EXPECT().Save(anyArg, anyArg).Return(nil).Once()

		resp, err := e.srv.UpdateRepairOrderLine(signedIn(t), &repairorderv1.UpdateRepairOrderLineRequest{
			GarageId: garageID, RepairOrderId: repairOrderID, LineId: "line-1",
			Line: &repairorderv1.RepairOrderLine{
				Status:                repairorderv1.RepairOrderLineStatus_REPAIR_ORDER_LINE_STATUS_IN_PROGRESS,
				AssignedTechnicianIds: []string{actorID},
			},
		})
		require.NoError(t, err)
		assert.Equal(t, repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_IN_PROGRESS, resp.GetRepairOrder().GetStatus())
	})

	t.Run("rejects an unknown mask path", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)

		_, err := e.srv.UpdateRepairOrderLine(signedIn(t), &repairorderv1.UpdateRepairOrderLineRequest{
			GarageId: garageID, RepairOrderId: repairOrderID, LineId: "line-1",
			Line:       &repairorderv1.RepairOrderLine{},
			UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"description"}},
		})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})
}

func TestCloseRepairOrder(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	ro := openRepairOrder()
	ro.Lines[0].Status = domain.LineCompleted

	e.repairOrders.EXPECT().FindByID(anyArg, garageID, repairOrderID).Return(ro, nil).Once()
	e.repairOrders.EXPECT().Save(anyArg, anyArg).Return(nil).Once()
	e.events.EXPECT().Publish(anyArg, usecase.EventRepairOrderClosed, garageID, "", anyArg).Return(nil).Once()

	resp, err := e.srv.CloseRepairOrder(signedIn(t), &repairorderv1.CloseRepairOrderRequest{
		GarageId: garageID, RepairOrderId: repairOrderID,
	})
	require.NoError(t, err)
	assert.Equal(t, repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_CLOSED, resp.GetRepairOrder().GetStatus())
}
