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
		code codes.Code
		want int
	}{
		{codes.OK, http.StatusOK},
		{codes.InvalidArgument, http.StatusBadRequest},
		{codes.NotFound, http.StatusNotFound},
		{codes.PermissionDenied, http.StatusForbidden},
		{codes.ResourceExhausted, http.StatusTooManyRequests},
		{codes.FailedPrecondition, http.StatusConflict},
		{codes.Unimplemented, http.StatusMethodNotAllowed},
		{codes.Unauthenticated, http.StatusUnauthorized},
		{codes.AlreadyExists, http.StatusConflict},
		{codes.Internal, http.StatusInternalServerError},
		{codes.Code(99), http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.code.String(), func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, converter.GRPCCodeToHTTPStatus(tt.code))
		})
	}
}
