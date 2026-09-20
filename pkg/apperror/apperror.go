// Package apperror defines shared sentinel errors and a typed AppError used
// across every VietGara backend service, so gRPC handlers can map business
// errors to the correct status code without leaking raw infrastructure
// errors (e.g. Postgres errors) to API responses.
//
// See: Coding_Standards.docx Section 2.3, LLD Section 6.
package apperror

import (
	"errors"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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
	CodeInternal           Code = "INTERNAL"
)

// Sentinel errors for common business-rule violations shared across
// services. Service-specific sentinel errors live in each service's own
// domain package (e.g. identity/domain.ErrInvalidCredential).
var (
	ErrNotFound         = errors.New("resource not found")
	ErrPermissionDenied = errors.New("permission denied")
)

// AppError is the standard error type returned by every usecase in the
// codebase. Never return a raw *pgconn.PgError or similar infrastructure
// error from a usecase -- always wrap it into an AppError first.
type AppError struct {
	Code    Code
	Message string
	Err     error // underlying cause, for logging only -- never exposed to the API response
}

func (e *AppError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Err)
	}

	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *AppError) Unwrap() error { return e.Err }

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

// As is a small convenience wrapper around errors.As for extracting an
// *AppError from a wrapped error chain.
func As(err error) (*AppError, bool) {
	if appErr, ok := errors.AsType[*AppError](err); ok {
		return appErr, true
	}

	return nil, false
}

// ToGRPCError translates *AppError into a gRPC status error.
func ToGRPCError(err error) error {
	appErr, ok := As(err)
	if !ok {
		return status.Error(codes.Internal, "internal error")
	}

	switch appErr.Code {
	case CodeNotFound:
		return status.Error(codes.NotFound, appErr.Message)
	case CodeAlreadyExists:
		return status.Error(codes.AlreadyExists, appErr.Message)
	case CodeInvalidArgument:
		return status.Error(codes.InvalidArgument, appErr.Message)
	case CodePermissionDenied:
		return status.Error(codes.PermissionDenied, appErr.Message)
	case CodeUnauthenticated:
		return status.Error(codes.Unauthenticated, appErr.Message)
	case CodeFailedPrecondition:
		return status.Error(codes.FailedPrecondition, appErr.Message)
	case CodeInternal:
		return status.Error(codes.Internal, appErr.Message)
	default:
		return status.Error(codes.Internal, "internal error")
	}
}
