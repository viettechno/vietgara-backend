package main

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"github.com/viettechno/vietgara-backend/internal/identity/domain"
	identityUsecase "github.com/viettechno/vietgara-backend/internal/identity/usecase"
	tenantRepo "github.com/viettechno/vietgara-backend/internal/tenant/repository"
)

// tenantToIdentityGarageReaderAdapter bridges the Tenant module's
// GarageStaffRepository to the narrower GarageMembershipReader interface
// the Identity module depends on -- avoids a circular import between the
// two modules while both still live in the same Modular Monolith binary
// (ADR-001). Converts tenant repository.RoleAssignment into
// identity usecase.GarageRole.
type tenantToIdentityGarageReaderAdapter struct {
	staffRepo *tenantRepo.PostgresGarageStaffRepository
}

func (a *tenantToIdentityGarageReaderAdapter) ListGarageRolesForAccount(ctx context.Context, accountID string) ([]identityUsecase.GarageRole, error) {
	assignments, err := a.staffRepo.ListRolesForAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}

	result := make([]identityUsecase.GarageRole, 0, len(assignments))
	for _, assignment := range assignments {
		result = append(result, identityUsecase.GarageRole{GarageID: assignment.GarageID, Role: assignment.Role})
	}

	return result, nil
}

// inMemoryRefreshTokenStore is an ephemeral RefreshTokenStore.
// TODO(Phase 1 hardening, before staging): replace with a Redis
// (Memorystore) implementation -- see Security Architecture Section 2.2.
// An in-memory store does not survive a process restart and does not work
// across multiple replicas, which is only acceptable for local
// development.
type inMemoryRefreshTokenStore struct {
	mu     sync.Mutex
	tokens map[string]string // refreshToken -> accountID
}

func newInMemoryRefreshTokenStore() *inMemoryRefreshTokenStore {
	return &inMemoryRefreshTokenStore{tokens: map[string]string{}}
}

func (s *inMemoryRefreshTokenStore) Store(_ context.Context, refreshToken, accountID string, _ int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.tokens[refreshToken] = accountID

	return nil
}

func (s *inMemoryRefreshTokenStore) GetAccountID(_ context.Context, refreshToken string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	accountID, ok := s.tokens[refreshToken]
	if !ok {
		return "", errors.New("refresh token not found or revoked")
	}

	return accountID, nil
}

func (s *inMemoryRefreshTokenStore) Revoke(_ context.Context, refreshToken string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.tokens, refreshToken)

	return nil
}

// stubOTPVerifier always accepts the dev code "000000" -- local development
// only.
// TODO(Phase 1 hardening, before staging): replace with a real SMS OTP
// provider integration.
type stubOTPVerifier struct{}

func (s *stubOTPVerifier) Verify(_ context.Context, _, code string) (bool, error) {
	return code == "000000", nil
}

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
