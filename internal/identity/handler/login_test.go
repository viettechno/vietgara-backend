package handler_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/viettechno/vietgara-backend/internal/identity/domain"
	"github.com/viettechno/vietgara-backend/internal/identity/usecase"
	"github.com/viettechno/vietgara-backend/internal/mocks"
	identityv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/identity/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestServer_Login(t *testing.T) {
	t.Parallel()
	email := "login@example.com"
	t.Run("successful email/password login returns the session", func(t *testing.T) {
		t.Parallel()
		accounts := mocks.NewMockAccountRepository(t)
		garages := mocks.NewMockGarageMembershipReader(t)
		tokens := mocks.NewMockRefreshTokenStore(t)
		srv := newTestHandler(t, accounts, garages, tokens)
		accounts.On("FindByEmail", mock.Anything, email).Return(&domain.Account{ID: "acc-1", Email: &email, AuthProvider: domain.AuthProviderEmail, FullName: "Login User", PasswordHash: passwordHash(t, "secret123"), Status: domain.AccountStatusActive}, nil).Once()
		garages.On("ListGarageRolesForAccount", mock.Anything, "acc-1").Return([]usecase.GarageRole{{GarageID: "g-1", Role: "owner"}}, nil).Once()
		tokens.On("Store", mock.Anything, mock.Anything, "acc-1", 86400).Return(nil).Once()
		resp, err := srv.Login(t.Context(), &identityv1.LoginRequest{Method: &identityv1.LoginRequest_EmailPassword{EmailPassword: &identityv1.EmailPasswordCredential{Email: email, Password: "secret123"}}})
		require.NoError(t, err)
		require.NotNil(t, resp.GetAuth())
		require.Len(t, resp.GetAuth().GetGarages(), 1)
		assert.Equal(t, "g-1", resp.GetAuth().GetGarages()[0].GetGarageId())
		assert.Equal(t, "ROLE_OWNER", resp.GetAuth().GetGarages()[0].GetRole().String())
	})
	t.Run("wrong password maps to Unauthenticated", func(t *testing.T) {
		t.Parallel()
		accounts := mocks.NewMockAccountRepository(t)
		garages := mocks.NewMockGarageMembershipReader(t)
		tokens := mocks.NewMockRefreshTokenStore(t)
		srv := newTestHandler(t, accounts, garages, tokens)
		accounts.On("FindByEmail", mock.Anything, email).Return(&domain.Account{ID: "acc-1", Email: &email, AuthProvider: domain.AuthProviderEmail, PasswordHash: passwordHash(t, "secret123"), Status: domain.AccountStatusActive}, nil).Once()
		_, err := srv.Login(t.Context(), &identityv1.LoginRequest{Method: &identityv1.LoginRequest_EmailPassword{EmailPassword: &identityv1.EmailPasswordCredential{Email: email, Password: "wrong"}}})
		require.Error(t, err)
		assert.Equal(t, codes.Unauthenticated, status.Code(err))
	})
	t.Run("suspended account maps to FailedPrecondition", func(t *testing.T) {
		t.Parallel()
		accounts := mocks.NewMockAccountRepository(t)
		garages := mocks.NewMockGarageMembershipReader(t)
		tokens := mocks.NewMockRefreshTokenStore(t)
		srv := newTestHandler(t, accounts, garages, tokens)
		accounts.On("FindByEmail", mock.Anything, email).Return(&domain.Account{ID: "acc-1", Email: &email, AuthProvider: domain.AuthProviderEmail, PasswordHash: passwordHash(t, "secret123"), Status: domain.AccountStatusSuspended}, nil).Once()
		_, err := srv.Login(t.Context(), &identityv1.LoginRequest{Method: &identityv1.LoginRequest_EmailPassword{EmailPassword: &identityv1.EmailPasswordCredential{Email: email, Password: "secret123"}}})
		require.Error(t, err)
		assert.Equal(t, codes.FailedPrecondition, status.Code(err))
	})
}

func TestServer_Login_Social(t *testing.T) {
	t.Parallel()
	t.Run("social login maps all garage roles", func(t *testing.T) {
		t.Parallel()
		uid := "stub-social-token"
		accounts := mocks.NewMockAccountRepository(t)
		garages := mocks.NewMockGarageMembershipReader(t)
		tokens := mocks.NewMockRefreshTokenStore(t)
		srv := newTestHandler(t, accounts, garages, tokens)
		accounts.On("FindByIdentityPlatformUID", mock.Anything, uid).Return(&domain.Account{ID: "acc-social", AuthProvider: domain.AuthProviderFacebook, IdentityPlatformUID: &uid, FullName: "Social User", Status: domain.AccountStatusActive}, nil).Once()
		garages.On("ListGarageRolesForAccount", mock.Anything, "acc-social").Return([]usecase.GarageRole{{GarageID: "g-1", Role: "owner"}, {GarageID: "g-2", Role: "accountant"}, {GarageID: "g-3", Role: "technician"}, {GarageID: "g-4", Role: "service_advisor"}, {GarageID: "g-5", Role: "inventory_staff"}, {GarageID: "g-6", Role: "unknown_role"}}, nil).Once()
		tokens.On("Store", mock.Anything, mock.Anything, "acc-social", 86400).Return(nil).Once()
		resp, err := srv.Login(t.Context(), &identityv1.LoginRequest{Method: &identityv1.LoginRequest_Social{Social: &identityv1.SocialCredential{Provider: identityv1.AuthProvider_AUTH_PROVIDER_FACEBOOK, IdToken: "social-token"}}})
		require.NoError(t, err)
		require.Len(t, resp.GetAuth().GetGarages(), 6)
		assert.Equal(t, "ROLE_OWNER", resp.GetAuth().GetGarages()[0].GetRole().String())
		assert.Equal(t, "ROLE_ACCOUNTANT", resp.GetAuth().GetGarages()[1].GetRole().String())
		assert.Equal(t, "ROLE_TECHNICIAN", resp.GetAuth().GetGarages()[2].GetRole().String())
		assert.Equal(t, "ROLE_SERVICE_ADVISOR", resp.GetAuth().GetGarages()[3].GetRole().String())
		assert.Equal(t, "ROLE_INVENTORY_STAFF", resp.GetAuth().GetGarages()[4].GetRole().String())
		assert.Equal(t, "ROLE_UNSPECIFIED", resp.GetAuth().GetGarages()[5].GetRole().String())
		assert.Equal(t, identityv1.AuthProvider_AUTH_PROVIDER_FACEBOOK, resp.GetAuth().GetAccount().GetAuthProvider())
	})
	t.Run("social login with invalid id token is internal", func(t *testing.T) {
		t.Parallel()
		accounts := mocks.NewMockAccountRepository(t)
		garages := mocks.NewMockGarageMembershipReader(t)
		tokens := mocks.NewMockRefreshTokenStore(t)
		srv := newTestHandler(t, accounts, garages, tokens)
		_, err := srv.Login(t.Context(), &identityv1.LoginRequest{Method: &identityv1.LoginRequest_Social{Social: &identityv1.SocialCredential{Provider: identityv1.AuthProvider_AUTH_PROVIDER_GOOGLE, IdToken: ""}}})
		require.Error(t, err)
		assert.Equal(t, codes.Internal, status.Code(err))
	})
	t.Run("phone/otp login works end to end", func(t *testing.T) {
		t.Parallel()
		phone := "+84901234567"
		accounts := mocks.NewMockAccountRepository(t)
		garages := mocks.NewMockGarageMembershipReader(t)
		tokens := mocks.NewMockRefreshTokenStore(t)
		srv := newTestHandler(t, accounts, garages, tokens)
		accounts.On("FindByPhone", mock.Anything, phone).Return(&domain.Account{ID: "acc-2", Phone: &phone, AuthProvider: domain.AuthProviderEmail, FullName: "Phone User", Status: domain.AccountStatusActive}, nil).Once()
		garages.On("ListGarageRolesForAccount", mock.Anything, "acc-2").Return(nil, nil).Once()
		tokens.On("Store", mock.Anything, mock.Anything, "acc-2", 86400).Return(nil).Once()
		resp, err := srv.Login(t.Context(), &identityv1.LoginRequest{Method: &identityv1.LoginRequest_PhoneOtp{PhoneOtp: &identityv1.PhoneOtpCredential{Phone: phone, OtpCode: testOTPCode}}})
		require.NoError(t, err)
		assert.Equal(t, phone, resp.GetAuth().GetAccount().GetPhone())
	})
}
