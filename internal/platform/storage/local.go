package storage

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// LocalStore writes objects under a directory on the server's disk.
type LocalStore struct {
	dir     string
	baseURL string
}

// DefaultLocalBaseURL is where Handler serves local files.
const DefaultLocalBaseURL = "/api/v1/files"

// NewLocal returns a LocalStore rooted at dir (created if missing);
// baseURL defaults to DefaultLocalBaseURL.
func NewLocal(dir, baseURL string) (*LocalStore, error) {
	if baseURL == "" {
		baseURL = DefaultLocalBaseURL
	}

	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("storage: create %s: %w", dir, err)
	}

	return &LocalStore{dir: dir, baseURL: strings.TrimSuffix(baseURL, "/")}, nil
}

// Put writes data to dir/key.
func (s *LocalStore) Put(_ context.Context, key, _ string, data []byte) (string, error) {
	key, err := cleanKey(key)
	if err != nil {
		return "", err
	}

	full := filepath.Join(s.dir, filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		return "", fmt.Errorf("storage: create directory: %w", err)
	}

	if err := os.WriteFile(full, data, 0o600); err != nil {
		return "", fmt.Errorf("storage: write %s: %w", key, err)
	}

	return s.baseURL + "/" + key, nil
}

// Delete removes dir/key.
func (s *LocalStore) Delete(_ context.Context, key string) error {
	key, err := cleanKey(key)
	if err != nil {
		return err
	}

	if err := os.Remove(filepath.Join(s.dir, filepath.FromSlash(key))); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("storage: delete %s: %w", key, err)
	}

	return nil
}

// DeletePrefix removes the directory named by prefix and its contents.
func (s *LocalStore) DeletePrefix(ctx context.Context, prefix string) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, fmt.Errorf("storage: delete prefix: %w", err)
	}

	key, err := cleanPrefix(prefix)
	if err != nil {
		return 0, err
	}

	root, err := os.OpenRoot(s.dir)
	if err != nil {
		return 0, fmt.Errorf("storage: open root: %w", err)
	}
	defer func() { _ = root.Close() }()

	info, err := root.Lstat(key)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return 0, nil
		}

		return 0, fmt.Errorf("storage: stat prefix %s: %w", prefix, err)
	}

	var deleted int64
	if !info.IsDir() {
		deleted = 1
	} else if err := fs.WalkDir(root.FS(), key, func(_ string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if !entry.IsDir() {
			deleted++
		}

		return nil
	}); err != nil {
		return 0, fmt.Errorf("storage: walk prefix %s: %w", prefix, err)
	}

	if err := root.RemoveAll(key); err != nil {
		return 0, fmt.Errorf("storage: delete prefix %s: %w", prefix, err)
	}

	return deleted, nil
}

// KeyFromURL strips the base URL.
func (s *LocalStore) KeyFromURL(url string) (string, bool) {
	key, found := strings.CutPrefix(url, s.baseURL+"/")
	if !found {
		return "", false
	}

	if _, err := cleanKey(key); err != nil {
		return "", false
	}

	return key, true
}

// Handler serves the stored files; mount it at the base URL path. Only
// images are served, with a long cache lifetime (keys are unique per
// upload).
func (s *LocalStore) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key, ok := s.KeyFromURL(r.URL.Path)
		if !ok || (r.Method != http.MethodGet && r.Method != http.MethodHead) {
			http.NotFound(w, r)

			return
		}

		contentType := mime.TypeByExtension(filepath.Ext(key))
		if !strings.HasPrefix(contentType, "image/") {
			http.NotFound(w, r)

			return
		}

		f, err := os.Open(filepath.Join(s.dir, filepath.FromSlash(key))) //nolint:gosec // G703: key passed cleanKey (no "..", relative)
		if err != nil {
			http.NotFound(w, r)

			return
		}
		defer func() { _ = f.Close() }()

		info, err := f.Stat()
		if err != nil || info.IsDir() {
			http.NotFound(w, r)

			return
		}

		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		http.ServeContent(w, r, "", info.ModTime(), f)
	})
}
