// Package usecase contains the application use cases of the Quote &
// Pricing module (FRD 3.5): creating and editing quotes, sending them for
// customer approval (FR-QUO-02, with the LINK/MANUAL choice this module
// adds per the updated requirement), recording the customer's decision
// (FR-QUO-03) and converting an approved quote into a draft repair order
// (FR-QUO-04). Every method expects to run inside a garage-scoped
// transaction (see middleware.Garage) so Row-Level Security applies.
// See: Coding_Standards.docx Section 2.3, HLD Section 4, LLD Section 2.
package usecase

import (
	"errors"
	"time"

	"github.com/viettechno/vietgara-backend/internal/quote/domain"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// Paging defaults from API Specification Section 1.1.
const (
	DefaultPageSize = 20
	MaxPageSize     = 100
)

// Document types recorded in document_sequences (internal/platform/numbering).
const (
	docTypeQuote       = "quote"
	docTypeRepairOrder = "repair_order"
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

// Page is one page of a listing together with the total number of matches.
type Page[T any] struct {
	Items    []T
	Total    int
	Page     int
	PageSize int
}

var (
	notFoundErrors = []error{domain.ErrQuoteNotFound}
	invalidErrors  = []error{
		domain.ErrCustomerRequired, domain.ErrVehicleRequired, domain.ErrNoLines, domain.ErrTooManyLines,
		domain.ErrDescriptionRequired, domain.ErrInvalidItemType, domain.ErrInvalidQuantity, domain.ErrInvalidUnitPrice,
		domain.ErrInvalidApprovalMode, domain.ErrFieldTooLong, domain.ErrDecisionMismatch,
	}
	preconditionErrors = []error{domain.ErrNotEditable, domain.ErrNotApproved, domain.ErrLinkModeOnly}
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
	case isAny(err, invalidErrors):
		return apperror.Wrap(apperror.CodeInvalidArgument, err.Error(), err)
	case isAny(err, preconditionErrors):
		return apperror.Wrap(apperror.CodeFailedPrecondition, err.Error(), err)
	case errors.Is(err, domain.ErrPublicTokenInvalid):
		return apperror.Wrap(apperror.CodeInvalidArgument, err.Error(), err).WithReason(ReasonPublicTokenInvalid)
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

// ReasonPublicTokenInvalid is the API error code for an unknown, expired
// or already-used approval link.
const ReasonPublicTokenInvalid = "QUOTE_LINK_INVALID" //nolint:gosec // G101: an API error code, not a credential

func newClock() clock { return time.Now }
