package handler_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/viettechno/vietgara-backend/internal/identity/domain"
	"github.com/viettechno/vietgara-backend/internal/identity/handler"
	"github.com/viettechno/vietgara-backend/internal/mocks"
	identityv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/identity/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestServer_RegisterHTTPRoutes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		method string
		path   string
	}{
		{name: "register", method: http.MethodPost, path: "/auth/register"},
		{name: "login", method: http.MethodPost, path: "/auth/login"},
		{name: "google login", method: http.MethodGet, path: "/auth/google/login"},
		{name: "google callback", method: http.MethodGet, path: "/auth/google/callback"},
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

func TestServer_HandleHTTPRegisterInvalidBody(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
	}{
		{name: "invalid JSON", body: "{invalid"},
		{name: "empty body", body: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			e := echo.New()
			srv := &handler.Server{}
			srv.RegisterHTTPRoutes(e.Group(""))

			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/auth/register", bytes.NewBufferString(tc.body))
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)

			require.Equal(t, http.StatusBadRequest, rec.Code)
			assertHTTPErrorMessage(t, rec, "invalid request body")
		})
	}
}

func TestServer_HandleHTTPLoginInvalidBody(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
	}{
		{name: "invalid JSON", body: "{invalid"},
		{name: "empty body", body: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			e := echo.New()
			srv := &handler.Server{}
			srv.RegisterHTTPRoutes(e.Group(""))

			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/auth/login", bytes.NewBufferString(tc.body))
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)

			require.Equal(t, http.StatusBadRequest, rec.Code)
			assertHTTPErrorMessage(t, rec, "invalid request body")
		})
	}
}

func TestServer_HandleHTTPRegisterAndLoginSuccess(t *testing.T) {
	t.Parallel()

	t.Run("register translates JSON and writes auth proto", func(t *testing.T) {
		t.Parallel()

		accounts := mocks.NewMockAccountRepository(t)
		garages := mocks.NewMockGarageMembershipReader(t)
		tokens := mocks.NewMockRefreshTokenStore(t)
		srv := newTestHandler(t, accounts, garages, tokens)
		accounts.EXPECT().FindByEmail(mock.Anything, "http-register@example.com").Return(nil, domain.ErrAccountNotFound).Once()
		accounts.EXPECT().Create(mock.Anything, mock.Anything).Return(nil).Once()
		garages.EXPECT().ListGarageRolesForAccount(mock.Anything, mock.Anything).Return(nil, nil).Once()
		tokens.EXPECT().Store(mock.Anything, mock.Anything, mock.Anything, 86400).Return(nil).Once()

		e := echo.New()
		srv.RegisterHTTPRoutes(e.Group(""))

		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/auth/register", bytes.NewBufferString(`{"email":"http-register@example.com","password":"secret123","fullName":"HTTP User"}`))
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		require.Equal(t, "application/json", rec.Header().Get("Content-Type"))

		var response identityv1.AuthResponse
		require.NoError(t, protojson.Unmarshal(rec.Body.Bytes(), &response))
		require.Equal(t, "http-register@example.com", response.GetAccount().GetEmail())
	})

	t.Run("login translates JSON and writes auth proto", func(t *testing.T) {
		t.Parallel()

		accounts := mocks.NewMockAccountRepository(t)
		garages := mocks.NewMockGarageMembershipReader(t)
		tokens := mocks.NewMockRefreshTokenStore(t)
		srv := newTestHandler(t, accounts, garages, tokens)

		email := "http-login@example.com"
		accounts.EXPECT().FindByEmail(mock.Anything, email).Return(&domain.Account{
			ID: "account-1", Email: &email, AuthProvider: domain.AuthProviderEmail,
			FullName: "HTTP User", PasswordHash: passwordHash(t, "secret123"), Status: domain.AccountStatusActive,
		}, nil).Once()
		garages.EXPECT().ListGarageRolesForAccount(mock.Anything, "account-1").Return(nil, nil).Once()
		tokens.EXPECT().Store(mock.Anything, mock.Anything, "account-1", 86400).Return(nil).Once()

		e := echo.New()
		srv.RegisterHTTPRoutes(e.Group(""))

		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/auth/login", bytes.NewBufferString(`{"email":"http-login@example.com","password":"secret123"}`))
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)

		var response identityv1.AuthResponse
		require.NoError(t, protojson.Unmarshal(rec.Body.Bytes(), &response))
		require.Equal(t, email, response.GetAccount().GetEmail())
	})
}

func TestServer_HandleHTTPErrorMapping(t *testing.T) {
	t.Parallel()

	t.Run("duplicate register maps to conflict", func(t *testing.T) {
		t.Parallel()

		accounts := mocks.NewMockAccountRepository(t)
		garages := mocks.NewMockGarageMembershipReader(t)
		tokens := mocks.NewMockRefreshTokenStore(t)
		srv := newTestHandler(t, accounts, garages, tokens)
		accounts.EXPECT().FindByEmail(mock.Anything, "dup@example.com").Return(&domain.Account{ID: "acc-1"}, nil).Once()

		e := echo.New()
		srv.RegisterHTTPRoutes(e.Group(""))

		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/auth/register", bytes.NewBufferString(`{"email":"dup@example.com","password":"secret123","fullName":"Dup"}`))
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		require.Equal(t, http.StatusConflict, rec.Code)
	})

	t.Run("wrong password maps to unauthorized", func(t *testing.T) {
		t.Parallel()

		accounts := mocks.NewMockAccountRepository(t)
		garages := mocks.NewMockGarageMembershipReader(t)
		tokens := mocks.NewMockRefreshTokenStore(t)
		srv := newTestHandler(t, accounts, garages, tokens)

		email := "http-login@example.com"
		accounts.EXPECT().FindByEmail(mock.Anything, email).Return(&domain.Account{
			ID: "account-1", Email: &email, AuthProvider: domain.AuthProviderEmail,
			FullName: "HTTP User", PasswordHash: passwordHash(t, "secret123"), Status: domain.AccountStatusActive,
		}, nil).Once()

		e := echo.New()
		srv.RegisterHTTPRoutes(e.Group(""))

		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/auth/login", bytes.NewBufferString(`{"email":"http-login@example.com","password":"wrong"}`))
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		require.Equal(t, http.StatusUnauthorized, rec.Code)
	})
}

func assertHTTPErrorMessage(t *testing.T, rec *httptest.ResponseRecorder, expected string) {
	t.Helper()

	var payload map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
	require.Equal(t, expected, payload["message"])
}
