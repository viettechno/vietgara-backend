package storage_test

import (
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

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
	_, err = storage.NewLocal(filepath.Join(blocker, "sub"), "", nil)
	require.Error(t, err)
}

func TestLocalStore(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store, err := storage.NewLocal(dir, "/api/v1/files/", nil)
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

func TestLocalStoreDeletePrefix(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store, err := storage.NewLocal(dir, "", nil)
	require.NoError(t, err)

	for _, key := range []string{
		"garages/g1/logo.png",
		"garages/g1/older/logo-old.png",
		"garages/g10/logo.png",
	} {
		_, err := store.Put(t.Context(), key, "image/png", png)
		require.NoError(t, err)
	}

	deleted, err := store.DeletePrefix(t.Context(), "garages/g1/")
	require.NoError(t, err)
	assert.EqualValues(t, 2, deleted)
	assert.NoFileExists(t, filepath.Join(dir, "garages", "g1", "logo.png"))
	assert.NoFileExists(t, filepath.Join(dir, "garages", "g1", "older", "logo-old.png"))
	assert.FileExists(t, filepath.Join(dir, "garages", "g10", "logo.png"))

	deleted, err = store.DeletePrefix(t.Context(), "garages/g1/")
	require.NoError(t, err)
	assert.Zero(t, deleted)

	for _, prefix := range []string{"", "/", "garages", "../garages/g1/", "garages/../g2/", "garages//g1/"} {
		_, err := store.DeletePrefix(t.Context(), prefix)
		require.ErrorIs(t, err, storage.ErrInvalidKey, prefix)
	}
}

// mediaOf returns the object content of an upload request: the second part
// of a multipart upload, or the body of a media upload.
func mediaOf(r *http.Request) []byte {
	mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !strings.HasPrefix(mediaType, "multipart/") {
		body, _ := io.ReadAll(r.Body)

		return body
	}

	reader := multipart.NewReader(r.Body, params["boundary"])

	var content []byte

	for {
		part, err := reader.NextPart()
		if err != nil {
			return content
		}

		content, _ = io.ReadAll(part) // the last part is the media
	}
}

// fakeGCS implements the Cloud Storage JSON API calls the store makes.
type fakeGCS struct {
	mu sync.Mutex
	// objects is the public bucket "logos", private the bucket "private-files".
	objects map[string][]byte
	private map[string][]byte
	fail    bool
}

// bucket returns the objects of the bucket named in an API path.
func (f *fakeGCS) bucket(path string) (string, map[string][]byte) {
	for _, name := range []string{"logos", "private-files"} {
		if strings.Contains(path, "/b/"+name+"/o") {
			if name == "logos" {
				return name, f.objects
			}

			return name, f.private
		}
	}

	return "", nil
}

func (f *fakeGCS) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.fail {
		if r.Method == http.MethodGet {
			http.Error(w, `{"error":{"code":400,"message":"bad request"}}`, http.StatusBadRequest)

			return
		}

		http.Error(w, `{"error":{"code":500,"message":"boom"}}`, http.StatusInternalServerError)

		return
	}

	bucket, objects := f.bucket(r.URL.Path)

	switch {
	case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/upload/storage/v1/b/"+bucket+"/o"):
		body := mediaOf(r)

		name := r.URL.Query().Get("name")
		if name == "" {
			name = "garages/g1/logo.png"
		}

		objects[name] = body

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"bucket":"`+bucket+`","name":"`+name+`"}`) //nolint:gosec // fake storage API in a test
	case r.Method == http.MethodGet && bucket == "" && strings.HasPrefix(r.URL.Path, "/logos/"):
		body, ok := f.objects[strings.TrimPrefix(r.URL.Path, "/logos/")]
		if !ok {
			http.Error(w, "no such object", http.StatusNotFound)

			return
		}

		_, _ = w.Write(body)
	case r.Method == http.MethodGet && r.URL.Path == "/storage/v1/b/"+bucket+"/o":
		prefix := r.URL.Query().Get("prefix")

		items := make([]struct {
			Name string `json:"name"`
		}, 0)

		for name := range objects {
			if strings.HasPrefix(name, prefix) {
				items = append(items, struct {
					Name string `json:"name"`
				}{Name: name})
			}
		}

		response, err := json.Marshal(struct {
			Items []struct {
				Name string `json:"name"`
			} `json:"items"`
		}{Items: items})
		if err != nil {
			http.Error(w, "encode response", http.StatusInternalServerError)

			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(response)
	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/storage/v1/b/"+bucket+"/o/"):
		name := strings.TrimPrefix(r.URL.Path, "/storage/v1/b/"+bucket+"/o/")
		if _, ok := objects[name]; !ok {
			http.Error(w, `{"error":{"code":404,"message":"No such object"}}`, http.StatusNotFound)

			return
		}

		delete(objects, name)
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "unexpected "+r.Method+" "+r.URL.String(), http.StatusBadRequest)
	}
}

func TestGCSStore(t *testing.T) {
	fake := &fakeGCS{objects: map[string][]byte{}, private: map[string][]byte{}}
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

	for _, objectKey := range []string{
		"garages/g1/logo-old.png",
		"garages/g1/older/logo.png",
		"garages/g10/logo.png",
	} {
		_, err := store.Put(t.Context(), objectKey, "image/png", png)
		require.NoError(t, err)
	}

	deleted, err := store.DeletePrefix(t.Context(), "garages/g1/")
	require.NoError(t, err)
	assert.EqualValues(t, 2, deleted)
	assert.Contains(t, fake.objects, "garages/g10/logo.png")
	assert.NotContains(t, fake.objects, "garages/g1/logo-old.png")
	assert.NotContains(t, fake.objects, "garages/g1/older/logo.png")

	_, err = store.Put(t.Context(), "../x", "image/png", png)
	require.ErrorIs(t, err, storage.ErrInvalidKey)
	require.ErrorIs(t, store.Delete(t.Context(), "../x"), storage.ErrInvalidKey)
	_, err = store.DeletePrefix(t.Context(), "../x/")
	require.ErrorIs(t, err, storage.ErrInvalidKey)

	fake.fail = true
	_, err = store.Put(t.Context(), "garages/g1/other.png", "image/png", png)
	require.ErrorContains(t, err, "upload")
	require.ErrorContains(t, store.Delete(t.Context(), "garages/g1/other.png"), "delete")
	_, err = store.DeletePrefix(t.Context(), "garages/g1/")
	require.ErrorContains(t, err, "list prefix")

	cdn, err := storage.NewGCS(t.Context(), "logos", "https://cdn.vietgara.viettechno.com/")
	require.NoError(t, err)

	fake.fail = false
	url, err = cdn.Put(t.Context(), "garages/g1/logo.png", "image/png", png)
	require.NoError(t, err)
	assert.Equal(t, "https://cdn.vietgara.viettechno.com/garages/g1/logo.png", url)
}

func TestLocalStorePrivateObjects(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store, err := storage.NewLocal(dir, "/api/v1/files", []byte("test-signing-key"))
	require.NoError(t, err)

	handler := store.Handler()
	get := func(url string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, url, nil))

		return rec
	}

	require.NoError(t, store.PutPrivate(t.Context(), "garages/g1/repair_photo/r1/a.png", "image/png", png))
	require.NoError(t, store.PutPrivate(t.Context(), "garages/g1/export/e1/report.xlsx", "application/octet-stream", []byte("xlsx")))

	t.Run("a valid signed URL serves the object", func(t *testing.T) {
		t.Parallel()

		url, expires, err := store.SignedURL(t.Context(), "garages/g1/repair_photo/r1/a.png", time.Minute)
		require.NoError(t, err)
		assert.WithinDuration(t, time.Now().Add(time.Minute), expires, 5*time.Second)
		assert.True(t, strings.HasPrefix(url, "/api/v1/files/private/garages/g1/repair_photo/r1/a.png?expires="))

		rec := get(url)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, png, rec.Body.Bytes())
		assert.Equal(t, "image/png", rec.Header().Get("Content-Type"))
		assert.Equal(t, "private, no-store", rec.Header().Get("Cache-Control"))
		assert.Empty(t, rec.Header().Get("Content-Disposition"), "images show inline")
	})

	t.Run("other files download as attachments", func(t *testing.T) {
		t.Parallel()

		url, _, err := store.SignedURL(t.Context(), "garages/g1/export/e1/report.xlsx", time.Minute)
		require.NoError(t, err)

		rec := get(url)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "attachment", rec.Header().Get("Content-Disposition"))
	})

	t.Run("an expired URL is refused", func(t *testing.T) {
		t.Parallel()

		url, _, err := store.SignedURL(t.Context(), "garages/g1/repair_photo/r1/a.png", -time.Minute)
		require.NoError(t, err)
		assert.Equal(t, http.StatusForbidden, get(url).Code)
	})

	t.Run("a tampered or foreign URL is refused", func(t *testing.T) {
		t.Parallel()

		url, _, err := store.SignedURL(t.Context(), "garages/g1/repair_photo/r1/a.png", time.Minute)
		require.NoError(t, err)

		other := strings.Replace(url, "garages/g1/repair_photo/r1/a.png", "garages/g1/export/e1/report.xlsx", 1)
		assert.Equal(t, http.StatusForbidden, get(other).Code, "the signature names its key")
		assert.Equal(t, http.StatusForbidden, get(strings.Replace(url, "signature=", "signature=00", 1)).Code)
		assert.Equal(t, http.StatusForbidden, get("/api/v1/files/private/garages/g1/repair_photo/r1/a.png").Code, "unsigned")
		assert.Equal(t, http.StatusForbidden, get("/api/v1/files/private/garages/g1/repair_photo/r1/a.png?expires=x&signature=y").Code)

		stranger, err := storage.NewLocal(dir, "/api/v1/files", []byte("another-key"))
		require.NoError(t, err)

		foreign, _, err := stranger.SignedURL(t.Context(), "garages/g1/repair_photo/r1/a.png", time.Minute)
		require.NoError(t, err)
		assert.Equal(t, http.StatusForbidden, get(foreign).Code, "signed with another key")
	})

	t.Run("private objects are never served as public ones", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, http.StatusNotFound, get("/api/v1/files/garages/g1/repair_photo/r1/a.png").Code)
		assert.Equal(t, http.StatusNotFound, get("/api/v1/files/_private/garages/g1/repair_photo/r1/a.png").Code)

		_, err := store.Put(t.Context(), "_private/garages/g1/x.png", "image/png", png)
		require.ErrorIs(t, err, storage.ErrInvalidKey)
		require.ErrorIs(t, store.PutPrivate(t.Context(), "_private/x.png", "image/png", png), storage.ErrInvalidKey)
		_, ok := store.KeyFromURL("/api/v1/files/_private/garages/g1/x.png")
		assert.False(t, ok)
	})

	t.Run("a missing object is not found even with a valid signature", func(t *testing.T) {
		t.Parallel()

		url, _, err := store.SignedURL(t.Context(), "garages/g1/repair_photo/r1/missing.png", time.Minute)
		require.NoError(t, err)
		assert.Equal(t, http.StatusNotFound, get(url).Code)
	})

	t.Run("invalid keys cannot be signed", func(t *testing.T) {
		t.Parallel()

		_, _, err := store.SignedURL(t.Context(), "../x", time.Minute)
		require.ErrorIs(t, err, storage.ErrInvalidKey)
	})

	t.Run("deleting removes the private object and a prefix removes both", func(t *testing.T) {
		t.Parallel()

		_, err := store.Put(t.Context(), "garages/g2/logo/g2/a.png", "image/png", png)
		require.NoError(t, err)
		require.NoError(t, store.PutPrivate(t.Context(), "garages/g2/export/e1/a.csv", "text/csv", []byte("a")))
		require.NoError(t, store.PutPrivate(t.Context(), "garages/g3/export/e1/a.csv", "text/csv", []byte("a")))

		deleted, err := store.DeletePrefix(t.Context(), "garages/g2/")
		require.NoError(t, err)
		assert.EqualValues(t, 2, deleted)
		assert.FileExists(t, filepath.Join(dir, "_private", "garages", "g3", "export", "e1", "a.csv"))

		require.NoError(t, store.Delete(t.Context(), "garages/g3/export/e1/a.csv"))
		assert.NoFileExists(t, filepath.Join(dir, "_private", "garages", "g3", "export", "e1", "a.csv"))

		_, err = store.DeletePrefix(t.Context(), "_private/garages/g3/")
		require.ErrorIs(t, err, storage.ErrInvalidKey)
	})
}

func TestLocalStorePublicURL(t *testing.T) {
	t.Parallel()

	store, err := storage.NewLocal(t.TempDir(), "/api/v1/files", nil)
	require.NoError(t, err)

	assert.Equal(t, "/api/v1/files/garages/g1/logo/g1/a.png", store.PublicURL("garages/g1/logo/g1/a.png"))
}

func TestGCSStorePrivateObjects(t *testing.T) {
	fake := &fakeGCS{objects: map[string][]byte{}, private: map[string][]byte{}}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	t.Setenv("STORAGE_EMULATOR_HOST", strings.TrimPrefix(srv.URL, "http://"))

	t.Run("without a private bucket", func(t *testing.T) {
		store, err := storage.NewGCS(t.Context(), "logos", "")
		require.NoError(t, err)

		require.ErrorIs(t, store.PutPrivate(t.Context(), "garages/g1/export/e1/a.csv", "text/csv", nil), storage.ErrPrivateNotConfigured)
		_, _, err = store.SignedURL(t.Context(), "garages/g1/export/e1/a.csv", time.Minute)
		require.ErrorIs(t, err, storage.ErrPrivateNotConfigured)
	})

	t.Run("with a private bucket", func(t *testing.T) {
		store, err := storage.New(t.Context(), storage.Config{Backend: "gcs", Bucket: "logos", PrivateBucket: "private-files"})
		require.NoError(t, err)

		require.NoError(t, store.PutPrivate(t.Context(), "garages/g1/export/e1/a.csv", "text/csv", []byte("a")))
		assert.Contains(t, fake.private, "garages/g1/export/e1/a.csv")
		assert.Empty(t, fake.objects, "private objects never go to the public bucket")

		_, err = store.Put(t.Context(), "garages/g1/logo/g1/a.png", "image/png", png)
		require.NoError(t, err)

		deleted, err := store.DeletePrefix(t.Context(), "garages/g1/")
		require.NoError(t, err)
		assert.EqualValues(t, 2, deleted, "both buckets")
		assert.Empty(t, fake.private)

		require.ErrorIs(t, store.PutPrivate(t.Context(), "../x", "text/csv", nil), storage.ErrInvalidKey)
		_, _, err = store.SignedURL(t.Context(), "../x", time.Minute)
		require.ErrorIs(t, err, storage.ErrInvalidKey)

		// Without a service account key and outside GCE the client cannot
		// sign; the failure is reported, not hidden.
		_, _, err = store.SignedURL(t.Context(), "garages/g1/export/e1/a.csv", time.Minute)
		require.ErrorContains(t, err, "sign")

		assert.Equal(t, "https://storage.googleapis.com/logos/garages/g1/logo/g1/a.png", store.PublicURL("garages/g1/logo/g1/a.png"))
	})

	t.Run("reads a public object", func(t *testing.T) {
		store, err := storage.NewGCS(t.Context(), "logos", "")
		require.NoError(t, err)

		_, err = store.Put(t.Context(), "garages/g1/logo/g1/b.png", "image/png", png)
		require.NoError(t, err)

		got, err := store.Get(t.Context(), "garages/g1/logo/g1/b.png")
		require.NoError(t, err)
		assert.Equal(t, png, got)

		_, err = store.Get(t.Context(), "garages/g1/missing.png")
		require.Error(t, err)

		_, err = store.Get(t.Context(), "../x")
		require.ErrorIs(t, err, storage.ErrInvalidKey)
	})
}

func TestLocalStoreGet(t *testing.T) {
	t.Parallel()

	store, err := storage.NewLocal(t.TempDir(), "", nil)
	require.NoError(t, err)

	_, err = store.Put(t.Context(), "garages/g1/logo/g1/a.png", "image/png", png)
	require.NoError(t, err)
	require.NoError(t, store.PutPrivate(t.Context(), "garages/g1/export/e1/a.csv", "text/csv", []byte("a")))

	got, err := store.Get(t.Context(), "garages/g1/logo/g1/a.png")
	require.NoError(t, err)
	assert.Equal(t, png, got)

	_, err = store.Get(t.Context(), "garages/g1/export/e1/a.csv")
	require.Error(t, err, "private objects are not public")

	_, err = store.Get(t.Context(), "garages/g1/missing.png")
	require.Error(t, err)

	_, err = store.Get(t.Context(), "../x")
	require.ErrorIs(t, err, storage.ErrInvalidKey)

	_, err = store.Put(t.Context(), "garages/g1/big.png", "image/png", make([]byte, storage.MaxGetBytes+1))
	require.NoError(t, err)

	_, err = store.Get(t.Context(), "garages/g1/big.png")
	require.ErrorIs(t, err, storage.ErrTooLarge)
}

// TestLocalStoreStaysInItsDirectory covers SR-21: a symbolic link under the
// store directory must not lead a read or a write outside of it.
func TestLocalStoreStaysInItsDirectory(t *testing.T) {
	t.Parallel()

	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "secret.png"), png, 0o600))

	dir := t.TempDir()
	store, err := storage.NewLocal(dir, "", nil)
	require.NoError(t, err)
	require.NoError(t, os.Symlink(outside, filepath.Join(dir, "link")))

	_, err = store.Get(t.Context(), "link/secret.png")
	require.Error(t, err)

	rec := httptest.NewRecorder()
	store.Handler().ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, storage.DefaultLocalBaseURL+"/link/secret.png", http.NoBody))
	assert.Equal(t, http.StatusNotFound, rec.Code)

	_, err = store.Put(t.Context(), "link/new.png", "image/png", png)
	require.Error(t, err)
	assert.NoFileExists(t, filepath.Join(outside, "new.png"))
}
