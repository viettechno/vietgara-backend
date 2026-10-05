package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/identity/handler"
	"github.com/viettechno/vietgara-backend/internal/platform/httpapi"
)

// fakeGoogle serves the token and userinfo endpoints.
func fakeGoogle(t *testing.T, tokenStatus int, userinfo string) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			assert.Equal(t, "the-code", r.FormValue("code"))
			w.WriteHeader(tokenStatus)
			_, _ = w.Write([]byte(`{"access_token":"at"}`))
		case "/userinfo":
			assert.Equal(t, "Bearer at", r.Header.Get("Authorization"))

			_, _ = w.Write([]byte(userinfo))
		}
	}))
	t.Cleanup(srv.Close)

	return srv
}

func config(google *httptest.Server) handler.GoogleOAuthConfig {
	return handler.GoogleOAuthConfig{
		ClientID: "cid", ClientSecret: "secret", AuthEndpoint: "https://accounts.example/auth",
		TokenEndpoint: google.URL + "/token", UserInfoURL: google.URL + "/userinfo",
		AppBaseURL: "http://localhost:8000",
	}
}

var messagePattern = regexp.MustCompile(`postMessage\((\{.*\}),"`)

// popupMessage extracts the JSON posted to the opener.
func popupMessage(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()

	m := messagePattern.FindStringSubmatch(rec.Body.String())
	require.Len(t, m, 2, rec.Body.String())

	var msg map[string]any
	require.NoError(t, json.Unmarshal([]byte(m[1]), &msg))
	assert.Equal(t, "vietgara-google-auth", msg["type"])

	return msg
}

func serve(t *testing.T, e *env, path string, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()

	mux := httpapi.NewMux()
	require.NoError(t, e.srv.RegisterHTTPRoutes(mux))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	return rec
}

func TestGoogleLoginFlow(t *testing.T) {
	t.Parallel()

	google := fakeGoogle(t, http.StatusOK, `{"sub":"g-1","email":"New@Gmail.com","email_verified":true,"name":"Người Mới","locale":"en-US"}`)
	e := newEnv(t, config(google))

	rec := serve(t, e, handler.GoogleLoginPath, nil)
	require.Equal(t, http.StatusFound, rec.Code)

	location, err := url.Parse(rec.Header().Get("Location"))
	require.NoError(t, err)

	state := location.Query().Get("state")
	assert.Equal(t, "cid", location.Query().Get("client_id"))
	assert.Equal(t, "http://localhost:8000"+handler.GoogleCallbackPath, location.Query().Get("redirect_uri"))
	require.NotEmpty(t, state)

	cookie := rec.Result().Cookies()[0]
	rec = serve(t, e, handler.GoogleCallbackPath+"?code=the-code&state="+state, cookie)
	require.Equal(t, http.StatusOK, rec.Code)

	session, ok := popupMessage(t, rec)["session"].(map[string]any)
	require.True(t, ok)
	account, ok := session["account"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "new@gmail.com", account["email"])
	assert.Equal(t, true, account["emailVerified"])
	assert.Equal(t, "LOCALE_EN", account["locale"])
	assert.Equal(t, "AUTH_PROVIDER_GOOGLE", account["authProvider"])
}

func TestGoogleCallbackErrors(t *testing.T) {
	t.Parallel()

	good := `{"sub":"g","email":"a@b.vn","email_verified":true,"name":"A"}`
	cookie := &http.Cookie{Name: "vietgara_google_oauth_state", Value: "s", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode}

	tests := []struct {
		name   string
		cfg    func() handler.GoogleOAuthConfig
		path   string
		cookie *http.Cookie
		want   string
	}{
		{name: "not configured", cfg: func() handler.GoogleOAuthConfig { return handler.GoogleOAuthConfig{} }, path: handler.GoogleCallbackPath, want: "GOOGLE_NOT_CONFIGURED"},
		{name: "missing state cookie", cfg: func() handler.GoogleOAuthConfig { return config(fakeGoogle(t, 200, good)) }, path: handler.GoogleCallbackPath + "?state=s&code=the-code", want: "GOOGLE_STATE_INVALID"},
		{name: "denied", cfg: func() handler.GoogleOAuthConfig { return config(fakeGoogle(t, 200, good)) }, path: handler.GoogleCallbackPath + "?state=s&error=access_denied", cookie: cookie, want: "GOOGLE_DENIED"},
		{name: "token endpoint fails", cfg: func() handler.GoogleOAuthConfig { return config(fakeGoogle(t, 500, good)) }, path: handler.GoogleCallbackPath + "?state=s&code=the-code", cookie: cookie, want: "GOOGLE_EXCHANGE_FAILED"},
		{name: "profile without e-mail", cfg: func() handler.GoogleOAuthConfig { return config(fakeGoogle(t, 200, `{"sub":"g"}`)) }, path: handler.GoogleCallbackPath + "?state=s&code=the-code", cookie: cookie, want: "GOOGLE_EXCHANGE_FAILED"},
		{name: "userinfo garbage", cfg: func() handler.GoogleOAuthConfig { return config(fakeGoogle(t, 200, `nope`)) }, path: handler.GoogleCallbackPath + "?state=s&code=the-code", cookie: cookie, want: "GOOGLE_EXCHANGE_FAILED"},
		{name: "unverified e-mail", cfg: func() handler.GoogleOAuthConfig { return config(fakeGoogle(t, 200, `{"sub":"g","email":"a@b.vn"}`)) }, path: handler.GoogleCallbackPath + "?state=s&code=the-code", cookie: cookie, want: "INVALID_CREDENTIALS"},
		{name: "unreachable google", cfg: func() handler.GoogleOAuthConfig {
			c := config(fakeGoogle(t, 200, good))
			c.TokenEndpoint = "http://127.0.0.1:1/token"

			return c
		}, path: handler.GoogleCallbackPath + "?state=s&code=the-code", cookie: cookie, want: "GOOGLE_EXCHANGE_FAILED"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := serve(t, newEnv(t, tt.cfg()), tt.path, tt.cookie)
			assert.Equal(t, tt.want, popupMessage(t, rec)["error"])
		})
	}

	rec := serve(t, newEnv(t, handler.GoogleOAuthConfig{}), handler.GoogleLoginPath, nil)
	assert.Equal(t, "GOOGLE_NOT_CONFIGURED", popupMessage(t, rec)["error"])
	assert.Contains(t, rec.Body.String(), `,"/")`, "same-origin target without a base URL")

	cfg := handler.GoogleOAuthConfig{ClientID: "c", ClientSecret: "s", //nolint:gosec // test fixture, not a credential
		AppBaseURL: "https://staging.vietgara.vn/app/", UserInfoURL: "http://127.0.0.1:1/u", TokenEndpoint: "http://127.0.0.1:1/t"}
	rec = serve(t, newEnv(t, cfg), handler.GoogleCallbackPath+"?state=s&code=x", cookie)
	assert.Contains(t, rec.Body.String(), `,"https://staging.vietgara.vn")`, "origin of the base URL")

	rec = serve(t, newEnv(t, cfg), handler.GoogleLoginPath, nil)
	location, err := url.Parse(rec.Header().Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, "https://staging.vietgara.vn"+handler.GoogleCallbackPath, location.Query().Get("redirect_uri"))
}
