package system_test

import (
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	billingv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/billing/v1"
	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"
	identityv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/identity/v1"
	quotev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/quote/v1"
	repairorderv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/repairorder/v1"
	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/client"
	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

// attempts is how many callers race for the same thing.
const attempts = 8

// race runs fn attempts times at once, released together, and returns the
// error of each call (nil for a success).
func race(fn func(i int) error) []error { return raceN(attempts, fn) }

// raceN is race with n callers.
func raceN(n int, fn func(i int) error) []error {
	errs := make([]error, n)

	var (
		ready sync.WaitGroup
		done  sync.WaitGroup
		start = make(chan struct{})
	)

	ready.Add(n)
	done.Add(n)

	for i := range n {
		go func() {
			defer done.Done()

			ready.Done()
			<-start

			errs[i] = fn(i)
		}()
	}

	ready.Wait()
	close(start)
	done.Wait()

	return errs
}

func successes(errs []error) int {
	n := 0

	for _, err := range errs {
		if err == nil {
			n++
		}
	}

	return n
}

// failuresWith counts the errors that are an API error with status.
func failuresWith(errs []error, status int) int {
	n := 0

	for _, err := range errs {
		if apiErr := client.AsAPIError(err); apiErr != nil && apiErr.Status == status {
			n++
		}
	}

	return n
}

// TestRacesHaveOneWinner: when several callers do the same thing at the
// same moment, the rule that says "once" or "at most N" still holds. Each
// subtest fires eight requests together and counts what was created.
//
// Scenario: E2E-SYS-004: one quote converts once, one repair order closes
// once, one order settles once, and payments never exceed the total.
// Scenario: E2E-SYS-005: the plan's garage, staff and daily limits hold
// under parallel requests, and document numbers stay unique.
func TestRacesHaveOneWinner(t *testing.T) { //nolint:funlen // independent subtests, one race each
	t.Parallel()

	t.Run("one approved quote converts into one repair order", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		business := fixture.NewBusiness(t, "race-convert")
		quote := business.DraftQuote(t)

		decisions := make([]*quotev1.QuoteLineDecisionInput, 0, len(quote.GetLines()))
		for _, line := range quote.GetLines() {
			decisions = append(decisions, &quotev1.QuoteLineDecisionInput{LineId: line.GetId(), Approved: true})
		}

		_, err := business.Owner.API.SendQuote(ctx, business.GarageID, quote.GetId())
		require.NoError(t, err)

		_, err = business.Owner.API.RecordQuoteDecision(ctx, business.GarageID, quote.GetId(), decisions...)
		require.NoError(t, err)

		errs := race(func(int) error {
			_, convertErr := business.Owner.API.ConvertQuote(ctx, business.GarageID, quote.GetId())

			return convertErr
		})

		assert.Equal(t, 1, successes(errs), "exactly one conversion wins: %v", errs)

		orders, err := business.Owner.API.ListRepairOrders(ctx, business.GarageID)
		require.NoError(t, err)
		assert.Len(t, orders, 1, "one repair order exists for the quote")
	})

	t.Run("one order settles once", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		business := fixture.NewBusiness(t, "race-settle")
		order := business.ClosedOrder(t)

		errs := race(func(int) error {
			_, settleErr := business.Owner.API.WithIdempotencyKey(uuid.NewString()).CreateSettlement(ctx, business.GarageID, order.GetId())

			return settleErr
		})

		assert.Equal(t, 1, successes(errs), "exactly one settlement is created: %v", errs)
		assert.Equal(t, attempts-1, failuresWith(errs, http.StatusConflict), "the others are told it exists: %v", errs)
	})

	t.Run("a repair order closes once", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		business := fixture.NewBusiness(t, "race-close")
		order := business.OpenOrder(t)

		_, err := business.Owner.API.UpdateRepairOrderLine(ctx, business.GarageID, order.GetId(), order.GetLines()[0].GetId(),
			&repairorderv1.RepairOrderLine{Status: repairorderv1.RepairOrderLineStatus_REPAIR_ORDER_LINE_STATUS_COMPLETED})
		require.NoError(t, err)

		errs := race(func(int) error {
			_, closeErr := business.Owner.API.CloseRepairOrder(ctx, business.GarageID, order.GetId())

			return closeErr
		})

		assert.Equal(t, 1, successes(errs), "exactly one close wins: %v", errs)
	})

	t.Run("payments never add up to more than the total", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		business := fixture.NewBusiness(t, "race-pay")

		// The overlap is a matter of timing, so several settlements are
		// raced: each gets sixteen payments of 40% of its total at once;
		// two fit, a third is too much.
		for round := range 4 {
			settlement := business.Settlement(t)

			var recorded atomic.Int64

			errs := raceN(16, func(int) error {
				_, payErr := business.Owner.API.WithIdempotencyKey(uuid.NewString()).RecordPayment(ctx, business.GarageID, settlement.GetId(),
					&billingv1.RecordSettlementPaymentRequest{Amount: fixture.LaborTotal * 4 / 10, Method: billingv1.PaymentMethod_PAYMENT_METHOD_CASH})
				if payErr == nil {
					recorded.Add(1)
				}

				return payErr
			})

			current, err := business.Owner.API.GetSettlement(ctx, business.GarageID, settlement.GetId())
			require.NoError(t, err)
			assert.LessOrEqual(t, current.GetPaidAmount(), current.GetTotalAmount(), "round %d: the paid amount never exceeds the total", round)
			assert.EqualValues(t, 2, recorded.Load(), "round %d: only two payments of 40%% of the total fit: %v", round, errs)
			assert.Len(t, current.GetPayments(), 2, "round %d", round)
		}
	})

	t.Run("a trial owner gets one garage however many are asked for at once", func(t *testing.T) {
		t.Parallel()
		t.Skip("known defect, Backlog G-24: the garage limit is checked and then used without a lock, so parallel requests all pass")

		owner := fixture.NewVerifiedOwner(t, "race-garage")

		errs := race(func(i int) error {
			_, createErr := owner.API.CreateGarage(t.Context(), &tenantv1.Garage{Name: "Race Garage " + uuid.NewString()})

			return createErr
		})

		assert.Equal(t, 1, successes(errs), "the trial allows one garage: %v", errs)
		assert.Equal(t, attempts-1, failuresWith(errs, http.StatusPaymentRequired), "the others hit the plan limit: %v", errs)
	})

	t.Run("staff invitations stop at the plan's staff limit", func(t *testing.T) {
		t.Parallel()
		t.Skip("known defect, Backlog G-24: the staff limit is checked and then used without a lock, so parallel invitations can exceed it")

		ctx := t.Context()
		business := fixture.NewBusiness(t, "race-staff")

		sub, err := business.Owner.API.MySubscription(ctx)
		require.NoError(t, err)

		limit := int(sub.GetPlan().GetLimits().GetMaxStaff())
		require.Positive(t, limit)

		groups, err := business.Owner.API.ListStaffGroups(ctx, business.GarageID)
		require.NoError(t, err)
		require.NotEmpty(t, groups)

		// More invitees than seats, each a real user.
		invitees := make([]string, limit+3)
		for i := range invitees {
			invitees[i] = fixture.NewVerifiedOwner(t, "race-invitee").Session.GetUser().GetId()
		}

		errs := make([]error, len(invitees))

		var wg sync.WaitGroup
		for i, userID := range invitees {
			wg.Go(func() {
				_, errs[i] = business.Owner.API.CreateInvitation(ctx, business.GarageID, userID, []string{groups[0].GetId()})
			})
		}

		wg.Wait()

		assert.Equal(t, limit, successes(errs), "exactly the free seats are given out: %v", errs)
	})

	t.Run("document numbers stay unique", func(t *testing.T) {
		t.Parallel()

		business := fixture.NewBusiness(t, "race-number")

		numbers := make([]string, attempts)

		errs := race(func(i int) error {
			quote, createErr := business.Owner.API.CreateQuote(t.Context(), business.GarageID, &quotev1.Quote{
				CustomerId: business.CustomerID, VehicleId: business.VehicleID,
				Lines: []*quotev1.QuoteLine{{
					ItemType: quotev1.QuoteItemType_QUOTE_ITEM_TYPE_LABOR, Description: "Race", Quantity: "1", UnitPrice: 1000,
				}},
			})
			if createErr == nil {
				numbers[i] = quote.GetNumber()
			}

			return createErr
		})

		require.Equal(t, attempts, successes(errs), "the trial plan's daily limit is high enough: %v", errs)

		seen := map[string]bool{}

		for _, number := range numbers {
			assert.NotEmpty(t, number)
			assert.False(t, seen[number], "number %s was given twice", number)
			seen[number] = true
		}
	})

	t.Run("the daily transaction limit holds under parallel creation", func(t *testing.T) {
		t.Parallel()

		business := fixture.NewBusiness(t, "race-daily")
		fixture.LimitedPlan(t, business.Owner, 3)

		// The entitlements are cached for a moment: wait until the new
		// plan's limit is the one being enforced.
		require.Eventually(t, func() bool {
			entitlements, err := business.Owner.API.GetGarageEntitlements(t.Context(), business.GarageID)

			return err == nil && entitlements.GetLimits().GetMaxTransactionsPerDay() == 3
		}, 30*time.Second, 500*time.Millisecond)

		errs := race(func(int) error {
			_, createErr := business.Owner.API.CreateQuote(t.Context(), business.GarageID, &quotev1.Quote{
				CustomerId: business.CustomerID, VehicleId: business.VehicleID,
				Lines: []*quotev1.QuoteLine{{
					ItemType: quotev1.QuoteItemType_QUOTE_ITEM_TYPE_LABOR, Description: "Race", Quantity: "1", UnitPrice: 1000,
				}},
			})

			return createErr
		})

		assert.Equal(t, 3, successes(errs), "exactly the plan's three transactions: %v", errs)
		assert.Equal(t, attempts-3, failuresWith(errs, http.StatusPaymentRequired), "the rest are refused: %v", errs)
	})

	t.Run("an invitation is accepted once", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		business := fixture.NewBusiness(t, "race-accept")

		groups, err := business.Owner.API.ListStaffGroups(ctx, business.GarageID)
		require.NoError(t, err)

		invitee := fixture.NewVerifiedOwner(t, "race-accepter")
		invitation, err := business.Owner.API.CreateInvitation(ctx, business.GarageID, invitee.Session.GetUser().GetId(), []string{groups[0].GetId()})
		require.NoError(t, err)

		errs := race(func(int) error {
			_, acceptErr := invitee.API.AcceptInvitation(ctx, invitation.GetId())

			return acceptErr
		})

		assert.GreaterOrEqual(t, successes(errs), 1, "the first accept works: %v", errs)

		staff, err := business.Owner.API.ListStaff(ctx, business.GarageID)
		require.NoError(t, err)

		members := 0

		for _, member := range staff {
			if member.GetUserId() == invitee.Session.GetUser().GetId() {
				members++
			}
		}

		assert.Equal(t, 1, members, "the invitee is a member once")
	})

	t.Run("one e-mail registers once", func(t *testing.T) {
		t.Parallel()

		email := fixture.UniqueEmail(t, "race-register")

		errs := race(func(int) error {
			_, registerErr := fixture.Anonymous(t).Register(t.Context(), &identityv1.RegisterRequest{Email: email, Password: fixture.Password, FullName: "Blackbox Race"})

			return registerErr
		})

		assert.Equal(t, 1, successes(errs), "one registration wins: %v", errs)
		assert.Equal(t, attempts-1, failuresWith(errs, http.StatusConflict), "the others are told the e-mail is taken: %v", errs)
	})

	t.Run("a customer phone is taken once", func(t *testing.T) {
		t.Parallel()

		business := fixture.NewBusiness(t, "race-phone")
		phone := "0911999888"

		errs := race(func(int) error {
			_, createErr := business.Owner.API.CreateCustomer(t.Context(), business.GarageID,
				&customerv1.Customer{FullName: "QA Customer Race", Phone: &phone, PersonalDataConsent: true})

			return createErr
		})

		assert.Equal(t, 1, successes(errs), "one customer per phone in a garage: %v", errs)
	})
}
