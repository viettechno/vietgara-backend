// Scenario: E2E-INV-030
// docs: vietgara-docs/docs/05-Testing/E2E_Scenarios/07_Inventory.md
package inventory_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	cataloguev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/catalogue/v1"
	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

// Scenario: E2E-INV-030, Release 7.2, barcodes (FR-INV-13): a part has several codes, for
// the base unit or for a packaging unit; a code is unique in the garage
// without regard to case; lookup and search find the part; a packaging unit
// with a barcode can be edited but not removed; deleting a part frees its
// codes; another garage may use the same code.
// The lookup of an inactive part is covered by the unit tests.
func TestPartBarcodes(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage := setup(t, "inv-barcodes")
	garageID := garage.GetId()

	can := fixture.NewUnit(t, owner.API, garageID, "Can", "can")
	carton := fixture.NewUnit(t, owner.API, garageID, "Carton", "ctn")
	beer := fixture.NewPart(t, owner.API, garageID, "BEER-330", "Beer 330 ml", can)
	cola := fixture.NewPart(t, owner.API, garageID, "COLA-330", "Cola 330 ml", can)

	_, err := owner.API.SetPartUnits(ctx, garageID, beer.GetId(), []*cataloguev1.PackagingUnit{{UnitId: carton.GetId(), Factor: "24"}})
	require.NoError(t, err)

	t.Run("several codes for the base unit and a case", func(t *testing.T) {
		part, err := owner.API.SetPartBarcodes(ctx, garageID, beer.GetId(), []*cataloguev1.PartBarcode{
			{Barcode: " 8930001 "},
			{Barcode: "8930024", UnitId: new(carton.GetId())},
			{Barcode: "Ab-1"},
		})
		require.NoError(t, err)
		require.Len(t, part.GetBarcodes(), 3)

		byCode := map[string]*cataloguev1.PartBarcode{}
		for _, b := range part.GetBarcodes() {
			byCode[b.GetBarcode()] = b
		}

		assert.Empty(t, byCode["8930001"].GetUnitId(), "trimmed, and the base unit")
		assert.Equal(t, carton.GetId(), byCode["8930024"].GetUnitId())
	})

	t.Run("a code is unique in the garage, in any case", func(t *testing.T) {
		for _, code := range []string{"8930001", " 8930001 ", "aB-1", "AB-1"} {
			_, err := owner.API.SetPartBarcodes(ctx, garageID, cola.GetId(), []*cataloguev1.PartBarcode{{Barcode: code}})
			fixture.RequireAPIError(t, err, http.StatusConflict, "BARCODE_ALREADY_USED")
		}

		got, err := owner.API.GetPart(ctx, garageID, cola.GetId())
		require.NoError(t, err)
		assert.Empty(t, got.GetBarcodes())
	})

	t.Run("malformed codes and units are refused", func(t *testing.T) {
		for _, code := range []string{"", "   ", "ab_cd"} {
			_, err := owner.API.SetPartBarcodes(ctx, garageID, cola.GetId(), []*cataloguev1.PartBarcode{{Barcode: code}})
			fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "BARCODE_INVALID")
		}

		_, err := owner.API.SetPartBarcodes(ctx, garageID, cola.GetId(), []*cataloguev1.PartBarcode{{Barcode: "1", UnitId: new(carton.GetId())}})
		fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "BARCODE_INVALID")
	})

	t.Run("lookup finds the part and the unit of the code, in any case", func(t *testing.T) {
		found, err := owner.API.LookupPartByBarcode(ctx, garageID, "8930024")
		require.NoError(t, err)
		assert.Equal(t, beer.GetId(), found.GetPart().GetId())
		assert.Equal(t, carton.GetId(), found.GetBarcode().GetUnitId())

		found, err = owner.API.LookupPartByBarcode(ctx, garageID, "ab-1")
		require.NoError(t, err)
		assert.Equal(t, beer.GetId(), found.GetPart().GetId())

		_, err = owner.API.LookupPartByBarcode(ctx, garageID, "0000000")
		fixture.RequireAPIError(t, err, http.StatusNotFound, "")

		_, err = owner.API.LookupPartByBarcode(ctx, garageID, "has_odd:chars")
		fixture.RequireAPIError(t, err, http.StatusNotFound, "")
	})

	t.Run("the parts list finds a part by an exact code", func(t *testing.T) {
		parts, err := owner.API.ListParts(ctx, garageID, url.Values{"query": {"8930001"}})
		require.NoError(t, err)
		require.Len(t, parts, 1)
		assert.Equal(t, beer.GetId(), parts[0].GetId())

		parts, err = owner.API.ListParts(ctx, garageID, url.Values{"query": {"89300"}})
		require.NoError(t, err)
		assert.Empty(t, parts, "a fragment of a code matches nothing")
	})

	t.Run("a packaging unit with a barcode can be edited but not removed", func(t *testing.T) {
		_, err := owner.API.SetPartUnits(ctx, garageID, beer.GetId(), nil)
		fixture.RequireAPIError(t, err, http.StatusConflict, "PACKAGING_UNIT_HAS_BARCODE")

		part, err := owner.API.SetPartUnits(ctx, garageID, beer.GetId(), []*cataloguev1.PackagingUnit{{UnitId: carton.GetId(), Factor: "12"}})
		require.NoError(t, err)
		assert.Equal(t, "12", part.GetPackagingUnits()[0].GetFactor())
		assert.Len(t, part.GetBarcodes(), 3, "the codes were kept")
	})

	t.Run("a deleted part frees its codes", func(t *testing.T) {
		_, err := owner.API.DeletePart(ctx, garageID, beer.GetId(), false)
		require.NoError(t, err) // no stock movement refers to it yet: it is deleted

		_, err = owner.API.LookupPartByBarcode(ctx, garageID, "8930001")
		fixture.RequireAPIError(t, err, http.StatusNotFound, "")

		got, err := owner.API.SetPartBarcodes(ctx, garageID, cola.GetId(), []*cataloguev1.PartBarcode{{Barcode: "8930001"}})
		require.NoError(t, err)
		assert.Len(t, got.GetBarcodes(), 1)
	})

	t.Run("another garage may use the same code and cannot see this one's", func(t *testing.T) {
		other, err := owner.API.CreateGarage(ctx, &tenantv1.Garage{Name: "Blackbox other barcodes"})
		require.NoError(t, err)

		unit := fixture.NewUnit(t, owner.API, other.GetId(), "Piece", "pc")
		part := fixture.NewPart(t, owner.API, other.GetId(), "OTHER-1", "Other", unit)

		_, err = owner.API.SetPartBarcodes(ctx, other.GetId(), part.GetId(), []*cataloguev1.PartBarcode{{Barcode: "8930001"}})
		require.NoError(t, err)

		found, err := owner.API.LookupPartByBarcode(ctx, garageID, "8930001")
		require.NoError(t, err)
		assert.Equal(t, cola.GetId(), found.GetPart().GetId(), "this garage still answers with its own part")
	})
}
