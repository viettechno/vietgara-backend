package main

import "context"

// stubOTPVerifier always accepts the dev code "000000" -- local development
// only.
// TODO(Phase 1 hardening, before staging): replace with a real SMS OTP
// provider integration.
type stubOTPVerifier struct{}

func (s *stubOTPVerifier) Verify(_ context.Context, _, code string) (bool, error) {
	return code == "000000", nil
}
