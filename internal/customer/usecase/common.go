// Package usecase contains the application use cases of the Customer &
// Vehicle module: they validate input, enforce business rules, and
// orchestrate the domain and repository layers. Every method expects to
// run inside a garage-scoped transaction (see middleware.TransactionInterceptor
// and the HTTP handlers) so Row-Level Security applies to all queries.
// See: Coding_Standards.docx Section 2.3, LLD Section 2.
package usecase

import (
	"errors"
	"strings"
	"time"

	"github.com/viettechno/vietgara-backend/internal/customer/domain"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
	"github.com/viettechno/vietgara-backend/pkg/email"
	"github.com/viettechno/vietgara-backend/pkg/phone"
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

func (p PageRequest) filter(query string) ListFilter {
	return ListFilter{Query: strings.TrimSpace(query), Limit: p.PageSize, Offset: (p.Page - 1) * p.PageSize}
}

// Page is one page of a listing together with the total number of matches.
type Page[T any] struct {
	Items    []T
	Total    int
	Page     int
	PageSize int
}

// Option configures the use cases of this module.
type Option func(*options)

type options struct {
	now func() time.Time
}

// WithClock replaces time.Now, e.g. to make consent timestamps
// deterministic in tests.
func WithClock(now func() time.Time) Option {
	return func(o *options) {
		o.now = now
	}
}

func newOptions(opts []Option) options {
	o := options{now: time.Now}
	for _, opt := range opts {
		opt(&o)
	}

	return o
}

// normalizer cleans and validates one non-empty input value.
type normalizer func(string) (string, error)

// text trims a free-text value and enforces its maximum length.
func text(field string, maxLen int) normalizer {
	return func(v string) (string, error) {
		v = strings.TrimSpace(v)

		return v, domain.CheckLength(field, v, maxLen)
	}
}

// code is text upper-cased, for identifiers such as VIN or engine numbers.
func code(field string, maxLen int) normalizer {
	return func(v string) (string, error) {
		return text(field, maxLen)(strings.ToUpper(v))
	}
}

// optional applies n to v, mapping blank input to nil (a NULL column).
func optional(v *string, n normalizer) (*string, error) {
	if v == nil || strings.TrimSpace(*v) == "" {
		return nil, nil
	}

	out, err := n(*v)
	if err != nil {
		return nil, err
	}

	return &out, nil
}

// patch updates *dst from a partial-update field: nil keeps the current
// value, a blank string clears it, anything else is normalized and set.
func patch(dst **string, v *string, n normalizer) error {
	if v == nil {
		return nil
	}

	out, err := optional(v, n)
	if err != nil {
		return err
	}

	*dst = out

	return nil
}

// required normalizes a mandatory value, returning missing when it is blank.
func required(v string, missing error, n normalizer) (string, error) {
	if strings.TrimSpace(v) == "" {
		return "", missing
	}

	return n(v)
}

// actorRef returns nil for an unknown actor so audit columns stay NULL.
func actorRef(userID string) *string {
	if userID == "" {
		return nil
	}

	return &userID
}

var (
	notFoundErrors = []error{domain.ErrCustomerNotFound, domain.ErrVehicleNotFound}
	conflictErrors = []error{domain.ErrPhoneTaken, domain.ErrLicensePlateTaken}
	invalidErrors  = []error{
		domain.ErrFullNameRequired, domain.ErrLicensePlateRequired, domain.ErrCustomerRequired,
		phone.ErrInvalid, email.ErrInvalid, domain.ErrInvalidLicensePlate, domain.ErrInvalidYear,
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
