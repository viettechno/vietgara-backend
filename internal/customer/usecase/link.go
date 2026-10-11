package usecase

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/viettechno/vietgara-backend/internal/customer/domain"
	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/platform/audit"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// Audited actions of the customer-user link (Security Architecture Section 7).
const (
	ActionLinkRequested = "customer.link_requested"
	ActionLinkConfirmed = "customer.link_confirmed"
	ActionLinkEnded     = "customer.link_ended"
	ActionLinkAnswered  = "customer.link_answered"

	entityLink = "customer_user_link"
)

// LinkRepository defines the persistence contract for customer-user links.
type LinkRepository interface {
	// Create inserts l; a customer with a current link returns
	// domain.ErrAlreadyLinked.
	Create(ctx context.Context, l *domain.UserLink) error
	// FindCurrent returns the customer's requested or linked link, or
	// domain.ErrLinkNotFound.
	FindCurrent(ctx context.Context, garageID, customerID string) (*domain.UserLink, error)
	// FindForUser returns one of the user's own links, or
	// domain.ErrLinkNotFound.
	FindForUser(ctx context.Context, userID, linkID string) (*domain.UserLink, error)
	// ListOpenForUser returns the user's requested and linked links.
	ListOpenForUser(ctx context.Context, userID string) ([]*domain.UserLink, error)
	// HasAny reports whether the customer ever had a link with the user.
	HasAny(ctx context.Context, customerID, userID string) (bool, error)
	// Update persists the link's status, channel and timestamps.
	Update(ctx context.Context, l *domain.UserLink) error
}

// UserDirectory answers, for the whole platform, which user owns a verified
// e-mail. It returns no user data: the garage only learns that someone can
// be asked. Implemented in cmd/server over the Identity module.
type UserDirectory interface {
	// FindVerifiedByEmail returns the id of the user whose verified e-mail
	// is email, and false when there is none.
	FindVerifiedByEmail(ctx context.Context, email string) (userID string, found bool, err error)
}

// LinkCodes sends and checks the one-time code that confirms a link
// request; implemented in cmd/server over the Identity module's OTP
// infrastructure.
type LinkCodes interface {
	// Send e-mails the user a code for the link request of garageName.
	Send(ctx context.Context, userID, linkID, garageName string) error
	// Check consumes the code, or returns a classified error when it is
	// wrong, expired or tried too often.
	Check(ctx context.Context, linkID, code string) error
}

// LinkGarages names a garage for the request the user receives.
type LinkGarages interface {
	Name(ctx context.Context, garageID string) (string, error)
}

// LinkAuditor records link changes in the caller's transaction.
type LinkAuditor interface {
	Record(ctx context.Context, e audit.Entry) error
}

// LinkDeps are the collaborators of LinkUseCase.
type LinkDeps struct {
	Links     LinkRepository
	Customers CustomerRepository
	Users     UserDirectory
	Codes     LinkCodes
	Garages   LinkGarages
	Audit     LinkAuditor
}

// LinkUseCase (FR-CUS-06) links a garage's customer profile with a VietGara
// user, with the user's consent.
type LinkUseCase struct {
	LinkDeps

	now func() time.Time
}

// NewLinkUseCase wires the link use case.
func NewLinkUseCase(deps LinkDeps, opts ...Option) *LinkUseCase {
	return &LinkUseCase{LinkDeps: deps, now: newOptions(opts).now}
}

// Request asks the user that owns the customer's verified e-mail to link:
// a one-time code goes to their e-mail, and the request appears in the
// Customer App. Asking again while a request is waiting sends a fresh code.
// domain.ErrNoMatchingUser when the customer has no e-mail or no verified
// user owns it.
func (uc *LinkUseCase) Request(ctx context.Context, garageID, customerID, actorID string) (*domain.UserLink, error) {
	customer, err := uc.Customers.FindByID(ctx, garageID, customerID)
	if err != nil {
		return nil, toAppError("request link", err)
	}

	userID, err := uc.matchUser(ctx, customer)
	if err != nil {
		return nil, err
	}

	link, done, err := uc.reuseCurrent(ctx, garageID, customerID, userID, actorID)
	if err != nil || done {
		return link, err
	}

	link = &domain.UserLink{
		ID: uuid.NewString(), GarageID: garageID, CustomerID: customerID, UserID: userID,
		Status: domain.LinkRequested, RequestedAt: uc.now().UTC(),
	}

	if err := uc.Links.Create(ctx, link); err != nil {
		return nil, toAppError("request link", err)
	}

	if err := uc.sendCode(ctx, link, garageID); err != nil {
		return nil, err
	}

	if err := uc.audit(ctx, actorID, link, ActionLinkRequested, nil, linkAudit(link)); err != nil {
		return nil, err
	}

	return link, nil
}

// reuseCurrent deals with a customer's current link before a new request:
// a link already in force is a conflict, a request waiting for this user
// gets its code sent again (done), and one waiting for another user -- the
// e-mail changed hands -- ends.
func (uc *LinkUseCase) reuseCurrent(
	ctx context.Context, garageID, customerID, userID, actorID string,
) (link *domain.UserLink, done bool, err error) {
	link, err = uc.Links.FindCurrent(ctx, garageID, customerID)

	switch {
	case errors.Is(err, domain.ErrLinkNotFound):
		return nil, false, nil
	case err != nil:
		return nil, false, toAppError("request link", err)
	case link.Status == domain.LinkLinked:
		return nil, false, toAppError("request link", domain.ErrAlreadyLinked)
	case link.UserID == userID:
		if err := uc.sendCode(ctx, link, garageID); err != nil {
			return nil, false, err
		}

		return link, true, nil
	}

	return nil, false, uc.end(ctx, link, domain.LinkUnlinked, actorID, ActionLinkEnded)
}

// matchUser finds the user who owns the customer's e-mail.
func (uc *LinkUseCase) matchUser(ctx context.Context, c *domain.Customer) (string, error) {
	if c.Email == nil || strings.TrimSpace(*c.Email) == "" {
		return "", toAppError("request link", domain.ErrNoMatchingUser)
	}

	userID, found, err := uc.Users.FindVerifiedByEmail(ctx, *c.Email)
	if err != nil {
		return "", toAppError("find the user to link", err)
	}

	if !found {
		return "", toAppError("request link", domain.ErrNoMatchingUser)
	}

	return userID, nil
}

func (uc *LinkUseCase) sendCode(ctx context.Context, link *domain.UserLink, garageID string) error {
	name, err := uc.Garages.Name(ctx, garageID)
	if err != nil {
		return toAppError("read the garage", err)
	}

	if err := uc.Codes.Send(ctx, link.UserID, link.ID, name); err != nil {
		return toAppError("send the link code", err)
	}

	return nil
}

// Confirm links the customer when code is the one the user read out.
func (uc *LinkUseCase) Confirm(ctx context.Context, garageID, customerID, actorID, code string) (*domain.UserLink, error) {
	link, err := uc.Links.FindCurrent(ctx, garageID, customerID)

	switch {
	case errors.Is(err, domain.ErrLinkNotFound):
		return nil, toAppError("confirm link", domain.ErrNoPendingRequest)
	case err != nil:
		return nil, toAppError("confirm link", err)
	case link.Status != domain.LinkRequested:
		return nil, toAppError("confirm link", domain.ErrAlreadyLinked)
	}

	if err := uc.Codes.Check(ctx, link.ID, strings.TrimSpace(code)); err != nil {
		return nil, toAppError("check the link code", err)
	}

	before := linkAudit(link)

	if err := uc.markLinked(ctx, link, domain.ChannelOTP); err != nil {
		return nil, err
	}

	if err := uc.audit(ctx, actorID, link, ActionLinkConfirmed, before, linkAudit(link)); err != nil {
		return nil, err
	}

	return link, nil
}

// Unlink ends the customer's current link, or cancels a waiting request,
// from the garage's side; the profile stays.
func (uc *LinkUseCase) Unlink(ctx context.Context, garageID, customerID, actorID string) (*domain.UserLink, error) {
	link, err := uc.Links.FindCurrent(ctx, garageID, customerID)
	if err != nil {
		return nil, toAppError("unlink customer", err)
	}

	if err := uc.end(ctx, link, domain.LinkUnlinked, actorID, ActionLinkEnded); err != nil {
		return nil, err
	}

	return link, nil
}

// ListForUser returns the user's requested and linked links.
func (uc *LinkUseCase) ListForUser(ctx context.Context, userID string) ([]*domain.UserLink, error) {
	links, err := uc.Links.ListOpenForUser(ctx, userID)
	if err != nil {
		return nil, apperror.Internal("list the user's links", err)
	}

	return links, nil
}

// FindForUser returns one of the user's own links; run it in the user's
// scope to learn the link's garage before answering it in that garage's.
func (uc *LinkUseCase) FindForUser(ctx context.Context, userID, linkID string) (*domain.UserLink, error) {
	link, err := uc.Links.FindForUser(ctx, userID, linkID)
	if err != nil {
		return nil, toAppError("find the link", err)
	}

	return link, nil
}

// Accept links the customer on the user's say-so.
func (uc *LinkUseCase) Accept(ctx context.Context, userID, linkID string) (*domain.UserLink, error) {
	link, err := uc.waiting(ctx, userID, linkID)
	if err != nil {
		return nil, err
	}

	before := linkAudit(link)

	if err := uc.markLinked(ctx, link, domain.ChannelApp); err != nil {
		return nil, err
	}

	if err := uc.audit(ctx, userID, link, ActionLinkConfirmed, before, linkAudit(link)); err != nil {
		return nil, err
	}

	return link, nil
}

// Decline turns a waiting request down.
func (uc *LinkUseCase) Decline(ctx context.Context, userID, linkID string) (*domain.UserLink, error) {
	link, err := uc.waiting(ctx, userID, linkID)
	if err != nil {
		return nil, err
	}

	if err := uc.end(ctx, link, domain.LinkDeclined, userID, ActionLinkAnswered); err != nil {
		return nil, err
	}

	return link, nil
}

// UnlinkByUser ends an established link from the user's side.
func (uc *LinkUseCase) UnlinkByUser(ctx context.Context, userID, linkID string) (*domain.UserLink, error) {
	link, err := uc.Links.FindForUser(ctx, userID, linkID)
	if err != nil {
		return nil, toAppError("unlink", err)
	}

	if link.Status != domain.LinkLinked {
		return nil, toAppError("unlink", domain.ErrLinkNotOpen)
	}

	if err := uc.end(ctx, link, domain.LinkUnlinked, userID, ActionLinkEnded); err != nil {
		return nil, err
	}

	return link, nil
}

// AutoLink links the customer records whose e-mail the signed-in user has
// verified (the Customer App's sign-in match), unless the customer already
// has a link with this user -- a refusal or an unlink is respected -- or is
// linked to someone else.
func (uc *LinkUseCase) AutoLink(ctx context.Context, userID, garageID, customerID string) error {
	had, err := uc.Links.HasAny(ctx, customerID, userID)
	if err != nil {
		return apperror.Internal("check the customer's links", err)
	}

	if had {
		return nil
	}

	now := uc.now().UTC()
	link := &domain.UserLink{
		ID: uuid.NewString(), GarageID: garageID, CustomerID: customerID, UserID: userID,
		Status: domain.LinkLinked, Channel: domain.ChannelAutoEmail, RequestedAt: now, LinkedAt: &now,
	}

	err = uc.Links.Create(ctx, link)
	if errors.Is(err, domain.ErrAlreadyLinked) {
		return nil
	}

	if err != nil {
		return apperror.Internal("link the customer", err)
	}

	return uc.audit(ctx, userID, link, ActionLinkConfirmed, nil, linkAudit(link))
}

// waiting returns the user's link when it still waits for an answer.
func (uc *LinkUseCase) waiting(ctx context.Context, userID, linkID string) (*domain.UserLink, error) {
	link, err := uc.Links.FindForUser(ctx, userID, linkID)
	if err != nil {
		return nil, toAppError("answer link", err)
	}

	if link.Status != domain.LinkRequested {
		return nil, toAppError("answer link", domain.ErrLinkNotOpen)
	}

	return link, nil
}

func (uc *LinkUseCase) markLinked(ctx context.Context, link *domain.UserLink, channel domain.LinkChannel) error {
	now := uc.now().UTC()
	link.Status, link.Channel, link.LinkedAt = domain.LinkLinked, channel, &now

	if err := uc.Links.Update(ctx, link); err != nil {
		return toAppError("link customer", err)
	}

	return nil
}

func (uc *LinkUseCase) end(ctx context.Context, link *domain.UserLink, status domain.LinkStatus, actorID, action string) error {
	before := linkAudit(link)
	now := uc.now().UTC()
	link.Status, link.EndedAt = status, &now

	if err := uc.Links.Update(ctx, link); err != nil {
		return toAppError("end link", err)
	}

	return uc.audit(ctx, actorID, link, action, before, linkAudit(link))
}

// audit records a link change; the user behind a link is never written, only
// who acted.
func (uc *LinkUseCase) audit(ctx context.Context, actorID string, link *domain.UserLink, action string, before, after any) error {
	err := uc.Audit.Record(ctx, audit.Entry{
		Actor: actorID, GarageID: link.GarageID, Action: action, EntityType: entityLink, EntityID: link.ID,
		Before: before, After: after, IP: middleware.ClientIP(ctx),
	})
	if err != nil {
		return toAppError("audit "+action, err)
	}

	return nil
}

func linkAudit(l *domain.UserLink) map[string]any {
	return map[string]any{"customer_id": l.CustomerID, "status": l.Status, "channel": l.Channel}
}
