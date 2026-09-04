package handler_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/identity/domain"
	"github.com/viettechno/vietgara-backend/internal/identity/handler"
	"github.com/viettechno/vietgara-backend/internal/identity/usecase"
	"github.com/viettechno/vietgara-backend/internal/mocks"
	"github.com/viettechno/vietgara-backend/internal/platform/auth"

	"golang.org/x/crypto/bcrypt"
)

const (
	testSigningKey = "identity-handler-test-key"
	testOTPCode    = "000000"
)

func newTestHandler(t *testing.T, accounts *mocks.MockAccountRepository, garages *mocks.MockGarageMembershipReader, tokens *mocks.MockRefreshTokenStore) *handler.Server {
	t.Helper()

	issuer := auth.NewTokenIssuer([]byte(testSigningKey), 15*time.Minute, 24*time.Hour)
	otp := mocks.NewMockOTPVerifier(t)
	otp.On("Verify", mock.Anything, mock.Anything, testOTPCode).Return(true, nil).Maybe()
	uc := usecase.NewIdentityUseCase(accounts, garages, tokens, otp, issuer, 24*time.Hour)

	return handler.NewServer(uc, stubSocialVerifier{})
}

type stubSocialVerifier struct{}

func (stubSocialVerifier) VerifyIDToken(_ context.Context, _ domain.AuthProvider, idToken string) (*usecase.VerifiedSocialProfile, error) {
	if idToken == "" {
		return nil, errors.New("empty id_token")
	}

	return &usecase.VerifiedSocialProfile{IdentityPlatformUID: "stub-" + idToken, Provider: domain.AuthProviderGoogle, FullName: "Social User"}, nil
}

func passwordHash(t *testing.T, password string) *string {
	t.Helper()

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	require.NoError(t, err)

	s := string(hash)

	return &s
}
