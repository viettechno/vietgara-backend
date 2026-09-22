package handler_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/viettechno/vietgara-backend/internal/identity/domain"
	"github.com/viettechno/vietgara-backend/internal/mocks"
	identityv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/identity/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestServer_RefreshToken(t *testing.T) {
	t.Parallel()
	t.Run("valid token returns a fresh session", func(t *testing.T) {
		t.Parallel()
		accounts := mocks.NewMockAccountRepository(t)
		garages := mocks.NewMockGarageMembershipReader(t)
		tokens := mocks.NewMockRefreshTokenStore(t)
		srv := newTestHandler(t, accounts, garages, tokens)
		accounts.On("FindByID", mock.Anything, "acc-1").Return(&domain.Account{ID: "acc-1", AuthProvider: domain.AuthProviderEmail, Status: domain.AccountStatusActive}, nil).Once()
		tokens.On("GetAccountID", mock.Anything, "session-1").Return("acc-1", nil).Once()
		tokens.On("Revoke", mock.Anything, "session-1").Return(nil).Once()
		garages.On("ListGarageRolesForAccount", mock.Anything, "acc-1").Return(nil, nil).Once()
		tokens.On("Store", mock.Anything, mock.Anything, "acc-1", 86400).Return(nil).Once()

		resp, err := srv.RefreshToken(t.Context(), &identityv1.RefreshTokenRequest{RefreshToken: "session-1"})
		require.NoError(t, err)
		assert.NotEmpty(t, resp.GetAuth().GetAccessToken())
	})
	t.Run("missing token is invalid", func(t *testing.T) {
		t.Parallel()
		accounts := mocks.NewMockAccountRepository(t)
		garages := mocks.NewMockGarageMembershipReader(t)
		tokens := mocks.NewMockRefreshTokenStore(t)
		srv := newTestHandler(t, accounts, garages, tokens)
		_, err := srv.RefreshToken(t.Context(), &identityv1.RefreshTokenRequest{})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})
	t.Run("invalid token maps to Unauthenticated", func(t *testing.T) {
		t.Parallel()
		accounts := mocks.NewMockAccountRepository(t)
		garages := mocks.NewMockGarageMembershipReader(t)
		tokens := mocks.NewMockRefreshTokenStore(t)
		srv := newTestHandler(t, accounts, garages, tokens)
		tokens.On("GetAccountID", mock.Anything, "bad-token").Return("", errors.New("not found")).Once()

		_, err := srv.RefreshToken(t.Context(), &identityv1.RefreshTokenRequest{RefreshToken: "bad-token"})
		require.Error(t, err)
		assert.Equal(t, codes.Unauthenticated, status.Code(err))
	})
	t.Run("facebook inactive account is echoed in the session", func(t *testing.T) {
		t.Parallel()
		accounts := mocks.NewMockAccountRepository(t)
		garages := mocks.NewMockGarageMembershipReader(t)
		tokens := mocks.NewMockRefreshTokenStore(t)
		srv := newTestHandler(t, accounts, garages, tokens)
		uid := "fb-uid"
		accounts.On("FindByID", mock.Anything, "acc-fb").Return(&domain.Account{ID: "acc-fb", AuthProvider: domain.AuthProviderFacebook, IdentityPlatformUID: &uid, Status: domain.AccountStatusInactive}, nil).Once()
		tokens.On("GetAccountID", mock.Anything, "session-1").Return("acc-fb", nil).Once()
		tokens.On("Revoke", mock.Anything, "session-1").Return(nil).Once()
		garages.On("ListGarageRolesForAccount", mock.Anything, "acc-fb").Return(nil, nil).Once()
		tokens.On("Store", mock.Anything, mock.Anything, "acc-fb", 86400).Return(nil).Once()

		resp, err := srv.RefreshToken(t.Context(), &identityv1.RefreshTokenRequest{RefreshToken: "session-1"})
		require.NoError(t, err)
		assert.Equal(t, identityv1.AuthProvider_AUTH_PROVIDER_FACEBOOK, resp.GetAuth().GetAccount().GetAuthProvider())
		assert.Equal(t, identityv1.AccountStatus_ACCOUNT_STATUS_INACTIVE, resp.GetAuth().GetAccount().GetStatus())
	})
}
