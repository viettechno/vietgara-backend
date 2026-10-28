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

var (
	errTestMailpitUnavailable  = errors.New("mailpit unavailable")
	errTestDatabaseUnavailable = errors.New("database unavailable")
)

func TestConfirmedDomain(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		configured   string
		appEnv       string
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
			name:         "production refuses to purge",
			configured:   "blackbox.vietgara.test",
			appEnv:       "production",
			confirmation: "blackbox.vietgara.test",
			wantErr:      true,
		},
		{
			name:         "staging may purge",
			configured:   "blackbox.vietgara.test",
			appEnv:       "staging",
			confirmation: "blackbox.vietgara.test",
			want:         "blackbox.vietgara.test",
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

			if tt.appEnv != "" {
				env["APP_ENV"] = tt.appEnv
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
	garageIDs     []string
	listErr       error
	garageListErr error
	deleteErr     error
	deletedUsers  int64
	listedDomain  string
	deletedDomain string
	listedCutoff  time.Time
	garageCutoff  time.Time
	deletedCutoff time.Time
}

func (f *fakeTestUserRepository) ListTestUserEmails(_ context.Context, domain string, cutoff time.Time) ([]string, error) {
	*f.events = append(*f.events, "list")
	f.listedDomain = domain
	f.listedCutoff = cutoff

	return f.recipients, f.listErr
}

func (f *fakeTestUserRepository) ListTestUserGarageIDs(_ context.Context, domain string, cutoff time.Time) ([]string, error) {
	*f.events = append(*f.events, "list-garages")
	f.listedDomain = domain
	f.garageCutoff = cutoff

	return f.garageIDs, f.garageListErr
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

type fakeUploadedFileDeleter struct {
	events    *[]string
	prefixes  []string
	deleted   int64
	deleteErr error
}

func (f *fakeUploadedFileDeleter) DeletePrefix(_ context.Context, prefix string) (int64, error) {
	*f.events = append(*f.events, "delete-files")
	f.prefixes = append(f.prefixes, prefix)

	return f.deleted, f.deleteErr
}

func TestPurgeTestUsersDeletesMailAndFilesBeforeUsersWithSameCutoff(t *testing.T) {
	t.Parallel()

	events := []string{}
	cutoff := time.Date(2026, time.June, 1, 12, 0, 0, 0, time.UTC)
	users := &fakeTestUserRepository{
		events:       &events,
		recipients:   []string{"old@blackbox.vietgara.test"},
		garageIDs:    []string{"garage-1", "garage-2"},
		deletedUsers: 1,
	}
	mail := &fakeMailpitDeleter{events: &events, deleted: 3}
	files := &fakeUploadedFileDeleter{events: &events, deleted: 4}

	deletedUsers, deletedMessages, deletedFiles, err := purgeTestUsers(
		t.Context(), users, mail, files, "blackbox.vietgara.test", cutoff,
	)
	require.NoError(t, err)
	assert.Equal(t, int64(1), deletedUsers)
	assert.Equal(t, 3, deletedMessages)
	assert.Equal(t, int64(8), deletedFiles)
	assert.Equal(t, []string{"list", "list-garages", "delete-mail", "delete-files", "delete-files", "delete-users"}, events)
	assert.Equal(t, []string{"garages/garage-1/", "garages/garage-2/"}, files.prefixes)
	assert.Equal(t, users.recipients, mail.recipients)
	assert.Equal(t, "blackbox.vietgara.test", users.listedDomain)
	assert.Equal(t, users.listedDomain, users.deletedDomain)
	assert.Equal(t, cutoff, users.listedCutoff)
	assert.Equal(t, cutoff, users.garageCutoff)
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
		deleteErr: errTestMailpitUnavailable,
	}
	files := &fakeUploadedFileDeleter{events: &events}

	deletedUsers, deletedMessages, deletedFiles, err := purgeTestUsers(
		t.Context(), users, mail, files, "blackbox.vietgara.test", time.Now(),
	)
	require.ErrorContains(t, err, "mailpit unavailable")
	assert.Zero(t, deletedUsers)
	assert.Zero(t, deletedMessages)
	assert.Zero(t, deletedFiles)
	assert.Equal(t, []string{"list", "list-garages", "delete-mail"}, events)
}

func TestPurgeTestUsersDoesNotContactMailpitWhenListingFails(t *testing.T) {
	t.Parallel()

	events := []string{}
	users := &fakeTestUserRepository{
		events:  &events,
		listErr: errTestDatabaseUnavailable,
	}
	mail := &fakeMailpitDeleter{events: &events}
	files := &fakeUploadedFileDeleter{events: &events}

	_, _, _, err := purgeTestUsers(
		t.Context(), users, mail, files, "blackbox.vietgara.test", time.Now(),
	)
	require.ErrorContains(t, err, "database unavailable")
	assert.Equal(t, []string{"list"}, events)
}

func TestPurgeTestUsersDoesNotDeleteAnythingWhenGarageListingFails(t *testing.T) {
	t.Parallel()

	events := []string{}
	users := &fakeTestUserRepository{
		events:        &events,
		garageListErr: errTestDatabaseUnavailable,
	}
	mail := &fakeMailpitDeleter{events: &events}
	files := &fakeUploadedFileDeleter{events: &events}

	_, _, _, err := purgeTestUsers(
		t.Context(), users, mail, files, "blackbox.vietgara.test", time.Now(),
	)
	require.ErrorContains(t, err, "database unavailable")
	assert.Equal(t, []string{"list", "list-garages"}, events)
}

func TestPurgeTestUsersDoesNotDeleteUsersWhenFileCleanupFails(t *testing.T) {
	t.Parallel()

	events := []string{}
	users := &fakeTestUserRepository{
		events:     &events,
		recipients: []string{"old@blackbox.vietgara.test"},
		garageIDs:  []string{"garage-1"},
	}
	mail := &fakeMailpitDeleter{events: &events}
	files := &fakeUploadedFileDeleter{
		events:    &events,
		deleteErr: errTestDatabaseUnavailable,
	}

	deletedUsers, _, _, err := purgeTestUsers(
		t.Context(), users, mail, files, "blackbox.vietgara.test", time.Now(),
	)
	require.ErrorContains(t, err, "delete uploaded files for garage garage-1")
	assert.Zero(t, deletedUsers)
	assert.Equal(t, []string{"list", "list-garages", "delete-mail", "delete-files"}, events)
}
