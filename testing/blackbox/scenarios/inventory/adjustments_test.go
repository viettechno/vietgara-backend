package inventory_test

import (
	"bytes"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	cataloguev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/catalogue/v1"
	identityv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/identity/v1"
	inventoryv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/inventory/v1"
	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/client"
	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

// stockLot is a delivery of one part: a quantity at a cost per base unit.
type stockLot struct {
	quantity string
	cost     int64
}

// stockedParts adds a part per lot to the garage and receives the lot.
func stockedParts(t *testing.T, owner *fixture.Actor, garageID string, unit *cataloguev1.Unit, lots ...stockLot) []*cataloguev1.Part {
	t.Helper()

	parts := make([]*cataloguev1.Part, 0, len(lots))

	for _, lot := range lots {
		part := fixture.NewPart(t, owner.API, garageID, sku(), "Part "+lot.quantity, unit)

		_, err := owner.API.ReceiveStock(t.Context(), garageID, part.GetId(), lot.quantity, "", lot.cost)
		require.NoError(t, err)

		parts = append(parts, part)
	}

	return parts
}

func onHand(t *testing.T, api *client.Client, garageID string, part *cataloguev1.Part) string {
	t.Helper()

	level, err := api.GetStockLevel(t.Context(), garageID, part.GetId())
	require.NoError(t, err)

	return level.GetQuantityOnHand()
}

func lineOfPart(t *testing.T, count *inventoryv1.StockCount, part *cataloguev1.Part) *inventoryv1.StockCountLine {
	t.Helper()

	for _, l := range count.GetLines() {
		if l.GetPartId() == part.GetId() {
			return l
		}
	}

	t.Fatalf("part %s is not on the count", part.GetId())

	return nil
}

// joinAs adds a new user to the garage in the staff group named group.
func joinAs(t *testing.T, owner *fixture.Actor, garageID, group, label string) *fixture.Actor {
	t.Helper()

	ctx := t.Context()

	groups, err := owner.API.ListStaffGroups(ctx, garageID)
	require.NoError(t, err)

	var groupID string

	for _, g := range groups {
		if g.GetName() == group {
			groupID = g.GetId()
		}
	}

	require.NotEmpty(t, groupID, group)

	member := fixture.NewVerifiedOwner(t, label)
	candidate, err := owner.API.LookupStaffCandidate(ctx, garageID, member.Email)
	require.NoError(t, err)

	_, err = owner.API.CreateInvitation(ctx, garageID, candidate.GetUserId(), []string{groupID})
	require.NoError(t, err)

	invitations, err := member.API.ListMyInvitations(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, invitations)

	_, err = member.API.AcceptInvitation(ctx, invitations[0].GetId())
	require.NoError(t, err)

	return member
}

// Scenario: Release 7.1, stock count (FR-INV-12): counting 3 parts with 2
// differences gives one document with matched, over and short lines;
// completing it makes stock equal the count and values the difference at
// average cost; it cannot be edited afterwards, appears in the part's stock
// card, and cancelling it reverses the adjustments.
func TestStockCountMakesStockEqualTheCount(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage := setup(t, "inv-count")
	garageID := garage.GetId()
	parts := stockedParts(t, owner, garageID, litre(t, owner, garageID), stockLot{"10", 100}, stockLot{"20", 50}, stockLot{"5", 200})
	oil, filter, plug := parts[0], parts[1], parts[2]

	count, err := owner.API.CreateStockCount(ctx, garageID, &inventoryv1.StockCount{Name: "Shelf A", Notes: "monthly"})
	require.NoError(t, err)
	assert.Equal(t, inventoryv1.StockCountStatus_STOCK_COUNT_STATUS_DRAFT, count.GetStatus())
	assert.NotEmpty(t, count.GetNumber())

	count, err = owner.API.AddStockCountLines(ctx, garageID, count.GetId(), []string{oil.GetId(), filter.GetId(), plug.GetId()}, "")
	require.NoError(t, err)
	require.Len(t, count.GetLines(), 3)
	assert.EqualValues(t, 3, count.GetUncountedLines())
	assert.Equal(t, "10", lineOfPart(t, count, oil).GetSystemQuantity(), "the system quantity is captured when the line is added")

	// Completing with an uncounted line is refused.
	_, err = owner.API.CompleteStockCount(ctx, garageID, count.GetId(), false)
	fixture.RequireAPIError(t, err, http.StatusConflict, "STOCK_COUNT_HAS_UNCOUNTED_LINES")

	for part, counted := range map[*cataloguev1.Part]string{oil: "10", filter: "18", plug: "7"} {
		count, err = owner.API.CountStockLine(ctx, garageID, count.GetId(), lineOfPart(t, count, part).GetId(), "", counted)
		require.NoError(t, err)
	}

	assert.EqualValues(t, 1, count.GetMatchedLines())
	assert.EqualValues(t, 1, count.GetShortLines())
	assert.EqualValues(t, 1, count.GetOverLines())
	assert.EqualValues(t, 0, count.GetUncountedLines())
	assert.Equal(t, inventoryv1.StockCountLineResult_STOCK_COUNT_LINE_RESULT_SHORT, lineOfPart(t, count, filter).GetResult())
	assert.Equal(t, "-2", lineOfPart(t, count, filter).GetDifferenceQuantity())
	assert.Equal(t, "2", lineOfPart(t, count, plug).GetDifferenceQuantity())
	assert.EqualValues(t, -100, lineOfPart(t, count, filter).GetDifferenceValue())
	assert.EqualValues(t, 400, lineOfPart(t, count, plug).GetDifferenceValue())
	assert.EqualValues(t, 300, count.GetTotalDifferenceValue(), "-2 x 50 + 2 x 200")

	// The draft changes no stock, and has an Excel export for review.
	assert.Equal(t, "20", onHand(t, owner.API, garageID, filter))

	contentType, workbook, err := owner.API.DownloadStockCountExcel(ctx, garageID, count.GetId())
	require.NoError(t, err)
	assert.Contains(t, contentType, "spreadsheetml")
	assert.True(t, bytes.HasPrefix(workbook, []byte("PK")), "an xlsx file is a zip")

	count, err = owner.API.CompleteStockCount(ctx, garageID, count.GetId(), false)
	require.NoError(t, err)
	assert.Equal(t, inventoryv1.StockCountStatus_STOCK_COUNT_STATUS_COMPLETED, count.GetStatus())
	assert.NotNil(t, count.GetCompletedAt())
	assert.EqualValues(t, 300, count.GetTotalDifferenceValue())

	assert.Equal(t, "10", onHand(t, owner.API, garageID, oil))
	assert.Equal(t, "18", onHand(t, owner.API, garageID, filter), "stock equals the count")
	assert.Equal(t, "7", onHand(t, owner.API, garageID, plug))

	level, err := owner.API.GetStockLevel(ctx, garageID, plug.GetId())
	require.NoError(t, err)
	assert.EqualValues(t, 200, level.GetAverageCost(), "an adjustment leaves the average cost alone")

	// A completed count cannot be edited.
	_, err = owner.API.CountStockLine(ctx, garageID, count.GetId(), lineOfPart(t, count, oil).GetId(), "", "1")
	fixture.RequireAPIError(t, err, http.StatusConflict, "STOCK_COUNT_NOT_DRAFT")

	// The adjustment is in the part's stock card, with the document and the user.
	card, err := owner.API.ListStockCard(ctx, garageID, plug.GetId(), inventoryv1.StockTransactionRefType_STOCK_TRANSACTION_REF_TYPE_STOCK_COUNT)
	require.NoError(t, err)
	require.Len(t, card, 1)
	assert.Equal(t, count.GetNumber(), card[0].GetRefNumber())
	assert.Equal(t, inventoryv1.StockTransactionType_STOCK_TRANSACTION_TYPE_IN, card[0].GetType())
	assert.Equal(t, "2", card[0].GetQuantity())
	assert.Equal(t, "7", card[0].GetQuantityAfter())
	assert.NotEmpty(t, card[0].GetCreatedByName())
	assert.EqualValues(t, 200, card[0].GetUnitCost(), "valued at average cost")

	// The list shows the tally without the lines.
	listed, err := owner.API.ListStockCounts(ctx, garageID, url.Values{"status": {"2"}})
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.EqualValues(t, 1, listed[0].GetShortLines())
	assert.Empty(t, listed[0].GetLines())

	// Cancelling posts the reversal.
	cancelled, err := owner.API.CancelStockCount(ctx, garageID, count.GetId())
	require.NoError(t, err)
	assert.Equal(t, inventoryv1.StockCountStatus_STOCK_COUNT_STATUS_CANCELLED, cancelled.GetStatus())
	assert.Equal(t, "20", onHand(t, owner.API, garageID, filter))
	assert.Equal(t, "5", onHand(t, owner.API, garageID, plug))

	_, err = owner.API.CancelStockCount(ctx, garageID, count.GetId())
	fixture.RequireAPIError(t, err, http.StatusConflict, "STOCK_COUNT_ALREADY_CANCELLED")
}

// Scenario: Release 7.1, drafts can be merged (FR-INV-12): two people count
// different shelves, the drafts merge into one with the counted quantities of
// a shared part added up, and parts nobody counted can be skipped on completion.
func TestStockCountDraftsMerge(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage := setup(t, "inv-count-merge")
	garageID := garage.GetId()
	parts := stockedParts(t, owner, garageID, litre(t, owner, garageID), stockLot{"10", 100}, stockLot{"4", 50}, stockLot{"3", 200})
	oil, filter, plug := parts[0], parts[1], parts[2]

	draft := func(name string, counted map[*cataloguev1.Part]string, uncounted ...*cataloguev1.Part) *inventoryv1.StockCount {
		t.Helper()

		count, err := owner.API.CreateStockCount(ctx, garageID, &inventoryv1.StockCount{Name: name})
		require.NoError(t, err)

		ids := make([]string, 0, len(counted)+len(uncounted))
		for part := range counted {
			ids = append(ids, part.GetId())
		}

		for _, part := range uncounted {
			ids = append(ids, part.GetId())
		}

		count, err = owner.API.AddStockCountLines(ctx, garageID, count.GetId(), ids, "")
		require.NoError(t, err)

		for part, quantity := range counted {
			count, err = owner.API.CountStockLine(ctx, garageID, count.GetId(), lineOfPart(t, count, part).GetId(), "", quantity)
			require.NoError(t, err)
		}

		return count
	}

	first := draft("Shelf A", map[*cataloguev1.Part]string{oil: "4", filter: "4"})
	second := draft("Shelf B", map[*cataloguev1.Part]string{oil: "6"}, plug)

	// Merging needs two different counts.
	_, err := owner.API.MergeStockCounts(ctx, garageID, first.GetId())
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "")

	merged, err := owner.API.MergeStockCounts(ctx, garageID, first.GetId(), second.GetId())
	require.NoError(t, err)
	assert.Equal(t, first.GetId(), merged.GetId())
	require.Len(t, merged.GetLines(), 3)
	assert.Equal(t, "10", lineOfPart(t, merged, oil).GetCountedQuantity(), "4 + 6, counted in different places")
	assert.EqualValues(t, 1, merged.GetUncountedLines())

	absorbed, err := owner.API.GetStockCount(ctx, garageID, second.GetId())
	require.NoError(t, err)
	assert.Equal(t, inventoryv1.StockCountStatus_STOCK_COUNT_STATUS_CANCELLED, absorbed.GetStatus())
	assert.Empty(t, absorbed.GetLines())

	// A cancelled count cannot be merged again.
	_, err = owner.API.MergeStockCounts(ctx, garageID, first.GetId(), second.GetId())
	fixture.RequireAPIError(t, err, http.StatusConflict, "STOCK_COUNT_NOT_DRAFT")

	// The part nobody counted is dropped on request; the others post.
	done, err := owner.API.CompleteStockCount(ctx, garageID, merged.GetId(), true)
	require.NoError(t, err)
	assert.Len(t, done.GetLines(), 2)
	assert.Equal(t, "10", onHand(t, owner.API, garageID, oil))
	assert.Equal(t, "4", onHand(t, owner.API, garageID, filter))
	assert.Equal(t, "3", onHand(t, owner.API, garageID, plug), "the uncounted part is untouched")

	// A draft's lines can be removed and its name changed; an empty count cannot be completed.
	empty, err := owner.API.CreateStockCount(ctx, garageID, &inventoryv1.StockCount{Name: "Empty"})
	require.NoError(t, err)

	renamed, err := owner.API.UpdateStockCount(ctx, garageID, empty.GetId(), &inventoryv1.StockCount{Name: "Renamed"}, "name")
	require.NoError(t, err)
	assert.Equal(t, "Renamed", renamed.GetName())

	withLine, err := owner.API.AddStockCountLines(ctx, garageID, empty.GetId(), []string{plug.GetId()}, "")
	require.NoError(t, err)

	withoutLine, err := owner.API.DeleteStockCountLine(ctx, garageID, empty.GetId(), withLine.GetLines()[0].GetId())
	require.NoError(t, err)
	assert.Empty(t, withoutLine.GetLines())

	_, err = owner.API.CompleteStockCount(ctx, garageID, empty.GetId(), true)
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "")
}

// Scenario: Release 7.1, a count reversal is refused when the stock it added
// has since been used (FR-INV-12).
func TestStockCountReversalNeedsTheStock(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage := setup(t, "inv-count-reverse")
	garageID := garage.GetId()
	oil := stockedParts(t, owner, garageID, litre(t, owner, garageID), stockLot{"5", 100})[0]

	count, err := owner.API.CreateStockCount(ctx, garageID, &inventoryv1.StockCount{})
	require.NoError(t, err)

	count, err = owner.API.AddStockCountLines(ctx, garageID, count.GetId(), []string{oil.GetId()}, "")
	require.NoError(t, err)

	// Typed in a packaging unit nobody defined, the count is refused.
	_, err = owner.API.CountStockLine(ctx, garageID, count.GetId(), count.GetLines()[0].GetId(), "00000000-0000-0000-0000-000000000000", "1")
	require.Error(t, err)

	count, err = owner.API.CountStockLine(ctx, garageID, count.GetId(), count.GetLines()[0].GetId(), "", "9")
	require.NoError(t, err)

	_, err = owner.API.CompleteStockCount(ctx, garageID, count.GetId(), false)
	require.NoError(t, err)

	_, err = owner.API.CreateWriteOff(ctx, garageID, &inventoryv1.WriteOff{
		Reason: inventoryv1.WriteOffReason_WRITE_OFF_REASON_LOST,
		Lines:  []*inventoryv1.WriteOffLine{{PartId: oil.GetId(), EnteredQuantity: "8"}},
	})
	require.NoError(t, err)

	_, err = owner.API.CancelStockCount(ctx, garageID, count.GetId())
	fixture.RequireAPIError(t, err, http.StatusConflict, "INSUFFICIENT_STOCK_TO_REVERSE")
}

// Scenario: Release 7.1, write-off (FR-INV-09): a document with a reason
// reduces stock at average cost, is refused beyond what is on hand, shows in
// the stock card, and cancelling it puts the parts back.
func TestWriteOffReducesStockAtAverageCost(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage := setup(t, "inv-writeoff")
	garageID := garage.GetId()
	parts := stockedParts(t, owner, garageID, litre(t, owner, garageID), stockLot{"10", 100}, stockLot{"4", 50})
	oil, filter := parts[0], parts[1]

	writeOff, err := owner.API.CreateWriteOff(ctx, garageID, &inventoryv1.WriteOff{
		Reason: inventoryv1.WriteOffReason_WRITE_OFF_REASON_DAMAGED, Notes: "dropped in transit",
		Lines: []*inventoryv1.WriteOffLine{
			{PartId: oil.GetId(), EnteredQuantity: "3"}, {PartId: filter.GetId(), EnteredQuantity: "1"},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, inventoryv1.AdjustmentStatus_ADJUSTMENT_STATUS_COMPLETED, writeOff.GetStatus())
	assert.NotEmpty(t, writeOff.GetNumber())
	assert.EqualValues(t, 3*100+1*50, writeOff.GetTotalValue())
	assert.EqualValues(t, 100, writeOff.GetLines()[0].GetUnitCost(), "at the average cost of the moment")
	assert.Equal(t, "7", onHand(t, owner.API, garageID, oil))
	assert.Equal(t, "3", onHand(t, owner.API, garageID, filter))

	// A reason is required, and the stock must cover the quantity.
	_, err = owner.API.CreateWriteOff(ctx, garageID, &inventoryv1.WriteOff{Lines: []*inventoryv1.WriteOffLine{{PartId: oil.GetId(), EnteredQuantity: "1"}}})
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "")

	_, err = owner.API.CreateWriteOff(ctx, garageID, &inventoryv1.WriteOff{
		Reason: inventoryv1.WriteOffReason_WRITE_OFF_REASON_EXPIRED,
		Lines:  []*inventoryv1.WriteOffLine{{PartId: oil.GetId(), EnteredQuantity: "70"}},
	})
	fixture.RequireAPIError(t, err, http.StatusConflict, "INSUFFICIENT_STOCK")

	card, err := owner.API.ListStockCard(ctx, garageID, oil.GetId(), inventoryv1.StockTransactionRefType_STOCK_TRANSACTION_REF_TYPE_WRITE_OFF)
	require.NoError(t, err)
	require.Len(t, card, 1)
	assert.Equal(t, writeOff.GetNumber(), card[0].GetRefNumber())
	assert.Equal(t, "damaged", card[0].GetReason())
	assert.Equal(t, "7", card[0].GetQuantityAfter())

	fetched, err := owner.API.GetWriteOff(ctx, garageID, writeOff.GetId())
	require.NoError(t, err)
	assert.Equal(t, "dropped in transit", fetched.GetNotes())

	listed, err := owner.API.ListWriteOffs(ctx, garageID, url.Values{"reason": {"1"}})
	require.NoError(t, err)
	require.Len(t, listed, 1)

	none, err := owner.API.ListWriteOffs(ctx, garageID, url.Values{"reason": {"3"}})
	require.NoError(t, err)
	assert.Empty(t, none)

	cancelled, err := owner.API.CancelWriteOff(ctx, garageID, writeOff.GetId())
	require.NoError(t, err)
	assert.Equal(t, inventoryv1.AdjustmentStatus_ADJUSTMENT_STATUS_CANCELLED, cancelled.GetStatus())
	assert.Equal(t, "10", onHand(t, owner.API, garageID, oil))

	level, err := owner.API.GetStockLevel(ctx, garageID, oil.GetId())
	require.NoError(t, err)
	assert.EqualValues(t, 100, level.GetAverageCost())

	_, err = owner.API.CancelWriteOff(ctx, garageID, writeOff.GetId())
	fixture.RequireAPIError(t, err, http.StatusConflict, "WRITE_OFF_ALREADY_CANCELLED")
}

// Scenario: Release 7.1, return to supplier (FR-INV-10): stock falls and the
// supplier credit is recorded, defaulting to what the linked receipt paid; a
// receipt of another supplier is refused; cancelling restores the stock.
func TestSupplierReturnRecordsTheCredit(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage := setup(t, "inv-supplier-return")
	garageID := garage.GetId()
	unit := litre(t, owner, garageID)
	oil := fixture.NewPart(t, owner.API, garageID, sku(), "Engine oil", unit)

	receipt, err := owner.API.ReceiveStock(ctx, garageID, oil.GetId(), "10", "", 120)
	require.NoError(t, err)

	supplierID := receipt.GetLines()[0].GetSupplierId()

	ret, err := owner.API.CreateSupplierReturn(ctx, garageID, &inventoryv1.SupplierReturn{
		SupplierId: supplierID, GoodsReceiptId: new(receipt.GetId()), CreditNoteNumber: "CN-1001", Notes: "wrong grade",
		Lines: []*inventoryv1.SupplierReturnLine{{PartId: oil.GetId(), EnteredQuantity: "4"}},
	})
	require.NoError(t, err)
	assert.Equal(t, inventoryv1.AdjustmentStatus_ADJUSTMENT_STATUS_COMPLETED, ret.GetStatus())
	assert.Equal(t, receipt.GetNumber(), ret.GetGoodsReceiptNumber())
	assert.NotEmpty(t, ret.GetSupplierName())
	assert.EqualValues(t, 480, ret.GetCreditAmount(), "4 x the 120 the receipt paid")
	assert.Equal(t, "6", onHand(t, owner.API, garageID, oil))

	// A typed credit wins.
	typed, err := owner.API.CreateSupplierReturn(ctx, garageID, &inventoryv1.SupplierReturn{
		SupplierId: supplierID, Lines: []*inventoryv1.SupplierReturnLine{{PartId: oil.GetId(), EnteredQuantity: "1", UnitCost: new(int64(90))}},
	})
	require.NoError(t, err)
	assert.EqualValues(t, 90, typed.GetCreditAmount())

	// Refusals: more than on hand, an unknown receipt link to another supplier.
	_, err = owner.API.CreateSupplierReturn(ctx, garageID, &inventoryv1.SupplierReturn{
		SupplierId: supplierID, Lines: []*inventoryv1.SupplierReturnLine{{PartId: oil.GetId(), EnteredQuantity: "60"}},
	})
	fixture.RequireAPIError(t, err, http.StatusConflict, "INSUFFICIENT_STOCK")

	other := supplier(t, owner.API, garageID, "Other ")
	_, err = owner.API.CreateSupplierReturn(ctx, garageID, &inventoryv1.SupplierReturn{
		SupplierId: other.GetId(), GoodsReceiptId: new(receipt.GetId()),
		Lines: []*inventoryv1.SupplierReturnLine{{PartId: oil.GetId(), EnteredQuantity: "1"}},
	})
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "")

	// The returned receipt can still be listed with the document.
	listed, err := owner.API.ListSupplierReturns(ctx, garageID, url.Values{"supplierId": {supplierID}})
	require.NoError(t, err)
	assert.Len(t, listed, 2)

	fetched, err := owner.API.GetSupplierReturn(ctx, garageID, ret.GetId())
	require.NoError(t, err)
	assert.Equal(t, "CN-1001", fetched.GetCreditNoteNumber())

	card, err := owner.API.ListStockCard(ctx, garageID, oil.GetId(), inventoryv1.StockTransactionRefType_STOCK_TRANSACTION_REF_TYPE_SUPPLIER_RETURN)
	require.NoError(t, err)
	require.Len(t, card, 2)

	cancelled, err := owner.API.CancelSupplierReturn(ctx, garageID, ret.GetId())
	require.NoError(t, err)
	assert.Equal(t, inventoryv1.AdjustmentStatus_ADJUSTMENT_STATUS_CANCELLED, cancelled.GetStatus())
	assert.Equal(t, "9", onHand(t, owner.API, garageID, oil))

	_, err = owner.API.CancelSupplierReturn(ctx, garageID, ret.GetId())
	fixture.RequireAPIError(t, err, http.StatusConflict, "SUPPLIER_RETURN_ALREADY_CANCELLED")
}

// Scenario: Release 7.1, return of unused parts from a repair order
// (FR-INV-11): once the stock issue is posted a part line can give parts back,
// with a reason, restoring stock and cost; more than was issued is refused.
func TestUnusedRepairOrderPartsReturnToStock(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage := setup(t, "inv-part-return")
	garageID := garage.GetId()
	oil := stockedParts(t, owner, garageID, litre(t, owner, garageID), stockLot{"10", 100})[0]

	order := orderWithParts(t, owner.API, garageID, [2]string{oil.GetId(), "4"})
	lineID := order.GetLines()[0].GetId()
	issue := waitForStockIssue(t, owner.API, garageID, order.GetId())

	returnPart := func(quantity, reason string) (*inventoryv1.RepairOrderPartReturn, error) {
		return owner.API.ReturnRepairOrderPart(ctx, garageID, order.GetId(), &inventoryv1.RepairOrderPartReturn{
			RepairOrderLineId: lineID, EnteredQuantity: quantity, Reason: reason,
		})
	}

	// Nothing has left stock while the issue is a draft.
	_, err := returnPart("1", "not needed")
	fixture.RequireAPIError(t, err, http.StatusConflict, "STOCK_ISSUE_NOT_POSTED")

	_, err = owner.API.PostStockIssue(ctx, garageID, issue.GetId())
	require.NoError(t, err)
	assert.Equal(t, "6", onHand(t, owner.API, garageID, oil))

	_, err = returnPart("1", "  ")
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "")

	back, err := returnPart("1.5", "customer declined the extra oil")
	require.NoError(t, err)
	assert.Equal(t, "1.5", back.GetQuantity())
	assert.Equal(t, "customer declined the extra oil", back.GetReason())
	assert.EqualValues(t, 100, back.GetUnitCost(), "restored at the cost it left with")
	assert.Equal(t, "7.5", onHand(t, owner.API, garageID, oil))

	level, err := owner.API.GetStockLevel(ctx, garageID, oil.GetId())
	require.NoError(t, err)
	assert.EqualValues(t, 100, level.GetAverageCost())

	_, err = returnPart("3", "too many")
	fixture.RequireAPIError(t, err, http.StatusConflict, "RETURN_EXCEEDS_ISSUED")

	_, err = owner.API.ReturnRepairOrderPart(ctx, garageID, order.GetId(), &inventoryv1.RepairOrderPartReturn{
		RepairOrderLineId: "00000000-0000-0000-0000-000000000000", EnteredQuantity: "1", Reason: "x",
	})
	fixture.RequireAPIError(t, err, http.StatusNotFound, "")

	returns, err := owner.API.ListRepairOrderPartReturns(ctx, garageID, order.GetId())
	require.NoError(t, err)
	require.Len(t, returns, 1)
	assert.Equal(t, oil.GetId(), returns[0].GetPartId())

	issue, err = owner.API.GetRepairOrderStockIssue(ctx, garageID, order.GetId())
	require.NoError(t, err)
	assert.Equal(t, "1.5", issue.GetLines()[0].GetReturnedQuantity())

	card, err := owner.API.ListStockCard(ctx, garageID, oil.GetId(), inventoryv1.StockTransactionRefType_STOCK_TRANSACTION_REF_TYPE_REPAIR_ORDER_RETURN)
	require.NoError(t, err)
	require.Len(t, card, 1)
	assert.Equal(t, "customer declined the extra oil", card[0].GetReason())
	assert.Equal(t, "7.5", card[0].GetQuantityAfter())
}

// Scenario: Release 7.1, permissions: inventory.count and inventory.adjust are
// separate from inventory.write. A technician reads stock but cannot count,
// write off, return to supplier or return repair order parts; inventory staff
// can; the count's cost fields follow inventory.cost.read.
func TestAdjustmentPermissions(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwnerWithLocale(t, "inv-adjust-perms", identityv1.Locale_LOCALE_EN)

	garage, err := owner.API.CreateGarage(ctx, &tenantv1.Garage{Name: "Blackbox adjust perms"})
	require.NoError(t, err)

	garageID := garage.GetId()
	oil := stockedParts(t, owner, garageID, litre(t, owner, garageID), stockLot{"10", 100})[0]
	tech := joinAs(t, owner, garageID, "Technician", "inv-adjust-tech")
	staff := joinAs(t, owner, garageID, "Inventory Staff", "inv-adjust-staff")

	count, err := owner.API.CreateStockCount(ctx, garageID, &inventoryv1.StockCount{Name: "Perms"})
	require.NoError(t, err)

	// The technician can read counts but not change anything.
	read, err := tech.API.GetStockCount(ctx, garageID, count.GetId())
	require.NoError(t, err)
	assert.Nil(t, read.TotalDifferenceValue, "cost is hidden without inventory.cost.read")

	_, err = tech.API.CreateStockCount(ctx, garageID, &inventoryv1.StockCount{})
	fixture.RequireAPIError(t, err, http.StatusForbidden, "")

	_, err = tech.API.AddStockCountLines(ctx, garageID, count.GetId(), []string{oil.GetId()}, "")
	fixture.RequireAPIError(t, err, http.StatusForbidden, "")

	_, err = tech.API.CreateWriteOff(ctx, garageID, &inventoryv1.WriteOff{
		Reason: inventoryv1.WriteOffReason_WRITE_OFF_REASON_LOST, Lines: []*inventoryv1.WriteOffLine{{PartId: oil.GetId(), EnteredQuantity: "1"}},
	})
	fixture.RequireAPIError(t, err, http.StatusForbidden, "")

	_, err = tech.API.CreateSupplierReturn(ctx, garageID, &inventoryv1.SupplierReturn{
		SupplierId: "00000000-0000-0000-0000-000000000000", Lines: []*inventoryv1.SupplierReturnLine{{PartId: oil.GetId(), EnteredQuantity: "1"}},
	})
	fixture.RequireAPIError(t, err, http.StatusForbidden, "")

	_, err = tech.API.ReturnRepairOrderPart(ctx, garageID, "00000000-0000-0000-0000-000000000000", &inventoryv1.RepairOrderPartReturn{})
	fixture.RequireAPIError(t, err, http.StatusForbidden, "")

	_, err = tech.API.ListWriteOffs(ctx, garageID, nil)
	require.NoError(t, err)

	// Inventory staff count and adjust.
	counted, err := staff.API.AddStockCountLines(ctx, garageID, count.GetId(), []string{oil.GetId()}, "")
	require.NoError(t, err)
	assert.Len(t, counted.GetLines(), 1)

	_, err = staff.API.CreateWriteOff(ctx, garageID, &inventoryv1.WriteOff{
		Reason: inventoryv1.WriteOffReason_WRITE_OFF_REASON_INTERNAL_USE, Lines: []*inventoryv1.WriteOffLine{{PartId: oil.GetId(), EnteredQuantity: "1"}},
	})
	require.NoError(t, err)
	assert.Equal(t, "9", onHand(t, owner.API, garageID, oil))
}
