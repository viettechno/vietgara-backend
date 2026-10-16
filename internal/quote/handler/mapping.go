package handler

import (
	"math"
	"time"

	"github.com/viettechno/vietgara-backend/internal/platform/httpapi"

	commonv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/common/v1"
	quotev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/quote/v1"

	"github.com/viettechno/vietgara-backend/internal/quote/domain"
	"github.com/viettechno/vietgara-backend/internal/quote/usecase"
	"github.com/viettechno/vietgara-backend/pkg/converter"
)

func quoteToProto(q *domain.Quote) *quotev1.Quote {
	lines := make([]*quotev1.QuoteLine, 0, len(q.Lines))
	for _, line := range q.Lines {
		lines = append(lines, lineToProto(line))
	}

	return &quotev1.Quote{
		Id: q.ID, GarageId: q.GarageID, CustomerId: q.CustomerID, CustomerName: q.CustomerName, VehicleId: q.VehicleID,
		VehicleLicensePlate: q.VehicleLicensePlate, Number: q.Number, Status: statusToProto(q.Status),
		ApprovalMode: approvalModeToProto(q.ApprovalMode), Lines: lines, TotalAmount: q.TotalAmount, Notes: q.Notes,
		SentAt: converter.TimeToProto(zeroTime(q.SentAt)), DecidedAt: converter.TimeToProto(zeroTime(q.DecidedAt)),
		ConvertedAt: converter.TimeToProto(zeroTime(q.ConvertedAt)),
		CreatedAt:   converter.TimeToProto(q.CreatedAt), UpdatedAt: converter.TimeToProto(q.UpdatedAt),
		BillingParty: httpapi.BillingPartyToProto(q.BillingParty),
	}
}

func publicQuoteToProto(q *domain.Quote) *quotev1.PublicQuote {
	lines := make([]*quotev1.QuoteLine, 0, len(q.Lines))
	for _, line := range q.Lines {
		lines = append(lines, lineToProto(line))
	}

	return &quotev1.PublicQuote{
		Number: q.Number, Status: statusToProto(q.Status), CustomerName: q.CustomerName,
		VehicleLicensePlate: q.VehicleLicensePlate, Lines: lines, TotalAmount: q.TotalAmount, Notes: q.Notes,
		ExpiresAt: converter.TimeToProto(zeroTime(q.PublicTokenExpiresAt)),
	}
}

func lineToProto(l domain.Line) *quotev1.QuoteLine {
	return &quotev1.QuoteLine{
		Id: l.ID, ItemType: itemTypeToProto(l.ItemType), ItemRefId: l.ItemRefID, Description: l.Description,
		Quantity: l.Quantity, UnitPrice: l.UnitPrice, LineTotal: l.LineTotal, Decision: decisionToProto(l.Decision),
		UnitId: l.UnitID, EnteredQuantity: derefString(l.EnteredQuantity), UnitAbbreviation: l.UnitAbbreviation,
	}
}

func lineFromProto(l *quotev1.QuoteLine) usecase.LineInput {
	return usecase.LineInput{
		ItemType: itemTypeFromProto(l.GetItemType()), ItemRefID: l.ItemRefId, Description: l.GetDescription(),
		Quantity: l.GetQuantity(), UnitID: l.GetUnitId(), EnteredQuantity: l.GetEnteredQuantity(), UnitPrice: l.GetUnitPrice(),
	}
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}

	return *s
}

func linesFromProto(lines []*quotev1.QuoteLine) []usecase.LineInput {
	out := make([]usecase.LineInput, 0, len(lines))
	for _, l := range lines {
		out = append(out, lineFromProto(l))
	}

	return out
}

func statusToProto(s domain.Status) quotev1.QuoteStatus {
	switch s {
	case domain.StatusDraft:
		return quotev1.QuoteStatus_QUOTE_STATUS_DRAFT
	case domain.StatusSent:
		return quotev1.QuoteStatus_QUOTE_STATUS_SENT
	case domain.StatusApproved:
		return quotev1.QuoteStatus_QUOTE_STATUS_APPROVED
	case domain.StatusRejected:
		return quotev1.QuoteStatus_QUOTE_STATUS_REJECTED
	case domain.StatusConverted:
		return quotev1.QuoteStatus_QUOTE_STATUS_CONVERTED
	default:
		return quotev1.QuoteStatus_QUOTE_STATUS_UNSPECIFIED
	}
}

// statusFromProto maps UNSPECIFIED (and unknown values) to "", which the
// use case treats as "every status" when listing.
func statusFromProto(s quotev1.QuoteStatus) domain.Status {
	switch s {
	case quotev1.QuoteStatus_QUOTE_STATUS_DRAFT:
		return domain.StatusDraft
	case quotev1.QuoteStatus_QUOTE_STATUS_SENT:
		return domain.StatusSent
	case quotev1.QuoteStatus_QUOTE_STATUS_APPROVED:
		return domain.StatusApproved
	case quotev1.QuoteStatus_QUOTE_STATUS_REJECTED:
		return domain.StatusRejected
	case quotev1.QuoteStatus_QUOTE_STATUS_CONVERTED:
		return domain.StatusConverted
	case quotev1.QuoteStatus_QUOTE_STATUS_UNSPECIFIED:
		return ""
	default:
		return ""
	}
}

func approvalModeToProto(m domain.ApprovalMode) commonv1.QuoteApprovalMode {
	if m == domain.ApprovalModeManual {
		return commonv1.QuoteApprovalMode_QUOTE_APPROVAL_MODE_MANUAL
	}

	return commonv1.QuoteApprovalMode_QUOTE_APPROVAL_MODE_LINK
}

// approvalModeFromProto maps UNSPECIFIED to "", which the use case treats
// as "use the garage's default".
func approvalModeFromProto(m commonv1.QuoteApprovalMode) domain.ApprovalMode {
	switch m {
	case commonv1.QuoteApprovalMode_QUOTE_APPROVAL_MODE_LINK:
		return domain.ApprovalModeLink
	case commonv1.QuoteApprovalMode_QUOTE_APPROVAL_MODE_MANUAL:
		return domain.ApprovalModeManual
	case commonv1.QuoteApprovalMode_QUOTE_APPROVAL_MODE_UNSPECIFIED:
		return ""
	default:
		return ""
	}
}

func itemTypeToProto(t domain.ItemType) quotev1.QuoteItemType {
	if t == domain.ItemTypePart {
		return quotev1.QuoteItemType_QUOTE_ITEM_TYPE_PART
	}

	return quotev1.QuoteItemType_QUOTE_ITEM_TYPE_LABOR
}

func itemTypeFromProto(t quotev1.QuoteItemType) domain.ItemType {
	switch t {
	case quotev1.QuoteItemType_QUOTE_ITEM_TYPE_LABOR:
		return domain.ItemTypeLabor
	case quotev1.QuoteItemType_QUOTE_ITEM_TYPE_PART:
		return domain.ItemTypePart
	case quotev1.QuoteItemType_QUOTE_ITEM_TYPE_UNSPECIFIED:
		return ""
	default:
		return ""
	}
}

func decisionToProto(d domain.LineDecision) quotev1.QuoteLineDecision {
	switch d {
	case domain.DecisionApproved:
		return quotev1.QuoteLineDecision_QUOTE_LINE_DECISION_APPROVED
	case domain.DecisionRejected:
		return quotev1.QuoteLineDecision_QUOTE_LINE_DECISION_REJECTED
	case domain.DecisionPending:
		return quotev1.QuoteLineDecision_QUOTE_LINE_DECISION_PENDING
	default:
		return quotev1.QuoteLineDecision_QUOTE_LINE_DECISION_PENDING
	}
}

func decisionsFromProto(in []*quotev1.QuoteLineDecisionInput) []usecase.LineDecisionInput {
	out := make([]usecase.LineDecisionInput, 0, len(in))
	for _, d := range in {
		out = append(out, usecase.LineDecisionInput{LineID: d.GetLineId(), Approved: d.GetApproved()})
	}

	return out
}

func paginationToProto(p *usecase.Page[*domain.Quote]) *commonv1.Pagination {
	return &commonv1.Pagination{Total: toInt32(p.Total), Page: toInt32(p.Page), PageSize: toInt32(p.PageSize)}
}

func pageRequest(page, pageSize int32) usecase.PageRequest {
	return usecase.PageRequest{Page: int(page), PageSize: int(pageSize)}
}

// zeroTime returns time.Time{} for a nil pointer so converter.TimeToProto
// yields nil (as it does for any zero timestamp).
func zeroTime(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}

	return *t
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
