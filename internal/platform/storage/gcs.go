package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"

	gcs "cloud.google.com/go/storage"
	"google.golang.org/api/iterator"
)

// GCSStore keeps objects in a Cloud Storage bucket (production).
type GCSStore struct {
	bucket  *gcs.BucketHandle
	baseURL string
}

// NewGCS connects to bucket with Application Default Credentials; baseURL
// defaults to https://storage.googleapis.com/<bucket>.
func NewGCS(ctx context.Context, bucket, baseURL string) (*GCSStore, error) {
	client, err := gcs.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: cloud storage client: %w", err)
	}

	return newGCSStore(client.Bucket(bucket), bucket, baseURL), nil
}

func newGCSStore(bucket *gcs.BucketHandle, name, baseURL string) *GCSStore {
	if baseURL == "" {
		baseURL = "https://storage.googleapis.com/" + name
	}

	return &GCSStore{bucket: bucket, baseURL: strings.TrimSuffix(baseURL, "/")}
}

// Put uploads data as key.
func (s *GCSStore) Put(ctx context.Context, key, contentType string, data []byte) (string, error) {
	key, err := cleanKey(key)
	if err != nil {
		return "", err
	}

	w := s.bucket.Object(key).NewWriter(ctx)
	w.ContentType = contentType
	w.CacheControl = "public, max-age=31536000, immutable"

	if _, err := w.Write(data); err != nil {
		_ = w.Close()

		return "", fmt.Errorf("storage: upload %s: %w", key, err)
	}

	if err := w.Close(); err != nil {
		return "", fmt.Errorf("storage: upload %s: %w", key, err)
	}

	return s.baseURL + "/" + key, nil
}

// Delete removes key.
func (s *GCSStore) Delete(ctx context.Context, key string) error {
	key, err := cleanKey(key)
	if err != nil {
		return err
	}

	if err := s.bucket.Object(key).Delete(ctx); err != nil && !errors.Is(err, gcs.ErrObjectNotExist) {
		return fmt.Errorf("storage: delete %s: %w", key, err)
	}

	return nil
}

// DeletePrefix removes every object under prefix.
func (s *GCSStore) DeletePrefix(ctx context.Context, prefix string) (int64, error) {
	prefix, err := cleanPrefix(prefix)
	if err != nil {
		return 0, err
	}

	prefix += "/"

	objects := s.bucket.Objects(ctx, &gcs.Query{Prefix: prefix})

	var deleted int64

	for {
		attrs, err := objects.Next()
		if errors.Is(err, iterator.Done) {
			return deleted, nil
		}

		if err != nil {
			return deleted, fmt.Errorf("storage: list prefix %s: %w", prefix, err)
		}

		if err := s.bucket.Object(attrs.Name).Delete(ctx); err != nil {
			if errors.Is(err, gcs.ErrObjectNotExist) {
				continue
			}

			return deleted, fmt.Errorf("storage: delete %s: %w", attrs.Name, err)
		}

		deleted++
	}
}

// KeyFromURL strips the base URL.
func (s *GCSStore) KeyFromURL(url string) (string, bool) {
	key, found := strings.CutPrefix(url, s.baseURL+"/")
	if !found {
		return "", false
	}

	if _, err := cleanKey(key); err != nil {
		return "", false
	}

	return key, true
}
