package handler

import (
	"context"

	"github.com/viettechno/vietgara-backend/internal/license/usecase"
	"github.com/viettechno/vietgara-backend/internal/middleware"
)

// Enforcement is the middleware.LicenseChecker of the request pipeline:
// garage requests are enforced against their owner's subscription, read
// from the entitlement cache (LLD Section 3.3).
type Enforcement struct {
	entitlements *usecase.EntitlementUseCase
}

// NewEnforcement returns the license checker.
func NewEnforcement(entitlements *usecase.EntitlementUseCase) *Enforcement {
	return &Enforcement{entitlements: entitlements}
}

// LicenseMode implements middleware.LicenseChecker.
func (e *Enforcement) LicenseMode(ctx context.Context, garageID string) (middleware.LicenseMode, error) {
	return e.entitlements.LicenseMode(ctx, garageID)
}
