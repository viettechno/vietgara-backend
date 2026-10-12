// Package inventory_test covers Inventory at API level: the catalogue,
// goods receipts, the low-stock badge rule, search, edit/delete with
// kept history, the movement report with direction filters, automatic
// deduction on close, and the free-text part line that deducts nothing.
// Units, thresholds and empty states on screen are web-E2E.
package inventory_test

import (
	"fmt"
	"net/http"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// Scenario: E2E-INV-001 (partial): a part code is added to the
// catalogue — the API substance; the row and cross-garage hiding are
// web-E2E (isolation follows the same garage-scoping as customers).
func TestAddCatalogueItem(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage := setup(t, "inv-add")

	item, err := owner.API.CreateItem(ctx, garage.GetId(), &inventoryv1.Item{
		Sku: sku(), Name: "Engine oil 5W-30", Unit: "L", ReorderThreshold: 5,
	})
	require.NoError(t, err)
	assert.Equal(t, "0", item.GetQuantityOnHand())
	assert.EqualValues(t, 5, item.GetReorderThreshold())

	got, err := owner.API.GetItem(ctx, garage.GetId(), item.GetId())
	require.NoError(t, err)
	assert.Equal(t, item.GetId(), got.GetId())

	listed, err := owner.API.ListItems(ctx, garage.GetId())
	require.NoError(t, err)
	require.NotEmpty(t, listed)
}

// Scenario: E2E-INV-002 (partial): the SKU is unique per garage, and
// item fields are validated — the API guards behind the form.
func TestItemSKUUniqueAndValidation(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage := setup(t, "inv-valid")

	code := sku()

	_, err := owner.API.CreateItem(ctx, garage.GetId(), &inventoryv1.Item{Sku: code, Name: "Engine oil"})
	require.NoError(t, err)

	// Duplicate SKU is refused.
	_, err = owner.API.CreateItem(ctx, garage.GetId(), &inventoryv1.Item{Sku: code, Name: "Different oil"})
	fixture.RequireAPIError(t, err, http.StatusConflict, "")

	// Empty SKU is refused.
	_, err = owner.API.CreateItem(ctx, garage.GetId(), &inventoryv1.Item{Name: "Air filter"})
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "")

	// Empty name is refused.
	_, err = owner.API.CreateItem(ctx, garage.GetId(), &inventoryv1.Item{Sku: "FILTER-01", Name: ""})
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "")

	saved, err := owner.API.CreateItem(ctx, garage.GetId(), &inventoryv1.Item{Sku: "FILTER-01", Name: "Air filter"})
	require.NoError(t, err)

	require.NoError(t, owner.API.DeleteItem(ctx, garage.GetId(), saved.GetId()))
}

// Scenario: E2E-INV-003 (partial): a goods receipt increases stock on
// hand — asserted at API level; the tab badge is web-E2E.
func TestGoodsReceiptIncreasesStock(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage := setup(t, "inv-receipt")

	item, err := owner.API.CreateItem(ctx, garage.GetId(), &inventoryv1.Item{
		Sku: sku(), Name: "Engine oil 5W-30", Unit: "L", ReorderThreshold: 5,
	})
	require.NoError(t, err)

	first, err := owner.API.CreateStockReceipt(ctx, garage.GetId(), item.GetId(), "10", 150000)
	require.NoError(t, err)
	assert.Equal(t, inventoryv1.StockTransactionType_STOCK_TRANSACTION_TYPE_IN, first.GetType())
	assert.Equal(t, "10", first.GetQuantity())

	afterFirst, err := owner.API.GetItem(ctx, garage.GetId(), item.GetId())
	require.NoError(t, err)
	assert.Equal(t, "10", afterFirst.GetQuantityOnHand())

	// Unit cost is optional per receipt.
	_, err = owner.API.CreateStockReceipt(ctx, garage.GetId(), item.GetId(), "5", 0)
	require.NoError(t, err)

	afterSecond, err := owner.API.GetItem(ctx, garage.GetId(), item.GetId())
	require.NoError(t, err)
	assert.Equal(t, "15", afterSecond.GetQuantityOnHand())

	movements, err := owner.API.ListStockTransactions(ctx, garage.GetId())
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(movements), 2)
}

// Scenario: E2E-INV-004 (partial): the low-stock rule (on hand at or
// below the threshold) — asserted at API level through the item read;
// the badge and highlight are web-E2E.
func TestLowStockThreshold(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage := setup(t, "inv-lowstock")

	item, err := owner.API.CreateItem(ctx, garage.GetId(), &inventoryv1.Item{
		Sku: sku(), Name: "Engine oil", Unit: "L", ReorderThreshold: 5,
	})
	require.NoError(t, err)

	_, err = owner.API.CreateStockReceipt(ctx, garage.GetId(), item.GetId(), "15", 0)
	require.NoError(t, err)

	// 15 <= 20: low.
	low, err := owner.API.UpdateItem(ctx, garage.GetId(), item.GetId(),
		&inventoryv1.Item{ReorderThreshold: 20}, "reorder_threshold")
	require.NoError(t, err)
	assertLowStock(t, low.GetQuantityOnHand(), low.GetReorderThreshold(), true)

	// 15 <= 15, boundary: still low.
	boundary, err := owner.API.UpdateItem(ctx, garage.GetId(), item.GetId(),
		&inventoryv1.Item{ReorderThreshold: 15}, "reorder_threshold")
	require.NoError(t, err)
	assertLowStock(t, boundary.GetQuantityOnHand(), boundary.GetReorderThreshold(), true)

	// Back to 5: not low.
	restored, err := owner.API.UpdateItem(ctx, garage.GetId(), item.GetId(),
		&inventoryv1.Item{ReorderThreshold: 5}, "reorder_threshold")
	require.NoError(t, err)
	assertLowStock(t, restored.GetQuantityOnHand(), restored.GetReorderThreshold(), false)
}

// assertLowStock checks the documented inclusive low-stock rule (on
// hand at or below the threshold) against the API values; the badge
// itself is web-E2E.
func assertLowStock(t *testing.T, onHand string, threshold int32, wantLow bool) {
	t.Helper()

	parsed, err := strconv.ParseFloat(onHand, 64)
	require.NoError(t, err)
	assert.Equal(t, wantLow, parsed <= float64(threshold))
}

// Scenario: E2E-INV-005 (partial): items are searched by SKU or name —
// the API substance; the empty state is web-E2E.
func TestSearchItems(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage := setup(t, "inv-search")

	oilSKU := sku()

	_, err := owner.API.CreateItem(ctx, garage.GetId(), &inventoryv1.Item{Sku: oilSKU, Name: "Engine oil 5W-30", Unit: "L"})
	require.NoError(t, err)

	_, err = owner.API.CreateItem(ctx, garage.GetId(), &inventoryv1.Item{Sku: "FILTER-01", Name: "Air filter", Unit: "piece"})
	require.NoError(t, err)

	for _, query := range []string{"OIL", "Engine", "FILTER"} {
		found, err := owner.API.SearchItems(ctx, garage.GetId(), query)
		require.NoError(t, err, "query %q", query)
		require.NotEmpty(t, found, "query %q", query)
	}

	none, err := owner.API.SearchItems(ctx, garage.GetId(), "no-such-item-zzz")
	require.NoError(t, err)
	assert.Empty(t, none)
}

// Scenario: E2E-INV-006 (partial): an item is edited and deleted while
// its past transactions are kept — the API substance; the confirm copy
// is web-E2E.
func TestEditAndDeleteItem(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage := setup(t, "inv-edit")

	item, err := owner.API.CreateItem(ctx, garage.GetId(), &inventoryv1.Item{
		Sku: "TEST-PART-01", Name: "QA Test Part", Unit: "piece",
	})
	require.NoError(t, err)

	_, err = owner.API.CreateStockReceipt(ctx, garage.GetId(), item.GetId(), "3", 0)
	require.NoError(t, err)

	renamed, err := owner.API.UpdateItem(ctx, garage.GetId(), item.GetId(),
		&inventoryv1.Item{Name: "QA Test Part Renamed", Unit: "piece"}, "name", "unit")
	require.NoError(t, err)
	assert.Equal(t, "QA Test Part Renamed", renamed.GetName())

	require.NoError(t, owner.API.DeleteItem(ctx, garage.GetId(), item.GetId()))

	_, err = owner.API.GetItem(ctx, garage.GetId(), item.GetId())
	fixture.RequireAPIError(t, err, http.StatusNotFound, "")

	// The receipt transaction is still listed.
	movements, err := owner.API.ListStockTransactions(ctx, garage.GetId())
	require.NoError(t, err)
	require.NotEmpty(t, movements)
	assert.Equal(t, "TEST-PART-01", movements[0].GetItemSku())
}

// Scenario: E2E-INV-007 (partial): the stock movement report filters by
// direction — the API substance; the tab is web-E2E.
func TestStockMovementReportFilters(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage := setup(t, "inv-report")

	item, err := owner.API.CreateItem(ctx, garage.GetId(), &inventoryv1.Item{Sku: sku(), Name: "Engine oil", Unit: "L"})
	require.NoError(t, err)

	_, err = owner.API.CreateStockReceipt(ctx, garage.GetId(), item.GetId(), "10", 0)
	require.NoError(t, err)

	closeOrderWithPart(t, owner.API, garage.GetId(), item.GetId(), "4")

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

	// The OUT leg arrives through the async outbox relay; poll for it.
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

	item, err := owner.API.CreateItem(ctx, garage.GetId(), &inventoryv1.Item{Sku: sku(), Name: "Engine oil", Unit: "L"})
	require.NoError(t, err)

	_, err = owner.API.CreateStockReceipt(ctx, garage.GetId(), item.GetId(), "10", 0)
	require.NoError(t, err)

	closeOrderWithPart(t, owner.API, garage.GetId(), item.GetId(), "4")

	// Deduction arrives through the async outbox relay; poll for it.
	waitForOnHand(t, owner.API, garage.GetId(), item.GetId(), "6")
}

// Scenario: E2E-INV-009 (partial): a part line without a catalogue item
// deducts nothing on close — asserted at API level.
func TestCloseWithoutCatalogueItemDeductsNothing(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage := setup(t, "inv-nodeduct")

	item, err := owner.API.CreateItem(ctx, garage.GetId(), &inventoryv1.Item{Sku: sku(), Name: "Engine oil", Unit: "L"})
	require.NoError(t, err)

	_, err = owner.API.CreateStockReceipt(ctx, garage.GetId(), item.GetId(), "10", 0)
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

	after, err := owner.API.GetItem(ctx, garage.GetId(), item.GetId())
	require.NoError(t, err)
	assert.Equal(t, "10", after.GetQuantityOnHand())
}

// waitForOnHand polls the item until its on-hand quantity reads want
// (the close deduction arrives through the async outbox relay).
func waitForOnHand(t *testing.T, api *client.Client, garageID, itemID, want string) {
	t.Helper()

	deadline := time.Now().Add(60 * time.Second)

	for {
		item, err := api.GetItem(t.Context(), garageID, itemID)
		require.NoError(t, err)

		if item.GetQuantityOnHand() == want {
			return
		}

		if time.Now().After(deadline) {
			t.Fatalf("on hand is %q, want %q after close", item.GetQuantityOnHand(), want)
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
// linked to itemID, quantity qty.
func closeOrderWithPart(t *testing.T, api *client.Client, garageID, itemID, qty string) {
	t.Helper()

	ctx := t.Context()
	vehicle := vehicleFor(t, api, garageID)
	itemRef := itemID

	order, err := api.CreateRepairOrder(ctx, garageID, &repairorderv1.RepairOrder{
		VehicleId: vehicle.GetId(),
		Lines: []*repairorderv1.RepairOrderLine{{
			ItemType:  repairorderv1.RepairOrderItemType_REPAIR_ORDER_ITEM_TYPE_PART,
			ItemRefId: &itemRef, Description: "Engine oil 5W-30", Quantity: qty, UnitPrice: 150000,
		}},
	})
	require.NoError(t, err)

	_, err = api.UpdateRepairOrderLine(ctx, garageID, order.GetId(), order.GetLines()[0].GetId(),
		&repairorderv1.RepairOrderLine{Status: repairorderv1.RepairOrderLineStatus_REPAIR_ORDER_LINE_STATUS_COMPLETED})
	require.NoError(t, err)

	_, err = api.CloseRepairOrder(ctx, garageID, order.GetId())
	require.NoError(t, err)
}
