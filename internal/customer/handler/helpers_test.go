package handler_test

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/customer/domain"
	"github.com/viettechno/vietgara-backend/internal/customer/handler"
	"github.com/viettechno/vietgara-backend/internal/customer/usecase"
	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/mocks"
	"github.com/viettechno/vietgara-backend/internal/platform/auth"
	dbpkg "github.com/viettechno/vietgara-backend/internal/platform/db"
)

const (
	garageID   = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	accountID  = "55555555-5555-5555-5555-555555555555"
	customerID = "11111111-1111-1111-1111-111111111111"
	vehicleID  = "22222222-2222-2222-2222-222222222222"
	partnerID  = "44444444-4444-4444-4444-444444444444"

	garagePath = "/garages/" + garageID
)

var (
	testTime = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	errBoom  = errors.New("boom")
)

type fixture struct {
	srv       *handler.Server
	customers *mocks.MockCustomerRepository
	vehicles  *mocks.MockVehicleRepository
	partners  *mocks.MockPartnerRepository
	members   *mocks.MockMembershipReader
}

// newFixture wires the handler to real use cases over mocked repositories;
// role is the caller's role in garageID ("" = not a member).
func newFixture(t *testing.T, role string, opts ...handler.ServerOption) fixture {
	t.Helper()

	f := fixture{
		customers: mocks.NewMockCustomerRepository(t),
		vehicles:  mocks.NewMockVehicleRepository(t),
		partners:  mocks.NewMockPartnerRepository(t),
		members:   mocks.NewMockMembershipReader(t),
	}

	clock := usecase.WithClock(func() time.Time { return testTime })
	f.srv = handler.NewServer(
		usecase.NewCustomerUseCase(f.customers, f.vehicles, clock),
		usecase.NewVehicleUseCase(f.vehicles, f.customers, clock),
		usecase.NewPartnerUseCase(f.partners),
		f.members,
		opts...,
	)

	if role == "" {
		f.members.EXPECT().RoleInGarage(mock.Anything, garageID, accountID).Return("", domain.ErrNoAccessToGarage).Maybe()
	} else {
		f.members.EXPECT().RoleInGarage(mock.Anything, garageID, accountID).Return(role, nil).Maybe()
	}

	return f
}

// do sends an authenticated request through HTTPAuth and the Echo routes.
func (f fixture) do(t *testing.T, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()

	issuer := auth.NewTokenIssuer([]byte("customer-test-key"), time.Minute, time.Hour)
	token, _, err := issuer.IssueAccessToken(accountID, nil)
	require.NoError(t, err)

	e := echo.New()
	f.srv.RegisterHTTPRoutes(e.Group(""))

	req := httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	middleware.HTTPAuth(issuer, e).ServeHTTP(rec, req)

	return rec
}

func testCustomer() *domain.Customer {
	return &domain.Customer{
		ID: customerID, GarageID: garageID, FullName: "Tran Binh", Phone: new("0912345678"), VehicleCount: 1,
		ConsentLog: []domain.ConsentRecord{{Granted: true, RecordedAt: testTime}},
		CreatedAt:  testTime, UpdatedAt: testTime,
	}
}

func testVehicle() *domain.Vehicle {
	year := 2020

	return &domain.Vehicle{
		ID: vehicleID, GarageID: garageID, CustomerID: customerID, CustomerName: "Tran Binh",
		LicensePlate: "51F-123.45", Make: new("Toyota"), Year: &year, CreatedAt: testTime, UpdatedAt: testTime,
	}
}

func testPartner() *domain.Partner {
	return &domain.Partner{
		ID: partnerID, GarageID: garageID, Type: domain.PartnerTypeSupplier, Name: "Phu Tung Sai Gon",
		Contact: domain.PartnerContact{Phone: new("0281234567")}, CreatedAt: testTime, UpdatedAt: testTime,
	}
}

// withTx returns a server option backed by a mocked pool that expects one
// transaction scoped to tenant (nil = no tenant variable).
func withTx(t *testing.T, tenant *string) handler.ServerOption {
	t.Helper()

	conn := mocks.NewMockConnPool(t)
	tx := mocks.NewMockTx(t)

	conn.EXPECT().Begin(mock.Anything).Return(tx, nil).Once()
	tx.EXPECT().Rollback(mock.Anything).Return(nil).Once()

	if tenant != nil {
		tx.EXPECT().Exec(mock.Anything, dbpkg.SetTenantSQL, []any{*tenant}).Return(pgconn.CommandTag{}, nil).Once()
		tx.EXPECT().Commit(mock.Anything).Return(nil).Once()
	}

	return handler.WithTxManager(dbpkg.NewTxManager(dbpkg.NewPool(t.Context(), conn)))
}
