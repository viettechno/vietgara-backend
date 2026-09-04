package handler_test

import (
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

func TestServer_Register(t *testing.T) {
	t.Parallel()
	email := "register@example.com"
	t.Run("email/password register returns a session", func(t *testing.T) {
		t.Parallel()
		accounts := mocks.NewMockAccountRepository(t)
		garages := mocks.NewMockGarageMembershipReader(t)
		tokens := mocks.NewMockRefreshTokenStore(t)
		srv := newTestHandler(t, accounts, garages, tokens)
		accounts.On("FindByEmail", mock.Anything, email).Return(nil, domain.ErrAccountNotFound).Once()
		accounts.On("Create", mock.Anything, mock.Anything).Return(nil).Once()
		garages.On("ListGarageRolesForAccount", mock.Anything, mock.Anything).Return(nil, nil).Once()
		tokens.On("Store", mock.Anything, mock.Anything, mock.Anything, 86400).Return(nil).Once()
		resp, err := srv.Register(t.Context(), &identityv1.RegisterRequest{FullName: "Register User", Method: &identityv1.RegisterRequest_EmailPassword{EmailPassword: &identityv1.EmailPasswordCredential{Email: email, Password: "secret123"}}})
		require.NoError(t, err)
		require.NotNil(t, resp.GetAuth())
		assert.NotEmpty(t, resp.GetAuth().GetAccessToken())
		assert.NotEmpty(t, resp.GetAuth().GetRefreshToken())
		assert.Equal(t, "Bearer", resp.GetAuth().GetTokenType())
		assert.Positive(t, resp.GetAuth().GetExpiresIn())
		require.NotNil(t, resp.GetAuth().GetAccount())
		assert.Equal(t, email, resp.GetAuth().GetAccount().GetEmail())
		assert.Equal(t, "Register User", resp.GetAuth().GetAccount().GetFullName())
		assert.Equal(t, identityv1.AuthProvider_AUTH_PROVIDER_EMAIL, resp.GetAuth().GetAccount().GetAuthProvider())
		assert.Equal(t, identityv1.AccountStatus_ACCOUNT_STATUS_ACTIVE, resp.GetAuth().GetAccount().GetStatus())
	})
	t.Run("duplicate email maps to AlreadyExists", func(t *testing.T) {
		t.Parallel()
		accounts := mocks.NewMockAccountRepository(t)
		garages := mocks.NewMockGarageMembershipReader(t)
		tokens := mocks.NewMockRefreshTokenStore(t)
		srv := newTestHandler(t, accounts, garages, tokens)
		accounts.On("FindByEmail", mock.Anything, email).Return(&domain.Account{ID: "acc-1"}, nil).Once()
		_, err := srv.Register(t.Context(), &identityv1.RegisterRequest{FullName: "Register User", Method: &identityv1.RegisterRequest_EmailPassword{EmailPassword: &identityv1.EmailPasswordCredential{Email: email, Password: "secret123"}}})
		require.Error(t, err)
		assert.Equal(t, codes.AlreadyExists, status.Code(err))
	})
	t.Run("missing method is invalid", func(t *testing.T) {
		t.Parallel()
		accounts := mocks.NewMockAccountRepository(t)
		garages := mocks.NewMockGarageMembershipReader(t)
		tokens := mocks.NewMockRefreshTokenStore(t)
		srv := newTestHandler(t, accounts, garages, tokens)
		_, err := srv.Register(t.Context(), &identityv1.RegisterRequest{FullName: "X"})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})
	t.Run("social register works through the verifier", func(t *testing.T) {
		t.Parallel()
		accounts := mocks.NewMockAccountRepository(t)
		garages := mocks.NewMockGarageMembershipReader(t)
		tokens := mocks.NewMockRefreshTokenStore(t)
		srv := newTestHandler(t, accounts, garages, tokens)
		accounts.On("FindByIdentityPlatformUID", mock.Anything, "stub-tok-1").Return(nil, domain.ErrAccountNotFound).Once()
		accounts.On("Create", mock.Anything, mock.Anything).Return(nil).Once()
		garages.On("ListGarageRolesForAccount", mock.Anything, mock.Anything).Return(nil, nil).Once()
		tokens.On("Store", mock.Anything, mock.Anything, mock.Anything, 86400).Return(nil).Once()
		resp, err := srv.Register(t.Context(), &identityv1.RegisterRequest{FullName: "Social User", Method: &identityv1.RegisterRequest_Social{Social: &identityv1.SocialCredential{Provider: identityv1.AuthProvider_AUTH_PROVIDER_GOOGLE, IdToken: "tok-1"}}})
		require.NoError(t, err)
		require.NotNil(t, resp.GetAuth())
	})
	t.Run("phone provider register succeeds", func(t *testing.T) {
		t.Parallel()
		phone := "+84901234567"
		accounts := mocks.NewMockAccountRepository(t)
		garages := mocks.NewMockGarageMembershipReader(t)
		tokens := mocks.NewMockRefreshTokenStore(t)
		srv := newTestHandler(t, accounts, garages, tokens)
		accounts.On("FindByIdentityPlatformUID", mock.Anything, "stub-"+phone).Return(nil, domain.ErrAccountNotFound).Once()
		accounts.On("Create", mock.Anything, mock.Anything).Return(nil).Once()
		garages.On("ListGarageRolesForAccount", mock.Anything, mock.Anything).Return(nil, nil).Once()
		tokens.On("Store", mock.Anything, mock.Anything, mock.Anything, 86400).Return(nil).Once()

		resp, err := srv.Register(t.Context(), &identityv1.RegisterRequest{
			FullName: "Phone User",
			Method: &identityv1.RegisterRequest_Social{Social: &identityv1.SocialCredential{
				Provider: identityv1.AuthProvider_AUTH_PROVIDER_PHONE,
				IdToken:  phone,
			}},
		})
		require.NoError(t, err)
		require.NotNil(t, resp.GetAuth())
	})
}

func TestServer_Register_AlternateMethods(t *testing.T) {
	t.Parallel()
	t.Run("phone otp register succeeds", func(t *testing.T) {
		t.Parallel()
		phone := "+84901234567"
		accounts := mocks.NewMockAccountRepository(t)
		garages := mocks.NewMockGarageMembershipReader(t)
		tokens := mocks.NewMockRefreshTokenStore(t)
		srv := newTestHandler(t, accounts, garages, tokens)
		accounts.On("FindByPhone", mock.Anything, phone).Return(nil, domain.ErrAccountNotFound).Once()
		accounts.On("Create", mock.Anything, mock.Anything).Return(nil).Once()
		garages.On("ListGarageRolesForAccount", mock.Anything, mock.Anything).Return(nil, nil).Once()
		tokens.On("Store", mock.Anything, mock.Anything, mock.Anything, 86400).Return(nil).Once()
		resp, err := srv.Register(t.Context(), &identityv1.RegisterRequest{FullName: "Phone User", Method: &identityv1.RegisterRequest_PhoneOtp{PhoneOtp: &identityv1.PhoneOtpCredential{Phone: phone}}})
		require.NoError(t, err)
		assert.Equal(t, phone, resp.GetAuth().GetAccount().GetPhone())
	})
	for _, tt := range []struct {
		name     string
		provider identityv1.AuthProvider
		token    string
		want     codes.Code
	}{
		{name: "missing provider", provider: identityv1.AuthProvider_AUTH_PROVIDER_UNSPECIFIED, token: "tok-1", want: codes.InvalidArgument},
		{name: "invalid id token", provider: identityv1.AuthProvider_AUTH_PROVIDER_FACEBOOK, token: "", want: codes.Internal},
	} {
		t.Run("social register with "+tt.name+" is invalid", func(t *testing.T) {
			t.Parallel()
			accounts := mocks.NewMockAccountRepository(t)
			garages := mocks.NewMockGarageMembershipReader(t)
			tokens := mocks.NewMockRefreshTokenStore(t)
			srv := newTestHandler(t, accounts, garages, tokens)
			_, err := srv.Register(t.Context(), &identityv1.RegisterRequest{FullName: "X", Method: &identityv1.RegisterRequest_Social{Social: &identityv1.SocialCredential{Provider: tt.provider, IdToken: tt.token}}})
			require.Error(t, err)
			assert.Equal(t, tt.want, status.Code(err))
		})
	}
}
