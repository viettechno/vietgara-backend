package middleware_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/viettechno/vietgara-backend/internal/platform/auth"
)

const (
	methodPublic   = "/test.v1.Svc/Public"
	methodUser     = "/test.v1.Svc/GetUser"
	methodGarage   = "/test.v1.Svc/UpdateGarage"
	methodGarageRO = "/test.v1.Svc/ListThings"
	garageID       = "11111111-1111-4111-8111-111111111111"
)

var issuer = auth.NewTokenIssuer([]byte("test-key"), time.Minute, time.Hour)

func token(t *testing.T, id auth.Identity) string {
	t.Helper()

	signed, _, err := issuer.IssueAccessToken(id)
	require.NoError(t, err)

	return signed
}

func withToken(ctx context.Context, tok string) context.Context {
	return metadata.NewIncomingContext(ctx, metadata.Pairs("authorization", "Bearer "+tok))
}

func info(method string) *grpc.UnaryServerInfo {
	return &grpc.UnaryServerInfo{FullMethod: method}
}

// okHandler records that it ran and returns "ok".
func okHandler(ran *bool) grpc.UnaryHandler {
	return func(context.Context, any) (any, error) {
		*ran = true

		return "ok", nil
	}
}

func reasonOf(err error) string {
	for _, d := range status.Convert(err).Details() {
		if info, ok := d.(*errdetails.ErrorInfo); ok {
			return info.GetReason()
		}
	}

	return ""
}

func requireCode(t *testing.T, err error, code codes.Code, reason string) {
	t.Helper()
	require.Equal(t, code, status.Code(err), err)
	require.Equal(t, reason, reasonOf(err))
}

type garageReq struct{ id string }

func (r garageReq) GetGarageId() string { return r.id }
