package usecase_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/viettechno/vietgara-backend/internal/identity/domain"
	"github.com/viettechno/vietgara-backend/internal/identity/usecase"
	"github.com/viettechno/vietgara-backend/internal/mocks"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

const defaultPassword = "secret123"

func validPasswordHash(t *testing.T, password string) string {
	t.Helper()

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	require.NoError(t, err)

	return string(hash)
}

func activeEmailAccount(t *testing.T, email string) *domain.Account {
	t.Helper()

	hash := validPasswordHash(t, defaultPassword)

	return &domain.Account{
		ID:           "acc-1",
		Email:        &email,
		AuthProvider: domain.AuthProviderEmail,
		FullName:     "Test User",
		PasswordHash: &hash,
		Status:       domain.AccountStatusActive,
	}
}

func TestIdentityUseCase_Login_EmailPassword(t *testing.T) {
	t.Parallel()

	email := "user@example.com"

	t.Run("success issues session with garage claims", func(t *testing.T) {
		t.Parallel()

		accounts := mocks.NewMockAccountRepository(t)
		garages := mocks.NewMockGarageMembershipReader(t)
		tokens := mocks.NewMockRefreshTokenStore(t)
		otp := mocks.NewMockOTPVerifier(t)
		uc := newIdentityUseCase(accounts, garages, tokens, otp, newTestTokenIssuer())

		accounts.On("FindByEmail", mock.Anything, email).Return(activeEmailAccount(t, email), nil).Once()
		garages.On("ListGarageRolesForAccount", mock.Anything, "acc-1").
			Return([]usecase.GarageRole{{GarageID: "g-1", Role: "owner"}}, nil).Once()
		tokens.On("Store", mock.Anything, mock.Anything, "acc-1", 86400).Return(nil).Once()

		result, err := uc.Login(t.Context(), usecase.LoginRequest{Email: &email, Password: new(defaultPassword)})
		require.NoError(t, err)
		require.NotEmpty(t, result.AccessToken)
		require.NotEmpty(t, result.RefreshToken)
		assert.Equal(t, "acc-1", result.Account.ID)
		require.Len(t, result.Garages, 1)
		assert.Equal(t, "g-1", result.Garages[0].GarageID)
		assert.Equal(t, "owner", result.Garages[0].Role)

		claims, err := tokenClaims(t, result.AccessToken)
		require.NoError(t, err)
		assert.Equal(t, "acc-1", claims.Subject)
		require.Len(t, claims.Garages, 1)
		assert.Equal(t, "owner", claims.Garages[0].Role)

		mock.AssertExpectationsForObjects(t, accounts, garages, tokens)
	})

	t.Run("wrong password is rejected", func(t *testing.T) {
		t.Parallel()

		accounts := mocks.NewMockAccountRepository(t)
		garages := mocks.NewMockGarageMembershipReader(t)
		tokens := mocks.NewMockRefreshTokenStore(t)
		otp := mocks.NewMockOTPVerifier(t)
		uc := newIdentityUseCase(accounts, garages, tokens, otp, newTestTokenIssuer())

		accounts.On("FindByEmail", mock.Anything, email).Return(activeEmailAccount(t, email), nil).Once()

		_, err := uc.Login(t.Context(), usecase.LoginRequest{Email: &email, Password: new("wrong")})
		assertAppError(t, err, apperror.CodeUnauthenticated)
	})

	t.Run("unknown email is indistinguishable from wrong password", func(t *testing.T) {
		t.Parallel()

		accounts := mocks.NewMockAccountRepository(t)
		garages := mocks.NewMockGarageMembershipReader(t)
		tokens := mocks.NewMockRefreshTokenStore(t)
		otp := mocks.NewMockOTPVerifier(t)
		uc := newIdentityUseCase(accounts, garages, tokens, otp, newTestTokenIssuer())

		accounts.On("FindByEmail", mock.Anything, email).Return(nil, domain.ErrAccountNotFound).Once()

		_, err := uc.Login(t.Context(), usecase.LoginRequest{Email: &email, Password: new(defaultPassword)})
		assertAppError(t, err, apperror.CodeUnauthenticated)
	})

	t.Run("suspended account is rejected", func(t *testing.T) {
		t.Parallel()

		accounts := mocks.NewMockAccountRepository(t)
		garages := mocks.NewMockGarageMembershipReader(t)
		tokens := mocks.NewMockRefreshTokenStore(t)
		otp := mocks.NewMockOTPVerifier(t)
		uc := newIdentityUseCase(accounts, garages, tokens, otp, newTestTokenIssuer())

		acc := activeEmailAccount(t, email)
		acc.Status = domain.AccountStatusSuspended
		accounts.On("FindByEmail", mock.Anything, email).Return(acc, nil).Once()

		_, err := uc.Login(t.Context(), usecase.LoginRequest{Email: &email, Password: new(defaultPassword)})
		assertAppError(t, err, apperror.CodeFailedPrecondition)
	})

	t.Run("missing both credential pairs is invalid", func(t *testing.T) {
		t.Parallel()

		accounts := mocks.NewMockAccountRepository(t)
		garages := mocks.NewMockGarageMembershipReader(t)
		tokens := mocks.NewMockRefreshTokenStore(t)
		otp := mocks.NewMockOTPVerifier(t)
		uc := newIdentityUseCase(accounts, garages, tokens, otp, newTestTokenIssuer())

		_, err := uc.Login(t.Context(), usecase.LoginRequest{})
		assertAppError(t, err, apperror.CodeInvalidArgument)
	})

	t.Run("repository failure is internal", func(t *testing.T) {
		t.Parallel()

		accounts := mocks.NewMockAccountRepository(t)
		garages := mocks.NewMockGarageMembershipReader(t)
		tokens := mocks.NewMockRefreshTokenStore(t)
		otp := mocks.NewMockOTPVerifier(t)
		uc := newIdentityUseCase(accounts, garages, tokens, otp, newTestTokenIssuer())

		accounts.On("FindByEmail", mock.Anything, email).Return(nil, errors.New("connection refused")).Once()

		_, err := uc.Login(t.Context(), usecase.LoginRequest{Email: &email, Password: new(defaultPassword)})
		assertAppError(t, err, apperror.CodeInternal)
	})
}

func TestIdentityUseCase_Login_PhoneOTP(t *testing.T) {
	t.Parallel()

	phone := "+84123456789"

	t.Run("success with valid otp", func(t *testing.T) {
		t.Parallel()

		accounts := mocks.NewMockAccountRepository(t)
		garages := mocks.NewMockGarageMembershipReader(t)
		tokens := mocks.NewMockRefreshTokenStore(t)
		otp := mocks.NewMockOTPVerifier(t)
		otp.On("Verify", mock.Anything, phone, "123456").Return(true, nil).Once()
		uc := newIdentityUseCase(accounts, garages, tokens, otp, newTestTokenIssuer())

		acc := &domain.Account{
			ID:           "acc-2",
			Phone:        &phone,
			AuthProvider: domain.AuthProviderEmail,
			FullName:     "Phone User",
			Status:       domain.AccountStatusActive,
		}
		accounts.On("FindByPhone", mock.Anything, phone).Return(acc, nil).Once()
		garages.On("ListGarageRolesForAccount", mock.Anything, "acc-2").Return(nil, nil).Once()
		tokens.On("Store", mock.Anything, mock.Anything, "acc-2", 86400).Return(nil).Once()

		result, err := uc.Login(t.Context(), usecase.LoginRequest{Phone: &phone, OTPCode: new("123456")})
		require.NoError(t, err)
		assert.Equal(t, "acc-2", result.Account.ID)

		mock.AssertExpectationsForObjects(t, accounts, garages, tokens)
	})

	t.Run("invalid otp is rejected", func(t *testing.T) {
		t.Parallel()

		accounts := mocks.NewMockAccountRepository(t)
		garages := mocks.NewMockGarageMembershipReader(t)
		tokens := mocks.NewMockRefreshTokenStore(t)
		otp := mocks.NewMockOTPVerifier(t)
		otp.On("Verify", mock.Anything, phone, "000000").Return(false, nil).Once()
		uc := newIdentityUseCase(accounts, garages, tokens, otp, newTestTokenIssuer())

		_, err := uc.Login(t.Context(), usecase.LoginRequest{Phone: &phone, OTPCode: new("000000")})
		assertAppError(t, err, apperror.CodeUnauthenticated)
	})

	t.Run("otp verifier failure is internal", func(t *testing.T) {
		t.Parallel()

		accounts := mocks.NewMockAccountRepository(t)
		garages := mocks.NewMockGarageMembershipReader(t)
		tokens := mocks.NewMockRefreshTokenStore(t)
		otp := mocks.NewMockOTPVerifier(t)
		otp.On("Verify", mock.Anything, phone, "123456").Return(false, errors.New("otp provider down")).Once()
		uc := newIdentityUseCase(accounts, garages, tokens, otp, newTestTokenIssuer())

		_, err := uc.Login(t.Context(), usecase.LoginRequest{Phone: &phone, OTPCode: new("123456")})
		assertAppError(t, err, apperror.CodeInternal)
	})

	t.Run("valid otp but unknown phone is unauthenticated", func(t *testing.T) {
		t.Parallel()

		accounts := mocks.NewMockAccountRepository(t)
		garages := mocks.NewMockGarageMembershipReader(t)
		tokens := mocks.NewMockRefreshTokenStore(t)
		otp := mocks.NewMockOTPVerifier(t)
		otp.On("Verify", mock.Anything, phone, "123456").Return(true, nil).Once()
		uc := newIdentityUseCase(accounts, garages, tokens, otp, newTestTokenIssuer())

		accounts.On("FindByPhone", mock.Anything, phone).Return(nil, domain.ErrAccountNotFound).Once()

		_, err := uc.Login(t.Context(), usecase.LoginRequest{Phone: &phone, OTPCode: new("123456")})
		assertAppError(t, err, apperror.CodeUnauthenticated)
	})

	t.Run("phone lookup failure is internal", func(t *testing.T) {
		t.Parallel()

		accounts := mocks.NewMockAccountRepository(t)
		garages := mocks.NewMockGarageMembershipReader(t)
		tokens := mocks.NewMockRefreshTokenStore(t)
		otp := mocks.NewMockOTPVerifier(t)
		otp.On("Verify", mock.Anything, phone, "123456").Return(true, nil).Once()
		uc := newIdentityUseCase(accounts, garages, tokens, otp, newTestTokenIssuer())

		accounts.On("FindByPhone", mock.Anything, phone).Return(nil, errors.New("connection refused")).Once()

		_, err := uc.Login(t.Context(), usecase.LoginRequest{Phone: &phone, OTPCode: new("123456")})
		assertAppError(t, err, apperror.CodeInternal)
	})
}

func TestLoginUseCase_Execute_SessionIssuanceFailures(t *testing.T) {
	t.Parallel()

	email := "user@example.com"

	t.Run("garage reader failure is internal", func(t *testing.T) {
		t.Parallel()

		accounts := mocks.NewMockAccountRepository(t)
		garages := mocks.NewMockGarageMembershipReader(t)
		tokens := mocks.NewMockRefreshTokenStore(t)
		otp := mocks.NewMockOTPVerifier(t)
		uc := newIdentityUseCase(accounts, garages, tokens, otp, newTestTokenIssuer())

		accounts.On("FindByEmail", mock.Anything, email).Return(activeEmailAccount(t, email), nil).Once()
		garages.On("ListGarageRolesForAccount", mock.Anything, "acc-1").Return(nil, errors.New("tenant db down")).Once()

		_, err := uc.Login(t.Context(), usecase.LoginRequest{Email: &email, Password: new(defaultPassword)})
		assertAppError(t, err, apperror.CodeInternal)
	})

	t.Run("refresh token store failure is internal", func(t *testing.T) {
		t.Parallel()

		accounts := mocks.NewMockAccountRepository(t)
		garages := mocks.NewMockGarageMembershipReader(t)
		tokens := mocks.NewMockRefreshTokenStore(t)
		otp := mocks.NewMockOTPVerifier(t)
		uc := newIdentityUseCase(accounts, garages, tokens, otp, newTestTokenIssuer())

		accounts.On("FindByEmail", mock.Anything, email).Return(activeEmailAccount(t, email), nil).Once()
		garages.On("ListGarageRolesForAccount", mock.Anything, "acc-1").Return(nil, nil).Once()
		tokens.On("Store", mock.Anything, mock.Anything, "acc-1", 86400).Return(errors.New("redis down")).Once()

		_, err := uc.Login(t.Context(), usecase.LoginRequest{Email: &email, Password: new(defaultPassword)})
		assertAppError(t, err, apperror.CodeInternal)
	})
}
