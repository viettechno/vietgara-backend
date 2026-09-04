// Package domain contains pure business types for the Identity & Access
// module. This layer MUST NOT import any framework, database driver, or
// gRPC package -- see Coding_Standards.docx Section 2.1.
package domain

import (
	"time"
)

// AuthProvider identifies the authentication method an account was
// registered with (FR-IAM-01, FR-IAM-02).
type AuthProvider string

// Valid AuthProvider values.
const (
	AuthProviderEmail    AuthProvider = "email"
	AuthProviderPhone    AuthProvider = "phone"
	AuthProviderGoogle   AuthProvider = "google"
	AuthProviderFacebook AuthProvider = "facebook"
)

// IsValid reports whether p is one of the defined AuthProvider values.
func (p AuthProvider) IsValid() bool {
	switch p {
	case AuthProviderEmail, AuthProviderPhone, AuthProviderGoogle, AuthProviderFacebook:
		return true
	}

	return false
}

// AccountStatus is the lifecycle status of an account.
type AccountStatus string

// Valid AccountStatus values.
const (
	AccountStatusActive    AccountStatus = "active"
	AccountStatusInactive  AccountStatus = "inactive"
	AccountStatusSuspended AccountStatus = "suspended"
)

// IsValid reports whether s is one of the defined AccountStatus values.
func (s AccountStatus) IsValid() bool {
	switch s {
	case AccountStatusActive, AccountStatusInactive, AccountStatusSuspended:
		return true
	}

	return false
}

// Account is the login identity. It may own multiple garages (see the
// Tenant module) -- Account itself carries no garage_id and is not subject
// to Row-Level Security, since it sits above the tenant level.
// See: Database Design Section 2.1, Decision Log DEC-003.
type Account struct {
	ID                  string
	Email               *string
	Phone               *string
	AuthProvider        AuthProvider
	IdentityPlatformUID *string
	FullName            string
	PasswordHash        *string // only set for AuthProviderEmail
	Status              AccountStatus
	CreatedAt           time.Time
	UpdatedAt           time.Time
}
