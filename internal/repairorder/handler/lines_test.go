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
)

func TestAddRepairOrderLine(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.repairOrders.EXPECT().FindByID(anyArg, garageID, repairOrderID).Return(openRepairOrder(), nil).Once()
	e.repairOrders.EXPECT().ReplaceLines(anyArg, anyArg).Return(nil).Once()

	resp, err := e.srv.AddRepairOrderLine(signedIn(t), &repairorderv1.AddRepairOrderLineRequest{
		GarageId: garageID, RepairOrderId: repairOrderID,
		Line: &repairorderv1.RepairOrderLine{
			ItemType: repairorderv1.RepairOrderItemType_REPAIR_ORDER_ITEM_TYPE_LABOR, Description: "Wash", Quantity: "1", UnitPrice: 50_000,
			JobId: new("99999999-9999-9999-9999-999999999999"),
		},
	})
	require.NoError(t, err)
	require.Len(t, resp.GetRepairOrder().GetLines(), 2)
	assert.Equal(t, "99999999-9999-9999-9999-999999999999", resp.GetRepairOrder().GetLines()[1].GetJobId())

	e.repairOrders.EXPECT().FindByID(anyArg, garageID, repairOrderID).Return(openRepairOrder(), nil).Once()

	_, err = e.srv.AddRepairOrderLine(signedIn(t), &repairorderv1.AddRepairOrderLineRequest{GarageId: garageID, RepairOrderId: repairOrderID})
	require.Error(t, err) // no line given
}

func TestDeleteRepairOrderLine(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	ro := openRepairOrder()
	ro.Lines = append(ro.Lines, domain.Line{ID: "line-2", Position: 2, ItemType: domain.ItemTypeLabor, Description: "Wash", Quantity: "1"})
	e.repairOrders.EXPECT().FindByID(anyArg, garageID, repairOrderID).Return(ro, nil).Once()
	e.repairOrders.EXPECT().ReplaceLines(anyArg, anyArg).Return(nil).Once()

	resp, err := e.srv.DeleteRepairOrderLine(signedIn(t), &repairorderv1.DeleteRepairOrderLineRequest{
		GarageId: garageID, RepairOrderId: repairOrderID, LineId: "line-2",
	})
	require.NoError(t, err)
	assert.Len(t, resp.GetRepairOrder().GetLines(), 1)

	e.repairOrders.EXPECT().FindByID(anyArg, garageID, repairOrderID).Return(openRepairOrder(), nil).Once()

	_, err = e.srv.DeleteRepairOrderLine(signedIn(t), &repairorderv1.DeleteRepairOrderLineRequest{
		GarageId: garageID, RepairOrderId: repairOrderID, LineId: "line-1",
	})
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
}

func TestCancelRepairOrder(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.repairOrders.EXPECT().FindByID(anyArg, garageID, repairOrderID).Return(openRepairOrder(), nil).Once()
	e.issues.EXPECT().Posted(anyArg, garageID, repairOrderID).Return(false, nil).Once()
	e.repairOrders.EXPECT().Save(anyArg, anyArg).Return(nil).Once()
	e.events.EXPECT().Publish(anyArg, anyArg, anyArg, anyArg, anyArg).Return(nil).Once()

	resp, err := e.srv.CancelRepairOrder(signedIn(t), &repairorderv1.CancelRepairOrderRequest{
		GarageId: garageID, RepairOrderId: repairOrderID,
	})
	require.NoError(t, err)
	assert.Equal(t, repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_CANCELLED, resp.GetRepairOrder().GetStatus())

	cancelled := openRepairOrder()
	cancelled.Status = domain.StatusCancelled
	e.repairOrders.EXPECT().FindByID(anyArg, garageID, repairOrderID).Return(cancelled, nil).Once()

	_, err = e.srv.CancelRepairOrder(signedIn(t), &repairorderv1.CancelRepairOrderRequest{
		GarageId: garageID, RepairOrderId: repairOrderID,
	})
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
}

func TestUpdateRepairOrderLineContent(t *testing.T) {
	t.Parallel()

	t.Run("edits the fields the mask names", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)
		e.repairOrders.EXPECT().FindByID(anyArg, garageID, repairOrderID).Return(openRepairOrder(), nil).Once()
		e.repairOrders.EXPECT().ReplaceLines(anyArg, anyArg).Return(nil).Once()

		resp, err := e.srv.UpdateRepairOrderLine(signedIn(t), &repairorderv1.UpdateRepairOrderLineRequest{
			GarageId: garageID, RepairOrderId: repairOrderID, LineId: "line-1",
			Line:       &repairorderv1.RepairOrderLine{Description: "Oil and filter", UnitPrice: 300_000, Quantity: "2"},
			UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"description", "unit_price", "quantity"}},
		})
		require.NoError(t, err)
		assert.Equal(t, "Oil and filter", resp.GetRepairOrder().GetLines()[0].GetDescription())
		assert.Equal(t, int64(600_000), resp.GetRepairOrder().GetTotalAmount())
	})

	t.Run("every content path is accepted", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)
		e.repairOrders.EXPECT().FindByID(anyArg, garageID, repairOrderID).Return(openRepairOrder(), nil).Once()

		_, err := e.srv.UpdateRepairOrderLine(signedIn(t), &repairorderv1.UpdateRepairOrderLineRequest{
			GarageId: garageID, RepairOrderId: repairOrderID, LineId: "line-1",
			Line: &repairorderv1.RepairOrderLine{Description: "x", ItemRefId: new("p"), UnitId: new("u"), EnteredQuantity: "1"},
			UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{
				"item_ref_id", "unit_id", "entered_quantity",
			}},
		})
		require.Error(t, err) // a line without a part cannot take a unit: validated by the use case
	})
}
