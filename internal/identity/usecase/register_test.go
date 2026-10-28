package usecase_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/identity/domain"
	"github.com/viettechno/vietgara-backend/internal/identity/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

func TestRegister(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	session := e.register(t)

	user := session.User
	assert.Equal(t, "owner@example.com", user.Email)
	assert.False(t, user.EmailVerified())
	assert.Equal(t, domain.LocaleVI, user.Locale)
	assert.NotNil(t, user.PasswordHash)
	assert.NotEmpty(t, session.AccessToken)

	claims, err := e.issuer.Verify(session.AccessToken)
	require.NoError(t, err)
	assert.False(t, claims.EmailVerified)

	require.Len(t, e.mailer.sent, 1, "verification OTP e-mailed")
	assert.Equal(t, "email_verification", e.mailer.sent[0].Tag)
	assert.Contains(t, e.mailer.sent[0].Subject, "Mã xác minh")
}

func TestRegisterValidation(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.register(t)

	tests := []struct {
		name   string
		req    usecase.RegisterRequest
		reason string
	}{
		{name: "bad email", req: usecase.RegisterRequest{Email: "x", Password: "secret123", FullName: "A"}, reason: apperror.ReasonValidation},
		{name: "no name", req: usecase.RegisterRequest{Email: "a@b.vn", Password: "secret123"}, reason: apperror.ReasonValidation},
		{name: "short password", req: usecase.RegisterRequest{Email: "a@b.vn", Password: "short", FullName: "A"}, reason: apperror.ReasonValidation},
		{name: "bad phone", req: usecase.RegisterRequest{Email: "a@b.vn", Password: "secret123", FullName: "A", Phone: new("12")}, reason: apperror.ReasonValidation},
		{name: "email taken", req: usecase.RegisterRequest{Email: "OWNER@example.com", Password: "secret123", FullName: "A"}, reason: domain.ReasonEmailTaken},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := e.uc.Register(t.Context(), tt.req)
			requireReason(t, err, tt.reason)
		})
	}

	_, err := e.uc.Register(t.Context(), usecase.RegisterRequest{Email: "p1@b.vn", Password: "secret123", FullName: "P", Phone: new("+84 90 111 2222")})
	require.NoError(t, err)

	_, err = e.uc.Register(t.Context(), usecase.RegisterRequest{Email: "p2@b.vn", Password: "secret123", FullName: "P", Phone: new("0901112222")})
	requireReason(t, err, domain.ReasonPhoneTaken)
}

func TestRegisterFailures(t *testing.T) {
	t.Parallel()

	req := usecase.RegisterRequest{Email: "a@b.vn", Password: "secret123", FullName: "A", Locale: domain.LocaleEN}

	e := newEnv(t)
	e.users.createErr = domain.ErrEmailAlreadyExists
	_, err := e.uc.Register(t.Context(), req)
	requireReason(t, err, domain.ReasonEmailTaken)

	e.users.createErr = domain.ErrPhoneAlreadyExists
	_, err = e.uc.Register(t.Context(), req)
	requireReason(t, err, domain.ReasonPhoneTaken)

	e.users.createErr = errBoom
	_, err = e.uc.Register(t.Context(), req)
	requireReason(t, err, apperror.ReasonInternal)

	e.users.createErr, e.users.findErr = nil, errBoom
	_, err = e.uc.Register(t.Context(), req)
	requireReason(t, err, apperror.ReasonInternal)

	req.Phone = new("0901112222")
	e.users.findErr = nil
	e2 := newEnv(t)
	e2.users.findErr = nil
	_, err = e2.uc.Register(t.Context(), req)
	require.NoError(t, err)

	// A failing mail provider does not fail sign-up: the OTP can be resent.
	e3 := newEnv(t)
	e3.mailer.err = errBoom
	_, err = e3.uc.Register(t.Context(), usecase.RegisterRequest{Email: "c@b.vn", Password: "secret123", FullName: "C"})
	require.NoError(t, err)
}

func TestGrantPlatformAdmins(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	require.NoError(t, e.uc.GrantPlatformAdmins(t.Context(), nil))
	require.NoError(t, e.uc.GrantPlatformAdmins(t.Context(), []string{" Admin@viettechno.com ", "bad"}))
	assert.Equal(t, []string{"admin@viettechno.com"}, e.users.granted)

	e.users.findErr = errBoom
	require.Error(t, e.uc.GrantPlatformAdmins(t.Context(), []string{"a@b.vn"}))
}

// TestPlatformAdminNeedsVerifiedEmail covers SR-25: a configured admin
// address is granted once its e-mail is verified, not when it registers.
func TestPlatformAdminNeedsVerifiedEmail(t *testing.T) {
	t.Parallel()

	t.Run("by OTP", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t, usecase.WithPlatformAdminEmails([]string{"Owner@Example.com"}))
		registered := e.register(t)
		assert.False(t, registered.User.PlatformAdmin)
		assert.Empty(t, e.users.granted, "nothing is granted before the e-mail is verified")

		verified, err := e.uc.VerifyEmail(t.Context(), registered.User.ID, e.mailer.lastCode(t, "owner@example.com"), registered.RefreshToken)
		require.NoError(t, err)
		assert.True(t, verified.User.PlatformAdmin)
		assert.Equal(t, []string{"owner@example.com"}, e.users.granted)
	})

	t.Run("by Google", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t, usecase.WithPlatformAdminEmails([]string{"owner@example.com"}))

		session, err := e.uc.GoogleLogin(t.Context(), usecase.GoogleProfile{Subject: "g-7", Email: "owner@example.com", EmailVerified: true, FullName: "Minh"})
		require.NoError(t, err)
		assert.True(t, session.User.PlatformAdmin)
	})

	t.Run("another address is never granted", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t, usecase.WithPlatformAdminEmails([]string{"admin@viettechno.com"}))
		registered := e.register(t)

		verified, err := e.uc.VerifyEmail(t.Context(), registered.User.ID, e.mailer.lastCode(t, "owner@example.com"), registered.RefreshToken)
		require.NoError(t, err)
		assert.False(t, verified.User.PlatformAdmin)
		assert.Empty(t, e.users.granted)
	})
}
