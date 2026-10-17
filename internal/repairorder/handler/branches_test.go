package handler_test

import (
	"math"
	"testing"
	"time"

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

func TestRepairOrderToProtoVariants(t *testing.T) {
	t.Parallel()

	quoteID := "quote-1"
	closedAt := time.Now()

	converted := openRepairOrder()
	converted.QuoteID = &quoteID
	converted.Status = domain.StatusClosed

	byQuote := handler.RepairOrderToProto(converted)
	assert.Equal(t, quoteID, byQuote.GetQuoteId())
	assert.Equal(t, repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_CLOSED, byQuote.GetStatus())

	withClosedAt := openRepairOrder()
	withClosedAt.ClosedAt = &closedAt
	assert.NotNil(t, handler.RepairOrderToProto(withClosedAt).GetClosedAt())

	part := openRepairOrder()
	part.Lines[0].ItemType = domain.ItemTypePart
	part.Lines[0].Status = domain.LineCompleted
	lines := handler.RepairOrderToProto(part).GetLines()
	require.Len(t, lines, 1)
	assert.Equal(t, repairorderv1.RepairOrderItemType_REPAIR_ORDER_ITEM_TYPE_PART, lines[0].GetItemType())
	assert.Equal(t, repairorderv1.RepairOrderLineStatus_REPAIR_ORDER_LINE_STATUS_COMPLETED, lines[0].GetStatus())

	unknown := openRepairOrder()
	unknown.Status = "archived"
	assert.Equal(t, repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_UNSPECIFIED, handler.RepairOrderToProto(unknown).GetStatus())

	unknownLine := openRepairOrder()
	unknownLine.Lines[0].Status = "archived"
	assert.Equal(t,
		repairorderv1.RepairOrderLineStatus_REPAIR_ORDER_LINE_STATUS_UNSPECIFIED,
		handler.RepairOrderToProto(unknownLine).GetLines()[0].GetStatus())
}

func TestListRepairOrdersFilters(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		status     repairorderv1.RepairOrderStatus
		technician string
		filter     usecase.ListFilter
	}{
		{
			name:   "completed",
			status: repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_COMPLETED,
			filter: usecase.ListFilter{Status: domain.StatusCompleted, Limit: 20},
		},
		{
			name:   "waiting on parts",
			status: repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_WAITING_ON_PARTS,
			filter: usecase.ListFilter{Status: domain.StatusWaitingOnParts, Limit: 20},
		},
		{
			name:   "open",
			status: repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_OPEN,
			filter: usecase.ListFilter{Status: domain.StatusOpen, Limit: 20},
		},
		{
			name:   "in progress",
			status: repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_IN_PROGRESS,
			filter: usecase.ListFilter{Status: domain.StatusInProgress, Limit: 20},
		},
		{
			name:   "closed",
			status: repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_CLOSED,
			filter: usecase.ListFilter{Status: domain.StatusClosed, Limit: 20},
		},
		{
			name:       "technician",
			technician: actorID,
			filter:     usecase.ListFilter{TechnicianID: actorID, Limit: 20},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			e := newEnv(t)
			e.repairOrders.EXPECT().List(anyArg, garageID, c.filter).Return([]*domain.RepairOrder{openRepairOrder()}, 1, nil).Once()

			resp, err := e.srv.ListRepairOrders(signedIn(t), &repairorderv1.ListRepairOrdersRequest{
				GarageId: garageID, Status: c.status, TechnicianId: c.technician,
			})
			require.NoError(t, err)
			assert.Len(t, resp.GetData(), 1)
		})
	}
}

func TestListRepairOrdersFailure(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.repairOrders.EXPECT().List(anyArg, garageID, usecase.ListFilter{Limit: 20}).Return(nil, 0, domain.ErrRepairOrderNotFound).Once()

	_, err := e.srv.ListRepairOrders(signedIn(t), &repairorderv1.ListRepairOrdersRequest{GarageId: garageID})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestListRepairOrdersPaginationClamps(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.repairOrders.EXPECT().List(anyArg, garageID, usecase.ListFilter{Limit: 20}).Return(nil, int(math.MaxInt32)+1, nil).Once()

	resp, err := e.srv.ListRepairOrders(signedIn(t), &repairorderv1.ListRepairOrdersRequest{GarageId: garageID})
	require.NoError(t, err)
	assert.Equal(t, int32(math.MaxInt32), resp.GetPagination().GetTotal())

	e = newEnv(t)
	e.repairOrders.EXPECT().List(anyArg, garageID, usecase.ListFilter{Limit: 20}).Return(nil, int(math.MinInt32)-1, nil).Once()

	resp, err = e.srv.ListRepairOrders(signedIn(t), &repairorderv1.ListRepairOrdersRequest{GarageId: garageID})
	require.NoError(t, err)
	assert.Equal(t, int32(math.MinInt32), resp.GetPagination().GetTotal())
}

func TestCreateRepairOrderPartLine(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.garages.EXPECT().Get(anyArg, garageID).Return(defaultGarage(), nil).Once()
	e.vehicles.EXPECT().Find(anyArg, garageID, vehicleID).Return("An Nguyen", "51F-123.45", nil).Once()
	e.limits.EXPECT().ConsumeTransaction(anyArg, ownerID).Return(nil).Once()
	e.sequences.EXPECT().Next(anyArg, garageID, "repair_order", "LSC-{SEQ:4}").Return(2, nil).Once()
	e.repairOrders.EXPECT().Create(anyArg, anyArg).Return(nil).Once()
	e.events.EXPECT().Publish(anyArg, anyArg, anyArg, anyArg, anyArg).Return(nil).Once()

	resp, err := e.srv.CreateRepairOrder(signedIn(t), &repairorderv1.CreateRepairOrderRequest{
		GarageId: garageID,
		RepairOrder: &repairorderv1.RepairOrder{
			VehicleId: vehicleID,
			Lines: []*repairorderv1.RepairOrderLine{{
				ItemType: repairorderv1.RepairOrderItemType_REPAIR_ORDER_ITEM_TYPE_PART, Description: "Brake pad",
				Quantity: "2", UnitPrice: 450_000,
			}},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "LSC-0002", resp.GetRepairOrder().GetNumber())
	assert.Equal(t, repairorderv1.RepairOrderItemType_REPAIR_ORDER_ITEM_TYPE_PART, resp.GetRepairOrder().GetLines()[0].GetItemType())
}

func TestGetRepairOrderStatuses(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		status domain.RepairOrderStatus
		want   repairorderv1.RepairOrderStatus
	}{
		{"completed", domain.StatusCompleted, repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_COMPLETED},
		{"waiting on parts", domain.StatusWaitingOnParts, repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_WAITING_ON_PARTS},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			e := newEnv(t)
			ro := openRepairOrder()
			ro.Status = c.status
			e.repairOrders.EXPECT().FindByID(anyArg, garageID, repairOrderID).Return(ro, nil).Once()

			resp, err := e.srv.GetRepairOrder(signedIn(t), &repairorderv1.GetRepairOrderRequest{
				GarageId: garageID, RepairOrderId: repairOrderID,
			})
			require.NoError(t, err)
			assert.Equal(t, c.want, resp.GetRepairOrder().GetStatus())
		})
	}
}

func TestCreateRepairOrderFailure(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.garages.EXPECT().Get(anyArg, garageID).Return(defaultGarage(), nil).Once()
	e.vehicles.EXPECT().Find(anyArg, garageID, vehicleID).Return("", "", domain.ErrRepairOrderNotFound).Once()

	_, err := e.srv.CreateRepairOrder(signedIn(t), &repairorderv1.CreateRepairOrderRequest{
		GarageId: garageID,
		RepairOrder: &repairorderv1.RepairOrder{
			VehicleId: vehicleID,
			Lines: []*repairorderv1.RepairOrderLine{{
				ItemType: repairorderv1.RepairOrderItemType_REPAIR_ORDER_ITEM_TYPE_LABOR, Description: "Oil change",
				Quantity: "1", UnitPrice: 250_000,
			}},
		},
	})
	require.Error(t, err)
}

func TestUpdateRepairOrderLineStatuses(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		status repairorderv1.RepairOrderLineStatus
		want   repairorderv1.RepairOrderStatus
	}{
		{"not started stays open", repairorderv1.RepairOrderLineStatus_REPAIR_ORDER_LINE_STATUS_NOT_STARTED, repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_OPEN},
		{"completed derives completed", repairorderv1.RepairOrderLineStatus_REPAIR_ORDER_LINE_STATUS_COMPLETED, repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_COMPLETED},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			e := newEnv(t)
			e.repairOrders.EXPECT().FindByID(anyArg, garageID, repairOrderID).Return(openRepairOrder(), nil).Once()
			e.repairOrders.EXPECT().Save(anyArg, anyArg).Return(nil).Once()
			e.events.EXPECT().Publish(anyArg, anyArg, anyArg, anyArg, anyArg).Return(nil).Maybe()

			resp, err := e.srv.UpdateRepairOrderLine(signedIn(t), &repairorderv1.UpdateRepairOrderLineRequest{
				GarageId: garageID, RepairOrderId: repairOrderID, LineId: "line-1",
				Line: &repairorderv1.RepairOrderLine{Status: c.status},
			})
			require.NoError(t, err)
			assert.Equal(t, c.want, resp.GetRepairOrder().GetStatus())
		})
	}
}

func TestUpdateRepairOrderLineMaskedStatus(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.repairOrders.EXPECT().FindByID(anyArg, garageID, repairOrderID).Return(openRepairOrder(), nil).Once()
	e.repairOrders.EXPECT().Save(anyArg, anyArg).Return(nil).Once()
	e.events.EXPECT().Publish(anyArg, anyArg, anyArg, anyArg, anyArg).Return(nil).Maybe()

	resp, err := e.srv.UpdateRepairOrderLine(signedIn(t), &repairorderv1.UpdateRepairOrderLineRequest{
		GarageId: garageID, RepairOrderId: repairOrderID, LineId: "line-1",
		Line: &repairorderv1.RepairOrderLine{
			Status: repairorderv1.RepairOrderLineStatus_REPAIR_ORDER_LINE_STATUS_IN_PROGRESS,
		},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"status"}},
	})
	require.NoError(t, err)
	assert.Equal(t, repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_IN_PROGRESS, resp.GetRepairOrder().GetStatus())
}

func TestCreateRepairOrderUnspecifiedItemType(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.garages.EXPECT().Get(anyArg, garageID).Return(defaultGarage(), nil).Once()

	_, err := e.srv.CreateRepairOrder(signedIn(t), &repairorderv1.CreateRepairOrderRequest{
		GarageId: garageID,
		RepairOrder: &repairorderv1.RepairOrder{
			VehicleId: vehicleID,
			Lines: []*repairorderv1.RepairOrderLine{{
				ItemType: repairorderv1.RepairOrderItemType_REPAIR_ORDER_ITEM_TYPE_UNSPECIFIED, Description: "Mystery part",
				Quantity: "1", UnitPrice: 10_000,
			}},
		},
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestCloseRepairOrderNotReady(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.repairOrders.EXPECT().FindByID(anyArg, garageID, repairOrderID).Return(openRepairOrder(), nil).Once()

	_, err := e.srv.CloseRepairOrder(signedIn(t), &repairorderv1.CloseRepairOrderRequest{
		GarageId: garageID, RepairOrderId: repairOrderID,
	})
	require.Error(t, err)
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
}
