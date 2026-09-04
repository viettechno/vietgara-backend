package handler_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/viettechno/vietgara-backend/internal/mocks"
	identityv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/identity/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestServer_Logout(t *testing.T) {
	t.Parallel()
	t.Run("revokes the session", func(t *testing.T) {
		t.Parallel()
		accounts := mocks.NewMockAccountRepository(t)
		garages := mocks.NewMockGarageMembershipReader(t)
		tokens := mocks.NewMockRefreshTokenStore(t)
		srv := newTestHandler(t, accounts, garages, tokens)
		tokens.On("Revoke", mock.Anything, "session-1").Return(nil).Once()
		resp, err := srv.Logout(t.Context(), &identityv1.LogoutRequest{RefreshToken: "session-1"})
		require.NoError(t, err)
		assert.True(t, resp.GetRevoked())
	})
	t.Run("missing token is invalid", func(t *testing.T) {
		t.Parallel()
		accounts := mocks.NewMockAccountRepository(t)
		garages := mocks.NewMockGarageMembershipReader(t)
		tokens := mocks.NewMockRefreshTokenStore(t)
		srv := newTestHandler(t, accounts, garages, tokens)
		_, err := srv.Logout(t.Context(), &identityv1.LogoutRequest{})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})
	t.Run("revoke failure maps to Internal", func(t *testing.T) {
		t.Parallel()
		accounts := mocks.NewMockAccountRepository(t)
		garages := mocks.NewMockGarageMembershipReader(t)
		tokens := mocks.NewMockRefreshTokenStore(t)
		srv := newTestHandler(t, accounts, garages, tokens)
		tokens.On("Revoke", mock.Anything, "session-1").Return(errors.New("redis down")).Once()
		_, err := srv.Logout(t.Context(), &identityv1.LogoutRequest{RefreshToken: "session-1"})
		require.Error(t, err)
		assert.Equal(t, codes.Internal, status.Code(err))
	})
}
