package middleware

import "context"

// LicenseMode is what a garage's subscription currently allows.
type LicenseMode int

// LicenseMode values.
const (
	// LicenseFull allows everything (trial, active, or no subscription yet).
	LicenseFull LicenseMode = iota
	// LicenseReadOnly blocks writes (past due, in the grace period).
	LicenseReadOnly
	// LicenseLocked blocks every request (suspended or cancelled).
	LicenseLocked
)

// Messages returned when the license blocks a request.
const (
	msgLicenseLocked   = "subscription suspended: renew your plan in Garage Settings"
	msgLicenseReadOnly = "read-only mode: subscription past due"
)

// LicenseChecker reports a garage's LicenseMode from its owner's
// subscription, reading the entitlement cache (LLD Section 3.3);
// implemented by the License module.
type LicenseChecker interface {
	LicenseMode(ctx context.Context, garageID string) (LicenseMode, error)
}
