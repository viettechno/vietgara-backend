package apidocs_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/viettechno/vietgara-proto/openapi"

	"github.com/viettechno/vietgara-backend/internal/platform/apidocs"
	"github.com/viettechno/vietgara-backend/internal/platform/httpapi"
)

func serve(t *testing.T, path string) *httptest.ResponseRecorder {
	t.Helper()

	mux := httpapi.NewMux()
	require.NoError(t, apidocs.Register(mux, openapi.Spec))

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil))

	return rec
}

func TestSpecServesTheProtoDocument(t *testing.T) {
	rec := serve(t, apidocs.SpecPath)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var doc struct {
		Swagger string         `json:"swagger"`
		Paths   map[string]any `json:"paths"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &doc))
	assert.Equal(t, "2.0", doc.Swagger)
	assert.Contains(t, doc.Paths, "/api/v1/auth/email-verification")
}

func TestDocsPageLoadsTheSpec(t *testing.T) {
	rec := serve(t, apidocs.DocsPath)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "text/html; charset=utf-8", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Body.String(), "url: '"+apidocs.SpecPath+"'")
	assert.Contains(t, rec.Body.String(), "@scalar/api-reference@")
}
