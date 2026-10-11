// Package storage keeps uploaded files such as garage logos (FR-TEN-01).
//
// Backends:
//   - local: the server's disk, for local development and staging (staging
//     must run a single replica or mount a shared volume). Files are served
//     by Handler under /api/v1/files/.
//   - gcs: a Google Cloud Storage bucket, for production. Objects are
//     public-read through the bucket's public URL (or a CDN in front).
package storage

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
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
}

// ErrInvalidKey is returned for keys that could escape the store.
var ErrInvalidKey = errors.New("storage: invalid object key")

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

	// Bucket is the Cloud Storage bucket (gcs).
	Bucket string
}

// New returns the backend selected by cfg. The gcs backend uses
// Application Default Credentials (Workload Identity on GKE).
func New(ctx context.Context, cfg Config) (Store, error) {
	switch cfg.Backend {
	case "local":
		if cfg.LocalDir == "" {
			return nil, errLocalStorageDirRequired
		}

		return NewLocal(cfg.LocalDir, cfg.PublicBaseURL)
	case "gcs":
		if cfg.Bucket == "" {
			return nil, errGCSBucketRequired
		}

		return NewGCS(ctx, cfg.Bucket, cfg.PublicBaseURL)
	default:
		return nil, fmt.Errorf("%w %q (want local or gcs)", errUnknownStorageBackend, cfg.Backend)
	}
}
