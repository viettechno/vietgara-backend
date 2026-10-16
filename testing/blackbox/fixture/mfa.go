package fixture

import (
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // RFC 6238
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"testing"
	"time"
)

const totpPeriod = 30 * time.Second

// Authenticator is a TOTP app (RFC 6238, SHA-1, 6 digits, 30 seconds) holding
// one secret. The backend accepts a step only once and only within a step of
// its clock, so Code hands out the next step that can still be used: the
// current one first, the following one right after, and then it waits for
// the clock to catch up.
type Authenticator struct {
	secret   []byte
	lastStep int64
}

// NewAuthenticator reads the base32 secret EnrollMfa returned.
func NewAuthenticator(tb testing.TB, secret string) *Authenticator {
	tb.Helper()

	raw, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		tb.Fatalf("decode TOTP secret: %v", err)
	}

	return &Authenticator{secret: raw}
}

// Code returns a code the backend has not seen from this authenticator.
func (a *Authenticator) Code(tb testing.TB) string {
	tb.Helper()

	step := max(a.lastStep+1, time.Now().Unix()/int64(totpPeriod.Seconds()))

	// The backend accepts a step one ahead of its clock, not two.
	if wait := time.Until(time.Unix((step-1)*int64(totpPeriod.Seconds()), 0)); wait > 0 {
		tb.Logf("waiting %s for the next TOTP step", wait.Round(time.Second))

		select {
		case <-time.After(wait):
		case <-tb.Context().Done():
			tb.Fatal("cancelled while waiting for the next TOTP step")
		}
	}

	a.lastStep = step

	return a.codeAt(step)
}

// Stale returns the code Code returned last: the backend must refuse it as
// already used.
func (a *Authenticator) Stale() string {
	return a.codeAt(a.lastStep)
}

func (a *Authenticator) codeAt(step int64) string {
	mac := hmac.New(sha1.New, a.secret)

	var counter [8]byte

	binary.BigEndian.PutUint64(counter[:], uint64(step)) //nolint:gosec // positive
	mac.Write(counter[:])
	sum := mac.Sum(nil)

	offset := sum[len(sum)-1] & 0x0f
	value := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff

	return fmt.Sprintf("%06d", value%1_000_000)
}

// EnableMfa turns the second factor on for owner and returns its
// authenticator and recovery codes. The confirming code spends the current
// step.
func EnableMfa(tb testing.TB, owner *Actor) (*Authenticator, []string) {
	tb.Helper()

	enrollment, err := owner.API.EnrollMfa(tb.Context())
	if err != nil {
		tb.Fatalf("enrol MFA: %v", err)
	}

	app := NewAuthenticator(tb, enrollment.GetSecret())

	confirmed, err := owner.API.ConfirmMfaEnrollment(tb.Context(), app.Code(tb))
	if err != nil {
		tb.Fatalf("confirm MFA enrolment: %v", err)
	}

	return app, confirmed.GetRecoveryCodes()
}
