// Package usecase implements read-only support access (Release 8.2,
// FR-IAM-11): admin staff request a grant for one garage, the garage owner
// approves or revokes it, and a request to read the garage's data is allowed
// while the grant is active. Every read is logged for the owner and in the
// audit log.
package usecase

import (
	"context"
	"errors"
	"log/slog"
	"path"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/platform/access"
	"github.com/viettechno/vietgara-backend/internal/platform/audit"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platform/mail"
	"github.com/viettechno/vietgara-backend/internal/support/domain"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// Repository stores grants and the access log.
type Repository interface {
	Create(ctx context.Context, g *domain.Grant) error
	Update(ctx context.Context, g *domain.Grant) error
	Find(ctx context.Context, id string) (*domain.Grant, error)
	ListByStaff(ctx context.Context, staffUserID string) ([]*domain.Grant, error)
	ListByGarage(ctx context.Context, garageID string) ([]*domain.Grant, error)
	// HasOpen reports a pending or active grant of the staff member for the garage.
	HasOpen(ctx context.Context, garageID, staffUserID string, now time.Time) (bool, error)
	// ActiveGrant returns domain.ErrGrantNotFound when the staff member may not read the garage now.
	ActiveGrant(ctx context.Context, garageID, staffUserID string, now time.Time) (grantID, ownerUserID string, err error)
	// Garage returns domain.ErrGarageNotFound.
	Garage(ctx context.Context, garageID string) (*domain.Garage, error)
	Directory(ctx context.Context, query string, page domain.Page) ([]*domain.DirectoryEntry, int, error)
	InsertEvent(ctx context.Context, e *domain.AccessEvent) error
	ListEvents(ctx context.Context, garageID, grantID string, page domain.Page) ([]*domain.AccessEvent, int, error)
	ListOutbox(ctx context.Context, garageID, status string, page domain.Page) ([]*domain.OutboxEvent, int, error)
	ListNotifications(ctx context.Context, garageID string, page domain.Page) ([]*domain.Notification, int, error)
	// SetReadOnly makes the rest of the request's transaction read-only.
	SetReadOnly(ctx context.Context) error
}

// AuditLogger records changes in the audit log, in the request's
// transaction.
type AuditLogger interface {
	Record(ctx context.Context, entry audit.Entry) error
}

// Deps are the dependencies of UseCase.
type Deps struct {
	Repo     Repository
	Platform access.PlatformReader
	Audit    AuditLogger
	Mailer   mail.Sender
	// Tx scopes the diagnostic queries to the garage of a grant.
	Tx middleware.Transactor
	// OwnerBaseURL is the owner web, used in e-mailed links.
	OwnerBaseURL string
	// Now defaults to time.Now.
	Now func() time.Time
}

// UseCase implements every action of the module. It is also the
// middleware.SupportSessions of the request pipeline.
type UseCase struct {
	Deps
}

var _ middleware.SupportSessions = (*UseCase)(nil)

// New wires the use case.
func New(deps Deps) *UseCase {
	if deps.Now == nil {
		deps.Now = time.Now
	}

	return &UseCase{Deps: deps}
}

func (uc *UseCase) now() time.Time { return uc.Now().UTC() }

// audit records an action of the caller on a garage.
func (uc *UseCase) audit(ctx context.Context, garageID, action, entityType, entityID string, after any) error {
	return uc.Audit.Record(ctx, audit.Entry{
		Actor: middleware.UserID(ctx), GarageID: garageID, Action: action, EntityType: entityType, EntityID: entityID,
		After: after, IP: middleware.ClientIP(ctx),
	})
}

// toAppError classifies err into an *apperror.AppError; op names the failed
// operation for unexpected (internal) errors.
func toAppError(op string, err error) error {
	if _, ok := apperror.As(err); ok {
		return err
	}

	type mapping struct {
		target error
		code   apperror.Code
		reason string
	}

	for _, m := range []mapping{
		{domain.ErrGrantNotFound, apperror.CodeNotFound, ""},
		{domain.ErrGarageNotFound, apperror.CodeNotFound, ""},
		{domain.ErrGrantNotPending, apperror.CodeFailedPrecondition, domain.ReasonGrantNotPending},
		{domain.ErrGrantNotOpen, apperror.CodeFailedPrecondition, domain.ReasonGrantNotOpen},
		{domain.ErrGrantNotActive, apperror.CodeFailedPrecondition, domain.ReasonGrantNotActive},
		{domain.ErrGrantOpen, apperror.CodeAlreadyExists, domain.ReasonGrantOpen},
	} {
		if errors.Is(err, m.target) {
			return apperror.Wrap(m.code, m.target.Error(), err).WithReason(m.reason)
		}
	}

	return apperror.Internal(op, err)
}

// --- the grant lifecycle ---

// Request asks for read-only access to a garage. A Platform Admin's grant is
// active at once and the owner is told; any other staff member waits for the
// owner's approval, which the owner is asked for by e-mail.
func (uc *UseCase) Request(ctx context.Context, garageID, reason string, ttlHours int) (*domain.Grant, error) {
	reason = strings.TrimSpace(reason)

	switch {
	case reason == "":
		return nil, apperror.Validation("reason", apperror.IssueRequired, domain.ErrReasonRequired.Error())
	case utf8.RuneCountInString(reason) > domain.MaxReasonLength:
		return nil, apperror.Validation("reason", apperror.IssueTooLong, domain.ErrReasonTooLong.Error())
	}

	ttl := domain.DefaultTTL
	if ttlHours != 0 {
		ttl = time.Duration(ttlHours) * time.Hour
	}

	if !domain.ValidTTL(ttl) {
		return nil, apperror.Validation("ttlHours", apperror.IssueInvalid, domain.ErrInvalidTTL.Error())
	}

	garage, err := uc.Repo.Garage(ctx, garageID)
	if err != nil {
		return nil, toAppError("find garage", err)
	}

	userID, now := middleware.UserID(ctx), uc.now()

	open, err := uc.Repo.HasOpen(ctx, garageID, userID, now)
	if err != nil {
		return nil, toAppError("check open grants", err)
	}

	if open {
		return nil, toAppError("request support grant", domain.ErrGrantOpen)
	}

	acc, _ := access.PlatformFromContext(ctx)
	admin := acc != nil && acc.Admin

	grant := domain.NewGrant(garageID, userID, reason, ttl, admin, now)
	if err := uc.Repo.Create(ctx, grant); err != nil {
		return nil, toAppError("create support grant", err)
	}

	created, err := uc.Repo.Find(ctx, grant.ID)
	if err != nil {
		return nil, toAppError("get support grant", err)
	}

	action, template := "support_grant.requested", mail.TemplateSupportGrantRequested
	if admin {
		action, template = "support_grant.opened", mail.TemplateSupportGrantOpened
	}

	if err := uc.audit(ctx, garageID, action, "support_grant", created.ID, created); err != nil {
		return nil, toAppError("audit support grant", err)
	}

	platformdb.AfterCommit(ctx, func() { uc.notifyOwner(context.WithoutCancel(ctx), template, garage, created) })

	return created, nil
}

// ListMine returns the caller's own grants, optionally of one status.
func (uc *UseCase) ListMine(ctx context.Context, status domain.Status) ([]*domain.Grant, error) {
	grants, err := uc.Repo.ListByStaff(ctx, middleware.UserID(ctx))
	if err != nil {
		return nil, toAppError("list support grants", err)
	}

	return uc.filter(grants, status), nil
}

// End closes the caller's own pending or active grant.
func (uc *UseCase) End(ctx context.Context, grantID string) (*domain.Grant, error) {
	grant, err := uc.Repo.Find(ctx, grantID)
	if err != nil {
		return nil, toAppError("get support grant", err)
	}

	if grant.StaffUserID != middleware.UserID(ctx) {
		return nil, toAppError("get support grant", domain.ErrGrantNotFound)
	}

	return uc.revoke(ctx, grant, "support_grant.ended")
}

// ListForGarage returns the grants of the garage in the request's scope.
func (uc *UseCase) ListForGarage(ctx context.Context, status domain.Status) ([]*domain.Grant, error) {
	grants, err := uc.Repo.ListByGarage(ctx, garageID(ctx))
	if err != nil {
		return nil, toAppError("list support grants", err)
	}

	return uc.filter(grants, status), nil
}

// Approve is the owner's yes to a pending request: the session starts now.
func (uc *UseCase) Approve(ctx context.Context, grantID string) (*domain.Grant, error) {
	grant, err := uc.garageGrant(ctx, grantID)
	if err != nil {
		return nil, err
	}

	if err := grant.Approve(middleware.UserID(ctx), uc.now()); err != nil {
		return nil, toAppError("approve support grant", err)
	}

	if err := uc.Repo.Update(ctx, grant); err != nil {
		return nil, toAppError("approve support grant", err)
	}

	if err := uc.audit(ctx, grant.GarageID, "support_grant.approved", "support_grant", grant.ID, grant); err != nil {
		return nil, toAppError("audit support grant", err)
	}

	platformdb.AfterCommit(ctx, func() { uc.notifyStaff(context.WithoutCancel(ctx), grant, true) })

	return uc.Repo.Find(ctx, grant.ID)
}

// Revoke is the owner ending an active grant at once, or declining a
// pending request.
func (uc *UseCase) Revoke(ctx context.Context, grantID string) (*domain.Grant, error) {
	grant, err := uc.garageGrant(ctx, grantID)
	if err != nil {
		return nil, err
	}

	revoked, err := uc.revoke(ctx, grant, "support_grant.revoked")
	if err != nil {
		return nil, err
	}

	platformdb.AfterCommit(ctx, func() { uc.notifyStaff(context.WithoutCancel(ctx), revoked, false) })

	return revoked, nil
}

func (uc *UseCase) revoke(ctx context.Context, grant *domain.Grant, action string) (*domain.Grant, error) {
	if err := grant.Revoke(uc.now()); err != nil {
		return nil, toAppError("revoke support grant", err)
	}

	if err := uc.Repo.Update(ctx, grant); err != nil {
		return nil, toAppError("revoke support grant", err)
	}

	if err := uc.audit(ctx, grant.GarageID, action, "support_grant", grant.ID, grant); err != nil {
		return nil, toAppError("audit support grant", err)
	}

	return grant, nil
}

// garageGrant loads a grant of the garage in the request's scope; another
// garage's grant is reported as not found.
func (uc *UseCase) garageGrant(ctx context.Context, grantID string) (*domain.Grant, error) {
	grant, err := uc.Repo.Find(ctx, grantID)
	if err != nil {
		return nil, toAppError("get support grant", err)
	}

	if grant.GarageID != garageID(ctx) {
		return nil, toAppError("get support grant", domain.ErrGrantNotFound)
	}

	return grant, nil
}

// garageID is the garage of an owner endpoint, authorized by the interceptor.
func garageID(ctx context.Context) string {
	if a, ok := access.FromContext(ctx); ok {
		return a.GarageID
	}

	return ""
}

func (uc *UseCase) filter(grants []*domain.Grant, status domain.Status) []*domain.Grant {
	now := uc.now()
	out := make([]*domain.Grant, 0, len(grants))

	for _, g := range grants {
		if status == "" || g.Status(now) == status {
			out = append(out, g)
		}
	}

	return out
}

// GrantStatus is the status of g now.
func (uc *UseCase) GrantStatus(g *domain.Grant) domain.Status { return g.Status(uc.now()) }

// --- what the owner and the staff see ---

// AccessLog returns the reads made under the garage's grants, newest first.
func (uc *UseCase) AccessLog(ctx context.Context, grantID string, page domain.Page) ([]*domain.AccessEvent, int, error) {
	events, total, err := uc.Repo.ListEvents(ctx, garageID(ctx), grantID, page)
	if err != nil {
		return nil, 0, toAppError("list support access log", err)
	}

	return events, total, nil
}

// Directory searches garages by name and owner.
func (uc *UseCase) Directory(ctx context.Context, query string, page domain.Page) ([]*domain.DirectoryEntry, int, error) {
	entries, total, err := uc.Repo.Directory(ctx, query, page)
	if err != nil {
		return nil, 0, toAppError("search garages", err)
	}

	return entries, total, nil
}

// OutboxEvents shows the delivery status of the grant's garage's events.
func (uc *UseCase) OutboxEvents(ctx context.Context, grantID, status string, page domain.Page) (events []*domain.OutboxEvent, total int, err error) {
	err = uc.diagnose(ctx, grantID, "ListSupportOutboxEvents", "outbox_events", func(ctx context.Context, garageID string) error {
		var err error

		events, total, err = uc.Repo.ListOutbox(ctx, garageID, status, page)

		return err
	})

	return events, total, err
}

// Notifications shows the notification log of the grant's garage's people.
func (uc *UseCase) Notifications(ctx context.Context, grantID string, page domain.Page) (notifications []*domain.Notification, total int, err error) {
	err = uc.diagnose(ctx, grantID, "ListSupportNotifications", "notification_log", func(ctx context.Context, garageID string) error {
		var err error

		notifications, total, err = uc.Repo.ListNotifications(ctx, garageID, page)

		return err
	})

	return notifications, total, err
}

// diagnose runs a diagnostic view of the garage of the caller's active
// grant, in that garage's scope, and logs the read like any other.
func (uc *UseCase) diagnose(ctx context.Context, grantID, method, entityType string, fn func(ctx context.Context, garageID string) error) error {
	grant, err := uc.Repo.Find(ctx, grantID)
	if err != nil {
		return toAppError("get support grant", err)
	}

	userID := middleware.UserID(ctx)
	if grant.StaffUserID != userID {
		return toAppError("get support grant", domain.ErrGrantNotFound)
	}

	if grant.Status(uc.now()) != domain.StatusActive {
		return toAppError("use support grant", domain.ErrGrantNotActive)
	}

	scope := platformdb.Scope{GarageID: grant.GarageID, UserID: userID}

	err = uc.Tx.WithTransaction(ctx, scope, func(ctx context.Context) error {
		if err := uc.logRead(ctx, grant.ID, grant.GarageID, userID, method, entityType, ""); err != nil {
			return err
		}

		return fn(ctx, grant.GarageID)
	})
	if err != nil {
		return toAppError("run diagnostic view", err)
	}

	return nil
}

// --- the support session (middleware.SupportSessions) ---

// SupportAccess is the read-only access of a staff member to a garage under
// an active grant. It asks for the platform permission on every request, so
// removing the user from their group ends the session at once.
func (uc *UseCase) SupportAccess(ctx context.Context, garageID, userID string) (*access.Access, error) {
	grantID, ownerID, err := uc.Repo.ActiveGrant(ctx, garageID, userID, uc.now())
	if errors.Is(err, domain.ErrGrantNotFound) {
		return nil, access.ErrNoMembership
	}

	if err != nil {
		return nil, err
	}

	platform, err := uc.Platform.PlatformAccess(ctx, userID)
	if err != nil {
		return nil, err
	}

	if !platform.Has(access.PlatformSupportWrite) {
		return nil, access.ErrNoMembership
	}

	return &access.Access{
		GarageID: garageID, UserID: userID, OwnerUserID: ownerID, Active: true,
		Permissions: access.ReadOnly(), SupportGrantID: grantID,
	}, nil
}

// BeginRead logs the read and makes the request's transaction read-only.
func (uc *UseCase) BeginRead(ctx context.Context, a *access.Access, method, entityType, entityID string) error {
	if err := uc.logRead(ctx, a.SupportGrantID, a.GarageID, a.UserID, method, entityType, entityID); err != nil {
		return err
	}

	return uc.Repo.SetReadOnly(ctx)
}

// logRead appends one read to the owner's log and to the audit log. It
// commits on its own, before the read runs: a read that finds nothing or
// fails is still on record, so ids cannot be probed unseen.
func (uc *UseCase) logRead(ctx context.Context, grantID, garageID, userID, method, entityType, entityID string) error {
	event := &domain.AccessEvent{
		GrantID: grantID, GarageID: garageID, StaffUserID: userID,
		Method: path.Base(method), EntityType: entityType, EntityID: entityID,
	}
	scope := platformdb.Scope{GarageID: garageID, UserID: userID}

	return uc.Tx.WithTransaction(platformdb.Detached(ctx), scope, func(ctx context.Context) error {
		if err := uc.Repo.InsertEvent(ctx, event); err != nil {
			return err
		}

		return uc.audit(ctx, garageID, "support.read", entityType, entityID, map[string]string{"grantId": grantID, "method": event.Method})
	})
}

// --- e-mail ---

func (uc *UseCase) notifyOwner(ctx context.Context, template string, garage *domain.Garage, g *domain.Grant) {
	data := map[string]any{
		"Name": garage.OwnerName, "GarageName": garage.Name, "StaffName": g.StaffName, "StaffEmail": g.StaffEmail,
		"Reason": g.Reason, "Hours": int(g.TTL.Hours()),
		"ReviewURL": strings.TrimSuffix(uc.OwnerBaseURL, "/") + "/support-access?garageId=" + g.GarageID,
		"ExpiresAt": "",
	}
	if g.ExpiresAt != nil {
		data["ExpiresAt"] = formatTime(*g.ExpiresAt, garage.OwnerLocale)
	}

	uc.send(ctx, template, garage.OwnerLocale, garage.OwnerEmail, garage.OwnerName, data)
}

func (uc *UseCase) notifyStaff(ctx context.Context, g *domain.Grant, approved bool) {
	data := map[string]any{
		"Name": g.StaffName, "GarageName": g.GarageName, "Approved": approved, "ExpiresAt": "",
	}
	if approved && g.ExpiresAt != nil {
		data["ExpiresAt"] = formatTime(*g.ExpiresAt, g.StaffLocale)
	}

	uc.send(ctx, mail.TemplateSupportGrantAnswered, g.StaffLocale, g.StaffEmail, g.StaffName, data)
}

func (uc *UseCase) send(ctx context.Context, template, locale, email, name string, data map[string]any) {
	msg, err := mail.Render(template, locale, email, name, data)
	if err == nil {
		err = uc.Mailer.Send(ctx, msg)
	}

	if err != nil {
		slog.Warn("support: sending the e-mail failed", "template", template, "error", err)
	}
}

func formatTime(t time.Time, locale string) string {
	if locale == mail.LocaleEN {
		return t.UTC().Format("Jan 2, 2006 15:04 UTC")
	}

	return t.UTC().Format("02/01/2006 15:04 UTC")
}
