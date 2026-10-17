package usecase

import (
	"errors"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
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

// classified maps a domain error to the API error code and, where a client
// needs to tell cases apart, a reason.
var classified = []struct {
	err    error
	code   apperror.Code
	reason string
}{
	{domain.ErrPartNotFound, apperror.CodeNotFound, ""},
	{domain.ErrGoodsReceiptNotFound, apperror.CodeNotFound, ""},
	{domain.ErrStockIssueNotFound, apperror.CodeNotFound, ""},
	{domain.ErrPartInactive, apperror.CodeFailedPrecondition, ""},
	{domain.ErrGoodsReceiptNotDraft, apperror.CodeFailedPrecondition, ReasonGoodsReceiptNotDraft},
	{domain.ErrGoodsReceiptAlreadyCancelled, apperror.CodeFailedPrecondition, ReasonGoodsReceiptAlreadyCancelled},
	{domain.ErrSupplierRequired, apperror.CodeFailedPrecondition, ReasonSupplierRequired},
	{domain.ErrInsufficientStockToReverse, apperror.CodeFailedPrecondition, ReasonInsufficientStockToReverse},
	{domain.ErrStockIssueNotDraft, apperror.CodeFailedPrecondition, ReasonStockIssueNotDraft},
	{domain.ErrNoLines, apperror.CodeInvalidArgument, ""},
	{domain.ErrSupplierNotFound, apperror.CodeInvalidArgument, ""},
	{domain.ErrInvalidReceiptDate, apperror.CodeInvalidArgument, ""},
	{domain.ErrInvalidReport, apperror.CodeInvalidArgument, ""},
	{domain.ErrDuplicateSupplier, apperror.CodeInvalidArgument, ""},
	{domain.ErrTooManyDefaults, apperror.CodeInvalidArgument, ""},
	{domain.ErrInvalidQuantity, apperror.CodeInvalidArgument, ""},
	{domain.ErrInvalidCost, apperror.CodeInvalidArgument, ""},
}

// Reasons that tell clients which rule refused a request.
const (
	ReasonGoodsReceiptNotDraft         = "GOODS_RECEIPT_NOT_DRAFT"
	ReasonGoodsReceiptAlreadyCancelled = "GOODS_RECEIPT_ALREADY_CANCELLED"
	ReasonSupplierRequired             = "SUPPLIER_REQUIRED"
	ReasonInsufficientStockToReverse   = "INSUFFICIENT_STOCK_TO_REVERSE"
	ReasonStockIssueNotDraft           = "STOCK_ISSUE_NOT_DRAFT"
)

// toAppError classifies err into an *apperror.AppError; op names the
// failed operation for unexpected (internal) errors. Errors the Catalogue
// already classified pass through.
func toAppError(op string, err error) error {
	if _, ok := apperror.As(err); ok {
		return err
	}

	for _, c := range classified {
		if errors.Is(err, c.err) {
			appErr := apperror.Wrap(c.code, err.Error(), err)
			if c.reason != "" {
				appErr = appErr.WithReason(c.reason)
			}

			return appErr
		}
	}

	return apperror.Wrap(apperror.CodeInternal, op, err)
}

// actorRef returns nil for an unknown actor so audit columns stay NULL.
func actorRef(userID string) *string {
	if userID == "" {
		return nil
	}

	return &userID
}
