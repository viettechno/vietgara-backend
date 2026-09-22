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

func TestIdentityUseCase_SocialLogin(t *testing.T) {
	t.Parallel()

	uid := "google-uid-123"

	t.Run("first login creates the account and issues a session", func(t *testing.T) {
		t.Parallel()

		accounts := mocks.NewMockAccountRepository(t)
		garages := mocks.NewMockGarageMembershipReader(t)
		tokens := mocks.NewMockRefreshTokenStore(t)
		otp := mocks.NewMockOTPVerifier(t)
		uc := newIdentityUseCase(accounts, garages, tokens, otp, newTestTokenIssuer())

		accounts.On("FindByIdentityPlatformUID", mock.Anything, uid).Return(nil, domain.ErrAccountNotFound).Once()
		accounts.On("Create", mock.Anything, mock.MatchedBy(func(acc *domain.Account) bool {
			return acc.Status == domain.AccountStatusActive &&
				acc.IdentityPlatformUID != nil &&
				*acc.IdentityPlatformUID == uid &&
				acc.AuthProvider == domain.AuthProviderGoogle
		})).Return(nil).Once()
		garages.On("ListGarageRolesForAccount", mock.Anything, mock.Anything).Return(nil, nil).Once()
		tokens.On("Store", mock.Anything, mock.Anything, mock.Anything, 86400).Return(nil).Once()

		result, err := uc.SocialLogin(t.Context(), usecase.VerifiedSocialProfile{
			IdentityPlatformUID: uid,
			Provider:            domain.AuthProviderGoogle,
			Email:               new("social@example.com"),
			FullName:            "Social User",
		})
		require.NoError(t, err)
		assert.Equal(t, domain.AuthProviderGoogle, result.Account.AuthProvider)
		require.NotNil(t, result.Account.IdentityPlatformUID)
		assert.Equal(t, uid, *result.Account.IdentityPlatformUID)

		mock.AssertExpectationsForObjects(t, accounts, garages, tokens)
	})

	t.Run("known uid logs the existing account in", func(t *testing.T) {
		t.Parallel()

		accounts := mocks.NewMockAccountRepository(t)
		garages := mocks.NewMockGarageMembershipReader(t)
		tokens := mocks.NewMockRefreshTokenStore(t)
		otp := mocks.NewMockOTPVerifier(t)
		uc := newIdentityUseCase(accounts, garages, tokens, otp, newTestTokenIssuer())

		existing := &domain.Account{
			ID:                  "acc-social",
			AuthProvider:        domain.AuthProviderFacebook,
			IdentityPlatformUID: &uid,
			FullName:            "Existing",
			Status:              domain.AccountStatusActive,
		}
		accounts.On("FindByIdentityPlatformUID", mock.Anything, uid).Return(existing, nil).Once()
		garages.On("ListGarageRolesForAccount", mock.Anything, "acc-social").Return(nil, nil).Once()
		tokens.On("Store", mock.Anything, mock.Anything, "acc-social", 86400).Return(nil).Once()

		result, err := uc.SocialLogin(t.Context(), usecase.VerifiedSocialProfile{
			IdentityPlatformUID: uid,
			Provider:            domain.AuthProviderFacebook,
			FullName:            "Existing",
		})
		require.NoError(t, err)
		assert.Equal(t, "acc-social", result.Account.ID)
		accounts.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)

		mock.AssertExpectationsForObjects(t, accounts, garages, tokens)
	})

	t.Run("suspended social account is rejected", func(t *testing.T) {
		t.Parallel()

		accounts := mocks.NewMockAccountRepository(t)
		garages := mocks.NewMockGarageMembershipReader(t)
		tokens := mocks.NewMockRefreshTokenStore(t)
		otp := mocks.NewMockOTPVerifier(t)
		uc := newIdentityUseCase(accounts, garages, tokens, otp, newTestTokenIssuer())

		existing := &domain.Account{
			ID:                  "acc-suspended",
			AuthProvider:        domain.AuthProviderGoogle,
			IdentityPlatformUID: &uid,
			Status:              domain.AccountStatusSuspended,
		}
		accounts.On("FindByIdentityPlatformUID", mock.Anything, uid).Return(existing, nil).Once()

		_, err := uc.SocialLogin(t.Context(), usecase.VerifiedSocialProfile{IdentityPlatformUID: uid, Provider: domain.AuthProviderGoogle})
		assertAppError(t, err, apperror.CodeFailedPrecondition)
	})

	t.Run("repository failure is internal", func(t *testing.T) {
		t.Parallel()

		accounts := mocks.NewMockAccountRepository(t)
		garages := mocks.NewMockGarageMembershipReader(t)
		tokens := mocks.NewMockRefreshTokenStore(t)
		otp := mocks.NewMockOTPVerifier(t)
		uc := newIdentityUseCase(accounts, garages, tokens, otp, newTestTokenIssuer())

		accounts.On("FindByIdentityPlatformUID", mock.Anything, uid).Return(nil, errors.New("connection refused")).Once()

		_, err := uc.SocialLogin(t.Context(), usecase.VerifiedSocialProfile{IdentityPlatformUID: uid, Provider: domain.AuthProviderGoogle})
		assertAppError(t, err, apperror.CodeInternal)
	})
}

func TestIdentityUseCase_Refresh(t *testing.T) {
	t.Parallel()

	issuer := newTestTokenIssuer()

	t.Run("valid refresh token is rotated for a fresh pair", func(t *testing.T) {
		t.Parallel()

		accounts := mocks.NewMockAccountRepository(t)
		garages := mocks.NewMockGarageMembershipReader(t)
		tokens := mocks.NewMockRefreshTokenStore(t)
		otp := mocks.NewMockOTPVerifier(t)
		uc := newIdentityUseCase(accounts, garages, tokens, otp, issuer)

		accounts.On("FindByID", mock.Anything, "acc-1").Return(activeEmailAccount(t, "u@example.com"), nil).Once()
		tokens.On("GetAccountID", mock.Anything, "old-token").Return("acc-1", nil).Once()
		tokens.On("Revoke", mock.Anything, "old-token").Return(nil).Once()
		garages.On("ListGarageRolesForAccount", mock.Anything, "acc-1").Return(nil, nil).Once()
		tokens.On("Store", mock.Anything, mock.Anything, "acc-1", 86400).Return(nil).Once()

		result, err := uc.Refresh(t.Context(), "old-token")
		require.NoError(t, err)
		assert.NotEmpty(t, result.AccessToken)
		assert.NotEmpty(t, result.RefreshToken)

		mock.AssertExpectationsForObjects(t, accounts, garages, tokens)
	})

	t.Run("unknown refresh token is rejected", func(t *testing.T) {
		t.Parallel()

		accounts := mocks.NewMockAccountRepository(t)
		garages := mocks.NewMockGarageMembershipReader(t)
		tokens := mocks.NewMockRefreshTokenStore(t)
		otp := mocks.NewMockOTPVerifier(t)
		uc := newIdentityUseCase(accounts, garages, tokens, otp, issuer)

		tokens.On("GetAccountID", mock.Anything, "bad-token").Return("", errors.New("not found")).Once()

		_, err := uc.Refresh(t.Context(), "bad-token")
		assertAppError(t, err, apperror.CodeUnauthenticated)
	})

	t.Run("revoked token cannot be reused", func(t *testing.T) {
		t.Parallel()

		accounts := mocks.NewMockAccountRepository(t)
		garages := mocks.NewMockGarageMembershipReader(t)
		tokens := mocks.NewMockRefreshTokenStore(t)
		otp := mocks.NewMockOTPVerifier(t)
		uc := newIdentityUseCase(accounts, garages, tokens, otp, issuer)

		tokens.On("GetAccountID", mock.Anything, "revoked").Return("", errors.New("refresh token not found or revoked")).Once()

		_, err := uc.Refresh(t.Context(), "revoked")
		assertAppError(t, err, apperror.CodeUnauthenticated)
	})

	t.Run("unknown account is unauthenticated", func(t *testing.T) {
		t.Parallel()

		accounts := mocks.NewMockAccountRepository(t)
		garages := mocks.NewMockGarageMembershipReader(t)
		tokens := mocks.NewMockRefreshTokenStore(t)
		otp := mocks.NewMockOTPVerifier(t)
		uc := newIdentityUseCase(accounts, garages, tokens, otp, issuer)

		tokens.On("GetAccountID", mock.Anything, "session-1").Return("acc-gone", nil).Once()
		accounts.On("FindByID", mock.Anything, "acc-gone").Return(nil, domain.ErrAccountNotFound).Once()

		_, err := uc.Refresh(t.Context(), "session-1")
		assertAppError(t, err, apperror.CodeUnauthenticated)
	})

	t.Run("revoke failure is internal", func(t *testing.T) {
		t.Parallel()

		accounts := mocks.NewMockAccountRepository(t)
		garages := mocks.NewMockGarageMembershipReader(t)
		tokens := mocks.NewMockRefreshTokenStore(t)
		otp := mocks.NewMockOTPVerifier(t)
		uc := newIdentityUseCase(accounts, garages, tokens, otp, issuer)

		tokens.On("GetAccountID", mock.Anything, "session-1").Return("acc-1", nil).Once()
		accounts.On("FindByID", mock.Anything, "acc-1").Return(activeEmailAccount(t, "u@example.com"), nil).Once()
		tokens.On("Revoke", mock.Anything, "session-1").Return(errors.New("redis down")).Once()

		_, err := uc.Refresh(t.Context(), "session-1")
		assertAppError(t, err, apperror.CodeInternal)
	})
}

func TestIdentityUseCase_Logout(t *testing.T) {
	t.Parallel()

	t.Run("revokes the presented refresh token", func(t *testing.T) {
		t.Parallel()

		tokens := mocks.NewMockRefreshTokenStore(t)
		uc := usecase.NewIdentityUseCase(nil, nil, tokens, nil, nil, 0)

		tokens.On("Revoke", mock.Anything, "session-token").Return(nil).Once()

		err := uc.Logout(t.Context(), "session-token")
		require.NoError(t, err)
		mock.AssertExpectationsForObjects(t, tokens)
	})

	t.Run("store failure is internal", func(t *testing.T) {
		t.Parallel()

		tokens := mocks.NewMockRefreshTokenStore(t)
		uc := usecase.NewIdentityUseCase(nil, nil, tokens, nil, nil, 0)

		tokens.On("Revoke", mock.Anything, "session-token").Return(errors.New("redis down")).Once()

		err := uc.Logout(t.Context(), "session-token")
		assertAppError(t, err, apperror.CodeInternal)
	})
}
