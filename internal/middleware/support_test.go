package middleware_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"

	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/mocks"
	"github.com/viettechno/vietgara-backend/internal/platform/access"
	"github.com/viettechno/vietgara-backend/internal/platform/auth"
	supportv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/support/v1"
)

type fakeSupport struct {
	grant    *access.Access
	err      error
	beginErr error
	began    []string
}

func (f *fakeSupport) SupportAccess(context.Context, string, string) (*access.Access, error) {
	return f.grant, f.err
}

func (f *fakeSupport) BeginRead(_ context.Context, _ *access.Access, method, entityType, entityID string) error {
	f.began = append(f.began, method+"|"+entityType+"|"+entityID)

	return f.beginErr
}

func supportPipeline(t *testing.T, support middleware.SupportSessions, rule access.Rule, method string) func(req any) (*access.Access, error) {
	t.Helper()

	reader := mocks.NewMockAccessReader(t)
	reader.EXPECT().GarageAccess(mock.Anything, garageID, "user-1").Return(nil, access.ErrNoMembership)

	tok := token(t, auth.Identity{UserID: "user-1", EmailVerified: true})
	pols := middleware.Policies{method: middleware.GarageMethod(rule)}
	run := middleware.ChainUnaryInterceptors(middleware.Auth(issuer, pols), middleware.Garage(&fakeTx{}, reader, nil, nil, support))

	return func(req any) (*access.Access, error) {
		var got *access.Access

		_, err := run(withToken(context.Background(), tok), req, info(method), func(ctx context.Context, _ any) (any, error) {
			got, _ = access.FromContext(ctx)

			return nil, nil
		})

		return got, err
	}
}

func TestGarage_SupportSession(t *testing.T) {
	t.Parallel()

	grant := &access.Access{GarageID: garageID, UserID: "user-1", Active: true, Permissions: access.ReadOnly(), SupportGrantID: "grant-1"}
	read := "/vietgara.billing.v1.SettlementService/GetSettlement"

	t.Run("reads under a grant are logged first", func(t *testing.T) {
		t.Parallel()

		support := &fakeSupport{grant: grant}
		got, err := supportPipeline(t, support, access.Requires(access.BillingRead), read)(&supportv1.ListSupportAccessLogRequest{GarageId: garageID, GrantId: "grant-9"})
		require.NoError(t, err)
		assert.Equal(t, "grant-1", got.SupportGrantID)
		assert.Equal(t, []string{read + "|settlement|grant-9"}, support.began, "the first *_id field other than the garage")
	})

	t.Run("a list names no id", func(t *testing.T) {
		t.Parallel()

		support := &fakeSupport{grant: grant}
		_, err := supportPipeline(t, support, access.Requires(access.BillingRead), "/x.v1.Svc/ListRepairOrders")(garageReq{garageID})
		require.NoError(t, err)
		assert.Equal(t, []string{"/x.v1.Svc/ListRepairOrders|repair_orders|"}, support.began)
	})

	t.Run("a write is refused", func(t *testing.T) {
		t.Parallel()

		support := &fakeSupport{grant: grant}
		_, err := supportPipeline(t, support, access.Requires(access.BillingRead), "/x.v1.Svc/UpdateSettlement")(garageReq{garageID})
		requireCode(t, err, codes.PermissionDenied, middleware.ReasonSupportReadOnly)
		assert.Empty(t, support.began)
	})

	t.Run("owner-only and write permissions are refused", func(t *testing.T) {
		t.Parallel()

		support := &fakeSupport{grant: grant}
		_, err := supportPipeline(t, support, access.OwnerOnly(), "/x.v1.Svc/ListThings")(garageReq{garageID})
		requireCode(t, err, codes.PermissionDenied, middleware.ReasonOwnerOnly)

		_, err = supportPipeline(t, support, access.Requires(access.BillingWrite), "/x.v1.Svc/GetThing")(garageReq{garageID})
		requireCode(t, err, codes.PermissionDenied, "PERMISSION_DENIED")
		assert.Empty(t, support.began)
	})

	t.Run("no grant keeps the ordinary refusal", func(t *testing.T) {
		t.Parallel()

		_, err := supportPipeline(t, &fakeSupport{err: access.ErrNoMembership}, access.Member(), "/x.v1.Svc/GetThing")(garageReq{garageID})
		requireCode(t, err, codes.PermissionDenied, middleware.ReasonGarageAccessDenied)
	})

	t.Run("failures are internal", func(t *testing.T) {
		t.Parallel()

		_, err := supportPipeline(t, &fakeSupport{err: errMiddlewareDBDown}, access.Member(), "/x.v1.Svc/GetThing")(garageReq{garageID})
		requireCode(t, err, codes.Internal, "")

		_, err = supportPipeline(t, &fakeSupport{grant: grant, beginErr: errMiddlewareDBDown}, access.Member(), "/x.v1.Svc/GetThing")(garageReq{garageID})
		requireCode(t, err, codes.Internal, "")
	})
}

func TestSupportEntity_FitsTheLog(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("x", 300)
	_, id := middleware.SupportEntityForTest("/x.v1.Svc/GetThing", &supportv1.ListSupportAccessLogRequest{GarageId: garageID, GrantId: long + "\x00"})
	assert.Len(t, id, 100)

	_, id = middleware.SupportEntityForTest("/x.v1.Svc/GetThing", &supportv1.ListSupportAccessLogRequest{GarageId: garageID, GrantId: "a\x00b\xff"})
	assert.Equal(t, "ab", id)
}
