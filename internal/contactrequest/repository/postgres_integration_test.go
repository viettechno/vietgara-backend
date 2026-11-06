//go:build integration

package repository_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/contactrequest/domain"
	"github.com/viettechno/vietgara-backend/internal/contactrequest/repository"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platform/db/pgtest"
)

func request(hash string) *domain.Request {
	return &domain.Request{
		ID: uuid.NewString(), Name: "Nguyễn Văn A", Phone: "0938038621", Locale: "vi",
		ConsentAt: time.Now().UTC(), SourceIPHash: hash, Status: domain.StatusNew,
	}
}

// TestContactRequestsUnderTheApplicationRole stores and counts requests as the
// application role, and checks what that role may not do: change a request
// other than its status, or delete one.
func TestContactRequestsUnderTheApplicationRole(t *testing.T) {
	pg := pgtest.StartMigrated(t)
	ctx := t.Context()
	pool := pg.AppPool(t)
	repo := repository.NewPostgresRepository(pool)

	first := request("hash-a")
	first.GarageName, first.Email, first.Message, first.UserAgent = "Gara Minh Phát", "a@example.com", "Demo", "Mozilla/5.0"
	require.NoError(t, repo.Create(ctx, first))
	assert.WithinDuration(t, time.Now(), first.CreatedAt, time.Minute, "the creation time is read back")

	require.NoError(t, repo.Create(ctx, request("hash-a")), "optional fields may be empty")
	require.NoError(t, repo.Create(ctx, request("hash-b")))

	n, err := repo.CountSince(ctx, "hash-a", time.Now().Add(-time.Hour))
	require.NoError(t, err)
	assert.Equal(t, 2, n, "only the requests of that source")

	n, err = repo.CountSince(ctx, "hash-a", time.Now().Add(time.Hour))
	require.NoError(t, err)
	assert.Zero(t, n, "requests older than the window are not counted")

	q := platformdb.QuerierFromContext(ctx, pool)

	_, err = q.Exec(ctx, `UPDATE contact_requests SET status = 'handled' WHERE id = $1`, first.ID)
	require.NoError(t, err, "support may mark a request handled")

	_, err = q.Exec(ctx, `UPDATE contact_requests SET status = 'archived' WHERE id = $1`, first.ID)
	require.ErrorContains(t, err, "contact_requests_status_check", "the status is one of the two values")

	_, err = q.Exec(ctx, `UPDATE contact_requests SET name = 'Someone else' WHERE id = $1`, first.ID)
	require.ErrorContains(t, err, "permission denied", "the application role may not rewrite a request")

	_, err = q.Exec(ctx, `DELETE FROM contact_requests WHERE id = $1`, first.ID)
	require.ErrorContains(t, err, "permission denied", "the application role may not delete a request")

	bad := request("hash-c")
	bad.Locale = "fr"
	require.ErrorContains(t, repo.Create(ctx, bad), "contact_requests_locale_check")
}
