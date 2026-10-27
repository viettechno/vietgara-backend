package apperror_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

func TestAppError(t *testing.T) {
	t.Parallel()

	cause := errApperrorDBDown

	withCause := apperror.Wrap(apperror.CodeInternal, "load", cause)
	assert.Equal(t, "INTERNAL: load: db down", withCause.Error())
	require.ErrorIs(t, withCause, cause)

	plain := apperror.New(apperror.CodeNotFound, "missing")
	assert.Equal(t, "NOT_FOUND: missing", plain.Error())
	assert.Equal(t, apperror.ReasonNotFound, plain.ReasonOrDefault())
	assert.Equal(t, "CUSTOM", plain.WithReason("CUSTOM").ReasonOrDefault())

	validation := apperror.Validation("email", apperror.IssueInvalid, "email is invalid")
	assert.Equal(t, apperror.ReasonValidation, validation.ReasonOrDefault())
	assert.Equal(t, []apperror.FieldViolation{{Field: "email", Issue: "invalid"}}, validation.Details)

	assert.Equal(t, apperror.CodeInternal, apperror.Internal("op", cause).Code)

	wrapped := fmt.Errorf("outer: %w", validation)
	got, ok := apperror.As(wrapped)
	require.True(t, ok)
	assert.Same(t, validation, got)

	_, ok = apperror.As(cause)
	assert.False(t, ok)

	assert.Equal(t, apperror.ReasonValidation, apperror.ReasonOf(wrapped))
	assert.Empty(t, apperror.ReasonOf(cause))
}

func TestDefaultReasons(t *testing.T) {
	t.Parallel()

	tests := map[apperror.Code]string{
		apperror.CodeNotFound:           apperror.ReasonNotFound,
		apperror.CodeAlreadyExists:      apperror.ReasonAlreadyExists,
		apperror.CodeInvalidArgument:    apperror.ReasonValidation,
		apperror.CodePermissionDenied:   apperror.ReasonPermissionDenied,
		apperror.CodeUnauthenticated:    apperror.ReasonUnauthenticated,
		apperror.CodeFailedPrecondition: apperror.ReasonFailedPrecondition,
		apperror.CodeResourceExhausted:  apperror.ReasonRateLimited,
		apperror.CodeInternal:           apperror.ReasonInternal,
		apperror.Code("OTHER"):          apperror.ReasonInternal,
	}

	for code, want := range tests {
		assert.Equal(t, want, apperror.New(code, "x").ReasonOrDefault(), code)
	}
}

func TestToGRPCError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		wantCode   codes.Code
		wantMsg    string
		wantReason string
		wantFields int
	}{
		{name: "not found", err: apperror.New(apperror.CodeNotFound, "gone"), wantCode: codes.NotFound, wantMsg: "gone", wantReason: "NOT_FOUND"},
		{name: "already exists", err: apperror.New(apperror.CodeAlreadyExists, "dup"), wantCode: codes.AlreadyExists, wantMsg: "dup", wantReason: "ALREADY_EXISTS"},
		{name: "validation with fields", err: apperror.Validation("a", "required", "bad").WithField("b", "invalid"), wantCode: codes.InvalidArgument, wantMsg: "bad", wantReason: "VALIDATION_ERROR", wantFields: 2},
		{name: "permission denied", err: apperror.New(apperror.CodePermissionDenied, "no"), wantCode: codes.PermissionDenied, wantMsg: "no", wantReason: "PERMISSION_DENIED"},
		{name: "unauthenticated", err: apperror.New(apperror.CodeUnauthenticated, "who"), wantCode: codes.Unauthenticated, wantMsg: "who", wantReason: "UNAUTHENTICATED"},
		{name: "failed precondition with reason", err: apperror.New(apperror.CodeFailedPrecondition, "limit").WithReason("PLAN_LIMIT_REACHED"), wantCode: codes.FailedPrecondition, wantMsg: "limit", wantReason: "PLAN_LIMIT_REACHED"},
		{name: "resource exhausted", err: apperror.New(apperror.CodeResourceExhausted, "slow"), wantCode: codes.ResourceExhausted, wantMsg: "slow", wantReason: "RATE_LIMITED"},
		{name: "internal hides details", err: apperror.Wrap(apperror.CodeInternal, "secret", errApperrorPostgres), wantCode: codes.Internal, wantMsg: "internal error"},
		{name: "unknown code", err: apperror.New(apperror.Code("WEIRD"), "x"), wantCode: codes.Internal, wantMsg: "internal error"},
		{name: "plain error", err: errApperrorRaw, wantCode: codes.Internal, wantMsg: "internal error"},
		{name: "status passes through", err: status.Error(codes.Unavailable, "later"), wantCode: codes.Unavailable, wantMsg: "later"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			st := status.Convert(apperror.ToGRPCError(tt.err))
			assert.Equal(t, tt.wantCode, st.Code())
			assert.Equal(t, tt.wantMsg, st.Message())

			var (
				reason string
				fields int
			)

			for _, d := range st.Details() {
				switch detail := d.(type) {
				case *errdetails.ErrorInfo:
					reason = detail.GetReason()
					assert.Equal(t, apperror.ErrorDomain, detail.GetDomain())
				case *errdetails.BadRequest:
					fields = len(detail.GetFieldViolations())
				}
			}

			assert.Equal(t, tt.wantReason, reason)
			assert.Equal(t, tt.wantFields, fields)
		})
	}
}

func TestInternalCause(t *testing.T) {
	t.Parallel()

	for name, err := range map[string]error{
		"an internal app error": apperror.ToGRPCError(apperror.Internal("load", errApperrorDBDown)),
		"an unknown error":      apperror.ToGRPCError(errApperrorDBDown),
	} {
		assert.Equal(t, codes.Internal, status.Code(err), name)
		assert.Equal(t, "internal error", status.Convert(err).Message(), "the client sees no cause: %s", name)
		require.ErrorIs(t, err, errApperrorDBDown, name)
		require.ErrorIs(t, apperror.InternalCause(err), errApperrorDBDown, name)
	}

	assert.NoError(t, apperror.InternalCause(apperror.ToGRPCError(apperror.New(apperror.CodeNotFound, "missing"))))
	assert.NoError(t, apperror.InternalCause(nil))
}
