package jobs_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	cataloguev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/catalogue/v1"
	commonv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/common/v1"
	quotev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/quote/v1"
	repairorderv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/repairorder/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

func quoteLabor(description string, price int64) *quotev1.QuoteLine {
	return &quotev1.QuoteLine{
		ItemType: quotev1.QuoteItemType_QUOTE_ITEM_TYPE_LABOR, Description: description, Quantity: "1", UnitPrice: price,
	}
}

func quotePart(part *cataloguev1.Part, quantity string, price int64) *quotev1.QuoteLine {
	return &quotev1.QuoteLine{
		ItemType: quotev1.QuoteItemType_QUOTE_ITEM_TYPE_PART, ItemRefId: new(part.GetId()), Description: part.GetName(),
		Quantity: quantity, UnitPrice: price,
	}
}

// draft creates a MANUAL-mode draft quote with the given lines.
func draft(t *testing.T, k *garageKit, lines ...*quotev1.QuoteLine) *quotev1.Quote {
	t.Helper()

	quote, err := k.owner.API.CreateQuote(t.Context(), k.garageID, &quotev1.Quote{
		CustomerId: k.customer.GetId(), VehicleId: k.vehicle.GetId(),
		ApprovalMode: commonv1.QuoteApprovalMode_QUOTE_APPROVAL_MODE_MANUAL, Lines: lines,
	})
	require.NoError(t, err)

	return quote
}

func lineByDescription(quote *quotev1.Quote, description string) *quotev1.QuoteLine {
	for _, l := range quote.GetLines() {
		if l.GetDescription() == description {
			return l
		}
	}

	return nil
}

// billableDecisions approves or rejects every line that is not a hidden
// consumable, as the customer sees them.
func billableDecisions(quote *quotev1.Quote, approve func(*quotev1.QuoteLine) bool) []*quotev1.QuoteLineDecisionInput {
	var out []*quotev1.QuoteLineDecisionInput

	for _, l := range quote.GetLines() {
		if !l.GetNonBillable() {
			out = append(out, &quotev1.QuoteLineDecisionInput{LineId: l.GetId(), Approved: approve(l)})
		}
	}

	return out
}

// Scenario: E2E-QUO-015, Release 7.3, a job on a quote (FR-CAT-06): one labor line at the
// job's price and a line per active suggested part at the part's price; a
// packaging unit is typed and priced per base unit; the lines are then
// ordinary and editable; an unknown job, a quote that is not a draft and a
// quote that would pass 100 lines are refused.
func TestAddJobToQuote(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	k := setup(t, "quote-job")
	api, garageID := k.owner.API, k.garageID
	c := k.catalogue(t)
	job := newJob(t, k, "Oil change", 200_000, suggest(c.oil, "4"), suggest(c.filter, "1", hidden))

	quote := draft(t, k, quoteLabor("Inspection", 100_000))

	t.Run("the labor line and the part lines appear", func(t *testing.T) {
		with, err := api.AddQuoteJob(ctx, garageID, quote.GetId(), job.GetId())
		require.NoError(t, err)
		require.Len(t, with.GetLines(), 4)

		labor := lineByDescription(with, "Oil change")
		require.NotNil(t, labor)
		assert.Equal(t, quotev1.QuoteItemType_QUOTE_ITEM_TYPE_LABOR, labor.GetItemType())
		assert.EqualValues(t, 200_000, labor.GetLineTotal())
		assert.Equal(t, job.GetId(), labor.GetJobId())

		oil := lineByDescription(with, "Engine oil 5W-30")
		require.NotNil(t, oil)
		assert.Equal(t, c.oil.GetId(), oil.GetItemRefId())
		assert.Equal(t, "4", oil.GetQuantity())
		assert.EqualValues(t, 90_000, oil.GetUnitPrice())
		assert.EqualValues(t, 360_000, oil.GetLineTotal())
		assert.Equal(t, job.GetId(), oil.GetJobId())
		assert.False(t, oil.GetNonBillable())

		filter := lineByDescription(with, "Oil filter")
		require.NotNil(t, filter)
		assert.True(t, filter.GetNonBillable())
		assert.EqualValues(t, 0, filter.GetUnitPrice())
		assert.EqualValues(t, 0, filter.GetLineTotal())

		assert.EqualValues(t, 100_000+200_000+360_000, with.GetTotalAmount(), "the consumable adds nothing")
		assert.Equal(t, "Inspection", with.GetLines()[0].GetDescription(), "the new lines come after the existing ones")
		assert.Empty(t, with.GetLines()[0].GetJobId())
	})

	t.Run("the lines are editable like any other", func(t *testing.T) {
		current, err := api.GetQuote(ctx, garageID, quote.GetId())
		require.NoError(t, err)

		lineByDescription(current, "Engine oil 5W-30").Quantity = "5"

		updated, err := api.UpdateQuote(ctx, garageID, quote.GetId(), &quotev1.Quote{Lines: current.GetLines()}, "lines")
		require.NoError(t, err)

		oil := lineByDescription(updated, "Engine oil 5W-30")
		assert.EqualValues(t, 450_000, oil.GetLineTotal())
		assert.Equal(t, job.GetId(), oil.GetJobId(), "the line still remembers its job")
		assert.True(t, lineByDescription(updated, "Oil filter").GetNonBillable(), "and the hidden mark")
		assert.EqualValues(t, 100_000+200_000+450_000, updated.GetTotalAmount())
	})

	t.Run("a job may be used twice", func(t *testing.T) {
		twice, err := api.AddQuoteJob(ctx, garageID, quote.GetId(), job.GetId())
		require.NoError(t, err)
		assert.Len(t, twice.GetLines(), 7)
		assert.EqualValues(t, 100_000+200_000+450_000+200_000+360_000, twice.GetTotalAmount())
	})

	t.Run("a packaging unit is typed in the unit and priced per base unit", func(t *testing.T) {
		jugJob := newJob(t, k, "Oil by the jug", 0, suggest(c.oil, "2", inUnit(c.jug)))
		fresh := draft(t, k, quoteLabor("Check", 1))

		with, err := api.AddQuoteJob(ctx, garageID, fresh.GetId(), jugJob.GetId())
		require.NoError(t, err)

		oil := lineByDescription(with, "Engine oil 5W-30")
		require.NotNil(t, oil)
		assert.Equal(t, c.jug.GetId(), oil.GetUnitId())
		assert.Equal(t, "2", oil.GetEnteredQuantity())
		assert.Equal(t, "10", oil.GetQuantity(), "two jugs of 5 L")
		assert.EqualValues(t, 90_000, oil.GetUnitPrice(), "priced per litre")
		assert.EqualValues(t, 900_000, oil.GetLineTotal(), "2 jugs at 450,000")
	})

	t.Run("an unknown job is not found", func(t *testing.T) {
		_, err := api.AddQuoteJob(ctx, garageID, quote.GetId(), uuid.NewString())
		fixture.RequireAPIError(t, err, http.StatusNotFound, "")

		_, err = api.AddQuoteJob(ctx, garageID, quote.GetId(), "not-an-id")
		fixture.RequireAPIError(t, err, http.StatusNotFound, "")
	})

	t.Run("a quote that would pass 100 lines is refused", func(t *testing.T) {
		lines := make([]*quotev1.QuoteLine, 0, 99)
		for range 99 {
			lines = append(lines, quoteLabor("Line", 1))
		}

		full := draft(t, k, lines...)

		_, err := api.AddQuoteJob(ctx, garageID, full.GetId(), job.GetId())
		fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "")
	})

	t.Run("a quote that is no longer a draft is refused", func(t *testing.T) {
		sent, err := api.SendQuote(ctx, garageID, quote.GetId())
		require.NoError(t, err)
		require.Equal(t, quotev1.QuoteStatus_QUOTE_STATUS_SENT, sent.GetQuote().GetStatus())

		_, err = api.AddQuoteJob(ctx, garageID, quote.GetId(), job.GetId())
		fixture.RequireAPIError(t, err, http.StatusConflict, "")
	})
}

// Scenario: E2E-QUO-016, Release 7.3, hidden consumables (FR-CAT-08): priced zero and left
// out of the total, the public approval page and the decisions; only a
// catalogue part line can be hidden and a quote needs a billable line.
//
//nolint:funlen // one scenario, read top to bottom
func TestHiddenConsumableOnTheQuote(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	k := setup(t, "quote-hidden")
	api, garageID := k.owner.API, k.garageID
	c := k.catalogue(t)
	job := newJob(t, k, "Oil change", 200_000, suggest(c.oil, "4"), suggest(c.filter, "1", hidden))

	t.Run("a hidden line is stored at zero, whatever price was sent", func(t *testing.T) {
		line := quotePart(c.filter, "1", 5_000)
		line.NonBillable = true

		quote := draft(t, k, quoteLabor("Work", 100_000), line)
		assert.EqualValues(t, 100_000, quote.GetTotalAmount())

		got := lineByDescription(quote, c.filter.GetName())
		assert.True(t, got.GetNonBillable())
		assert.EqualValues(t, 0, got.GetUnitPrice())
		assert.EqualValues(t, 0, got.GetLineTotal())
	})

	t.Run("only a catalogue part line can be hidden, and a billable line is needed", func(t *testing.T) {
		hiddenLabor := quoteLabor("Hidden labor", 1)
		hiddenLabor.NonBillable = true

		noPart := quotePart(c.filter, "1", 0)
		noPart.ItemRefId = nil
		noPart.NonBillable = true

		onlyHidden := quotePart(c.filter, "1", 0)
		onlyHidden.NonBillable = true

		badJob := quoteLabor("Bad job", 1)
		badJob.JobId = new("not-an-id")

		for name, lines := range map[string][]*quotev1.QuoteLine{
			"a hidden labor line":      {quoteLabor("Work", 1), hiddenLabor},
			"a hidden free-text part":  {quoteLabor("Work", 1), noPart},
			"nothing but a hidden one": {onlyHidden},
			"a malformed job id":       {badJob},
		} {
			_, err := api.CreateQuote(t.Context(), garageID, &quotev1.Quote{CustomerId: k.customer.GetId(), VehicleId: k.vehicle.GetId(), Lines: lines})
			require.Error(t, err, name)
			fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "")
		}
	})

	t.Run("the customer sees and decides on the billable lines only", func(t *testing.T) {
		link, err := api.CreateQuote(ctx, garageID, &quotev1.Quote{
			CustomerId: k.customer.GetId(), VehicleId: k.vehicle.GetId(),
			ApprovalMode: commonv1.QuoteApprovalMode_QUOTE_APPROVAL_MODE_LINK, Lines: []*quotev1.QuoteLine{quoteLabor("Inspection", 100_000)},
		})
		require.NoError(t, err)

		with, err := api.AddQuoteJob(ctx, garageID, link.GetId(), job.GetId())
		require.NoError(t, err)

		sent, err := api.SendQuote(ctx, garageID, link.GetId())
		require.NoError(t, err)
		require.NotEmpty(t, sent.GetPublicUrl())

		public, err := api.GetPublicQuote(ctx, sent.GetPublicUrl())
		require.NoError(t, err)
		assert.Len(t, public.GetLines(), 3, "the inspection, the labor and the oil")
		assert.Equal(t, with.GetTotalAmount(), public.GetTotalAmount())

		for _, l := range public.GetLines() {
			assert.NotEqual(t, "Oil filter", l.GetDescription(), "the hidden consumable is not shown")
			assert.False(t, l.GetNonBillable())
		}

		// The customer decides on what they see.
		var decisions []*quotev1.QuoteLineDecisionInput
		for _, l := range public.GetLines() {
			decisions = append(decisions, &quotev1.QuoteLineDecisionInput{LineId: l.GetId(), Approved: true})
		}

		// A decision that names a hidden line is not one the customer can make.
		hiddenID := lineByDescription(with, "Oil filter").GetId()
		_, err = api.DecidePublicQuote(ctx, sent.GetPublicUrl(), append(decisions, &quotev1.QuoteLineDecisionInput{LineId: hiddenID, Approved: true})...)
		fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "")

		decided, err := api.DecidePublicQuote(ctx, sent.GetPublicUrl(), decisions...)
		require.NoError(t, err)
		assert.Equal(t, quotev1.QuoteStatus_QUOTE_STATUS_APPROVED, decided.GetStatus())

		staff, err := api.GetQuote(ctx, garageID, link.GetId())
		require.NoError(t, err)
		assert.Equal(t, quotev1.QuoteLineDecision_QUOTE_LINE_DECISION_APPROVED, lineByDescription(staff, "Oil filter").GetDecision(),
			"the consumable follows its job")
	})

	t.Run("staff decide on the billable lines only, too", func(t *testing.T) {
		manual := draft(t, k, quoteLabor("Inspection", 100_000))

		with, err := api.AddQuoteJob(ctx, garageID, manual.GetId(), job.GetId())
		require.NoError(t, err)

		_, err = api.SendQuote(ctx, garageID, manual.GetId())
		require.NoError(t, err)

		all := billableDecisions(with, func(*quotev1.QuoteLine) bool { return true })
		all = append(all, &quotev1.QuoteLineDecisionInput{LineId: lineByDescription(with, "Oil filter").GetId(), Approved: true})

		_, err = api.RecordQuoteDecision(ctx, garageID, manual.GetId(), all...)
		fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "")

		decided, err := api.RecordQuoteDecision(ctx, garageID, manual.GetId(), billableDecisions(with, func(*quotev1.QuoteLine) bool { return true })...)
		require.NoError(t, err)
		assert.Equal(t, quotev1.QuoteStatus_QUOTE_STATUS_APPROVED, decided.GetStatus())
	})

	t.Run("the quote PDF downloads", func(t *testing.T) {
		quote := draft(t, k, quoteLabor("Work", 100_000))

		_, err := api.AddQuoteJob(ctx, garageID, quote.GetId(), job.GetId())
		require.NoError(t, err)

		contentType, body, err := api.DownloadQuotePDF(ctx, garageID, quote.GetId())
		require.NoError(t, err)
		assert.Contains(t, contentType, "pdf")
		assert.NotEmpty(t, body)
	})
}

// Scenario: E2E-QUO-017, Release 7.3, a hidden consumable goes to the repair order with
// its job: when a billable line of the job is approved; not when the whole job
// is rejected; a consumable that belongs to no job follows the quote. The
// order does not bill it.
func TestHiddenConsumableFollowsItsJob(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	k := setup(t, "quote-follow")
	api, garageID := k.owner.API, k.garageID
	c := k.catalogue(t)
	job := newJob(t, k, "Oil change", 200_000, suggest(c.oil, "4"), suggest(c.filter, "1", hidden))

	convert := func(t *testing.T, quote *quotev1.Quote, approve func(*quotev1.QuoteLine) bool) *repairorderv1.RepairOrder {
		t.Helper()

		_, err := api.SendQuote(ctx, garageID, quote.GetId())
		require.NoError(t, err)

		_, err = api.RecordQuoteDecision(ctx, garageID, quote.GetId(), billableDecisions(quote, approve)...)
		require.NoError(t, err)

		converted, err := api.ConvertQuote(ctx, garageID, quote.GetId())
		require.NoError(t, err)

		order, err := api.GetRepairOrder(ctx, garageID, converted.GetRepairOrderId())
		require.NoError(t, err)

		return order
	}

	descriptions := func(order *repairorderv1.RepairOrder) []string {
		var out []string
		for _, l := range order.GetLines() {
			out = append(out, l.GetDescription())
		}

		return out
	}

	t.Run("approving a billable line of the job brings the consumable", func(t *testing.T) {
		quote := draft(t, k, quoteLabor("Extra", 50_000))

		with, err := api.AddQuoteJob(ctx, garageID, quote.GetId(), job.GetId())
		require.NoError(t, err)

		order := convert(t, with, func(l *quotev1.QuoteLine) bool { return l.GetDescription() == "Oil change" })
		assert.ElementsMatch(t, []string{"Oil change", "Oil filter"}, descriptions(order))
		assert.EqualValues(t, 200_000, order.GetTotalAmount(), "the order does not bill the consumable")

		var consumable *repairorderv1.RepairOrderLine

		for _, l := range order.GetLines() {
			if l.GetDescription() == "Oil filter" {
				consumable = l
			}
		}

		require.NotNil(t, consumable)
		assert.True(t, consumable.GetNonBillable())
		assert.Equal(t, job.GetId(), consumable.GetJobId())
		assert.EqualValues(t, 0, consumable.GetLineTotal())
	})

	t.Run("rejecting the whole job drops the consumable", func(t *testing.T) {
		quote := draft(t, k, quoteLabor("Extra", 50_000))

		with, err := api.AddQuoteJob(ctx, garageID, quote.GetId(), job.GetId())
		require.NoError(t, err)

		order := convert(t, with, func(l *quotev1.QuoteLine) bool { return l.GetDescription() == "Extra" })
		assert.Equal(t, []string{"Extra"}, descriptions(order))
	})

	t.Run("a consumable that belongs to no job follows the quote", func(t *testing.T) {
		loose := quotePart(c.filter, "1", 0)
		loose.NonBillable = true

		quote := draft(t, k, quoteLabor("Work", 70_000), loose)

		order := convert(t, quote, func(*quotev1.QuoteLine) bool { return true })
		assert.ElementsMatch(t, []string{"Work", c.filter.GetName()}, descriptions(order))
		assert.EqualValues(t, 70_000, order.GetTotalAmount())
	})
}
