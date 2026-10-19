package handler_test

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	inventoryv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/inventory/v1"

	"github.com/viettechno/vietgara-backend/internal/dataimport/domain"
	"github.com/viettechno/vietgara-backend/internal/dataimport/handler"
	"github.com/viettechno/vietgara-backend/internal/platform/access"
	"github.com/viettechno/vietgara-backend/internal/platform/httpapi"
)

// restRequest sends a request through the REST gateway to the real handler,
// so routes, the enum in the path and the JSON/base64 body are exercised as
// a client would use them.
func restRequest(t *testing.T, e *env, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()

	mux := httpapi.NewMux()
	require.NoError(t, inventoryv1.RegisterImportServiceHandlerServer(t.Context(), mux, e.srv))

	req := httptest.NewRequestWithContext(as(t, access.CatalogueWrite), method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	return rec
}

func TestRESTRoutes(t *testing.T) {
	t.Parallel()

	t.Run("the template is a download", func(t *testing.T) {
		t.Parallel()

		rec := restRequest(t, newEnv(t, nil), http.MethodGet, "/api/v1/garages/"+garageID+"/imports/IMPORT_TYPE_PARTS/template", "")

		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.Equal(t, handler.ContentType, rec.Header().Get("Content-Type"))
		assert.Equal(t, "PK", rec.Body.String()[:2], "an .xlsx is a zip, not JSON")
	})

	t.Run("a preview takes the file as base64 in the JSON body", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t, headerOnly(domain.TypeParts))
		e.expectNothingExists()

		body, err := json.Marshal(map[string]string{"fileName": "parts.xlsx", "content": base64.StdEncoding.EncodeToString([]byte("xlsx"))})
		require.NoError(t, err)

		rec := restRequest(t, e, http.MethodPost, "/api/v1/garages/"+garageID+"/imports/IMPORT_TYPE_PARTS/preview", string(body))

		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		var got struct {
			ImportType string   `json:"importType"`
			Columns    []string `json:"columns"`
			CanConfirm bool     `json:"canConfirm"`
		}

		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		assert.Equal(t, "IMPORT_TYPE_PARTS", got.ImportType)
		assert.NotEmpty(t, got.Columns)
		assert.False(t, got.CanConfirm)
	})

	t.Run("an unspecified type is a validation error, not a missing route", func(t *testing.T) {
		t.Parallel()

		rec := restRequest(t, newEnv(t, nil), http.MethodGet, "/api/v1/garages/"+garageID+"/imports/IMPORT_TYPE_UNSPECIFIED/template", "")
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	})

	t.Run("confirm refuses an empty file with the stable reason", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t, headerOnly(domain.TypeParts))
		e.expectNothingExists()
		e.runs.EXPECT().Find(anyArg, garageID, "parts", anyArg).Return(nil, nil)

		rec := restRequest(t, e, http.MethodPost, "/api/v1/garages/"+garageID+"/imports/IMPORT_TYPE_PARTS/confirm",
			`{"content":"`+base64.StdEncoding.EncodeToString([]byte("xlsx"))+`"}`)

		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
		assert.Contains(t, rec.Body.String(), "IMPORT_FILE_INVALID")
	})
}
