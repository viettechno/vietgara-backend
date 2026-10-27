package middleware_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"

	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/mocks"
	"github.com/viettechno/vietgara-backend/internal/platform/access"
	"github.com/viettechno/vietgara-backend/internal/platform/auth"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// fakeTx runs fn directly and records the scope.
type fakeTx struct {
	scope platformdb.Scope
	err   error
}

func (f *fakeTx) WithTransaction(ctx context.Context, scope platformdb.Scope, fn func(ctx context.Context) error) error {
	f.scope = scope
	if f.err != nil {
		return f.err
	}

	return fn(ctx)
}

// garagePipeline runs Auth then Garage for a verified caller.
func garagePipeline(t *testing.T, tx middleware.Transactor, reader access.Reader, license middleware.LicenseChecker, policy middleware.Policy, method string) func(req any, h func(ctx context.Context) error) error {
	t.Helper()

	tok := token(t, auth.Identity{UserID: "user-1", EmailVerified: true})
	pols := middleware.Policies{method: policy}
	run := middleware.ChainUnaryInterceptors(middleware.Auth(issuer, pols), middleware.Garage(tx, reader, nil, license, nil))

	return func(req any, h func(ctx context.Context) error) error {
		_, err := run(withToken(context.Background(), tok), req, info(method), func(ctx context.Context, _ any) (any, error) {
			return nil, h(ctx)
		})

		return err
	}
}

func TestGarage_UserScope(t *testing.T) {
	t.Parallel()

	tx := &fakeTx{}
	call := garagePipeline(t, tx, nil, nil, middleware.UserMethod(), methodUser)

	require.NoError(t, call(nil, func(context.Context) error { return nil }))
	assert.Equal(t, platformdb.UserScope("user-1"), tx.scope)

	failing := errMiddlewareHandler
	err := call(nil, func(context.Context) error { return apperror.New(apperror.CodeNotFound, "gone") })
	requireCode(t, err, codes.NotFound, "NOT_FOUND")

	err = call(nil, func(context.Context) error { return failing })
	requireCode(t, err, codes.Internal, "")
}

func TestGarage_PublicSkipsTransaction(t *testing.T) {
	t.Parallel()

	tx := &fakeTx{err: errMiddlewareMustNotRun}
	ran := false
	_, err := middleware.ChainUnaryInterceptors(
		middleware.Auth(issuer, middleware.Policies{methodPublic: middleware.PublicMethod()}),
		middleware.Garage(tx, nil, nil, nil, nil),
	)(context.Background(), nil, info(methodPublic), okHandler(&ran))
	require.NoError(t, err)
	assert.True(t, ran)
}

func TestGarage_Authorization(t *testing.T) {
	t.Parallel()

	rule := middleware.GarageMethod(access.Requires(access.CustomerWrite))
	member := &access.Access{
		GarageID: garageID, UserID: "user-1", OwnerUserID: "owner", Active: true,
		Permissions: []access.Permission{access.CustomerWrite},
	}

	tests := []struct {
		name       string
		req        any
		access     *access.Access
		accessErr  error
		mode       middleware.LicenseMode
		modeErr    error
		method     string
		policy     *middleware.Policy
		wantCode   codes.Code
		wantReason string
	}{
		{name: "missing garage id", req: nil, wantCode: codes.InvalidArgument, wantReason: "VALIDATION_ERROR"},
		{name: "invalid garage id", req: garageReq{id: "nope"}, wantCode: codes.InvalidArgument, wantReason: "VALIDATION_ERROR"},
		{name: "no membership", req: garageReq{id: garageID}, accessErr: access.ErrNoMembership, wantCode: codes.PermissionDenied, wantReason: middleware.ReasonGarageAccessDenied},
		{name: "access query fails", req: garageReq{id: garageID}, accessErr: errMiddlewareDB, wantCode: codes.Internal},
		{name: "missing permission", req: garageReq{id: garageID}, access: &access.Access{Active: true}, wantCode: codes.PermissionDenied, wantReason: "PERMISSION_DENIED"},
		{name: "owner only", req: garageReq{id: garageID}, access: member, policy: new(middleware.GarageMethod(access.OwnerOnly())), wantCode: codes.PermissionDenied, wantReason: middleware.ReasonOwnerOnly},
		{name: "inactive garage", req: garageReq{id: garageID}, access: &access.Access{Permissions: member.Permissions}, wantCode: codes.FailedPrecondition, wantReason: middleware.ReasonGarageInactive},
		{name: "allowed", req: garageReq{id: garageID}, access: member},
		{name: "locked subscription", req: garageReq{id: garageID}, access: member, mode: middleware.LicenseLocked, wantCode: codes.FailedPrecondition, wantReason: middleware.ReasonSubscriptionSuspended},
		{name: "read-only blocks writes", req: garageReq{id: garageID}, access: member, mode: middleware.LicenseReadOnly, wantCode: codes.PermissionDenied, wantReason: middleware.ReasonSubscriptionReadOnly},
		{name: "read-only allows reads", req: garageReq{id: garageID}, access: member, mode: middleware.LicenseReadOnly, method: methodGarageRO},
		{name: "license failure lets through", req: garageReq{id: garageID}, access: member, modeErr: errMiddlewareCacheDown},
		{name: "license skipped", req: garageReq{id: garageID}, access: member, mode: middleware.LicenseLocked, policy: func() *middleware.Policy {
			p := rule
			p.SkipLicense = true

			return &p
		}()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			reader := mocks.NewMockAccessReader(t)
			if tt.access != nil || tt.accessErr != nil {
				reader.EXPECT().GarageAccess(mock.Anything, garageID, "user-1").Return(tt.access, tt.accessErr)
			}

			checker := mocks.NewMockLicenseChecker(t)

			policy := rule
			if tt.policy != nil {
				policy = *tt.policy
			}

			if tt.access != nil && tt.access.Check(*policy.Garage) == nil && !policy.SkipLicense {
				checker.EXPECT().LicenseMode(mock.Anything, garageID).Return(tt.mode, tt.modeErr)
			}

			method := methodGarage
			if tt.method != "" {
				method = tt.method
			}

			tx := &fakeTx{}

			var gotAccess *access.Access

			err := garagePipeline(t, tx, reader, checker, policy, method)(tt.req, func(ctx context.Context) error {
				gotAccess, _ = access.FromContext(ctx)

				return nil
			})

			if tt.wantCode != codes.OK {
				requireCode(t, err, tt.wantCode, tt.wantReason)

				return
			}

			require.NoError(t, err)
			assert.Same(t, tt.access, gotAccess)
			assert.Equal(t, platformdb.Scope{GarageID: garageID, UserID: "user-1"}, tx.scope)
		})
	}
}

func TestGarage_NilLicenseChecker(t *testing.T) {
	t.Parallel()

	reader := mocks.NewMockAccessReader(t)
	reader.EXPECT().GarageAccess(mock.Anything, garageID, "user-1").Return(&access.Access{Owner: true}, nil)

	err := garagePipeline(t, &fakeTx{}, reader, nil, middleware.GarageMethod(access.Member()), methodGarage)(
		garageReq{id: garageID}, func(context.Context) error { return nil })
	require.NoError(t, err)
}

//go:fix inline

type fakePlatform struct {
	access *access.PlatformAccess
	err    error
	asked  string
}

func (f *fakePlatform) PlatformAccess(_ context.Context, userID string) (*access.PlatformAccess, error) {
	f.asked = userID

	return f.access, f.err
}

func TestGarage_PlatformPermission(t *testing.T) {
	t.Parallel()

	const method = "/test.v1.Svc/PlatformThing"

	run := func(reader access.PlatformReader, required access.PlatformPermission) (ran bool, got *access.PlatformAccess, err error) {
		tok := token(t, auth.Identity{UserID: "user-1", EmailVerified: true})
		pols := middleware.Policies{method: middleware.PlatformMethod(required)}
		chain := middleware.ChainUnaryInterceptors(middleware.Auth(issuer, pols), middleware.Garage(&fakeTx{}, nil, reader, nil, nil))

		_, err = chain(withToken(context.Background(), tok), nil, info(method), func(ctx context.Context, _ any) (any, error) {
			ran = true
			got, _ = access.PlatformFromContext(ctx)

			return nil, nil
		})

		return ran, got, err
	}

	staff := &fakePlatform{access: &access.PlatformAccess{UserID: "user-1", Permissions: []access.PlatformPermission{access.PlatformSubscriptionRead}}}

	ran, got, err := run(staff, access.PlatformSubscriptionRead)
	require.NoError(t, err)
	assert.True(t, ran)
	assert.Equal(t, "user-1", staff.asked, "the user comes from the token, the permissions from the database")
	assert.Same(t, staff.access, got, "the handler can read the caller's platform access")

	ran, _, err = run(staff, access.PlatformSubscriptionWrite)
	requireCode(t, err, codes.PermissionDenied, middleware.ReasonPlatformDenied)
	assert.False(t, ran, "the handler does not run without the permission")

	admin := &fakePlatform{access: &access.PlatformAccess{UserID: "user-1", Admin: true}}
	ran, _, err = run(admin, access.PlatformPlanWrite)
	require.NoError(t, err)
	assert.True(t, ran, "a Platform Admin passes every check")

	_, _, err = run(&fakePlatform{err: errMiddlewareHandler}, access.PlatformPlanRead)
	requireCode(t, err, codes.Internal, "")
}
