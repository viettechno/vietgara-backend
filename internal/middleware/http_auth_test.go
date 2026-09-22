package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/viettechno/vietgara-backend/internal/middleware"
)

func TestHTTPAuth(t *testing.T) {
	t.Parallel()

	issuer := newTestIssuer(t)
	validToken := issueToken(t, issuer, "acc-1", nil)

	tests := []struct {
		name       string
		authHeader string
		wantStatus int
	}{
		{name: "missing header", authHeader: "", wantStatus: http.StatusUnauthorized},
		{name: "missing bearer prefix", authHeader: "Token abc", wantStatus: http.StatusUnauthorized},
		{name: "header shorter than prefix", authHeader: "Bear", wantStatus: http.StatusUnauthorized},
		{name: "invalid token", authHeader: "Bearer not-a-jwt", wantStatus: http.StatusUnauthorized},
		{name: "valid token", authHeader: "Bearer " + validToken, wantStatus: http.StatusOK},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var gotClaims bool

			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, gotClaims = middleware.ClaimsFromContext(r.Context())

				w.WriteHeader(http.StatusOK)
			})

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/protected", nil)
			if tc.authHeader != "" {
				req.Header.Set("Authorization", tc.authHeader)
			}

			rec := httptest.NewRecorder()
			middleware.HTTPAuth(issuer, next).ServeHTTP(rec, req)

			require.Equal(t, tc.wantStatus, rec.Code)
			assert.Equal(t, tc.wantStatus == http.StatusOK, gotClaims)
		})
	}
}
