package billing_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

// Scenario: E2E-BIL-010 (partial): the daily transaction limit counts the
// creation of a settlement, not only of quotes and repair orders. The
// next-day reset needs a faked clock and is not asserted here.
func TestDailyLimitCountsSettlementCreation(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwner(t, "bil-limit")

	// One transaction a day: the repair order below uses it up.
	fixture.LimitedPlan(t, owner, 1)

	garageID, orderID := closedRepairOrder(t, owner.API)

	_, err := owner.API.WithIdempotencyKey(uuid.NewString()).CreateSettlement(ctx, garageID, orderID)
	fixture.RequireAPIError(t, err, http.StatusPaymentRequired, "PLAN_LIMIT_REACHED")

	settlements, err := owner.API.ListSettlements(ctx, garageID)
	require.NoError(t, err)
	require.Empty(t, settlements, "nothing was created")
}
