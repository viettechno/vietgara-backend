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
		CreatedAt:    converter.TimeToProto(c.CreatedAt),
		UpdatedAt:    converter.TimeToProto(c.UpdatedAt),
	}

	if consent := c.LatestConsent(); consent != nil {
		out.PersonalDataConsent = consent.Granted
		out.PersonalDataConsentAt = converter.TimeToProto(consent.RecordedAt)
	}

	return out
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
		CreatedAt:    converter.TimeToProto(v.CreatedAt),
		UpdatedAt:    converter.TimeToProto(v.UpdatedAt),
	}

	if v.Year != nil {
		year := toInt32(*v.Year)
		out.Year = &year
	}

	return out
}

func partnerToProto(p *domain.Partner) *customerv1.Partner {
	return &customerv1.Partner{
		Id:       p.ID,
		GarageId: p.GarageID,
		Type:     partnerTypeToProto(p.Type),
		Name:     p.Name,
		TaxCode:  p.TaxCode,
		Contact: &customerv1.PartnerContact{
			ContactPerson: p.Contact.ContactPerson,
			Phone:         p.Contact.Phone,
			Email:         p.Contact.Email,
			Address:       p.Contact.Address,
		},
		Note:      p.Note,
		CreatedAt: converter.TimeToProto(p.CreatedAt),
		UpdatedAt: converter.TimeToProto(p.UpdatedAt),
	}
}

// partnerContactFromProto maps an absent contact to an empty one.
func partnerContactFromProto(c *customerv1.PartnerContact) usecase.PartnerContactInput {
	if c == nil {
		return usecase.PartnerContactInput{}
	}

	return usecase.PartnerContactInput{
		ContactPerson: c.ContactPerson,
		Phone:         c.Phone,
		Email:         c.Email,
		Address:       c.Address,
	}
}

func partnerTypeToProto(t domain.PartnerType) customerv1.PartnerType {
	switch t {
	case domain.PartnerTypeSupplier:
		return customerv1.PartnerType_PARTNER_TYPE_SUPPLIER
	case domain.PartnerTypeInsurer:
		return customerv1.PartnerType_PARTNER_TYPE_INSURER
	default:
		return customerv1.PartnerType_PARTNER_TYPE_UNSPECIFIED
	}
}

// partnerTypeFromProto maps UNSPECIFIED (and unknown values) to "", which
// the use cases treat as "no type" (rejected on create, "all" when listing).
func partnerTypeFromProto(t customerv1.PartnerType) domain.PartnerType {
	switch t {
	case customerv1.PartnerType_PARTNER_TYPE_SUPPLIER:
		return domain.PartnerTypeSupplier
	case customerv1.PartnerType_PARTNER_TYPE_INSURER:
		return domain.PartnerTypeInsurer
	case customerv1.PartnerType_PARTNER_TYPE_UNSPECIFIED:
		return ""
	default:
		return ""
	}
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
