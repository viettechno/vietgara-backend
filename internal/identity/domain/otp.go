package domain

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"math/big"
	"time"
)

// OTP policy (FR-IAM-01, FR-IAM-04): 6 digits, valid for 2 minutes, at most
// 5 wrong attempts, and a new code can be requested after 30 seconds.
const (
	OTPLength         = 6
	OTPTTL            = 2 * time.Minute
	OTPResendCooldown = 30 * time.Second
	OTPMaxAttempts    = 5
	// ResetTokenTTL is how long a verified password-reset OTP may be
	// exchanged for a new password.
	ResetTokenTTL = 10 * time.Minute
)

// OTPPurpose separates the OTPs of different flows.
type OTPPurpose string

// OTP purposes.
const (
	OTPEmailVerification OTPPurpose = "email_verification"
	OTPPasswordReset     OTPPurpose = "password_reset"
	// OTPCustomerLink confirms a garage's request to link a customer
	// profile with the user (FR-CUS-06); its subject is the link request.
	OTPCustomerLink OTPPurpose = "customer_link"
)

// OTP is an issued one-time password. Only a hash of the code is kept.
type OTP struct {
	CodeHash  string
	ExpiresAt time.Time
	ResendAt  time.Time
	Attempts  int
}

// OTPKey identifies the OTP of one user and purpose.
func OTPKey(purpose OTPPurpose, userID string) string {
	return string(purpose) + ":" + userID
}

// NewOTPCode returns a random 6-digit code.
func NewOTPCode() string {
	n, _ := rand.Int(rand.Reader, big.NewInt(1_000_000)) // crypto/rand never fails (Go 1.24+)

	s := n.String()
	for len(s) < OTPLength {
		s = "0" + s
	}

	return s
}

// HashOTP is the HMAC of code, bound to key, under secret. Bound to key, a
// stored hash is useless for any other user or purpose. A 6-digit code has
// only a million values, so without the secret a plain hash could be
// reversed by trying them all (SR-21).
func HashOTP(secret []byte, key, code string) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte("otp|" + key + "|" + code))

	return hex.EncodeToString(mac.Sum(nil))
}

// NewOTP issues code at now.
func NewOTP(secret []byte, key, code string, now time.Time) *OTP {
	return &OTP{CodeHash: HashOTP(secret, key, code), ExpiresAt: now.Add(OTPTTL), ResendAt: now.Add(OTPResendCooldown)}
}

// Matches compares code in constant time.
func (o *OTP) Matches(secret []byte, key, code string) bool {
	return subtle.ConstantTimeCompare([]byte(o.CodeHash), []byte(HashOTP(secret, key, code))) == 1
}

// Expired reports whether the OTP can no longer be used at now.
func (o *OTP) Expired(now time.Time) bool {
	return !now.Before(o.ExpiresAt)
}
