package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"maps"
	"time"

	"github.com/google/uuid"

	"github.com/viettechno/vietgara-backend/internal/notification/domain"
	"github.com/viettechno/vietgara-backend/internal/platform/mail"
	"github.com/viettechno/vietgara-backend/internal/platform/outbox"
)

// Template variables used by several e-mails.
const (
	keyName     = "Name"
	keyPlanCode = "PlanCode"
)

// Deps are the dependencies of NotificationUseCase. The fields after Log
// serve the e-mails of a garage (FR-NOT-05); the account and licence e-mails
// need only the first three.
type Deps struct {
	Mailer Mailer
	Users  UserReader
	Log    Repository

	Settings     SettingsRepository
	Suppressions Suppressions
	Customers    CustomerReader
	Garages      GarageReader
	Staff        StaffReader
	Receipts     Receipts
	Tx           Transactor
	// OptOutKey signs the opt-out links of customer e-mails.
	OptOutKey []byte
	// BaseURL is the public URL of the deployment: the console and the API
	// share its origin.
	BaseURL string
}

// NotificationUseCase implements the Notification module's event
// consumers (FR-NOT-04).
type NotificationUseCase struct {
	Deps
}

// NewNotificationUseCase wires the use case.
func NewNotificationUseCase(deps Deps) *NotificationUseCase {
	return &NotificationUseCase{Deps: deps}
}

type emailVerifiedPayload struct {
	UserID   string `json:"user_id"`
	Email    string `json:"email"`
	FullName string `json:"full_name"`
	Locale   string `json:"locale"`
}

// HandleUserEmailVerified sends a registration-confirmation e-mail
// once a user verifies its address (FR-NOT-04). The triggering
// event's own payload already carries everything needed to address it,
// so no cross-module read is required.
func (uc *NotificationUseCase) HandleUserEmailVerified(ctx context.Context, event outbox.Event) error {
	var payload emailVerifiedPayload
	if err := json.Unmarshal(event.Data, &payload); err != nil || payload.Email == "" {
		return nil //nolint:nilerr // a malformed event cannot succeed on retry: drop it
	}

	return uc.send(ctx, payload.UserID, mail.TemplateRegistrationConfirmation, payload.Locale, payload.Email, payload.FullName,
		map[string]any{keyName: payload.FullName})
}

type subscriptionExpiringSoonPayload struct {
	UserID           string `json:"user_id"`
	PlanCode         string `json:"plan_code"`
	CurrentPeriodEnd string `json:"current_period_end"`
	AmountDue        int64  `json:"amount_due"`
	BankName         string `json:"bank_name"`
	AccountNumber    string `json:"account_number"`
	AccountHolder    string `json:"account_holder"`
	Branch           string `json:"branch"`
}

// HandleSubscriptionExpiringSoon sends a license renewal reminder ahead
// of a subscription's period end (FR-NOT-04). The triggering event
// carries only the user id, so the user's e-mail/name/locale are
// read fresh from Identity.
func (uc *NotificationUseCase) HandleSubscriptionExpiringSoon(ctx context.Context, event outbox.Event) error {
	var payload subscriptionExpiringSoonPayload
	if err := json.Unmarshal(event.Data, &payload); err != nil || payload.UserID == "" {
		return nil //nolint:nilerr // a malformed event cannot succeed on retry: drop it
	}

	user, err := uc.Users.Get(ctx, payload.UserID)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			slog.Warn("notification: user no longer exists, dropping its renewal reminder", "user_id", payload.UserID)

			return nil
		}

		return err
	}

	return uc.send(ctx, payload.UserID, mail.TemplateSubscriptionExpiringSoon, user.Locale, user.Email, user.FullName,
		map[string]any{
			keyName: user.FullName, keyPlanCode: payload.PlanCode,
			"CurrentPeriodEnd": formatPeriodEnd(payload.CurrentPeriodEnd, user.Locale),
			"AmountDue":        payload.AmountDue, "BankName": payload.BankName, "AccountNumber": payload.AccountNumber,
			"AccountHolder": payload.AccountHolder, "Branch": payload.Branch,
		})
}

type statusChangedPayload struct {
	UserID         string `json:"user_id"`
	From           string `json:"from"`
	To             string `json:"to"`
	PlanCode       string `json:"plan_code"`
	GracePeriodEnd string `json:"grace_period_end"`
	AmountDue      int64  `json:"amount_due"`
	BankName       string `json:"bank_name"`
	AccountNumber  string `json:"account_number"`
	AccountHolder  string `json:"account_holder"`
	Branch         string `json:"branch"`
}

// HandleSubscriptionStatusChanged tells the owner when the subscription
// becomes past due (read-only until the end of the grace period) or
// suspended, with what to pay and where (FR-LIC-04, FR-NOT-04). Other
// transitions are not e-mailed: a payment's own e-mail covers them.
func (uc *NotificationUseCase) HandleSubscriptionStatusChanged(ctx context.Context, event outbox.Event) error {
	var p statusChangedPayload
	if err := json.Unmarshal(event.Data, &p); err != nil || p.UserID == "" {
		return nil //nolint:nilerr // a malformed event cannot succeed on retry: drop it
	}

	var template string

	switch p.To {
	case "past_due":
		template = mail.TemplateSubscriptionPastDue
	case "suspended":
		template = mail.TemplateSubscriptionSuspended
	default:
		return nil
	}

	user, err := uc.Users.Get(ctx, p.UserID)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			slog.Warn("notification: user no longer exists, dropping its subscription e-mail", "user_id", p.UserID)

			return nil
		}

		return err
	}

	return uc.send(ctx, p.UserID, template, user.Locale, user.Email, user.FullName, map[string]any{
		keyName: user.FullName, keyPlanCode: p.PlanCode, "Trial": p.From == "trial",
		"GracePeriodEnd": formatPeriodEnd(p.GracePeriodEnd, user.Locale),
		"AmountDue":      p.AmountDue, "BankName": p.BankName, "AccountNumber": p.AccountNumber,
		"AccountHolder": p.AccountHolder, "Branch": p.Branch,
	})
}

type paymentPayload struct {
	UserID            string `json:"user_id"`
	PlanCode          string `json:"plan_code"`
	Reference         string `json:"reference"`
	AmountDue         int64  `json:"amount_due"`
	TransferredAmount int64  `json:"transferred_amount"`
	AmountMismatch    bool   `json:"amount_mismatch"`
	Reason            string `json:"reason"`
	Waiting           int    `json:"waiting"`
	OldestSubmittedAt string `json:"oldest_submitted_at"`
}

// HandlePaymentSubmitted tells every platform admin a plan payment waits
// for review (FR-LIC-07).
func (uc *NotificationUseCase) HandlePaymentSubmitted(ctx context.Context, event outbox.Event) error {
	var p paymentPayload
	if err := json.Unmarshal(event.Data, &p); err != nil || p.UserID == "" {
		return nil //nolint:nilerr // a malformed event cannot succeed on retry: drop it
	}

	return uc.tellAdmins(ctx, mail.TemplatePaymentSubmitted, map[string]any{
		"Reference": p.Reference, keyPlanCode: p.PlanCode, "AmountDue": p.AmountDue, "TransferredAmount": p.TransferredAmount,
		"Mismatch": p.AmountMismatch,
	})
}

// HandlePaymentReviewReminder reminds the platform admins daily of requests
// still waiting for review.
func (uc *NotificationUseCase) HandlePaymentReviewReminder(ctx context.Context, event outbox.Event) error {
	var p paymentPayload
	if err := json.Unmarshal(event.Data, &p); err != nil || p.Waiting == 0 {
		return nil //nolint:nilerr // a malformed event cannot succeed on retry: drop it
	}

	return uc.tellAdmins(ctx, mail.TemplatePaymentReviewReminder, map[string]any{
		"Waiting": p.Waiting, "Oldest": formatPeriodEnd(p.OldestSubmittedAt, mail.LocaleVI),
	})
}

// tellAdmins sends one e-mail to each platform admin, in their language.
func (uc *NotificationUseCase) tellAdmins(ctx context.Context, template string, data map[string]any) error {
	admins, err := uc.Users.PlatformAdmins(ctx)
	if err != nil {
		return err
	}

	for _, admin := range admins {
		personal := map[string]any{keyName: admin.FullName}
		maps.Copy(personal, data)

		if err := uc.send(ctx, admin.ID, template, admin.Locale, admin.Email, admin.FullName, personal); err != nil {
			return err
		}
	}

	return nil
}

// HandlePaymentApproved tells the owner their plan payment was approved and
// the plan applies.
func (uc *NotificationUseCase) HandlePaymentApproved(ctx context.Context, event outbox.Event) error {
	return uc.tellOwner(ctx, event, mail.TemplatePaymentApproved)
}

// HandlePaymentRejected tells the owner their plan payment was rejected,
// with the reason.
func (uc *NotificationUseCase) HandlePaymentRejected(ctx context.Context, event outbox.Event) error {
	return uc.tellOwner(ctx, event, mail.TemplatePaymentRejected)
}

func (uc *NotificationUseCase) tellOwner(ctx context.Context, event outbox.Event, template string) error {
	var p paymentPayload
	if err := json.Unmarshal(event.Data, &p); err != nil || p.UserID == "" {
		return nil //nolint:nilerr // a malformed event cannot succeed on retry: drop it
	}

	user, err := uc.Users.Get(ctx, p.UserID)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			slog.Warn("notification: user no longer exists, dropping its payment notice", "user_id", p.UserID)

			return nil
		}

		return err
	}

	return uc.send(ctx, p.UserID, template, user.Locale, user.Email, user.FullName, map[string]any{
		keyName: user.FullName, keyPlanCode: p.PlanCode, "Reference": p.Reference, "Reason": p.Reason,
	})
}

// formatPeriodEnd renders an RFC3339 instant as a plain date in the
// user's language, the same locale-aware convention
// tenant/usecase/invitation.go's sendInvitation already uses. Falls back
// to the raw value if it doesn't parse, rather than failing the whole
// reminder over a display detail.
func formatPeriodEnd(rfc3339, locale string) string {
	t, err := time.Parse(time.RFC3339, rfc3339)
	if err != nil {
		return rfc3339
	}

	layout := "02/01/2006"
	if locale == mail.LocaleEN {
		layout = "Jan 2, 2006"
	}

	return t.Format(layout)
}

// send renders and delivers one e-mail, logging it once sent. A send
// failure is returned unchanged so the triggering event redelivers and
// the e-mail is retried -- no "failed" row is written, since it would
// only roll back with the rest of the transaction anyway.
func (uc *NotificationUseCase) send(ctx context.Context, userID, template, locale, email, name string, data any) error {
	msg, err := mail.Render(template, locale, email, name, data)
	if err != nil {
		return err
	}

	if err := uc.Mailer.Send(ctx, msg); err != nil {
		return err
	}

	return uc.Log.Create(ctx, &domain.Log{
		ID: uuid.NewString(), UserID: userID, Channel: domain.ChannelEmail, Template: template, Recipient: email,
		Status: domain.StatusSent,
	})
}

// FormatPeriodEndForTest is a test helper for formatPeriodEnd.
func FormatPeriodEndForTest(rfc3339, locale string) string {
	return formatPeriodEnd(rfc3339, locale)
}

// SendForTest is a test helper for send.
func (uc *NotificationUseCase) SendForTest(ctx context.Context, userID, template, locale, email, name string, data any) error {
	return uc.send(ctx, userID, template, locale, email, name, data)
}
