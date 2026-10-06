package handler_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	repairorderv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/repairorder/v1"
	technicianv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/technician/v1"

	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/platform/auth"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/repairorder/domain"
	"github.com/viettechno/vietgara-backend/internal/technician/handler"
	"github.com/viettechno/vietgara-backend/internal/technician/usecase"
)

type memberships struct{ err error }

func (m memberships) ListForAccount(context.Context, string) ([]usecase.Membership, error) {
	return []usecase.Membership{{GarageID: "g-1", GarageName: "Gara An Phat", Active: true, CanReadRepairOrders: true}}, m.err
}

type repairOrders struct{ gotAccount string }

func (r *repairOrders) ListAssigned(_ context.Context, _, accountID string) ([]*domain.RepairOrder, error) {
	r.gotAccount = accountID

	return []*domain.RepairOrder{{
		ID: "ro-1", Number: "LSC-0001", Status: domain.StatusInProgress, VehicleLicensePlate: "51A-12345",
		CustomerName: "Tran Thi Binh", CreatedAt: time.Date(2026, 10, 5, 8, 0, 0, 0, time.UTC),
		Lines: []domain.Line{
			{ID: "l1", Description: "Oil change", Quantity: "1", Status: domain.LineInProgress, AssignedTechnicianIDs: []string{"acc-tech"}},
			{ID: "l2", Description: "Brakes", Quantity: "1", AssignedTechnicianIDs: []string{"other"}},
		},
	}}, nil
}

type passTx struct{}

func (passTx) WithTransaction(ctx context.Context, _ platformdb.Scope, fn func(context.Context) error) error {
	return fn(ctx)
}

func signedIn(accountID string) context.Context {
	return middleware.WithClaims(context.Background(), &auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: accountID}, EmailVerified: true,
	})
}

func TestListMyWork(t *testing.T) {
	orders := &repairOrders{}
	srv := handler.NewServer(usecase.NewTechnicianUseCase(usecase.Deps{
		Memberships: memberships{}, RepairOrders: orders, Tx: passTx{},
	}))

	resp, err := srv.ListMyWork(signedIn("acc-tech"), &technicianv1.ListMyWorkRequest{})

	require.NoError(t, err)
	assert.Equal(t, "acc-tech", orders.gotAccount)
	require.Len(t, resp.GetData(), 1)
	w := resp.GetData()[0]
	assert.Equal(t, "g-1", w.GetGarageId())
	assert.Equal(t, "Gara An Phat", w.GetGarageName())
	assert.Equal(t, "ro-1", w.GetRepairOrderId())
	assert.Equal(t, "LSC-0001", w.GetNumber())
	assert.Equal(t, repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_IN_PROGRESS, w.GetStatus())
	assert.Equal(t, "51A-12345", w.GetVehicleLicensePlate())
	assert.Equal(t, "Tran Thi Binh", w.GetCustomerName())
	assert.Equal(t, time.Date(2026, 10, 5, 8, 0, 0, 0, time.UTC), w.GetCreatedAt().AsTime())
	require.Len(t, w.GetLines(), 1)
	assert.Equal(t, "Oil change", w.GetLines()[0].GetDescription())
	assert.Equal(t, repairorderv1.RepairOrderLineStatus_REPAIR_ORDER_LINE_STATUS_IN_PROGRESS, w.GetLines()[0].GetStatus())
}

func TestListMyWork_Error(t *testing.T) {
	srv := handler.NewServer(usecase.NewTechnicianUseCase(usecase.Deps{
		Memberships: memberships{err: errors.New("db down")}, RepairOrders: &repairOrders{}, Tx: passTx{},
	}))

	_, err := srv.ListMyWork(signedIn("acc-tech"), &technicianv1.ListMyWorkRequest{})

	assert.Equal(t, codes.Internal, status.Code(err))
}

func TestPolicies(t *testing.T) {
	assert.Equal(t, middleware.Policies{
		technicianv1.TechnicianService_ListMyWork_FullMethodName: middleware.AccountMethod(),
	}, handler.Policies())
}
