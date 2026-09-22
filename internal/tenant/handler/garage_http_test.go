package handler_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/platform/auth"
	"github.com/viettechno/vietgara-backend/internal/tenant/domain"
	"github.com/viettechno/vietgara-backend/internal/tenant/handler"
)

func TestServer_RegisterHTTPRoutesGarages(t *testing.T) {
	t.Parallel()

	e := echo.New()
	(&handler.Server{}).RegisterHTTPRoutes(e.Group(""))

	registered := map[string]bool{}
	for _, route := range e.Routes() {
		registered[route.Method+" "+route.Path] = true
	}

	for _, want := range []string{"GET /garages", "POST /garages", "GET /garages/:id", "PATCH /garages/:id"} {
		assert.Truef(t, registered[want], "expected route %s to be registered", want)
	}
}

// garageHTTPRequest sends a request through HTTPAuth as accountID, whose token
// carries the given garage claims.
func garageHTTPRequest(t *testing.T, srv *handler.Server, accountID string, claims []auth.GarageClaim, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()

	issuer := auth.NewTokenIssuer([]byte("tenant-test-key"), time.Minute, time.Hour)
	token, _, err := issuer.IssueAccessToken(accountID, claims)
	require.NoError(t, err)

	e := echo.New()
	srv.RegisterHTTPRoutes(e.Group(""))

	req := httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	middleware.HTTPAuth(issuer, e).ServeHTTP(rec, req)

	return rec
}

func TestServer_ListHTTPGarages(t *testing.T) {
	t.Parallel()

	srv, garages, _, _ := newTestServer(t)
	garages.EXPECT().ListByAccountID(mock.Anything, "acc-1").Return([]*domain.Garage{testGarage()}, nil).Once()

	rec := garageHTTPRequest(t, srv, "acc-1", nil, http.MethodGet, "/garages", "")

	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"garages":[{"id":"garage-1","accountId":"acc-1","name":"Gara A","taxCode":"0123456789","address":"12 Nguyen Hue","logoUrl":"https://storage.example.com/logo.png","timezone":"Asia/Ho_Chi_Minh","currency":"VND","status":"GARAGE_STATUS_ACTIVE","createdAt":"2026-01-02T03:04:05Z","updatedAt":"2026-01-02T03:04:05Z"}]}`, rec.Body.String())
}

func TestServer_CreateHTTPGarage(t *testing.T) {
	t.Parallel()

	t.Run("creates garage for the caller", func(t *testing.T) {
		t.Parallel()

		srv, garages, staff, events := newTestServer(t)
		garages.EXPECT().Create(mock.Anything, mock.MatchedBy(func(g *domain.Garage) bool {
			return g.AccountID == "acc-1" && g.Name == "Gara B" && g.Currency == "VND"
		})).Return(nil).Once()
		staff.EXPECT().Create(mock.Anything, mock.Anything).Return(nil).Once()
		events.EXPECT().Publish(mock.Anything, "garage.created", mock.Anything, mock.Anything).Return(nil).Once()

		rec := garageHTTPRequest(t, srv, "acc-1", nil, http.MethodPost, "/garages", `{"name":"Gara B","address":"1 Le Loi"}`)

		require.Equal(t, http.StatusCreated, rec.Code)
		assert.Contains(t, rec.Body.String(), `"name":"Gara B"`)
	})

	tests := []struct {
		name string
		body string
	}{
		{name: "invalid JSON", body: `{`},
		{name: "missing name", body: `{"address":"1 Le Loi"}`},
		{name: "blank name", body: `{"name":"  "}`},
		{name: "bad currency", body: `{"name":"Gara B","currency":"VN"}`},
		{name: "tax code too long", body: `{"name":"Gara B","taxCode":"0123456789012345678901"}`},
	}

	for _, tc := range tests {
		t.Run(tc.name+" returns bad request", func(t *testing.T) {
			t.Parallel()

			srv, _, _, _ := newTestServer(t)
			rec := garageHTTPRequest(t, srv, "acc-1", nil, http.MethodPost, "/garages", tc.body)

			require.Equal(t, http.StatusBadRequest, rec.Code)
			assert.Contains(t, rec.Body.String(), `"message"`)
		})
	}
}

func TestServer_GetHTTPGarage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		accountID  string
		claims     []auth.GarageClaim
		wantStatus int
	}{
		{name: "owning account", accountID: "acc-1", wantStatus: http.StatusOK},
		{name: "staff with garage in token", accountID: "acc-2", claims: []auth.GarageClaim{{GarageID: "garage-1", Role: "technician"}}, wantStatus: http.StatusOK},
		{name: "unrelated account", accountID: "acc-3", wantStatus: http.StatusForbidden},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			srv, garages, _, _ := newTestServer(t)
			garages.EXPECT().FindByID(mock.Anything, "garage-1").Return(testGarage(), nil).Once()

			rec := garageHTTPRequest(t, srv, tc.accountID, tc.claims, http.MethodGet, "/garages/garage-1", "")

			assert.Equal(t, tc.wantStatus, rec.Code)
		})
	}

	t.Run("unknown garage returns not found", func(t *testing.T) {
		t.Parallel()

		srv, garages, _, _ := newTestServer(t)
		garages.EXPECT().FindByID(mock.Anything, "missing").Return(nil, domain.ErrGarageNotFound).Once()

		rec := garageHTTPRequest(t, srv, "acc-1", nil, http.MethodGet, "/garages/missing", "")

		assert.Equal(t, http.StatusNotFound, rec.Code)
	})
}

func TestServer_UpdateHTTPGarage(t *testing.T) {
	t.Parallel()

	t.Run("owning account updates garage", func(t *testing.T) {
		t.Parallel()

		srv, garages, _, _ := newTestServer(t)
		garages.EXPECT().FindByID(mock.Anything, "garage-1").Return(testGarage(), nil).Twice()
		garages.EXPECT().Update(mock.Anything, mock.MatchedBy(func(g *domain.Garage) bool {
			return g.Name == "Renamed" && g.Currency == "VND"
		})).Return(nil).Once()

		rec := garageHTTPRequest(t, srv, "acc-1", nil, http.MethodPatch, "/garages/garage-1", `{"name":"Renamed"}`)

		require.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), `"name":"Renamed"`)
	})

	t.Run("staff who is not an owner is forbidden", func(t *testing.T) {
		t.Parallel()

		srv, garages, _, _ := newTestServer(t)
		garages.EXPECT().FindByID(mock.Anything, "garage-1").Return(testGarage(), nil).Once()

		rec := garageHTTPRequest(t, srv, "acc-2", []auth.GarageClaim{{GarageID: "garage-1", Role: "technician"}}, http.MethodPatch, "/garages/garage-1", `{"name":"Renamed"}`)

		assert.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("blank name returns bad request", func(t *testing.T) {
		t.Parallel()

		srv := handler.NewServer(nil, nil)
		rec := garageHTTPRequest(t, srv, "acc-1", nil, http.MethodPatch, "/garages/garage-1", `{"name":""}`)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("owning account deactivates and reactivates a garage", func(t *testing.T) {
		t.Parallel()

		for _, tc := range []struct {
			body string
			want string
		}{
			{body: `{"status":"inactive"}`, want: `"status":"GARAGE_STATUS_INACTIVE"`},
			{body: `{"status":"active"}`, want: `"status":"GARAGE_STATUS_ACTIVE"`},
		} {
			srv, garages, _, _ := newTestServer(t)
			garages.EXPECT().FindByID(mock.Anything, "garage-1").Return(testGarage(), nil).Twice()
			garages.EXPECT().Update(mock.Anything, mock.Anything).Return(nil).Once()

			rec := garageHTTPRequest(t, srv, "acc-1", nil, http.MethodPatch, "/garages/garage-1", tc.body)

			require.Equal(t, http.StatusOK, rec.Code)
			assert.Contains(t, rec.Body.String(), tc.want)
		}
	})

	t.Run("invalid status returns bad request", func(t *testing.T) {
		t.Parallel()

		srv := handler.NewServer(nil, nil)
		rec := garageHTTPRequest(t, srv, "acc-1", nil, http.MethodPatch, "/garages/garage-1", `{"status":"archived"}`)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
}
