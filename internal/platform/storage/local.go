package storage

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// LocalStore writes objects under a directory on the server's disk.
type LocalStore struct {
	dir        string
	baseURL    string
	signingKey []byte
	now        func() time.Time
}

// privateURLSegment follows the base URL in a signed private URL.
const privateURLSegment = "private/"

// DefaultLocalBaseURL is where Handler serves local files.
const DefaultLocalBaseURL = "/api/v1/files"

// NewLocal returns a LocalStore rooted at dir (created if missing);
// baseURL defaults to DefaultLocalBaseURL. signingKey signs the URLs of
// private objects; empty generates a random key for this process.
func NewLocal(dir, baseURL string, signingKey []byte) (*LocalStore, error) {
	if baseURL == "" {
		baseURL = DefaultLocalBaseURL
	}

	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("storage: create %s: %w", dir, err)
	}

	if len(signingKey) == 0 {
		signingKey = make([]byte, 32)
		if _, err := rand.Read(signingKey); err != nil {
			return nil, fmt.Errorf("storage: signing key: %w", err)
		}
	}

	return &LocalStore{dir: dir, baseURL: strings.TrimSuffix(baseURL, "/"), signingKey: signingKey, now: time.Now}, nil
}

func (s *LocalStore) write(key string, data []byte) error {
	full := filepath.Join(s.dir, filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		return fmt.Errorf("storage: create directory: %w", err)
	}

	if err := os.WriteFile(full, data, 0o600); err != nil {
		return fmt.Errorf("storage: write %s: %w", key, err)
	}

	return nil
}

// Put writes data to dir/key.
func (s *LocalStore) Put(_ context.Context, key, _ string, data []byte) (string, error) {
	key, err := cleanPublicKey(key)
	if err != nil {
		return "", err
	}

	if err := s.write(key, data); err != nil {
		return "", err
	}

	return s.baseURL + "/" + key, nil
}

// PutPrivate writes data under the reserved private directory.
func (s *LocalStore) PutPrivate(_ context.Context, key, _ string, data []byte) error {
	key, err := cleanPublicKey(key)
	if err != nil {
		return err
	}

	return s.write(privateRoot+"/"+key, data)
}

// PublicURL returns the URL Handler serves key at.
func (s *LocalStore) PublicURL(key string) string { return s.baseURL + "/" + key }

// Get reads a public object.
func (s *LocalStore) Get(_ context.Context, key string) ([]byte, error) {
	key, err := cleanPublicKey(key)
	if err != nil {
		return nil, err
	}

	f, err := os.Open(filepath.Join(s.dir, filepath.FromSlash(key)))
	if err != nil {
		return nil, fmt.Errorf("storage: open %s: %w", key, err)
	}
	defer func() { _ = f.Close() }()

	return readLimited(f, key)
}

// Delete removes dir/key, public or private.
func (s *LocalStore) Delete(_ context.Context, key string) error {
	key, err := cleanPublicKey(key)
	if err != nil {
		return err
	}

	for _, name := range []string{key, privateRoot + "/" + key} {
		if err := os.Remove(filepath.Join(s.dir, filepath.FromSlash(name))); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("storage: delete %s: %w", key, err)
		}
	}

	return nil
}

// signature is the HMAC of a private key and its expiry.
func (s *LocalStore) signature(key string, expires int64) string {
	mac := hmac.New(sha256.New, s.signingKey)
	_, _ = mac.Write([]byte(key + "\n" + strconv.FormatInt(expires, 10)))

	return hex.EncodeToString(mac.Sum(nil))
}

// SignedURL returns a URL, valid for ttl, that Handler serves the private
// object at.
func (s *LocalStore) SignedURL(_ context.Context, key string, ttl time.Duration) (string, time.Time, error) {
	key, err := cleanPublicKey(key)
	if err != nil {
		return "", time.Time{}, err
	}

	expiresAt := s.now().Add(ttl)
	expires := expiresAt.Unix()

	return fmt.Sprintf("%s/%s%s?expires=%d&signature=%s", s.baseURL, privateURLSegment, key, expires, s.signature(key, expires)),
		expiresAt, nil
}

// DeletePrefix removes the directory named by prefix, public and private,
// and its contents.
func (s *LocalStore) DeletePrefix(ctx context.Context, prefix string) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, fmt.Errorf("storage: delete prefix: %w", err)
	}

	key, err := cleanPrefix(prefix)
	if err != nil {
		return 0, err
	}

	if key == privateRoot || strings.HasPrefix(key, privateRoot+"/") {
		return 0, ErrInvalidKey
	}

	root, err := os.OpenRoot(s.dir)
	if err != nil {
		return 0, fmt.Errorf("storage: open root: %w", err)
	}
	defer func() { _ = root.Close() }()

	var deleted int64

	for _, name := range []string{key, privateRoot + "/" + key} {
		count, err := removeTree(root, name)
		if err != nil {
			return deleted, fmt.Errorf("storage: delete prefix %s: %w", prefix, err)
		}

		deleted += count
	}

	return deleted, nil
}

// removeTree removes name (a file or a directory) from root and counts the
// files it held; a missing name counts zero.
func removeTree(root *os.Root, name string) (int64, error) {
	info, err := root.Lstat(name)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return 0, nil
		}

		return 0, err
	}

	var count int64
	if !info.IsDir() {
		count = 1
	} else if err := fs.WalkDir(root.FS(), name, func(_ string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if !entry.IsDir() {
			count++
		}

		return nil
	}); err != nil {
		return 0, err
	}

	if err := root.RemoveAll(name); err != nil {
		return 0, err
	}

	return count, nil
}

// KeyFromURL strips the base URL.
func (s *LocalStore) KeyFromURL(url string) (string, bool) {
	key, found := strings.CutPrefix(url, s.baseURL+"/")
	if !found {
		return "", false
	}

	if _, err := cleanPublicKey(key); err != nil {
		return "", false
	}

	return key, true
}

// Handler serves the stored files; mount it at the base URL path. Only
// images are served, with a long cache lifetime (keys are unique per
// upload).
func (s *LocalStore) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if rest, ok := strings.CutPrefix(r.URL.Path, s.baseURL+"/"+privateURLSegment); ok {
			s.servePrivate(w, r, rest)

			return
		}

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

// servePrivate serves a private object for a valid, unexpired signature.
func (s *LocalStore) servePrivate(w http.ResponseWriter, r *http.Request, key string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.NotFound(w, r)

		return
	}

	key, err := cleanPublicKey(key)
	expires, expErr := strconv.ParseInt(r.URL.Query().Get("expires"), 10, 64)

	if err != nil || expErr != nil || s.now().Unix() > expires ||
		!hmac.Equal([]byte(r.URL.Query().Get("signature")), []byte(s.signature(key, expires))) {
		http.Error(w, "the link is invalid or has expired", http.StatusForbidden)

		return
	}

	f, err := os.Open(filepath.Join(s.dir, privateRoot, filepath.FromSlash(key))) //nolint:gosec // key passed cleanKey (no "..", relative)
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

	contentType := mime.TypeByExtension(filepath.Ext(key))
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	if !strings.HasPrefix(contentType, "image/") {
		w.Header().Set("Content-Disposition", "attachment")
	}

	http.ServeContent(w, r, "", info.ModTime(), f)
}
