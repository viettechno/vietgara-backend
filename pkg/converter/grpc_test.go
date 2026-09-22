package converter_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"

	"github.com/viettechno/vietgara-backend/pkg/converter"
)

func TestGRPCCodeToHTTPStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		code codes.Code
		want int
	}{
		{name: "OK", code: codes.OK, want: http.StatusOK},
		{name: "Canceled", code: codes.Canceled, want: http.StatusRequestTimeout},
		{name: "Unknown", code: codes.Unknown, want: http.StatusInternalServerError},
		{name: "InvalidArgument", code: codes.InvalidArgument, want: http.StatusBadRequest},
		{name: "DeadlineExceeded", code: codes.DeadlineExceeded, want: http.StatusGatewayTimeout},
		{name: "NotFound", code: codes.NotFound, want: http.StatusNotFound},
		{name: "PermissionDenied", code: codes.PermissionDenied, want: http.StatusForbidden},
		{name: "ResourceExhausted", code: codes.ResourceExhausted, want: http.StatusTooManyRequests},
		{name: "FailedPrecondition", code: codes.FailedPrecondition, want: http.StatusPreconditionFailed},
		{name: "Aborted", code: codes.Aborted, want: http.StatusConflict},
		{name: "OutOfRange", code: codes.OutOfRange, want: http.StatusBadRequest},
		{name: "Unimplemented", code: codes.Unimplemented, want: http.StatusNotImplemented},
		{name: "Internal", code: codes.Internal, want: http.StatusInternalServerError},
		{name: "Unavailable", code: codes.Unavailable, want: http.StatusServiceUnavailable},
		{name: "DataLoss", code: codes.DataLoss, want: http.StatusInternalServerError},
		{name: "Unauthenticated", code: codes.Unauthenticated, want: http.StatusUnauthorized},
		{name: "AlreadyExists", code: codes.AlreadyExists, want: http.StatusConflict},
		{name: "unknown code", code: codes.Code(99), want: http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, converter.GRPCCodeToHTTPStatus(tt.code))
		})
	}
}
