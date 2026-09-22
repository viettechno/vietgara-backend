package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/viettechno/vietgara-backend/internal/identity/domain"
	"github.com/viettechno/vietgara-backend/internal/identity/handler"
	"github.com/viettechno/vietgara-backend/internal/mocks"
)

// googleStateCookieName mirrors the unexported cookie name google_oauth.go
// uses to persist the CSRF state between the login redirect and the
// callback.
const googleStateCookieName = "vietgara_google_oauth_state"

func newGoogleTestBackend(t *testing.T, tokenHandler, userinfoHandler http.HandlerFunc) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/token", tokenHandler)
	mux.HandleFunc("/userinfo", userinfoHandler)

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return srv
}

func jsonHandler(t *testing.T, status int, body any) http.HandlerFunc {
	t.Helper()

	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}
}

func newConfiguredGoogleHandler(t *testing.T, accounts *mocks.MockAccountRepository, garages *mocks.MockGarageMembershipReader, tokens *mocks.MockRefreshTokenStore, backend *httptest.Server) *handler.Server {
	t.Helper()

	srv := newTestHandler(t, accounts, garages, tokens)
	srv.ConfigureGoogleOAuth(handler.GoogleOAuthConfig{
		ClientID:       "client-id",
		ClientSecret:   "client-secret",
		AuthEndpoint:   "https://accounts.google.com/o/oauth2/v2/auth",
		TokenEndpoint:  backend.URL + "/token",
		UserInfoURL:    backend.URL + "/userinfo",
		RedirectURI:    "https://app.example.com/auth/google/callback",
		FrontendOrigin: "https://app.example.com",
	})

	return srv
}

// googleStateCookie returns a cookie carrying the given OAuth state value,
// matching the flags google_oauth.go sets on the real cookie.
func googleStateCookie(value string) *http.Cookie {
	return &http.Cookie{
		Name:     googleStateCookieName,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   true,
	}
}

func TestServer_GoogleLogin(t *testing.T) {
	t.Parallel()

	t.Run("not configured returns 501", func(t *testing.T) {
		t.Parallel()

		srv := newTestHandler(t, mocks.NewMockAccountRepository(t), mocks.NewMockGarageMembershipReader(t), mocks.NewMockRefreshTokenStore(t))

		e := echo.New()
		srv.RegisterHTTPRoutes(e.Group(""))

		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/auth/google/login", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		require.Equal(t, http.StatusNotImplemented, rec.Code)
	})

	t.Run("configured redirects to google with a matching state cookie", func(t *testing.T) {
		t.Parallel()

		srv := newTestHandler(t, mocks.NewMockAccountRepository(t), mocks.NewMockGarageMembershipReader(t), mocks.NewMockRefreshTokenStore(t))
		srv.ConfigureGoogleOAuth(handler.GoogleOAuthConfig{
			ClientID:     "client-id",
			ClientSecret: "client-secret",
			AuthEndpoint: "https://accounts.google.com/o/oauth2/v2/auth",
			RedirectURI:  "https://app.example.com/auth/google/callback",
		})

		e := echo.New()
		srv.RegisterHTTPRoutes(e.Group(""))

		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/auth/google/login", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		require.Equal(t, http.StatusFound, rec.Code)

		loc, err := url.Parse(rec.Header().Get("Location"))
		require.NoError(t, err)
		require.Equal(t, "accounts.google.com", loc.Host)
		require.Equal(t, "client-id", loc.Query().Get("client_id"))

		state := loc.Query().Get("state")
		require.NotEmpty(t, state)

		cookies := rec.Result().Cookies()
		require.Len(t, cookies, 1)
		require.Equal(t, googleStateCookieName, cookies[0].Name)
		require.Equal(t, state, cookies[0].Value)
	})
}

func TestServer_GoogleCallback_NotConfigured(t *testing.T) {
	t.Parallel()

	srv := newTestHandler(t, mocks.NewMockAccountRepository(t), mocks.NewMockGarageMembershipReader(t), mocks.NewMockRefreshTokenStore(t))

	e := echo.New()
	srv.RegisterHTTPRoutes(e.Group(""))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/auth/google/callback", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNotImplemented, rec.Code)
}

func TestServer_GoogleCallback_State(t *testing.T) {
	t.Parallel()

	backend := newGoogleTestBackend(t,
		jsonHandler(t, http.StatusOK, map[string]string{"access_token": "access-token"}),
		jsonHandler(t, http.StatusOK, map[string]string{"sub": "google-sub-1", "email": "social@example.com", "name": "Social User"}),
	)

	tests := []struct {
		name        string
		queryString string
		cookie      *http.Cookie
		wantStatus  int
		wantBody    string
	}{
		{
			name:        "missing state cookie is rejected",
			queryString: "state=abc&code=xyz",
			wantStatus:  http.StatusBadRequest,
			wantBody:    "invalid OAuth state",
		},
		{
			name:        "mismatched state is rejected",
			queryString: "state=abc&code=xyz",
			cookie:      googleStateCookie("different"),
			wantStatus:  http.StatusBadRequest,
			wantBody:    "invalid OAuth state",
		},
		{
			name:        "provider error is surfaced",
			queryString: "state=abc&error=access_denied",
			cookie:      googleStateCookie("abc"),
			wantStatus:  http.StatusBadRequest,
			wantBody:    "Google authorization failed: access_denied",
		},
		{
			name:        "missing code is rejected",
			queryString: "state=abc",
			cookie:      googleStateCookie("abc"),
			wantStatus:  http.StatusBadRequest,
			wantBody:    "missing authorization code",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			srv := newConfiguredGoogleHandler(t, mocks.NewMockAccountRepository(t), mocks.NewMockGarageMembershipReader(t), mocks.NewMockRefreshTokenStore(t), backend)

			e := echo.New()
			srv.RegisterHTTPRoutes(e.Group(""))

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/auth/google/callback?"+tc.queryString, nil)
			if tc.cookie != nil {
				req.AddCookie(tc.cookie)
			}

			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)

			require.Equal(t, tc.wantStatus, rec.Code)
			require.Contains(t, rec.Body.String(), tc.wantBody)
		})
	}
}

func TestServer_GoogleCallback_TokenExchangeFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		backend *httptest.Server
	}{
		{
			name: "token endpoint errors",
			backend: newGoogleTestBackend(t,
				func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) },
				jsonHandler(t, http.StatusOK, map[string]string{}),
			),
		},
		{
			name: "token endpoint omits access_token",
			backend: newGoogleTestBackend(t,
				jsonHandler(t, http.StatusOK, map[string]string{}),
				jsonHandler(t, http.StatusOK, map[string]string{}),
			),
		},
		{
			name: "userinfo endpoint errors",
			backend: newGoogleTestBackend(t,
				jsonHandler(t, http.StatusOK, map[string]string{"access_token": "access-token"}),
				func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) },
			),
		},
		{
			name: "profile missing required fields",
			backend: newGoogleTestBackend(t,
				jsonHandler(t, http.StatusOK, map[string]string{"access_token": "access-token"}),
				jsonHandler(t, http.StatusOK, map[string]string{"email": "social@example.com"}),
			),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			srv := newConfiguredGoogleHandler(t, mocks.NewMockAccountRepository(t), mocks.NewMockGarageMembershipReader(t), mocks.NewMockRefreshTokenStore(t), tc.backend)

			e := echo.New()
			srv.RegisterHTTPRoutes(e.Group(""))

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/auth/google/callback?state=abc&code=xyz", nil)
			req.AddCookie(googleStateCookie("abc"))

			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)

			require.Equal(t, http.StatusBadGateway, rec.Code)
			require.Contains(t, rec.Body.String(), "Google token exchange failed")
		})
	}
}

func TestServer_GoogleCallback_UsecaseFailure(t *testing.T) {
	t.Parallel()

	backend := newGoogleTestBackend(t,
		jsonHandler(t, http.StatusOK, map[string]string{"access_token": "access-token"}),
		jsonHandler(t, http.StatusOK, map[string]string{"sub": "google-sub-1", "email": "social@example.com", "name": "Social User"}),
	)

	accounts := mocks.NewMockAccountRepository(t)
	garages := mocks.NewMockGarageMembershipReader(t)
	tokens := mocks.NewMockRefreshTokenStore(t)
	srv := newConfiguredGoogleHandler(t, accounts, garages, tokens, backend)
	accounts.EXPECT().FindByIdentityPlatformUID(mock.Anything, "google-sub-1").Return(nil, domain.ErrAccountNotFound).Once()
	accounts.EXPECT().Create(mock.Anything, mock.Anything).Return(domain.ErrAccountNotFound).Once()

	e := echo.New()
	srv.RegisterHTTPRoutes(e.Group(""))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/auth/google/callback?state=abc&code=xyz", nil)
	req.AddCookie(googleStateCookie("abc"))

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestServer_GoogleCallback_Success(t *testing.T) {
	t.Parallel()

	backend := newGoogleTestBackend(t,
		jsonHandler(t, http.StatusOK, map[string]string{"access_token": "access-token"}),
		jsonHandler(t, http.StatusOK, map[string]string{"sub": "google-sub-1", "email": "social@example.com", "name": "Social User"}),
	)

	accounts := mocks.NewMockAccountRepository(t)
	garages := mocks.NewMockGarageMembershipReader(t)
	tokens := mocks.NewMockRefreshTokenStore(t)
	srv := newConfiguredGoogleHandler(t, accounts, garages, tokens, backend)
	accounts.EXPECT().FindByIdentityPlatformUID(mock.Anything, "google-sub-1").Return(nil, domain.ErrAccountNotFound).Once()
	accounts.EXPECT().Create(mock.Anything, mock.Anything).Return(nil).Once()
	garages.EXPECT().ListGarageRolesForAccount(mock.Anything, mock.Anything).Return(nil, nil).Once()
	tokens.EXPECT().Store(mock.Anything, mock.Anything, mock.Anything, 86400).Return(nil).Once()

	e := echo.New()
	srv.RegisterHTTPRoutes(e.Group(""))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/auth/google/callback?state=abc&code=xyz", nil)
	req.AddCookie(googleStateCookie("abc"))

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "text/html; charset=utf-8", rec.Header().Get("Content-Type"))
	require.Contains(t, rec.Body.String(), "vietgara-google-auth")
	require.Contains(t, rec.Body.String(), "https://app.example.com")
	require.Contains(t, rec.Body.String(), "social@example.com")

	cookies := rec.Result().Cookies()
	require.Len(t, cookies, 1)
	require.Equal(t, googleStateCookieName, cookies[0].Name)
	require.Negative(t, cookies[0].MaxAge)
}

func TestServer_GoogleCallback_DefaultFrontendOrigin(t *testing.T) {
	t.Parallel()

	backend := newGoogleTestBackend(t,
		jsonHandler(t, http.StatusOK, map[string]string{"access_token": "access-token"}),
		jsonHandler(t, http.StatusOK, map[string]string{"sub": "google-sub-1", "email": "social@example.com", "name": "Social User"}),
	)

	accounts := mocks.NewMockAccountRepository(t)
	garages := mocks.NewMockGarageMembershipReader(t)
	tokens := mocks.NewMockRefreshTokenStore(t)
	srv := newTestHandler(t, accounts, garages, tokens)
	srv.ConfigureGoogleOAuth(handler.GoogleOAuthConfig{
		ClientID:      "client-id",
		ClientSecret:  "client-secret",
		AuthEndpoint:  "https://accounts.google.com/o/oauth2/v2/auth",
		TokenEndpoint: backend.URL + "/token",
		UserInfoURL:   backend.URL + "/userinfo",
		RedirectURI:   "https://app.example.com/auth/google/callback",
	})
	accounts.EXPECT().FindByIdentityPlatformUID(mock.Anything, "google-sub-1").Return(nil, domain.ErrAccountNotFound).Once()
	accounts.EXPECT().Create(mock.Anything, mock.Anything).Return(nil).Once()
	garages.EXPECT().ListGarageRolesForAccount(mock.Anything, mock.Anything).Return(nil, nil).Once()
	tokens.EXPECT().Store(mock.Anything, mock.Anything, mock.Anything, 86400).Return(nil).Once()

	e := echo.New()
	srv.RegisterHTTPRoutes(e.Group(""))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/auth/google/callback?state=abc&code=xyz", nil)
	req.AddCookie(googleStateCookie("abc"))

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "http://localhost:8000")
}
