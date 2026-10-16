package fixture

import (
	"testing"

	"github.com/stretchr/testify/require"
	cataloguev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/catalogue/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/client"
)

// NewUnit adds a unit of measure to garageID.
func NewUnit(tb testing.TB, api *client.Client, garageID, name, abbreviation string) *cataloguev1.Unit {
	tb.Helper()

	unit, err := api.CreateUnit(tb.Context(), garageID, name, abbreviation)
	require.NoError(tb, err)

	return unit
}

// NewPart adds a part with baseUnit to garageID; options adjust the
// request (price, brand, category, threshold...) before it is sent.
func NewPart(
	tb testing.TB, api *client.Client, garageID, sku, name string, baseUnit *cataloguev1.Unit, options ...func(*cataloguev1.Part),
) *cataloguev1.Part {
	tb.Helper()

	part := &cataloguev1.Part{Sku: sku, Name: name, BaseUnitId: baseUnit.GetId()}
	for _, option := range options {
		option(part)
	}

	created, err := api.CreatePart(tb.Context(), garageID, part)
	require.NoError(tb, err)

	return created
}

// WithPrice sets the selling price of a new part.
func WithPrice(price int64) func(*cataloguev1.Part) {
	return func(p *cataloguev1.Part) { p.SalePrice = price }
}

// WithThreshold sets the reorder threshold of a new part.
func WithThreshold(threshold string) func(*cataloguev1.Part) {
	return func(p *cataloguev1.Part) { p.ReorderThreshold = threshold }
}

// WithBrand sets the brand of a new part.
func WithBrand(brand string) func(*cataloguev1.Part) {
	return func(p *cataloguev1.Part) { p.Brand = brand }
}

// WithCategory puts a new part in a category.
func WithCategory(categoryID string) func(*cataloguev1.Part) {
	return func(p *cataloguev1.Part) { p.CategoryId = &categoryID }
}
