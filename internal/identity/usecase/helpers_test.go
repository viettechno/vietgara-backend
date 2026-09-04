package usecase_test

import (
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/viettechno/vietgara-backend/internal/identity/usecase"
	"github.com/viettechno/vietgara-backend/internal/mocks"
	"github.com/viettechno/vietgara-backend/internal/platform/auth"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

const testSigningKey = "identity-test-signing-key"

// assertAppError asserts that err is an AppError carrying wantCode.
func assertAppError(t *testing.T, err error, wantCode apperror.Code) {
	t.Helper()

	appErr, ok := apperror.As(err)
	require.True(t, ok, "expected an AppError, got %v", err)
	assert.Equal(t, wantCode, appErr.Code)
}

// tokenClaims parses the access token with the test signing key and
// returns the claims it carries.
func tokenClaims(t *testing.T, token string) (*auth.Claims, error) {
	t.Helper()

	parsed, err := jwt.ParseWithClaims(token, &auth.Claims{}, func(_ *jwt.Token) (any, error) {
		return []byte(testSigningKey), nil
	})
	if err != nil {
		return nil, err
	}

	claims, ok := parsed.Claims.(*auth.Claims)
	if !ok {
		return nil, errors.New("unexpected claims type")
	}

	return claims, nil
}

func newTestTokenIssuer() *auth.TokenIssuer {
	return auth.NewTokenIssuer([]byte(testSigningKey), 15*time.Minute, 24*time.Hour)
}

func newIdentityUseCase(
	accounts *mocks.MockAccountRepository,
	garageReader *mocks.MockGarageMembershipReader,
	tokens *mocks.MockRefreshTokenStore,
	otp *mocks.MockOTPVerifier,
	issuer *auth.TokenIssuer,
) *usecase.IdentityUseCase {
	return usecase.NewIdentityUseCase(accounts, garageReader, tokens, otp, issuer, 24*time.Hour)
}
