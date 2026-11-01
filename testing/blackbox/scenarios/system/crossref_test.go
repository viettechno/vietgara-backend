package system_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"
	quotev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/quote/v1"
	repairorderv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/repairorder/v1"
	supplierv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/supplier/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

// TestAnotherOwnersRecordsAreNotReachableByTheirIDs: a member of one garage
// who learns the id of a record of another owner's garage (a leaked URL, a
// guess) gets "not found" for it under their own garage, and cannot attach
// it to their own records. This is the application-level check that row
// security alone does not give: the record is looked up in the garage the
// request names.
//
// Scenario: E2E-SYS-002: a record id of another garage is not found under
// mine, on reads, changes, deletes and references.
func TestAnotherOwnersRecordsAreNotReachableByTheirIDs(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	mine := fixture.NewBusiness(t, "xref-mine")
	theirs := fixture.NewBusiness(t, "xref-theirs")

	quote := theirs.DraftQuote(t)
	order := theirs.OpenOrder(t)
	settlement := theirs.Settlement(t)

	supplier, err := theirs.Owner.API.CreateSupplier(ctx, theirs.GarageID, &supplierv1.Supplier{Name: "QA Supplier Other"})
	require.NoError(t, err)

	base := "/api/v1/garages/" + mine.GarageID

	records := map[string]string{
		"customers":     theirs.CustomerID,
		"vehicles":      theirs.VehicleID,
		"quotes":        quote.GetId(),
		"repair-orders": order.GetId(),
		"settlements":   settlement.GetId(),
		"suppliers":     supplier.GetId(),
	}

	// A valid edit of each collection, so the request is refused for the
	// record and not for its mask.
	patches := map[string]struct{ mask, body string }{
		"customers":     {"address", `{"address":"1 Elsewhere"}`},
		"vehicles":      {"model", `{"model":"Elsewhere"}`},
		"quotes":        {"notes", `{"notes":"Elsewhere"}`},
		"repair-orders": {"notes", `{"notes":"Elsewhere"}`},
		"settlements":   {"invoice_requested", `{"invoiceRequested":false}`},
		"suppliers":     {"name", `{"name":"Elsewhere"}`},
	}

	t.Run("reads, changes and deletes are not found", func(t *testing.T) {
		for collection, id := range records {
			path := base + "/" + collection + "/" + id

			for _, method := range []string{http.MethodGet, http.MethodPatch, http.MethodDelete} {
				if method != http.MethodGet && (collection == "repair-orders" || collection == "settlements") && method == http.MethodDelete {
					continue // these have no delete
				}

				body := []byte(nil)

				if method == http.MethodPatch {
					edit := patches[collection]
					path = fmt.Sprintf("%s?updateMask=%s", path, edit.mask)
					body = []byte(edit.body)
				}

				probe, probeErr := mine.Owner.API.Probe(ctx, method, path, body)
				require.NoError(t, probeErr)

				if probe.Status == http.StatusMethodNotAllowed || probe.Status == http.StatusNotImplemented {
					continue // the collection does not offer this method
				}

				assert.Equal(t, http.StatusNotFound, probe.Status, "%s %s: %s", method, path, probe.Body)
			}
		}
	})

	t.Run("documents and histories are not found", func(t *testing.T) {
		for _, path := range []string{
			base + "/quotes/" + quote.GetId() + "/pdf",
			base + "/repair-orders/" + order.GetId() + "/pdf",
			base + "/repair-orders/" + order.GetId() + "/stock-issue",
			base + "/settlements/" + settlement.GetId() + "/receipt-pdf",
			base + "/vehicles/" + theirs.VehicleID + "/history",
		} {
			probe, probeErr := mine.Owner.API.Probe(ctx, http.MethodGet, path, nil)
			require.NoError(t, probeErr)
			assert.Equal(t, http.StatusNotFound, probe.Status, "GET %s: %s", path, probe.Body)
		}

		probe, probeErr := mine.Owner.API.Probe(ctx, http.MethodPost, base+"/settlements/"+settlement.GetId()+"/payments",
			[]byte(`{"amount":"1000","method":"PAYMENT_METHOD_CASH"}`))
		require.NoError(t, probeErr)
		assert.NotEqual(t, http.StatusOK, probe.Status, "a payment must not land on another garage's settlement: %s", probe.Body)
	})

	t.Run("records cannot be attached to mine", func(t *testing.T) {
		_, err := mine.Owner.API.CreateVehicle(ctx, mine.GarageID, &customerv1.Vehicle{CustomerId: theirs.CustomerID, LicensePlate: "51Z-99991"})
		require.Error(t, err, "a vehicle for another garage's customer")

		_, err = mine.Owner.API.CreateQuote(ctx, mine.GarageID, &quotev1.Quote{
			CustomerId: theirs.CustomerID, VehicleId: theirs.VehicleID,
			Lines: []*quotev1.QuoteLine{{ItemType: quotev1.QuoteItemType_QUOTE_ITEM_TYPE_LABOR, Description: "x", Quantity: "1", UnitPrice: 1}},
		})
		require.Error(t, err, "a quote for another garage's customer and vehicle")

		_, err = mine.Owner.API.CreateQuote(ctx, mine.GarageID, &quotev1.Quote{
			CustomerId: mine.CustomerID, VehicleId: theirs.VehicleID,
			Lines: []*quotev1.QuoteLine{{ItemType: quotev1.QuoteItemType_QUOTE_ITEM_TYPE_LABOR, Description: "x", Quantity: "1", UnitPrice: 1}},
		})
		require.Error(t, err, "a quote for my customer with another garage's vehicle")

		_, err = mine.Owner.API.CreateRepairOrder(ctx, mine.GarageID, &repairorderv1.RepairOrder{
			VehicleId: theirs.VehicleID,
			Lines: []*repairorderv1.RepairOrderLine{{
				ItemType: repairorderv1.RepairOrderItemType_REPAIR_ORDER_ITEM_TYPE_LABOR, Description: "x", Quantity: "1", UnitPrice: 1,
			}},
		})
		require.Error(t, err, "a repair order for another garage's vehicle")

		_, err = mine.Owner.API.WithIdempotencyKey(uuid.NewString()).CreateSettlement(ctx, mine.GarageID, order.GetId())
		require.Error(t, err, "a settlement of another garage's repair order")
	})

	t.Run("their records are untouched", func(t *testing.T) {
		got, err := theirs.Owner.API.GetCustomer(ctx, theirs.GarageID, theirs.CustomerID)
		require.NoError(t, err)
		assert.Equal(t, "QA Customer xref-theirs", got.GetFullName())

		vehicles, err := theirs.Owner.API.ListVehicles(ctx, theirs.GarageID)
		require.NoError(t, err)
		assert.Len(t, vehicles, 1, "no vehicle was added for their customer")
	})
}
