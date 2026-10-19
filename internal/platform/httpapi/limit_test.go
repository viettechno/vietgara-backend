package httpapi_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/platform/httpapi"
)

func TestLimitBody(t *testing.T) {
	t.Parallel()

	read := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
		}
	})
	handler := httpapi.LimitBody(read, func(r *http.Request) int64 {
		if strings.HasPrefix(r.URL.Path, "/big") {
			return 100
		}

		return 10
	})

	send := func(path, body string) int {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, path, strings.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		return rec.Code
	}

	assert.Equal(t, http.StatusOK, send("/small", "0123456789"))
	assert.Equal(t, http.StatusRequestEntityTooLarge, send("/small", "0123456789A"))
	assert.Equal(t, http.StatusOK, send("/big", strings.Repeat("x", 100)), "a route may allow more")
	assert.Equal(t, http.StatusRequestEntityTooLarge, send("/big", strings.Repeat("x", 101)))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/small", http.NoBody)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "a request without a body is untouched")
}
