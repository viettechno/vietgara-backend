package handler_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cataloguev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/catalogue/v1"

	"github.com/viettechno/vietgara-backend/internal/catalogue/domain"
	"github.com/viettechno/vietgara-backend/internal/platform/httpapi"
)

// TestBarcodeRESTRoutes sends the barcode requests through the REST gateway,
// so the custom verb of the lookup and the PUT of the barcode table are
// exercised as a client would call them.
func TestBarcodeRESTRoutes(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	mux := httpapi.NewMux()
	require.NoError(t, cataloguev1.RegisterCatalogueServiceHandlerServer(t.Context(), mux, e.srv))

	send := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(signedIn(t), method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		return rec
	}

	carton := caseID
	part := samplePart()
	part.Images = nil
	part.Barcodes = []domain.PartBarcode{{Barcode: "8930024", UnitID: &carton, UnitName: "Carton", UnitAbbreviation: "ctn"}}

	e.repo.EXPECT().FindBarcode(anyArg, garageID, "8930024").Return(part, &part.Barcodes[0], nil).Once()
	e.repo.EXPECT().FindBarcode(anyArg, garageID, "none").Return(nil, nil, domain.ErrBarcodeNotFound).Once()
	e.repo.EXPECT().FindPart(anyArg, garageID, partID).Return(part, nil)
	e.repo.EXPECT().ReplaceBarcodes(anyArg, garageID, partID, actorID, anyArg).Return(nil).Once()

	rec := send(http.MethodGet, "/api/v1/garages/"+garageID+"/barcodes:lookup?barcode=8930024", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"unitAbbreviation":"ctn"`)
	assert.Contains(t, rec.Body.String(), `"sku":"BEER-330"`)

	rec = send(http.MethodGet, "/api/v1/garages/"+garageID+"/barcodes:lookup?barcode=none", "")
	assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())

	rec = send(http.MethodPut, "/api/v1/garages/"+garageID+"/parts/"+partID+"/barcodes",
		`{"barcodes":[{"barcode":"8930024","unitId":"`+caseID+`"}]}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"barcode":"8930024"`, "the response is the part, not a wrapper")
}
