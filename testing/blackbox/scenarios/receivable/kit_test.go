// Package receivable_test runs the receivables of a garage (FR-REC-01, 03, 04):
// what customers owe, fed by the events of Billing & Settlement, its reminders
// and its reconciliation. The receivable follows the settlement through the
// outbox, so a test waits for it with a short poll.
package receivable_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	billingv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/billing/v1"
	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"
	receivablev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/receivable/v1"
	repairorderv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/repairorder/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/client"
	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

// How long the outbox and the jobs may take.
const (
	arrives = 30 * time.Second
	absent  = 5 * time.Second
	poll    = 250 * time.Millisecond
)

type kit struct {
	*fixture.Business

	email string
	inbox *client.Mailpit
}

// newKit builds a garage whose customer has an address on the test domain.
func newKit(t *testing.T, label string) *kit {
	t.Helper()

	b := fixture.NewBusiness(t, label)
	email := fixture.UniqueEmail(t, "customer-"+label)

	_, err := b.Owner.API.UpdateCustomer(t.Context(), b.GarageID, b.CustomerID, &customerv1.Customer{Email: &email}, "email")
	require.NoError(t, err)

	return &kit{Business: b, email: email, inbox: fixture.Mailpit(t)}
}

// eventually polls cond until it holds or the wait runs out.
func eventually(t *testing.T, wait time.Duration, msg string, cond func(ctx context.Context) bool) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), wait)
	defer cancel()

	for {
		if cond(ctx) {
			return
		}

		select {
		case <-ctx.Done():
			require.FailNow(t, "gave up waiting", msg)
		case <-time.After(poll):
		}
	}
}

// settle makes a closed repair order of one labour line of price for the
// customer of vehicleID and settles it.
func (k *kit) settle(t *testing.T, price int64) *billingv1.Settlement {
	t.Helper()

	return k.settleFor(t, k.VehicleID, price)
}

func (k *kit) settleFor(t *testing.T, vehicleID string, price int64) *billingv1.Settlement {
	t.Helper()

	ctx := t.Context()
	api := k.Owner.API

	order, err := api.CreateRepairOrder(ctx, k.GarageID, &repairorderv1.RepairOrder{
		VehicleId: vehicleID,
		Lines: []*repairorderv1.RepairOrderLine{{
			ItemType: repairorderv1.RepairOrderItemType_REPAIR_ORDER_ITEM_TYPE_LABOR, Description: "Oil change", Quantity: "1", UnitPrice: price,
		}},
	})
	require.NoError(t, err)

	_, err = api.UpdateRepairOrderLine(ctx, k.GarageID, order.GetId(), order.GetLines()[0].GetId(),
		&repairorderv1.RepairOrderLine{Status: repairorderv1.RepairOrderLineStatus_REPAIR_ORDER_LINE_STATUS_COMPLETED})
	require.NoError(t, err)

	_, err = api.CloseRepairOrder(ctx, k.GarageID, order.GetId())
	require.NoError(t, err)

	settlement, err := api.WithIdempotencyKey(uuid.NewString()).CreateSettlement(ctx, k.GarageID, order.GetId())
	require.NoError(t, err)

	return settlement
}

// pay records a cash payment and returns the settlement and the payment id.
func (k *kit) pay(t *testing.T, settlementID string, amount int64, method billingv1.PaymentMethod) *billingv1.SettlementPayment {
	t.Helper()

	settlement, err := k.Owner.API.WithIdempotencyKey(uuid.NewString()).RecordPayment(t.Context(), k.GarageID, settlementID,
		&billingv1.RecordSettlementPaymentRequest{Amount: amount, Method: method})
	require.NoError(t, err)

	payments := settlement.GetPayments()

	return payments[len(payments)-1]
}

// receivableOf waits for the receivable of a settlement to exist and to
// satisfy ok (nil: any), and returns it with its ledger.
func (k *kit) receivableOf(t *testing.T, settlementID string, ok func(*receivablev1.Receivable) bool) *receivablev1.Receivable {
	t.Helper()

	var found *receivablev1.Receivable

	eventually(t, arrives, "the receivable of the settlement", func(ctx context.Context) bool {
		list, err := k.Owner.API.ListReceivables(ctx, k.GarageID, client.ReceivableFilter{IncludePaid: true, PageSize: "100"})
		if err != nil {
			return false
		}

		for _, r := range list.GetData() {
			if r.GetSettlementId() != settlementID {
				continue
			}

			full, err := k.Owner.API.GetReceivable(ctx, k.GarageID, r.GetId())
			if err != nil || (ok != nil && !ok(full)) {
				return false
			}

			found = full

			return true
		}

		return false
	})

	return found
}

// mailTo waits for an e-mail to the customer whose subject has fragment.
func (k *kit) mailTo(t *testing.T, fragment string) *client.Message {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), arrives)
	defer cancel()

	msg, err := k.inbox.WaitForMatching(ctx, k.email, func(m *client.Message) bool { return strings.Contains(m.Subject, fragment) })
	require.NoError(t, err, "an e-mail to %s with %q in its subject", k.email, fragment)

	return msg
}

// noMail proves that nothing with fragment in its subject reaches the customer.
func (k *kit) noMail(t *testing.T, fragment string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), absent)
	defer cancel()

	_, err := k.inbox.WaitForMatching(ctx, k.email, func(m *client.Message) bool { return strings.Contains(m.Subject, fragment) })
	require.ErrorIs(t, err, client.ErrNoMessage, "no e-mail to %s with %q", k.email, fragment)
}

const reminderSubject = "Nhắc thanh toán"

// today is the calendar day in the garages' default zone.
func today() time.Time {
	hcm, err := time.LoadLocation("Asia/Ho_Chi_Minh")
	if err != nil {
		hcm = time.UTC
	}

	y, m, d := time.Now().In(hcm).Date()

	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// sameDay reports whether the due date is the wanted day, allowing the day
// to turn between the settlement and the check.
func sameDay(due string, want time.Time) bool {
	got, err := time.Parse(time.DateOnly, due)
	if err != nil {
		return false
	}

	diff := got.Sub(want)

	return diff >= -24*time.Hour && diff <= 24*time.Hour
}
