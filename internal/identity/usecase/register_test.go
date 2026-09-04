package usecase_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/identity/domain"
	"github.com/viettechno/vietgara-backend/internal/identity/usecase"
	"github.com/viettechno/vietgara-backend/internal/mocks"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

func TestIdentityUseCase_Register(t *testing.T) {
	t.Parallel()

	email := "new@example.com"

	t.Run("success with email and password issues a session", func(t *testing.T) {
		t.Parallel()

		accounts := mocks.NewMockAccountRepository(t)
		garages := mocks.NewMockGarageMembershipReader(t)
		tokens := mocks.NewMockRefreshTokenStore(t)
		otp := mocks.NewMockOTPVerifier(t)
		uc := newIdentityUseCase(accounts, garages, tokens, otp, newTestTokenIssuer())

		accounts.On("FindByEmail", mock.Anything, email).Return(nil, domain.ErrAccountNotFound).Once()
		accounts.On("Create", mock.Anything, mock.MatchedBy(func(acc *domain.Account) bool {
			return acc.FullName == "New User" &&
				acc.PasswordHash != nil &&
				*acc.PasswordHash != "secret123" // password must never be stored in plaintext
		})).Return(nil).Once()
		garages.On("ListGarageRolesForAccount", mock.Anything, mock.Anything).Return(nil, nil).Once()
		tokens.On("Store", mock.Anything, mock.Anything, mock.Anything, 86400).Return(nil).Once()

		result, err := uc.Register(t.Context(), usecase.RegisterRequest{
			Email:    &email,
			Password: "secret123",
			FullName: "New User",
		})
		require.NoError(t, err)
		assert.NotEmpty(t, result.AccessToken)
		assert.NotEmpty(t, result.RefreshToken)
		assert.Equal(t, "New User", result.Account.FullName)
		assert.Equal(t, domain.AuthProviderEmail, result.Account.AuthProvider)
		assert.Equal(t, domain.AccountStatusActive, result.Account.Status)
		require.NotNil(t, result.Account.PasswordHash)

		mock.AssertExpectationsForObjects(t, accounts, garages, tokens)
	})

	t.Run("phone-only registration succeeds without a password", func(t *testing.T) {
		t.Parallel()

		phone := "+84901234567"

		accounts := mocks.NewMockAccountRepository(t)
		garages := mocks.NewMockGarageMembershipReader(t)
		tokens := mocks.NewMockRefreshTokenStore(t)
		otp := mocks.NewMockOTPVerifier(t)
		uc := newIdentityUseCase(accounts, garages, tokens, otp, newTestTokenIssuer())

		accounts.On("FindByEmail", mock.Anything, mock.Anything).Maybe().Return(nil, domain.ErrAccountNotFound)
		accounts.On("FindByPhone", mock.Anything, phone).Return(nil, domain.ErrAccountNotFound).Once()
		accounts.On("Create", mock.Anything, mock.AnythingOfType("*domain.Account")).Return(nil).Once()
		garages.On("ListGarageRolesForAccount", mock.Anything, mock.Anything).Return(nil, nil).Once()
		tokens.On("Store", mock.Anything, mock.Anything, mock.Anything, 86400).Return(nil).Once()

		result, err := uc.Register(t.Context(), usecase.RegisterRequest{Phone: &phone, FullName: "Phone User"})
		require.NoError(t, err)
		assert.Equal(t, *result.Account.Phone, phone)
		assert.Nil(t, result.Account.PasswordHash)

		mock.AssertExpectationsForObjects(t, accounts, garages, tokens)
	})

	t.Run("duplicate email is rejected", func(t *testing.T) {
		t.Parallel()

		accounts := mocks.NewMockAccountRepository(t)
		garages := mocks.NewMockGarageMembershipReader(t)
		tokens := mocks.NewMockRefreshTokenStore(t)
		otp := mocks.NewMockOTPVerifier(t)
		uc := newIdentityUseCase(accounts, garages, tokens, otp, newTestTokenIssuer())

		accounts.On("FindByEmail", mock.Anything, email).Return(activeEmailAccount(t, email), nil).Once()

		_, err := uc.Register(t.Context(), usecase.RegisterRequest{Email: &email, Password: "x", FullName: "N"})
		assertAppError(t, err, apperror.CodeAlreadyExists)
	})

	t.Run("duplicate phone is rejected", func(t *testing.T) {
		t.Parallel()

		phone := "+84901234567"

		accounts := mocks.NewMockAccountRepository(t)
		garages := mocks.NewMockGarageMembershipReader(t)
		tokens := mocks.NewMockRefreshTokenStore(t)
		otp := mocks.NewMockOTPVerifier(t)
		uc := newIdentityUseCase(accounts, garages, tokens, otp, newTestTokenIssuer())

		accounts.On("FindByEmail", mock.Anything, mock.Anything).Maybe().Return(nil, domain.ErrAccountNotFound)
		accounts.On("FindByPhone", mock.Anything, phone).Return(&domain.Account{ID: "acc-existing"}, nil).Once()

		_, err := uc.Register(t.Context(), usecase.RegisterRequest{Phone: &phone, FullName: "N"})
		assertAppError(t, err, apperror.CodeAlreadyExists)
	})

	t.Run("missing identifiers is invalid", func(t *testing.T) {
		t.Parallel()

		accounts := mocks.NewMockAccountRepository(t)
		garages := mocks.NewMockGarageMembershipReader(t)
		tokens := mocks.NewMockRefreshTokenStore(t)
		otp := mocks.NewMockOTPVerifier(t)
		uc := newIdentityUseCase(accounts, garages, tokens, otp, newTestTokenIssuer())

		_, err := uc.Register(t.Context(), usecase.RegisterRequest{FullName: "N"})
		assertAppError(t, err, apperror.CodeInvalidArgument)
	})
}

func TestIdentityUseCase_Register_ValidationErrors(t *testing.T) {
	t.Parallel()

	email := "new@example.com"

	t.Run("missing full name is invalid", func(t *testing.T) {
		t.Parallel()

		accounts := mocks.NewMockAccountRepository(t)
		garages := mocks.NewMockGarageMembershipReader(t)
		tokens := mocks.NewMockRefreshTokenStore(t)
		otp := mocks.NewMockOTPVerifier(t)
		uc := newIdentityUseCase(accounts, garages, tokens, otp, newTestTokenIssuer())

		_, err := uc.Register(t.Context(), usecase.RegisterRequest{Email: &email, Password: "x"})
		assertAppError(t, err, apperror.CodeInvalidArgument)
	})

	t.Run("email uniqueness check failure is internal", func(t *testing.T) {
		t.Parallel()

		accounts := mocks.NewMockAccountRepository(t)
		garages := mocks.NewMockGarageMembershipReader(t)
		tokens := mocks.NewMockRefreshTokenStore(t)
		otp := mocks.NewMockOTPVerifier(t)
		uc := newIdentityUseCase(accounts, garages, tokens, otp, newTestTokenIssuer())

		accounts.On("FindByEmail", mock.Anything, email).Return(nil, errors.New("connection refused")).Once()

		_, err := uc.Register(t.Context(), usecase.RegisterRequest{Email: &email, Password: "x", FullName: "N"})
		assertAppError(t, err, apperror.CodeInternal)
	})

	t.Run("phone uniqueness check failure is internal", func(t *testing.T) {
		t.Parallel()

		phone := "+84901234567"

		accounts := mocks.NewMockAccountRepository(t)
		garages := mocks.NewMockGarageMembershipReader(t)
		tokens := mocks.NewMockRefreshTokenStore(t)
		otp := mocks.NewMockOTPVerifier(t)
		uc := newIdentityUseCase(accounts, garages, tokens, otp, newTestTokenIssuer())

		accounts.On("FindByEmail", mock.Anything, mock.Anything).Maybe().Return(nil, domain.ErrAccountNotFound)
		accounts.On("FindByPhone", mock.Anything, phone).Return(nil, errors.New("connection refused")).Once()

		_, err := uc.Register(t.Context(), usecase.RegisterRequest{Phone: &phone, FullName: "N"})
		assertAppError(t, err, apperror.CodeInternal)
	})

	t.Run("account creation failure is internal", func(t *testing.T) {
		t.Parallel()

		accounts := mocks.NewMockAccountRepository(t)
		garages := mocks.NewMockGarageMembershipReader(t)
		tokens := mocks.NewMockRefreshTokenStore(t)
		otp := mocks.NewMockOTPVerifier(t)
		uc := newIdentityUseCase(accounts, garages, tokens, otp, newTestTokenIssuer())

		accounts.On("FindByEmail", mock.Anything, email).Return(nil, domain.ErrAccountNotFound).Once()
		accounts.On("Create", mock.Anything, mock.AnythingOfType("*domain.Account")).Return(errors.New("insert boom")).Once()

		_, err := uc.Register(t.Context(), usecase.RegisterRequest{Email: &email, Password: "x", FullName: "N"})
		assertAppError(t, err, apperror.CodeInternal)
	})
}
