package client

import (
	"context"
	"net/http"

	identityv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/identity/v1"
)

// VerifyMfaChallenge completes a sign-in that answered mfaRequired.
func (c *Client) VerifyMfaChallenge(ctx context.Context, challengeToken, code string) (*identityv1.Session, error) {
	return call[identityv1.Session](ctx, c, http.MethodPost, "/api/v1/auth/mfa/challenge",
		&identityv1.VerifyMfaChallengeRequest{ChallengeToken: challengeToken, Code: code})
}

// MfaStatus reports whether the signed-in user's second factor is on.
func (c *Client) MfaStatus(ctx context.Context) (*identityv1.GetMfaStatusResponse, error) {
	return call[identityv1.GetMfaStatusResponse](ctx, c, http.MethodGet, "/api/v1/me/mfa", nil)
}

// EnrollMfa starts an enrolment.
func (c *Client) EnrollMfa(ctx context.Context) (*identityv1.EnrollMfaResponse, error) {
	return call[identityv1.EnrollMfaResponse](ctx, c, http.MethodPost, "/api/v1/me/mfa/enroll",
		&identityv1.EnrollMfaRequest{})
}

// ConfirmMfaEnrollment turns MFA on and returns the recovery codes.
func (c *Client) ConfirmMfaEnrollment(ctx context.Context, code string) (*identityv1.ConfirmMfaEnrollmentResponse, error) {
	return call[identityv1.ConfirmMfaEnrollmentResponse](ctx, c, http.MethodPost, "/api/v1/me/mfa/enroll:confirm",
		&identityv1.ConfirmMfaEnrollmentRequest{Code: code})
}

// RegenerateRecoveryCodes replaces the recovery codes.
func (c *Client) RegenerateRecoveryCodes(ctx context.Context, code string) (*identityv1.RegenerateRecoveryCodesResponse, error) {
	return call[identityv1.RegenerateRecoveryCodesResponse](ctx, c, http.MethodPost, "/api/v1/me/mfa/recovery-codes",
		&identityv1.RegenerateRecoveryCodesRequest{Code: code})
}

// DisableMfa turns the second factor off.
func (c *Client) DisableMfa(ctx context.Context, code string) error {
	_, err := call[identityv1.DisableMfaResponse](ctx, c, http.MethodPost, "/api/v1/me/mfa:disable",
		&identityv1.DisableMfaRequest{Code: code})

	return err
}
