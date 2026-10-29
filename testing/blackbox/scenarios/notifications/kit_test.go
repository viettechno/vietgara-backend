// Package notifications_test runs the e-mails of a garage (FR-NOT-05, FR-INV-04)
// against a stack with a test inbox: every customer and member of staff here has
// an address on the test domain, so their e-mail lands in Mailpit.
package notifications_test

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	billingv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/billing/v1"
	commonv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/common/v1"
	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"
	identityv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/identity/v1"
	notificationv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/notification/v1"
	quotev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/quote/v1"
	repairorderv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/repairorder/v1"
	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/client"
	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

var seq atomic.Int64

// How long an e-mail may take: the outbox relay delivers within a second on the
// stubbed stack, a little more on staging.
const (
	arrives = 30 * time.Second
	absent  = 4 * time.Second
)

type kit struct {
	owner         *fixture.Actor
	garageID      string
	customer      *customerv1.Customer
	customerEmail string
	vehicle       *customerv1.Vehicle
	inbox         *client.Mailpit
}

// newKit signs up an owner (English) with a garage and a customer who has an
// e-mail address (Vietnamese, the default language of a customer).
func newKit(t *testing.T, label string) *kit {
	t.Helper()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwnerWithLocale(t, label, identityv1.Locale_LOCALE_EN)

	garage, err := owner.API.CreateGarage(ctx, &tenantv1.Garage{Name: "Gara " + label})
	require.NoError(t, err)

	suffix := fmt.Sprintf("%05d", seq.Add(1)%100000)
	phone := "09150" + suffix
	email := fixture.UniqueEmail(t, "customer-"+label)

	customer, err := owner.API.CreateCustomer(ctx, garage.GetId(), &customerv1.Customer{
		FullName: "QA Customer " + label, Phone: &phone, Email: &email, PersonalDataConsent: true,
	})
	require.NoError(t, err)

	vehicle, err := owner.API.CreateVehicle(ctx, garage.GetId(), &customerv1.Vehicle{
		CustomerId: customer.GetId(), LicensePlate: "51K-" + suffix,
	})
	require.NoError(t, err)

	return &kit{
		owner: owner, garageID: garage.GetId(), customer: customer, customerEmail: email, vehicle: vehicle, inbox: fixture.Mailpit(t),
	}
}

// within gives each wait its own deadline.
func within(t *testing.T, d time.Duration) context.Context {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), d)
	t.Cleanup(cancel)

	return ctx
}

// mailTo waits for an e-mail to address whose subject contains fragment.
func (k *kit) mailTo(t *testing.T, address, fragment string) *client.Message {
	t.Helper()

	msg, err := k.inbox.WaitForMatching(within(t, arrives), address, func(m *client.Message) bool {
		return strings.Contains(m.Subject, fragment)
	})
	require.NoError(t, err, "an e-mail to %s with %q in its subject", address, fragment)

	return msg
}

// noMailTo proves that nothing with fragment in its subject reaches address.
func (k *kit) noMailTo(t *testing.T, address, fragment string) {
	t.Helper()

	_, err := k.inbox.WaitForMatching(within(t, absent), address, func(m *client.Message) bool {
		return strings.Contains(m.Subject, fragment)
	})
	require.ErrorIs(t, err, client.ErrNoMessage, "no e-mail to %s with %q", address, fragment)
}

// sentQuote makes a LINK-mode quote of one labour line and sends it.
func (k *kit) sentQuote(t *testing.T, price int64) (*quotev1.Quote, string) {
	t.Helper()

	ctx := t.Context()
	api := k.owner.API

	quote, err := api.CreateQuote(ctx, k.garageID, &quotev1.Quote{
		CustomerId: k.customer.GetId(), VehicleId: k.vehicle.GetId(),
		ApprovalMode: commonv1.QuoteApprovalMode_QUOTE_APPROVAL_MODE_LINK,
		Lines: []*quotev1.QuoteLine{{
			ItemType: quotev1.QuoteItemType_QUOTE_ITEM_TYPE_LABOR, Description: "Oil change", Quantity: "1", UnitPrice: price,
		}},
	})
	require.NoError(t, err)

	sent, err := api.SendQuote(ctx, k.garageID, quote.GetId())
	require.NoError(t, err)
	require.NotEmpty(t, sent.GetPublicUrl())

	return sent.GetQuote(), sent.GetPublicUrl()
}

// approve approves every line of a quote through its public link.
func (k *kit) approve(t *testing.T, quote *quotev1.Quote, link string) {
	t.Helper()

	var decisions []*quotev1.QuoteLineDecisionInput
	for _, l := range quote.GetLines() {
		decisions = append(decisions, &quotev1.QuoteLineDecisionInput{LineId: l.GetId(), Approved: true})
	}

	_, err := fixture.Anonymous(t).DecidePublicQuote(t.Context(), link, decisions...)
	require.NoError(t, err)
}

// repairOrderFor converts an approved quote and returns the order.
func (k *kit) repairOrderFor(t *testing.T, quote *quotev1.Quote) *repairorderv1.RepairOrder {
	t.Helper()

	converted, err := k.owner.API.ConvertQuote(t.Context(), k.garageID, quote.GetId())
	require.NoError(t, err)

	order, err := k.owner.API.GetRepairOrder(t.Context(), k.garageID, converted.GetRepairOrderId())
	require.NoError(t, err)

	return order
}

// complete finishes every line of an order.
func (k *kit) complete(t *testing.T, order *repairorderv1.RepairOrder) {
	t.Helper()

	for _, l := range order.GetLines() {
		_, err := k.owner.API.UpdateRepairOrderLine(t.Context(), k.garageID, order.GetId(), l.GetId(),
			&repairorderv1.RepairOrderLine{Status: repairorderv1.RepairOrderLineStatus_REPAIR_ORDER_LINE_STATUS_COMPLETED})
		require.NoError(t, err)
	}
}

// settle closes the order and creates its settlement.
func (k *kit) settle(t *testing.T, order *repairorderv1.RepairOrder) string {
	t.Helper()

	_, err := k.owner.API.CloseRepairOrder(t.Context(), k.garageID, order.GetId())
	require.NoError(t, err)

	settlement, err := k.owner.API.WithIdempotencyKey(uuid.NewString()).CreateSettlement(t.Context(), k.garageID, order.GetId())
	require.NoError(t, err)

	return settlement.GetId()
}

// pay records a cash payment.
func (k *kit) pay(t *testing.T, settlementID string, amount int64) {
	t.Helper()

	_, err := k.owner.API.WithIdempotencyKey(uuid.NewString()).RecordPayment(t.Context(), k.garageID, settlementID,
		&billingv1.RecordSettlementPaymentRequest{Amount: amount, Method: billingv1.PaymentMethod_PAYMENT_METHOD_CASH})
	require.NoError(t, err)
}

// logOf returns the log entries of one type.
func (k *kit) logOf(t *testing.T, typ notificationv1.NotificationType) []*notificationv1.NotificationLogEntry {
	t.Helper()

	entries, err := k.owner.API.ListNotificationLog(t.Context(), k.garageID, typ, notificationv1.NotificationStatus_NOTIFICATION_STATUS_UNSPECIFIED)
	require.NoError(t, err)

	return entries
}

// waitForLog waits until the log holds want entries of typ with status.
func (k *kit) waitForLog(t *testing.T, typ notificationv1.NotificationType, status notificationv1.NotificationStatus, want int) []*notificationv1.NotificationLogEntry {
	t.Helper()

	var matching []*notificationv1.NotificationLogEntry

	assert.Eventually(t, func() bool {
		matching = nil

		for _, e := range k.logOf(t, typ) {
			if e.GetStatus() == status {
				matching = append(matching, e)
			}
		}

		return len(matching) == want
	}, arrives, 500*time.Millisecond, "%d %s log entries of %s", want, status, typ)

	return matching
}
