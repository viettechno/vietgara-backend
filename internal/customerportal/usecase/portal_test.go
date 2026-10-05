package usecase_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	customerdomain "github.com/viettechno/vietgara-backend/internal/customer/domain"
	customerusecase "github.com/viettechno/vietgara-backend/internal/customer/usecase"
	"github.com/viettechno/vietgara-backend/internal/customerportal/usecase"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

const me = "acc-binh"

var errBoom = errors.New("boom")

type accounts struct{ err error }

func (a accounts) Email(_ context.Context, accountID string) (string, error) {
	if accountID != me {
		return "", errors.New("unexpected account")
	}

	return "binh@example.com", a.err
}

// world is a fake of the other modules; scope records the scope of the
// transaction each read ran in.
type world struct {
	refs      []usecase.CustomerRef
	findErr   error
	customers map[string]*customerdomain.Customer // by id
	vehicles  map[string]*customerdomain.Vehicle  // by id
	garages   map[string]usecase.GarageInfo
	readErr   error
	scope     platformdb.Scope
	scopes    []platformdb.Scope
}

func (w *world) FindByEmail(_ context.Context, email string) ([]usecase.CustomerRef, error) {
	if email != "binh@example.com" {
		return nil, nil
	}

	return w.refs, w.findErr
}

func (w *world) check(garageID string) error {
	if w.scope.GarageID != garageID {
		return errors.New("read outside its garage scope")
	}

	return w.readErr
}

func (w *world) Get(_ context.Context, garageID, customerID string) (*customerdomain.Customer, error) {
	if err := w.check(garageID); err != nil {
		return nil, err
	}

	return w.customers[customerID], nil
}

func (w *world) ListForCustomer(_ context.Context, garageID, customerID string) ([]*customerdomain.Vehicle, error) {
	if err := w.check(garageID); err != nil {
		return nil, err
	}

	var list []*customerdomain.Vehicle

	for _, v := range w.vehicles {
		if v.GarageID == garageID && v.CustomerID == customerID {
			list = append(list, v)
		}
	}

	return list, nil
}

type vehicles struct{ *world }

func (v vehicles) Get(_ context.Context, garageID, vehicleID string) (*customerdomain.Vehicle, error) {
	if err := v.check(garageID); err != nil {
		return nil, err
	}

	vehicle, ok := v.vehicles[vehicleID]
	if !ok || vehicle.GarageID != garageID {
		return nil, customerdomain.ErrVehicleNotFound
	}

	return vehicle, nil
}

func (v vehicles) History(_ context.Context, garageID, vehicleID string) ([]customerusecase.HistoryEntry, error) {
	if err := v.check(garageID); err != nil {
		return nil, err
	}

	return []customerusecase.HistoryEntry{{Type: customerusecase.HistoryEntryQuote, EntityID: "q-" + vehicleID}}, nil
}

type garages struct{ *world }

func (g garages) Get(_ context.Context, garageID string) (usecase.GarageInfo, error) {
	if err := g.check(garageID); err != nil {
		return usecase.GarageInfo{}, err
	}

	return g.garages[garageID], nil
}

type tx struct{ *world }

func (t tx) WithTransaction(ctx context.Context, scope platformdb.Scope, fn func(context.Context) error) error {
	t.scope = scope

	t.scopes = append(t.scopes, scope)
	defer func() { t.scope = platformdb.Scope{} }()

	return fn(ctx)
}

func newWorld() *world {
	return &world{
		refs: []usecase.CustomerRef{{ID: "c-1", GarageID: "g-1"}, {ID: "c-2", GarageID: "g-2"}},
		customers: map[string]*customerdomain.Customer{
			"c-1": {ID: "c-1", GarageID: "g-1", FullName: "Tran Thi Binh"},
			"c-2": {ID: "c-2", GarageID: "g-2", FullName: "Binh Tran"},
		},
		vehicles: map[string]*customerdomain.Vehicle{
			"v-1":     {ID: "v-1", GarageID: "g-1", CustomerID: "c-1", LicensePlate: "51A-12345"},
			"v-2":     {ID: "v-2", GarageID: "g-2", CustomerID: "c-2", LicensePlate: "30K-67890"},
			"v-other": {ID: "v-other", GarageID: "g-1", CustomerID: "c-someone-else", LicensePlate: "29B-11111"},
		},
		garages: map[string]usecase.GarageInfo{
			"g-1": {Name: "Gara Zeta", Address: "12 Le Loi", Phone: "0281"},
			"g-2": {Name: "Gara Alpha"},
		},
	}
}

func portal(w *world, acc accounts) *usecase.PortalUseCase {
	return usecase.NewPortalUseCase(usecase.Deps{
		Accounts: acc, Customers: w, Vehicles: vehicles{w}, Garages: garages{w}, Tx: tx{w},
	})
}

func TestListMyVehicles_GroupsByGarageReadingEachInItsScope(t *testing.T) {
	w := newWorld()

	mine, err := portal(w, accounts{}).ListMyVehicles(context.Background(), me)

	require.NoError(t, err)
	require.Len(t, mine, 2)
	assert.Equal(t, "Gara Alpha", mine[0].Garage.Name, "by garage name")
	assert.Equal(t, "Binh Tran", mine[0].CustomerName)
	require.Len(t, mine[0].Vehicles, 1)
	assert.Equal(t, "30K-67890", mine[0].Vehicles[0].LicensePlate)
	assert.Equal(t, "g-1", mine[1].GarageID)
	assert.Equal(t, "c-1", mine[1].CustomerID)
	require.Len(t, mine[1].Vehicles, 1, "never another customer's vehicle")
	assert.Equal(t, "51A-12345", mine[1].Vehicles[0].LicensePlate)
	assert.Equal(t, []platformdb.Scope{{GarageID: "g-1", AccountID: me}, {GarageID: "g-2", AccountID: me}}, w.scopes)
}

func TestListMyVehicles_EmptyWithoutCustomerRecords(t *testing.T) {
	w := newWorld()
	w.refs = nil

	mine, err := portal(w, accounts{}).ListMyVehicles(context.Background(), me)

	require.NoError(t, err)
	assert.Empty(t, mine)
}

func TestListMyVehicles_Errors(t *testing.T) {
	w := newWorld()
	_, err := portal(w, accounts{err: errBoom}).ListMyVehicles(context.Background(), me)
	require.ErrorIs(t, err, errBoom)

	w = newWorld()
	w.findErr = errBoom
	_, err = portal(w, accounts{}).ListMyVehicles(context.Background(), me)
	require.ErrorIs(t, err, errBoom)

	w = newWorld()
	w.readErr = errBoom
	_, err = portal(w, accounts{}).ListMyVehicles(context.Background(), me)
	assert.ErrorIs(t, err, errBoom)
}

func TestGetMyVehicleHistory(t *testing.T) {
	w := newWorld()

	history, err := portal(w, accounts{}).GetMyVehicleHistory(context.Background(), me, "g-1", "v-1")

	require.NoError(t, err)
	assert.Equal(t, "q-v-1", history[0].EntityID)
	assert.Equal(t, []platformdb.Scope{{GarageID: "g-1", AccountID: me}}, w.scopes)
}

func TestGetMyVehicleHistory_IsNotFoundOutsideTheirRecords(t *testing.T) {
	cases := map[string][2]string{
		"another customer's vehicle":           {"g-1", "v-other"},
		"a garage where they are no customer":  {"g-9", "v-1"},
		"their vehicle under the wrong garage": {"g-2", "v-1"},
		"an unknown vehicle":                   {"g-1", "v-missing"},
	}

	for name, ids := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := portal(newWorld(), accounts{}).GetMyVehicleHistory(context.Background(), me, ids[0], ids[1])

			var appErr *apperror.AppError
			require.ErrorAs(t, err, &appErr)
			assert.Equal(t, apperror.CodeNotFound, appErr.Code)
		})
	}
}

func TestGetMyVehicleHistory_Errors(t *testing.T) {
	_, err := portal(newWorld(), accounts{err: errBoom}).GetMyVehicleHistory(context.Background(), me, "g-1", "v-1")
	require.ErrorIs(t, err, errBoom)

	w := newWorld()
	w.readErr = errBoom
	_, err = portal(w, accounts{}).GetMyVehicleHistory(context.Background(), me, "g-1", "v-1")
	assert.ErrorIs(t, err, errBoom)
}
