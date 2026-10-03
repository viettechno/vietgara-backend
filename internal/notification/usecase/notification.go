package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/viettechno/vietgara-backend/internal/notification/domain"
	"github.com/viettechno/vietgara-backend/internal/platform/mail"
	"github.com/viettechno/vietgara-backend/internal/platform/outbox"
)

// Deps are the dependencies of NotificationUseCase.
type Deps struct {
	Mailer   Mailer
	Accounts AccountReader
	Log      Repository
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
	AccountID string `json:"account_id"`
	Email     string `json:"email"`
	FullName  string `json:"full_name"`
	Locale    string `json:"locale"`
}

// HandleAccountEmailVerified sends a registration-confirmation e-mail
// once an account verifies its address (FR-NOT-04). The triggering
// event's own payload already carries everything needed to address it,
// so no cross-module read is required.
func (uc *NotificationUseCase) HandleAccountEmailVerified(ctx context.Context, event outbox.Event) error {
	var payload emailVerifiedPayload
	if err := json.Unmarshal(event.Data, &payload); err != nil || payload.Email == "" {
		return nil //nolint:nilerr // a malformed event cannot succeed on retry: drop it
	}

	return uc.send(ctx, payload.AccountID, mail.TemplateRegistrationConfirmation, payload.Locale, payload.Email, payload.FullName,
		map[string]any{"Name": payload.FullName})
}

type subscriptionExpiringSoonPayload struct {
	AccountID        string `json:"account_id"`
	PlanCode         string `json:"plan_code"`
	CurrentPeriodEnd string `json:"current_period_end"`
}

// HandleSubscriptionExpiringSoon sends a license renewal reminder ahead
// of a subscription's period end (FR-NOT-04). The triggering event
// carries only the account id, so the account's e-mail/name/locale are
// read fresh from Identity.
func (uc *NotificationUseCase) HandleSubscriptionExpiringSoon(ctx context.Context, event outbox.Event) error {
	var payload subscriptionExpiringSoonPayload
	if err := json.Unmarshal(event.Data, &payload); err != nil || payload.AccountID == "" {
		return nil //nolint:nilerr // a malformed event cannot succeed on retry: drop it
	}

	account, err := uc.Accounts.Get(ctx, payload.AccountID)
	if err != nil {
		if errors.Is(err, ErrAccountNotFound) {
			slog.Warn("notification: account no longer exists, dropping its renewal reminder", "account_id", payload.AccountID)

			return nil
		}

		return err
	}

	return uc.send(ctx, payload.AccountID, mail.TemplateSubscriptionExpiringSoon, account.Locale, account.Email, account.FullName,
		map[string]any{
			"Name": account.FullName, "PlanCode": payload.PlanCode,
			"CurrentPeriodEnd": formatPeriodEnd(payload.CurrentPeriodEnd, account.Locale),
		})
}

// formatPeriodEnd renders an RFC3339 instant as a plain date in the
// account's language, the same locale-aware convention
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
func (uc *NotificationUseCase) send(ctx context.Context, accountID, template, locale, email, name string, data any) error {
	msg, err := mail.Render(template, locale, email, name, data)
	if err != nil {
		return err
	}

	if err := uc.Mailer.Send(ctx, msg); err != nil {
		return err
	}

	return uc.Log.Create(ctx, &domain.Log{
		ID: uuid.NewString(), AccountID: accountID, Channel: domain.ChannelEmail, Template: template, Recipient: email,
		Status: domain.StatusSent,
	})
}
