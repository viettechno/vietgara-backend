// Package apperror defines shared sentinel errors and a typed AppError used
// across every VietGara backend service, so gRPC handlers can map business
// errors to the correct status code without leaking raw infrastructure
// errors (e.g. Postgres errors) to API responses.
//
// Every AppError carries a stable Reason (e.g. EMAIL_NOT_VERIFIED) that
// travels as a google.rpc.ErrorInfo status detail and becomes the "code" of
// the REST error body (API Specification Section 1.1); clients translate
// it. Field problems travel as a google.rpc.BadRequest detail.
//
// See: Coding_Standards.docx Section 2.3, LLD Section 6.
package apperror

import (
	"errors"
	"fmt"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/protoadapt"
)

// Code is a stable, machine-readable error category independent of the
// underlying transport (gRPC status code, HTTP status code are mapped from
// this at the handler layer).
type Code string

// Error categories shared across every VietGara backend service.
const (
	CodeNotFound           Code = "NOT_FOUND"
	CodeAlreadyExists      Code = "ALREADY_EXISTS"
	CodeInvalidArgument    Code = "INVALID_ARGUMENT"
	CodeUnauthenticated    Code = "UNAUTHENTICATED"
	CodePermissionDenied   Code = "PERMISSION_DENIED"
	CodeFailedPrecondition Code = "FAILED_PRECONDITION"
	CodeResourceExhausted  Code = "RESOURCE_EXHAUSTED"
	CodeInternal           Code = "INTERNAL"
)

// ErrorDomain is the google.rpc.ErrorInfo domain of VietGara errors: the
// production API host.
const ErrorDomain = "vietgara-api.viettechno.com"

// Reasons shared across modules. Module-specific reasons (OTP_EXPIRED,
// ROLE_IN_USE, ...) are declared next to the module's errors.
const (
	ReasonValidation         = "VALIDATION_ERROR"
	ReasonNotFound           = "NOT_FOUND"
	ReasonAlreadyExists      = "ALREADY_EXISTS"
	ReasonUnauthenticated    = "UNAUTHENTICATED"
	ReasonPermissionDenied   = "PERMISSION_DENIED"
	ReasonFailedPrecondition = "FAILED_PRECONDITION"
	ReasonRateLimited        = "RATE_LIMITED"
	ReasonInternal           = "INTERNAL"
)

// Field issues used in FieldViolation.Issue.
const (
	IssueRequired = "required"
	IssueInvalid  = "invalid"
	IssueTooLong  = "too_long"
	IssueTooShort = "too_short"
	IssueTaken    = "taken"
)

// Sentinel errors for common business-rule violations shared across
// services. Service-specific sentinel errors live in each service's own
// domain package (e.g. identity/domain.ErrInvalidCredential).
var (
	ErrNotFound         = errors.New("resource not found")
	ErrPermissionDenied = errors.New("permission denied")
)

// FieldViolation is one invalid request field.
type FieldViolation struct {
	// Field is the JSON name, e.g. "email" or "garage.name".
	Field string
	// Issue is machine-readable: required, invalid, too_long, taken, ...
	Issue string
}

// AppError is the standard error type returned by every usecase in the
// codebase. Never return a raw *pgconn.PgError or similar infrastructure
// error from a usecase -- always wrap it into an AppError first.
type AppError struct {
	Code Code
	// Reason is the client-facing error code; defaults from Code.
	Reason  string
	Message string
	Details []FieldViolation
	Err     error // underlying cause, for logging only -- never exposed to the API response
}

func (e *AppError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Err)
	}

	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *AppError) Unwrap() error { return e.Err }

// WithReason sets the client-facing reason and returns e.
func (e *AppError) WithReason(reason string) *AppError {
	e.Reason = reason

	return e
}

// WithField adds a field violation and returns e.
func (e *AppError) WithField(field, issue string) *AppError {
	e.Details = append(e.Details, FieldViolation{Field: field, Issue: issue})

	return e
}

// ReasonOrDefault returns Reason, or the default reason of Code.
func (e *AppError) ReasonOrDefault() string {
	if e.Reason != "" {
		return e.Reason
	}

	return defaultReason(e.Code)
}

// New builds an AppError with the given code and message and no underlying
// cause.
func New(code Code, message string) *AppError {
	return &AppError{Code: code, Message: message}
}

// Wrap builds an AppError that wraps err as the underlying cause. The cause
// is kept for logging only and is never exposed to the API response.
func Wrap(code Code, message string, err error) *AppError {
	return &AppError{Code: code, Message: message, Err: err}
}

// Validation builds an INVALID_ARGUMENT/VALIDATION_ERROR for one field.
func Validation(field, issue, message string) *AppError {
	return New(CodeInvalidArgument, message).WithReason(ReasonValidation).WithField(field, issue)
}

// Internal wraps an unexpected error; op names the failed operation.
func Internal(op string, err error) *AppError {
	return Wrap(CodeInternal, op, err)
}

// As is a small convenience wrapper around errors.As for extracting an
// *AppError from a wrapped error chain.
func As(err error) (*AppError, bool) {
	if appErr, ok := errors.AsType[*AppError](err); ok {
		return appErr, true
	}

	return nil, false
}

// ReasonOf returns the reason of an *AppError in err's chain, or "".
func ReasonOf(err error) string {
	if appErr, ok := As(err); ok {
		return appErr.ReasonOrDefault()
	}

	return ""
}

var grpcCodes = map[Code]codes.Code{
	CodeNotFound:           codes.NotFound,
	CodeAlreadyExists:      codes.AlreadyExists,
	CodeInvalidArgument:    codes.InvalidArgument,
	CodePermissionDenied:   codes.PermissionDenied,
	CodeUnauthenticated:    codes.Unauthenticated,
	CodeFailedPrecondition: codes.FailedPrecondition,
	CodeResourceExhausted:  codes.ResourceExhausted,
	CodeInternal:           codes.Internal,
}

func defaultReason(code Code) string {
	switch code {
	case CodeNotFound:
		return ReasonNotFound
	case CodeAlreadyExists:
		return ReasonAlreadyExists
	case CodeInvalidArgument:
		return ReasonValidation
	case CodePermissionDenied:
		return ReasonPermissionDenied
	case CodeUnauthenticated:
		return ReasonUnauthenticated
	case CodeFailedPrecondition:
		return ReasonFailedPrecondition
	case CodeResourceExhausted:
		return ReasonRateLimited
	case CodeInternal:
		return ReasonInternal
	default:
		return ReasonInternal
	}
}

// ToGRPCError translates *AppError into a gRPC status error carrying the
// reason (ErrorInfo) and field violations (BadRequest). Errors that already
// are gRPC statuses pass through; anything else is an opaque internal error.
func ToGRPCError(err error) error {
	appErr, ok := As(err)
	if !ok {
		if _, isStatus := status.FromError(err); isStatus {
			return err
		}

		return &internalError{cause: err}
	}

	code, known := grpcCodes[appErr.Code]
	if !known || code == codes.Internal {
		return &internalError{cause: err}
	}

	st := status.New(code, appErr.Message)

	details := []protoadapt.MessageV1{&errdetails.ErrorInfo{Reason: appErr.ReasonOrDefault(), Domain: ErrorDomain}}
	if len(appErr.Details) > 0 {
		violations := make([]*errdetails.BadRequest_FieldViolation, 0, len(appErr.Details))
		for _, d := range appErr.Details {
			violations = append(violations, &errdetails.BadRequest_FieldViolation{Field: d.Field, Description: d.Issue})
		}

		details = append(details, &errdetails.BadRequest{FieldViolations: violations})
	}

	withDetails, detailErr := st.WithDetails(details...)
	if detailErr != nil {
		return st.Err()
	}

	return withDetails.Err()
}

// internalError is the gRPC error of an unexpected failure. Clients receive
// an opaque Internal status; the cause stays on the error, where
// InternalCause finds it for the request log (it was never logged before).
type internalError struct{ cause error }

func (e *internalError) Error() string { return "internal error" }

func (e *internalError) Unwrap() error { return e.cause }

// GRPCStatus implements the interface gRPC uses to answer with the status.
func (e *internalError) GRPCStatus() *status.Status {
	return status.New(codes.Internal, "internal error")
}

// InternalCause returns the cause of an error that ToGRPCError turned into an
// opaque Internal status, or nil for any other error.
func InternalCause(err error) error {
	var internal *internalError
	if errors.As(err, &internal) {
		return internal.cause
	}

	return nil
}
