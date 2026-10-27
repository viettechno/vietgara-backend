package handler_test

import (
	"context"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	supportv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/support/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/platform/access"
	"github.com/viettechno/vietgara-backend/internal/platform/auth"
	"github.com/viettechno/vietgara-backend/internal/support/domain"
	"github.com/viettechno/vietgara-backend/internal/support/handler"
	"github.com/viettechno/vietgara-backend/internal/support/supporttest"
	"github.com/viettechno/vietgara-backend/internal/support/usecase"
)

const garageID = "g-1"

type env struct {
	srv  *handler.Server
	repo *supporttest.Repo
}

func newEnv() *env {
	repo := supporttest.NewRepo()
	repo.Garages[garageID] = &domain.Garage{ID: garageID, Name: "Gara One", OwnerUserID: "u-owner", OwnerName: "Owner", OwnerEmail: "o@x.test", OwnerLocale: "en"}
	repo.Outbox = []*domain.OutboxEvent{{ID: "o-1", EventType: "x", Status: "failed", Attempts: 2}}
	repo.Notified = []*domain.Notification{{ID: "n-1", Template: "t", Status: "sent"}}

	uc := usecase.New(usecase.Deps{
		Repo: repo, Platform: &supporttest.Platform{}, Audit: &supporttest.Audit{}, Mailer: &supporttest.Mailer{},
		Tx: &supporttest.Tx{}, OwnerBaseURL: "https://app.test", Now: func() time.Time { return time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC) },
	})

	return &env{srv: handler.NewServer(uc), repo: repo}
}

func staffCtx(admin bool) context.Context {
	ctx := middleware.WithClaims(context.Background(), &auth.Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: "u-staff"}})

	return access.WithPlatformAccess(ctx, &access.PlatformAccess{UserID: "u-staff", Admin: admin})
}

func ownerCtx() context.Context {
	ctx := middleware.WithClaims(context.Background(), &auth.Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: "u-owner"}})

	return access.WithAccess(ctx, &access.Access{GarageID: garageID, UserID: "u-owner", Owner: true, Active: true})
}

func TestSupportFlow(t *testing.T) {
	t.Parallel()

	e := newEnv()
	staff, owner := staffCtx(false), ownerCtx()

	dir, err := e.srv.ListGarageDirectory(staff, &supportv1.ListGarageDirectoryRequest{Query: "gara", PageSize: 500})
	require.NoError(t, err)
	require.Len(t, dir.GetData(), 1)
	assert.EqualValues(t, 100, dir.GetPagination().GetPageSize(), "the page size is capped")

	created, err := e.srv.RequestSupportGrant(staff, &supportv1.RequestSupportGrantRequest{GarageId: garageID, Reason: "ticket", TtlHours: 3})
	require.NoError(t, err)

	grant := created.GetGrant()
	assert.Equal(t, supportv1.SupportGrantStatus_SUPPORT_GRANT_STATUS_PENDING, grant.GetStatus())
	assert.EqualValues(t, 3, grant.GetTtlHours())

	pending, err := e.srv.ListGarageSupportGrants(owner, &supportv1.ListGarageSupportGrantsRequest{Status: supportv1.SupportGrantStatus_SUPPORT_GRANT_STATUS_PENDING})
	require.NoError(t, err)
	assert.Len(t, pending.GetData(), 1)

	approved, err := e.srv.ApproveSupportGrant(owner, &supportv1.ApproveSupportGrantRequest{GarageId: garageID, GrantId: grant.GetId()})
	require.NoError(t, err)
	assert.Equal(t, supportv1.SupportGrantStatus_SUPPORT_GRANT_STATUS_ACTIVE, approved.GetGrant().GetStatus())

	mine, err := e.srv.ListSupportGrants(staff, &supportv1.ListSupportGrantsRequest{})
	require.NoError(t, err)
	assert.Len(t, mine.GetData(), 1)

	sessions, err := e.srv.ListMySupportSessions(staff, &supportv1.ListMySupportSessionsRequest{})
	require.NoError(t, err)
	assert.Len(t, sessions.GetData(), 1)

	outbox, err := e.srv.ListSupportOutboxEvents(staff, &supportv1.ListSupportOutboxEventsRequest{GrantId: grant.GetId()})
	require.NoError(t, err)
	assert.Equal(t, "failed", outbox.GetData()[0].GetStatus())

	notifications, err := e.srv.ListSupportNotifications(staff, &supportv1.ListSupportNotificationsRequest{GrantId: grant.GetId()})
	require.NoError(t, err)
	assert.Equal(t, "t", notifications.GetData()[0].GetTemplate())

	logged, err := e.srv.ListSupportAccessLog(owner, &supportv1.ListSupportAccessLogRequest{GarageId: garageID})
	require.NoError(t, err)
	assert.Len(t, logged.GetData(), 2)

	revoked, err := e.srv.RevokeSupportGrant(owner, &supportv1.RevokeSupportGrantRequest{GarageId: garageID, GrantId: grant.GetId()})
	require.NoError(t, err)
	assert.Equal(t, supportv1.SupportGrantStatus_SUPPORT_GRANT_STATUS_REVOKED, revoked.GetGrant().GetStatus())

	_, err = e.srv.EndSupportGrant(staff, &supportv1.EndSupportGrantRequest{GrantId: grant.GetId()})
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
}

func TestErrorsAreAPIErrors(t *testing.T) {
	t.Parallel()

	e := newEnv()
	staff, owner := staffCtx(true), ownerCtx()
	e.repo.FailOn = "ListEvents"

	_, err := e.srv.ListSupportAccessLog(owner, &supportv1.ListSupportAccessLogRequest{GarageId: garageID})
	assert.Equal(t, codes.Internal, status.Code(err))

	_, err = e.srv.RequestSupportGrant(staff, &supportv1.RequestSupportGrantRequest{GarageId: "nope", Reason: "r"})
	assert.Equal(t, codes.NotFound, status.Code(err))

	_, err = e.srv.ListMySupportSessions(staff, &supportv1.ListMySupportSessionsRequest{})
	require.NoError(t, err)

	e.repo.FailOn = "ListByStaff"

	for _, call := range []func() error{
		func() error {
			_, err := e.srv.ListSupportGrants(staff, &supportv1.ListSupportGrantsRequest{})
			return err
		},
		func() error {
			_, err := e.srv.ListMySupportSessions(staff, &supportv1.ListMySupportSessionsRequest{})
			return err
		},
	} {
		assert.Equal(t, codes.Internal, status.Code(call()))
	}

	e.repo.FailOn = "ListByGarage"
	_, err = e.srv.ListGarageSupportGrants(owner, &supportv1.ListGarageSupportGrantsRequest{})
	assert.Equal(t, codes.Internal, status.Code(err))

	e.repo.FailOn = "Directory"
	_, err = e.srv.ListGarageDirectory(staff, &supportv1.ListGarageDirectoryRequest{})
	assert.Equal(t, codes.Internal, status.Code(err))

	e.repo.FailOn = "Find"
	_, err = e.srv.ApproveSupportGrant(owner, &supportv1.ApproveSupportGrantRequest{GrantId: "x"})
	assert.Equal(t, codes.Internal, status.Code(err))
	_, err = e.srv.RevokeSupportGrant(owner, &supportv1.RevokeSupportGrantRequest{GrantId: "x"})
	assert.Equal(t, codes.Internal, status.Code(err))
	_, err = e.srv.EndSupportGrant(staff, &supportv1.EndSupportGrantRequest{GrantId: "x"})
	assert.Equal(t, codes.Internal, status.Code(err))
	_, err = e.srv.ListSupportOutboxEvents(staff, &supportv1.ListSupportOutboxEventsRequest{GrantId: "x"})
	assert.Equal(t, codes.Internal, status.Code(err))
	_, err = e.srv.ListSupportNotifications(staff, &supportv1.ListSupportNotificationsRequest{GrantId: "x"})
	assert.Equal(t, codes.Internal, status.Code(err))
}

func TestPoliciesNeedThePlatformOrOwnerRight(t *testing.T) {
	t.Parallel()

	policies := handler.Policies()
	assert.Equal(t, access.PlatformSupportWrite, policies[supportv1.SupportService_RequestSupportGrant_FullMethodName].Platform)
	assert.Equal(t, access.PlatformSupportRead, policies[supportv1.SupportService_ListSupportGrants_FullMethodName].Platform)
	assert.Equal(t, access.PlatformGarageDirectoryRead, policies[supportv1.SupportService_ListGarageDirectory_FullMethodName].Platform)

	approve := policies[supportv1.GarageSupportService_ApproveSupportGrant_FullMethodName]
	require.NotNil(t, approve.Garage)
	assert.True(t, approve.Garage.OwnerOnly)
	assert.True(t, approve.SkipLicense, "an owner can always revoke")
}
