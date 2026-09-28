package usecase_test

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/identity/domain"
	"github.com/viettechno/vietgara-backend/internal/identity/repository"
	"github.com/viettechno/vietgara-backend/internal/identity/usecase"
	"github.com/viettechno/vietgara-backend/internal/platform/auth"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platform/mail"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

var errBoom = errors.New("boom")

// fakeAccounts is an in-memory AccountRepository.
type fakeAccounts struct {
	mu        sync.Mutex
	byID      map[string]*domain.Account
	createErr error
	updateErr error
	findErr   error
	granted   []string
}

func newFakeAccounts() *fakeAccounts { return &fakeAccounts{byID: map[string]*domain.Account{}} }

func (f *fakeAccounts) find(match func(*domain.Account) bool) (*domain.Account, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.findErr != nil {
		return nil, f.findErr
	}

	for _, a := range f.byID {
		if match(a) {
			copied := *a

			return &copied, nil
		}
	}

	return nil, domain.ErrAccountNotFound
}

func (f *fakeAccounts) FindByID(_ context.Context, id string) (*domain.Account, error) {
	return f.find(func(a *domain.Account) bool { return a.ID == id })
}

func (f *fakeAccounts) FindByEmail(_ context.Context, email string) (*domain.Account, error) {
	return f.find(func(a *domain.Account) bool { return strings.EqualFold(a.Email, email) })
}

func (f *fakeAccounts) FindByPhone(_ context.Context, phone string) (*domain.Account, error) {
	return f.find(func(a *domain.Account) bool { return a.Phone != nil && *a.Phone == phone })
}

func (f *fakeAccounts) FindByGoogleSubject(_ context.Context, subject string) (*domain.Account, error) {
	return f.find(func(a *domain.Account) bool { return a.GoogleSubject != nil && *a.GoogleSubject == subject })
}

func (f *fakeAccounts) Create(_ context.Context, acc *domain.Account) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.createErr != nil {
		return f.createErr
	}

	acc.ID = uuid.NewString()
	acc.CreatedAt, acc.UpdatedAt = time.Now(), time.Now()
	copied := *acc
	f.byID[acc.ID] = &copied

	return nil
}

func (f *fakeAccounts) Update(_ context.Context, acc *domain.Account) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.updateErr != nil {
		return f.updateErr
	}

	copied := *acc
	f.byID[acc.ID] = &copied

	return nil
}

func (f *fakeAccounts) GrantPlatformAdmins(_ context.Context, emails []string) (int, error) {
	f.granted = emails
	if f.findErr != nil {
		return 0, f.findErr
	}

	return len(emails), nil
}

func (f *fakeAccounts) get(id string) *domain.Account {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.byID[id]
}

// fakeMailer captures messages.
type fakeMailer struct {
	mu   sync.Mutex
	sent []mail.Message
	err  error
}

func (m *fakeMailer) Send(_ context.Context, msg mail.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.err != nil {
		return m.err
	}

	m.sent = append(m.sent, msg)

	return nil
}

var codePattern = regexp.MustCompile(`\d{6}`)

// lastCode returns the OTP of the last message sent to email.
func (m *fakeMailer) lastCode(t *testing.T, email string) string {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, v := range slices.Backward(m.sent) {
		if v.ToEmail == email {
			return codePattern.FindString(v.Subject)
		}
	}

	t.Fatalf("no mail to %s", email)

	return ""
}

// fakeEvents captures published events.
type fakeEvents struct {
	types []string
	err   error
}

func (e *fakeEvents) Publish(_ context.Context, eventType, _, _ string, _ any) error {
	e.types = append(e.types, eventType)

	return e.err
}

// passTx runs fn directly.
type passTx struct{ err error }

func (p passTx) WithTransaction(ctx context.Context, _ platformdb.Scope, fn func(context.Context) error) error {
	if p.err != nil {
		return p.err
	}

	return fn(ctx)
}

type env struct {
	uc       *usecase.IdentityUseCase
	accounts *fakeAccounts
	store    *repository.MemoryStore
	mailer   *fakeMailer
	events   *fakeEvents
	now      time.Time
	clock    *time.Time
	issuer   *auth.TokenIssuer
}

func newEnv(t *testing.T) *env {
	t.Helper()

	now := time.Now().UTC().Truncate(time.Second)
	e := &env{
		accounts: newFakeAccounts(), store: repository.NewMemoryStore(), mailer: &fakeMailer{}, events: &fakeEvents{},
		now: now, issuer: auth.NewTokenIssuer([]byte("k"), 20*time.Minute, time.Hour),
	}
	e.clock = &now
	e.uc = usecase.NewIdentityUseCase(e.accounts, e.store, e.store, e.mailer, e.events, passTx{}, e.issuer,
		usecase.WithClock(func() time.Time { return *e.clock }))

	return e
}

func (e *env) advance(d time.Duration) { *e.clock = e.clock.Add(d) }

// register creates owner@example.com.
func (e *env) register(t *testing.T) *usecase.Session {
	t.Helper()

	session, err := e.uc.Register(t.Context(), usecase.RegisterRequest{
		Email: "Owner@Example.com", Password: "secret123", FullName: "Minh",
	})
	require.NoError(t, err)

	return session
}

func requireReason(t *testing.T, err error, reason string, _ ...string) {
	t.Helper()
	require.Error(t, err)
	require.Equal(t, reason, apperror.ReasonOf(err), err)
}

//go:fix inline
