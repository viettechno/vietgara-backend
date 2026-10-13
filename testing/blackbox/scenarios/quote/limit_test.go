package quote_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	quotev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/quote/v1"
	repairorderv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/repairorder/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

// Scenario: E2E-QUO-012 (partial): with two transactions a day, the
// quote and its conversion use them up, a third creation is refused, and
// closing the repair order is not a creation so it still succeeds. The
// banner copy is web-E2E.
func TestDailyLimitBlocksCreationNotClosing(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage, customer, vehicle := setup(t, "quo-limit", fixture.UniqueEmail(t, "quo-limit"))

	fixture.LimitedPlan(t, owner, 2)

	// Transaction 1: the quote. Transaction 2: its conversion.
	quote := draftQuote(t, owner.API, garage.GetId(), customer.GetId(), vehicle.GetId())

	_, err := owner.API.SendQuote(ctx, garage.GetId(), quote.GetId())
	require.NoError(t, err)

	_, err = owner.API.RecordQuoteDecision(ctx, garage.GetId(), quote.GetId(), decisions(quote, true)...)
	require.NoError(t, err)

	converted, err := owner.API.ConvertQuote(ctx, garage.GetId(), quote.GetId())
	require.NoError(t, err)

	sub, err := owner.API.MySubscription(ctx)
	require.NoError(t, err)
	assert.EqualValues(t, 2, sub.GetUsage().GetTransactionsToday())

	// Transaction 3 is refused.
	_, err = owner.API.CreateQuote(ctx, garage.GetId(), &quotev1.Quote{
		CustomerId: customer.GetId(), VehicleId: vehicle.GetId(),
		Lines: []*quotev1.QuoteLine{laborLine("Second job", "1", 100000)},
	})
	fixture.RequireAPIError(t, err, http.StatusPaymentRequired, "PLAN_LIMIT_REACHED")

	// Closing is separate from creation and is not counted.
	order, err := owner.API.GetRepairOrder(ctx, garage.GetId(), converted.GetRepairOrderId())
	require.NoError(t, err)

	for _, line := range order.GetLines() {
		_, err = owner.API.UpdateRepairOrderLine(ctx, garage.GetId(), order.GetId(), line.GetId(),
			&repairorderv1.RepairOrderLine{Status: repairorderv1.RepairOrderLineStatus_REPAIR_ORDER_LINE_STATUS_COMPLETED})
		require.NoError(t, err)
	}

	closed, err := owner.API.CloseRepairOrder(ctx, garage.GetId(), order.GetId())
	require.NoError(t, err)
	assert.Equal(t, repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_CLOSED, closed.GetStatus())
}
