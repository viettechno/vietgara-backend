// Package catalogue_test covers the Catalogue module (Release 6.0) at API
// level: units of measure, parts with price, brand and category, packaging
// and the conversion of a quantity typed in a packaging unit, the category
// tree, part images, the delete-or-deactivate rule, the stock-movement
// Excel with both units, and the catalogue permissions. Forms, pickers and
// thumbnails on screen are web-E2E.
package catalogue_test

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	cataloguev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/catalogue/v1"
	commonv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/common/v1"
	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"
	identityv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/identity/v1"
	quotev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/quote/v1"
	repairorderv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/repairorder/v1"
	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/client"
	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

var seq atomic.Int64

// setup returns a fresh owner and garage.
func setup(t *testing.T, label string) (*fixture.Actor, string) {
	t.Helper()

	owner := fixture.NewVerifiedOwnerWithLocale(t, label, identityv1.Locale_LOCALE_EN)

	garage, err := owner.API.CreateGarage(t.Context(), &tenantv1.Garage{Name: "Blackbox " + label})
	require.NoError(t, err)

	return owner, garage.GetId()
}

// beer adds the documented BEER-330 shape: base unit can, 24 cans to the
// case, selling price 9,000 per can and an override of 200,000 per case.
func beer(t *testing.T, api *client.Client, garageID string) (part *cataloguev1.Part, can, caseUnit *cataloguev1.Unit) {
	t.Helper()

	can = fixture.NewUnit(t, api, garageID, "Can", "can")
	caseUnit = fixture.NewUnit(t, api, garageID, "Case", "case")
	part = fixture.NewPart(t, api, garageID, "BEER-330", "Beer 330ml", can,
		fixture.WithPrice(9000), fixture.WithBrand("Sabeco"), fixture.WithThreshold("100"))

	part, err := api.SetPartUnits(t.Context(), garageID, part.GetId(), []*cataloguev1.PackagingUnit{
		{UnitId: caseUnit.GetId(), Factor: "24", SalePrice: new(int64(200_000))},
	})
	require.NoError(t, err)

	return part, can, caseUnit
}

func waitForOnHand(t *testing.T, api *client.Client, garageID, partID, want string) {
	t.Helper()

	deadline := time.Now().Add(60 * time.Second)

	for {
		level, err := api.GetStockLevel(t.Context(), garageID, partID)
		require.NoError(t, err)

		if level.GetQuantityOnHand() == want {
			return
		}

		if time.Now().After(deadline) {
			t.Fatalf("on hand is %q, want %q", level.GetQuantityOnHand(), want)
		}

		time.Sleep(500 * time.Millisecond)
	}
}

// vehicleFor creates a customer with a vehicle.
func vehicleFor(t *testing.T, api *client.Client, garageID string) (*customerv1.Customer, *customerv1.Vehicle) {
	t.Helper()

	suffix := fmt.Sprintf("%05d", seq.Add(1)%100000)
	phone := "09170" + suffix

	customer, err := api.CreateCustomer(t.Context(), garageID, &customerv1.Customer{
		FullName: "QA Customer Catalogue", Phone: &phone, PersonalDataConsent: true,
	})
	require.NoError(t, err)

	vehicle, err := api.CreateVehicle(t.Context(), garageID, &customerv1.Vehicle{CustomerId: customer.GetId(), LicensePlate: "51C-" + suffix})
	require.NoError(t, err)

	return customer, vehicle
}

// closeOrder completes every line of the order and closes it.
func closeOrder(t *testing.T, api *client.Client, garageID string, order *repairorderv1.RepairOrder) {
	t.Helper()

	for _, line := range order.GetLines() {
		_, err := api.UpdateRepairOrderLine(t.Context(), garageID, order.GetId(), line.GetId(),
			&repairorderv1.RepairOrderLine{Status: repairorderv1.RepairOrderLineStatus_REPAIR_ORDER_LINE_STATUS_COMPLETED})
		require.NoError(t, err)
	}

	_, err := api.CloseRepairOrder(t.Context(), garageID, order.GetId())
	require.NoError(t, err)
}

// Scenario: E2E-CAT-001 (partial): a unit and a part are created with
// base unit, price, category and brand, and the unit name is unique
// ignoring letter case — the API substance; the forms are web-E2E.
func TestCreateUnitAndPart(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garageID := setup(t, "cat-create")

	bottle := fixture.NewUnit(t, owner.API, garageID, "Bottle", "btl")

	_, err := owner.API.CreateUnit(ctx, garageID, "bottle", "b")
	fixture.RequireAPIError(t, err, http.StatusConflict, "")

	category, err := owner.API.CreatePartCategory(ctx, garageID, "Filters", "")
	require.NoError(t, err)

	_, err = owner.API.CreatePart(ctx, garageID, &cataloguev1.Part{Sku: "QA-WATER-500", Name: "Mineral water 500 ml"})
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "")

	part := fixture.NewPart(t, owner.API, garageID, "QA-WATER-500", "Mineral water 500 ml", bottle,
		fixture.WithPrice(6000), fixture.WithBrand("Aquafina"), fixture.WithCategory(category.GetId()))

	got, err := owner.API.GetPart(ctx, garageID, part.GetId())
	require.NoError(t, err)
	assert.Equal(t, "Bottle", got.GetBaseUnitName())
	assert.Equal(t, "btl", got.GetBaseUnitAbbreviation())
	assert.EqualValues(t, 6000, got.GetSalePrice())
	assert.Equal(t, "Filters", got.GetCategoryName())
	assert.Equal(t, "Aquafina", got.GetBrand())
	assert.True(t, got.GetIsActive())

	level, err := owner.API.GetStockLevel(ctx, garageID, part.GetId())
	require.NoError(t, err)
	assert.Equal(t, "0", level.GetQuantityOnHand())
	assert.Equal(t, "btl", level.GetBaseUnitAbbreviation())

	// Clean-up: nothing uses them, so both are deleted.
	deleted, err := owner.API.DeletePart(ctx, garageID, part.GetId(), false)
	require.NoError(t, err)
	assert.Equal(t, commonv1.DeleteOutcome_DELETE_OUTCOME_DELETED, deleted.GetOutcome())

	deleted2, err := owner.API.DeleteUnit(ctx, garageID, bottle.GetId(), false)
	require.NoError(t, err)
	assert.Equal(t, commonv1.DeleteOutcome_DELETE_OUTCOME_DELETED, deleted2.GetOutcome())
}

// Scenario: E2E-CAT-002 (partial): stock is received by the case and
// issued by the can: the case converts to cans, the cost is recorded per
// can, and a repair-order line in cans or in cases deducts the base amount
// — the API substance; the case-and-can display is web-E2E.
func TestReceiveByTheCaseIssueByTheCan(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garageID := setup(t, "cat-receive")
	part, _, caseUnit := beer(t, owner.API, garageID)

	// More than three decimals after conversion is refused, never rounded.
	_, err := owner.API.CreateStockReceipt(ctx, garageID, part.GetId(), "0.0001", caseUnit.GetId(), 0)
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "")

	receipt, err := owner.API.CreateStockReceipt(ctx, garageID, part.GetId(), "10", caseUnit.GetId(), 150_000)
	require.NoError(t, err)
	assert.Equal(t, "240", receipt.GetStockLevel().GetQuantityOnHand())
	assert.Equal(t, "240", receipt.GetTransaction().GetQuantity())
	assert.Equal(t, "10", receipt.GetTransaction().GetEnteredQuantity())
	assert.Equal(t, "case", receipt.GetTransaction().GetUnitAbbreviation())
	assert.Equal(t, "can", receipt.GetTransaction().GetBaseUnitAbbreviation())
	assert.EqualValues(t, 6250, receipt.GetTransaction().GetUnitCost(), "150,000 per case of 24 is 6,250 per can")

	// A unit that is not one of the part's units is refused.
	other := fixture.NewUnit(t, owner.API, garageID, "Crate", "crt")
	_, err = owner.API.CreateStockReceipt(ctx, garageID, part.GetId(), "1", other.GetId(), 0)
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "")

	// 30 cans on a repair order.
	_, vehicle := vehicleFor(t, owner.API, garageID)
	partRef := part.GetId()

	order, err := owner.API.CreateRepairOrder(ctx, garageID, &repairorderv1.RepairOrder{
		VehicleId: vehicle.GetId(),
		Lines: []*repairorderv1.RepairOrderLine{{
			ItemType:  repairorderv1.RepairOrderItemType_REPAIR_ORDER_ITEM_TYPE_PART,
			ItemRefId: &partRef, Description: "Beer", Quantity: "30", UnitPrice: 9000,
		}},
	})
	require.NoError(t, err)
	closeOrder(t, owner.API, garageID, order)
	waitForOnHand(t, owner.API, garageID, part.GetId(), "210")

	// One case on another order: the line keeps the unit, the quantity is
	// the 24 cans derived from it (a quantity sent with the unit is ignored).
	caseID := caseUnit.GetId()

	order2, err := owner.API.CreateRepairOrder(ctx, garageID, &repairorderv1.RepairOrder{
		VehicleId: vehicle.GetId(),
		Lines: []*repairorderv1.RepairOrderLine{{
			ItemType:  repairorderv1.RepairOrderItemType_REPAIR_ORDER_ITEM_TYPE_PART,
			ItemRefId: &partRef, Description: "Beer", Quantity: "999", UnitId: &caseID, EnteredQuantity: "1", UnitPrice: 9000,
		}},
	})
	require.NoError(t, err)
	require.Len(t, order2.GetLines(), 1)
	assert.Equal(t, "24", order2.GetLines()[0].GetQuantity())
	assert.Equal(t, "1", order2.GetLines()[0].GetEnteredQuantity())
	assert.Equal(t, "case", order2.GetLines()[0].GetUnitAbbreviation())
	assert.EqualValues(t, 216_000, order2.GetLines()[0].GetLineTotal(), "24 cans x 9,000")

	closeOrder(t, owner.API, garageID, order2)
	waitForOnHand(t, owner.API, garageID, part.GetId(), "186")
}

// Scenario: E2E-CAT-003 (partial): a quote keeps the price it was saved
// with when the part's selling price changes later, and a part line in a
// packaging unit keeps the unit and derives the base quantity — the API
// substance; the prefill is web-E2E.
func TestQuoteKeepsItsPrice(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garageID := setup(t, "cat-quote")
	part, _, caseUnit := beer(t, owner.API, garageID)
	customer, vehicle := vehicleFor(t, owner.API, garageID)

	partRef, caseID := part.GetId(), caseUnit.GetId()

	quote, err := owner.API.CreateQuote(ctx, garageID, &quotev1.Quote{
		CustomerId: customer.GetId(), VehicleId: vehicle.GetId(),
		Lines: []*quotev1.QuoteLine{
			{ItemType: quotev1.QuoteItemType_QUOTE_ITEM_TYPE_PART, ItemRefId: &partRef, Description: "Beer", Quantity: "1", UnitPrice: part.GetSalePrice()},
			{ItemType: quotev1.QuoteItemType_QUOTE_ITEM_TYPE_PART, ItemRefId: &partRef, Description: "Beer by the case", Quantity: "1", UnitId: &caseID, EnteredQuantity: "2", UnitPrice: 8000},
		},
	})
	require.NoError(t, err)
	require.Len(t, quote.GetLines(), 2)
	assert.EqualValues(t, 9000, quote.GetLines()[0].GetUnitPrice())
	assert.Equal(t, "48", quote.GetLines()[1].GetQuantity(), "2 cases are 48 cans")
	assert.Equal(t, "2", quote.GetLines()[1].GetEnteredQuantity())
	assert.Equal(t, "case", quote.GetLines()[1].GetUnitAbbreviation())
	assert.EqualValues(t, 384_000, quote.GetLines()[1].GetLineTotal(), "48 x 8,000")

	_, err = owner.API.UpdatePart(ctx, garageID, part.GetId(), &cataloguev1.Part{SalePrice: 12_000}, "sale_price")
	require.NoError(t, err)

	reread, err := owner.API.GetQuote(ctx, garageID, quote.GetId())
	require.NoError(t, err)
	assert.EqualValues(t, 9000, reread.GetLines()[0].GetUnitPrice(), "the saved quote keeps its price")

	// A unit on a labor line, or without a typed amount, is refused.
	for name, line := range map[string]*quotev1.QuoteLine{
		"labor with unit": {ItemType: quotev1.QuoteItemType_QUOTE_ITEM_TYPE_LABOR, Description: "Labor", Quantity: "1", UnitId: &caseID, EnteredQuantity: "1"},
		"unit, no amount": {ItemType: quotev1.QuoteItemType_QUOTE_ITEM_TYPE_PART, ItemRefId: &partRef, Description: "Beer", Quantity: "1", UnitId: &caseID},
	} {
		_, err = owner.API.CreateQuote(ctx, garageID, &quotev1.Quote{
			CustomerId: customer.GetId(), VehicleId: vehicle.GetId(), Lines: []*quotev1.QuoteLine{line},
		})
		fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "")

		_ = name
	}
}

// Scenario: E2E-CAT-004: a part's packaging table reports derived and
// overridden prices and rejects invalid tables, leaving the saved table
// unchanged.
func TestPackagingPricesAndInvalidTables(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garageID := setup(t, "cat-packaging")
	part, can, caseUnit := beer(t, owner.API, garageID)

	require.Len(t, part.GetPackagingUnits(), 1)
	assert.Equal(t, "24", part.GetPackagingUnits()[0].GetFactor())
	assert.EqualValues(t, 200_000, part.GetPackagingUnits()[0].GetSalePrice())
	assert.True(t, part.GetPackagingUnits()[0].GetSalePriceOverridden())

	// Without the override the price is the selling price times the factor.
	derived, err := owner.API.SetPartUnits(ctx, garageID, part.GetId(), []*cataloguev1.PackagingUnit{
		{UnitId: caseUnit.GetId(), Factor: "24"},
	})
	require.NoError(t, err)
	assert.EqualValues(t, 216_000, derived.GetPackagingUnits()[0].GetSalePrice())
	assert.False(t, derived.GetPackagingUnits()[0].GetSalePriceOverridden())

	for name, table := range map[string][]*cataloguev1.PackagingUnit{
		"the base unit as packaging":    {{UnitId: can.GetId(), Factor: "1"}},
		"a unit twice":                  {{UnitId: caseUnit.GetId(), Factor: "24"}, {UnitId: caseUnit.GetId(), Factor: "12"}},
		"a zero factor":                 {{UnitId: caseUnit.GetId(), Factor: "0"}},
		"a negative factor":             {{UnitId: caseUnit.GetId(), Factor: "-2"}},
		"a factor that is not a number": {{UnitId: caseUnit.GetId(), Factor: "1/2"}},
	} {
		_, err = owner.API.SetPartUnits(ctx, garageID, part.GetId(), table)
		fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "")

		_ = name
	}

	unchanged, err := owner.API.GetPart(ctx, garageID, part.GetId())
	require.NoError(t, err)
	require.Len(t, unchanged.GetPackagingUnits(), 1)
	assert.EqualValues(t, 216_000, unchanged.GetPackagingUnits()[0].GetSalePrice())

	// The base unit is frozen once the part is packaged or has stock.
	other := fixture.NewUnit(t, owner.API, garageID, "Bottle", "btl")
	_, err = owner.API.UpdatePart(ctx, garageID, part.GetId(), &cataloguev1.Part{BaseUnitId: other.GetId()}, "base_unit_id")
	fixture.RequireAPIError(t, err, http.StatusConflict, "")
}

// Scenario: E2E-CAT-005 (partial): the category tree is three levels
// deep, a category filter includes its sub-categories, and parts filter by
// brand — the API substance; the tree screen is web-E2E.
func TestCategoryTreeAndFilters(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garageID := setup(t, "cat-tree")
	piece := fixture.NewUnit(t, owner.API, garageID, "Piece", "pc")

	oil, err := owner.API.CreatePartCategory(ctx, garageID, "Engine oil", "")
	require.NoError(t, err)

	synthetic, err := owner.API.CreatePartCategory(ctx, garageID, "Synthetic", oil.GetId())
	require.NoError(t, err)

	leaf, err := owner.API.CreatePartCategory(ctx, garageID, "5W-30", synthetic.GetId())
	require.NoError(t, err)

	_, err = owner.API.CreatePartCategory(ctx, garageID, "Too deep", leaf.GetId())
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "")

	filters, err := owner.API.CreatePartCategory(ctx, garageID, "Filter", "")
	require.NoError(t, err)

	// A category is renamed in place, and moved by changing its parent.
	filters, err = owner.API.UpdatePartCategory(ctx, garageID, filters.GetId(), &cataloguev1.PartCategory{Name: "Filters"}, "name")
	require.NoError(t, err)
	assert.Equal(t, "Filters", filters.GetName())

	moved, err := owner.API.UpdatePartCategory(ctx, garageID, leaf.GetId(),
		&cataloguev1.PartCategory{ParentId: new(oil.GetId())}, "parent_id")
	require.NoError(t, err)
	assert.Equal(t, oil.GetId(), moved.GetParentId())

	_, err = owner.API.UpdatePartCategory(ctx, garageID, oil.GetId(),
		&cataloguev1.PartCategory{ParentId: new(leaf.GetId())}, "parent_id")
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "")

	fixture.NewPart(t, owner.API, garageID, "OIL-5W30", "Engine oil 5W-30", piece, fixture.WithCategory(synthetic.GetId()), fixture.WithBrand("Castrol"))
	fixture.NewPart(t, owner.API, garageID, "FLT-01", "Oil filter", piece, fixture.WithCategory(filters.GetId()), fixture.WithBrand("Bosch"))

	skus := func(category, brand string) []string {
		t.Helper()

		query := url.Values{}
		if category != "" {
			query.Set("categoryId", category)
		}

		if brand != "" {
			query.Set("brand", brand)
		}

		levels, err := owner.API.SearchStockLevels(ctx, garageID, query)
		require.NoError(t, err)

		out := make([]string, 0, len(levels))
		for _, l := range levels {
			out = append(out, l.GetSku())
		}

		return out
	}

	assert.ElementsMatch(t, []string{"OIL-5W30", "FLT-01"}, skus("", ""))
	assert.Equal(t, []string{"OIL-5W30"}, skus(oil.GetId(), ""), "Engine oil lists its sub-categories' parts")
	assert.Equal(t, []string{"FLT-01"}, skus(filters.GetId(), ""))
	assert.Equal(t, []string{"OIL-5W30"}, skus("", "Castrol"))
	assert.Empty(t, skus("", "nobrand"))

	// A category used by nothing is deleted; one with a sub-category or a
	// part is deactivated.
	deleted, err := owner.API.DeletePartCategory(ctx, garageID, leaf.GetId(), false)
	require.NoError(t, err)
	assert.Equal(t, commonv1.DeleteOutcome_DELETE_OUTCOME_DELETED, deleted.GetOutcome())

	kept, err := owner.API.DeletePartCategory(ctx, garageID, filters.GetId(), true)
	require.NoError(t, err)
	assert.Equal(t, commonv1.DeleteOutcome_DELETE_OUTCOME_DEACTIVATED, kept.GetOutcome())
}

// png is the smallest content the file store accepts as an image.
func png() []byte { return append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, 32)...) }

// Scenario: E2E-CAT-006 (partial): a part has up to three images, only
// images are accepted, and removing one promotes the next as the
// thumbnail — the API substance; the thumbnails on screen are web-E2E.
func TestPartImages(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garageID := setup(t, "cat-images")
	piece := fixture.NewUnit(t, owner.API, garageID, "Piece", "pc")
	part := fixture.NewPart(t, owner.API, garageID, "WPR-24", "Wiper blade 24 inch", piece)

	var fileIDs []string

	for range 3 {
		file, err := owner.API.UploadPartImage(ctx, garageID, part.GetId(), png(), "image/png")
		require.NoError(t, err)
		assert.NotEmpty(t, file.GetUrl())

		fileIDs = append(fileIDs, file.GetId())

		withImage, err := owner.API.AddPartImage(ctx, garageID, part.GetId(), file.GetId())
		require.NoError(t, err)
		assert.Len(t, withImage.GetImages(), len(fileIDs))
	}

	// A fourth is refused; so is a text file, and an image of another part.
	fourth, err := owner.API.UploadPartImage(ctx, garageID, part.GetId(), png(), "image/png")
	require.NoError(t, err)

	_, err = owner.API.AddPartImage(ctx, garageID, part.GetId(), fourth.GetId())
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "")

	// The image still waiting for a part is deleted, not kept.
	waiting, err := owner.API.DeleteFile(ctx, garageID, fourth.GetId(), false)
	require.NoError(t, err)
	assert.Equal(t, commonv1.DeleteOutcome_DELETE_OUTCOME_DELETED, waiting.GetOutcome())

	_, err = owner.API.UploadPartImage(ctx, garageID, part.GetId(), []byte("not an image"), "text/plain")
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "")

	_, err = owner.API.UploadPartImage(ctx, garageID, "00000000-0000-4000-8000-000000000000", png(), "image/png")
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "")

	// The first image is the thumbnail of the stock list.
	got, err := owner.API.GetPart(ctx, garageID, part.GetId())
	require.NoError(t, err)
	require.Len(t, got.GetImages(), 3)
	assert.Equal(t, fileIDs[0], got.GetImages()[0].GetFileId())

	served, err := owner.API.GetFileURL(ctx, garageID, fileIDs[0])
	require.NoError(t, err)
	assert.Equal(t, got.GetImages()[0].GetUrl(), served.GetUrl(), "a part image is public")

	levels, err := owner.API.ListStockLevels(ctx, garageID)
	require.NoError(t, err)
	require.Len(t, levels, 1)
	assert.Equal(t, got.GetImages()[0].GetUrl(), levels[0].GetThumbnailUrl())

	after, err := owner.API.RemovePartImage(ctx, garageID, part.GetId(), fileIDs[0])
	require.NoError(t, err)
	require.Len(t, after.GetImages(), 2)
	assert.Equal(t, fileIDs[1], after.GetImages()[0].GetFileId(), "the next image becomes the thumbnail")

	levels, err = owner.API.ListStockLevels(ctx, garageID)
	require.NoError(t, err)
	assert.Equal(t, after.GetImages()[0].GetUrl(), levels[0].GetThumbnailUrl())
}

// Scenario: E2E-CAT-007: a unit, a category and a part in use are
// deactivated, not deleted, and stay on what already refers to them; those
// used nowhere are deleted; deactivated ones are not offered and can be
// reactivated.
func TestDeleteOrDeactivate(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garageID := setup(t, "cat-deactivate")
	part, _, caseUnit := beer(t, owner.API, garageID)

	_, err := owner.API.CreateStockReceipt(ctx, garageID, part.GetId(), "1", caseUnit.GetId(), 0)
	require.NoError(t, err)

	// The case unit is used by the part's packaging and a stock movement.
	unitOutcome, err := owner.API.DeleteUnit(ctx, garageID, caseUnit.GetId(), true)
	require.NoError(t, err)
	assert.Equal(t, commonv1.DeleteOutcome_DELETE_OUTCOME_DEACTIVATED, unitOutcome.GetOutcome())

	unitOutcome, err = owner.API.DeleteUnit(ctx, garageID, caseUnit.GetId(), false)
	require.NoError(t, err)
	assert.Equal(t, commonv1.DeleteOutcome_DELETE_OUTCOME_DEACTIVATED, unitOutcome.GetOutcome())

	partOutcome, err := owner.API.DeletePart(ctx, garageID, part.GetId(), false)
	require.NoError(t, err)
	assert.Equal(t, commonv1.DeleteOutcome_DELETE_OUTCOME_DEACTIVATED, partOutcome.GetOutcome())

	// Not offered any more, but still readable where history needs them.
	units, err := owner.API.ListUnits(ctx, garageID, false)
	require.NoError(t, err)

	for _, u := range units {
		assert.NotEqual(t, caseUnit.GetId(), u.GetId())
	}

	parts, err := owner.API.ListParts(ctx, garageID, nil)
	require.NoError(t, err)
	assert.Empty(t, parts)

	stillThere, err := owner.API.GetPart(ctx, garageID, part.GetId())
	require.NoError(t, err)
	assert.False(t, stillThere.GetIsActive())
	require.Len(t, stillThere.GetPackagingUnits(), 1, "the packaging keeps the deactivated unit")

	// Reactivate what was deactivated.
	_, err = owner.API.UpdateUnit(ctx, garageID, caseUnit.GetId(), &cataloguev1.Unit{IsActive: true}, "is_active")
	require.NoError(t, err)

	reactivated, err := owner.API.ActivatePart(ctx, garageID, part.GetId())
	require.NoError(t, err)
	assert.True(t, reactivated.GetIsActive())

	units, err = owner.API.ListUnits(ctx, garageID, false)
	require.NoError(t, err)

	found := false

	for _, u := range units {
		found = found || u.GetId() == caseUnit.GetId()
	}

	assert.True(t, found, "the reactivated unit is offered again")

	// Used nowhere: deleted.
	fresh := fixture.NewUnit(t, owner.API, garageID, "Fresh", "fr")
	freshPart := fixture.NewPart(t, owner.API, garageID, "FRESH-1", "Fresh part", fresh)

	deleted, err := owner.API.DeletePart(ctx, garageID, freshPart.GetId(), false)
	require.NoError(t, err)
	assert.Equal(t, commonv1.DeleteOutcome_DELETE_OUTCOME_DELETED, deleted.GetOutcome())

	_, err = owner.API.GetPart(ctx, garageID, freshPart.GetId())
	fixture.RequireAPIError(t, err, http.StatusNotFound, "")
}

// Scenario: E2E-CAT-008 (partial): the stock-movement Excel lists a
// receipt with both the base amount and the amount entered in the
// packaging unit — the revenue export is checked in the reporting
// scenarios; opening the files is web-E2E.
func TestStockMovementsExcelHasBothUnits(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garageID := setup(t, "cat-excel")
	part, _, caseUnit := beer(t, owner.API, garageID)

	_, err := owner.API.CreateStockReceipt(ctx, garageID, part.GetId(), "10", caseUnit.GetId(), 150_000)
	require.NoError(t, err)

	contentType, body, err := owner.API.DownloadStockMovementsExcel(ctx, garageID)
	require.NoError(t, err)
	assert.Contains(t, contentType, "spreadsheet")

	text := workbookText(t, body)
	for _, want := range []string{"BEER-330", "240", "can", "10", "case", "Entered quantity"} {
		assert.Contains(t, text, ">"+want+"<", "the workbook shows %q", want)
	}
}

// workbookText returns the shared strings and the first sheet's XML of an
// .xlsx workbook, enough to check that values appear without a parser.
func workbookText(t *testing.T, body []byte) string {
	t.Helper()

	archive, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	require.NoError(t, err)

	var out bytes.Buffer

	for _, f := range archive.File {
		if f.Name != "xl/sharedStrings.xml" && f.Name != "xl/worksheets/sheet1.xml" {
			continue
		}

		r, err := f.Open()
		require.NoError(t, err)

		_, err = io.Copy(&out, io.LimitReader(r, 1<<20))
		require.NoError(t, err)
		require.NoError(t, r.Close())
	}

	return out.String()
}

// Scenario: E2E-CAT-009: a member with catalogue read-only can see parts,
// units and categories but change none; a member with catalogue
// add-and-edit can; the stock list needs the inventory permission.
//
//nolint:funlen // one scenario with three roles, read top to bottom
func TestCataloguePermissions(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garageID := setup(t, "cat-perms")
	piece := fixture.NewUnit(t, owner.API, garageID, "Piece", "pc")
	part := fixture.NewPart(t, owner.API, garageID, "PERM-1", "Permission part", piece)

	join := func(label string, permission tenantv1.Permission) *fixture.Actor {
		t.Helper()

		role, err := owner.API.CreateRole(ctx, &tenantv1.Role{Name: "QA " + label, Permissions: []tenantv1.Permission{permission}})
		require.NoError(t, err)

		group, err := owner.API.CreateStaffGroup(ctx, garageID, &tenantv1.StaffGroup{Name: "QA " + label, RoleId: role.GetId()})
		require.NoError(t, err)

		member := fixture.NewVerifiedOwner(t, "cat-perms-"+label)

		candidate, err := owner.API.LookupStaffCandidate(ctx, garageID, member.Email)
		require.NoError(t, err)

		_, err = owner.API.CreateInvitation(ctx, garageID, candidate.GetUserId(), []string{group.GetId()})
		require.NoError(t, err)

		invitations, err := member.API.ListMyInvitations(ctx)
		require.NoError(t, err)
		require.NotEmpty(t, invitations)

		_, err = member.API.AcceptInvitation(ctx, invitations[0].GetId())
		require.NoError(t, err)

		return member
	}

	t.Run("read-only", func(t *testing.T) {
		t.Parallel()

		reader := join("reader", tenantv1.Permission_PERMISSION_CATALOGUE_READ)

		parts, err := reader.API.ListParts(ctx, garageID, nil)
		require.NoError(t, err)
		require.Len(t, parts, 1)
		assert.NotEmpty(t, parts[0].GetSku())

		_, err = reader.API.ListUnits(ctx, garageID, false)
		require.NoError(t, err)

		_, err = reader.API.ListPartCategories(ctx, garageID, false)
		require.NoError(t, err)

		_, err = reader.API.CreatePart(ctx, garageID, &cataloguev1.Part{Sku: "X", Name: "X", BaseUnitId: piece.GetId()})
		fixture.RequireAPIError(t, err, http.StatusForbidden, "")

		_, err = reader.API.CreateUnit(ctx, garageID, "X", "x")
		fixture.RequireAPIError(t, err, http.StatusForbidden, "")

		_, err = reader.API.CreatePartCategory(ctx, garageID, "X", "")
		fixture.RequireAPIError(t, err, http.StatusForbidden, "")

		_, err = reader.API.UpdatePart(ctx, garageID, part.GetId(), &cataloguev1.Part{Name: "Y"}, "name")
		fixture.RequireAPIError(t, err, http.StatusForbidden, "")

		_, err = reader.API.DeletePart(ctx, garageID, part.GetId(), false)
		fixture.RequireAPIError(t, err, http.StatusForbidden, "")

		_, err = reader.API.UploadPartImage(ctx, garageID, part.GetId(), png(), "image/png")
		fixture.RequireAPIError(t, err, http.StatusForbidden, "")

		// The stock list belongs to Inventory.
		_, err = reader.API.ListStockLevels(ctx, garageID)
		fixture.RequireAPIError(t, err, http.StatusForbidden, "")
	})

	t.Run("add and edit", func(t *testing.T) {
		t.Parallel()

		editor := join("editor", tenantv1.Permission_PERMISSION_CATALOGUE_WRITE)

		unit, err := editor.API.CreateUnit(ctx, garageID, "Editor unit", "eu")
		require.NoError(t, err)

		category, err := editor.API.CreatePartCategory(ctx, garageID, "Editor category", "")
		require.NoError(t, err)

		created, err := editor.API.CreatePart(ctx, garageID, &cataloguev1.Part{
			Sku: "EDIT-1", Name: "Editor part", BaseUnitId: unit.GetId(), CategoryId: new(category.GetId()),
		})
		require.NoError(t, err)

		updated, err := editor.API.UpdatePart(ctx, garageID, created.GetId(), &cataloguev1.Part{Name: "Edited"}, "name")
		require.NoError(t, err)
		assert.Equal(t, "Edited", updated.GetName())

		file, err := editor.API.UploadPartImage(ctx, garageID, created.GetId(), png(), "image/png")
		require.NoError(t, err)

		_, err = editor.API.AddPartImage(ctx, garageID, created.GetId(), file.GetId())
		require.NoError(t, err)

		// Catalogue write does not open the stock.
		_, err = editor.API.ListStockLevels(ctx, garageID)
		fixture.RequireAPIError(t, err, http.StatusForbidden, "")
	})

	t.Run("default technician and advisor can see the catalogue", func(t *testing.T) {
		t.Parallel()

		groups, err := owner.API.ListStaffGroups(ctx, garageID)
		require.NoError(t, err)

		for name, label := range map[string]string{"Technician": "technician", "Service Advisor": "advisor"} {
			var groupID string

			for _, g := range groups {
				if g.GetName() == name {
					groupID = g.GetId()
				}
			}

			if groupID == "" {
				continue
			}

			member := fixture.NewVerifiedOwner(t, "cat-default-"+label)

			candidate, err := owner.API.LookupStaffCandidate(ctx, garageID, member.Email)
			require.NoError(t, err)

			_, err = owner.API.CreateInvitation(ctx, garageID, candidate.GetUserId(), []string{groupID})
			require.NoError(t, err)

			invitations, err := member.API.ListMyInvitations(ctx)
			require.NoError(t, err)
			require.NotEmpty(t, invitations)

			_, err = member.API.AcceptInvitation(ctx, invitations[0].GetId())
			require.NoError(t, err)

			parts, err := member.API.ListParts(ctx, garageID, nil)
			require.NoError(t, err, name)
			assert.NotEmpty(t, parts, name)

			_, err = member.API.CreateUnit(ctx, garageID, "Nope "+name, "n")
			fixture.RequireAPIError(t, err, http.StatusForbidden, "")
		}
	})
}
