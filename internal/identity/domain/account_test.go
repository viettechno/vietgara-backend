package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/viettechno/vietgara-backend/internal/identity/domain"
)

func TestAuthProvider_IsValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		provider domain.AuthProvider
		want     bool
	}{
		{name: "email", provider: domain.AuthProviderEmail, want: true},
		{name: "google", provider: domain.AuthProviderGoogle, want: true},
		{name: "facebook", provider: domain.AuthProviderFacebook, want: true},
		{name: "phone", provider: domain.AuthProviderPhone, want: true},
		{name: "empty", provider: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, tt.provider.IsValid())
		})
	}
}

func TestAccountStatus_IsValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status domain.AccountStatus
		want   bool
	}{
		{name: "active", status: domain.AccountStatusActive, want: true},
		{name: "inactive", status: domain.AccountStatusInactive, want: true},
		{name: "suspended", status: domain.AccountStatusSuspended, want: true},
		{name: "unknown", status: domain.AccountStatus("deleted"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, tt.status.IsValid())
		})
	}
}
