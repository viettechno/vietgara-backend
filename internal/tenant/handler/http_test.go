package handler_test

import (
	"bytes"
	"encoding/json"
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
	"github.com/viettechno/vietgara-backend/internal/mocks"
	"github.com/viettechno/vietgara-backend/internal/platform/auth"
	"github.com/viettechno/vietgara-backend/internal/tenant/domain"
	"github.com/viettechno/vietgara-backend/internal/tenant/handler"
	"github.com/viettechno/vietgara-backend/internal/tenant/usecase"
)

func TestServer_RegisterHTTPRoutesStaff(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		method string
		path   string
	}{
		{name: "list staff route", method: http.MethodGet, path: "/staff"},
		{name: "add staff route", method: http.MethodPost, path: "/staff"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			e := echo.New()
			srv := &handler.Server{}
			srv.RegisterHTTPRoutes(e.Group(""))

			found := false

			for _, route := range e.Routes() {
				if route.Method == tc.method && route.Path == tc.path {
					found = true

					break
				}
			}

			require.Truef(t, found, "expected route %s %s to be registered", tc.method, tc.path)
		})
	}
}

func TestServer_ListHTTPStaff(t *testing.T) {
	t.Parallel()

	t.Run("missing garageId returns bad request", func(t *testing.T) {
		t.Parallel()

		e := echo.New()
		srv := &handler.Server{}
		srv.RegisterHTTPRoutes(e.Group(""))

		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/staff", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		require.Equal(t, http.StatusBadRequest, rec.Code)

		var payload map[string]string
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
		assert.Equal(t, "garageId is required", payload["message"])
	})

	t.Run("without access returns forbidden", func(t *testing.T) {
		t.Parallel()

		e := echo.New()
		srv := &handler.Server{}
		srv.RegisterHTTPRoutes(e.Group(""))

		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/staff?garageId=garage-1", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		require.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("lists staff for allowed garage", func(t *testing.T) {
		t.Parallel()

		srv, staffRepo := newStaffHTTPTestServer(t)
		staffRepo.EXPECT().ListByGarageID(mock.Anything, "garage-1").Return([]*domain.GarageStaff{{
			ID:        "staff-1",
			GarageID:  "garage-1",
			AccountID: "acc-1",
			Role:      domain.RoleOwner,
			Status:    domain.StaffStatusActive,
			CreatedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
			UpdatedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		}}, nil).Once()

		issuer := auth.NewTokenIssuer([]byte("tenant-test-key"), time.Minute, time.Hour)
		token, _, err := issuer.IssueAccessToken("acc-1", []auth.GarageClaim{{GarageID: "garage-1", Role: "owner"}})
		require.NoError(t, err)

		e := echo.New()
		srv.RegisterHTTPRoutes(e.Group(""))

		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/staff?garageId=garage-1", nil)
		req.Header.Set("Authorization", "Bearer "+token)

		rec := httptest.NewRecorder()
		middleware.HTTPAuth(issuer, e).ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

		var payload map[string]any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))

		staffList, ok := payload["staff"].([]any)
		require.True(t, ok)
		require.Len(t, staffList, 1)
	})
}

func TestServer_AddHTTPStaff(t *testing.T) {
	t.Parallel()

	t.Run("invalid JSON returns bad request", func(t *testing.T) {
		t.Parallel()

		e := echo.New()
		srv := &handler.Server{}
		srv.RegisterHTTPRoutes(e.Group(""))

		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/staff", strings.NewReader("{not-json"))
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		require.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("without access returns forbidden", func(t *testing.T) {
		t.Parallel()

		e := echo.New()
		srv := &handler.Server{}
		srv.RegisterHTTPRoutes(e.Group(""))

		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/staff", bytes.NewBufferString(`{"garageId":"garage-1","accountId":"acc-1","role":"owner"}`))
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		require.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("adds staff when allowed", func(t *testing.T) {
		t.Parallel()

		srv, staffRepo := newStaffHTTPTestServer(t)
		staffRepo.EXPECT().FindByGarageAndAccount(mock.Anything, "garage-1", "acc-1").Return(nil, nil).Once()
		staffRepo.EXPECT().Create(mock.Anything, mock.MatchedBy(func(s *domain.GarageStaff) bool {
			return s.GarageID == "garage-1" && s.AccountID == "acc-1" && s.Role == domain.RoleOwner && s.Status == domain.StaffStatusActive
		})).Return(nil).Once()

		issuer := auth.NewTokenIssuer([]byte("tenant-test-key"), time.Minute, time.Hour)
		token, _, err := issuer.IssueAccessToken("acc-1", []auth.GarageClaim{{GarageID: "garage-1", Role: "owner"}})
		require.NoError(t, err)

		e := echo.New()
		srv.RegisterHTTPRoutes(e.Group(""))

		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/staff", bytes.NewBufferString(`{"garageId":"garage-1","accountId":"acc-1","role":"owner"}`))
		req.Header.Set("Authorization", "Bearer "+token)

		rec := httptest.NewRecorder()
		middleware.HTTPAuth(issuer, e).ServeHTTP(rec, req)

		require.Equal(t, http.StatusCreated, rec.Code)

		var payload map[string]any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
		assert.Equal(t, "garage-1", payload["garageId"])
	})
}

func TestServer_StaffHTTPAccessAndPayload(t *testing.T) {
	t.Parallel()

	t.Run("missing token returns unauthorized", func(t *testing.T) {
		t.Parallel()

		srv, _ := newStaffHTTPTestServer(t)
		issuer := auth.NewTokenIssuer([]byte("tenant-test-key"), time.Minute, time.Hour)

		e := echo.New()
		srv.RegisterHTTPRoutes(e.Group(""))

		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/staff?garageId=garage-1", nil)
		rec := httptest.NewRecorder()
		middleware.HTTPAuth(issuer, e).ServeHTTP(rec, req)

		require.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("json response writes payload with content type", func(t *testing.T) {
		t.Parallel()

		srv, staffRepo := newStaffHTTPTestServer(t)
		staffRepo.EXPECT().ListByGarageID(mock.Anything, "garage-1").Return([]*domain.GarageStaff{}, nil).Once()

		issuer := auth.NewTokenIssuer([]byte("tenant-test-key"), time.Minute, time.Hour)
		token, _, err := issuer.IssueAccessToken("acc-1", []auth.GarageClaim{{GarageID: "garage-1", Role: "owner"}})
		require.NoError(t, err)

		e := echo.New()
		srv.RegisterHTTPRoutes(e.Group(""))

		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/staff?garageId=garage-1", nil)
		req.Header.Set("Authorization", "Bearer "+token)

		rec := httptest.NewRecorder()
		middleware.HTTPAuth(issuer, e).ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		require.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	})
}

func newStaffHTTPTestServer(t *testing.T) (*handler.Server, *mocks.MockGarageStaffRepository) {
	t.Helper()

	staffRepo := mocks.NewMockGarageStaffRepository(t)

	return handler.NewServer(nil, usecase.NewStaffUseCase(staffRepo)), staffRepo
}
