package handler

import (
	"math"

	commonv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/common/v1"
	repairorderv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/repairorder/v1"

	"github.com/viettechno/vietgara-backend/internal/repairorder/domain"
	"github.com/viettechno/vietgara-backend/internal/repairorder/usecase"
	"github.com/viettechno/vietgara-backend/pkg/converter"
)

func repairOrderToProto(ro *domain.RepairOrder) *repairorderv1.RepairOrder {
	out := &repairorderv1.RepairOrder{
		Id: ro.ID, GarageId: ro.GarageID, VehicleId: ro.VehicleID, VehicleLicensePlate: ro.VehicleLicensePlate,
		CustomerName: ro.CustomerName, Number: ro.Number, Status: statusToProto(ro.Status), TotalAmount: ro.TotalAmount,
		Lines:     make([]*repairorderv1.RepairOrderLine, 0, len(ro.Lines)),
		CreatedAt: converter.TimeToProto(ro.CreatedAt), UpdatedAt: converter.TimeToProto(ro.UpdatedAt),
	}

	if ro.QuoteID != nil {
		out.QuoteId = *ro.QuoteID
	}

	if ro.ClosedAt != nil {
		out.ClosedAt = converter.TimeToProto(*ro.ClosedAt)
	}

	for _, line := range ro.Lines {
		out.Lines = append(out.Lines, lineToProto(line))
	}

	return out
}

func lineToProto(line domain.Line) *repairorderv1.RepairOrderLine {
	return &repairorderv1.RepairOrderLine{
		Id: line.ID, ItemType: itemTypeToProto(line.ItemType), ItemRefId: line.ItemRefID, Description: line.Description,
		Quantity: line.Quantity, UnitPrice: line.UnitPrice, LineTotal: line.LineTotal, Status: lineStatusToProto(line.Status),
		AssignedTechnicianIds: line.AssignedTechnicianIDs,
	}
}

func statusToProto(s domain.RepairOrderStatus) repairorderv1.RepairOrderStatus {
	switch s {
	case domain.StatusOpen:
		return repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_OPEN
	case domain.StatusInProgress:
		return repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_IN_PROGRESS
	case domain.StatusWaitingOnParts:
		return repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_WAITING_ON_PARTS
	case domain.StatusCompleted:
		return repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_COMPLETED
	case domain.StatusClosed:
		return repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_CLOSED
	default:
		return repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_UNSPECIFIED
	}
}

// statusFromProto maps UNSPECIFIED (and unknown values) to "", which the
// use case treats as "every status" when listing.
func statusFromProto(s repairorderv1.RepairOrderStatus) domain.RepairOrderStatus {
	switch s {
	case repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_OPEN:
		return domain.StatusOpen
	case repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_IN_PROGRESS:
		return domain.StatusInProgress
	case repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_WAITING_ON_PARTS:
		return domain.StatusWaitingOnParts
	case repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_COMPLETED:
		return domain.StatusCompleted
	case repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_CLOSED:
		return domain.StatusClosed
	case repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_UNSPECIFIED:
		return ""
	default:
		return ""
	}
}

func itemTypeToProto(t domain.ItemType) repairorderv1.RepairOrderItemType {
	if t == domain.ItemTypePart {
		return repairorderv1.RepairOrderItemType_REPAIR_ORDER_ITEM_TYPE_PART
	}

	return repairorderv1.RepairOrderItemType_REPAIR_ORDER_ITEM_TYPE_LABOR
}

func itemTypeFromProto(t repairorderv1.RepairOrderItemType) domain.ItemType {
	switch t {
	case repairorderv1.RepairOrderItemType_REPAIR_ORDER_ITEM_TYPE_LABOR:
		return domain.ItemTypeLabor
	case repairorderv1.RepairOrderItemType_REPAIR_ORDER_ITEM_TYPE_PART:
		return domain.ItemTypePart
	case repairorderv1.RepairOrderItemType_REPAIR_ORDER_ITEM_TYPE_UNSPECIFIED:
		return ""
	default:
		return ""
	}
}

func lineStatusToProto(s domain.LineStatus) repairorderv1.RepairOrderLineStatus {
	switch s {
	case domain.LineNotStarted:
		return repairorderv1.RepairOrderLineStatus_REPAIR_ORDER_LINE_STATUS_NOT_STARTED
	case domain.LineInProgress:
		return repairorderv1.RepairOrderLineStatus_REPAIR_ORDER_LINE_STATUS_IN_PROGRESS
	case domain.LineCompleted:
		return repairorderv1.RepairOrderLineStatus_REPAIR_ORDER_LINE_STATUS_COMPLETED
	default:
		return repairorderv1.RepairOrderLineStatus_REPAIR_ORDER_LINE_STATUS_UNSPECIFIED
	}
}

func lineStatusFromProto(s repairorderv1.RepairOrderLineStatus) domain.LineStatus {
	switch s {
	case repairorderv1.RepairOrderLineStatus_REPAIR_ORDER_LINE_STATUS_NOT_STARTED:
		return domain.LineNotStarted
	case repairorderv1.RepairOrderLineStatus_REPAIR_ORDER_LINE_STATUS_IN_PROGRESS:
		return domain.LineInProgress
	case repairorderv1.RepairOrderLineStatus_REPAIR_ORDER_LINE_STATUS_COMPLETED:
		return domain.LineCompleted
	case repairorderv1.RepairOrderLineStatus_REPAIR_ORDER_LINE_STATUS_UNSPECIFIED:
		return ""
	default:
		return ""
	}
}

// lineInputsFromProto maps a create request's lines to the use case's
// LineInput.
func lineInputsFromProto(lines []*repairorderv1.RepairOrderLine) []usecase.LineInput {
	out := make([]usecase.LineInput, 0, len(lines))
	for _, line := range lines {
		out = append(out, usecase.LineInput{
			ItemType: itemTypeFromProto(line.GetItemType()), ItemRefID: line.ItemRefId,
			Description: line.GetDescription(), Quantity: line.GetQuantity(), UnitPrice: line.GetUnitPrice(),
		})
	}

	return out
}

func paginationToProto(p *usecase.Page[*domain.RepairOrder]) *commonv1.Pagination {
	return &commonv1.Pagination{Total: toInt32(p.Total), Page: toInt32(p.Page), PageSize: toInt32(p.PageSize)}
}

func pageRequest(page, pageSize int32) usecase.PageRequest {
	return usecase.PageRequest{Page: int(page), PageSize: int(pageSize)}
}

func toInt32(n int) int32 {
	if n > math.MaxInt32 {
		return math.MaxInt32
	}

	if n < math.MinInt32 {
		return math.MinInt32
	}

	return int32(n)
}
