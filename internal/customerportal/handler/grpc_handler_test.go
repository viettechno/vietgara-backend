package handler_test

import (
	"context"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"
	customerportalv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customerportal/v1"

	customerdomain "github.com/viettechno/vietgara-backend/internal/customer/domain"
	customerusecase "github.com/viettechno/vietgara-backend/internal/customer/usecase"
	"github.com/viettechno/vietgara-backend/internal/customerportal/handler"
	"github.com/viettechno/vietgara-backend/internal/customerportal/usecase"
	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/platform/auth"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
)

type fake struct{ emailErr error }

func (f fake) Email(context.Context, string) (string, error) { return "binh@example.com", f.emailErr }

func (fake) FindByEmail(context.Context, string) ([]usecase.CustomerRef, error) {
	return []usecase.CustomerRef{{ID: "c-1", GarageID: "g-1"}}, nil
}

func (fake) Get(_ context.Context, _, id string) (*customerdomain.Customer, error) {
	return &customerdomain.Customer{ID: id, FullName: "Tran Thi Binh"}, nil
}

func (fake) ListForUser(context.Context, string) ([]*customerdomain.UserLink, error) {
	return []*customerdomain.UserLink{{ID: "l-1", GarageID: "g-1", CustomerID: "c-1", Status: customerdomain.LinkLinked}}, nil
}

func (fake) FindForUser(context.Context, string, string) (*customerdomain.UserLink, error) {
	return &customerdomain.UserLink{ID: "l-1", GarageID: "g-1", CustomerID: "c-1", Status: customerdomain.LinkRequested}, nil
}

func (fake) Accept(context.Context, string, string) (*customerdomain.UserLink, error) {
	return &customerdomain.UserLink{ID: "l-1", GarageID: "g-1", CustomerID: "c-1", Status: customerdomain.LinkLinked}, nil
}

func (fake) Decline(context.Context, string, string) (*customerdomain.UserLink, error) {
	return &customerdomain.UserLink{ID: "l-1", GarageID: "g-1", CustomerID: "c-1", Status: customerdomain.LinkDeclined}, nil
}

func (fake) UnlinkByUser(context.Context, string, string) (*customerdomain.UserLink, error) {
	return &customerdomain.UserLink{ID: "l-1", GarageID: "g-1", CustomerID: "c-1", Status: customerdomain.LinkUnlinked}, nil
}

func (fake) AutoLink(context.Context, string, string, string) error { return nil }

type vehicles struct{}

func (vehicles) ListForCustomer(context.Context, string, string) ([]*customerdomain.Vehicle, error) {
	return []*customerdomain.Vehicle{{ID: "v-1", GarageID: "g-1", CustomerID: "c-1", LicensePlate: "51A-12345"}}, nil
}

func (vehicles) Get(_ context.Context, _, id string) (*customerdomain.Vehicle, error) {
	return &customerdomain.Vehicle{ID: id, GarageID: "g-1", CustomerID: "c-1"}, nil
}

func (vehicles) History(context.Context, string, string) ([]customerusecase.HistoryEntry, error) {
	return []customerusecase.HistoryEntry{{Type: customerusecase.HistoryEntryRepairOrder, EntityID: "ro-1", Number: "LSC-0001"}}, nil
}

type garages struct{}

func (garages) Get(context.Context, string) (usecase.GarageInfo, error) {
	return usecase.GarageInfo{Name: "Gara An Phat", Address: "12 Le Loi", Phone: "0281"}, nil
}

type passTx struct{}

func (passTx) WithTransaction(ctx context.Context, _ platformdb.Scope, fn func(context.Context) error) error {
	return fn(ctx)
}

func server(f fake) *handler.Server {
	return handler.NewServer(usecase.NewPortalUseCase(usecase.Deps{
		Users: f, Customers: f, Vehicles: vehicles{}, Garages: garages{}, Links: f, Tx: passTx{},
	}))
}

func signedIn() context.Context {
	return middleware.WithClaims(context.Background(), &auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: "user-binh"}, EmailVerified: true,
	})
}

func TestListMyVehicles(t *testing.T) {
	resp, err := server(fake{}).ListMyVehicles(signedIn(), &customerportalv1.ListMyVehiclesRequest{})

	require.NoError(t, err)
	require.Len(t, resp.GetData(), 1)
	g := resp.GetData()[0]
	assert.Equal(t, "g-1", g.GetGarageId())
	assert.Equal(t, "Gara An Phat", g.GetName())
	assert.Equal(t, "12 Le Loi", g.GetAddress())
	assert.Equal(t, "0281", g.GetPhone())
	assert.Equal(t, "c-1", g.GetCustomerId())
	assert.Equal(t, "Tran Thi Binh", g.GetCustomerName())
	require.Len(t, g.GetVehicles(), 1)
	assert.Equal(t, "51A-12345", g.GetVehicles()[0].GetLicensePlate())
}

func TestGetMyVehicleHistory(t *testing.T) {
	resp, err := server(fake{}).GetMyVehicleHistory(signedIn(), &customerportalv1.GetMyVehicleHistoryRequest{
		GarageId: "g-1", VehicleId: "v-1",
	})

	require.NoError(t, err)
	require.Len(t, resp.GetData(), 1)
	assert.Equal(t, "LSC-0001", resp.GetData()[0].GetNumber())
	assert.Equal(t, customerv1.VehicleHistoryEntryType_VEHICLE_HISTORY_ENTRY_TYPE_REPAIR_ORDER, resp.GetData()[0].GetType())

	_, err = server(fake{}).GetMyVehicleHistory(signedIn(), &customerportalv1.GetMyVehicleHistoryRequest{
		GarageId: "g-other", VehicleId: "v-1",
	})
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestErrors(t *testing.T) {
	broken := server(fake{emailErr: errCustomerPortalDBDown})

	_, err := broken.ListMyVehicles(signedIn(), &customerportalv1.ListMyVehiclesRequest{})
	assert.Equal(t, codes.Internal, status.Code(err))

	_, err = broken.GetMyVehicleHistory(signedIn(), &customerportalv1.GetMyVehicleHistoryRequest{GarageId: "g-1", VehicleId: "v-1"})
	assert.Equal(t, codes.Internal, status.Code(err))
}

func TestPolicies(t *testing.T) {
	p := handler.Policies()
	assert.Len(t, p, 6)
	assert.Equal(t, middleware.UserMethod(), p[customerportalv1.CustomerPortalService_ListMyVehicles_FullMethodName])
	assert.Equal(t, middleware.UserMethod(), p[customerportalv1.CustomerPortalService_GetMyVehicleHistory_FullMethodName])
}

func TestGarageLinks(t *testing.T) {
	list, err := server(fake{}).ListMyGarageLinks(signedIn(), &customerportalv1.ListMyGarageLinksRequest{})
	require.NoError(t, err)
	require.Len(t, list.GetData(), 1)

	l := list.GetData()[0]
	assert.Equal(t, "l-1", l.GetId())
	assert.Equal(t, "Gara An Phat", l.GetGarageName())
	assert.Equal(t, "Tran Thi Binh", l.GetCustomerName())
	assert.Equal(t, customerv1.CustomerUserLinkStatus_CUSTOMER_USER_LINK_STATUS_LINKED, l.GetStatus())

	accepted, err := server(fake{}).AcceptGarageLink(signedIn(), &customerportalv1.AcceptGarageLinkRequest{LinkId: "l-1"})
	require.NoError(t, err)
	assert.Equal(t, customerv1.CustomerUserLinkStatus_CUSTOMER_USER_LINK_STATUS_LINKED, accepted.GetLink().GetStatus())

	declined, err := server(fake{}).DeclineGarageLink(signedIn(), &customerportalv1.DeclineGarageLinkRequest{LinkId: "l-1"})
	require.NoError(t, err)
	assert.Equal(t, customerv1.CustomerUserLinkStatus_CUSTOMER_USER_LINK_STATUS_DECLINED, declined.GetLink().GetStatus())

	unlinked, err := server(fake{}).UnlinkGarageLink(signedIn(), &customerportalv1.UnlinkGarageLinkRequest{LinkId: "l-1"})
	require.NoError(t, err)
	assert.Equal(t, customerv1.CustomerUserLinkStatus_CUSTOMER_USER_LINK_STATUS_UNLINKED, unlinked.GetLink().GetStatus())
}
