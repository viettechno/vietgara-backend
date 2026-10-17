package inventory_test

import (
	"net/http"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	identityv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/identity/v1"
	inventoryv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/inventory/v1"
	repairorderv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/repairorder/v1"
	supplierv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/supplier/v1"
	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/client"
	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

var supplierSeq atomic.Int64

// supplier adds a supplier to the garage.
func supplier(t *testing.T, api *client.Client, garageID, name string) *supplierv1.Supplier {
	t.Helper()

	created, err := api.CreateSupplier(t.Context(), garageID, &supplierv1.Supplier{Name: name + time.Now().Format("150405") + string(rune('A'+supplierSeq.Add(1)%26))})
	require.NoError(t, err)

	return created
}

// Scenario: Release 7.0, goods receipt documents (FR-INV-06): a delivery of
// three parts from two suppliers is one numbered document with a supplier on
// each line; it is saved as a draft, a line without a supplier blocks
// completion, and completing it updates stock and the weighted-average cost.
func TestGoodsReceiptSeveralSuppliers(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage := setup(t, "inv-gr-multi")
	garageID := garage.GetId()
	unit := litre(t, owner, garageID)

	oil := fixture.NewPart(t, owner.API, garageID, sku(), "Engine oil", unit)
	filter := fixture.NewPart(t, owner.API, garageID, sku(), "Oil filter", unit)
	plug := fixture.NewPart(t, owner.API, garageID, sku(), "Spark plug", unit)
	acme, bolt := supplier(t, owner.API, garageID, "Acme "), supplier(t, owner.API, garageID, "Bolt ")

	draft, err := owner.API.CreateGoodsReceipt(ctx, garageID, &inventoryv1.GoodsReceipt{
		DefaultSupplierId: new(acme.GetId()), DeliveryNoteNumber: "DN-77", Notes: "morning delivery",
		Lines: []*inventoryv1.GoodsReceiptLine{
			{PartId: oil.GetId(), EnteredQuantity: "10", UnitCost: new(int64(100))},
			{PartId: filter.GetId(), EnteredQuantity: "4", UnitCost: new(int64(2500)), SupplierId: bolt.GetId()},
			{PartId: plug.GetId(), EnteredQuantity: "8", UnitCost: new(int64(300))},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, inventoryv1.GoodsReceiptStatus_GOODS_RECEIPT_STATUS_DRAFT, draft.GetStatus())
	assert.NotEmpty(t, draft.GetNumber())
	assert.EqualValues(t, 1000+10000+2400, draft.GetTotalAmount())
	assert.Equal(t, acme.GetId(), draft.GetLines()[0].GetSupplierId(), "a line inherits the receipt's supplier")
	assert.Equal(t, bolt.GetId(), draft.GetLines()[1].GetSupplierId(), "and may override it")

	level, err := owner.API.GetStockLevel(ctx, garageID, oil.GetId())
	require.NoError(t, err)
	assert.Equal(t, "0", level.GetQuantityOnHand(), "a draft changes no stock")

	// A line without a supplier blocks completion.
	noSupplier, err := owner.API.CreateGoodsReceipt(ctx, garageID, &inventoryv1.GoodsReceipt{
		Lines: []*inventoryv1.GoodsReceiptLine{{PartId: oil.GetId(), EnteredQuantity: "1"}},
	})
	require.NoError(t, err)

	_, err = owner.API.CompleteGoodsReceipt(ctx, garageID, noSupplier.GetId())
	fixture.RequireAPIError(t, err, http.StatusConflict, "SUPPLIER_REQUIRED")

	// The draft is edited, then completed.
	edited, err := owner.API.UpdateGoodsReceipt(ctx, garageID, draft.GetId(), &inventoryv1.GoodsReceipt{Notes: "edited"}, "notes")
	require.NoError(t, err)
	assert.Equal(t, "edited", edited.GetNotes())

	done, err := owner.API.CompleteGoodsReceipt(ctx, garageID, draft.GetId())
	require.NoError(t, err)
	assert.Equal(t, inventoryv1.GoodsReceiptStatus_GOODS_RECEIPT_STATUS_COMPLETED, done.GetStatus())
	assert.NotNil(t, done.GetCompletedAt())

	level, err = owner.API.GetStockLevel(ctx, garageID, oil.GetId())
	require.NoError(t, err)
	assert.Equal(t, "10", level.GetQuantityOnHand())
	assert.EqualValues(t, 100, level.GetAverageCost())
	assert.EqualValues(t, 1000, level.GetStockValue())

	// A completed receipt cannot be edited or completed again.
	_, err = owner.API.UpdateGoodsReceipt(ctx, garageID, draft.GetId(), &inventoryv1.GoodsReceipt{Notes: "late"}, "notes")
	fixture.RequireAPIError(t, err, http.StatusConflict, "GOODS_RECEIPT_NOT_DRAFT")

	// A second delivery at a different price changes the average.
	second, err := owner.API.ReceiveStock(ctx, garageID, oil.GetId(), "10", "", 200)
	require.NoError(t, err)

	level, err = owner.API.GetStockLevel(ctx, garageID, oil.GetId())
	require.NoError(t, err)
	assert.Equal(t, "20", level.GetQuantityOnHand())
	assert.EqualValues(t, 150, level.GetAverageCost(), "(10 x 100 + 10 x 200) / 20")

	// The receipt list filters by supplier and status.
	byBolt, err := owner.API.ListGoodsReceipts(ctx, garageID, url.Values{"supplierId": {bolt.GetId()}})
	require.NoError(t, err)
	require.Len(t, byBolt, 1)
	assert.Equal(t, draft.GetId(), byBolt[0].GetId())

	drafts, err := owner.API.ListGoodsReceipts(ctx, garageID, url.Values{"status": {"1"}})
	require.NoError(t, err)
	require.Len(t, drafts, 1, "only the receipt without a supplier is still a draft")

	// Cancelling the second receipt restores the previous stock and cost.
	cancelled, err := owner.API.CancelGoodsReceipt(ctx, garageID, second.GetId())
	require.NoError(t, err)
	assert.Equal(t, inventoryv1.GoodsReceiptStatus_GOODS_RECEIPT_STATUS_CANCELLED, cancelled.GetStatus())

	level, err = owner.API.GetStockLevel(ctx, garageID, oil.GetId())
	require.NoError(t, err)
	assert.Equal(t, "10", level.GetQuantityOnHand())
	assert.EqualValues(t, 100, level.GetAverageCost())

	_, err = owner.API.CancelGoodsReceipt(ctx, garageID, second.GetId())
	fixture.RequireAPIError(t, err, http.StatusConflict, "GOODS_RECEIPT_ALREADY_CANCELLED")
}

// Scenario: a completed receipt whose stock was already used cannot be
// reversed (FR-INV-06).
func TestCancelReceiptWithUsedStockIsRefused(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage := setup(t, "inv-gr-used")
	garageID := garage.GetId()

	part := fixture.NewPart(t, owner.API, garageID, sku(), "Engine oil", litre(t, owner, garageID))

	receipt, err := owner.API.ReceiveStock(ctx, garageID, part.GetId(), "5", "", 100)
	require.NoError(t, err)

	closeOrderWithPart(t, owner.API, garageID, part.GetId(), "3")
	waitForOnHand(t, owner.API, garageID, part.GetId(), "2")

	_, err = owner.API.CancelGoodsReceipt(ctx, garageID, receipt.GetId())
	fixture.RequireAPIError(t, err, http.StatusConflict, "INSUFFICIENT_STOCK_TO_REVERSE")
}

// Scenario: part suppliers (FR-INV-19): a part lists its usual suppliers, one
// the default that pre-fills a receipt line; completing a receipt records the
// last cost paid.
func TestPartSuppliersDefaultAndLastCost(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage := setup(t, "inv-gr-suppliers")
	garageID := garage.GetId()

	part := fixture.NewPart(t, owner.API, garageID, sku(), "Engine oil", litre(t, owner, garageID))
	acme, bolt := supplier(t, owner.API, garageID, "Acme "), supplier(t, owner.API, garageID, "Bolt ")

	list, err := owner.API.PutPartSuppliers(ctx, garageID, part.GetId(), []*inventoryv1.PartSupplier{
		{SupplierId: bolt.GetId(), SupplierSku: "B-1"}, {SupplierId: acme.GetId(), SupplierSku: "A-1", IsDefault: true},
	})
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.Equal(t, acme.GetId(), list[0].GetSupplierId(), "the default comes first")

	// A line with no supplier takes the part's default.
	receipt, err := owner.API.CreateGoodsReceipt(ctx, garageID, &inventoryv1.GoodsReceipt{
		Lines: []*inventoryv1.GoodsReceiptLine{{PartId: part.GetId(), EnteredQuantity: "10", UnitCost: new(int64(120))}},
	})
	require.NoError(t, err)
	assert.Equal(t, acme.GetId(), receipt.GetLines()[0].GetSupplierId())

	_, err = owner.API.CompleteGoodsReceipt(ctx, garageID, receipt.GetId())
	require.NoError(t, err)

	list, err = owner.API.GetPartSuppliers(ctx, garageID, part.GetId())
	require.NoError(t, err)
	assert.EqualValues(t, 120, list[0].GetLastUnitCost())
	assert.Nil(t, list[1].LastUnitCost, "the other supplier was never paid")

	_, err = owner.API.PutPartSuppliers(ctx, garageID, part.GetId(), []*inventoryv1.PartSupplier{
		{SupplierId: acme.GetId(), IsDefault: true}, {SupplierId: bolt.GetId(), IsDefault: true},
	})
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "")
}

// Scenario: cost reports (FR-INV-07, FR-INV-19): purchases by supplier and
// period, and the valuation of stock.
func TestPurchasesAndValuationReports(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage := setup(t, "inv-gr-reports")
	garageID := garage.GetId()
	unit := litre(t, owner, garageID)

	oil := fixture.NewPart(t, owner.API, garageID, sku(), "Engine oil", unit)
	filter := fixture.NewPart(t, owner.API, garageID, sku(), "Oil filter", unit)
	acme, bolt := supplier(t, owner.API, garageID, "Acme "), supplier(t, owner.API, garageID, "Bolt ")

	receipt, err := owner.API.CreateGoodsReceipt(ctx, garageID, &inventoryv1.GoodsReceipt{
		Lines: []*inventoryv1.GoodsReceiptLine{
			{PartId: oil.GetId(), EnteredQuantity: "10", UnitCost: new(int64(100)), SupplierId: acme.GetId()},
			{PartId: filter.GetId(), EnteredQuantity: "4", UnitCost: new(int64(2500)), SupplierId: bolt.GetId()},
		},
	})
	require.NoError(t, err)

	_, err = owner.API.CompleteGoodsReceipt(ctx, garageID, receipt.GetId())
	require.NoError(t, err)

	all, err := owner.API.GetPurchasesReport(ctx, garageID, "", "", "")
	require.NoError(t, err)
	assert.EqualValues(t, 11000, all.GetTotalAmount())
	assert.Len(t, all.GetData(), 2)

	byAcme, err := owner.API.GetPurchasesReport(ctx, garageID, acme.GetId(), "", "")
	require.NoError(t, err)
	assert.EqualValues(t, 1000, byAcme.GetTotalAmount())
	require.Len(t, byAcme.GetData(), 1)
	assert.Equal(t, acme.GetName(), byAcme.GetData()[0].GetSupplierName())

	future, err := owner.API.GetPurchasesReport(ctx, garageID, "", time.Now().AddDate(1, 0, 0).Format(time.DateOnly), "")
	require.NoError(t, err)
	assert.Empty(t, future.GetData())

	valuation, err := owner.API.GetStockValuationReport(ctx, garageID, "")
	require.NoError(t, err)
	assert.EqualValues(t, 11000, valuation.GetTotalValue())
	assert.Len(t, valuation.GetData(), 2)

	yesterday, err := owner.API.GetStockValuationReport(ctx, garageID, time.Now().AddDate(0, 0, -2).Format(time.DateOnly))
	require.NoError(t, err)
	assert.Empty(t, yesterday.GetData(), "nothing was in stock two days ago")
}

// Scenario: cost visibility (FR-INV-08): a technician sees quantities but no
// cost, and cannot read the cost reports.
func TestCostIsHiddenFromTechnicians(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwnerWithLocale(t, "inv-cost-owner", identityv1.Locale_LOCALE_EN)

	garage, err := owner.API.CreateGarage(ctx, &tenantv1.Garage{Name: "Blackbox Cost"})
	require.NoError(t, err)

	garageID := garage.GetId()
	part := fixture.NewPart(t, owner.API, garageID, sku(), "Engine oil", litre(t, owner, garageID))

	receipt, err := owner.API.ReceiveStock(ctx, garageID, part.GetId(), "10", "", 150000)
	require.NoError(t, err)
	assert.EqualValues(t, 1_500_000, receipt.GetTotalAmount(), "the owner sees cost")

	groups, err := owner.API.ListStaffGroups(ctx, garageID)
	require.NoError(t, err)

	tech := fixture.NewVerifiedOwner(t, "inv-cost-tech")
	candidate, err := owner.API.LookupStaffCandidate(ctx, garageID, tech.Email)
	require.NoError(t, err)

	var group string

	for _, g := range groups {
		if g.GetName() == "Technician" {
			group = g.GetId()
		}
	}

	require.NotEmpty(t, group)

	_, err = owner.API.CreateInvitation(ctx, garageID, candidate.GetUserId(), []string{group})
	require.NoError(t, err)

	invitations, err := tech.API.ListMyInvitations(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, invitations)

	_, err = tech.API.AcceptInvitation(ctx, invitations[0].GetId())
	require.NoError(t, err)

	level, err := tech.API.GetStockLevel(ctx, garageID, part.GetId())
	require.NoError(t, err)
	assert.Equal(t, "10", level.GetQuantityOnHand())
	assert.Nil(t, level.AverageCost)
	assert.Nil(t, level.StockValue)

	seen, err := tech.API.GetGoodsReceipt(ctx, garageID, receipt.GetId())
	require.NoError(t, err)
	assert.Nil(t, seen.TotalAmount)
	assert.Nil(t, seen.GetLines()[0].UnitCost)

	_, err = tech.API.GetPurchasesReport(ctx, garageID, "", "", "")
	fixture.RequireAPIError(t, err, http.StatusForbidden, "")

	_, err = tech.API.GetStockValuationReport(ctx, garageID, "")
	fixture.RequireAPIError(t, err, http.StatusForbidden, "")
}

// orderWithParts creates an open repair order with one part line per
// (part, quantity) pair.
func orderWithParts(t *testing.T, api *client.Client, garageID string, lines ...[2]string) *repairorderv1.RepairOrder {
	t.Helper()

	vehicle := vehicleFor(t, api, garageID)
	orderLines := make([]*repairorderv1.RepairOrderLine, 0, len(lines))

	for _, l := range lines {
		ref := l[0]
		orderLines = append(orderLines, &repairorderv1.RepairOrderLine{
			ItemType:  repairorderv1.RepairOrderItemType_REPAIR_ORDER_ITEM_TYPE_PART,
			ItemRefId: &ref, Description: "Part", Quantity: l[1], UnitPrice: 100000,
		})
	}

	order, err := api.CreateRepairOrder(t.Context(), garageID, &repairorderv1.RepairOrder{VehicleId: vehicle.GetId(), Lines: orderLines})
	require.NoError(t, err)

	return order
}

// waitForStockIssue polls until the repair order has its stock issue (created
// by the repair_order.created consumer).
func waitForStockIssue(t *testing.T, api *client.Client, garageID, orderID string) *inventoryv1.StockIssue {
	t.Helper()

	deadline := time.Now().Add(60 * time.Second)

	for {
		issue, err := api.GetRepairOrderStockIssue(t.Context(), garageID, orderID)
		if err == nil {
			return issue
		}

		if apiErr := client.AsAPIError(err); apiErr == nil || apiErr.Status != http.StatusNotFound {
			require.NoError(t, err)
		}

		if time.Now().After(deadline) {
			t.Fatalf("repair order %s has no stock issue", orderID)
		}

		time.Sleep(500 * time.Millisecond)
	}
}

// waitForReserved polls until the part's reserved quantity reads want.
func waitForReserved(t *testing.T, api *client.Client, garageID, partID, want string) *inventoryv1.StockLevel {
	t.Helper()

	deadline := time.Now().Add(60 * time.Second)

	for {
		level, err := api.GetStockLevel(t.Context(), garageID, partID)
		require.NoError(t, err)

		if level.GetQuantityReserved() == want {
			return level
		}

		if time.Now().After(deadline) {
			t.Fatalf("reserved is %q, want %q", level.GetQuantityReserved(), want)
		}

		time.Sleep(500 * time.Millisecond)
	}
}

// Scenario: Release 7.0, stock issue document (FR-INV-20): creating a repair
// order with two part lines creates a numbered draft stock issue that reserves
// them (available drops, on hand does not); "issue now" posts it earlier and
// the stock movements carry the average cost.
func TestStockIssueReservesThenIssuesNow(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage := setup(t, "inv-issue-now")
	garageID := garage.GetId()
	unit := litre(t, owner, garageID)

	oil := fixture.NewPart(t, owner.API, garageID, sku(), "Engine oil", unit)
	filter := fixture.NewPart(t, owner.API, garageID, sku(), "Oil filter", unit)

	_, err := owner.API.ReceiveStock(ctx, garageID, oil.GetId(), "10", "", 100)
	require.NoError(t, err)
	_, err = owner.API.ReceiveStock(ctx, garageID, filter.GetId(), "5", "", 2000)
	require.NoError(t, err)

	order := orderWithParts(t, owner.API, garageID, [2]string{oil.GetId(), "4"}, [2]string{filter.GetId(), "1"})
	issue := waitForStockIssue(t, owner.API, garageID, order.GetId())

	assert.Equal(t, inventoryv1.StockIssueStatus_STOCK_ISSUE_STATUS_DRAFT, issue.GetStatus())
	assert.NotEmpty(t, issue.GetNumber())
	assert.Equal(t, order.GetNumber(), issue.GetRepairOrderNumber())
	assert.Len(t, issue.GetLines(), 2)

	level := waitForReserved(t, owner.API, garageID, oil.GetId(), "4")
	assert.Equal(t, "10", level.GetQuantityOnHand(), "a draft reserves; it does not take stock")
	assert.Equal(t, "6", level.GetQuantityAvailable())

	posted, err := owner.API.PostStockIssue(ctx, garageID, issue.GetId())
	require.NoError(t, err)
	assert.Equal(t, inventoryv1.StockIssueStatus_STOCK_ISSUE_STATUS_POSTED, posted.GetStatus())
	assert.NotNil(t, posted.GetPostedAt())

	for _, l := range posted.GetLines() {
		assert.NotNil(t, l.UnitCost, "each line keeps the average cost of the moment")
	}

	level, err = owner.API.GetStockLevel(ctx, garageID, oil.GetId())
	require.NoError(t, err)
	assert.Equal(t, "6", level.GetQuantityOnHand())
	assert.Equal(t, "0", level.GetQuantityReserved())

	_, err = owner.API.PostStockIssue(ctx, garageID, issue.GetId())
	fixture.RequireAPIError(t, err, http.StatusConflict, "STOCK_ISSUE_NOT_DRAFT")
}

// Scenario: closing the repair order posts its stock issue, once.
func TestClosingTheOrderPostsTheStockIssue(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage := setup(t, "inv-issue-close")
	garageID := garage.GetId()

	part := fixture.NewPart(t, owner.API, garageID, sku(), "Engine oil", litre(t, owner, garageID))

	_, err := owner.API.ReceiveStock(ctx, garageID, part.GetId(), "10", "", 100)
	require.NoError(t, err)

	order := orderWithParts(t, owner.API, garageID, [2]string{part.GetId(), "4"})
	waitForStockIssue(t, owner.API, garageID, order.GetId())
	waitForReserved(t, owner.API, garageID, part.GetId(), "4")

	_, err = owner.API.UpdateRepairOrderLine(ctx, garageID, order.GetId(), order.GetLines()[0].GetId(),
		&repairorderv1.RepairOrderLine{Status: repairorderv1.RepairOrderLineStatus_REPAIR_ORDER_LINE_STATUS_COMPLETED})
	require.NoError(t, err)

	_, err = owner.API.CloseRepairOrder(ctx, garageID, order.GetId())
	require.NoError(t, err)

	waitForOnHand(t, owner.API, garageID, part.GetId(), "6")

	level, err := owner.API.GetStockLevel(ctx, garageID, part.GetId())
	require.NoError(t, err)
	assert.Equal(t, "0", level.GetQuantityReserved())

	issue, err := owner.API.GetRepairOrderStockIssue(ctx, garageID, order.GetId())
	require.NoError(t, err)
	assert.Equal(t, inventoryv1.StockIssueStatus_STOCK_ISSUE_STATUS_POSTED, issue.GetStatus())

	outs := waitForTransactions(t, owner.API, garageID, inventoryv1.StockTransactionType_STOCK_TRANSACTION_TYPE_OUT)
	require.Len(t, outs, 1, "one movement for the part")
	assert.Equal(t, inventoryv1.StockTransactionRefType_STOCK_TRANSACTION_REF_TYPE_STOCK_ISSUE, outs[0].GetRefType())
	assert.EqualValues(t, 100, outs[0].GetUnitCost(), "stamped with the average cost")
}

// Scenario: with the garage setting stock_issue_mode = on_start, the stock
// issue posts when work starts instead of when the order closes.
func TestStockIssueModeOnStart(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage := setup(t, "inv-issue-start")
	garageID := garage.GetId()

	_, err := owner.API.UpdateGarage(ctx, garageID,
		&tenantv1.Garage{StockIssueMode: tenantv1.StockIssueMode_STOCK_ISSUE_MODE_ON_START}, "stock_issue_mode")
	require.NoError(t, err)

	part := fixture.NewPart(t, owner.API, garageID, sku(), "Engine oil", litre(t, owner, garageID))

	_, err = owner.API.ReceiveStock(ctx, garageID, part.GetId(), "10", "", 100)
	require.NoError(t, err)

	order := orderWithParts(t, owner.API, garageID, [2]string{part.GetId(), "4"})
	waitForStockIssue(t, owner.API, garageID, order.GetId())

	_, err = owner.API.UpdateRepairOrderLine(ctx, garageID, order.GetId(), order.GetLines()[0].GetId(),
		&repairorderv1.RepairOrderLine{Status: repairorderv1.RepairOrderLineStatus_REPAIR_ORDER_LINE_STATUS_IN_PROGRESS})
	require.NoError(t, err)

	waitForOnHand(t, owner.API, garageID, part.GetId(), "6")
}
