package handler

import (
	"context"

	identityv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/identity/v1"

	"github.com/viettechno/vietgara-backend/internal/identity/usecase"
)

// verifySocialProfile verifies the provider id_token and rejects profiles
// whose provider does not map to a known AuthProvider value.
func (s *Server) verifySocialProfile(ctx context.Context, provider identityv1.AuthProvider, idToken string) (*usecase.VerifiedSocialProfile, error) {
	domainProvider, err := authProviderFromProto(provider)
	if err != nil {
		return nil, err
	}

	profile, err := s.socialVerifier.VerifyIDToken(ctx, domainProvider, idToken)
	if err != nil {
		return nil, toGRPCError(err)
	}

	return profile, nil
}
