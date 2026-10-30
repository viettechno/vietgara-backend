package notifications_test

import (
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	cataloguev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/catalogue/v1"
	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"
	inventoryv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/inventory/v1"
	notificationv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/notification/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/client"
	"github.com/viettechno/vietgara-backend/testing/blackbox/env"
	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

const (
	sentStatus    = notificationv1.NotificationStatus_NOTIFICATION_STATUS_SENT
	skippedStatus = notificationv1.NotificationStatus_NOTIFICATION_STATUS_SKIPPED
)

// Scenario: E2E-NOT-001, Release 10.1, a vehicle followed from quote to payment: the
// expected e-mails, each logged once (FR-NOT-05).
func TestVehicleFromQuoteToPayment(t *testing.T) {
	t.Parallel()

	k := newKit(t, "not-001")
	owner := k.owner.Email

	quote, link := k.sentQuote(t, 500_000)

	// Quote ready: the approval link, in the customer's language (Vietnamese by default).
	ready := k.mailTo(t, k.customerEmail, "cần bạn duyệt")
	assert.Contains(t, ready.Text, link)
	assert.Equal(t, "Gara not-001", ready.From.Name, "the garage's name is the sender")

	headers, err := k.inbox.Header(t.Context(), ready, "List-Unsubscribe")
	require.NoError(t, err)
	assert.Empty(t, headers, "an essential e-mail has no opt-out")

	// Quote decided: the owner made the quote, so the owner is told once.
	k.approve(t, quote, link)
	k.mailTo(t, owner, "was approved")

	order := k.repairOrderFor(t, quote)
	k.complete(t, order)

	// Vehicle ready, with the opt-out link and headers.
	vehicleReady := k.mailTo(t, k.customerEmail, "đã sẵn sàng")
	assert.NotEmpty(t, client.OptOutLinkToken(vehicleReady.Text))

	headers, err = k.inbox.Header(t.Context(), vehicleReady, "List-Unsubscribe")
	require.NoError(t, err)
	assert.Len(t, headers, 1)

	settlementID := k.settle(t, order)

	k.pay(t, settlementID, 200_000)
	k.mailTo(t, k.customerEmail, "đã nhận")

	k.pay(t, settlementID, 300_000)

	receipt := k.mailTo(t, k.customerEmail, "Biên nhận")
	require.Len(t, receipt.Attachments, 1)
	assert.Equal(t, "application/pdf", receipt.Attachments[0].ContentType)
	assert.Contains(t, receipt.Attachments[0].FileName, "receipt-")
	k.mailTo(t, owner, "is paid in full")

	// The last payment sends the receipt only, not "payment received" as well.
	count, err := k.inbox.Count(t.Context(), k.customerEmail, "đã nhận")
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	for _, typ := range []notificationv1.NotificationType{
		notificationv1.NotificationType_NOTIFICATION_TYPE_QUOTE_READY,
		notificationv1.NotificationType_NOTIFICATION_TYPE_QUOTE_DECIDED,
		notificationv1.NotificationType_NOTIFICATION_TYPE_VEHICLE_READY,
		notificationv1.NotificationType_NOTIFICATION_TYPE_PAYMENT_RECEIVED,
		notificationv1.NotificationType_NOTIFICATION_TYPE_SETTLEMENT_RECEIPT,
		notificationv1.NotificationType_NOTIFICATION_TYPE_SETTLEMENT_PAID,
	} {
		entries := k.waitForLog(t, typ, sentStatus, 1)
		assert.Equal(t, typ, entries[0].GetType())
	}
}

// Scenario: E2E-NOT-002, Release 10.1, a type switched off and a sender name set in the
// garage settings (FR-NOT-05).
func TestSwitchOffAndSenderName(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	k := newKit(t, "not-002")
	api := k.owner.API

	defaults, err := api.GetNotificationSettings(ctx, k.garageID)
	require.NoError(t, err)
	require.Len(t, defaults.GetTypes(), 7)

	for _, setting := range defaults.GetTypes() {
		assert.True(t, setting.GetEnabled(), setting.GetType().String())
	}

	_, err = api.UpdateNotificationSettings(ctx, k.garageID, strings200("a"))
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "VALIDATION_ERROR")

	updated, err := api.UpdateNotificationSettings(ctx, k.garageID, "Gara Anh Minh", &notificationv1.NotificationTypeSetting{
		Type: notificationv1.NotificationType_NOTIFICATION_TYPE_VEHICLE_READY, Enabled: false,
	})
	require.NoError(t, err)
	assert.Equal(t, "Gara Anh Minh", updated.GetSenderName())

	quote, link := k.sentQuote(t, 100_000)

	ready := k.mailTo(t, k.customerEmail, "cần bạn duyệt")
	assert.Equal(t, "Gara Anh Minh", ready.From.Name)

	k.approve(t, quote, link)
	k.complete(t, k.repairOrderFor(t, quote))

	skipped := k.waitForLog(t, notificationv1.NotificationType_NOTIFICATION_TYPE_VEHICLE_READY, skippedStatus, 1)
	assert.Equal(t, notificationv1.NotificationSkipReason_NOTIFICATION_SKIP_REASON_DISABLED, skipped[0].GetReason())
	k.noMailTo(t, k.customerEmail, "đã sẵn sàng")
}

func strings200(s string) string {
	out := make([]byte, 0, 101)
	for range 101 {
		out = append(out, s[0])
	}

	return string(out)
}

// Scenario: E2E-NOT-003, Release 10.1, a customer opts out through the page: the
// non-essential e-mails stop, the essential ones go on (FR-NOT-05, PD-15).
func TestCustomerOptsOut(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	k := newKit(t, "not-003")
	api, public := k.owner.API, fixture.Anonymous(t)

	quote, link := k.sentQuote(t, 100_000)
	k.approve(t, quote, link)
	order := k.repairOrderFor(t, quote)
	k.complete(t, order)

	token := client.OptOutLinkToken(k.mailTo(t, k.customerEmail, "đã sẵn sàng").Text)
	require.NotEmpty(t, token)

	// The page changes nothing; a changed link is not found.
	status, body, err := public.OptOutPage(ctx, token)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, status)
	assert.Contains(t, body, "<form")

	customer, err := api.GetCustomer(ctx, k.garageID, k.customer.GetId())
	require.NoError(t, err)
	assert.False(t, customer.GetEmailOptOut())

	status, _, err = public.OptOutPage(ctx, token+"0")
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, status)

	// The button opts out; pressing it again changes nothing more.
	for range 2 {
		status, _, err = public.OptOut(ctx, token)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, status)
	}

	customer, err = api.GetCustomer(ctx, k.garageID, k.customer.GetId())
	require.NoError(t, err)
	assert.True(t, customer.GetEmailOptOut())
	assert.NotNil(t, customer.GetEmailOptOutAt())

	// A second vehicle through the garage: quote ready still arrives, vehicle ready does not.
	second, secondLink := k.sentQuote(t, 100_000)
	k.mailTo(t, k.customerEmail, "cần bạn duyệt")
	k.approve(t, second, secondLink)
	k.complete(t, k.repairOrderFor(t, second))

	skipped := k.waitForLog(t, notificationv1.NotificationType_NOTIFICATION_TYPE_VEHICLE_READY, skippedStatus, 1)
	assert.Equal(t, notificationv1.NotificationSkipReason_NOTIFICATION_SKIP_REASON_OPTED_OUT, skipped[0].GetReason())

	count, err := k.inbox.Count(ctx, k.customerEmail, "đã sẵn sàng")
	require.NoError(t, err)
	assert.Equal(t, 1, count, "only the e-mail from before the opt-out")

	// A member of staff records that the customer agreed again.
	_, err = api.UpdateCustomer(ctx, k.garageID, k.customer.GetId(), &customerv1.Customer{EmailOptOut: false}, "email_opt_out")
	require.NoError(t, err)

	customer, err = api.GetCustomer(ctx, k.garageID, k.customer.GetId())
	require.NoError(t, err)
	assert.False(t, customer.GetEmailOptOut())
}

// Scenario: E2E-NOT-004, Release 10.1, a bounce suppresses the address; a wrong webhook
// secret is refused (FR-NOT-05).
func TestBounceSuppressesTheAddress(t *testing.T) {
	t.Parallel()

	env.RequireStubbed(t, "knows the webhook secret")

	secret := os.Getenv("BLACKBOX_MAILJET_WEBHOOK_SECRET")
	if secret == "" {
		secret = "blackbox-webhook-secret"
	}

	ctx := t.Context()
	k := newKit(t, "not-004")
	public := fixture.Anonymous(t)

	status, err := public.MailjetWebhook(ctx, secret, `[{"event":"sent","email":"`+k.customerEmail+`"}]`)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, status)

	k.sentQuote(t, 100_000)
	k.mailTo(t, k.customerEmail, "cần bạn duyệt")

	status, err = public.MailjetWebhook(ctx, secret, `[{"event":"bounce","email":"`+k.customerEmail+`","hard_bounce":true}]`)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, status)

	k.sentQuote(t, 100_000)

	skipped := k.waitForLog(t, notificationv1.NotificationType_NOTIFICATION_TYPE_QUOTE_READY, skippedStatus, 1)
	assert.Equal(t, notificationv1.NotificationSkipReason_NOTIFICATION_SKIP_REASON_SUPPRESSED, skipped[0].GetReason())

	count, err := k.inbox.Count(ctx, k.customerEmail, "cần bạn duyệt")
	require.NoError(t, err)
	assert.Equal(t, 1, count, "the second quote's e-mail was not sent")

	status, err = public.MailjetWebhook(ctx, "wrong-secret", `[{"event":"bounce","email":"someone@example.com","hard_bounce":true}]`)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, status)
}

// Scenario: E2E-NOT-005, Release 10.1, a part falling under its threshold notifies the
// owner once per crossing (FR-INV-04).
func TestLowStockOncePerCrossing(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	k := newKit(t, "not-005")
	api := k.owner.API

	unit := fixture.NewUnit(t, api, k.garageID, "Litre", "L")
	id := seq.Add(1)
	part := fixture.NewPart(t, api, k.garageID, fmt.Sprintf("LOW-%05d", id), fmt.Sprintf("Low oil %d", id), unit, fixture.WithThreshold("4"))
	subject := "Low stock: " + part.GetName()

	_, err := api.ReceiveStock(ctx, k.garageID, part.GetId(), "10", "", 0)
	require.NoError(t, err)

	writeOff := func(quantity string) {
		t.Helper()

		_, err := api.CreateWriteOff(ctx, k.garageID, &inventoryv1.WriteOff{
			Reason: inventoryv1.WriteOffReason_WRITE_OFF_REASON_DAMAGED,
			Lines:  []*inventoryv1.WriteOffLine{{PartId: part.GetId(), EnteredQuantity: quantity}},
		})
		require.NoError(t, err)
	}

	writeOff("5") // 5 left: above the threshold
	k.noMailTo(t, k.owner.Email, subject)

	writeOff("2") // 3 left: crosses
	k.mailTo(t, k.owner.Email, subject)

	writeOff("1") // 2 left: already under, so no second alert
	assert.Never(t, func() bool {
		n, err := k.inbox.Count(ctx, k.owner.Email, subject)

		return err != nil || n != 1
	}, absent, 500*time.Millisecond, "a part that stays under its threshold alerts once")

	_, err = api.ReceiveStock(ctx, k.garageID, part.GetId(), "10", "", 0) // 12
	require.NoError(t, err)

	writeOff("9") // 3 left: crosses again

	assert.Eventually(t, func() bool {
		n, err := k.inbox.Count(ctx, k.owner.Email, subject)

		return err == nil && n == 2
	}, arrives, 500*time.Millisecond, "a second alert after the stock rose and fell again")

	// Raising the threshold above the quantity alerts nobody.
	_, err = api.UpdatePart(ctx, k.garageID, part.GetId(), &cataloguev1.Part{ReorderThreshold: "20"}, "reorder_threshold")
	require.NoError(t, err)

	n, err := k.inbox.Count(ctx, k.owner.Email, subject)
	require.NoError(t, err)
	assert.Equal(t, 2, n)
}

// Scenario: E2E-NOT-006 (partial), Release 10.1, the log shows only the garage's own sends
// (FR-NOT-05). The retry of a failed send needs a mail server that can be made to fail;
// the relay's bounded attempts are covered by its unit tests.
func TestLogIsPrivateToTheGarage(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	a, b := newKit(t, "not-006a"), newKit(t, "not-006b")

	a.sentQuote(t, 100_000)
	a.mailTo(t, a.customerEmail, "cần bạn duyệt")
	a.waitForLog(t, notificationv1.NotificationType_NOTIFICATION_TYPE_QUOTE_READY, sentStatus, 1)

	own, err := b.owner.API.ListNotificationLog(ctx, b.garageID,
		notificationv1.NotificationType_NOTIFICATION_TYPE_UNSPECIFIED, notificationv1.NotificationStatus_NOTIFICATION_STATUS_UNSPECIFIED)
	require.NoError(t, err)
	assert.Empty(t, own, "garage B has sent nothing, and the account e-mails are not in its log")

	_, err = b.owner.API.ListNotificationLog(ctx, a.garageID,
		notificationv1.NotificationType_NOTIFICATION_TYPE_UNSPECIFIED, notificationv1.NotificationStatus_NOTIFICATION_STATUS_UNSPECIFIED)
	fixture.RequireAPIError(t, err, http.StatusForbidden, "GARAGE_ACCESS_DENIED")
}
