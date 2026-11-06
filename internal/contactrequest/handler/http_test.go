package handler_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/contactrequest/domain"
	"github.com/viettechno/vietgara-backend/internal/contactrequest/handler"
	"github.com/viettechno/vietgara-backend/internal/contactrequest/usecase"
	"github.com/viettechno/vietgara-backend/internal/platform/mail"
)

const key = "a-key-the-website-and-the-backend-share"

type repo struct {
	stored []*domain.Request
	count  int
	source string
}

func (r *repo) Create(_ context.Context, req *domain.Request) error {
	r.stored = append(r.stored, req)

	return nil
}

func (r *repo) CountSince(_ context.Context, hash string, _ time.Time) (int, error) {
	r.source = hash

	return r.count, nil
}

type mailer struct{ sent []mail.Message }

func (m *mailer) Send(_ context.Context, msg mail.Message) error {
	m.sent = append(m.sent, msg)

	return nil
}

const goodBody = `{"name":"Nguyễn Văn A","garageName":"Gara Minh Phát","phone":"0938038621","email":"a@example.com","message":"Demo","consent":true,"locale":"en"}`

func setup(t *testing.T, k string, r *repo, m *mailer) http.Handler {
	t.Helper()

	srv := handler.NewServer(usecase.New(r, m, "support@vietgara.vn", []byte("0123456789abcdef0123456789abcdef")), k)
	mux := runtime.NewServeMux()
	require.NoError(t, srv.RegisterHTTPRoutes(mux))

	return mux
}

func post(h http.Handler, body string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, handler.Path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	return rec
}

func TestAcceptsARequestWithTheKey(t *testing.T) {
	t.Parallel()

	r, m := &repo{}, &mailer{}
	rec := post(setup(t, key, r, m), goodBody, map[string]string{handler.KeyHeader: key, "X-Real-IP": "203.0.113.7"})

	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	require.Len(t, r.stored, 1)
	assert.JSONEq(t, `{"id":"`+r.stored[0].ID+`"}`, rec.Body.String())
	assert.Equal(t, "en", r.stored[0].Locale)
	assert.Len(t, m.sent, 1)
}

func TestRefusesWithoutTheKey(t *testing.T) {
	t.Parallel()

	for name, headers := range map[string]map[string]string{
		"no key":    nil,
		"wrong key": {handler.KeyHeader: "nope"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			r := &repo{}
			rec := post(setup(t, key, r, &mailer{}), goodBody, headers)
			assert.Equal(t, http.StatusUnauthorized, rec.Code)
			assert.Empty(t, r.stored)
		})
	}
}

func TestIsOffWithoutAKey(t *testing.T) {
	t.Parallel()

	rec := post(setup(t, "", &repo{}, &mailer{}), goodBody, map[string]string{handler.KeyHeader: ""})
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestRefusesBadRequests(t *testing.T) {
	t.Parallel()

	h := map[string]string{handler.KeyHeader: key}

	tests := map[string]struct {
		body string
		code int
	}{
		"no consent": {`{"name":"A","phone":"0938038621","consent":false}`, http.StatusUnprocessableEntity},
		"not JSON":   {`name=A`, http.StatusUnprocessableEntity},
		"too big":    {`{"message":"` + strings.Repeat("a", 20<<10) + `"}`, http.StatusUnprocessableEntity},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			r := &repo{}
			rec := post(setup(t, key, r, &mailer{}), tt.body, h)
			assert.Equal(t, tt.code, rec.Code, rec.Body.String())
			assert.Empty(t, r.stored)
		})
	}
}

func TestAnswers429WhenTheSourceIsOverItsLimit(t *testing.T) {
	t.Parallel()

	r := &repo{count: usecase.MaxPerHour}
	rec := post(setup(t, key, r, &mailer{}), goodBody, map[string]string{handler.KeyHeader: key})
	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
	assert.Empty(t, r.stored)
}

func TestCountsTheVisitorNotTheProxy(t *testing.T) {
	t.Parallel()

	source := func(headers map[string]string) string {
		r := &repo{}
		headers[handler.KeyHeader] = key
		rec := post(setup(t, key, r, &mailer{}), goodBody, headers)
		require.Equal(t, http.StatusAccepted, rec.Code)

		return r.source
	}

	visitor := source(map[string]string{"X-Real-IP": "198.51.100.1", handler.VisitorIPHeader: "203.0.113.7"})
	other := source(map[string]string{"X-Real-IP": "198.51.100.1", handler.VisitorIPHeader: "203.0.113.8"})
	proxy := source(map[string]string{"X-Real-IP": "198.51.100.1"})
	bad := source(map[string]string{"X-Real-IP": "198.51.100.1", handler.VisitorIPHeader: "not an address"})

	assert.NotEqual(t, visitor, other, "two visitors behind one proxy are counted apart")
	assert.NotEqual(t, visitor, proxy)
	assert.Equal(t, proxy, bad, "an unparsable address falls back to the gateway's")
}
