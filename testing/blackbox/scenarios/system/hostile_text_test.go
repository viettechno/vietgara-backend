package system_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

// TestHostileTextIsStoredAndSearchedAsText: what a user types is data. SQL
// and HTML are kept exactly as typed, the search treats % _ ' and \ as the
// characters they are, not as wildcards or syntax, and letters with Vietnamese
// diacritics or an emoji survive and match whatever the case.
//
// Scenario: E2E-SYS-010: names with SQL, HTML, wildcard and Unicode text
// round-trip unchanged and are found by a search for exactly that text.
func TestHostileTextIsStoredAndSearchedAsText(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	business := fixture.NewBusiness(t, "hostile")
	api := business.Owner.API

	names := []string{
		"Nguyễn Văn Ánh",
		"O'Brien; DROP TABLE customers;--",
		"<script>alert(1)</script>",
		"100% Auto_Parts",
		"Trần Thị Bé 🚗",
		`back\slash`,
	}

	ids := map[string]string{}

	for index, name := range names {
		phone := "0915" + string(rune('1'+index)) + "00000"

		customer, err := api.CreateCustomer(ctx, business.GarageID, &customerv1.Customer{FullName: name, Phone: &phone})
		require.NoError(t, err, name)

		got, err := api.GetCustomer(ctx, business.GarageID, customer.GetId())
		require.NoError(t, err)
		assert.Equal(t, name, got.GetFullName(), "stored exactly as typed")

		ids[name] = customer.GetId()
	}

	// Each query finds exactly the customer whose name holds that text; the
	// garage also holds the fixture's own customer, which none of them match.
	for query, want := range map[string]string{
		"Nguyễn":     "Nguyễn Văn Ánh",
		"NGUYỄN":     "Nguyễn Văn Ánh",
		"ánh":        "Nguyễn Văn Ánh",
		"'":          "O'Brien; DROP TABLE customers;--",
		"DROP TABLE": "O'Brien; DROP TABLE customers;--",
		"<script>":   "<script>alert(1)</script>",
		"%":          "100% Auto_Parts",
		"_":          "100% Auto_Parts",
		"Auto_P":     "100% Auto_Parts",
		"🚗":          "Trần Thị Bé 🚗",
		`\`:          `back\slash`,
	} {
		found, err := api.SearchCustomers(ctx, business.GarageID, query)
		require.NoError(t, err, query)
		require.Len(t, found, 1, "query %q finds one customer", query)
		assert.Equal(t, ids[want], found[0].GetId(), "query %q", query)
	}

	t.Run("a wildcard does not stand for other characters", func(t *testing.T) {
		found, err := api.SearchCustomers(ctx, business.GarageID, "AutoXParts")
		require.NoError(t, err)
		assert.Empty(t, found)
	})

	t.Run("the table is still there", func(t *testing.T) {
		all, err := api.SearchCustomers(ctx, business.GarageID, "")
		require.NoError(t, err)
		assert.Len(t, all, len(names)+1)
	})
}
