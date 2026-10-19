package jobs_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	cataloguev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/catalogue/v1"
	inventoryv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/inventory/v1"
	quotev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/quote/v1"
	repairorderv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/repairorder/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/client"
	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

func laborLine(description, quantity string, price int64) *repairorderv1.RepairOrderLine {
	return &repairorderv1.RepairOrderLine{
		ItemType:    repairorderv1.RepairOrderItemType_REPAIR_ORDER_ITEM_TYPE_LABOR,
		Description: description, Quantity: quantity, UnitPrice: price,
	}
}

func partLine(part *cataloguev1.Part, quantity string, price int64) *repairorderv1.RepairOrderLine {
	return &repairorderv1.RepairOrderLine{
		ItemType:  repairorderv1.RepairOrderItemType_REPAIR_ORDER_ITEM_TYPE_PART,
		ItemRefId: new(part.GetId()), Description: part.GetName(), Quantity: quantity, UnitPrice: price,
	}
}

func newOrder(t *testing.T, k *garageKit, lines ...*repairorderv1.RepairOrderLine) *repairorderv1.RepairOrder {
	t.Helper()

	order, err := k.owner.API.CreateRepairOrder(t.Context(), k.garageID, &repairorderv1.RepairOrder{VehicleId: k.vehicle.GetId(), Lines: lines})
	require.NoError(t, err)

	return order
}

func orderLine(order *repairorderv1.RepairOrder, description string) *repairorderv1.RepairOrderLine {
	for _, l := range order.GetLines() {
		if l.GetDescription() == description {
			return l
		}
	}

	return nil
}

// complete marks every line of the order completed.
func complete(t *testing.T, api *client.Client, garageID string, order *repairorderv1.RepairOrder) *repairorderv1.RepairOrder {
	t.Helper()

	var err error

	for _, l := range order.GetLines() {
		if l.GetNonBillable() {
			continue
		}

		order, err = api.UpdateRepairOrderLine(t.Context(), garageID, order.GetId(), l.GetId(),
			&repairorderv1.RepairOrderLine{Status: repairorderv1.RepairOrderLineStatus_REPAIR_ORDER_LINE_STATUS_COMPLETED})
		require.NoError(t, err)
	}

	return order
}

func receive(t *testing.T, k *garageKit, part *cataloguev1.Part, quantity string, cost int64) {
	t.Helper()

	_, err := k.owner.API.ReceiveStock(t.Context(), k.garageID, part.GetId(), quantity, "", cost)
	require.NoError(t, err)
}

// Scenario: E2E-REP-013, Release 7.3, editing lines (FR-REP-07): lines are added, edited
// and removed on an open order, the totals follow, a line in a packaging unit
// takes its amount from the entered quantity, and the last line, a closed
// order, a full order and an invalid line are refused.
//
//nolint:funlen // one scenario, read top to bottom
func TestRepairOrderLinesAddEditRemove(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	k := setup(t, "ro-lines")
	api, garageID := k.owner.API, k.garageID
	c := k.catalogue(t)
	order := newOrder(t, k, laborLine("Oil change", "1", 250_000))
	orderID := order.GetId()

	t.Run("a labor line and a part line are added", func(t *testing.T) {
		o, err := api.AddRepairOrderLine(ctx, garageID, orderID, laborLine("Wash", "1", 50_000))
		require.NoError(t, err)
		assert.Len(t, o.GetLines(), 2)
		assert.EqualValues(t, 300_000, o.GetTotalAmount())
		assert.Equal(t, repairorderv1.RepairOrderLineStatus_REPAIR_ORDER_LINE_STATUS_NOT_STARTED, orderLine(o, "Wash").GetStatus())

		o, err = api.AddRepairOrderLine(ctx, garageID, orderID, partLine(c.oil, "4", 90_000))
		require.NoError(t, err)
		assert.Len(t, o.GetLines(), 3)
		assert.EqualValues(t, 300_000+360_000, o.GetTotalAmount())
	})

	current, err := api.GetRepairOrder(ctx, garageID, orderID)
	require.NoError(t, err)

	wash, oil := orderLine(current, "Wash"), orderLine(current, c.oil.GetName())

	t.Run("a description and a price are edited", func(t *testing.T) {
		o, err := api.UpdateRepairOrderLineMasked(ctx, garageID, orderID, wash.GetId(),
			&repairorderv1.RepairOrderLine{Description: "Full wash", UnitPrice: 80_000}, "description", "unit_price")
		require.NoError(t, err)

		edited := orderLine(o, "Full wash")
		require.NotNil(t, edited)
		assert.Equal(t, wash.GetId(), edited.GetId(), "the line keeps its identity")
		assert.EqualValues(t, 80_000, edited.GetLineTotal())
		assert.EqualValues(t, 250_000+80_000+360_000, o.GetTotalAmount())
	})

	t.Run("a quantity is edited", func(t *testing.T) {
		o, err := api.UpdateRepairOrderLineMasked(ctx, garageID, orderID, oil.GetId(), &repairorderv1.RepairOrderLine{Quantity: "5"}, "quantity")
		require.NoError(t, err)
		assert.EqualValues(t, 450_000, orderLine(o, c.oil.GetName()).GetLineTotal())
		assert.EqualValues(t, 250_000+80_000+450_000, o.GetTotalAmount())
	})

	t.Run("a packaging unit takes its amount from the entered quantity", func(t *testing.T) {
		o, err := api.UpdateRepairOrderLineMasked(ctx, garageID, orderID, oil.GetId(),
			&repairorderv1.RepairOrderLine{UnitId: new(c.jug.GetId()), EnteredQuantity: "1"}, "unit_id", "entered_quantity")
		require.NoError(t, err)

		line := orderLine(o, c.oil.GetName())
		assert.Equal(t, "5", line.GetQuantity(), "one jug of 5 L")
		assert.Equal(t, "jug", line.GetUnitAbbreviation())
		assert.EqualValues(t, 450_000, line.GetLineTotal())

		_, err = api.UpdateRepairOrderLineMasked(ctx, garageID, orderID, oil.GetId(), &repairorderv1.RepairOrderLine{Quantity: "3"}, "quantity")
		fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "")

		o, err = api.UpdateRepairOrderLineMasked(ctx, garageID, orderID, oil.GetId(),
			&repairorderv1.RepairOrderLine{UnitId: new(""), Quantity: "3"}, "unit_id", "quantity")
		require.NoError(t, err)

		back := orderLine(o, c.oil.GetName())
		assert.Equal(t, "3", back.GetQuantity())
		assert.Empty(t, back.GetUnitId(), "back to the base unit")
	})

	t.Run("invalid edits are refused and change nothing", func(t *testing.T) {
		for _, tt := range []struct {
			line *repairorderv1.RepairOrderLine
			mask string
		}{
			{&repairorderv1.RepairOrderLine{Description: "  "}, "description"},
			{&repairorderv1.RepairOrderLine{UnitPrice: -1}, "unit_price"},
			{&repairorderv1.RepairOrderLine{Quantity: "0"}, "quantity"},
			{&repairorderv1.RepairOrderLine{Quantity: "lots"}, "quantity"},
		} {
			_, err := api.UpdateRepairOrderLineMasked(ctx, garageID, orderID, oil.GetId(), tt.line, tt.mask)
			fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "")
		}

		_, err := api.UpdateRepairOrderLineMasked(ctx, garageID, orderID, oil.GetId(), &repairorderv1.RepairOrderLine{Description: "x"}, "id")
		fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "")

		_, err = api.UpdateRepairOrderLineMasked(ctx, garageID, orderID, uuid.NewString(), &repairorderv1.RepairOrderLine{Description: "x"}, "description")
		fixture.RequireAPIError(t, err, http.StatusNotFound, "")

		unchanged, err := api.GetRepairOrder(ctx, garageID, orderID)
		require.NoError(t, err)
		assert.Equal(t, "3", orderLine(unchanged, c.oil.GetName()).GetQuantity())
	})

	t.Run("invalid lines are refused", func(t *testing.T) {
		noRef := partLine(c.oil, "1", 1)
		noRef.ItemRefId = nil
		noRef.NonBillable = true

		for _, line := range []*repairorderv1.RepairOrderLine{
			laborLine("", "1", 1), laborLine("x", "0", 1), laborLine("x", "1", -1), noRef,
		} {
			_, err := api.AddRepairOrderLine(ctx, garageID, orderID, line)
			fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "")
		}
	})

	t.Run("a line is removed", func(t *testing.T) {
		o, err := api.DeleteRepairOrderLine(ctx, garageID, orderID, wash.GetId())
		require.NoError(t, err)
		assert.Len(t, o.GetLines(), 2)
		assert.Nil(t, orderLine(o, "Full wash"))
		assert.EqualValues(t, 250_000+270_000, o.GetTotalAmount())

		_, err = api.DeleteRepairOrderLine(ctx, garageID, orderID, wash.GetId())
		fixture.RequireAPIError(t, err, http.StatusNotFound, "")
	})

	t.Run("the last line stays", func(t *testing.T) {
		o, err := api.DeleteRepairOrderLine(ctx, garageID, orderID, oil.GetId())
		require.NoError(t, err)
		require.Len(t, o.GetLines(), 1)

		_, err = api.DeleteRepairOrderLine(ctx, garageID, orderID, o.GetLines()[0].GetId())
		fixture.RequireAPIError(t, err, http.StatusConflict, "LAST_LINE")
	})

	t.Run("an order cannot be left with only a hidden consumable", func(t *testing.T) {
		consumable := partLine(c.filter, "1", 0)
		consumable.NonBillable = true

		o := newOrder(t, k, laborLine("Work", "1", 1000), consumable)
		labor := orderLine(o, "Work")

		_, err := api.DeleteRepairOrderLine(ctx, garageID, o.GetId(), labor.GetId())
		fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "")
	})

	t.Run("a full order takes no more lines", func(t *testing.T) {
		lines := make([]*repairorderv1.RepairOrderLine, 0, 100)
		for range 100 {
			lines = append(lines, laborLine("Line", "1", 1))
		}

		full := newOrder(t, k, lines...)

		_, err := api.AddRepairOrderLine(ctx, garageID, full.GetId(), laborLine("One more", "1", 1))
		fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "")
	})

	t.Run("a closed order cannot be changed", func(t *testing.T) {
		done := complete(t, api, garageID, newOrder(t, k, laborLine("Quick job", "1", 10_000)))

		_, err := api.CloseRepairOrder(ctx, garageID, done.GetId())
		require.NoError(t, err)

		line := done.GetLines()[0]

		_, err = api.AddRepairOrderLine(ctx, garageID, done.GetId(), laborLine("Late", "1", 1))
		fixture.RequireAPIError(t, err, http.StatusConflict, "")

		_, err = api.UpdateRepairOrderLineMasked(ctx, garageID, done.GetId(), line.GetId(), &repairorderv1.RepairOrderLine{Description: "x"}, "description")
		fixture.RequireAPIError(t, err, http.StatusConflict, "")

		_, err = api.DeleteRepairOrderLine(ctx, garageID, done.GetId(), line.GetId())
		fixture.RequireAPIError(t, err, http.StatusConflict, "")
	})

	t.Run("an unknown order is not found", func(t *testing.T) {
		_, err := api.AddRepairOrderLine(ctx, garageID, uuid.NewString(), laborLine("x", "1", 1))
		fixture.RequireAPIError(t, err, http.StatusNotFound, "")
	})
}

// Scenario: E2E-REP-014, Release 7.3, the draft stock issue follows the part lines: the
// reservation grows, shrinks and moves with them; once the issue is posted the
// parts are fixed (STOCK_ISSUE_POSTED) while labor and prices stay editable.
func TestStockIssueFollowsTheLines(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	k := setup(t, "ro-issue")
	api, garageID := k.owner.API, k.garageID
	c := k.catalogue(t)
	receive(t, k, c.oil, "20", 100)
	receive(t, k, c.filter, "5", 2000)

	order := newOrder(t, k, laborLine("Work", "1", 100_000), partLine(c.oil, "4", 90_000))
	waitForReserved(t, api, garageID, c.oil.GetId(), "4")

	oil := orderLine(order, c.oil.GetName())

	t.Run("a new quantity moves the reservation", func(t *testing.T) {
		_, err := api.UpdateRepairOrderLineMasked(ctx, garageID, order.GetId(), oil.GetId(), &repairorderv1.RepairOrderLine{Quantity: "6"}, "quantity")
		require.NoError(t, err)

		waitForReserved(t, api, garageID, c.oil.GetId(), "6")
	})

	t.Run("a new part line is reserved and a removed one is freed", func(t *testing.T) {
		o, err := api.AddRepairOrderLine(ctx, garageID, order.GetId(), partLine(c.filter, "1", 150_000))
		require.NoError(t, err)

		waitForReserved(t, api, garageID, c.filter.GetId(), "1")

		_, err = api.DeleteRepairOrderLine(ctx, garageID, order.GetId(), orderLine(o, c.filter.GetName()).GetId())
		require.NoError(t, err)

		waitForReserved(t, api, garageID, c.filter.GetId(), "0")
	})

	t.Run("a unit and a part change follow", func(t *testing.T) {
		_, err := api.UpdateRepairOrderLineMasked(ctx, garageID, order.GetId(), oil.GetId(),
			&repairorderv1.RepairOrderLine{UnitId: new(c.jug.GetId()), EnteredQuantity: "2"}, "unit_id", "entered_quantity")
		require.NoError(t, err)

		waitForReserved(t, api, garageID, c.oil.GetId(), "10")

		_, err = api.UpdateRepairOrderLineMasked(ctx, garageID, order.GetId(), oil.GetId(),
			&repairorderv1.RepairOrderLine{UnitId: new(""), ItemRefId: new(c.filter.GetId()), Quantity: "2"}, "unit_id", "item_ref_id", "quantity")
		require.NoError(t, err)

		waitForReserved(t, api, garageID, c.oil.GetId(), "0")
		waitForReserved(t, api, garageID, c.filter.GetId(), "2")

		_, err = api.UpdateRepairOrderLineMasked(ctx, garageID, order.GetId(), oil.GetId(),
			&repairorderv1.RepairOrderLine{ItemRefId: new(c.oil.GetId()), Quantity: "6"}, "item_ref_id", "quantity")
		require.NoError(t, err)

		waitForReserved(t, api, garageID, c.oil.GetId(), "6")
		waitForReserved(t, api, garageID, c.filter.GetId(), "0")
	})

	t.Run("after Issue now the parts are fixed", func(t *testing.T) {
		issue := waitForStockIssue(t, api, garageID, order.GetId(), anyIssue)

		_, err := api.PostStockIssue(ctx, garageID, issue.GetId())
		require.NoError(t, err)

		_, err = api.UpdateRepairOrderLineMasked(ctx, garageID, order.GetId(), oil.GetId(), &repairorderv1.RepairOrderLine{Quantity: "9"}, "quantity")
		fixture.RequireAPIError(t, err, http.StatusConflict, "STOCK_ISSUE_POSTED")

		_, err = api.AddRepairOrderLine(ctx, garageID, order.GetId(), partLine(c.filter, "1", 150_000))
		fixture.RequireAPIError(t, err, http.StatusConflict, "STOCK_ISSUE_POSTED")

		_, err = api.DeleteRepairOrderLine(ctx, garageID, order.GetId(), oil.GetId())
		fixture.RequireAPIError(t, err, http.StatusConflict, "STOCK_ISSUE_POSTED")

		// Labor, prices and descriptions can still change.
		o, err := api.UpdateRepairOrderLineMasked(ctx, garageID, order.GetId(), oil.GetId(),
			&repairorderv1.RepairOrderLine{UnitPrice: 95_000, Description: "Engine oil (premium)"}, "unit_price", "description")
		require.NoError(t, err)
		assert.EqualValues(t, 6*95_000, orderLine(o, "Engine oil (premium)").GetLineTotal())

		_, err = api.AddRepairOrderLine(ctx, garageID, order.GetId(), laborLine("Extra work", "1", 20_000))
		require.NoError(t, err)

		still, err := api.GetRepairOrderStockIssue(ctx, garageID, order.GetId())
		require.NoError(t, err)
		assert.Equal(t, inventoryv1.StockIssueStatus_STOCK_ISSUE_STATUS_POSTED, still.GetStatus())
	})
}

// Scenario: E2E-REP-015, Release 7.3, cancelling (FR-REP-08): an open order is cancelled,
// its stock issue is cancelled and the reservation released; the order is
// read-only; the quote stays converted; started, completed, closed and
// already cancelled orders, and one whose stock issue is posted, are refused.
//
//nolint:funlen // one scenario, read top to bottom
func TestCancelRepairOrder(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	k := setup(t, "ro-cancel")
	api, garageID := k.owner.API, k.garageID
	c := k.catalogue(t)
	receive(t, k, c.oil, "10", 100)

	quote := draft(t, k, quoteLabor("Work", 100_000), quotePart(c.oil, "4", 90_000))

	_, err := api.SendQuote(ctx, garageID, quote.GetId())
	require.NoError(t, err)

	_, err = api.RecordQuoteDecision(ctx, garageID, quote.GetId(), billableDecisions(quote, func(*quotev1.QuoteLine) bool { return true })...)
	require.NoError(t, err)

	converted, err := api.ConvertQuote(ctx, garageID, quote.GetId())
	require.NoError(t, err)

	orderID := converted.GetRepairOrderId()

	waitForReserved(t, api, garageID, c.oil.GetId(), "4")

	t.Run("an open order is cancelled and its reservation released", func(t *testing.T) {
		cancelled, err := api.CancelRepairOrder(ctx, garageID, orderID)
		require.NoError(t, err)
		assert.Equal(t, repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_CANCELLED, cancelled.GetStatus())

		waitForReserved(t, api, garageID, c.oil.GetId(), "0")

		issue := waitForStockIssue(t, api, garageID, orderID, func(i *inventoryv1.StockIssue) bool {
			return i.GetStatus() == inventoryv1.StockIssueStatus_STOCK_ISSUE_STATUS_CANCELLED
		})
		assert.False(t, issue.GetIsShort())

		level, err := api.GetStockLevel(ctx, garageID, c.oil.GetId())
		require.NoError(t, err)
		assert.Equal(t, "10", level.GetQuantityOnHand(), "nothing left the shelf")
		assert.Equal(t, "10", level.GetQuantityAvailable())
	})

	t.Run("the quote stays converted", func(t *testing.T) {
		got, err := api.GetQuote(ctx, garageID, quote.GetId())
		require.NoError(t, err)
		assert.Equal(t, quotev1.QuoteStatus_QUOTE_STATUS_CONVERTED, got.GetStatus())

		_, err = api.ConvertQuote(ctx, garageID, quote.GetId())
		fixture.RequireAPIError(t, err, http.StatusConflict, "")
	})

	t.Run("a cancelled order is read-only and final", func(t *testing.T) {
		got, err := api.GetRepairOrder(ctx, garageID, orderID)
		require.NoError(t, err)

		line := got.GetLines()[0]

		_, err = api.UpdateRepairOrderLine(ctx, garageID, orderID, line.GetId(),
			&repairorderv1.RepairOrderLine{Status: repairorderv1.RepairOrderLineStatus_REPAIR_ORDER_LINE_STATUS_IN_PROGRESS})
		fixture.RequireAPIError(t, err, http.StatusConflict, "")

		_, err = api.AddRepairOrderLine(ctx, garageID, orderID, laborLine("Late", "1", 1))
		fixture.RequireAPIError(t, err, http.StatusConflict, "")

		_, err = api.DeleteRepairOrderLine(ctx, garageID, orderID, line.GetId())
		fixture.RequireAPIError(t, err, http.StatusConflict, "")

		_, err = api.CloseRepairOrder(ctx, garageID, orderID)
		fixture.RequireAPIError(t, err, http.StatusConflict, "")

		_, err = api.CancelRepairOrder(ctx, garageID, orderID)
		fixture.RequireAPIError(t, err, http.StatusConflict, "REPAIR_ORDER_NOT_CANCELLABLE")

		all, err := api.ListRepairOrders(ctx, garageID)
		require.NoError(t, err)

		var found bool

		for _, o := range all {
			if o.GetId() == orderID {
				found = true

				assert.Equal(t, repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_CANCELLED, o.GetStatus())
			}
		}

		assert.True(t, found, "a cancelled order stays in the list")
	})

	t.Run("started, completed and closed orders cannot be cancelled", func(t *testing.T) {
		started := newOrder(t, k, laborLine("Started", "1", 1000), laborLine("Later", "1", 1000))

		_, err := api.UpdateRepairOrderLine(ctx, garageID, started.GetId(), started.GetLines()[0].GetId(),
			&repairorderv1.RepairOrderLine{Status: repairorderv1.RepairOrderLineStatus_REPAIR_ORDER_LINE_STATUS_IN_PROGRESS})
		require.NoError(t, err)

		_, err = api.CancelRepairOrder(ctx, garageID, started.GetId())
		fixture.RequireAPIError(t, err, http.StatusConflict, "REPAIR_ORDER_NOT_CANCELLABLE")

		finished := complete(t, api, garageID, newOrder(t, k, laborLine("Finished", "1", 1000)))

		_, err = api.CancelRepairOrder(ctx, garageID, finished.GetId())
		fixture.RequireAPIError(t, err, http.StatusConflict, "REPAIR_ORDER_NOT_CANCELLABLE")

		_, err = api.CloseRepairOrder(ctx, garageID, finished.GetId())
		require.NoError(t, err)

		_, err = api.CancelRepairOrder(ctx, garageID, finished.GetId())
		fixture.RequireAPIError(t, err, http.StatusConflict, "REPAIR_ORDER_NOT_CANCELLABLE")
	})

	t.Run("an order whose stock issue is posted cannot be cancelled", func(t *testing.T) {
		withIssue := newOrder(t, k, laborLine("Work", "1", 1000), partLine(c.oil, "1", 90_000))
		issue := waitForStockIssue(t, api, garageID, withIssue.GetId(), anyIssue)

		_, err := api.PostStockIssue(ctx, garageID, issue.GetId())
		require.NoError(t, err)

		_, err = api.CancelRepairOrder(ctx, garageID, withIssue.GetId())
		fixture.RequireAPIError(t, err, http.StatusConflict, "STOCK_ISSUE_POSTED")
	})

	t.Run("an unknown order is not found", func(t *testing.T) {
		_, err := api.CancelRepairOrder(ctx, garageID, uuid.NewString())
		fixture.RequireAPIError(t, err, http.StatusNotFound, "")
	})
}

// Scenario: E2E-REP-016, Release 7.3, waiting on parts (FR-INV-15): an order whose draft
// stock issue the stock cannot cover waits on parts and the issue says how
// much is short; reservations are served first come, first served; stock that
// arrives, a cancelled order or a smaller quantity brings it back; a started
// order that is short shows waiting too; a finished one does not.
func TestWaitingOnParts(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	k := setup(t, "ro-waiting")
	api, garageID := k.owner.API, k.garageID
	c := k.catalogue(t)
	receive(t, k, c.oil, "5", 100)

	oilOrder := func(quantity string) *repairorderv1.RepairOrder {
		return newOrder(t, k, laborLine("Work", "1", 1000), partLine(c.oil, quantity, 90_000))
	}

	first := oilOrder("4")
	waitForStockIssue(t, api, garageID, first.GetId(), anyIssue)

	second := oilOrder("4")
	waitForStatus(t, api, garageID, second.GetId(), repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_WAITING_ON_PARTS)

	t.Run("the first order is served, the second waits and is told how much is short", func(t *testing.T) {
		got, err := api.GetRepairOrder(ctx, garageID, first.GetId())
		require.NoError(t, err)
		assert.Equal(t, repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_OPEN, got.GetStatus())

		issue := waitForStockIssue(t, api, garageID, second.GetId(), func(i *inventoryv1.StockIssue) bool { return i.GetIsShort() })
		require.Len(t, issue.GetLines(), 1)
		assert.Equal(t, "3", issue.GetLines()[0].GetShortQuantity(), "5 on hand, 4 held by the first order")

		firstIssue := waitForStockIssue(t, api, garageID, first.GetId(), anyIssue)
		assert.False(t, firstIssue.GetIsShort())
		assert.Equal(t, "0", firstIssue.GetLines()[0].GetShortQuantity())
	})

	t.Run("stock that arrives brings the order back by itself", func(t *testing.T) {
		receive(t, k, c.oil, "10", 100)

		waitForStatus(t, api, garageID, second.GetId(), repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_OPEN)

		issue := waitForStockIssue(t, api, garageID, second.GetId(), func(i *inventoryv1.StockIssue) bool { return !i.GetIsShort() })
		assert.Equal(t, "0", issue.GetLines()[0].GetShortQuantity())
	})

	t.Run("cancelling an order covers the next one", func(t *testing.T) {
		third := oilOrder("10") // 15 on hand, 8 held by the first two: 7 left
		waitForStatus(t, api, garageID, third.GetId(), repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_WAITING_ON_PARTS)

		_, err := api.CancelRepairOrder(ctx, garageID, first.GetId())
		require.NoError(t, err)

		waitForStatus(t, api, garageID, third.GetId(), repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_OPEN)
	})

	t.Run("a smaller quantity brings the order back", func(t *testing.T) {
		fourth := oilOrder("20") // 15 on hand, 4 + 10 held: 1 left
		waitForStatus(t, api, garageID, fourth.GetId(), repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_WAITING_ON_PARTS)

		_, err := api.UpdateRepairOrderLineMasked(ctx, garageID, fourth.GetId(), orderLine(fourth, c.oil.GetName()).GetId(),
			&repairorderv1.RepairOrderLine{Quantity: "1"}, "quantity")
		require.NoError(t, err)

		waitForStatus(t, api, garageID, fourth.GetId(), repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_OPEN)
	})

	t.Run("a started order that is short shows waiting; a finished one does not", func(t *testing.T) {
		big := oilOrder("100")
		waitForStatus(t, api, garageID, big.GetId(), repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_WAITING_ON_PARTS)

		started, err := api.UpdateRepairOrderLine(ctx, garageID, big.GetId(), orderLine(big, "Work").GetId(),
			&repairorderv1.RepairOrderLine{Status: repairorderv1.RepairOrderLineStatus_REPAIR_ORDER_LINE_STATUS_IN_PROGRESS})
		require.NoError(t, err)
		assert.Equal(t, repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_WAITING_ON_PARTS, started.GetStatus(), "work started, parts still missing")

		_, err = api.CancelRepairOrder(ctx, garageID, big.GetId())
		fixture.RequireAPIError(t, err, http.StatusConflict, "REPAIR_ORDER_NOT_CANCELLABLE")

		done := complete(t, api, garageID, big)
		assert.Equal(t, repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_COMPLETED, done.GetStatus())
	})

	t.Run("a waiting order that has not started can be cancelled", func(t *testing.T) {
		waiting := oilOrder("100")
		waitForStatus(t, api, garageID, waiting.GetId(), repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_WAITING_ON_PARTS)

		cancelled, err := api.CancelRepairOrder(ctx, garageID, waiting.GetId())
		require.NoError(t, err)
		assert.Equal(t, repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_CANCELLED, cancelled.GetStatus())
	})

	t.Run("a shortage never blocks the work or the stock issue", func(t *testing.T) {
		stuck := oilOrder("200")
		waitForStatus(t, api, garageID, stuck.GetId(), repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_WAITING_ON_PARTS)

		issue := waitForStockIssue(t, api, garageID, stuck.GetId(), func(i *inventoryv1.StockIssue) bool { return i.GetIsShort() })

		posted, err := api.PostStockIssue(ctx, garageID, issue.GetId())
		require.NoError(t, err)
		assert.False(t, posted.GetIsShort(), "a posted issue is no longer short")

		waitForStatus(t, api, garageID, stuck.GetId(), repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_OPEN)
	})
}

// Scenario: E2E-REP-017, Release 7.3, a hidden consumable on the order: reserved and
// deducted at its cost, no price, left out of the total and the settlement, and
// it never holds the order back.
func TestHiddenConsumableOnTheOrder(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	k := setup(t, "ro-hidden")
	api, garageID := k.owner.API, k.garageID
	c := k.catalogue(t)
	receive(t, k, c.filter, "5", 40_000)

	order := newOrder(t, k, laborLine("Oil change", "1", 250_000))

	consumable := partLine(c.filter, "1", 5_000)
	consumable.NonBillable = true

	with, err := api.AddRepairOrderLine(ctx, garageID, order.GetId(), consumable)
	require.NoError(t, err)
	assert.EqualValues(t, 250_000, with.GetTotalAmount())

	hiddenLine := orderLine(with, c.filter.GetName())
	assert.True(t, hiddenLine.GetNonBillable())
	assert.EqualValues(t, 0, hiddenLine.GetUnitPrice())

	waitForReserved(t, api, garageID, c.filter.GetId(), "1")

	// Only the billable line has to be completed.
	done := complete(t, api, garageID, with)
	assert.Equal(t, repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_COMPLETED, done.GetStatus())

	closed, err := api.CloseRepairOrder(ctx, garageID, order.GetId())
	require.NoError(t, err)
	assert.Equal(t, repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_CLOSED, closed.GetStatus())

	eventually(t, "the consumable leaves the stock", func() (bool, string) {
		level, err := api.GetStockLevel(ctx, garageID, c.filter.GetId())
		require.NoError(t, err)

		return level.GetQuantityOnHand() == "4", "on hand is " + level.GetQuantityOnHand()
	})

	outs, err := api.ListStockTransactionsByType(ctx, garageID, inventoryv1.StockTransactionType_STOCK_TRANSACTION_TYPE_OUT)
	require.NoError(t, err)

	var found bool

	for _, tx := range outs {
		if tx.GetPartId() == c.filter.GetId() {
			found = true

			assert.Equal(t, "1", tx.GetQuantity())
			assert.EqualValues(t, 40_000, tx.GetUnitCost(), "deducted at its cost")
		}
	}

	assert.True(t, found, "the consumable's movement")

	settlement, err := api.WithIdempotencyKey(uuid.NewString()).CreateSettlement(ctx, garageID, order.GetId())
	require.NoError(t, err)
	assert.EqualValues(t, 250_000, settlement.GetTotalAmount(), "the settlement does not bill the consumable")

	contentType, body, err := api.DownloadRepairOrderPDF(ctx, garageID, order.GetId())
	require.NoError(t, err)
	assert.Contains(t, contentType, "pdf")
	assert.NotEmpty(t, body)
}

// Scenario: E2E-REP-018, Release 7.3, permissions: repair_order.read sees the lines but
// changes nothing, repair_order.write adds, edits, removes and cancels, a member
// of another garage is refused.
func TestRepairOrderLinePermissions(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	k := setup(t, "ro-perms")
	garageID := k.garageID
	order := newOrder(t, k, laborLine("Work", "1", 1000), laborLine("More work", "1", 1000))
	orderID := order.GetId()
	lineID := order.GetLines()[0].GetId()

	t.Run("read-only access sees the lines and changes nothing", func(t *testing.T) {
		reader := staffIn(t, k, "ro-reader", "Inventory Staff")

		got, err := reader.API.GetRepairOrder(ctx, garageID, orderID)
		require.NoError(t, err)
		assert.Len(t, got.GetLines(), 2)

		_, err = reader.API.AddRepairOrderLine(ctx, garageID, orderID, laborLine("Nope", "1", 1))
		fixture.RequireAPIError(t, err, http.StatusForbidden, "")

		_, err = reader.API.UpdateRepairOrderLineMasked(ctx, garageID, orderID, lineID, &repairorderv1.RepairOrderLine{Description: "Nope"}, "description")
		fixture.RequireAPIError(t, err, http.StatusForbidden, "")

		_, err = reader.API.DeleteRepairOrderLine(ctx, garageID, orderID, lineID)
		fixture.RequireAPIError(t, err, http.StatusForbidden, "")

		_, err = reader.API.CancelRepairOrder(ctx, garageID, orderID)
		fixture.RequireAPIError(t, err, http.StatusForbidden, "")
	})

	t.Run("write access changes lines and cancels", func(t *testing.T) {
		advisor := staffIn(t, k, "ro-advisor", "Service Advisor")

		added, err := advisor.API.AddRepairOrderLine(ctx, garageID, orderID, laborLine("Extra", "1", 1))
		require.NoError(t, err)

		_, err = advisor.API.UpdateRepairOrderLineMasked(ctx, garageID, orderID, lineID, &repairorderv1.RepairOrderLine{Description: "Edited"}, "description")
		require.NoError(t, err)

		_, err = advisor.API.DeleteRepairOrderLine(ctx, garageID, orderID, orderLine(added, "Extra").GetId())
		require.NoError(t, err)

		cancelled, err := advisor.API.CancelRepairOrder(ctx, garageID, orderID)
		require.NoError(t, err)
		assert.Equal(t, repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_CANCELLED, cancelled.GetStatus())
	})

	t.Run("a member of another garage is refused", func(t *testing.T) {
		other := setup(t, "ro-perms-other")

		_, err := other.owner.API.AddRepairOrderLine(ctx, garageID, orderID, laborLine("Nope", "1", 1))
		fixture.RequireAPIError(t, err, http.StatusForbidden, "GARAGE_ACCESS_DENIED")

		_, err = other.owner.API.CancelRepairOrder(ctx, other.garageID, orderID)
		fixture.RequireAPIError(t, err, http.StatusNotFound, "")
	})
}
