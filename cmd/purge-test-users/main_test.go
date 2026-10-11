package main

import (
	"context"
	"errors"
	"testing"
	"time"

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

type fakeTestUserRepository struct {
	events        *[]string
	recipients    []string
	listErr       error
	deleteErr     error
	deletedUsers  int64
	listedDomain  string
	deletedDomain string
	listedCutoff  time.Time
	deletedCutoff time.Time
}

func (f *fakeTestUserRepository) ListTestUserEmails(_ context.Context, domain string, cutoff time.Time) ([]string, error) {
	*f.events = append(*f.events, "list")
	f.listedDomain = domain
	f.listedCutoff = cutoff

	return f.recipients, f.listErr
}

func (f *fakeTestUserRepository) DeleteTestUsers(_ context.Context, domain string, cutoff time.Time) (int64, error) {
	*f.events = append(*f.events, "delete-users")
	f.deletedDomain = domain
	f.deletedCutoff = cutoff

	return f.deletedUsers, f.deleteErr
}

type fakeMailpitDeleter struct {
	events     *[]string
	recipients []string
	deleteErr  error
	deleted    int
}

func (f *fakeMailpitDeleter) DeleteMessagesForRecipients(_ context.Context, recipients []string) (int, error) {
	*f.events = append(*f.events, "delete-mail")
	f.recipients = append([]string(nil), recipients...)

	return f.deleted, f.deleteErr
}

func TestPurgeTestUsersDeletesMailBeforeUsersWithSameCutoff(t *testing.T) {
	t.Parallel()

	events := []string{}
	cutoff := time.Date(2026, time.June, 1, 12, 0, 0, 0, time.UTC)
	users := &fakeTestUserRepository{
		events:       &events,
		recipients:   []string{"old@blackbox.vietgara.test"},
		deletedUsers: 1,
	}
	mail := &fakeMailpitDeleter{events: &events, deleted: 3}

	deletedUsers, deletedMessages, err := purgeTestUsers(
		t.Context(), users, mail, "blackbox.vietgara.test", cutoff,
	)
	require.NoError(t, err)
	assert.Equal(t, int64(1), deletedUsers)
	assert.Equal(t, 3, deletedMessages)
	assert.Equal(t, []string{"list", "delete-mail", "delete-users"}, events)
	assert.Equal(t, users.recipients, mail.recipients)
	assert.Equal(t, "blackbox.vietgara.test", users.listedDomain)
	assert.Equal(t, users.listedDomain, users.deletedDomain)
	assert.Equal(t, cutoff, users.listedCutoff)
	assert.Equal(t, users.listedCutoff, users.deletedCutoff)
}

func TestPurgeTestUsersDoesNotDeleteUsersWhenMailpitFails(t *testing.T) {
	t.Parallel()

	events := []string{}
	users := &fakeTestUserRepository{
		events:     &events,
		recipients: []string{"old@blackbox.vietgara.test"},
	}
	mail := &fakeMailpitDeleter{
		events:    &events,
		deleteErr: errors.New("Mailpit unavailable"),
	}

	deletedUsers, deletedMessages, err := purgeTestUsers(
		t.Context(), users, mail, "blackbox.vietgara.test", time.Now(),
	)
	require.ErrorContains(t, err, "Mailpit unavailable")
	assert.Zero(t, deletedUsers)
	assert.Zero(t, deletedMessages)
	assert.Equal(t, []string{"list", "delete-mail"}, events)
}

func TestPurgeTestUsersDoesNotContactMailpitWhenListingFails(t *testing.T) {
	t.Parallel()

	events := []string{}
	users := &fakeTestUserRepository{
		events:  &events,
		listErr: errors.New("database unavailable"),
	}
	mail := &fakeMailpitDeleter{events: &events}

	_, _, err := purgeTestUsers(
		t.Context(), users, mail, "blackbox.vietgara.test", time.Now(),
	)
	require.ErrorContains(t, err, "database unavailable")
	assert.Equal(t, []string{"list"}, events)
}
