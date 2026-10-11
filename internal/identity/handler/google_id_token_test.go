package handler_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	identityv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/identity/v1"

	"github.com/viettechno/vietgara-backend/internal/identity/handler"
	"github.com/viettechno/vietgara-backend/internal/platform/googleid"
)

// fakeVerifier answers Verify with claims, or err.
type fakeVerifier struct {
	configured bool
	claims     googleid.Claims
	err        error
	got        string
}

func (f *fakeVerifier) Configured() bool { return f.configured }

func (f *fakeVerifier) Verify(_ context.Context, idToken string) (googleid.Claims, error) {
	f.got = idToken

	return f.claims, f.err
}

func TestSignInWithGoogleIdToken_CreatesAVerifiedUser(t *testing.T) {
	t.Parallel()

	verifier := &fakeVerifier{configured: true, claims: googleid.Claims{
		Subject: "g-42", Email: "Binh@Gmail.com", EmailVerified: true, Name: "Trần Thị Bình",
	}}
	e := newEnv(t, handler.GoogleOAuthConfig{})
	e.srv.WithGoogleIDTokenVerifier(verifier)

	resp, err := e.srv.SignInWithGoogleIdToken(context.Background(), &identityv1.SignInWithGoogleIdTokenRequest{
		IdToken: "the-id-token", Locale: identityv1.Locale_LOCALE_EN,
	})

	require.NoError(t, err)
	assert.Equal(t, "the-id-token", verifier.got)

	user := resp.GetSession().GetUser()
	assert.Equal(t, "binh@gmail.com", user.GetEmail())
	assert.True(t, user.GetEmailVerified())
	assert.Equal(t, identityv1.Locale_LOCALE_EN, user.GetLocale())
	assert.Equal(t, identityv1.AuthProvider_AUTH_PROVIDER_GOOGLE, user.GetAuthProvider())
	assert.NotEmpty(t, resp.GetSession().GetRefreshToken())

	// Signing in again finds the same user by its Google subject.
	again, err := e.srv.SignInWithGoogleIdToken(context.Background(), &identityv1.SignInWithGoogleIdTokenRequest{IdToken: "t2"})
	require.NoError(t, err)
	assert.Equal(t, user.GetId(), again.GetSession().GetUser().GetId())
}

func TestSignInWithGoogleIdToken_Errors(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		verifier handler.GoogleIDTokenVerifier
		code     codes.Code
		reason   string
	}{
		"not configured": {
			verifier: &fakeVerifier{configured: false},
			code:     codes.FailedPrecondition, reason: "GOOGLE_NOT_CONFIGURED",
		},
		"no verifier": {verifier: nil, code: codes.FailedPrecondition, reason: "GOOGLE_NOT_CONFIGURED"},
		"invalid token": {
			verifier: &fakeVerifier{configured: true, err: errBadGoogleSignature},
			code:     codes.Unauthenticated, reason: handler.ReasonGoogleTokenInvalid,
		},
		"unverified e-mail": {
			verifier: &fakeVerifier{configured: true, claims: googleid.Claims{Subject: "g-1", Email: "a@b.c"}},
			code:     codes.Unauthenticated, reason: "INVALID_CREDENTIALS",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			e := newEnv(t, handler.GoogleOAuthConfig{})
			if tc.verifier != nil {
				e.srv.WithGoogleIDTokenVerifier(tc.verifier)
			}

			_, err := e.srv.SignInWithGoogleIdToken(context.Background(), &identityv1.SignInWithGoogleIdTokenRequest{IdToken: "x"})

			assert.Equal(t, tc.code, status.Code(err))
			assert.Equal(t, tc.reason, reason(err))
		})
	}
}
