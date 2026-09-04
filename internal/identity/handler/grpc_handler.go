// Package handler wires the Identity/Auth usecases to the gRPC service
// generated from vietgara-proto (proto/vietgara/identity/v1).
package handler

import (
	"context"
	"time"

	identityv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/identity/v1"

	"github.com/viettechno/vietgara-backend/internal/identity/domain"
	"github.com/viettechno/vietgara-backend/internal/identity/usecase"
)

// SocialIdentityVerifier verifies a social provider's id_token against GCP
// Identity Platform and returns the verified profile. The implementation
// (admin SDK call) is injected at the composition root -- see
// cmd/server/wiring.go.
type SocialIdentityVerifier interface {
	VerifyIDToken(ctx context.Context, provider domain.AuthProvider, idToken string) (*usecase.VerifiedSocialProfile, error)
}

// Server implements identityv1.AuthServiceServer. ForgotPassword,
// ResetPassword and LinkSocialAccount are not implemented yet: the
// underlying password-reset and account-linking flows are part of a later
// release, so calls fall through to the embedded Unimplemented server and
// return codes.Unimplemented.
type Server struct {
	identityv1.UnimplementedAuthServiceServer

	uc *usecase.IdentityUseCase

	socialVerifier SocialIdentityVerifier

	now func() time.Time
}

// NewServer wires the handler to the identity use case and its
// dependencies.
func NewServer(
	uc *usecase.IdentityUseCase,
	socialVerifier SocialIdentityVerifier,
) *Server {
	return &Server{
		uc:             uc,
		socialVerifier: socialVerifier,
		now:            time.Now,
	}
}
