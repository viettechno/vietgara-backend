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

const me = "user-binh"

var errBoom = errors.New("boom")

type users struct{ err error }

func (a users) Email(_ context.Context, userID string) (string, error) {
	if userID != me {
		return "", errUnexpectedUser
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
	// links are the user's open links; newWorld links every ref.
	links       []*customerdomain.UserLink
	linkErr     error
	autoLinked  []string
	autoLinkErr error
}

func (w *world) FindByEmail(_ context.Context, email string) ([]usecase.CustomerRef, error) {
	if email != "binh@example.com" {
		return nil, nil
	}

	return w.refs, w.findErr
}

func (w *world) check(garageID string) error {
	if w.scope.GarageID != garageID {
		return errReadOutsideGarageScope
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

	return []customerusecase.HistoryEntry{
		{Type: customerusecase.HistoryEntryQuote, EntityID: "q-" + vehicleID, Status: "sent"},
		{Type: customerusecase.HistoryEntryQuote, EntityID: "draft-" + vehicleID, Status: "draft"},
		{Type: customerusecase.HistoryEntryRepairOrder, EntityID: "ro-" + vehicleID, Status: "draft"},
	}, nil
}

// Links of the fake world: the customer records the user is linked to, and
// the calls that changed them.
func (w *world) ListForUser(_ context.Context, userID string) ([]*customerdomain.UserLink, error) {
	if w.linkErr != nil {
		return nil, w.linkErr
	}

	return w.links, nil
}

func (w *world) FindForUser(_ context.Context, _, linkID string) (*customerdomain.UserLink, error) {
	for _, l := range w.links {
		if l.ID == linkID {
			return l, nil
		}
	}

	return nil, customerdomain.ErrLinkNotFound
}

func (w *world) Accept(_ context.Context, _, linkID string) (*customerdomain.UserLink, error) {
	return w.answer(linkID, customerdomain.LinkLinked)
}

func (w *world) Decline(_ context.Context, _, linkID string) (*customerdomain.UserLink, error) {
	return w.answer(linkID, customerdomain.LinkDeclined)
}

func (w *world) UnlinkByUser(_ context.Context, _, linkID string) (*customerdomain.UserLink, error) {
	return w.answer(linkID, customerdomain.LinkUnlinked)
}

func (w *world) answer(linkID string, status customerdomain.LinkStatus) (*customerdomain.UserLink, error) {
	for _, l := range w.links {
		if l.ID == linkID {
			if l.GarageID != w.scope.GarageID {
				return nil, errReadOutsideGarageScope
			}

			l.Status = status

			return l, nil
		}
	}

	return nil, customerdomain.ErrLinkNotFound
}

func (w *world) AutoLink(_ context.Context, _, garageID, customerID string) error {
	if w.scope.GarageID != garageID {
		return errReadOutsideGarageScope
	}

	w.autoLinked = append(w.autoLinked, customerID)

	return w.autoLinkErr
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
	w := newWorldWithoutLinks()

	for _, ref := range w.refs {
		w.links = append(w.links, &customerdomain.UserLink{
			ID: "l-" + ref.ID, GarageID: ref.GarageID, CustomerID: ref.ID, Status: customerdomain.LinkLinked,
		})
	}

	return w
}

func newWorldWithoutLinks() *world {
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

func portal(w *world, user users) *usecase.PortalUseCase {
	return usecase.NewPortalUseCase(usecase.Deps{
		Users: user, Customers: w, Vehicles: vehicles{w}, Garages: garages{w}, Links: w, Tx: tx{w},
	})
}

func TestListMyVehicles_GroupsByGarageReadingEachInItsScope(t *testing.T) {
	w := newWorld()

	mine, err := portal(w, users{}).ListMyVehicles(context.Background(), me)

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
	assert.Equal(t, []string{"c-1", "c-2"}, w.autoLinked, "records with the user's e-mail are linked")
	assert.Equal(t, []platformdb.Scope{
		{GarageID: "g-1", UserID: me}, {GarageID: "g-2", UserID: me}, // linking
		{GarageID: "g-1", UserID: me}, {GarageID: "g-2", UserID: me}, // reading
	}, w.scopes)
}

func TestListMyVehicles_EmptyWithoutCustomerRecords(t *testing.T) {
	w := newWorld()
	w.refs, w.links = nil, nil

	mine, err := portal(w, users{}).ListMyVehicles(context.Background(), me)

	require.NoError(t, err)
	assert.Empty(t, mine)
}

func TestListMyVehicles_Errors(t *testing.T) {
	w := newWorld()
	_, err := portal(w, users{err: errBoom}).ListMyVehicles(context.Background(), me)
	require.ErrorIs(t, err, errBoom)

	w = newWorld()
	w.findErr = errBoom
	_, err = portal(w, users{}).ListMyVehicles(context.Background(), me)
	require.ErrorIs(t, err, errBoom)

	w = newWorld()
	w.readErr = errBoom
	_, err = portal(w, users{}).ListMyVehicles(context.Background(), me)
	assert.ErrorIs(t, err, errBoom)
}

func TestGetMyVehicleHistory(t *testing.T) {
	w := newWorld()

	history, err := portal(w, users{}).GetMyVehicleHistory(context.Background(), me, "g-1", "v-1")

	require.NoError(t, err)
	// The draft quote is left out; only quotes have drafts.
	ids := make([]string, 0, len(history))
	for _, e := range history {
		ids = append(ids, e.EntityID)
	}

	assert.Equal(t, []string{"q-v-1", "ro-v-1"}, ids)
	assert.Equal(t, []platformdb.Scope{
		{GarageID: "g-1", UserID: me}, {GarageID: "g-2", UserID: me}, {GarageID: "g-1", UserID: me},
	}, w.scopes)
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
			_, err := portal(newWorld(), users{}).GetMyVehicleHistory(context.Background(), me, ids[0], ids[1])

			var appErr *apperror.AppError
			require.ErrorAs(t, err, &appErr)
			assert.Equal(t, apperror.CodeNotFound, appErr.Code)
		})
	}
}

func TestGetMyVehicleHistory_Errors(t *testing.T) {
	_, err := portal(newWorld(), users{err: errBoom}).GetMyVehicleHistory(context.Background(), me, "g-1", "v-1")
	require.ErrorIs(t, err, errBoom)

	w := newWorld()
	w.readErr = errBoom
	_, err = portal(w, users{}).GetMyVehicleHistory(context.Background(), me, "g-1", "v-1")
	assert.ErrorIs(t, err, errBoom)
}

func TestListMyVehicles_OnlyLinkedRecords(t *testing.T) {
	w := newWorld()
	w.links[0].Status = customerdomain.LinkRequested // g-1 still waits for the user's answer

	mine, err := portal(w, users{}).ListMyVehicles(context.Background(), me)

	require.NoError(t, err)
	require.Len(t, mine, 1)
	assert.Equal(t, "g-2", mine[0].GarageID)
}

func TestListMyVehicles_LinkErrors(t *testing.T) {
	w := newWorld()
	w.autoLinkErr = errBoom
	_, err := portal(w, users{}).ListMyVehicles(context.Background(), me)
	require.ErrorIs(t, err, errBoom)

	w = newWorld()
	w.linkErr = errBoom
	_, err = portal(w, users{}).ListMyVehicles(context.Background(), me)
	require.ErrorIs(t, err, errBoom)
}

func TestGarageLinks(t *testing.T) {
	w := newWorld()
	w.links[0].Status = customerdomain.LinkRequested
	uc := portal(w, users{})

	list, err := uc.ListMyGarageLinks(context.Background(), me)
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.Equal(t, "Gara Zeta", list[0].Garage.Name)
	assert.Equal(t, "Tran Thi Binh", list[0].CustomerName)

	w.scopes = nil
	accepted, err := uc.AcceptGarageLink(context.Background(), me, "l-c-1")
	require.NoError(t, err)
	assert.Equal(t, customerdomain.LinkLinked, accepted.Link.Status)
	assert.Equal(t, "Gara Zeta", accepted.Garage.Name)
	assert.Equal(t, platformdb.Scope{GarageID: "g-1", UserID: me}, w.scopes[0], "answered in the link's garage")

	declined, err := uc.DeclineGarageLink(context.Background(), me, "l-c-2")
	require.NoError(t, err)
	assert.Equal(t, customerdomain.LinkDeclined, declined.Link.Status)

	unlinked, err := uc.UnlinkGarageLink(context.Background(), me, "l-c-1")
	require.NoError(t, err)
	assert.Equal(t, customerdomain.LinkUnlinked, unlinked.Link.Status)

	_, err = uc.AcceptGarageLink(context.Background(), me, "missing")
	require.ErrorIs(t, err, customerdomain.ErrLinkNotFound)

	w.readErr = errBoom
	_, err = uc.AcceptGarageLink(context.Background(), me, "l-c-1")
	assertAppError(t, err)

	_, err = uc.ListMyGarageLinks(context.Background(), me)
	assertAppError(t, err)

	w.readErr, w.linkErr = nil, errBoom
	_, err = uc.ListMyGarageLinks(context.Background(), me)
	require.ErrorIs(t, err, errBoom)
}

func assertAppError(t *testing.T, err error) {
	t.Helper()

	var appErr *apperror.AppError

	require.ErrorAs(t, err, &appErr)
}
