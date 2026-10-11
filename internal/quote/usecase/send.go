package usecase

import (
	"context"
	"log/slog"
	"strings"

	"github.com/viettechno/vietgara-backend/internal/platform/mail"
	"github.com/viettechno/vietgara-backend/internal/quote/domain"
	"github.com/viettechno/vietgara-backend/pkg/converter"
)

// SendQuoteResult is the outcome of QuoteUseCase.Send.
type SendQuoteResult struct {
	Quote *domain.Quote
	// PublicURL is the approval link, set only for QUOTE_APPROVAL_MODE_LINK.
	PublicURL string
	// Emailed reports whether the link above was actually e-mailed to the
	// customer (they need an e-mail on file; a delivery failure is
	// logged, not returned, so the advisor can still copy PublicURL).
	Emailed bool
}

// Send moves a quote to "sent" (FR-QUO-02). For QUOTE_APPROVAL_MODE_LINK it
// generates a fresh public approval token, e-mailing it to the customer
// automatically when they have an e-mail on file -- "at least [notifying]
// customers [of] what happened to their vehicle and what should be
// fixed", per the gara-owner requirement this module implements. For
// QUOTE_APPROVAL_MODE_MANUAL no link or e-mail is produced: the garage
// only needs RecordQuoteDecision once the customer has decided face to
// face. The mode itself is chosen per quote (Create/Update), defaulting to
// the garage's configuration (FR-TEN-05).
func (uc *QuoteUseCase) Send(ctx context.Context, garageID, quoteID, actorID string) (SendQuoteResult, error) {
	q, err := uc.Quotes.FindByID(ctx, garageID, quoteID)
	if err != nil {
		return SendQuoteResult{}, toAppError("send quote", err)
	}

	if !q.Status.Sendable() {
		return SendQuoteResult{}, toAppError("send quote", domain.ErrNotEditable)
	}

	// The quote is issued to the customer now: it carries the garage's
	// legal identity (FR-TEN-06) as it is at this moment.
	garage, err := uc.garageInfo(ctx, garageID)
	if err != nil {
		return SendQuoteResult{}, err
	}

	now := uc.Now()
	q.Status = domain.StatusSent
	q.SentAt = &now
	q.Issuer = garage.Issuer
	q.UpdatedBy = actorRef(actorID)

	result := SendQuoteResult{Quote: q}

	if q.ApprovalMode == domain.ApprovalModeManual {
		q.PublicTokenHash, q.PublicTokenExpiresAt = nil, nil
	} else {
		token := domain.NewPublicToken()
		hash := domain.HashPublicToken(token)
		expiresAt := now.Add(domain.PublicTokenTTL)
		q.PublicTokenHash, q.PublicTokenExpiresAt = &hash, &expiresAt
		result.PublicURL = strings.TrimSuffix(uc.AppBaseURL, "/") + "/public/quotes/" + token
	}

	if err := uc.Quotes.Save(ctx, q); err != nil {
		return SendQuoteResult{}, toAppError("send quote", err)
	}

	if q.ApprovalMode == domain.ApprovalModeLink {
		result.Emailed = uc.emailApprovalLink(ctx, q, result.PublicURL)
	}

	return result, nil
}

// emailApprovalLink e-mails the approval link when the customer has an
// e-mail on file. A missing e-mail or a delivery failure is not an error:
// the quote is already sent and the advisor can share PublicURL by other
// means (this is exactly why LINK mode does not depend on the e-mail
// succeeding to be usable).
func (uc *QuoteUseCase) emailApprovalLink(ctx context.Context, q *domain.Quote, publicURL string) bool {
	email, err := uc.Customers.Email(ctx, q.GarageID, q.CustomerID)
	if err != nil {
		slog.Warn("quote: reading the customer's e-mail failed", "quote_id", q.ID, "error", err)

		return false
	}

	if email == nil {
		return false
	}

	msg, err := mail.Render(mail.TemplateQuoteReadyForApproval, mail.LocaleVI, *email, q.CustomerName, map[string]any{
		"CustomerName": q.CustomerName,
		"Number":       q.Number,
		"VehiclePlate": q.VehicleLicensePlate,
		"TotalAmount":  converter.FormatVND(q.TotalAmount),
		"ApproveURL":   publicURL,
	})
	if err == nil {
		err = uc.Mailer.Send(ctx, msg)
	}

	if err != nil {
		slog.Warn("quote: sending the approval e-mail failed", "quote_id", q.ID, "error", err)

		return false
	}

	return true
}
