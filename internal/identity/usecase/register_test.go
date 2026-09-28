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

	acc := session.Account
	assert.Equal(t, "owner@example.com", acc.Email)
	assert.False(t, acc.EmailVerified())
	assert.Equal(t, domain.LocaleVI, acc.Locale)
	assert.NotNil(t, acc.PasswordHash)
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
	e.accounts.createErr = domain.ErrEmailAlreadyExists
	_, err := e.uc.Register(t.Context(), req)
	requireReason(t, err, domain.ReasonEmailTaken)

	e.accounts.createErr = domain.ErrPhoneAlreadyExists
	_, err = e.uc.Register(t.Context(), req)
	requireReason(t, err, domain.ReasonPhoneTaken)

	e.accounts.createErr = errBoom
	_, err = e.uc.Register(t.Context(), req)
	requireReason(t, err, apperror.ReasonInternal)

	e.accounts.createErr, e.accounts.findErr = nil, errBoom
	_, err = e.uc.Register(t.Context(), req)
	requireReason(t, err, apperror.ReasonInternal)

	req.Phone = new("0901112222")
	e.accounts.findErr = nil
	e2 := newEnv(t)
	e2.accounts.findErr = nil
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
	require.NoError(t, e.uc.GrantPlatformAdmins(t.Context(), []string{" Admin@VietGara.vn ", "bad"}))
	assert.Equal(t, []string{"admin@vietgara.vn"}, e.accounts.granted)

	e.accounts.findErr = errBoom
	require.Error(t, e.uc.GrantPlatformAdmins(t.Context(), []string{"a@b.vn"}))
}
