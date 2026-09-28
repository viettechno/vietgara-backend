package handler_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/mock"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/status"

	"github.com/viettechno/vietgara-backend/internal/customer/handler"
	"github.com/viettechno/vietgara-backend/internal/customer/usecase"
	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/mocks"
	"github.com/viettechno/vietgara-backend/internal/platform/auth"
)

const garageID = "11111111-1111-4111-8111-111111111111"

type env struct {
	srv       *handler.Server
	customers *mocks.MockCustomerRepository
	vehicles  *mocks.MockVehicleRepository
	partners  *mocks.MockPartnerRepository
}

func newEnv(t *testing.T) *env {
	t.Helper()

	e := &env{
		customers: mocks.NewMockCustomerRepository(t),
		vehicles:  mocks.NewMockVehicleRepository(t),
		partners:  mocks.NewMockPartnerRepository(t),
	}
	e.srv = handler.NewServer(
		usecase.NewCustomerUseCase(e.customers, e.vehicles),
		usecase.NewVehicleUseCase(e.vehicles, e.customers),
		usecase.NewPartnerUseCase(e.partners),
	)

	return e
}

func signedIn(t *testing.T) context.Context {
	t.Helper()

	claims := &auth.Claims{}
	claims.Subject = "22222222-2222-4222-8222-222222222222"

	return middleware.WithClaims(t.Context(), claims)
}

func reason(err error) string {
	for _, d := range status.Convert(err).Details() {
		if info, ok := d.(*errdetails.ErrorInfo); ok {
			return info.GetReason()
		}
	}

	return ""
}

var anyArg = mock.Anything
