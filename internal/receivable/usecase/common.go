package usecase

import (
	"context"
	"errors"

	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/platform/audit"
	"github.com/viettechno/vietgara-backend/internal/receivable/domain"
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

// Reasons the client translates.
const (
	ReasonReceivableNotFound      = "RECEIVABLE_NOT_FOUND"
	ReasonPayableNotFound         = "PAYABLE_NOT_FOUND"
	ReasonVoucherNotFound         = "PAYMENT_OUT_VOUCHER_NOT_FOUND"
	ReasonSupplierNotFound        = "SUPPLIER_NOT_FOUND"
	ReasonInvalidPaymentOutAmount = "INVALID_PAYMENT_OUT_AMOUNT"
	ReasonInvalidAllocation       = "INVALID_ALLOCATION"
	ReasonAlreadyVoided           = "PAYMENT_OUT_ALREADY_VOIDED"
)

// Audited actions (Security Architecture Section 7).
const (
	ActionSettingsUpdated = "receivable.settings_updated"

	entitySettings = "receivable_settings"
)

// toAppError classifies err into an *apperror.AppError; op names the failed
// operation for unexpected (internal) errors.
func toAppError(op string, err error) error {
	if _, ok := apperror.As(err); ok {
		return err
	}

	switch {
	case errors.Is(err, domain.ErrReceivableNotFound):
		return apperror.Wrap(apperror.CodeNotFound, err.Error(), err).WithReason(ReasonReceivableNotFound)
	case errors.Is(err, domain.ErrInvalidTerms):
		return apperror.Validation("settings.paymentTermsDays", apperror.IssueInvalid, err.Error())
	case errors.Is(err, domain.ErrInvalidOffsets):
		return apperror.Validation("settings.reminderOffsetDays", apperror.IssueInvalid, err.Error())
	case errors.Is(err, domain.ErrIncompleteSettings):
		return apperror.Validation("settings", apperror.IssueRequired, err.Error())
	case errors.Is(err, domain.ErrInvalidPeriod):
		return apperror.Validation("from", apperror.IssueInvalid, err.Error())
	default:
		return payableAppError(op, err)
	}
}

// payableAppError classifies the errors of payables and payment-out vouchers.
func payableAppError(op string, err error) error {
	switch {
	case errors.Is(err, domain.ErrPayableNotFound):
		return apperror.Wrap(apperror.CodeNotFound, err.Error(), err).WithReason(ReasonPayableNotFound)
	case errors.Is(err, domain.ErrVoucherNotFound):
		return apperror.Wrap(apperror.CodeNotFound, err.Error(), err).WithReason(ReasonVoucherNotFound)
	case errors.Is(err, domain.ErrSupplierNotFound):
		return apperror.Wrap(apperror.CodeNotFound, err.Error(), err).WithReason(ReasonSupplierNotFound)
	case errors.Is(err, domain.ErrInvalidPaymentOutAmount):
		return apperror.Wrap(apperror.CodeInvalidArgument, err.Error(), err).WithReason(ReasonInvalidPaymentOutAmount)
	case errors.Is(err, domain.ErrInvalidAllocation):
		return apperror.Wrap(apperror.CodeInvalidArgument, err.Error(), err).WithReason(ReasonInvalidAllocation)
	case errors.Is(err, domain.ErrPaymentOutAlreadyVoided):
		return apperror.Wrap(apperror.CodeAlreadyExists, err.Error(), err).WithReason(ReasonAlreadyVoided)
	case errors.Is(err, domain.ErrInvalidPaymentOutMethod):
		return apperror.Validation("method", apperror.IssueInvalid, err.Error())
	case errors.Is(err, domain.ErrInvalidPaymentOutVoucher):
		return apperror.Validation("voucher", apperror.IssueInvalid, err.Error())
	case errors.Is(err, domain.ErrInvalidPaymentOutVoidNote):
		return apperror.Validation("reason", apperror.IssueInvalid, err.Error())
	default:
		return apperror.Wrap(apperror.CodeInternal, op, err)
	}
}

// record writes an audit entry in the caller's transaction.
func (uc *ReceivableUseCase) record(ctx context.Context, actorID, garageID, action, entityType, entityID string, before, after any) error {
	return recordAudit(ctx, uc.audit, actorID, garageID, action, entityType, entityID, before, after)
}

func recordAudit(ctx context.Context, logger AuditLogger, actorID, garageID, action, entityType, entityID string, before, after any) error {
	err := logger.Record(ctx, audit.Entry{
		Actor: actorID, GarageID: garageID, Action: action, EntityType: entityType, EntityID: entityID,
		Before: before, After: after, IP: middleware.ClientIP(ctx),
	})
	if err != nil {
		return toAppError("audit "+action, err)
	}

	return nil
}
