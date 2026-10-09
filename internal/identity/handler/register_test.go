package handler_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	identityv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/identity/v1"

	"github.com/viettechno/vietgara-backend/internal/identity/handler"
	"github.com/viettechno/vietgara-backend/internal/middleware"
)

func TestPolicies(t *testing.T) {
	t.Parallel()

	p := handler.Policies()
	assert.True(t, p[identityv1.AuthService_Login_FullMethodName].Public)
	assert.True(t, p[identityv1.AuthService_VerifyEmail_FullMethodName].AllowUnverified)
	assert.Equal(t, middleware.UnverifiedMethod(), p[identityv1.UserService_GetMe_FullMethodName])
	assert.True(t, p[identityv1.AuthService_SignInWithGoogleIdToken_FullMethodName].Public)
	assert.Len(t, p, 12)
}

func TestAuthFlow(t *testing.T) {
	t.Parallel()

	e := newEnv(t, handler.GoogleOAuthConfig{})
	ctx := t.Context()
	phone := "0901234567"

	reg, err := e.srv.Register(ctx, &identityv1.RegisterRequest{Email: "a@b.vn", Password: "secret123", FullName: "A", Phone: &phone, Locale: identityv1.Locale_LOCALE_EN})
	require.NoError(t, err)

	s := reg.GetSession()
	assert.Equal(t, "Bearer", s.GetTokenType())
	assert.EqualValues(t, 1200, s.GetExpiresIn())
	assert.Equal(t, identityv1.Locale_LOCALE_EN, s.GetUser().GetLocale())
	assert.Equal(t, identityv1.AuthProvider_AUTH_PROVIDER_EMAIL, s.GetUser().GetAuthProvider())
	assert.Equal(t, identityv1.UserStatus_USER_STATUS_ACTIVE, s.GetUser().GetStatus())
	assert.False(t, s.GetUser().GetEmailVerified())

	_, err = e.srv.Register(ctx, &identityv1.RegisterRequest{Email: "bad"})
	assert.Equal(t, "VALIDATION_ERROR", reason(err))

	me := e.as(t, s.GetAccessToken())

	_, err = e.srv.SendEmailVerificationOtp(me, &identityv1.SendEmailVerificationOtpRequest{})
	assert.Equal(t, "OTP_RESEND_TOO_SOON", reason(err))

	_, err = e.srv.VerifyEmail(me, &identityv1.VerifyEmailRequest{OtpCode: "000000"})
	assert.Equal(t, "OTP_INVALID", reason(err))

	verified, err := e.srv.VerifyEmail(me, &identityv1.VerifyEmailRequest{OtpCode: e.mail.lastCode(), RefreshToken: s.GetRefreshToken()})
	require.NoError(t, err)
	assert.True(t, verified.GetSession().GetUser().GetEmailVerified())

	_, err = e.srv.SendEmailVerificationOtp(me, &identityv1.SendEmailVerificationOtpRequest{})
	assert.Equal(t, "EMAIL_ALREADY_VERIFIED", reason(err))

	login, err := e.srv.Login(ctx, &identityv1.LoginRequest{Email: "a@b.vn", Password: "secret123"})
	require.NoError(t, err)
	_, err = e.srv.Login(ctx, &identityv1.LoginRequest{Email: "a@b.vn", Password: "nope"})
	assert.Equal(t, "INVALID_CREDENTIALS", reason(err))

	refreshed, err := e.srv.RefreshToken(ctx, &identityv1.RefreshTokenRequest{RefreshToken: login.GetSession().GetRefreshToken()})
	require.NoError(t, err)
	_, err = e.srv.RefreshToken(ctx, &identityv1.RefreshTokenRequest{RefreshToken: "gone"})
	assert.Equal(t, "REFRESH_TOKEN_INVALID", reason(err))

	_, err = e.srv.Logout(ctx, &identityv1.LogoutRequest{RefreshToken: refreshed.GetSession().GetRefreshToken()})
	require.NoError(t, err)
}

func TestPasswordResetRPCs(t *testing.T) {
	t.Parallel()

	e := newEnv(t, handler.GoogleOAuthConfig{})
	ctx := t.Context()

	_, err := e.srv.Register(ctx, &identityv1.RegisterRequest{Email: "a@b.vn", Password: "secret123", FullName: "A"})
	require.NoError(t, err)

	_, err = e.srv.RequestPasswordReset(ctx, &identityv1.RequestPasswordResetRequest{Email: "bad"})
	assert.Equal(t, "VALIDATION_ERROR", reason(err))

	challenge, err := e.srv.RequestPasswordReset(ctx, &identityv1.RequestPasswordResetRequest{Email: "a@b.vn"})
	require.NoError(t, err)
	assert.NotNil(t, challenge.GetChallenge().GetExpiresAt())

	_, err = e.srv.VerifyPasswordResetOtp(ctx, &identityv1.VerifyPasswordResetOtpRequest{Email: "a@b.vn", OtpCode: "000000"})
	assert.Equal(t, "OTP_INVALID", reason(err))

	token, err := e.srv.VerifyPasswordResetOtp(ctx, &identityv1.VerifyPasswordResetOtpRequest{Email: "a@b.vn", OtpCode: e.mail.lastCode()})
	require.NoError(t, err)

	_, err = e.srv.ResetPassword(ctx, &identityv1.ResetPasswordRequest{ResetToken: token.GetResetToken(), NewPassword: "x"})
	assert.Equal(t, "VALIDATION_ERROR", reason(err))
	_, err = e.srv.ResetPassword(ctx, &identityv1.ResetPasswordRequest{ResetToken: token.GetResetToken(), NewPassword: "another-pass"})
	require.NoError(t, err)
}

func TestProfileRPCs(t *testing.T) {
	t.Parallel()

	e := newEnv(t, handler.GoogleOAuthConfig{})
	reg, err := e.srv.Register(t.Context(), &identityv1.RegisterRequest{Email: "a@b.vn", Password: "secret123", FullName: "A"})
	require.NoError(t, err)

	me := e.as(t, reg.GetSession().GetAccessToken())

	got, err := e.srv.GetMe(me, &identityv1.GetMeRequest{})
	require.NoError(t, err)
	assert.Equal(t, "a@b.vn", got.GetUser().GetEmail())

	phone := "0901112222"
	updated, err := e.srv.UpdateMe(me, &identityv1.UpdateMeRequest{User: &identityv1.User{FullName: "B", Phone: &phone, Locale: identityv1.Locale_LOCALE_EN}})
	require.NoError(t, err)
	assert.Equal(t, "B", updated.GetUser().GetFullName())
	assert.Equal(t, identityv1.Locale_LOCALE_EN, updated.GetUser().GetLocale())

	masked, err := e.srv.UpdateMe(me, &identityv1.UpdateMeRequest{
		User:       &identityv1.User{FullName: "C", Locale: identityv1.Locale_LOCALE_VI},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"locale"}},
	})
	require.NoError(t, err)
	assert.Equal(t, "B", masked.GetUser().GetFullName())
	assert.Equal(t, identityv1.Locale_LOCALE_VI, masked.GetUser().GetLocale())

	_, err = e.srv.UpdateMe(me, &identityv1.UpdateMeRequest{User: &identityv1.User{}, UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"email"}}})
	assert.Equal(t, "VALIDATION_ERROR", reason(err))

	_, err = e.srv.UpdateMe(me, &identityv1.UpdateMeRequest{User: &identityv1.User{FullName: " "}})
	assert.Equal(t, "VALIDATION_ERROR", reason(err))

	_, err = e.srv.GetMe(t.Context(), &identityv1.GetMeRequest{})
	assert.Equal(t, "UNAUTHENTICATED", reason(err))
}
