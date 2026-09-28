package storage_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/platform/storage"
)

var png = []byte("\x89PNG\r\n\x1a\nfake")

func TestNewLocal(t *testing.T) {
	t.Parallel()

	store, err := storage.New(t.Context(), storage.Config{Backend: "local", LocalDir: t.TempDir()})
	require.NoError(t, err)
	assert.NotNil(t, store)

	_, err = storage.New(t.Context(), storage.Config{Backend: "local"})
	require.ErrorContains(t, err, "STORAGE_LOCAL_DIR")

	_, err = storage.New(t.Context(), storage.Config{Backend: "gcs"})
	require.ErrorContains(t, err, "STORAGE_GCS_BUCKET")

	_, err = storage.New(t.Context(), storage.Config{Backend: "s3"})
	require.ErrorContains(t, err, "unknown STORAGE_BACKEND")

	blocker := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(blocker, nil, 0o600))
	_, err = storage.NewLocal(filepath.Join(blocker, "sub"), "")
	require.Error(t, err)
}

func TestLocalStore(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store, err := storage.NewLocal(dir, "/api/v1/files/")
	require.NoError(t, err)

	url, err := store.Put(t.Context(), "garages/g1/logo-1.png", "image/png", png)
	require.NoError(t, err)
	assert.Equal(t, "/api/v1/files/garages/g1/logo-1.png", url)

	key, ok := store.KeyFromURL(url)
	assert.True(t, ok)
	assert.Equal(t, "garages/g1/logo-1.png", key)

	_, ok = store.KeyFromURL("https://elsewhere/x.png")
	assert.False(t, ok)
	_, ok = store.KeyFromURL("/api/v1/files/../etc/passwd")
	assert.False(t, ok)

	handler := store.Handler()

	get := func(method, path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), method, path, nil))

		return rec
	}

	rec := get(http.MethodGet, url)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "image/png", rec.Header().Get("Content-Type"))
	assert.Equal(t, png, rec.Body.Bytes())

	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "dir.png"), 0o750))

	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, url},
		{http.MethodGet, "/api/v1/files/notes.txt"},
		{http.MethodGet, "/api/v1/files/missing.png"},
		{http.MethodGet, "/api/v1/files/dir.png"},
		{http.MethodGet, "/other/logo.png"},
	} {
		assert.Equal(t, http.StatusNotFound, get(tc.method, tc.path).Code, tc.path)
	}

	require.NoError(t, store.Delete(t.Context(), key))
	require.NoError(t, store.Delete(t.Context(), key), "deleting twice is fine")
	assert.Equal(t, http.StatusNotFound, get(http.MethodGet, url).Code)

	for _, bad := range []string{"", "/abs.png", "../x.png", "a/../../x.png", `a\b.png`, "a//b.png", "."} {
		_, err := store.Put(t.Context(), bad, "image/png", png)
		require.ErrorIs(t, err, storage.ErrInvalidKey, bad)
		require.ErrorIs(t, store.Delete(t.Context(), bad), storage.ErrInvalidKey, bad)
	}

	require.NoError(t, os.WriteFile(filepath.Join(dir, "blocker"), nil, 0o600))
	_, err = store.Put(t.Context(), "blocker/x.png", "image/png", png)
	require.Error(t, err)
	_, err = store.Put(t.Context(), "dir.png", "image/png", png)
	require.Error(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "dir.png", "child"), nil, 0o600))
	require.Error(t, store.Delete(t.Context(), "dir.png"))
}

// fakeGCS implements the two Cloud Storage JSON API calls the store makes.
type fakeGCS struct {
	mu      sync.Mutex
	objects map[string][]byte
	fail    bool
}

func (f *fakeGCS) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.fail {
		http.Error(w, `{"error":{"code":500,"message":"boom"}}`, http.StatusInternalServerError)

		return
	}

	switch {
	case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/upload/storage/v1/b/logos/o"):
		body, _ := io.ReadAll(r.Body)

		name := r.URL.Query().Get("name")
		if name == "" {
			name = "garages/g1/logo.png"
		}

		f.objects[name] = body

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"bucket":"logos","name":"`+name+`"}`) //nolint:gosec // fake storage API in a test
	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/storage/v1/b/logos/o/"):
		name := strings.TrimPrefix(r.URL.Path, "/storage/v1/b/logos/o/")
		if _, ok := f.objects[name]; !ok {
			http.Error(w, `{"error":{"code":404,"message":"No such object"}}`, http.StatusNotFound)

			return
		}

		delete(f.objects, name)
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "unexpected "+r.Method+" "+r.URL.String(), http.StatusBadRequest)
	}
}

func TestGCSStore(t *testing.T) {
	fake := &fakeGCS{objects: map[string][]byte{}}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	t.Setenv("STORAGE_EMULATOR_HOST", strings.TrimPrefix(srv.URL, "http://"))

	store, err := storage.New(t.Context(), storage.Config{Backend: "gcs", Bucket: "logos"})
	require.NoError(t, err)

	url, err := store.Put(t.Context(), "garages/g1/logo.png", "image/png", png)
	require.NoError(t, err)
	assert.Equal(t, "https://storage.googleapis.com/logos/garages/g1/logo.png", url)
	assert.Len(t, fake.objects, 1)

	key, ok := store.KeyFromURL(url)
	assert.True(t, ok)
	assert.Equal(t, "garages/g1/logo.png", key)

	_, ok = store.KeyFromURL("/api/v1/files/x.png")
	assert.False(t, ok)
	_, ok = store.KeyFromURL("https://storage.googleapis.com/logos/../x.png")
	assert.False(t, ok)

	require.NoError(t, store.Delete(t.Context(), key))
	require.NoError(t, store.Delete(t.Context(), key), "missing objects are ignored")

	_, err = store.Put(t.Context(), "../x", "image/png", png)
	require.ErrorIs(t, err, storage.ErrInvalidKey)
	require.ErrorIs(t, store.Delete(t.Context(), "../x"), storage.ErrInvalidKey)

	fake.fail = true
	_, err = store.Put(t.Context(), "garages/g1/other.png", "image/png", png)
	require.ErrorContains(t, err, "upload")
	require.ErrorContains(t, store.Delete(t.Context(), "garages/g1/other.png"), "delete")

	cdn, err := storage.NewGCS(t.Context(), "logos", "https://cdn.vietgara.vn/")
	require.NoError(t, err)

	fake.fail = false
	url, err = cdn.Put(t.Context(), "garages/g1/logo.png", "image/png", png)
	require.NoError(t, err)
	assert.Equal(t, "https://cdn.vietgara.vn/garages/g1/logo.png", url)
}
