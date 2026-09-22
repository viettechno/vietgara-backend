package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/mocks"
	"github.com/viettechno/vietgara-backend/internal/platform/auth"
	"github.com/viettechno/vietgara-backend/internal/tenant/domain"
	"github.com/viettechno/vietgara-backend/internal/tenant/handler"
	"github.com/viettechno/vietgara-backend/internal/tenant/usecase"
)

func TestServer_RegisterHTTPRoutesStaff(t *testing.T) {
	t.Parallel()

	e := echo.New()
	(&handler.Server{}).RegisterHTTPRoutes(e.Group(""))

	registered := map[string]bool{}
	for _, route := range e.Routes() {
		registered[route.Method+" "+route.Path] = true
	}

	for _, want := range []string{"GET /staff", "POST /staff", "GET /staff/lookup"} {
		assert.Truef(t, registered[want], "expected route %s to be registered", want)
	}
}

type staffHTTPFixture struct {
	srv       *handler.Server
	garages   *mocks.MockGarageRepository
	staff     *mocks.MockGarageStaffRepository
	directory *mocks.MockAccountDirectory
}

// newStaffHTTPTestServer wires the handler to real use cases over mocked
// repositories and a mocked account directory.
func newStaffHTTPTestServer(t *testing.T) staffHTTPFixture {
	t.Helper()

	f := staffHTTPFixture{
		garages:   mocks.NewMockGarageRepository(t),
		staff:     mocks.NewMockGarageStaffRepository(t),
		directory: mocks.NewMockAccountDirectory(t),
	}
	f.srv = handler.NewServer(
		usecase.NewGarageUseCase(f.garages, f.staff, nil),
		usecase.NewStaffUseCase(f.staff, usecase.WithAccountDirectory(f.directory)),
	)

	return f
}

var (
	ownerClaims      = []auth.GarageClaim{{GarageID: "garage-1", Role: "owner"}}
	technicianClaims = []auth.GarageClaim{{GarageID: "garage-1", Role: "technician"}}
)

func TestServer_ListHTTPStaff(t *testing.T) {
	t.Parallel()

	t.Run("missing garageId returns bad request", func(t *testing.T) {
		t.Parallel()

		f := newStaffHTTPTestServer(t)
		rec := garageHTTPRequest(t, f.srv, "acc-1", nil, http.MethodGet, "/staff", "")

		require.Equal(t, http.StatusBadRequest, rec.Code)
		assert.JSONEq(t, `{"message":"garageId is required"}`, rec.Body.String())
	})

	t.Run("caller without access is forbidden", func(t *testing.T) {
		t.Parallel()

		f := newStaffHTTPTestServer(t)
		f.garages.EXPECT().FindByID(mock.Anything, "garage-1").Return(testGarage(), nil).Once()

		rec := garageHTTPRequest(t, f.srv, "acc-9", nil, http.MethodGet, "/staff?garageId=garage-1", "")

		require.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("lists staff with account details for any member", func(t *testing.T) {
		t.Parallel()

		f := newStaffHTTPTestServer(t)
		f.garages.EXPECT().FindByID(mock.Anything, "garage-1").Return(testGarage(), nil).Once()
		f.staff.EXPECT().ListByGarageID(mock.Anything, "garage-1").Return([]*domain.GarageStaff{{
			ID: "staff-1", GarageID: "garage-1", AccountID: "acc-2", Role: domain.RoleTechnician, Status: domain.StaffStatusActive,
			CreatedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), UpdatedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		}}, nil).Once()
		f.directory.EXPECT().ListByIDs(mock.Anything, []string{"acc-2"}).Return(map[string]*usecase.AccountSummary{
			"acc-2": {ID: "acc-2", FullName: "Tran Binh", Email: new("binh@example.com"), Phone: new("0912345678"), Active: true},
		}, nil).Once()

		rec := garageHTTPRequest(t, f.srv, "acc-2", technicianClaims, http.MethodGet, "/staff?garageId=garage-1", "")

		require.Equal(t, http.StatusOK, rec.Code)
		assert.JSONEq(t, `{"staff":[{"id":"staff-1","garageId":"garage-1","accountId":"acc-2","fullName":"Tran Binh","email":"binh@example.com","phone":"0912345678","role":"technician","status":"active","createdAt":"2026-01-02T03:04:05Z","updatedAt":"2026-01-02T03:04:05Z"}]}`, rec.Body.String())
	})
}

func TestServer_LookupHTTPStaff(t *testing.T) {
	t.Parallel()

	t.Run("finds an account by email", func(t *testing.T) {
		t.Parallel()

		f := newStaffHTTPTestServer(t)
		f.garages.EXPECT().FindByID(mock.Anything, "garage-1").Return(testGarage(), nil).Once()
		f.directory.EXPECT().FindByEmail(mock.Anything, "binh@example.com").Return(&usecase.AccountSummary{
			ID: "acc-2", FullName: "Tran Binh", Email: new("binh@example.com"), Active: true,
		}, nil).Once()
		f.staff.EXPECT().FindByGarageAndAccount(mock.Anything, "garage-1", "acc-2").Return(nil, nil).Once()

		rec := garageHTTPRequest(t, f.srv, "acc-1", nil, http.MethodGet, "/staff/lookup?garageId=garage-1&query=binh@example.com", "")

		require.Equal(t, http.StatusOK, rec.Code)
		assert.JSONEq(t, `{"accountId":"acc-2","fullName":"Tran Binh","email":"binh@example.com","alreadyStaff":false}`, rec.Body.String())
	})

	t.Run("finds an account by phone and flags existing staff", func(t *testing.T) {
		t.Parallel()

		f := newStaffHTTPTestServer(t)
		f.garages.EXPECT().FindByID(mock.Anything, "garage-1").Return(testGarage(), nil).Once()
		f.directory.EXPECT().FindByPhone(mock.Anything, "0912 345 678").Return(&usecase.AccountSummary{
			ID: "acc-2", FullName: "Tran Binh", Phone: new("0912345678"), Active: true,
		}, nil).Once()
		f.staff.EXPECT().FindByGarageAndAccount(mock.Anything, "garage-1", "acc-2").
			Return(&domain.GarageStaff{Status: domain.StaffStatusActive}, nil).Once()

		rec := garageHTTPRequest(t, f.srv, "acc-1", nil, http.MethodGet, "/staff/lookup?garageId=garage-1&query=0912+345+678", "")

		require.Equal(t, http.StatusOK, rec.Code)

		var payload map[string]any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
		assert.Equal(t, true, payload["alreadyStaff"])
	})

	t.Run("unknown account returns not found", func(t *testing.T) {
		t.Parallel()

		f := newStaffHTTPTestServer(t)
		f.garages.EXPECT().FindByID(mock.Anything, "garage-1").Return(testGarage(), nil).Once()
		f.directory.EXPECT().FindByEmail(mock.Anything, "nobody@example.com").Return(nil, domain.ErrAccountNotFound).Once()

		rec := garageHTTPRequest(t, f.srv, "acc-1", nil, http.MethodGet, "/staff/lookup?garageId=garage-1&query=nobody@example.com", "")

		require.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("partial identifier returns bad request", func(t *testing.T) {
		t.Parallel()

		f := newStaffHTTPTestServer(t)
		f.garages.EXPECT().FindByID(mock.Anything, "garage-1").Return(testGarage(), nil).Once()

		rec := garageHTTPRequest(t, f.srv, "acc-1", nil, http.MethodGet, "/staff/lookup?garageId=garage-1&query=bin", "")

		require.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("non-owner staff is forbidden", func(t *testing.T) {
		t.Parallel()

		f := newStaffHTTPTestServer(t)
		f.garages.EXPECT().FindByID(mock.Anything, "garage-1").Return(testGarage(), nil).Once()

		rec := garageHTTPRequest(t, f.srv, "acc-2", technicianClaims, http.MethodGet, "/staff/lookup?garageId=garage-1&query=binh@example.com", "")

		require.Equal(t, http.StatusForbidden, rec.Code)
	})
}

func TestServer_AddHTTPStaff(t *testing.T) {
	t.Parallel()

	const body = `{"garageId":"garage-1","accountId":"acc-2","role":"technician"}`

	t.Run("invalid JSON returns bad request", func(t *testing.T) {
		t.Parallel()

		f := newStaffHTTPTestServer(t)
		rec := garageHTTPRequest(t, f.srv, "acc-1", ownerClaims, http.MethodPost, "/staff", "{not-json")

		require.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("owner role cannot be invited", func(t *testing.T) {
		t.Parallel()

		f := newStaffHTTPTestServer(t)
		rec := garageHTTPRequest(t, f.srv, "acc-1", ownerClaims, http.MethodPost, "/staff", `{"garageId":"garage-1","accountId":"acc-2","role":"owner"}`)

		require.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("non-owner staff is forbidden", func(t *testing.T) {
		t.Parallel()

		f := newStaffHTTPTestServer(t)
		f.garages.EXPECT().FindByID(mock.Anything, "garage-1").Return(testGarage(), nil).Once()

		rec := garageHTTPRequest(t, f.srv, "acc-2", technicianClaims, http.MethodPost, "/staff", body)

		require.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("unknown account returns not found", func(t *testing.T) {
		t.Parallel()

		f := newStaffHTTPTestServer(t)
		f.garages.EXPECT().FindByID(mock.Anything, "garage-1").Return(testGarage(), nil).Once()
		f.directory.EXPECT().ListByIDs(mock.Anything, []string{"acc-2"}).Return(map[string]*usecase.AccountSummary{}, nil).Once()

		rec := garageHTTPRequest(t, f.srv, "acc-1", nil, http.MethodPost, "/staff", body)

		require.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("already active staff returns conflict", func(t *testing.T) {
		t.Parallel()

		f := newStaffHTTPTestServer(t)
		f.garages.EXPECT().FindByID(mock.Anything, "garage-1").Return(testGarage(), nil).Once()
		f.directory.EXPECT().ListByIDs(mock.Anything, []string{"acc-2"}).Return(map[string]*usecase.AccountSummary{"acc-2": {ID: "acc-2", Active: true}}, nil).Once()
		f.staff.EXPECT().FindByGarageAndAccount(mock.Anything, "garage-1", "acc-2").
			Return(&domain.GarageStaff{Status: domain.StaffStatusActive}, nil).Once()

		rec := garageHTTPRequest(t, f.srv, "acc-1", nil, http.MethodPost, "/staff", body)

		require.Equal(t, http.StatusConflict, rec.Code)
	})

	t.Run("owner adds an existing account", func(t *testing.T) {
		t.Parallel()

		f := newStaffHTTPTestServer(t)
		f.garages.EXPECT().FindByID(mock.Anything, "garage-1").Return(testGarage(), nil).Once()

		account := &usecase.AccountSummary{ID: "acc-2", FullName: "Tran Binh", Email: new("binh@example.com"), Active: true}
		f.directory.EXPECT().ListByIDs(mock.Anything, []string{"acc-2"}).Return(map[string]*usecase.AccountSummary{"acc-2": account}, nil).Once()
		f.staff.EXPECT().FindByGarageAndAccount(mock.Anything, "garage-1", "acc-2").Return(nil, nil).Once()

		var created *domain.GarageStaff

		f.staff.EXPECT().Create(mock.Anything, mock.MatchedBy(func(s *domain.GarageStaff) bool {
			created = s

			return s.GarageID == "garage-1" && s.AccountID == "acc-2" && s.Role == domain.RoleTechnician && s.Status == domain.StaffStatusActive
		})).Return(nil).Once()
		f.staff.EXPECT().ListByGarageID(mock.Anything, "garage-1").RunAndReturn(func(context.Context, string) ([]*domain.GarageStaff, error) {
			return []*domain.GarageStaff{created}, nil
		}).Once()
		f.directory.EXPECT().ListByIDs(mock.Anything, []string{"acc-2"}).Return(map[string]*usecase.AccountSummary{"acc-2": account}, nil).Once()

		rec := garageHTTPRequest(t, f.srv, "acc-1", nil, http.MethodPost, "/staff", body)

		require.Equal(t, http.StatusCreated, rec.Code)

		var payload map[string]any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
		assert.Equal(t, "garage-1", payload["garageId"])
		assert.Equal(t, "Tran Binh", payload["fullName"])
		assert.Equal(t, "technician", payload["role"])
	})
}
