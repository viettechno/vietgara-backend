package domain_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/viettechno/vietgara-backend/internal/identity/domain"
)

func TestEnums(t *testing.T) {
	t.Parallel()

	assert.True(t, domain.AuthProviderEmail.IsValid())
	assert.True(t, domain.AuthProviderGoogle.IsValid())
	assert.False(t, domain.AuthProvider("facebook").IsValid())
	assert.True(t, domain.UserStatusActive.IsValid())
	assert.True(t, domain.UserStatusSuspended.IsValid())
	assert.False(t, domain.UserStatus("inactive").IsValid())
	assert.Equal(t, domain.LocaleEN, domain.LocaleEN.OrDefault())
	assert.Equal(t, domain.LocaleVI, domain.Locale("fr").OrDefault())
}

func TestEmailVerification(t *testing.T) {
	t.Parallel()

	user := &domain.User{}
	assert.False(t, user.EmailVerified())

	now := time.Now()
	assert.True(t, user.MarkEmailVerified(now))
	assert.True(t, user.EmailVerified())
	assert.False(t, user.MarkEmailVerified(now.Add(time.Hour)))
	assert.Equal(t, now, *user.EmailVerifiedAt)
}

func TestNamesAndPasswords(t *testing.T) {
	t.Parallel()

	name, ok := domain.NormalizeFullName("  Nguyễn   Văn  Minh ")
	assert.True(t, ok)
	assert.Equal(t, "Nguyễn Văn Minh", name)

	_, ok = domain.NormalizeFullName("   ")
	assert.False(t, ok)
	_, ok = domain.NormalizeFullName(strings.Repeat("x", 256))
	assert.False(t, ok)

	assert.True(t, domain.ValidPassword("12345678"))
	assert.False(t, domain.ValidPassword("1234567"))
	assert.False(t, domain.ValidPassword(strings.Repeat("x", 73)))
}

func TestOTP(t *testing.T) {
	t.Parallel()

	code := domain.NewOTPCode()
	assert.Len(t, code, domain.OTPLength)

	key := domain.OTPKey(domain.OTPEmailVerification, "user-1")
	assert.Equal(t, "email_verification:user-1", key)

	now := time.Now()
	otp := domain.NewOTP(key, code, now)
	assert.True(t, otp.Matches(key, code))
	assert.False(t, otp.Matches(key, "not-it"))
	assert.False(t, otp.Matches(domain.OTPKey(domain.OTPPasswordReset, "user-1"), code), "bound to its key")
	assert.Equal(t, now.Add(domain.OTPTTL), otp.ExpiresAt)
	assert.Equal(t, now.Add(domain.OTPResendCooldown), otp.ResendAt)
	assert.False(t, otp.Expired(now.Add(time.Minute)))
	assert.True(t, otp.Expired(now.Add(domain.OTPTTL)))
	assert.NotEqual(t, domain.HashOTP(key, code), code)
}
