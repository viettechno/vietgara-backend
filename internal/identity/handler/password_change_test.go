package handler_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	identityv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/identity/v1"

	"github.com/viettechno/vietgara-backend/internal/identity/handler"
)

func TestChangePasswordRPC(t *testing.T) {
	t.Parallel()

	e := newEnv(t, handler.GoogleOAuthConfig{})

	_, session := verifiedUser(t, e)
	me := e.as(t, session().GetAccessToken())

	_, err := e.srv.ChangePassword(me, &identityv1.ChangePasswordRequest{CurrentPassword: "wrong-pass", NewPassword: "brand-new-pass"})
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
	assert.Equal(t, "PASSWORD_INCORRECT", reason(err))

	_, err = e.srv.ChangePassword(me, &identityv1.ChangePasswordRequest{CurrentPassword: "secret123", NewPassword: "short"})
	assert.Equal(t, codes.InvalidArgument, status.Code(err))

	changed, err := e.srv.ChangePassword(me, &identityv1.ChangePasswordRequest{CurrentPassword: "secret123", NewPassword: "brand-new-pass"})
	require.NoError(t, err)
	assert.NotEmpty(t, changed.GetSession().GetAccessToken())
	assert.NotEqual(t, session().GetRefreshToken(), changed.GetSession().GetRefreshToken())

	_, err = e.srv.ChangePassword(me, &identityv1.ChangePasswordRequest{CurrentPassword: "secret123", NewPassword: "another-pass"})
	assert.Equal(t, "PASSWORD_INCORRECT", reason(err), "the old password no longer confirms")
}
