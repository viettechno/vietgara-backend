// Package domain contains pure business types for the Identity & Access
// module. This layer MUST NOT import any framework, database driver, or
// gRPC package -- see Coding_Standards.docx Section 2.1.
package domain

import (
	"strings"
	"time"
	"unicode/utf8"
)

// AuthProvider identifies how a user signs in (FR-IAM-01/02): e-mail +
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

// UserStatus is the lifecycle status of a user.
type UserStatus string

// Valid UserStatus values.
const (
	UserStatusActive    UserStatus = "active"
	UserStatusSuspended UserStatus = "suspended"
)

// IsValid reports whether s is one of the defined UserStatus values.
func (s UserStatus) IsValid() bool {
	return s == UserStatusActive || s == UserStatusSuspended
}

// Locale is the user's interface and e-mail language (NFR-US-01).
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

// NameSource says where a user's display name came from (FR-IAM-12).
type NameSource string

// Valid NameSource values.
const (
	// NameSourceGoogle: taken from the Google profile; a later Google sign-in
	// may refresh it.
	NameSourceGoogle NameSource = "google"
	// NameSourceUser: typed by the user; Google sign-in never overwrites it.
	NameSourceUser NameSource = "user"
)

// OrDefault returns s, or NameSourceUser when s is not a valid source.
func (s NameSource) OrDefault() NameSource {
	if s == NameSourceGoogle {
		return NameSourceGoogle
	}

	return NameSourceUser
}

// User is the login identity. It may own multiple garages and be staff
// of others (see the Tenant module) -- User itself carries no garage_id
// and is not subject to Row-Level Security, since it sits above the tenant
// level. See: Database Design Section 2.1.
type User struct {
	ID    string
	Email string
	Phone *string
	// GoogleSubject is Google's stable user id (the identity_platform_uid
	// column), set for users that signed in with Google.
	GoogleSubject *string
	AuthProvider  AuthProvider
	FullName      string
	// FullNameSource is where FullName came from: Google at first sign-in,
	// or the user (FR-IAM-12).
	FullNameSource  NameSource
	PasswordHash    *string // only set for users with a password
	Status          UserStatus
	EmailVerifiedAt *time.Time
	Locale          Locale
	// PlatformAdmin is loaded from platform_admins.
	PlatformAdmin bool
	// MFAEnabled is true when the user confirmed a TOTP second factor.
	MFAEnabled bool
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// EmailVerified reports whether the e-mail OTP was confirmed.
func (a *User) EmailVerified() bool {
	return a.EmailVerifiedAt != nil
}

// MarkEmailVerified records verification at now; it reports false when the
// e-mail was already verified.
func (a *User) MarkEmailVerified(now time.Time) bool {
	if a.EmailVerified() {
		return false
	}

	a.EmailVerifiedAt = &now

	return true
}

// Limits of user fields.
const (
	MaxFullNameLength = 255
	MinPasswordLength = 8
	// MaxPasswordBytes is bcrypt's input limit.
	MaxPasswordBytes = 72
)

// NormalizeFullName trims a display name and reports whether it is valid.
func NormalizeFullName(raw string) (string, bool) {
	name := strings.Join(strings.Fields(raw), " ")

	return name, name != "" && utf8.RuneCountInString(name) <= MaxFullNameLength
}

// ValidPassword reports whether password meets the policy.
func ValidPassword(password string) bool {
	return utf8.RuneCountInString(password) >= MinPasswordLength && len(password) <= MaxPasswordBytes
}
