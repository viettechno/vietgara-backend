package handler_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	identityv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/identity/v1"

	"github.com/viettechno/vietgara-backend/internal/identity/handler"
	"github.com/viettechno/vietgara-backend/internal/platform/googleid"
)

// verifiedUser registers a@b.vn with a password, verifies the e-mail and
// returns the context of its verified session.
func verifiedUser(t *testing.T, e *env) (userID string, ctxAs func() *identityv1.Session) {
	t.Helper()

	reg, err := e.srv.Register(t.Context(), &identityv1.RegisterRequest{Email: "a@b.vn", Password: "secret123", FullName: "A"})
	require.NoError(t, err)

	verified, err := e.srv.VerifyEmail(e.as(t, reg.GetSession().GetAccessToken()),
		&identityv1.VerifyEmailRequest{OtpCode: e.mail.lastCode(), RefreshToken: reg.GetSession().GetRefreshToken()})
	require.NoError(t, err)

	return verified.GetSession().GetUser().GetId(), func() *identityv1.Session { return verified.GetSession() }
}

func TestGoogleLinkPolicies(t *testing.T) {
	t.Parallel()

	p := handler.Policies()

	for _, method := range []string{
		identityv1.AuthService_CompleteGoogleLink_FullMethodName,
		identityv1.AuthService_LinkGoogle_FullMethodName,
		identityv1.AuthService_UnlinkGoogle_FullMethodName,
	} {
		assert.False(t, p[method].Public, method)
		assert.False(t, p[method].AllowUnverified, "a user with an unverified e-mail cannot add a way to sign in: %s", method)
	}
}

func TestLinkAndUnlinkGoogleRPCs(t *testing.T) {
	t.Parallel()

	e := newEnv(t, handler.GoogleOAuthConfig{})
	verifier := &fakeVerifier{configured: true, claims: googleid.Claims{Subject: "g-1", Email: "a@gmail.com", EmailVerified: true, Name: "A"}}
	e.srv.WithGoogleIDTokenVerifier(verifier)

	_, session := verifiedUser(t, e)
	me := e.as(t, session().GetAccessToken())

	got, err := e.srv.GetMe(me, &identityv1.GetMeRequest{})
	require.NoError(t, err)
	assert.False(t, got.GetUser().GetGoogleLinked())
	assert.True(t, got.GetUser().GetHasPassword())

	_, err = e.srv.LinkGoogle(me, &identityv1.LinkGoogleRequest{IdToken: "t", Password: "wrong-pass"})
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
	assert.Equal(t, "PASSWORD_INCORRECT", reason(err))

	linked, err := e.srv.LinkGoogle(me, &identityv1.LinkGoogleRequest{IdToken: "the-token", Password: "secret123"})
	require.NoError(t, err)
	assert.Equal(t, "the-token", verifier.got)
	assert.True(t, linked.GetUser().GetGoogleLinked())
	assert.True(t, linked.GetUser().GetHasPassword())

	_, err = e.srv.UnlinkGoogle(me, &identityv1.UnlinkGoogleRequest{Password: "wrong-pass"})
	assert.Equal(t, "PASSWORD_INCORRECT", reason(err))

	unlinked, err := e.srv.UnlinkGoogle(me, &identityv1.UnlinkGoogleRequest{Password: "secret123"})
	require.NoError(t, err)
	assert.False(t, unlinked.GetSession().GetUser().GetGoogleLinked())
	assert.NotEmpty(t, unlinked.GetSession().GetAccessToken())
	assert.NotEqual(t, session().GetRefreshToken(), unlinked.GetSession().GetRefreshToken())

	_, err = e.srv.UnlinkGoogle(me, &identityv1.UnlinkGoogleRequest{Password: "secret123"})
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
	assert.Equal(t, "GOOGLE_NOT_LINKED", reason(err))
}

func TestLinkGoogleNeedsAVerifiableToken(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		verifier handler.GoogleIDTokenVerifier
		code     codes.Code
		reason   string
	}{
		"not configured": {nil, codes.FailedPrecondition, "GOOGLE_NOT_CONFIGURED"},
		"rejected token": {&fakeVerifier{configured: true, err: errBadGoogleSignature}, codes.Unauthenticated, handler.ReasonGoogleTokenInvalid},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			e := newEnv(t, handler.GoogleOAuthConfig{})
			if tc.verifier != nil {
				e.srv.WithGoogleIDTokenVerifier(tc.verifier)
			}

			_, session := verifiedUser(t, e)

			_, err := e.srv.LinkGoogle(e.as(t, session().GetAccessToken()), &identityv1.LinkGoogleRequest{IdToken: "t", Password: "secret123"})
			assert.Equal(t, tc.code, status.Code(err))
			assert.Equal(t, tc.reason, reason(err))
		})
	}
}

func TestCompleteGoogleLinkRPC(t *testing.T) {
	t.Parallel()

	google := fakeGoogle(t, http.StatusOK, `{"sub":"g-9","email":"a@gmail.com","email_verified":true,"name":"A","locale":"en"}`)
	e := newEnv(t, config(google))

	_, session := verifiedUser(t, e)
	me := e.as(t, session().GetAccessToken())

	// The popup: Google verifies the account and the callback posts a proof.
	rec := serve(t, e, handler.GoogleLoginPath+"?link=1", nil)
	require.Equal(t, http.StatusFound, rec.Code)

	location, err := url.Parse(rec.Header().Get("Location"))
	require.NoError(t, err)

	state := location.Query().Get("state")
	assert.True(t, strings.HasPrefix(state, "link."), "the popup is marked as a link, in the state Google carries")

	rec = serve(t, e, handler.GoogleCallbackPath+"?code=the-code&state="+state, rec.Result().Cookies()[0])
	require.Equal(t, http.StatusOK, rec.Code)

	message := popupMessage(t, rec)
	proof, ok := message["linkProof"].(string)
	require.True(t, ok, "the popup posts a proof, not a session")
	assert.NotContains(t, message, "session")

	_, err = e.srv.CompleteGoogleLink(me, &identityv1.CompleteGoogleLinkRequest{LinkProof: proof, Password: "wrong-pass"})
	assert.Equal(t, "PASSWORD_INCORRECT", reason(err))

	done, err := e.srv.CompleteGoogleLink(me, &identityv1.CompleteGoogleLinkRequest{LinkProof: proof, Password: "secret123"})
	require.NoError(t, err)
	assert.True(t, done.GetUser().GetGoogleLinked())

	_, err = e.srv.CompleteGoogleLink(me, &identityv1.CompleteGoogleLinkRequest{LinkProof: proof, Password: "secret123"})
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	assert.Equal(t, "LINK_PROOF_INVALID", reason(err))
}

func TestLinkPopupErrors(t *testing.T) {
	t.Parallel()

	google := fakeGoogle(t, http.StatusOK, `{"sub":"g-9","email":"a@gmail.com","email_verified":false,"name":"A"}`)
	e := newEnv(t, config(google))

	rec := serve(t, e, handler.GoogleLoginPath+"?link=1", nil)
	location, err := url.Parse(rec.Header().Get("Location"))
	require.NoError(t, err)

	rec = serve(t, e, handler.GoogleCallbackPath+"?code=the-code&state="+location.Query().Get("state"), rec.Result().Cookies()[0])
	assert.Equal(t, "INVALID_CREDENTIALS", popupMessage(t, rec)["error"], "an unverified Google e-mail gives no proof")
}
