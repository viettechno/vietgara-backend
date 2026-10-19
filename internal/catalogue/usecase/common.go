package usecase

import (
	"errors"

	"github.com/viettechno/vietgara-backend/internal/catalogue/domain"
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

// Client-facing reasons for barcode rules (FR-INV-13).
const (
	ReasonBarcodeAlreadyUsed      = "BARCODE_ALREADY_USED"
	ReasonBarcodeInvalid          = "BARCODE_INVALID"
	ReasonPackagingUnitHasBarcode = "PACKAGING_UNIT_HAS_BARCODE"
)

// Client-facing reasons for the job catalogue (FR-CAT-05).
const (
	ReasonJobInactive            = "JOB_INACTIVE"
	ReasonPackagingUnitUsedByJob = "PACKAGING_UNIT_USED_BY_JOB"
)

var (
	notFoundErrors = []error{
		domain.ErrPartNotFound, domain.ErrUnitNotFound, domain.ErrCategoryNotFound, domain.ErrImageNotFound,
		domain.ErrBarcodeNotFound, domain.ErrJobNotFound,
	}
	conflictErrors = []error{
		domain.ErrSKUTaken, domain.ErrUnitNameTaken, domain.ErrCategoryNameTaken, domain.ErrImageAlreadyAttached,
		domain.ErrBarcodeAlreadyUsed, domain.ErrJobNameTaken,
	}
	preconditionErrors = []error{domain.ErrBaseUnitLocked, domain.ErrPartInactive, domain.ErrPackagingUnitHasBarcode}
	invalidErrors      = []error{
		domain.ErrSKURequired, domain.ErrNameRequired, domain.ErrBaseUnitRequired, domain.ErrFieldTooLong,
		domain.ErrNegativePrice, domain.ErrInvalidThreshold, domain.ErrUnitNameRequired, domain.ErrUnitAbbrevRequired,
		domain.ErrCategoryNameRequired, domain.ErrCategoryDepth, domain.ErrCategoryCycle, domain.ErrInvalidFactor,
		domain.ErrBaseUnitPackaged, domain.ErrDuplicateUnit, domain.ErrInactiveUnit, domain.ErrInactiveCategory,
		domain.ErrTooManyImages, domain.ErrNotPartImage, domain.ErrUnitNotForPart, domain.ErrQuantityPrecision,
		domain.ErrInvalidQuantity, domain.ErrCategoryHasChildren, domain.ErrBarcodeInvalid,
		domain.ErrJobNameRequired, domain.ErrInvalidLaborPrice, domain.ErrInvalidDuration, domain.ErrTooManyJobParts,
		domain.ErrDuplicateJobPart,
	}
)

// ToAppError classifies err into an *apperror.AppError; op names the failed
// operation for unexpected (internal) errors. It is exported so the other
// modules that call Catalogue can pass its classified errors through.
func ToAppError(op string, err error) error {
	return toAppError(op, err)
}

func toAppError(op string, err error) error {
	if _, ok := apperror.As(err); ok {
		return err
	}

	if err := barcodeAppError(err); err != nil {
		return err
	}

	switch {
	case isAny(err, notFoundErrors):
		return apperror.Wrap(apperror.CodeNotFound, err.Error(), err)
	case isAny(err, conflictErrors):
		return apperror.Wrap(apperror.CodeAlreadyExists, err.Error(), err)
	case isAny(err, preconditionErrors):
		return apperror.Wrap(apperror.CodeFailedPrecondition, err.Error(), err)
	case isAny(err, invalidErrors):
		return apperror.Wrap(apperror.CodeInvalidArgument, err.Error(), err)
	default:
		return apperror.Wrap(apperror.CodeInternal, op, err)
	}
}

// barcodeAppError gives the barcode rules their own stable reasons; nil for
// any other error.
func barcodeAppError(err error) error {
	switch {
	case errors.Is(err, domain.ErrBarcodeAlreadyUsed):
		return apperror.Wrap(apperror.CodeAlreadyExists, err.Error(), err).WithReason(ReasonBarcodeAlreadyUsed)
	case errors.Is(err, domain.ErrBarcodeInvalid), errors.Is(err, domain.ErrUnitNotForPart):
		return apperror.Wrap(apperror.CodeInvalidArgument, err.Error(), err).WithReason(ReasonBarcodeInvalid)
	case errors.Is(err, domain.ErrPackagingUnitHasBarcode):
		return apperror.Wrap(apperror.CodeFailedPrecondition, err.Error(), err).WithReason(ReasonPackagingUnitHasBarcode)
	case errors.Is(err, domain.ErrJobInactive):
		return apperror.Wrap(apperror.CodeFailedPrecondition, err.Error(), err).WithReason(ReasonJobInactive)
	case errors.Is(err, domain.ErrPackagingUnitUsedByJob):
		return apperror.Wrap(apperror.CodeFailedPrecondition, err.Error(), err).WithReason(ReasonPackagingUnitUsedByJob)
	default:
		return nil
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
