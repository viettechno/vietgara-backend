package handler

import (
	identityv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/identity/v1"
	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/viettechno/vietgara-backend/internal/identity/domain"
	"github.com/viettechno/vietgara-backend/internal/identity/usecase"
)

func (s *Server) authToProto(result *usecase.AuthResult) *identityv1.AuthResponse {
	expiresIn := int64(0)
	if t := result.ExpiresAt; !t.IsZero() {
		expiresIn = int64(t.Sub(s.now()).Seconds())
	}

	garages := make([]*identityv1.GaragePermission, 0, len(result.Garages))
	for _, g := range result.Garages {
		garages = append(garages, &identityv1.GaragePermission{GarageId: g.GarageID, Role: roleStringToProto(g.Role)})
	}

	return &identityv1.AuthResponse{AccessToken: result.AccessToken, RefreshToken: result.RefreshToken, TokenType: "Bearer", ExpiresIn: expiresIn, Account: accountToProto(result.Account), Garages: garages}
}

func roleStringToProto(role string) tenantv1.Role {
	switch role {
	case "owner":
		return tenantv1.Role_ROLE_OWNER
	case "accountant":
		return tenantv1.Role_ROLE_ACCOUNTANT
	case "technician":
		return tenantv1.Role_ROLE_TECHNICIAN
	case "service_advisor":
		return tenantv1.Role_ROLE_SERVICE_ADVISOR
	case "inventory_staff":
		return tenantv1.Role_ROLE_INVENTORY_STAFF
	default:
		return tenantv1.Role_ROLE_UNSPECIFIED
	}
}

func accountToProto(acc *domain.Account) *identityv1.Account {
	provider := identityv1.AuthProvider_AUTH_PROVIDER_UNSPECIFIED

	switch acc.AuthProvider {
	case domain.AuthProviderEmail:
		provider = identityv1.AuthProvider_AUTH_PROVIDER_EMAIL
	case domain.AuthProviderPhone:
		provider = identityv1.AuthProvider_AUTH_PROVIDER_PHONE
	case domain.AuthProviderGoogle:
		provider = identityv1.AuthProvider_AUTH_PROVIDER_GOOGLE
	case domain.AuthProviderFacebook:
		provider = identityv1.AuthProvider_AUTH_PROVIDER_FACEBOOK
	}

	accStatus := identityv1.AccountStatus_ACCOUNT_STATUS_ACTIVE

	switch acc.Status {
	case domain.AccountStatusActive:
		accStatus = identityv1.AccountStatus_ACCOUNT_STATUS_ACTIVE
	case domain.AccountStatusInactive:
		accStatus = identityv1.AccountStatus_ACCOUNT_STATUS_INACTIVE
	case domain.AccountStatusSuspended:
		accStatus = identityv1.AccountStatus_ACCOUNT_STATUS_SUSPENDED
	}

	return &identityv1.Account{Id: acc.ID, Email: acc.Email, Phone: acc.Phone, AuthProvider: provider, FullName: acc.FullName, Status: accStatus}
}

// authProviderFromProto maps the proto AuthProvider enum to the domain
// value, rejecting unknown values so an unauthenticated user cannot force
// an arbitrary provider through.
func authProviderFromProto(p identityv1.AuthProvider) (domain.AuthProvider, error) {
	switch p {
	case identityv1.AuthProvider_AUTH_PROVIDER_EMAIL:
		return domain.AuthProviderEmail, nil
	case identityv1.AuthProvider_AUTH_PROVIDER_PHONE:
		return domain.AuthProviderPhone, nil
	case identityv1.AuthProvider_AUTH_PROVIDER_GOOGLE:
		return domain.AuthProviderGoogle, nil
	case identityv1.AuthProvider_AUTH_PROVIDER_FACEBOOK:
		return domain.AuthProviderFacebook, nil
	case identityv1.AuthProvider_AUTH_PROVIDER_UNSPECIFIED:
		return "", status.Error(codes.InvalidArgument, "auth provider is required")
	default:
		return "", status.Error(codes.InvalidArgument, "unsupported auth provider")
	}
}
