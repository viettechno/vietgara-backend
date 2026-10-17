package usecase

import (
	"context"
	"errors"
	"time"

	"github.com/viettechno/vietgara-backend/internal/identity/domain"
	"github.com/viettechno/vietgara-backend/internal/platform/audit"
	"github.com/viettechno/vietgara-backend/internal/platform/auth"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// Audit actions of the second factor (Security Architecture Section 7).
const (
	auditMFAEnabled             = "mfa.enabled"
	auditMFADisabled            = "mfa.disabled"
	auditMFARecoveryRegenerated = "mfa.recovery_codes_regenerated"
)

// MFAEnrollment is what the user adds to an authenticator app.
type MFAEnrollment struct {
	Secret     string
	OTPAuthURI string
}

// MFAStatus describes a user's second factor.
type MFAStatus struct {
	Enabled                bool
	EnabledAt              *time.Time
	RecoveryCodesRemaining int
}

// startMFAChallenge answers a correct password of a user with MFA on: the
// sign-in is parked behind a short-lived single-use token.
func (uc *IdentityUseCase) startMFAChallenge(ctx context.Context, user *domain.User) (*Session, error) {
	if user.Status == domain.UserStatusSuspended {
		return nil, apperror.Wrap(apperror.CodePermissionDenied, domain.ErrUserSuspended.Error(), domain.ErrUserSuspended).
			WithReason(domain.ReasonUserSuspended)
	}

	token := auth.NewOpaqueToken()
	if err := uc.sessions.SaveMFAChallenge(ctx, token, user.ID, domain.MFAChallengeTTL); err != nil {
		return nil, apperror.Internal("store mfa challenge", err)
	}

	return &Session{
		User: user, MFARequired: true, ChallengeToken: token, ChallengeExpiresAt: uc.now().UTC().Add(domain.MFAChallengeTTL),
	}, nil
}

// VerifyMFAChallenge completes a sign-in that answered MFARequired with a
// TOTP code or an unused recovery code (FR-IAM-09). Five wrong codes lock
// the user's second-factor checks for 15 minutes.
func (uc *IdentityUseCase) VerifyMFAChallenge(ctx context.Context, challengeToken, code string) (*Session, error) {
	userID, err := uc.sessions.MFAChallengeUser(ctx, challengeToken)
	if err != nil {
		return nil, mfaError(err)
	}

	user, err := uc.findUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	if err := uc.verifySecondFactor(ctx, user.ID, code, true); err != nil {
		return nil, err
	}

	if err := uc.sessions.DeleteMFAChallenge(ctx, challengeToken); err != nil {
		return nil, apperror.Internal("consume mfa challenge", err)
	}

	return uc.issueSession(ctx, user)
}

// GetMFAStatus reports whether the user's second factor is on.
func (uc *IdentityUseCase) GetMFAStatus(ctx context.Context, userID string) (MFAStatus, error) {
	if uc.mfa == nil {
		return MFAStatus{}, nil
	}

	m, err := uc.mfa.Find(ctx, userID)
	if errors.Is(err, domain.ErrMFANotFound) || (err == nil && !m.Enabled()) {
		return MFAStatus{}, nil
	}

	if err != nil {
		return MFAStatus{}, apperror.Internal("find mfa", err)
	}

	remaining, err := uc.mfa.RecoveryCodesRemaining(ctx, userID)
	if err != nil {
		return MFAStatus{}, apperror.Internal("count recovery codes", err)
	}

	return MFAStatus{Enabled: true, EnabledAt: m.EnabledAt, RecoveryCodesRemaining: remaining}, nil
}

// EnrollMFA starts an enrolment: a new secret that counts only once
// ConfirmMFAEnrollment proves the authenticator app holds it. It replaces an
// earlier unconfirmed secret.
func (uc *IdentityUseCase) EnrollMFA(ctx context.Context, userID string) (MFAEnrollment, error) {
	if uc.mfa == nil {
		return MFAEnrollment{}, mfaError(domain.ErrMFANotConfigured)
	}

	user, err := uc.findUser(ctx, userID)
	if err != nil {
		return MFAEnrollment{}, err
	}

	// The second factor guards the password sign-in only.
	if user.PasswordHash == nil {
		return MFAEnrollment{}, mfaError(domain.ErrMFANotAvailable)
	}

	if user.MFAEnabled {
		return MFAEnrollment{}, mfaError(domain.ErrMFAAlreadyEnabled)
	}

	secret := domain.NewTOTPSecret()

	sealed, err := uc.box.Seal(secret)
	if err != nil {
		return MFAEnrollment{}, apperror.Internal("seal totp secret", err)
	}

	saved, err := uc.mfa.SavePending(ctx, userID, sealed)
	if err != nil {
		return MFAEnrollment{}, apperror.Internal("save pending mfa", err)
	}

	if !saved {
		return MFAEnrollment{}, mfaError(domain.ErrMFAAlreadyEnabled)
	}

	return MFAEnrollment{Secret: domain.EncodeTOTPSecret(secret), OTPAuthURI: domain.OTPAuthURI(secret, user.Email)}, nil
}

// ConfirmMFAEnrollment turns the second factor on when code is valid and
// returns the recovery codes, which are never shown again.
func (uc *IdentityUseCase) ConfirmMFAEnrollment(ctx context.Context, userID, code string) ([]string, error) {
	if uc.mfa == nil {
		return nil, mfaError(domain.ErrMFANotConfigured)
	}

	m, err := uc.mfa.Find(ctx, userID)

	switch {
	case errors.Is(err, domain.ErrMFANotFound):
		return nil, mfaError(domain.ErrMFAEnrollmentNotStarted)
	case err != nil:
		return nil, apperror.Internal("find mfa", err)
	case m.Enabled():
		return nil, mfaError(domain.ErrMFAAlreadyEnabled)
	}

	if err := uc.ensureNotLocked(ctx, userID); err != nil {
		return nil, err
	}

	secret, err := uc.box.Open(m.SecretEncrypted)
	if err != nil {
		return nil, apperror.Internal("open totp secret", err)
	}

	step, ok := domain.MatchTOTP(secret, code, uc.now())
	if !ok {
		return nil, uc.wrongCode(ctx, userID)
	}

	codes := domain.NewRecoveryCodes()

	err = uc.tx.WithTransaction(ctx, platformdb.UserScope(userID), func(ctx context.Context) error {
		if err := uc.mfa.Enable(ctx, userID, uc.now().UTC(), step, hashRecoveryCodes(codes)); err != nil {
			return err
		}

		return uc.record(ctx, userID, auditMFAEnabled, nil, map[string]any{"enabled": true})
	})
	if errors.Is(err, domain.ErrMFAEnrollmentNotStarted) {
		return nil, mfaError(err)
	}

	if err != nil {
		return nil, apperror.Internal("enable mfa", err)
	}

	_ = uc.sessions.ClearMFAFailures(ctx, userID)

	return codes, nil
}

// RegenerateRecoveryCodes replaces the recovery codes; it needs a current
// TOTP code, not a recovery code.
func (uc *IdentityUseCase) RegenerateRecoveryCodes(ctx context.Context, userID, code string) ([]string, error) {
	if err := uc.requireEnabled(ctx, userID); err != nil {
		return nil, err
	}

	if err := uc.verifySecondFactor(ctx, userID, code, false); err != nil {
		return nil, err
	}

	codes := domain.NewRecoveryCodes()

	err := uc.tx.WithTransaction(ctx, platformdb.UserScope(userID), func(ctx context.Context) error {
		if err := uc.mfa.ReplaceRecoveryCodes(ctx, userID, hashRecoveryCodes(codes)); err != nil {
			return err
		}

		return uc.record(ctx, userID, auditMFARecoveryRegenerated, nil, nil)
	})
	if err != nil {
		return nil, apperror.Internal("replace recovery codes", err)
	}

	return codes, nil
}

// DisableMFA turns the second factor off; it needs a TOTP code or a
// recovery code. Resetting a password never does this.
func (uc *IdentityUseCase) DisableMFA(ctx context.Context, userID, code string) error {
	if err := uc.requireEnabled(ctx, userID); err != nil {
		return err
	}

	if err := uc.verifySecondFactor(ctx, userID, code, true); err != nil {
		return err
	}

	err := uc.tx.WithTransaction(ctx, platformdb.UserScope(userID), func(ctx context.Context) error {
		if err := uc.mfa.Delete(ctx, userID); err != nil {
			return err
		}

		return uc.record(ctx, userID, auditMFADisabled, map[string]any{"enabled": true}, map[string]any{"enabled": false})
	})
	if err != nil {
		return apperror.Internal("disable mfa", err)
	}

	return nil
}

func (uc *IdentityUseCase) requireEnabled(ctx context.Context, userID string) error {
	if uc.mfa == nil {
		return mfaError(domain.ErrMFANotConfigured)
	}

	m, err := uc.mfa.Find(ctx, userID)

	switch {
	case errors.Is(err, domain.ErrMFANotFound):
		return mfaError(domain.ErrMFANotEnabled)
	case err != nil:
		return apperror.Internal("find mfa", err)
	case !m.Enabled():
		return mfaError(domain.ErrMFANotEnabled)
	}

	return nil
}

// verifySecondFactor checks code against the user's enabled factor: a TOTP
// code that was not used before, or (when allowRecovery) an unspent
// recovery code, which it spends. Wrong codes count towards the lockout.
func (uc *IdentityUseCase) verifySecondFactor(ctx context.Context, userID, code string, allowRecovery bool) error {
	if uc.mfa == nil {
		return mfaError(domain.ErrMFANotConfigured)
	}

	if err := uc.ensureNotLocked(ctx, userID); err != nil {
		return err
	}

	m, err := uc.mfa.Find(ctx, userID)

	switch {
	case errors.Is(err, domain.ErrMFANotFound):
		return mfaError(domain.ErrMFANotEnabled)
	case err != nil:
		return apperror.Internal("find mfa", err)
	case !m.Enabled():
		return mfaError(domain.ErrMFANotEnabled)
	}

	ok, err := uc.codeMatches(ctx, m, code, allowRecovery)
	if err != nil {
		return err
	}

	if !ok {
		return uc.wrongCode(ctx, userID)
	}

	_ = uc.sessions.ClearMFAFailures(ctx, userID)

	return nil
}

func (uc *IdentityUseCase) codeMatches(ctx context.Context, m *domain.MFA, code string, allowRecovery bool) (bool, error) {
	if recovery, isRecovery := domain.NormalizeRecoveryCode(code); isRecovery {
		if !allowRecovery {
			return false, nil
		}

		used, err := uc.mfa.UseRecoveryCode(ctx, m.UserID, domain.HashRecoveryCode(recovery))
		if err != nil {
			return false, apperror.Internal("use recovery code", err)
		}

		return used, nil
	}

	secret, err := uc.box.Open(m.SecretEncrypted)
	if err != nil {
		return false, apperror.Internal("open totp secret", err)
	}

	step, ok := domain.MatchTOTP(secret, code, uc.now())
	if !ok {
		return false, nil
	}

	// A step is accepted once, so an observed code cannot be replayed.
	fresh, err := uc.mfa.AdvanceStep(ctx, m.UserID, step)
	if err != nil {
		return false, apperror.Internal("advance totp step", err)
	}

	return fresh, nil
}

func (uc *IdentityUseCase) ensureNotLocked(ctx context.Context, userID string) error {
	failures, err := uc.sessions.MFAFailures(ctx, userID)
	if err != nil {
		return apperror.Internal("read mfa failures", err)
	}

	if failures >= domain.MFAMaxFailures {
		return mfaError(domain.ErrMFALocked)
	}

	return nil
}

// wrongCode counts a wrong code and returns the error to answer with: the
// fifth one locks the checks.
func (uc *IdentityUseCase) wrongCode(ctx context.Context, userID string) error {
	failures, err := uc.sessions.RecordMFAFailure(ctx, userID, domain.MFALockout)
	if err != nil {
		return apperror.Internal("count mfa failure", err)
	}

	if failures >= domain.MFAMaxFailures {
		return mfaError(domain.ErrMFALocked)
	}

	return mfaError(domain.ErrMFACodeInvalid)
}

func (uc *IdentityUseCase) record(ctx context.Context, userID, action string, before, after any) error {
	if uc.audit == nil {
		return nil
	}

	return uc.audit.Record(ctx, audit.Entry{
		Actor: userID, Action: action, EntityType: "user_mfa", EntityID: userID, Before: before, After: after,
	})
}

func hashRecoveryCodes(codes []string) []string {
	hashes := make([]string, len(codes))

	for i, code := range codes {
		normalized, _ := domain.NormalizeRecoveryCode(code)
		hashes[i] = domain.HashRecoveryCode(normalized)
	}

	return hashes
}

func mfaError(err error) error {
	switch {
	case errors.Is(err, domain.ErrMFAChallengeInvalid):
		return apperror.Wrap(apperror.CodeUnauthenticated, err.Error(), err).WithReason(domain.ReasonMFAChallengeInvalid)
	case errors.Is(err, domain.ErrMFALocked):
		return apperror.Wrap(apperror.CodeResourceExhausted, err.Error(), err).WithReason(domain.ReasonMFALocked)
	case errors.Is(err, domain.ErrMFAAlreadyEnabled):
		return apperror.Wrap(apperror.CodeFailedPrecondition, err.Error(), err).WithReason(domain.ReasonMFAAlreadyEnabled)
	case errors.Is(err, domain.ErrMFANotEnabled):
		return apperror.Wrap(apperror.CodeFailedPrecondition, err.Error(), err).WithReason(domain.ReasonMFANotEnabled)
	case errors.Is(err, domain.ErrMFAEnrollmentNotStarted):
		return apperror.Wrap(apperror.CodeFailedPrecondition, err.Error(), err).WithReason(domain.ReasonMFAEnrollmentNotStarted)
	case errors.Is(err, domain.ErrMFANotAvailable):
		return apperror.Wrap(apperror.CodeFailedPrecondition, err.Error(), err).WithReason(domain.ReasonMFANotAvailable)
	case errors.Is(err, domain.ErrMFANotConfigured):
		return apperror.Wrap(apperror.CodeFailedPrecondition, err.Error(), err).WithReason(domain.ReasonMFANotConfigured)
	case errors.Is(err, domain.ErrMFACodeInvalid):
		return apperror.Wrap(apperror.CodeInvalidArgument, err.Error(), err).WithReason(domain.ReasonMFACodeInvalid).WithField("code", apperror.IssueInvalid)
	default:
		return apperror.Internal("mfa challenge", err)
	}
}
