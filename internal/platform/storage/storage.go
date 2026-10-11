// Package storage keeps uploaded files such as garage logos (FR-TEN-01).
//
// Objects are public (garage logos; served by URL) or private (every later
// kind: repair photos, customer and claim documents, exports; served only
// through short-lived signed URLs, FR-CAT-07's file registry).
//
// Backends:
//   - local: the server's disk, for local development and staging (staging
//     must run a single replica or mount a shared volume). Public files are
//     served by Handler under /api/v1/files/; private ones live under a
//     reserved directory and are served only for an HMAC-signed, expiring
//     URL, which is how the local backend checks authorization itself.
//   - gcs: Google Cloud Storage. Public objects sit in a public-read bucket
//     (or behind a CDN); private ones in a second bucket without public
//     access and are served through V4 signed URLs.
package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"
)

// Store saves and removes objects addressed by key ("garages/<id>/logo-x.png").
type Store interface {
	// Put stores data under key and returns its public URL.
	Put(ctx context.Context, key, contentType string, data []byte) (string, error)
	// Delete removes key; a missing object is not an error.
	Delete(ctx context.Context, key string) error
	// DeletePrefix removes all objects under prefix and returns the number removed.
	DeletePrefix(ctx context.Context, prefix string) (int64, error)
	// KeyFromURL returns the key of a URL this store produced.
	KeyFromURL(url string) (string, bool)
	// PublicURL returns the URL of a public object's key.
	PublicURL(key string) string
	// Get returns the content of a public object (at most MaxGetBytes), for
	// what the server itself draws, such as the logo of a PDF header.
	Get(ctx context.Context, key string) ([]byte, error)
	// PutPrivate stores data under key without making it public.
	PutPrivate(ctx context.Context, key, contentType string, data []byte) error
	// SignedURL returns a URL that serves the private object for ttl, and
	// when it stops working.
	SignedURL(ctx context.Context, key string, ttl time.Duration) (string, time.Time, error)
}

// ErrInvalidKey is returned for keys that could escape the store.
var ErrInvalidKey = errors.New("storage: invalid object key")

// ErrPrivateNotConfigured is returned when private objects are used but no
// private bucket (gcs) is configured.
var ErrPrivateNotConfigured = errors.New("storage: private storage is not configured")

// MaxGetBytes is the largest object Get returns.
const MaxGetBytes = 2 << 20

// ErrTooLarge is returned by Get for an object over MaxGetBytes.
var ErrTooLarge = errors.New("storage: object is too large")

// privateRoot is reserved by the local backend for private objects, so a
// public key may never name it.
const privateRoot = "_private"

var (
	errLocalStorageDirRequired = errors.New("storage: STORAGE_LOCAL_DIR is required")
	errGCSBucketRequired       = errors.New("storage: STORAGE_GCS_BUCKET is required")
	errUnknownStorageBackend   = errors.New("storage: unknown STORAGE_BACKEND")
)

// cleanKey validates key: relative, no "..", no empty segments.
func cleanKey(key string) (string, error) {
	if key == "" || strings.HasPrefix(key, "/") || strings.Contains(key, "\\") {
		return "", ErrInvalidKey
	}

	cleaned := path.Clean(key)
	if cleaned != key || cleaned == "." || strings.HasPrefix(cleaned, "../") || cleaned == ".." {
		return "", ErrInvalidKey
	}

	return cleaned, nil
}

// cleanPublicKey is cleanKey for public objects, which may not use the
// directory the local backend reserves for private ones.
func cleanPublicKey(key string) (string, error) {
	cleaned, err := cleanKey(key)
	if err != nil {
		return "", err
	}

	if cleaned == privateRoot || strings.HasPrefix(cleaned, privateRoot+"/") {
		return "", ErrInvalidKey
	}

	return cleaned, nil
}

func cleanPrefix(prefix string) (string, error) {
	if !strings.HasSuffix(prefix, "/") {
		return "", ErrInvalidKey
	}

	key, err := cleanKey(strings.TrimSuffix(prefix, "/"))
	if err != nil {
		return "", err
	}

	return key, nil
}

// Config selects and configures a backend.
type Config struct {
	// Backend is "local" or "gcs".
	Backend string

	// LocalDir is where the local backend writes files.
	LocalDir string
	// PublicBaseURL prefixes object keys in returned URLs, e.g.
	// "/api/v1/files" (local) or "https://storage.googleapis.com/<bucket>".
	PublicBaseURL string

	// SigningKey signs the URLs of private objects (local); an empty key
	// makes the backend generate one per process, so URLs stop working at a
	// restart.
	SigningKey []byte

	// Bucket is the public Cloud Storage bucket (gcs).
	Bucket string
	// PrivateBucket is the bucket for private objects (gcs); it must not
	// be public. Optional until the first private kind exists.
	PrivateBucket string
}

// New returns the backend selected by cfg. The gcs backend uses
// Application Default Credentials (Workload Identity on GKE).
func New(ctx context.Context, cfg Config) (Store, error) {
	switch cfg.Backend {
	case "local":
		if cfg.LocalDir == "" {
			return nil, errLocalStorageDirRequired
		}

		return NewLocal(cfg.LocalDir, cfg.PublicBaseURL, cfg.SigningKey)
	case "gcs":
		if cfg.Bucket == "" {
			return nil, errGCSBucketRequired
		}

		return NewGCSWithPrivate(ctx, cfg.Bucket, cfg.PrivateBucket, cfg.PublicBaseURL)
	default:
		return nil, fmt.Errorf("%w %q (want local or gcs)", errUnknownStorageBackend, cfg.Backend)
	}
}

// readLimited reads r up to MaxGetBytes.
func readLimited(r io.Reader, key string) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxGetBytes+1))
	if err != nil {
		return nil, fmt.Errorf("storage: read %s: %w", key, err)
	}

	if len(data) > MaxGetBytes {
		return nil, fmt.Errorf("%w: %s", ErrTooLarge, key)
	}

	return data, nil
}
