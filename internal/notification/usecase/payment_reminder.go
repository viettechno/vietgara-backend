package usecase

import (
	"context"
	"strings"
	"time"

	"github.com/viettechno/vietgara-backend/internal/notification/domain"
	"github.com/viettechno/vietgara-backend/internal/platform/mail"
	"github.com/viettechno/vietgara-backend/pkg/converter"
)

// PaymentReminderRequest is an overdue settlement the Receivables module asks
// Notification to remind a customer of (FR-REC-03).
type PaymentReminderRequest struct {
	GarageID     string
	CustomerID   string
	ReceivableID string
	// Number is the settlement's number.
	Number      string
	Remaining   int64
	DueDate     time.Time
	DaysOverdue int
}

// PaymentReminderResult is what became of a reminder: sent, or skipped with
// the reason the log also holds.
type PaymentReminderResult struct {
	Sent bool
	// Reason is DISABLED, NO_EMAIL, OPTED_OUT or SUPPRESSED (the log's
	// reason in capitals) when it was not sent.
	Reason string
}

// PaymentReminder e-mails a customer that a settlement is overdue. It is a
// call and not an event because the Receivables job owns the schedule and
// logs each reminder once. The garage's switch, the customer's opt-out and
// the suppression list apply; a skipped reminder is logged with its reason.
func (uc *NotificationUseCase) PaymentReminder(ctx context.Context, req PaymentReminderRequest) (PaymentReminderResult, error) {
	settings, garage, err := uc.settingsOf(ctx, req.GarageID)
	if err != nil {
		return PaymentReminderResult{}, err
	}

	customer, err := uc.Customers.Get(ctx, req.GarageID, req.CustomerID)
	if err != nil {
		return PaymentReminderResult{}, err
	}

	sent, err := uc.toCustomer(ctx, settings, garage, customer, note{
		typ: domain.TypePaymentReminder, garageID: req.GarageID, entityType: "receivable", entityID: req.ReceivableID,
		template: mail.TemplatePaymentReminder,
		data: map[string]any{
			keyNumber: req.Number, "Remaining": converter.FormatVND(req.Remaining), "DaysOverdue": req.DaysOverdue,
			"DueDate": req.DueDate.Format("02/01/2006"),
		},
	})
	if err != nil || sent {
		return PaymentReminderResult{Sent: sent}, err
	}

	return PaymentReminderResult{Reason: uc.skipReasonOf(settings, customer)}, nil
}

// skipReasonOf says why toCustomer did not send a non-essential customer
// e-mail, by the same order of checks.
func (uc *NotificationUseCase) skipReasonOf(settings domain.Settings, customer CustomerInfo) string {
	switch {
	case !settings.Enabled(domain.TypePaymentReminder):
		return strings.ToUpper(string(domain.SkipDisabled))
	case customer.Email == "":
		return strings.ToUpper(string(domain.SkipNoEmail))
	case customer.OptedOut:
		return strings.ToUpper(string(domain.SkipOptedOut))
	default:
		return strings.ToUpper(string(domain.SkipSuppressed))
	}
}
