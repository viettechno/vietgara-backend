package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/platform/testusers"
)

func TestConfirmedDomain(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		configured   string
		confirmation string
		want         string
		wantErr      bool
	}{
		{
			name:         "exact reserved domain is accepted",
			configured:   "blackbox.vietgara.test",
			confirmation: "blackbox.vietgara.test",
			want:         "blackbox.vietgara.test",
		},
		{
			name:    "unset domain disables purge",
			wantErr: true,
		},
		{
			name:         "non-reserved domain is rejected",
			configured:   "vietgara.com",
			confirmation: "vietgara.com",
			wantErr:      true,
		},
		{
			name:       "missing confirmation is rejected",
			configured: "blackbox.vietgara.test",
			wantErr:    true,
		},
		{
			name:         "parent domain confirmation is rejected",
			configured:   "blackbox.vietgara.test",
			confirmation: "vietgara.test",
			wantErr:      true,
		},
		{
			name:         "non-canonical confirmation is rejected",
			configured:   "BLACKBOX.VIETGARA.TEST",
			confirmation: "BLACKBOX.VIETGARA.TEST",
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			env := map[string]string{}
			if tt.configured != "" {
				env["TEST_USERS_EMAIL_DOMAIN"] = tt.configured
			}

			got, err := confirmedDomain(func(key string) (string, bool) {
				value, ok := env[key]
				return value, ok
			}, tt.confirmation)

			if tt.wantErr {
				require.Error(t, err)
				assert.True(t, got.IsZero())

				return
			}

			require.NoError(t, err)
			want, err := testusers.Parse(tt.want)
			require.NoError(t, err)
			assert.Equal(t, want, got)
		})
	}
}
