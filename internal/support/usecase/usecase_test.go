package usecase_test

import (
	"context"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/platform/access"
	"github.com/viettechno/vietgara-backend/internal/platform/auth"
	"github.com/viettechno/vietgara-backend/internal/support/domain"
	"github.com/viettechno/vietgara-backend/internal/support/supporttest"
	"github.com/viettechno/vietgara-backend/internal/support/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

const (
	adminID  = "u-admin"
	staffID  = "u-staff"
	ownerID  = "u-owner"
	garageID = "g-1"
)

type env struct {
	uc       *usecase.UseCase
	repo     *supporttest.Repo
	audit    *supporttest.Audit
	mailer   *supporttest.Mailer
	platform *supporttest.Platform
	tx       *supporttest.Tx
	now      time.Time
}

func newEnv(t *testing.T) *env {
	t.Helper()

	e := &env{
		repo: supporttest.NewRepo(), audit: &supporttest.Audit{}, mailer: &supporttest.Mailer{}, tx: &supporttest.Tx{},
		now: time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC),
		platform: &supporttest.Platform{Access: map[string]*access.PlatformAccess{
			adminID: {UserID: adminID, Admin: true},
			staffID: {UserID: staffID, Permissions: []access.PlatformPermission{access.PlatformSupportWrite}},
		}},
	}
	e.repo.Garages[garageID] = &domain.Garage{
		ID: garageID, Name: "Gara One", OwnerUserID: ownerID, OwnerName: "Owner", OwnerEmail: "owner@x.test", OwnerLocale: "vi",
	}
	e.uc = usecase.New(usecase.Deps{
		Repo: e.repo, Platform: e.platform, Audit: e.audit, Mailer: e.mailer, Tx: e.tx, OwnerBaseURL: "https://app.test/",
		Now: func() time.Time { return e.now },
	})

	return e
}

func (e *env) as(userID string) context.Context {
	ctx := middleware.WithClaims(context.Background(), &auth.Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: userID}})
	acc, _ := e.platform.PlatformAccess(ctx, userID)

	return access.WithPlatformAccess(ctx, acc)
}

// asOwner is the owner of garageID, as the Garage interceptor leaves the context.
func (e *env) asOwner() context.Context {
	return access.WithAccess(e.as(ownerID), &access.Access{GarageID: garageID, UserID: ownerID, Owner: true, Active: true})
}

func requireCode(t *testing.T, err error, code codes.Code, reason string) {
	t.Helper()

	appErr, ok := apperror.As(err)
	require.True(t, ok, "not an app error: %v", err)
	assert.Equal(t, code, status.Code(apperror.ToGRPCError(err)))
	assert.Equal(t, reason, appErr.ReasonOrDefault())
}

func TestRequest_StaffWaitsForTheOwner(t *testing.T) {
	t.Parallel()

	e := newEnv(t)

	grant, err := e.uc.Request(e.as(staffID), garageID, "  ticket 42 ", 0)
	require.NoError(t, err)

	assert.Equal(t, "ticket 42", grant.Reason)
	assert.Equal(t, domain.DefaultTTL, grant.TTL)
	assert.Equal(t, domain.StatusPending, e.uc.GrantStatus(grant))
	assert.Nil(t, grant.ExpiresAt, "the session starts at the approval")
	assert.False(t, grant.GrantedByPlatformAdmin)
	assert.Equal(t, []string{"support_grant.requested"}, e.audit.Actions())
	assert.Equal(t, garageID, e.audit.Entries[0].GarageID)

	require.Len(t, e.mailer.Sent, 1, "the owner is asked to approve")
	assert.Equal(t, "owner@x.test", e.mailer.Sent[0].ToEmail)
	assert.Equal(t, "support_grant_requested", e.mailer.Sent[0].Tag)
	assert.Contains(t, e.mailer.Sent[0].Text, "https://app.test/support-access?garageId="+garageID)
	assert.Contains(t, e.mailer.Sent[0].Text, "ticket 42")
}

func TestRequest_PlatformAdminOpensAtOnceAndTheOwnerIsTold(t *testing.T) {
	t.Parallel()

	e := newEnv(t)

	grant, err := e.uc.Request(e.as(adminID), garageID, "incident 7", 48)
	require.NoError(t, err)

	assert.Equal(t, domain.StatusActive, e.uc.GrantStatus(grant))
	assert.True(t, grant.GrantedByPlatformAdmin)
	assert.Empty(t, grant.ApprovedBy, "nobody approved it")
	assert.Equal(t, e.now.Add(48*time.Hour), *grant.ExpiresAt)
	assert.Equal(t, []string{"support_grant.opened"}, e.audit.Actions())

	require.Len(t, e.mailer.Sent, 1)
	assert.Equal(t, "support_grant_opened", e.mailer.Sent[0].Tag)
	assert.Contains(t, e.mailer.Sent[0].Text, "incident 7")
}

func TestRequest_Validation(t *testing.T) {
	t.Parallel()

	long := make([]rune, domain.MaxReasonLength+1)
	for i := range long {
		long[i] = 'x'
	}

	tests := []struct {
		name     string
		garage   string
		reason   string
		ttlHours int
		code     codes.Code
		reasonOf string
	}{
		{"no reason", garageID, "  ", 0, codes.InvalidArgument, apperror.ReasonValidation},
		{"reason too long", garageID, string(long), 0, codes.InvalidArgument, apperror.ReasonValidation},
		{"under an hour", garageID, "r", -1, codes.InvalidArgument, apperror.ReasonValidation},
		{"over seven days", garageID, "r", 169, codes.InvalidArgument, apperror.ReasonValidation},
		{"unknown garage", "g-404", "r", 1, codes.NotFound, "NOT_FOUND"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			e := newEnv(t)

			_, err := e.uc.Request(e.as(staffID), tt.garage, tt.reason, tt.ttlHours)
			requireCode(t, err, tt.code, tt.reasonOf)
			assert.Empty(t, e.repo.Grants)
			assert.Empty(t, e.mailer.Sent)
		})
	}
}

func TestRequest_OneOpenGrantPerStaffAndGarage(t *testing.T) {
	t.Parallel()

	e := newEnv(t)

	first, err := e.uc.Request(e.as(staffID), garageID, "r", 1)
	require.NoError(t, err)

	_, err = e.uc.Request(e.as(staffID), garageID, "again", 1)
	requireCode(t, err, codes.AlreadyExists, domain.ReasonGrantOpen)

	_, err = e.uc.Revoke(e.asOwner(), first.ID)
	require.NoError(t, err)

	_, err = e.uc.Request(e.as(staffID), garageID, "after the decline", 1)
	require.NoError(t, err, "a closed grant does not block a new request")
}

func TestOwnerApprovesAndRevokes(t *testing.T) {
	t.Parallel()

	e := newEnv(t)

	grant, err := e.uc.Request(e.as(staffID), garageID, "r", 2)
	require.NoError(t, err)

	approved, err := e.uc.Approve(e.asOwner(), grant.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.StatusActive, e.uc.GrantStatus(approved))
	assert.Equal(t, ownerID, approved.ApprovedBy)
	assert.Equal(t, e.now.Add(2*time.Hour), *approved.ExpiresAt, "counted from the approval")

	require.Len(t, e.mailer.Sent, 2)
	assert.Equal(t, "support_grant_answered", e.mailer.Sent[1].Tag)
	assert.Equal(t, staffID+"@x.test", e.mailer.Sent[1].ToEmail)
	assert.Contains(t, e.mailer.Sent[1].Subject, "approved")

	_, err = e.uc.Approve(e.asOwner(), grant.ID)
	requireCode(t, err, codes.FailedPrecondition, domain.ReasonGrantNotPending)

	revoked, err := e.uc.Revoke(e.asOwner(), grant.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.StatusRevoked, e.uc.GrantStatus(revoked))
	assert.Contains(t, e.mailer.Sent[2].Subject, "declined")

	_, err = e.uc.Revoke(e.asOwner(), grant.ID)
	requireCode(t, err, codes.FailedPrecondition, domain.ReasonGrantNotOpen)

	assert.Equal(t, []string{"support_grant.requested", "support_grant.approved", "support_grant.revoked"}, e.audit.Actions())
}

func TestOwnerCannotAnswerAnotherGaragesGrant(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.repo.Garages["g-2"] = &domain.Garage{ID: "g-2", Name: "Gara Two", OwnerUserID: "u-other"}

	grant, err := e.uc.Request(e.as(staffID), "g-2", "r", 1)
	require.NoError(t, err)

	_, err = e.uc.Approve(e.asOwner(), grant.ID)
	requireCode(t, err, codes.NotFound, "NOT_FOUND")

	_, err = e.uc.Revoke(e.asOwner(), grant.ID)
	requireCode(t, err, codes.NotFound, "NOT_FOUND")

	list, err := e.uc.ListForGarage(e.asOwner(), "")
	require.NoError(t, err)
	assert.Empty(t, list)
}

func TestRequestLapsesAndSessionExpires(t *testing.T) {
	t.Parallel()

	e := newEnv(t)

	grant, err := e.uc.Request(e.as(staffID), garageID, "r", 1)
	require.NoError(t, err)

	e.now = e.now.Add(domain.RequestLifetime + time.Second)
	_, err = e.uc.Approve(e.asOwner(), grant.ID)
	requireCode(t, err, codes.FailedPrecondition, domain.ReasonGrantNotPending)

	expired, err := e.uc.ListMine(e.as(staffID), domain.StatusExpired)
	require.NoError(t, err)
	assert.Len(t, expired, 1)

	pending, err := e.uc.ListMine(e.as(staffID), domain.StatusPending)
	require.NoError(t, err)
	assert.Empty(t, pending)
}

func TestStaffEndsOnlyTheirOwnGrant(t *testing.T) {
	t.Parallel()

	e := newEnv(t)

	grant, err := e.uc.Request(e.as(adminID), garageID, "r", 1)
	require.NoError(t, err)

	_, err = e.uc.End(e.as(staffID), grant.ID)
	requireCode(t, err, codes.NotFound, "NOT_FOUND")

	ended, err := e.uc.End(e.as(adminID), grant.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.StatusRevoked, e.uc.GrantStatus(ended))
	assert.Equal(t, "support_grant.ended", e.audit.Actions()[1])

	_, err = e.uc.End(e.as(adminID), "g-404")
	requireCode(t, err, codes.NotFound, "NOT_FOUND")
}

func TestSupportSession(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	ctx := e.as(staffID)

	_, err := e.uc.SupportAccess(ctx, garageID, staffID)
	require.ErrorIs(t, err, access.ErrNoMembership, "no grant")

	grant, err := e.uc.Request(ctx, garageID, "r", 1)
	require.NoError(t, err)

	_, err = e.uc.SupportAccess(ctx, garageID, staffID)
	require.ErrorIs(t, err, access.ErrNoMembership, "pending is not a session")

	_, err = e.uc.Approve(e.asOwner(), grant.ID)
	require.NoError(t, err)

	got, err := e.uc.SupportAccess(ctx, garageID, staffID)
	require.NoError(t, err)
	assert.Equal(t, grant.ID, got.SupportGrantID)
	assert.Equal(t, ownerID, got.OwnerUserID)
	assert.False(t, got.Owner)
	assert.True(t, got.Has(access.QuoteRead))
	assert.False(t, got.Has(access.QuoteWrite), "read-only")
	assert.False(t, got.Has(access.StaffWrite))

	e.platform.Access[staffID] = &access.PlatformAccess{UserID: staffID}
	_, err = e.uc.SupportAccess(ctx, garageID, staffID)
	require.ErrorIs(t, err, access.ErrNoMembership, "removed from the group: the session ends at once")

	e.platform.Access[staffID] = &access.PlatformAccess{UserID: staffID, Permissions: []access.PlatformPermission{access.PlatformSupportWrite}}
	e.now = e.now.Add(2 * time.Hour)
	_, err = e.uc.SupportAccess(ctx, garageID, staffID)
	require.ErrorIs(t, err, access.ErrNoMembership, "expired")

	e.platform.Err = supporttest.ErrFake
	e.now = e.now.Add(-2 * time.Hour)
	_, err = e.uc.SupportAccess(ctx, garageID, staffID)
	require.ErrorIs(t, err, supporttest.ErrFake)
}

func TestBeginReadLogsThenLocksTheTransaction(t *testing.T) {
	t.Parallel()

	e := newEnv(t)

	grant, err := e.uc.Request(e.as(adminID), garageID, "r", 1)
	require.NoError(t, err)

	session := &access.Access{GarageID: garageID, UserID: adminID, SupportGrantID: grant.ID}
	require.NoError(t, e.uc.BeginRead(e.as(adminID), session, "/vietgara.billing.v1.SettlementService/GetSettlement", "settlement", "s-1"))

	require.Len(t, e.repo.Events, 1)
	assert.Equal(t, domain.AccessEvent{
		ID: e.repo.Events[0].ID, GrantID: grant.ID, GarageID: garageID, StaffUserID: adminID,
		Method: "GetSettlement", EntityType: "settlement", EntityID: "s-1",
	}, *e.repo.Events[0])
	assert.True(t, e.repo.ReadOnly)

	last := e.audit.Entries[len(e.audit.Entries)-1]
	assert.Equal(t, "support.read", last.Action)
	assert.Equal(t, "settlement", last.EntityType)
	assert.Equal(t, "s-1", last.EntityID)
	assert.Equal(t, garageID, last.GarageID)

	log, total, err := e.uc.AccessLog(e.asOwner(), grant.ID, domain.Page{Page: 1, PageSize: 10})
	require.NoError(t, err)
	assert.Equal(t, 1, total)
	assert.Len(t, log, 1)

	e.repo.FailOn = "InsertEvent"
	e.repo.ReadOnly = false
	require.Error(t, e.uc.BeginRead(e.as(adminID), session, "GetSettlement", "settlement", "s-1"))
	assert.False(t, e.repo.ReadOnly, "an unlogged read is never started")

	e.repo.FailOn, e.audit.Err = "", supporttest.ErrFake
	require.ErrorIs(t, e.uc.BeginRead(e.as(adminID), session, "GetSettlement", "settlement", "s-1"), supporttest.ErrFake)
}

func TestDiagnosticViews(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.repo.Outbox = []*domain.OutboxEvent{{ID: "o-1", EventType: "quote.approved", Status: "failed", Attempts: 3}}
	e.repo.Notified = []*domain.Notification{{ID: "n-1", Template: "password_reset", Status: "sent"}}

	grant, err := e.uc.Request(e.as(staffID), garageID, "r", 1)
	require.NoError(t, err)

	page := domain.Page{Page: 1, PageSize: 10}

	_, _, err = e.uc.OutboxEvents(e.as(staffID), grant.ID, "", page)
	requireCode(t, err, codes.FailedPrecondition, domain.ReasonGrantNotActive)

	_, err = e.uc.Approve(e.asOwner(), grant.ID)
	require.NoError(t, err)

	events, total, err := e.uc.OutboxEvents(e.as(staffID), grant.ID, "failed", page)
	require.NoError(t, err)
	assert.Equal(t, 1, total)
	assert.Equal(t, "o-1", events[0].ID)

	notifications, total, err := e.uc.Notifications(e.as(staffID), grant.ID, page)
	require.NoError(t, err)
	assert.Equal(t, 1, total)
	assert.Equal(t, "n-1", notifications[0].ID)

	assert.Equal(t, []string{"ListSupportOutboxEvents", "ListSupportNotifications"}, []string{e.repo.Events[0].Method, e.repo.Events[1].Method})
	assert.Equal(t, "outbox_events", e.repo.Events[0].EntityType)

	for _, scope := range e.tx.Scopes {
		assert.Equal(t, garageID, scope.GarageID, "diagnostics run in the garage's scope")
		assert.Equal(t, staffID, scope.UserID)
	}

	_, _, err = e.uc.OutboxEvents(e.as(adminID), grant.ID, "", page)
	requireCode(t, err, codes.NotFound, "NOT_FOUND")

	e.tx.Err = supporttest.ErrFake
	_, _, err = e.uc.Notifications(e.as(staffID), grant.ID, page)
	requireCode(t, err, codes.Internal, "INTERNAL")
}

func TestDirectoryAndFailures(t *testing.T) {
	t.Parallel()

	e := newEnv(t)

	entries, total, err := e.uc.Directory(e.as(staffID), "gara", domain.Page{Page: 1, PageSize: 10})
	require.NoError(t, err)
	assert.Equal(t, 1, total)
	assert.Equal(t, "owner@x.test", entries[0].OwnerEmail)

	for _, method := range []string{"Directory", "Garage", "HasOpen", "Create", "Find", "ListByStaff", "ListByGarage", "ListEvents", "Update"} {
		e.repo.FailOn = method
		ctx := e.as(staffID)

		var err error

		switch method {
		case "Directory":
			_, _, err = e.uc.Directory(ctx, "", domain.Page{Page: 1, PageSize: 1})
		case "ListByStaff":
			_, err = e.uc.ListMine(ctx, "")
		case "ListByGarage":
			_, err = e.uc.ListForGarage(e.asOwner(), "")
		case "ListEvents":
			_, _, err = e.uc.AccessLog(e.asOwner(), "", domain.Page{Page: 1, PageSize: 1})
		case "Update":
			e.repo.FailOn = ""
			grant, reqErr := e.uc.Request(ctx, "g-1", "r", 1)
			require.NoError(t, reqErr)

			e.repo.FailOn = method
			_, err = e.uc.Approve(e.asOwner(), grant.ID)
		default:
			_, err = e.uc.Request(ctx, garageID, "r", 1)
		}

		requireCode(t, err, codes.Internal, "INTERNAL")

		e.repo.Grants = map[string]*domain.Grant{}
	}

	e.repo.FailOn = ""
	e.audit.Err = supporttest.ErrFake
	_, err = e.uc.Request(e.as(staffID), garageID, "r", 1)
	requireCode(t, err, codes.Internal, "INTERNAL")

	e.audit.Err, e.mailer.Err = nil, supporttest.ErrFake
	e.repo.Grants = map[string]*domain.Grant{} // the failed request rolled back
	_, err = e.uc.Request(e.as(staffID), garageID, "r", 1)
	require.NoError(t, err, "a failed e-mail never fails the request")
}
