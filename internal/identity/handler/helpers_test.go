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

type users struct {
	mu   sync.Mutex
	byID map[string]*domain.User
}

func (a *users) find(match func(*domain.User) bool) (*domain.User, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	for _, user := range a.byID {
		if match(user) {
			copied := *user

			return &copied, nil
		}
	}

	return nil, domain.ErrUserNotFound
}

func (a *users) FindByID(_ context.Context, id string) (*domain.User, error) {
	return a.find(func(user *domain.User) bool { return user.ID == id })
}

func (a *users) FindByEmail(_ context.Context, email string) (*domain.User, error) {
	return a.find(func(user *domain.User) bool { return strings.EqualFold(user.Email, email) })
}

func (a *users) FindByPhone(_ context.Context, phone string) (*domain.User, error) {
	return a.find(func(user *domain.User) bool { return user.Phone != nil && *user.Phone == phone })
}

func (a *users) FindByGoogleSubject(_ context.Context, sub string) (*domain.User, error) {
	return a.find(func(user *domain.User) bool { return user.GoogleSubject != nil && *user.GoogleSubject == sub })
}

func (a *users) Create(_ context.Context, user *domain.User) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	user.ID = uuid.NewString()
	copied := *user
	a.byID[user.ID] = &copied

	return nil
}

func (a *users) Update(_ context.Context, user *domain.User) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	copied := *user
	a.byID[user.ID] = &copied

	return nil
}

func (a *users) GrantPlatformAdmins(context.Context, []string) (int, error) { return 0, nil }

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
	users  *users
}

func newEnv(t *testing.T, google handler.GoogleOAuthConfig) *env {
	t.Helper()

	issuer := auth.NewTokenIssuer([]byte("k"), 20*time.Minute, time.Hour)
	store := repository.NewMemoryStore()
	m := &mailer{}
	repo := &users{byID: map[string]*domain.User{}}
	uc := usecase.NewIdentityUseCase(repo, store, store, m, events{}, passTx{}, issuer)

	return &env{srv: handler.NewServer(uc, 1200, google), issuer: issuer, mail: m, users: repo}
}

// as returns ctx signed in as the token's user.
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
