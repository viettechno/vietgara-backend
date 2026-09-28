package handler_test

import (
	"context"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/status"

	"github.com/viettechno/vietgara-backend/internal/identity/domain"
	"github.com/viettechno/vietgara-backend/internal/identity/handler"
	"github.com/viettechno/vietgara-backend/internal/identity/repository"
	"github.com/viettechno/vietgara-backend/internal/identity/usecase"
	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/platform/auth"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platform/mail"
)

type accounts struct {
	mu   sync.Mutex
	byID map[string]*domain.Account
}

func (a *accounts) find(match func(*domain.Account) bool) (*domain.Account, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	for _, acc := range a.byID {
		if match(acc) {
			copied := *acc

			return &copied, nil
		}
	}

	return nil, domain.ErrAccountNotFound
}

func (a *accounts) FindByID(_ context.Context, id string) (*domain.Account, error) {
	return a.find(func(acc *domain.Account) bool { return acc.ID == id })
}

func (a *accounts) FindByEmail(_ context.Context, email string) (*domain.Account, error) {
	return a.find(func(acc *domain.Account) bool { return strings.EqualFold(acc.Email, email) })
}

func (a *accounts) FindByPhone(_ context.Context, phone string) (*domain.Account, error) {
	return a.find(func(acc *domain.Account) bool { return acc.Phone != nil && *acc.Phone == phone })
}

func (a *accounts) FindByGoogleSubject(_ context.Context, sub string) (*domain.Account, error) {
	return a.find(func(acc *domain.Account) bool { return acc.GoogleSubject != nil && *acc.GoogleSubject == sub })
}

func (a *accounts) Create(_ context.Context, acc *domain.Account) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	acc.ID = uuid.NewString()
	copied := *acc
	a.byID[acc.ID] = &copied

	return nil
}

func (a *accounts) Update(_ context.Context, acc *domain.Account) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	copied := *acc
	a.byID[acc.ID] = &copied

	return nil
}

func (a *accounts) GrantPlatformAdmins(context.Context, []string) (int, error) { return 0, nil }

type mailer struct {
	mu   sync.Mutex
	sent []mail.Message
}

func (m *mailer) Send(_ context.Context, msg mail.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.sent = append(m.sent, msg)

	return nil
}

var code = regexp.MustCompile(`\d{6}`)

func (m *mailer) lastCode() string {
	m.mu.Lock()
	defer m.mu.Unlock()

	return code.FindString(m.sent[len(m.sent)-1].Subject)
}

type events struct{}

func (events) Publish(context.Context, string, string, string, any) error { return nil }

type passTx struct{}

func (passTx) WithTransaction(ctx context.Context, _ platformdb.Scope, fn func(context.Context) error) error {
	return fn(ctx)
}

type env struct {
	srv    *handler.Server
	issuer *auth.TokenIssuer
	mail   *mailer
}

func newEnv(t *testing.T, google handler.GoogleOAuthConfig) *env {
	t.Helper()

	issuer := auth.NewTokenIssuer([]byte("k"), 20*time.Minute, time.Hour)
	store := repository.NewMemoryStore()
	m := &mailer{}
	uc := usecase.NewIdentityUseCase(&accounts{byID: map[string]*domain.Account{}}, store, store, m, events{}, passTx{}, issuer)

	return &env{srv: handler.NewServer(uc, 1200, google), issuer: issuer, mail: m}
}

// as returns ctx signed in as the token's account.
func (e *env) as(t *testing.T, accessToken string) context.Context {
	t.Helper()

	claims, err := e.issuer.Verify(accessToken)
	if err != nil {
		t.Fatal(err)
	}

	return middleware.WithClaims(t.Context(), claims)
}

func reason(err error) string {
	for _, d := range status.Convert(err).Details() {
		if info, ok := d.(*errdetails.ErrorInfo); ok {
			return info.GetReason()
		}
	}

	return ""
}
