// Package inventory_test covers Inventory at API level: the stock levels
// of the Catalogue's parts, goods receipts, the low-stock rule, search,
// the deactivation of a part that has history, the movement report with
// direction filters, automatic deduction on close, and the free-text part
// line that deducts nothing. Units, thresholds and empty states on screen
// are web-E2E; the part itself (units, categories, images) is covered by
// the catalogue scenarios.
package inventory_test

import (
	"fmt"
	"net/http"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	cataloguev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/catalogue/v1"
	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"
	inventoryv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/inventory/v1"
	repairorderv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/repairorder/v1"
	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/client"
	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

var seq atomic.Int64

// setup returns a fresh owner and garage for inventory scenarios.
func setup(t *testing.T, label string) (*fixture.Actor, *tenantv1.Garage) {
	t.Helper()

	owner := fixture.NewVerifiedOwner(t, label)

	garage, err := owner.API.CreateGarage(t.Context(), &tenantv1.Garage{Name: "Blackbox " + label})
	require.NoError(t, err)

	return owner, garage
}

func sku() string { return fmt.Sprintf("OIL-%05d", seq.Add(1)%100000) }

// litre adds the unit most inventory scenarios stock their parts in.
func litre(t *testing.T, owner *fixture.Actor, garageID string) *cataloguev1.Unit {
	t.Helper()

	return fixture.NewUnit(t, owner.API, garageID, "Litre", "L")
}

// Scenario: E2E-INV-001 (partial): a part is added to the catalogue and
// shows in the stock list with nothing on hand — the API substance; the
// row and cross-garage hiding are web-E2E (isolation follows the same
// garage-scoping as customers).
func TestAddCatalogueItem(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage := setup(t, "inv-add")

	part := fixture.NewPart(t, owner.API, garage.GetId(), sku(), "Engine oil 5W-30", litre(t, owner, garage.GetId()),
		fixture.WithThreshold("5"))

	level, err := owner.API.GetStockLevel(ctx, garage.GetId(), part.GetId())
	require.NoError(t, err)
	assert.Equal(t, "0", level.GetQuantityOnHand())
	assert.Equal(t, "5", level.GetReorderThreshold())
	assert.Equal(t, "L", level.GetBaseUnitAbbreviation())
	assert.True(t, level.GetLowStock(), "0 on hand is at or below the threshold")

	listed, err := owner.API.ListStockLevels(ctx, garage.GetId())
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Equal(t, part.GetId(), listed[0].GetPartId())
}

// Scenario: E2E-INV-002 (partial): the SKU is unique per garage, and
// part fields are validated — the API guards behind the form.
func TestItemSKUUniqueAndValidation(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage := setup(t, "inv-valid")
	unit := litre(t, owner, garage.GetId())

	code := sku()
	fixture.NewPart(t, owner.API, garage.GetId(), code, "Engine oil", unit)

	// Duplicate SKU is refused.
	_, err := owner.API.CreatePart(ctx, garage.GetId(), &cataloguev1.Part{Sku: code, Name: "Different oil", BaseUnitId: unit.GetId()})
	fixture.RequireAPIError(t, err, http.StatusConflict, "")

	// Empty SKU, empty name and a missing base unit are refused.
	for name, part := range map[string]*cataloguev1.Part{
		"no SKU":       {Name: "Air filter", BaseUnitId: unit.GetId()},
		"no name":      {Sku: "FILTER-01", BaseUnitId: unit.GetId()},
		"no base unit": {Sku: "FILTER-01", Name: "Air filter"},
	} {
		_, err = owner.API.CreatePart(ctx, garage.GetId(), part)
		fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "")

		_ = name
	}

	saved := fixture.NewPart(t, owner.API, garage.GetId(), "FILTER-01", "Air filter", unit)

	outcome, err := owner.API.DeletePart(ctx, garage.GetId(), saved.GetId(), false)
	require.NoError(t, err)
	assert.Equal(t, "DELETE_OUTCOME_DELETED", outcome.GetOutcome().String(), "nothing refers to it yet")
}

// Scenario: E2E-INV-003 (partial): a goods receipt increases stock on
// hand — asserted at API level; the tab badge is web-E2E.
func TestGoodsReceiptIncreasesStock(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage := setup(t, "inv-receipt")

	part := fixture.NewPart(t, owner.API, garage.GetId(), sku(), "Engine oil 5W-30", litre(t, owner, garage.GetId()),
		fixture.WithThreshold("5"))

	first, err := owner.API.ReceiveStock(ctx, garage.GetId(), part.GetId(), "10", "", 150000)
	require.NoError(t, err)
	assert.Equal(t, inventoryv1.GoodsReceiptStatus_GOODS_RECEIPT_STATUS_COMPLETED, first.GetStatus())
	assert.Equal(t, "10", first.GetLines()[0].GetQuantity())
	assert.EqualValues(t, 150000, first.GetLines()[0].GetUnitCost())

	level, err := owner.API.GetStockLevel(ctx, garage.GetId(), part.GetId())
	require.NoError(t, err)
	assert.Equal(t, "10", level.GetQuantityOnHand())

	// Unit cost is optional per receipt.
	_, err = owner.API.ReceiveStock(ctx, garage.GetId(), part.GetId(), "5", "", 0)
	require.NoError(t, err)

	level, err = owner.API.GetStockLevel(ctx, garage.GetId(), part.GetId())
	require.NoError(t, err)
	assert.Equal(t, "15", level.GetQuantityOnHand())

	movements, err := owner.API.ListStockTransactions(ctx, garage.GetId())
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(movements), 2)
}

// Scenario: E2E-INV-004 (partial): the low-stock rule (on hand at or
// below the threshold) — asserted at API level through the stock level
// and its filter; the badge and highlight are web-E2E.
func TestLowStockThreshold(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage := setup(t, "inv-lowstock")

	part := fixture.NewPart(t, owner.API, garage.GetId(), sku(), "Engine oil", litre(t, owner, garage.GetId()),
		fixture.WithThreshold("5"))

	_, err := owner.API.ReceiveStock(ctx, garage.GetId(), part.GetId(), "15", "", 0)
	require.NoError(t, err)

	for _, tc := range []struct {
		threshold string
		wantLow   bool
	}{
		{"20", true},    // 15 <= 20
		{"15", true},    // the boundary is still low
		{"14.5", false}, // above the threshold
		{"5", false},
	} {
		_, err = owner.API.UpdatePart(ctx, garage.GetId(), part.GetId(),
			&cataloguev1.Part{ReorderThreshold: tc.threshold}, "reorder_threshold")
		require.NoError(t, err)

		level, err := owner.API.GetStockLevel(ctx, garage.GetId(), part.GetId())
		require.NoError(t, err)
		assert.Equal(t, tc.wantLow, level.GetLowStock(), "threshold %s", tc.threshold)

		low, err := owner.API.SearchStockLevels(ctx, garage.GetId(), url.Values{"lowStock": {"true"}})
		require.NoError(t, err)
		assert.Equal(t, tc.wantLow, len(low) == 1, "low-stock filter at threshold %s", tc.threshold)
	}
}

// Scenario: E2E-INV-005 (partial): the stock list is searched by SKU or
// name — the API substance; the empty state is web-E2E.
func TestSearchItems(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage := setup(t, "inv-search")
	unit := litre(t, owner, garage.GetId())

	fixture.NewPart(t, owner.API, garage.GetId(), sku(), "Engine oil 5W-30", unit)
	fixture.NewPart(t, owner.API, garage.GetId(), "FILTER-01", "Air filter", unit)

	for _, query := range []string{"OIL", "Engine", "FILTER"} {
		found, err := owner.API.SearchStockLevels(ctx, garage.GetId(), url.Values{"query": {query}})
		require.NoError(t, err, "query %q", query)
		require.NotEmpty(t, found, "query %q", query)
	}

	none, err := owner.API.SearchStockLevels(ctx, garage.GetId(), url.Values{"query": {"no-such-item-zzz"}})
	require.NoError(t, err)
	assert.Empty(t, none)
}

// Scenario: E2E-INV-006 (partial): a part with history is edited and
// then deactivated, not deleted, so its past transactions stay — the API
// substance; the confirm copy is web-E2E.
func TestEditAndDeleteItem(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage := setup(t, "inv-edit")
	piece := fixture.NewUnit(t, owner.API, garage.GetId(), "Piece", "pc")

	part := fixture.NewPart(t, owner.API, garage.GetId(), "TEST-PART-01", "QA Test Part", piece)

	_, err := owner.API.ReceiveStock(ctx, garage.GetId(), part.GetId(), "3", "", 0)
	require.NoError(t, err)

	renamed, err := owner.API.UpdatePart(ctx, garage.GetId(), part.GetId(), &cataloguev1.Part{Name: "QA Test Part Renamed"}, "name")
	require.NoError(t, err)
	assert.Equal(t, "QA Test Part Renamed", renamed.GetName())

	// The dry run says what a delete would do without doing it.
	dry, err := owner.API.DeletePart(ctx, garage.GetId(), part.GetId(), true)
	require.NoError(t, err)
	assert.Equal(t, "DELETE_OUTCOME_DEACTIVATED", dry.GetOutcome().String())

	done, err := owner.API.DeletePart(ctx, garage.GetId(), part.GetId(), false)
	require.NoError(t, err)
	assert.Equal(t, "DELETE_OUTCOME_DEACTIVATED", done.GetOutcome().String())

	// The default stock list no longer offers it; the history stays.
	active, err := owner.API.ListStockLevels(ctx, garage.GetId())
	require.NoError(t, err)
	assert.Empty(t, active)

	all, err := owner.API.SearchStockLevels(ctx, garage.GetId(), url.Values{"includeInactive": {"true"}})
	require.NoError(t, err)
	require.Len(t, all, 1)
	assert.False(t, all[0].GetIsActive())
	assert.Equal(t, "3", all[0].GetQuantityOnHand())

	movements, err := owner.API.ListStockTransactions(ctx, garage.GetId())
	require.NoError(t, err)
	require.NotEmpty(t, movements)
	assert.Equal(t, "TEST-PART-01", movements[0].GetPartSku())

	// A deactivated part cannot be received into until it is reactivated.
	_, err = owner.API.ReceiveStock(ctx, garage.GetId(), part.GetId(), "1", "", 0)
	fixture.RequireAPIError(t, err, http.StatusConflict, "")

	reactivated, err := owner.API.ActivatePart(ctx, garage.GetId(), part.GetId())
	require.NoError(t, err)
	assert.True(t, reactivated.GetIsActive())
}

// Scenario: E2E-INV-007 (partial): the stock movement report filters by
// direction — the API substance; the tab is web-E2E.
func TestStockMovementReportFilters(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage := setup(t, "inv-report")

	part := fixture.NewPart(t, owner.API, garage.GetId(), sku(), "Engine oil", litre(t, owner, garage.GetId()))

	_, err := owner.API.ReceiveStock(ctx, garage.GetId(), part.GetId(), "10", "", 0)
	require.NoError(t, err)

	closeOrderWithPart(t, owner.API, garage.GetId(), part.GetId(), "4")

	// The close deduction arrives through the async outbox relay: wait
	// for an OUT transaction before asserting the report contents.
	waitForTransactions(t, owner.API, garage.GetId(),
		inventoryv1.StockTransactionType_STOCK_TRANSACTION_TYPE_OUT)

	all, err := owner.API.ListStockTransactions(ctx, garage.GetId())
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(all), 2)

	ins, err := owner.API.ListStockTransactionsByType(ctx, garage.GetId(),
		inventoryv1.StockTransactionType_STOCK_TRANSACTION_TYPE_IN)
	require.NoError(t, err)
	require.NotEmpty(t, ins)

	for _, tx := range ins {
		assert.Equal(t, inventoryv1.StockTransactionType_STOCK_TRANSACTION_TYPE_IN, tx.GetType())
	}

	outs := waitForTransactions(t, owner.API, garage.GetId(),
		inventoryv1.StockTransactionType_STOCK_TRANSACTION_TYPE_OUT)

	for _, tx := range outs {
		assert.Equal(t, inventoryv1.StockTransactionType_STOCK_TRANSACTION_TYPE_OUT, tx.GetType())
	}
}

// Scenario: E2E-INV-008 (partial): closing a repair order with part
// lines deducts stock — asserted at API level on both sides.
func TestCloseDeductsStock(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage := setup(t, "inv-deduct")

	part := fixture.NewPart(t, owner.API, garage.GetId(), sku(), "Engine oil", litre(t, owner, garage.GetId()))

	_, err := owner.API.ReceiveStock(ctx, garage.GetId(), part.GetId(), "10", "", 0)
	require.NoError(t, err)

	closeOrderWithPart(t, owner.API, garage.GetId(), part.GetId(), "4")

	// Deduction arrives through the async outbox relay; poll for it.
	waitForOnHand(t, owner.API, garage.GetId(), part.GetId(), "6")
}

// Scenario: E2E-INV-009 (partial): a part line without a catalogue part
// deducts nothing on close — asserted at API level.
func TestCloseWithoutCatalogueItemDeductsNothing(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage := setup(t, "inv-nodeduct")

	part := fixture.NewPart(t, owner.API, garage.GetId(), sku(), "Engine oil", litre(t, owner, garage.GetId()))

	_, err := owner.API.ReceiveStock(ctx, garage.GetId(), part.GetId(), "10", "", 0)
	require.NoError(t, err)

	vehicle := vehicleFor(t, owner.API, garage.GetId())

	order, err := owner.API.CreateRepairOrder(ctx, garage.GetId(), &repairorderv1.RepairOrder{
		VehicleId: vehicle.GetId(),
		Lines: []*repairorderv1.RepairOrderLine{{
			ItemType:    repairorderv1.RepairOrderItemType_REPAIR_ORDER_ITEM_TYPE_PART,
			Description: "Brake pad set", Quantity: "1", UnitPrice: 450000,
		}},
	})
	require.NoError(t, err)

	completed, err := owner.API.UpdateRepairOrderLine(ctx, garage.GetId(), order.GetId(), order.GetLines()[0].GetId(),
		&repairorderv1.RepairOrderLine{Status: repairorderv1.RepairOrderLineStatus_REPAIR_ORDER_LINE_STATUS_COMPLETED})
	require.NoError(t, err)
	assert.NotNil(t, completed)

	_, err = owner.API.CloseRepairOrder(ctx, garage.GetId(), order.GetId())
	require.NoError(t, err)

	after, err := owner.API.GetStockLevel(ctx, garage.GetId(), part.GetId())
	require.NoError(t, err)
	assert.Equal(t, "10", after.GetQuantityOnHand())
}

// waitForOnHand polls the part until its on-hand quantity reads want
// (the close deduction arrives through the async outbox relay).
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
			t.Fatalf("on hand is %q, want %q after close", level.GetQuantityOnHand(), want)
		}

		time.Sleep(500 * time.Millisecond)
	}
}

// waitForTransactions polls the movement report until the direction
// filter returns at least one transaction.
func waitForTransactions(t *testing.T, api *client.Client, garageID string, direction inventoryv1.StockTransactionType) []*inventoryv1.StockTransaction {
	t.Helper()

	deadline := time.Now().Add(60 * time.Second)

	for {
		txs, err := api.ListStockTransactionsByType(t.Context(), garageID, direction)
		require.NoError(t, err)

		if len(txs) > 0 {
			return txs
		}

		if time.Now().After(deadline) {
			t.Fatalf("no %v transactions after close", direction)
		}

		time.Sleep(500 * time.Millisecond)
	}
}

// vehicleFor creates a customer with a vehicle in the garage.
func vehicleFor(t *testing.T, api *client.Client, garageID string) *customerv1.Vehicle {
	t.Helper()

	ctx := t.Context()
	suffix := fmt.Sprintf("%05d", seq.Add(1)%100000)
	phone := "09130" + suffix

	customer, err := api.CreateCustomer(ctx, garageID, &customerv1.Customer{
		FullName: "QA Customer Stock", Phone: &phone, PersonalDataConsent: true,
	})
	require.NoError(t, err)

	vehicle, err := api.CreateVehicle(ctx, garageID, &customerv1.Vehicle{
		CustomerId: customer.GetId(), LicensePlate: "51S-" + suffix,
	})
	require.NoError(t, err)

	return vehicle
}

// closeOrderWithPart creates and closes an order with one part line
// linked to partID, quantity qty (in the base unit).
func closeOrderWithPart(t *testing.T, api *client.Client, garageID, partID, qty string) {
	t.Helper()

	ctx := t.Context()
	vehicle := vehicleFor(t, api, garageID)
	partRef := partID

	order, err := api.CreateRepairOrder(ctx, garageID, &repairorderv1.RepairOrder{
		VehicleId: vehicle.GetId(),
		Lines: []*repairorderv1.RepairOrderLine{{
			ItemType:  repairorderv1.RepairOrderItemType_REPAIR_ORDER_ITEM_TYPE_PART,
			ItemRefId: &partRef, Description: "Engine oil 5W-30", Quantity: qty, UnitPrice: 150000,
		}},
	})
	require.NoError(t, err)

	_, err = api.UpdateRepairOrderLine(ctx, garageID, order.GetId(), order.GetLines()[0].GetId(),
		&repairorderv1.RepairOrderLine{Status: repairorderv1.RepairOrderLineStatus_REPAIR_ORDER_LINE_STATUS_COMPLETED})
	require.NoError(t, err)

	_, err = api.CloseRepairOrder(ctx, garageID, order.GetId())
	require.NoError(t, err)
}
