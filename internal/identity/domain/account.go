// Package domain contains pure business types for the Identity & Access
// module. This layer MUST NOT import any framework, database driver, or
// gRPC package -- see Coding_Standards.docx Section 2.1.
package domain

import (
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"
)

// AuthProvider identifies how an account signs in (FR-IAM-01/02): e-mail +
// password, or Google.
type AuthProvider string

// Valid AuthProvider values.
const (
	AuthProviderEmail  AuthProvider = "email"
	AuthProviderGoogle AuthProvider = "google"
)

// IsValid reports whether p is one of the defined AuthProvider values.
func (p AuthProvider) IsValid() bool {
	return p == AuthProviderEmail || p == AuthProviderGoogle
}

// AccountStatus is the lifecycle status of an account.
type AccountStatus string

// Valid AccountStatus values.
const (
	AccountStatusActive    AccountStatus = "active"
	AccountStatusSuspended AccountStatus = "suspended"
)

// IsValid reports whether s is one of the defined AccountStatus values.
func (s AccountStatus) IsValid() bool {
	return s == AccountStatusActive || s == AccountStatusSuspended
}

// Locale is the account's interface and e-mail language (NFR-US-01).
type Locale string

// Valid Locale values; Vietnamese is the default.
const (
	LocaleVI Locale = "vi"
	LocaleEN Locale = "en"
)

// OrDefault returns l, or Vietnamese when l is not a valid locale.
func (l Locale) OrDefault() Locale {
	if l == LocaleEN {
		return LocaleEN
	}

	return LocaleVI
}

// Account is the login identity. It may own multiple garages and be staff
// of others (see the Tenant module) -- Account itself carries no garage_id
// and is not subject to Row-Level Security, since it sits above the tenant
// level. See: Database Design Section 2.1.
type Account struct {
	ID    string
	Email string
	Phone *string
	// GoogleSubject is Google's stable user id (the identity_platform_uid
	// column), set for accounts that signed in with Google.
	GoogleSubject   *string
	AuthProvider    AuthProvider
	FullName        string
	PasswordHash    *string // only set for accounts with a password
	Status          AccountStatus
	EmailVerifiedAt *time.Time
	Locale          Locale
	// PlatformAdmin is loaded from platform_admins.
	PlatformAdmin bool
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// EmailVerified reports whether the e-mail OTP was confirmed.
func (a *Account) EmailVerified() bool {
	return a.EmailVerifiedAt != nil
}

// MarkEmailVerified records verification at now; it reports false when the
// e-mail was already verified.
func (a *Account) MarkEmailVerified(now time.Time) bool {
	if a.EmailVerified() {
		return false
	}

	a.EmailVerifiedAt = &now

	return true
}

// Limits of account fields.
const (
	MaxFullNameLength = 255
	MinPasswordLength = 8
	// MaxPasswordBytes is bcrypt's input limit.
	MaxPasswordBytes = 72
)

// NormalizeEmail lower-cases and trims an e-mail address and reports
// whether it is valid.
func NormalizeEmail(raw string) (string, bool) {
	email := strings.ToLower(strings.TrimSpace(raw))
	if email == "" || len(email) > 255 {
		return "", false
	}

	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email || !strings.Contains(email[strings.LastIndex(email, "@"):], ".") {
		return "", false
	}

	return email, true
}

var phoneFormatting = strings.NewReplacer(" ", "", "-", "", ".", "", "(", "", ")", "")

// NormalizePhone reduces a Vietnamese phone number to its national form
// ("+84 90 123 4567" -> "0901234567") and reports whether it is valid
// (9-10 digits after the leading 0).
func NormalizePhone(raw string) (string, bool) {
	p := phoneFormatting.Replace(strings.TrimSpace(raw))

	switch {
	case strings.HasPrefix(p, "+84"):
		p = "0" + strings.TrimPrefix(p, "+84")
	case strings.HasPrefix(p, "84") && len(p) >= 11:
		p = "0" + strings.TrimPrefix(p, "84")
	}

	if len(p) < 10 || len(p) > 11 || p[0] != '0' {
		return "", false
	}

	for _, r := range p {
		if r < '0' || r > '9' {
			return "", false
		}
	}

	return p, true
}

// NormalizeFullName trims a display name and reports whether it is valid.
func NormalizeFullName(raw string) (string, bool) {
	name := strings.Join(strings.Fields(raw), " ")

	return name, name != "" && utf8.RuneCountInString(name) <= MaxFullNameLength
}

// ValidPassword reports whether password meets the policy.
func ValidPassword(password string) bool {
	return utf8.RuneCountInString(password) >= MinPasswordLength && len(password) <= MaxPasswordBytes
}
