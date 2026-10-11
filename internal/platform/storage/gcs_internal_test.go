package storage

import (
	"testing"
	"time"

	gcs "cloud.google.com/go/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/option"
)

// TestGCSStoreSignedURL checks the signing plumbing; the signature itself
// is made by the Cloud Storage client (V4, through IAM signBlob on GKE).
func TestGCSStoreSignedURL(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 12, 8, 0, 0, 0, time.UTC)
	store := &GCSStore{
		private: nil,
		now:     func() time.Time { return now },
	}

	_, _, err := store.SignedURL(t.Context(), "garages/g1/export/e1/a.csv", time.Minute)
	require.ErrorIs(t, err, ErrPrivateNotConfigured)

	var signed struct {
		key     string
		expires time.Time
	}

	client, err := gcs.NewClient(t.Context(), option.WithoutAuthentication())
	require.NoError(t, err)

	store = newGCSStore(client.Bucket("logos"), "logos", "")
	store.now = func() time.Time { return now }
	store.private = client.Bucket("private-files")
	store.sign = func(key string, expires time.Time) (string, error) {
		signed.key, signed.expires = key, expires

		return "https://signed.example/" + key, nil
	}

	url, expires, err := store.SignedURL(t.Context(), "garages/g1/export/e1/a.csv", 5*time.Minute)
	require.NoError(t, err)
	assert.Equal(t, "https://signed.example/garages/g1/export/e1/a.csv", url)
	assert.Equal(t, now.Add(5*time.Minute), expires)
	assert.Equal(t, "garages/g1/export/e1/a.csv", signed.key)
	assert.Equal(t, expires, signed.expires)

	store.sign = func(string, time.Time) (string, error) { return "", assert.AnError }
	_, _, err = store.SignedURL(t.Context(), "garages/g1/export/e1/a.csv", time.Minute)
	require.ErrorIs(t, err, assert.AnError)

	_, _, err = store.SignedURL(t.Context(), "_private/x", time.Minute)
	require.ErrorIs(t, err, ErrInvalidKey)
}
