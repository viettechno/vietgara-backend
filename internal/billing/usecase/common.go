package usecase

import (
	"context"
	"errors"

	"github.com/viettechno/vietgara-backend/internal/billing/domain"
	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/platform/audit"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// Paging defaults from API Specification Section 1.1.
const (
	DefaultPageSize = 20
	MaxPageSize     = 100
)

// PageRequest asks for one page of a listing; zero values mean page 1 with
// DefaultPageSize records.
type PageRequest struct {
	Page     int
	PageSize int
}

func (p PageRequest) normalize() PageRequest {
	if p.Page < 1 {
		p.Page = 1
	}

	if p.PageSize < 1 {
		p.PageSize = DefaultPageSize
	}

	p.PageSize = min(p.PageSize, MaxPageSize)

	return p
}

func (p PageRequest) offset() int { return (p.Page - 1) * p.PageSize }

// Page is one page of a listing together with the total number of matches.
type Page[T any] struct {
	Items    []T
	Total    int
	Page     int
	PageSize int
}

var (
	notFoundErrors = []error{domain.ErrSettlementNotFound, domain.ErrInsurerNotFound}
	conflictErrors = []error{domain.ErrAlreadySettled}
	invalidErrors  = []error{
		domain.ErrInvalidAllocation, domain.ErrInsurerPartnerRequired, domain.ErrInvalidPayerType,
		domain.ErrInvalidPaymentMethod, domain.ErrInvalidPaymentAmount,
	}
	preconditionErrors = []error{domain.ErrRepairOrderNotClosed}
)

// toAppError classifies err into an *apperror.AppError; op names the
// failed operation for unexpected (internal) errors.
func toAppError(op string, err error) error {
	if _, ok := apperror.As(err); ok {
		return err
	}

	switch {
	case isAny(err, notFoundErrors):
		return apperror.Wrap(apperror.CodeNotFound, err.Error(), err)
	case isAny(err, conflictErrors):
		return apperror.Wrap(apperror.CodeAlreadyExists, err.Error(), err)
	case isAny(err, invalidErrors):
		return apperror.Wrap(apperror.CodeInvalidArgument, err.Error(), err)
	case isAny(err, preconditionErrors):
		return apperror.Wrap(apperror.CodeFailedPrecondition, err.Error(), err)
	default:
		return apperror.Wrap(apperror.CodeInternal, op, err)
	}
}

func isAny(err error, targets []error) bool {
	for _, target := range targets {
		if errors.Is(err, target) {
			return true
		}
	}

	return false
}

// actorRef returns nil for an unknown actor so audit columns stay NULL.
func actorRef(accountID string) *string {
	if accountID == "" {
		return nil
	}

	return &accountID
}

// Audited actions (Security Architecture Section 7).
const (
	ActionSettlementCreated   = "settlement.created"
	ActionSettlementAllocated = "settlement.allocated"
	ActionPaymentRecorded     = "settlement.payment_recorded"

	entitySettlement = "settlement"
	entityPayment    = "settlement_payment"
)

// audit records a change in the caller's transaction, so the change and
// its audit entry commit or roll back together.
func (uc *SettlementUseCase) audit(ctx context.Context, actorID, garageID, action, entityType, entityID string, before, after any) error {
	err := uc.Audit.Record(ctx, audit.Entry{
		Actor: actorID, GarageID: garageID, Action: action, EntityType: entityType, EntityID: entityID,
		Before: before, After: after, IP: middleware.ClientIP(ctx),
	})
	if err != nil {
		return toAppError("audit "+action, err)
	}

	return nil
}

// settlementAudit is what the audit log keeps of a settlement: its money,
// who pays it and its status.
func settlementAudit(s *domain.Settlement) map[string]any {
	return map[string]any{
		"number": s.Number, "repair_order_id": s.RepairOrderID, "total_amount": s.TotalAmount,
		"customer_amount": s.CustomerAmount, "insurer_amount": s.InsurerAmount, "insurer_partner_id": s.InsurerPartnerID,
		"customer_paid_amount": s.CustomerPaidAmount, "insurer_paid_amount": s.InsurerPaidAmount, "status": s.Status,
	}
}

func paymentAudit(p *domain.Payment) map[string]any {
	return map[string]any{"payer_type": p.PayerType, "amount": p.Amount, "method": p.Method, "note": p.Note}
}
