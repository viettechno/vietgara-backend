package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	gcs "cloud.google.com/go/storage"
	"google.golang.org/api/iterator"
)

// GCSStore keeps objects in a Cloud Storage bucket (production).
type GCSStore struct {
	bucket  *gcs.BucketHandle
	baseURL string
	// private is the bucket of private objects; nil until configured.
	private *gcs.BucketHandle
	// sign makes a V4 signed GET URL for a private key; replaced in tests.
	sign func(key string, expires time.Time) (string, error)
	now  func() time.Time
}

// NewGCS connects to bucket with Application Default Credentials; baseURL
// defaults to https://storage.googleapis.com/<bucket>.
func NewGCS(ctx context.Context, bucket, baseURL string) (*GCSStore, error) {
	return NewGCSWithPrivate(ctx, bucket, "", baseURL)
}

// NewGCSWithPrivate is NewGCS with a second, non-public bucket for private
// objects, served through V4 signed URLs. Signing needs no key file on GKE:
// the client signs through the IAM Credentials API with the service
// account's Workload Identity, which must be allowed to sign for itself
// (roles/iam.serviceAccountTokenCreator).
func NewGCSWithPrivate(ctx context.Context, bucket, privateBucket, baseURL string) (*GCSStore, error) {
	client, err := gcs.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage: cloud storage client: %w", err)
	}

	store := newGCSStore(client.Bucket(bucket), bucket, baseURL)

	if privateBucket != "" {
		store.private = client.Bucket(privateBucket)
		store.sign = func(key string, expires time.Time) (string, error) {
			return store.private.SignedURL(key, &gcs.SignedURLOptions{
				Scheme: gcs.SigningSchemeV4, Method: "GET", Expires: expires,
			})
		}
	}

	return store, nil
}

func newGCSStore(bucket *gcs.BucketHandle, name, baseURL string) *GCSStore {
	if baseURL == "" {
		baseURL = "https://storage.googleapis.com/" + name
	}

	return &GCSStore{bucket: bucket, baseURL: strings.TrimSuffix(baseURL, "/"), now: time.Now}
}

// Put uploads data as key.
func (s *GCSStore) Put(ctx context.Context, key, contentType string, data []byte) (string, error) {
	key, err := cleanPublicKey(key)
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

// PublicURL returns the URL a public object is served at.
func (s *GCSStore) PublicURL(key string) string { return s.baseURL + "/" + key }

// PutPrivate uploads data to the private bucket.
func (s *GCSStore) PutPrivate(ctx context.Context, key, contentType string, data []byte) error {
	if s.private == nil {
		return ErrPrivateNotConfigured
	}

	key, err := cleanPublicKey(key)
	if err != nil {
		return err
	}

	w := s.private.Object(key).NewWriter(ctx)
	w.ContentType = contentType
	w.CacheControl = "private, no-store"

	if _, err := w.Write(data); err != nil {
		_ = w.Close()

		return fmt.Errorf("storage: upload %s: %w", key, err)
	}

	if err := w.Close(); err != nil {
		return fmt.Errorf("storage: upload %s: %w", key, err)
	}

	return nil
}

// SignedURL returns a V4 signed URL for a private object.
func (s *GCSStore) SignedURL(_ context.Context, key string, ttl time.Duration) (string, time.Time, error) {
	if s.private == nil || s.sign == nil {
		return "", time.Time{}, ErrPrivateNotConfigured
	}

	key, err := cleanPublicKey(key)
	if err != nil {
		return "", time.Time{}, err
	}

	expires := s.now().Add(ttl)

	url, err := s.sign(key, expires)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("storage: sign %s: %w", key, err)
	}

	return url, expires, nil
}

// buckets lists the buckets an object may live in.
func (s *GCSStore) buckets() []*gcs.BucketHandle {
	if s.private == nil {
		return []*gcs.BucketHandle{s.bucket}
	}

	return []*gcs.BucketHandle{s.bucket, s.private}
}

// Delete removes key from both buckets.
func (s *GCSStore) Delete(ctx context.Context, key string) error {
	key, err := cleanPublicKey(key)
	if err != nil {
		return err
	}

	for _, bucket := range s.buckets() {
		if err := bucket.Object(key).Delete(ctx); err != nil && !errors.Is(err, gcs.ErrObjectNotExist) {
			return fmt.Errorf("storage: delete %s: %w", key, err)
		}
	}

	return nil
}

// DeletePrefix removes every object under prefix, in both buckets.
func (s *GCSStore) DeletePrefix(ctx context.Context, prefix string) (int64, error) {
	prefix, err := cleanPrefix(prefix)
	if err != nil {
		return 0, err
	}

	var deleted int64

	for _, bucket := range s.buckets() {
		count, err := deletePrefix(ctx, bucket, prefix+"/")
		deleted += count

		if err != nil {
			return deleted, err
		}
	}

	return deleted, nil
}

func deletePrefix(ctx context.Context, bucket *gcs.BucketHandle, prefix string) (int64, error) {
	objects := bucket.Objects(ctx, &gcs.Query{Prefix: prefix})

	var deleted int64

	for {
		attrs, err := objects.Next()
		if errors.Is(err, iterator.Done) {
			return deleted, nil
		}

		if err != nil {
			return deleted, fmt.Errorf("storage: list prefix %s: %w", prefix, err)
		}

		if err := bucket.Object(attrs.Name).Delete(ctx); err != nil {
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

	if _, err := cleanPublicKey(key); err != nil {
		return "", false
	}

	return key, true
}
