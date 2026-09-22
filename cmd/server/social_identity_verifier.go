package main

import (
	"context"
	"errors"
	"log/slog"

	"github.com/viettechno/vietgara-backend/internal/identity/domain"
	identityUsecase "github.com/viettechno/vietgara-backend/internal/identity/usecase"
)

// stubSocialIdentityVerifier skips real GCP Identity Platform verification
// (ADR-007) and treats the id_token itself as the verified profile, so the
// flow can be exercised end to end without a real GCP project.
// TODO(Phase 1 hardening, before staging): replace with the Identity
// Platform Admin SDK.
type stubSocialIdentityVerifier struct{}

func (s *stubSocialIdentityVerifier) VerifyIDToken(_ context.Context, provider domain.AuthProvider, idToken string) (*identityUsecase.VerifiedSocialProfile, error) {
	if idToken == "" {
		return nil, errors.New("empty id_token")
	}

	slog.Warn("using stub Identity Platform verifier -- replace before staging", "provider", provider)

	return &identityUsecase.VerifiedSocialProfile{
		IdentityPlatformUID: "stub-" + idToken,
		Provider:            provider,
		FullName:            "Dev User",
	}, nil
}
