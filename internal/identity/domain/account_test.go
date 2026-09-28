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
	assert.True(t, domain.AccountStatusActive.IsValid())
	assert.True(t, domain.AccountStatusSuspended.IsValid())
	assert.False(t, domain.AccountStatus("inactive").IsValid())
	assert.Equal(t, domain.LocaleEN, domain.LocaleEN.OrDefault())
	assert.Equal(t, domain.LocaleVI, domain.Locale("fr").OrDefault())
}

func TestEmailVerification(t *testing.T) {
	t.Parallel()

	acc := &domain.Account{}
	assert.False(t, acc.EmailVerified())

	now := time.Now()
	assert.True(t, acc.MarkEmailVerified(now))
	assert.True(t, acc.EmailVerified())
	assert.False(t, acc.MarkEmailVerified(now.Add(time.Hour)))
	assert.Equal(t, now, *acc.EmailVerifiedAt)
}

func TestNormalizeEmail(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		" Owner@Example.COM ":               "owner@example.com",
		"a.b+tag@mail.vn":                   "a.b+tag@mail.vn",
		"":                                  "",
		"not-an-email":                      "",
		"Name <a@b.com>":                    "",
		"a@localhost":                       "",
		strings.Repeat("a", 250) + "@x.com": "",
	}

	for in, want := range tests {
		got, ok := domain.NormalizeEmail(in)
		assert.Equal(t, want, got, in)
		assert.Equal(t, want != "", ok, in)
	}
}

func TestNormalizePhone(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"0901234567":      "0901234567",
		"+84 90 123 4567": "0901234567",
		"84901234567":     "0901234567",
		"(090) 123-4567":  "0901234567",
		"090.123.45678":   "09012345678",
		"12345":           "",
		"0901234abc":      "",
		"1901234567":      "",
		"090123456789":    "",
	}

	for in, want := range tests {
		got, ok := domain.NormalizePhone(in)
		assert.Equal(t, want, got, in)
		assert.Equal(t, want != "", ok, in)
	}
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

	key := domain.OTPKey(domain.OTPEmailVerification, "acc-1")
	assert.Equal(t, "email_verification:acc-1", key)

	now := time.Now()
	otp := domain.NewOTP(key, code, now)
	assert.True(t, otp.Matches(key, code))
	assert.False(t, otp.Matches(key, "not-it"))
	assert.False(t, otp.Matches(domain.OTPKey(domain.OTPPasswordReset, "acc-1"), code), "bound to its key")
	assert.Equal(t, now.Add(domain.OTPTTL), otp.ExpiresAt)
	assert.Equal(t, now.Add(domain.OTPResendCooldown), otp.ResendAt)
	assert.False(t, otp.Expired(now.Add(time.Minute)))
	assert.True(t, otp.Expired(now.Add(domain.OTPTTL)))
	assert.NotEqual(t, domain.HashOTP(key, code), code)
}
