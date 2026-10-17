package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/identity/domain"
)

// RFC 6238 Appendix B: SHA-1 secret "12345678901234567890", 8-digit codes;
// the 6-digit codes are their last six digits.
func TestTOTPCodeRFC6238Vectors(t *testing.T) {
	t.Parallel()

	secret := []byte("12345678901234567890")

	for unix, want := range map[int64]string{59: "287082", 1111111109: "081804", 1111111111: "050471", 1234567890: "005924", 2000000000: "279037"} {
		got := domain.TOTPCode(secret, domain.TOTPStep(time.Unix(unix, 0)))
		assert.Equal(t, want, got, "t=%d", unix)
	}
}

func TestMatchTOTP(t *testing.T) {
	t.Parallel()

	secret := domain.NewTOTPSecret()
	now := time.Unix(1_800_000_000, 0)
	step := domain.TOTPStep(now)

	for delta, ok := range map[int64]bool{0: true, -1: true, 1: true, -2: false, 2: false} {
		matched, found := domain.MatchTOTP(secret, domain.TOTPCode(secret, step+delta), now)
		assert.Equal(t, ok, found, "delta %d", delta)

		if ok {
			assert.Equal(t, step+delta, matched)
		}
	}

	_, found := domain.MatchTOTP(secret, " "+domain.TOTPCode(secret, step)[:3]+" "+domain.TOTPCode(secret, step)[3:], now)
	assert.True(t, found, "spaces are ignored")

	for _, bad := range []string{"", "12345", "1234567", "abcdef"} {
		_, found := domain.MatchTOTP(secret, bad, now)
		assert.False(t, found, bad)
	}
}

func TestOTPAuthURI(t *testing.T) {
	t.Parallel()

	uri := domain.OTPAuthURI([]byte("12345678901234567890"), "owner@example.com")

	assert.Contains(t, uri, "otpauth://totp/VietGara:owner@example.com?")
	assert.Contains(t, uri, "secret=GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ")
	assert.Contains(t, uri, "issuer=VietGara")
	assert.Contains(t, uri, "digits=6")
	assert.Contains(t, uri, "period=30")
}

func TestRecoveryCodes(t *testing.T) {
	t.Parallel()

	codes := domain.NewRecoveryCodes()
	require.Len(t, codes, domain.RecoveryCodeCount)

	seen := map[string]bool{}

	for _, code := range codes {
		require.Regexp(t, `^[A-Z2-9]{5}-[A-Z2-9]{5}$`, code)
		assert.False(t, seen[code], "duplicate")
		seen[code] = true

		normalized, ok := domain.NormalizeRecoveryCode(" " + code[:3] + "-" + code[3:] + " ")
		require.True(t, ok)
		assert.Equal(t, domain.HashRecoveryCode(normalized), domain.HashRecoveryCode(code[:5]+code[6:]))
	}

	for _, bad := range []string{"", "123456", "ABCDE-FGHI0", "ABCDE-FGHIJK"} {
		_, ok := domain.NormalizeRecoveryCode(bad)
		assert.False(t, ok, bad)
	}

	lower, ok := domain.NormalizeRecoveryCode("k7qx2-m9ptr")
	require.True(t, ok)
	assert.Equal(t, "K7QX2M9PTR", lower)
}
