package mailpit_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/platform/mailpit"
)

const testPageSize = 100

func TestNewClient(t *testing.T) {
	t.Parallel()

	credentialURL := (&url.URL{
		Scheme: "http",
		Host:   "mailpit:8025",
		User:   url.User("blackbox"),
	}).String()
	tests := []struct {
		name     string
		rawURL   string
		username string
		password string
		wantErr  bool
	}{
		{name: "HTTP origin", rawURL: "http://mailpit:8025"},
		{name: "HTTPS origin with credentials", rawURL: "https://mailpit.example", username: "blackbox", password: "secret"},
		{name: "path rejected", rawURL: "http://mailpit:8025/api", wantErr: true},
		{name: "URL credentials rejected", rawURL: credentialURL, wantErr: true},
		{name: "partial credentials rejected", rawURL: "http://mailpit:8025", username: "blackbox", wantErr: true},
		{name: "invalid scheme rejected", rawURL: "ftp://mailpit:8025", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := mailpit.NewClient(tt.rawURL, tt.username, tt.password)
			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, got)

				return
			}

			require.NoError(t, err)
			assert.NotNil(t, got)
		})
	}
}

func TestDeleteMessagesForRecipientsPaginatesAndDeletesOnlyFoundIDs(t *testing.T) {
	t.Parallel()

	const (
		alice = "alice@blackbox.vietgara.test"
		bob   = "bob@blackbox.vietgara.test"
	)

	deletedIDs := make(map[string]struct{})
	deleteRequests := 0
	searchRequests := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok {
			t.Error("request did not include basic authentication")
			http.Error(w, "missing auth", http.StatusUnauthorized)

			return
		}

		assert.Equal(t, "blackbox", username)
		assert.Equal(t, "secret", password)

		switch r.Method {
		case http.MethodGet:
			searchRequests++

			assert.Equal(t, "/api/v1/search", r.URL.Path)

			query := r.URL.Query()
			assert.Equal(t, strconv.Itoa(testPageSize), query.Get("limit"))
			recipient := strings.TrimPrefix(query.Get("query"), "to:")
			assert.Contains(t, []string{alice, bob}, recipient)

			start, err := strconv.Atoi(query.Get("start"))
			if err != nil {
				t.Errorf("parse start offset: %v", err)
				http.Error(w, "invalid offset", http.StatusBadRequest)

				return
			}

			messages := make([]struct {
				ID string `json:"ID"`
			}, 0)

			switch {
			case recipient == alice && start == 0:
				for i := range testPageSize {
					messages = append(messages, struct {
						ID string `json:"ID"`
					}{ID: fmt.Sprintf("alice-%03d", i)})
				}
			case recipient == alice && start == testPageSize:
				messages = append(messages, struct {
					ID string `json:"ID"`
				}{ID: "alice-last"})
			case recipient == bob && start == 0:
				messages = append(messages, struct {
					ID string `json:"ID"`
				}{ID: "alice-050"})
			default:
				assert.Fail(t, "unexpected search request", "recipient=%s start=%d", recipient, start)
			}

			if err := json.NewEncoder(w).Encode(struct {
				Messages any `json:"messages"`
			}{Messages: messages}); err != nil {
				t.Errorf("encode search results: %v", err)
			}
		case http.MethodDelete:
			deleteRequests++

			assert.Equal(t, "/api/v1/messages", r.URL.Path)

			var body struct {
				IDs []string `json:"IDs"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode delete request: %v", err)
				http.Error(w, "invalid delete request", http.StatusBadRequest)

				return
			}

			if len(body.IDs) == 0 {
				t.Error("empty Mailpit delete request would delete the entire mailbox")
				http.Error(w, "empty delete request", http.StatusBadRequest)

				return
			}

			for _, id := range body.IDs {
				assert.NotContains(t, deletedIDs, id)
				deletedIDs[id] = struct{}{}
			}

			w.WriteHeader(http.StatusOK)
		default:
			assert.Fail(t, "unexpected HTTP method", r.Method)
			http.Error(w, "unexpected method", http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(server.Close)

	client, err := mailpit.NewClient(server.URL, "blackbox", "secret")
	require.NoError(t, err)

	count, err := client.DeleteMessagesForRecipients(t.Context(), []string{alice, bob})
	require.NoError(t, err)
	assert.Equal(t, 101, count)
	assert.Equal(t, 3, searchRequests)
	assert.Equal(t, 2, deleteRequests)
	assert.Len(t, deletedIDs, 101)
	assert.Contains(t, deletedIDs, "alice-last")
	assert.Contains(t, deletedIDs, "alice-050")
}

func TestDeleteMessagesForRecipientsEmptyListDoesNotCallMailpit(t *testing.T) {
	t.Parallel()

	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true

		http.Error(w, "unexpected request", http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	client, err := mailpit.NewClient(server.URL, "", "")
	require.NoError(t, err)

	count, err := client.DeleteMessagesForRecipients(t.Context(), nil)
	require.NoError(t, err)
	assert.Zero(t, count)
	assert.False(t, called)
}

func TestDeleteMessagesForRecipientsStopsOnMailpitFailure(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)

	client, err := mailpit.NewClient(server.URL, "", "")
	require.NoError(t, err)

	count, err := client.DeleteMessagesForRecipients(t.Context(), []string{"owner@blackbox.vietgara.test"})
	require.Error(t, err)
	assert.Zero(t, count)
	assert.ErrorContains(t, err, "503 Service Unavailable")
}

func TestDeleteMessagesForRecipientsReportsDeleteFailure(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			if _, err := fmt.Fprint(w, `{"messages":[{"ID":"message-1"}]}`); err != nil {
				t.Errorf("write search response: %v", err)
			}

			return
		}

		http.Error(w, "delete rejected", http.StatusForbidden)
	}))
	t.Cleanup(server.Close)

	client, err := mailpit.NewClient(server.URL, "", "")
	require.NoError(t, err)

	count, err := client.DeleteMessagesForRecipients(t.Context(), []string{"owner@blackbox.vietgara.test"})
	require.Error(t, err)
	assert.Zero(t, count)
	require.ErrorContains(t, err, "403 Forbidden")
	require.ErrorContains(t, err, "delete rejected")
}

func TestDeleteMessagesForRecipientsRejectsInvalidRecipientsWithoutRequests(t *testing.T) {
	t.Parallel()

	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true

		http.Error(w, "unexpected request", http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	client, err := mailpit.NewClient(server.URL, "", "")
	require.NoError(t, err)

	_, err = client.DeleteMessagesForRecipients(t.Context(), []string{"not-an-address"})
	require.Error(t, err)
	assert.False(t, called)
}
