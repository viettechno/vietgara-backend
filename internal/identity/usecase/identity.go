// Package usecase contains the application use cases of the Identity
// module: they validate input, enforce business rules, and orchestrate the
// domain and repository layers.
// See: Coding_Standards.docx Section 2.3, LLD Section 6.
package usecase

import (
	"time"

	"github.com/viettechno/vietgara-backend/internal/platform/auth"
)

// IdentityUseCase implements every authentication action of the Identity
// module -- register, login (email+password / phone+OTP / social),
// refresh, and logout -- sharing the same account repository, refresh
// token store, garage membership reader, and token issuer, so they can be
// injected as a single dependency.
type IdentityUseCase struct {
	accountRepo  AccountRepository
	garageReader GarageMembershipReader
	tokenStore   RefreshTokenStore
	otpVerifier  OTPVerifier
	issuer       *auth.TokenIssuer
	refreshTTL   time.Duration
}

// NewIdentityUseCase wires the identity use case to its dependencies.
func NewIdentityUseCase(
	accountRepo AccountRepository,
	garageReader GarageMembershipReader,
	tokenStore RefreshTokenStore,
	otpVerifier OTPVerifier,
	issuer *auth.TokenIssuer,
	refreshTTL time.Duration,
) *IdentityUseCase {
	return &IdentityUseCase{
		accountRepo:  accountRepo,
		garageReader: garageReader,
		tokenStore:   tokenStore,
		otpVerifier:  otpVerifier,
		issuer:       issuer,
		refreshTTL:   refreshTTL,
	}
}
