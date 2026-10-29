package usecase

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/viettechno/vietgara-backend/internal/notification/domain"
	"github.com/viettechno/vietgara-backend/internal/platform/mail"
)

// note is one e-mail of a garage (FR-NOT-05) before it has a recipient.
type note struct {
	typ        domain.Type
	garageID   string
	customerID string
	entityType string
	entityID   string
	eventID    string
	template   string
	data       map[string]any
	attach     []mail.Attachment
}

// recipient is who a note goes to.
type recipient struct {
	userID string
	name   string
	email  string
	locale string
}

// settingsOf reads the garage's choices and says whether n's type is on.
func (uc *NotificationUseCase) settingsOf(ctx context.Context, garageID string) (domain.Settings, GarageInfo, error) {
	settings, err := uc.Settings.Get(ctx, garageID)
	if err != nil {
		return domain.Settings{}, GarageInfo{}, err
	}

	garage, err := uc.Garages.Get(ctx, garageID)
	if err != nil {
		return domain.Settings{}, GarageInfo{}, err
	}

	return settings, garage, nil
}

// toCustomer sends n to the customer: unless the garage switched the type
// off, the customer has an address, did not opt out of a non-essential type
// and the address did not bounce. It reports whether the e-mail went out.
func (uc *NotificationUseCase) toCustomer(
	ctx context.Context, settings domain.Settings, garage GarageInfo, customer CustomerInfo, n note,
) (bool, error) {
	n.customerID = customer.ID
	n.data["CustomerName"] = customer.Name
	n.data["GarageName"] = garage.Name
	n.data["GaragePhone"], n.data["GarageAddress"] = garage.Phone, garage.Address

	switch {
	case !settings.Enabled(n.typ):
		return false, uc.skip(ctx, n, customer.Email, domain.SkipDisabled)
	case customer.Email == "":
		return false, uc.skip(ctx, n, "", domain.SkipNoEmail)
	case !n.typ.Essential() && customer.OptedOut:
		return false, uc.skip(ctx, n, customer.Email, domain.SkipOptedOut)
	}

	var headers map[string]string

	if !n.typ.Essential() {
		url := uc.optOutURL(n.garageID, customer.ID)
		n.data["OptOutURL"] = url
		headers = map[string]string{"List-Unsubscribe": "<" + url + ">", "List-Unsubscribe-Post": "List-Unsubscribe=One-Click"}
	}

	return uc.deliver(ctx, settings, garage, n, recipient{name: customer.Name, email: customer.Email, locale: customer.Locale}, headers)
}

// toStaff sends n to each member of staff, in their own language.
func (uc *NotificationUseCase) toStaff(
	ctx context.Context, settings domain.Settings, garage GarageInfo, staff []UserInfo, n note,
) error {
	n.data["GarageName"] = garage.Name
	n.data["ConsoleURL"] = strings.TrimSuffix(uc.BaseURL, "/")

	if !settings.Enabled(n.typ) {
		return uc.skip(ctx, n, "", domain.SkipDisabled)
	}

	for _, user := range staff {
		personal := n
		personal.data = map[string]any{"Name": user.FullName}

		for k, v := range n.data {
			personal.data[k] = v
		}

		to := recipient{userID: user.ID, name: user.FullName, email: user.Email, locale: user.Locale}
		if _, err := uc.deliver(ctx, settings, garage, personal, to, nil); err != nil {
			return err
		}
	}

	return nil
}

// deliver renders and sends one e-mail and logs it. A failure is logged in a
// transaction of its own and returned, so the event is delivered again.
func (uc *NotificationUseCase) deliver(
	ctx context.Context, settings domain.Settings, garage GarageInfo, n note, to recipient, headers map[string]string,
) (bool, error) {
	suppressed, err := uc.Suppressions.Suppressed(ctx, to.email)
	if err != nil {
		return false, err
	}

	if suppressed {
		return false, uc.skip(ctx, n, to.email, domain.SkipSuppressed)
	}

	msg, err := mail.Render(n.template, to.locale, to.email, to.name, n.data)
	if err != nil {
		return false, err
	}

	msg.FromName = settings.SenderName
	if msg.FromName == "" {
		msg.FromName = garage.Name
	}

	msg.ReplyTo, msg.Headers, msg.Attachments = garage.Email, headers, n.attach

	if err := uc.Mailer.Send(ctx, msg); err != nil {
		if logErr := uc.Log.Failed(ctx, uc.logOf(n, to.userID, to.email, domain.StatusFailed, "", err.Error())); logErr != nil {
			slog.Warn("notification: logging a failed send failed", "type", n.typ, "error", logErr)
		}

		return false, err
	}

	return true, uc.Log.Create(ctx, uc.logOf(n, to.userID, to.email, domain.StatusSent, "", ""))
}

// skip logs an e-mail that was not sent and why.
func (uc *NotificationUseCase) skip(ctx context.Context, n note, email string, reason domain.SkipReason) error {
	return uc.Log.Create(ctx, uc.logOf(n, "", email, domain.StatusSkipped, reason, ""))
}

func (uc *NotificationUseCase) logOf(n note, userID, email string, status domain.Status, reason domain.SkipReason, failure string) *domain.Log {
	return &domain.Log{
		ID: uuid.NewString(), UserID: userID, Channel: domain.ChannelEmail, Template: n.template, Recipient: email,
		Status: status, Error: failure, GarageID: n.garageID, CustomerID: n.customerID, Type: n.typ,
		EntityType: n.entityType, EntityID: n.entityID, EventID: n.eventID, SkipReason: reason,
	}
}

// QuoteReadyRequest is the quote an advisor just sent to the customer.
type QuoteReadyRequest struct {
	GarageID     string
	QuoteID      string
	CustomerID   string
	Number       string
	VehiclePlate string
	// Total is the amount formatted for display.
	Total string
	// ApproveURL is the customer's link to approve the quote. It exists only
	// at this moment (the quote keeps its hash), which is why this e-mail is
	// a call and not an event.
	ApproveURL string
}

// QuoteReady e-mails the approval link of a quote to its customer (FR-NOT-05).
// It reports whether the e-mail went out; a failure is logged, not returned,
// since the advisor can still share the link by other means.
func (uc *NotificationUseCase) QuoteReady(ctx context.Context, req QuoteReadyRequest) bool {
	sent, err := uc.quoteReady(ctx, req)
	if err != nil {
		slog.Warn("notification: sending the quote e-mail failed", "quote_id", req.QuoteID, "error", err)

		return false
	}

	return sent
}

func (uc *NotificationUseCase) quoteReady(ctx context.Context, req QuoteReadyRequest) (bool, error) {
	settings, garage, err := uc.settingsOf(ctx, req.GarageID)
	if err != nil {
		return false, err
	}

	customer, err := uc.Customers.Get(ctx, req.GarageID, req.CustomerID)
	if err != nil {
		return false, err
	}

	return uc.toCustomer(ctx, settings, garage, customer, note{
		typ: domain.TypeQuoteReady, garageID: req.GarageID, entityType: "quote", entityID: req.QuoteID,
		template: mail.TemplateQuoteReadyForApproval,
		data: map[string]any{
			"Number": req.Number, "VehiclePlate": req.VehiclePlate, "TotalAmount": req.Total, "ApproveURL": req.ApproveURL,
		},
	})
}

// isGone reports an error that a retry cannot cure: the customer or the
// garage the event concerns no longer exists.
func isGone(err error) bool {
	return errors.Is(err, ErrCustomerNotFound) || errors.Is(err, ErrGarageNotFound)
}
