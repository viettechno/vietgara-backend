package domain

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // RFC 6238 default; authenticator apps expect HMAC-SHA1
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// MFA policy (FR-IAM-09, NFR-SE-04): an optional TOTP second factor for
// users that sign in with e-mail and password.
const (
	// TOTPPeriod is the length of one TOTP step.
	TOTPPeriod = 30 * time.Second
	// TOTPDigits is the length of a TOTP code.
	TOTPDigits = 6
	// TOTPSkew is how many steps before and after the current one are
	// accepted, to tolerate clock drift between phone and server.
	TOTPSkew = 1
	// MFAIssuer names the app in the authenticator.
	MFAIssuer = "VietGara"
	// MFAChallengeTTL is how long a sign-in may wait for the second factor.
	MFAChallengeTTL = 5 * time.Minute
	// MFAMaxFailures wrong codes lock the user's second-factor checks for
	// MFALockout.
	MFAMaxFailures = 5
	MFALockout     = 15 * time.Minute
	// RecoveryCodeCount codes are issued at enrolment and on regeneration.
	RecoveryCodeCount = 10
)

// MFA is a user's second factor. The secret is kept encrypted; enabling it
// needs a code that proves the authenticator app holds the secret.
type MFA struct {
	UserID          string
	SecretEncrypted []byte
	// EnabledAt is nil while the enrolment is pending confirmation.
	EnabledAt *time.Time
	// LastUsedStep is the newest TOTP step accepted, so a code works once.
	LastUsedStep int64
}

// Enabled reports whether the enrolment was confirmed.
func (m *MFA) Enabled() bool {
	return m.EnabledAt != nil
}

// NewTOTPSecret returns a random 160-bit secret.
func NewTOTPSecret() []byte {
	secret := make([]byte, 20)
	_, _ = rand.Read(secret) // crypto/rand.Read never fails (Go 1.24+)

	return secret
}

var base32NoPadding = base32.StdEncoding.WithPadding(base32.NoPadding)

// EncodeTOTPSecret is the base32 form the authenticator apps take.
func EncodeTOTPSecret(secret []byte) string {
	return base32NoPadding.EncodeToString(secret)
}

// OTPAuthURI is the otpauth:// URI an authenticator app reads from a QR
// code.
func OTPAuthURI(secret []byte, accountEmail string) string {
	label := url.PathEscape(MFAIssuer + ":" + accountEmail)
	query := url.Values{
		"secret":    {EncodeTOTPSecret(secret)},
		"issuer":    {MFAIssuer},
		"algorithm": {"SHA1"},
		"digits":    {strconv.Itoa(TOTPDigits)},
		"period":    {strconv.Itoa(int(TOTPPeriod.Seconds()))},
	}

	return "otpauth://totp/" + label + "?" + query.Encode()
}

// TOTPStep is the step number of t.
func TOTPStep(t time.Time) int64 {
	return t.Unix() / int64(TOTPPeriod.Seconds())
}

// TOTPCode computes the code of a step (RFC 4226 on the step counter).
func TOTPCode(secret []byte, step int64) string {
	mac := hmac.New(sha1.New, secret)

	var counter [8]byte

	binary.BigEndian.PutUint64(counter[:], uint64(step)) //nolint:gosec // step is positive
	mac.Write(counter[:])
	sum := mac.Sum(nil)

	offset := sum[len(sum)-1] & 0x0f
	value := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff

	return fmt.Sprintf("%0*d", TOTPDigits, value%1_000_000)
}

// MatchTOTP returns the step whose code equals code within the accepted
// skew around now, or false.
func MatchTOTP(secret []byte, code string, now time.Time) (int64, bool) {
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	if len(code) != TOTPDigits {
		return 0, false
	}

	current := TOTPStep(now)
	matched, found := int64(0), false

	// Every step is compared, so timing does not reveal which one matched.
	for step := current - TOTPSkew; step <= current+TOTPSkew; step++ {
		if subtle.ConstantTimeCompare([]byte(TOTPCode(secret, step)), []byte(code)) == 1 {
			matched, found = step, true
		}
	}

	return matched, found
}

// recoveryAlphabet leaves out look-alike characters (0/O, 1/I/L).
const (
	recoveryAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"
	recoveryGroup    = 5
)

// NewRecoveryCodes returns RecoveryCodeCount random codes like "K7QX2-M9PTR".
func NewRecoveryCodes() []string {
	codes := make([]string, RecoveryCodeCount)

	for i := range codes {
		buf := make([]byte, 2*recoveryGroup)
		_, _ = rand.Read(buf)

		for j, b := range buf {
			buf[j] = recoveryAlphabet[int(b)%len(recoveryAlphabet)]
		}

		codes[i] = string(buf[:recoveryGroup]) + "-" + string(buf[recoveryGroup:])
	}

	return codes
}

// NormalizeRecoveryCode upper-cases code and drops separators; it reports
// false when code cannot be a recovery code.
func NormalizeRecoveryCode(code string) (string, bool) {
	code = strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(strings.TrimSpace(code)))
	if len(code) != 2*recoveryGroup {
		return "", false
	}

	for _, c := range code {
		if !strings.ContainsRune(recoveryAlphabet, c) {
			return "", false
		}
	}

	return code, true
}

// HashRecoveryCode is what is stored of a (normalized) recovery code. The
// codes carry 50 bits of randomness and are single-use, so an unsalted
// SHA-256 is enough.
func HashRecoveryCode(normalized string) string {
	sum := sha256.Sum256([]byte(normalized))

	return hex.EncodeToString(sum[:])
}
