package usecase

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/netip"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/viettechno/vietgara-backend/internal/datarequest/domain"
	"github.com/viettechno/vietgara-backend/internal/platform/mail"
	"github.com/viettechno/vietgara-backend/pkg/email"
)

// Limits of the public page. The gateway limits the rate per source; these
// bound what is stored and e-mailed per address and per source.
const (
	MaxPerAddressPerHour = 3
	MaxPerSourcePerHour  = 10
	// LinkValidity is how long the e-mailed link works.
	LinkValidity = 24 * time.Hour
	// Retention is how long a waiting request is kept before it is purged with
	// its details.
	Retention = 7 * 24 * time.Hour

	mailTimeout = 30 * time.Second
	tokenBytes  = 32
)

// PublicUseCase serves the request page for people without the app: a request
// waits for its e-mailed link, which then acts on every customer record that
// holds the address, in every garage (FR-CUS-05).
type PublicUseCase struct {
	confirmations Confirmations
	customers     Customers
	documents     Documents
	repo          Repository
	names         Names
	tx            Transactor
	mailer        Mailer
	baseURL       string
	sourceKey     []byte
	now           func() time.Time
}

// PublicDeps are the collaborators of PublicUseCase.
type PublicDeps struct {
	Confirmations Confirmations
	Customers     Customers
	Documents     Documents
	Repo          Repository
	Names         Names
	Tx            Transactor
	Mailer        Mailer
	// BaseURL is where the confirmation page lives.
	BaseURL string
	// SourceKey keys the hash of a request's source address.
	SourceKey []byte
}

// NewPublicUseCase wires the use case.
func NewPublicUseCase(d PublicDeps) *PublicUseCase {
	return &PublicUseCase{
		confirmations: d.Confirmations, customers: d.Customers, documents: d.Documents, repo: d.Repo, names: d.Names, tx: d.Tx,
		mailer: d.Mailer, baseURL: strings.TrimSuffix(d.BaseURL, "/"), sourceKey: d.SourceKey, now: time.Now,
	}
}

// SubmitRequest is the input of Submit.
type SubmitRequest struct {
	Email    string
	Type     domain.Type
	Details  string
	Locale   string
	SourceIP string
}

// Submit records a request and e-mails the confirmation link to the address,
// and only to it. It answers the same whether or not a garage holds the
// address: nothing is looked up. An address or a source that asked too often
// is refused.
func (uc *PublicUseCase) Submit(ctx context.Context, req SubmitRequest) error {
	const op = "submit public data request"

	address, err := email.Normalize(req.Email)
	if err != nil {
		return toAppError(op, domain.ErrInvalidEmail)
	}

	if !req.Type.IsValid() {
		return toAppError(op, domain.ErrInvalidType)
	}

	details := strings.TrimSpace(req.Details)
	if req.Type != domain.TypeCorrect {
		details = ""
	}

	if err := checkDetails(req.Type, details); err != nil {
		return toAppError(op, err)
	}

	now := uc.now().UTC()
	source := uc.hashSource(req.SourceIP)

	if err := uc.throttle(ctx, address, source, now); err != nil {
		return err
	}

	token, err := newToken()
	if err != nil {
		return toAppError(op, err)
	}

	locale := mail.LocaleVI
	if req.Locale == mail.LocaleEN {
		locale = mail.LocaleEN
	}

	c := &domain.Confirmation{
		ID: uuid.NewString(), Email: address, Type: req.Type, Details: details, Locale: locale, TokenHash: hashToken(token),
		SourceHash: source, ExpiresAt: now.Add(LinkValidity), CreatedAt: now,
	}
	if err := uc.confirmations.CreateConfirmation(ctx, c); err != nil {
		return toAppError(op, err)
	}

	// The request is stored: the answer does not depend on the e-mail, so a
	// failure cannot tell whether the address is known (it is never looked up).
	mailCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), mailTimeout)
	defer cancel()

	msg, err := mail.Render(mail.TemplateDataRequestConfirmation, locale, address, "", map[string]any{
		"Type": string(req.Type), "ConfirmURL": uc.baseURL + "/data-request/confirm?token=" + token, "Hours": int(LinkValidity.Hours()),
	})
	if err == nil {
		err = uc.mailer.Send(mailCtx, msg)
	}

	if err != nil {
		slog.ErrorContext(ctx, "datarequest: the confirmation e-mail failed", "confirmation_id", c.ID, "error", err)
	}

	return nil
}

func (uc *PublicUseCase) throttle(ctx context.Context, address, source string, now time.Time) error {
	since := now.Add(-time.Hour)

	byAddress, err := uc.confirmations.CountByEmail(ctx, address, since)
	if err != nil {
		return toAppError("count requests", err)
	}

	bySource, err := uc.confirmations.CountBySource(ctx, source, since)
	if err != nil {
		return toAppError("count requests", err)
	}

	if byAddress >= MaxPerAddressPerHour || bySource >= MaxPerSourcePerHour {
		return toAppError("throttle", domain.ErrTooManyRequests)
	}

	return nil
}

// Confirm acts on a confirmed request: on every customer record that holds the
// address, in each garage in a transaction of its own, a view is e-mailed to
// the address, a deletion is carried out and a correction is passed to the
// garage. Each action is logged as a request of that garage. It returns how
// many garages were reached. The link works once.
func (uc *PublicUseCase) Confirm(ctx context.Context, token string) (int, error) {
	const op = "confirm public data request"

	if strings.TrimSpace(token) == "" {
		return 0, toAppError(op, domain.ErrTokenInvalid)
	}

	c, err := uc.confirmations.Consume(ctx, hashToken(token), uc.now().UTC())
	if err != nil {
		return 0, toAppError(op, err)
	}

	refs, err := uc.customers.ByEmail(ctx, c.Email)
	if err != nil {
		return 0, toAppError(op, err)
	}

	var (
		garages []string
		byGarge = map[string][]CustomerRef{}
		views   []viewSection
	)

	for _, ref := range refs {
		if _, ok := byGarge[ref.GarageID]; !ok {
			garages = append(garages, ref.GarageID)
		}

		byGarge[ref.GarageID] = append(byGarge[ref.GarageID], ref)
	}

	for _, garageID := range garages {
		section, err := uc.actInGarage(ctx, c, garageID, byGarge[garageID])
		if err != nil {
			return 0, toAppError(op, err)
		}

		views = append(views, section...)
	}

	if c.Type == domain.TypeView && len(views) > 0 {
		if err := uc.mailView(ctx, c, views); err != nil {
			return 0, toAppError(op, err)
		}
	}

	return len(garages), nil
}

// viewSection is the data one garage holds, for the e-mail of a view.
type viewSection struct {
	Garage string
	Data   string
}

func (uc *PublicUseCase) actInGarage(ctx context.Context, c *domain.Confirmation, garageID string, refs []CustomerRef) ([]viewSection, error) {
	var sections []viewSection

	err := uc.tx.InGarage(ctx, garageID, func(ctx context.Context) error {
		now := uc.now().UTC()

		for _, ref := range refs {
			r := &domain.Request{
				ID: uuid.NewString(), GarageID: garageID, CustomerID: ref.ID, Type: c.Type, Status: domain.StatusPending,
				Source: domain.SourcePublic, Details: c.Details, RequestedAt: now,
			}

			if err := act(ctx, uc.customers, uc.documents, r, "", now); err != nil {
				return err
			}

			if err := uc.repo.Create(ctx, r); err != nil {
				return err
			}

			switch c.Type {
			case domain.TypeView:
				name, err := uc.names.GarageName(ctx, garageID)
				if err != nil {
					return err
				}

				sections = append(sections, viewSection{Garage: name, Data: r.ExportJSON})
			case domain.TypeDelete:
				if err := uc.repo.ClearDetails(ctx, garageID, ref.ID); err != nil {
					return err
				}
			case domain.TypeCorrect:
			}
		}

		return nil
	})

	return sections, err
}

// mailView e-mails the data the garages hold to the confirmed address.
func (uc *PublicUseCase) mailView(ctx context.Context, c *domain.Confirmation, sections []viewSection) error {
	msg, err := mail.Render(mail.TemplateDataRequestView, c.Locale, c.Email, "", map[string]any{"Sections": sections})
	if err != nil {
		return err
	}

	return uc.mailer.Send(ctx, msg)
}

// Purge deletes the waiting requests older than the retention, with their
// details; it returns how many.
func (uc *PublicUseCase) Purge(ctx context.Context) (int, error) {
	n, err := uc.confirmations.PurgeBefore(ctx, uc.now().UTC().Add(-Retention))
	if err != nil {
		return 0, toAppError("purge data request confirmations", err)
	}

	return n, nil
}

// Schedule purges now and then every interval until ctx is cancelled.
func (uc *PublicUseCase) Schedule(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		if _, err := uc.Purge(ctx); err != nil && ctx.Err() == nil {
			slog.Error("datarequest: purging the waiting requests failed", "error", err)
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func newToken() (string, error) {
	b := make([]byte, tokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}

	return base64.RawURLEncoding.EncodeToString(b), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))

	return hex.EncodeToString(sum[:])
}

// hashSource is a keyed hash of the source address; an IPv6 address counts by
// its /64.
func (uc *PublicUseCase) hashSource(ip string) string {
	if addr, err := netip.ParseAddr(ip); err == nil {
		addr = addr.Unmap()
		if addr.Is6() {
			addr = netip.PrefixFrom(addr, 64).Masked().Addr()
		}

		ip = addr.String()
	}

	mac := hmac.New(sha256.New, uc.sourceKey)
	mac.Write([]byte("data-request-source\n" + ip))

	return hex.EncodeToString(mac.Sum(nil))
}
