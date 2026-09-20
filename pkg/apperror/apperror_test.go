package apperror_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestAppError_Error(t *testing.T) {
	tests := []struct {
		name string
		err  *apperror.AppError
		want string
	}{
		{
			name: "without cause",
			err:  &apperror.AppError{Code: apperror.CodeNotFound, Message: "garage not found"},
			want: "NOT_FOUND: garage not found",
		},
		{
			name: "with cause",
			err:  &apperror.AppError{Code: apperror.CodeInternal, Message: "create garage", Err: errors.New("db down")},
			want: "INTERNAL: create garage: db down",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Fatalf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAppError_Unwrap(t *testing.T) {
	base := errors.New("db failure")
	err := &apperror.AppError{Code: apperror.CodeInternal, Message: "create garage", Err: base}

	if got := err.Unwrap(); !errors.Is(got, base) {
		t.Fatalf("Unwrap() = %v, want %v", got, base)
	}
}

func TestNew(t *testing.T) {
	err := apperror.New(apperror.CodeInvalidArgument, "name is required")
	if err == nil {
		t.Fatal("New() returned nil")
	}

	if err.Code != apperror.CodeInvalidArgument {
		t.Fatalf("Code = %q, want %q", err.Code, apperror.CodeInvalidArgument)
	}

	if err.Message != "name is required" {
		t.Fatalf("Message = %q, want %q", err.Message, "name is required")
	}

	if err.Err != nil {
		t.Fatalf("Err = %v, want nil", err.Err)
	}
}

func TestWrap(t *testing.T) {
	base := errors.New("resource missing")

	err := apperror.Wrap(apperror.CodeNotFound, "garage not found", base)
	if err == nil {
		t.Fatal("Wrap() returned nil")
	}

	if err.Code != apperror.CodeNotFound {
		t.Fatalf("Code = %q, want %q", err.Code, apperror.CodeNotFound)
	}

	if err.Message != "garage not found" {
		t.Fatalf("Message = %q, want %q", err.Message, "garage not found")
	}

	if !errors.Is(err, base) {
		t.Fatalf("Wrap() did not preserve wrapped error: %v", base)
	}

	if !errors.Is(err.Err, base) {
		t.Fatalf("Err = %v, want %v", err.Err, base)
	}
}

func TestAs(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		wantOK  bool
		wantMsg string
	}{
		{
			name:    "wrapped app error",
			err:     apperror.Wrap(apperror.CodePermissionDenied, "forbidden", apperror.ErrPermissionDenied),
			wantOK:  true,
			wantMsg: "forbidden",
		},
		{
			name:    "plain error",
			err:     errors.New("plain error"),
			wantOK:  false,
			wantMsg: "",
		},
		{
			name:    "app error without wrap",
			err:     apperror.New(apperror.CodeUnauthenticated, "missing token"),
			wantOK:  true,
			wantMsg: "missing token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := apperror.As(tt.err)
			if ok != tt.wantOK {
				t.Fatalf("As() ok = %v, want %v", ok, tt.wantOK)
			}

			if !ok {
				return
			}

			if got.Message != tt.wantMsg {
				t.Fatalf("As().Message = %q, want %q", got.Message, tt.wantMsg)
			}
		})
	}
}

func TestToGRPCError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		err      error
		wantCode codes.Code
	}{
		{name: "not found", err: apperror.New(apperror.CodeNotFound, "missing"), wantCode: codes.NotFound},
		{name: "already exists", err: apperror.New(apperror.CodeAlreadyExists, "dup"), wantCode: codes.AlreadyExists},
		{name: "invalid argument", err: apperror.New(apperror.CodeInvalidArgument, "bad"), wantCode: codes.InvalidArgument},
		{name: "permission denied", err: apperror.New(apperror.CodePermissionDenied, "denied"), wantCode: codes.PermissionDenied},
		{name: "unauthenticated", err: apperror.New(apperror.CodeUnauthenticated, "who"), wantCode: codes.Unauthenticated},
		{name: "failed precondition", err: apperror.New(apperror.CodeFailedPrecondition, "precond"), wantCode: codes.FailedPrecondition},
		{name: "internal", err: apperror.New(apperror.CodeInternal, "boom"), wantCode: codes.Internal},
		{name: "unknown app error code", err: apperror.New(apperror.Code("SOMETHING_ELSE"), "boom"), wantCode: codes.Internal},
		{name: "non app error", err: errors.New("plain error"), wantCode: codes.Internal},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := apperror.ToGRPCError(tc.err)
			require.Error(t, err)
			assert.Equal(t, tc.wantCode, status.Code(err))
		})
	}
}
