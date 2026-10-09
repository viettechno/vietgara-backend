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

var (
	notFoundErrors = []error{domain.ErrItemNotFound}
	conflictErrors = []error{domain.ErrSKUTaken}
	invalidErrors  = []error{
		domain.ErrSKURequired, domain.ErrNameRequired, domain.ErrInvalidQuantity, domain.ErrInvalidReorderThreshold,
		domain.ErrFieldTooLong,
	}
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
func actorRef(userID string) *string {
	if userID == "" {
		return nil
	}

	return &userID
}
