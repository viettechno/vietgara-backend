package handler

import (
	"math"

	commonv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/common/v1"
	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"

	"github.com/viettechno/vietgara-backend/internal/customer/domain"
	"github.com/viettechno/vietgara-backend/internal/customer/usecase"
	"github.com/viettechno/vietgara-backend/pkg/converter"
)

func customerToProto(c *domain.Customer) *customerv1.Customer {
	out := &customerv1.Customer{
		Id:           c.ID,
		GarageId:     c.GarageID,
		FullName:     c.FullName,
		Phone:        c.Phone,
		Email:        c.Email,
		Address:      c.Address,
		VehicleCount: toInt32(c.VehicleCount),
		IsActive:     c.IsActive,
		LinkStatus:   LinkStatusToProto(c.LinkStatus),
		CreatedAt:    converter.TimeToProto(c.CreatedAt),
		UpdatedAt:    converter.TimeToProto(c.UpdatedAt),
	}

	if consent := c.LatestConsent(); consent != nil {
		out.PersonalDataConsent = consent.Granted
		out.PersonalDataConsentAt = converter.TimeToProto(consent.RecordedAt)
	}

	return out
}

// VehicleToProto and HistoryEntryToProto map for other modules' handlers
// (the Customer BFF), so the wire format stays defined in one place.
func VehicleToProto(v *domain.Vehicle) *customerv1.Vehicle { return vehicleToProto(v) }

// HistoryEntryToProto maps a history entry for other modules' handlers.
func HistoryEntryToProto(e usecase.HistoryEntry) *customerv1.VehicleHistoryEntry {
	return historyEntryToProto(e)
}

func vehicleToProto(v *domain.Vehicle) *customerv1.Vehicle {
	out := &customerv1.Vehicle{
		Id:           v.ID,
		GarageId:     v.GarageID,
		CustomerId:   v.CustomerID,
		CustomerName: v.CustomerName,
		LicensePlate: v.LicensePlate,
		Make:         v.Make,
		Model:        v.Model,
		Vin:          v.VIN,
		EngineNumber: v.EngineNumber,
		IsActive:     v.IsActive,
		CreatedAt:    converter.TimeToProto(v.CreatedAt),
		UpdatedAt:    converter.TimeToProto(v.UpdatedAt),
	}

	if v.Year != nil {
		year := toInt32(*v.Year)
		out.Year = &year
	}

	return out
}

func historyEntryToProto(e usecase.HistoryEntry) *customerv1.VehicleHistoryEntry {
	return &customerv1.VehicleHistoryEntry{
		Type: historyEntryTypeToProto(e.Type), EntityId: e.EntityID, Number: e.Number, Status: e.Status,
		TotalAmount: e.TotalAmount, OccurredAt: converter.TimeToProto(e.OccurredAt),
	}
}

func historyEntryTypeToProto(t usecase.HistoryEntryType) customerv1.VehicleHistoryEntryType {
	switch t {
	case usecase.HistoryEntryQuote:
		return customerv1.VehicleHistoryEntryType_VEHICLE_HISTORY_ENTRY_TYPE_QUOTE
	case usecase.HistoryEntryRepairOrder:
		return customerv1.VehicleHistoryEntryType_VEHICLE_HISTORY_ENTRY_TYPE_REPAIR_ORDER
	case usecase.HistoryEntrySettlement:
		return customerv1.VehicleHistoryEntryType_VEHICLE_HISTORY_ENTRY_TYPE_SETTLEMENT
	default:
		return customerv1.VehicleHistoryEntryType_VEHICLE_HISTORY_ENTRY_TYPE_UNSPECIFIED
	}
}

func paginationToProto[T any](p *usecase.Page[T]) *commonv1.Pagination {
	return &commonv1.Pagination{Total: toInt32(p.Total), Page: toInt32(p.Page), PageSize: toInt32(p.PageSize)}
}

func pageRequest(page, pageSize int32) usecase.PageRequest {
	return usecase.PageRequest{Page: int(page), PageSize: int(pageSize)}
}

func intPtr(v *int32) *int {
	if v == nil {
		return nil
	}

	n := int(*v)

	return &n
}

// toInt32 converts n, clamping it to the int32 range.
func toInt32(n int) int32 {
	if n > math.MaxInt32 {
		return math.MaxInt32
	}

	if n < math.MinInt32 {
		return math.MinInt32
	}

	return int32(n)
}
