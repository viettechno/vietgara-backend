package quote_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	licensev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/license/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

// Scenario: E2E-LIC-023 (partial): on a plan that counts only repair orders a
// quote does not use the daily limit; converting it into a repair order does.
func TestQuotesDoNotCountOnARepairOrderPlan(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage, customer, vehicle := setup(t, "quo-count", fixture.UniqueEmail(t, "quo-count"))

	fixture.LimitedPlanCounting(t, owner, 1, licensev1.TransactionKind_TRANSACTION_KIND_REPAIR_ORDER)

	var first string

	for range 3 {
		quote := draftQuote(t, owner.API, garage.GetId(), customer.GetId(), vehicle.GetId())
		if first == "" {
			first = quote.GetId()
		}
	}

	sub, err := owner.API.MySubscription(ctx)
	require.NoError(t, err)
	assert.Zero(t, sub.GetUsage().GetTransactionsToday(), "three quotes, none counted")

	quote, err := owner.API.GetQuote(ctx, garage.GetId(), first)
	require.NoError(t, err)

	_, err = owner.API.SendQuote(ctx, garage.GetId(), quote.GetId())
	require.NoError(t, err)

	_, err = owner.API.RecordQuoteDecision(ctx, garage.GetId(), quote.GetId(), decisions(quote, true)...)
	require.NoError(t, err)

	_, err = owner.API.ConvertQuote(ctx, garage.GetId(), quote.GetId())
	require.NoError(t, err)

	sub, err = owner.API.MySubscription(ctx)
	require.NoError(t, err)
	assert.EqualValues(t, 1, sub.GetUsage().GetTransactionsToday(), "the conversion is a repair order")
}
