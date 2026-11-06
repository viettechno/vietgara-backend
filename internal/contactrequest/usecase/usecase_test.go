package usecase_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/contactrequest/domain"
	"github.com/viettechno/vietgara-backend/internal/contactrequest/usecase"
	"github.com/viettechno/vietgara-backend/internal/platform/mail"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

var errBoom = errors.New("boom")

type fakeRepo struct {
	stored  []*domain.Request
	count   int
	createE error
	countE  error
}

func (f *fakeRepo) Create(_ context.Context, r *domain.Request) error {
	if f.createE != nil {
		return f.createE
	}

	f.stored = append(f.stored, r)

	return nil
}

func (f *fakeRepo) CountSince(_ context.Context, _ string, _ time.Time) (int, error) {
	return f.count, f.countE
}

type fakeMailer struct {
	sent []mail.Message
	err  error
}

func (f *fakeMailer) Send(_ context.Context, m mail.Message) error {
	f.sent = append(f.sent, m)

	return f.err
}

func input() domain.Input {
	return domain.Input{Name: "Nguyễn Văn A", GarageName: "Gara Minh Phát", Phone: "0938038621", Email: "a@example.com", Message: "Demo, please", Consent: true, Locale: "vi"}
}

func newUC(repo *fakeRepo, mailer *fakeMailer) *usecase.UseCase {
	return usecase.New(repo, mailer, "support@vietgara.vn", []byte("0123456789abcdef0123456789abcdef"))
}

func TestCreateStoresAndNotifiesSupport(t *testing.T) {
	t.Parallel()

	repo, mailer := &fakeRepo{}, &fakeMailer{}

	id, err := newUC(repo, mailer).Create(t.Context(), input(), usecase.Source{IP: "203.0.113.7", UserAgent: "Mozilla/5.0"})
	require.NoError(t, err)

	require.Len(t, repo.stored, 1)
	got := repo.stored[0]
	assert.Equal(t, id, got.ID)
	assert.Equal(t, domain.StatusNew, got.Status)
	assert.False(t, got.ConsentAt.IsZero(), "the time of the consent is kept")
	assert.Len(t, got.SourceIPHash, 64)
	assert.NotContains(t, got.SourceIPHash, "203.0.113.7", "the address itself is never stored")
	assert.Equal(t, "Mozilla/5.0", got.UserAgent)

	require.Len(t, mailer.sent, 1)
	msg := mailer.sent[0]
	assert.Equal(t, "support@vietgara.vn", msg.ToEmail)
	assert.Equal(t, "a@example.com", msg.ReplyTo, "support replies to the visitor")
	assert.Contains(t, msg.Subject, "Nguyễn Văn A")
	assert.Contains(t, msg.Subject, "Gara Minh Phát")
	assert.Contains(t, msg.Text, "0938038621")
	assert.Contains(t, msg.Text, id)
}

func TestCreateHashesTheSameSourceTheSameWay(t *testing.T) {
	t.Parallel()

	repo := &fakeRepo{}
	uc := newUC(repo, &fakeMailer{})

	for _, ip := range []string{"203.0.113.7", "203.0.113.7", "198.51.100.1"} {
		_, err := uc.Create(t.Context(), input(), usecase.Source{IP: ip})
		require.NoError(t, err)
	}

	assert.Equal(t, repo.stored[0].SourceIPHash, repo.stored[1].SourceIPHash)
	assert.NotEqual(t, repo.stored[0].SourceIPHash, repo.stored[2].SourceIPHash)
}

func TestCreateCountsAnIPv6SourceByItsNetwork(t *testing.T) {
	t.Parallel()

	repo := &fakeRepo{}
	uc := newUC(repo, &fakeMailer{})

	for _, ip := range []string{"2001:db8:1:2::1", "2001:DB8:1:2:ffff::9", "2001:db8:1:3::1", "::ffff:203.0.113.7", "203.0.113.7"} {
		_, err := uc.Create(t.Context(), input(), usecase.Source{IP: ip})
		require.NoError(t, err)
	}

	assert.Equal(t, repo.stored[0].SourceIPHash, repo.stored[1].SourceIPHash, "the same /64")
	assert.NotEqual(t, repo.stored[0].SourceIPHash, repo.stored[2].SourceIPHash, "another /64")
	assert.Equal(t, repo.stored[3].SourceIPHash, repo.stored[4].SourceIPHash, "an IPv4-mapped address is the IPv4 one")
}

func TestCreateRefusesWithoutStoring(t *testing.T) {
	t.Parallel()

	noConsent := input()
	noConsent.Consent = false

	repo, mailer := &fakeRepo{}, &fakeMailer{}
	_, err := newUC(repo, mailer).Create(t.Context(), noConsent, usecase.Source{})
	require.Error(t, err)
	assert.Empty(t, repo.stored)
	assert.Empty(t, mailer.sent)

	limited, mailer := &fakeRepo{count: usecase.MaxPerHour}, &fakeMailer{}
	_, err = newUC(limited, mailer).Create(t.Context(), input(), usecase.Source{IP: "203.0.113.7"})
	appErr, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, apperror.CodeResourceExhausted, appErr.Code)
	assert.Empty(t, limited.stored)
	assert.Empty(t, mailer.sent)
}

func TestCreateMailsEvenWhenTheCallerGoesAway(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	repo, mailer := &fakeRepo{}, &ctxMailer{}
	uc := usecase.New(repo, mailer, "support@vietgara.vn", []byte("0123456789abcdef0123456789abcdef"))

	cancel()

	_, err := uc.Create(ctx, input(), usecase.Source{})
	require.NoError(t, err)
	require.Len(t, mailer.errs, 1)
	assert.NoError(t, mailer.errs[0], "the e-mail is not cancelled with the request")
}

type ctxMailer struct{ errs []error }

func (m *ctxMailer) Send(ctx context.Context, _ mail.Message) error {
	m.errs = append(m.errs, ctx.Err())

	return nil
}

func TestCreateKeepsTheRequestWhenTheMailFails(t *testing.T) {
	t.Parallel()

	repo := &fakeRepo{}
	_, err := newUC(repo, &fakeMailer{err: errBoom}).Create(t.Context(), input(), usecase.Source{})
	require.NoError(t, err)
	assert.Len(t, repo.stored, 1)
}

func TestCreateReportsStorageErrors(t *testing.T) {
	t.Parallel()

	for _, repo := range []*fakeRepo{{createE: errBoom}, {countE: errBoom}} {
		mailer := &fakeMailer{}
		_, err := newUC(repo, mailer).Create(t.Context(), input(), usecase.Source{})
		appErr, ok := apperror.As(err)
		require.True(t, ok)
		assert.Equal(t, apperror.CodeInternal, appErr.Code)
		assert.Empty(t, mailer.sent)
	}
}

func TestCreateTruncatesTheUserAgent(t *testing.T) {
	t.Parallel()

	repo := &fakeRepo{}

	_, err := newUC(repo, &fakeMailer{}).Create(t.Context(), input(), usecase.Source{UserAgent: strings.Repeat("x", 500)})
	require.NoError(t, err)
	assert.Len(t, []rune(repo.stored[0].UserAgent), 300)
}
