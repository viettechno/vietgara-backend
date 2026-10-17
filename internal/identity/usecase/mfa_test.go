package usecase_test

import (
	"context"
	"encoding/base32"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/identity/domain"
	"github.com/viettechno/vietgara-backend/internal/identity/usecase"
	"github.com/viettechno/vietgara-backend/internal/platform/audit"
	"github.com/viettechno/vietgara-backend/internal/platform/secretbox"
)

// fakeMFA is an in-memory MFARepository that keeps the users' MFAEnabled
// flag in step, as the users query does.
type fakeMFA struct {
	mu    sync.Mutex
	users *fakeUsers
	rows  map[string]*domain.MFA
	codes map[string]map[string]bool // user -> hash -> spent
}

func newFakeMFA(users *fakeUsers) *fakeMFA {
	return &fakeMFA{users: users, rows: map[string]*domain.MFA{}, codes: map[string]map[string]bool{}}
}

func (f *fakeMFA) setFlag(userID string, enabled bool) {
	f.users.mu.Lock()
	defer f.users.mu.Unlock()

	if u := f.users.byID[userID]; u != nil {
		u.MFAEnabled = enabled
	}
}

func (f *fakeMFA) Find(_ context.Context, userID string) (*domain.MFA, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	m, ok := f.rows[userID]
	if !ok {
		return nil, domain.ErrMFANotFound
	}

	copied := *m

	return &copied, nil
}

func (f *fakeMFA) SavePending(_ context.Context, userID string, sealed []byte) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if m := f.rows[userID]; m != nil && m.Enabled() {
		return false, nil
	}

	f.rows[userID] = &domain.MFA{UserID: userID, SecretEncrypted: sealed}

	return true, nil
}

func (f *fakeMFA) Enable(_ context.Context, userID string, at time.Time, step int64, hashes []string) error {
	f.mu.Lock()
	m := f.rows[userID]

	if m == nil || m.Enabled() {
		f.mu.Unlock()

		return domain.ErrMFAEnrollmentNotStarted
	}

	m.EnabledAt, m.LastUsedStep = &at, step
	f.codes[userID] = map[string]bool{}

	for _, h := range hashes {
		f.codes[userID][h] = false
	}

	f.mu.Unlock()
	f.setFlag(userID, true)

	return nil
}

func (f *fakeMFA) ReplaceRecoveryCodes(_ context.Context, userID string, hashes []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.codes[userID] = map[string]bool{}

	for _, h := range hashes {
		f.codes[userID][h] = false
	}

	return nil
}

func (f *fakeMFA) AdvanceStep(_ context.Context, userID string, step int64) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	m := f.rows[userID]
	if m == nil || m.LastUsedStep >= step {
		return false, nil
	}

	m.LastUsedStep = step

	return true, nil
}

func (f *fakeMFA) UseRecoveryCode(_ context.Context, userID, hash string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	spent, ok := f.codes[userID][hash]
	if !ok || spent {
		return false, nil
	}

	f.codes[userID][hash] = true

	return true, nil
}

func (f *fakeMFA) RecoveryCodesRemaining(_ context.Context, userID string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	n := 0

	for _, spent := range f.codes[userID] {
		if !spent {
			n++
		}
	}

	return n, nil
}

func (f *fakeMFA) Delete(_ context.Context, userID string) error {
	f.mu.Lock()
	delete(f.rows, userID)
	delete(f.codes, userID)
	f.mu.Unlock()
	f.setFlag(userID, false)

	return nil
}

type fakeAudit struct{ actions []string }

func (a *fakeAudit) Record(_ context.Context, e audit.Entry) error {
	a.actions = append(a.actions, e.Action)

	return nil
}

type mfaEnv struct {
	*env

	mfa   *fakeMFA
	audit *fakeAudit
	owner string
}

// newMFAEnv is an env with MFA configured and a registered owner.
func newMFAEnv(t *testing.T) *mfaEnv {
	t.Helper()

	e := newEnv(t)
	box, err := secretbox.New(strings.Repeat("ab", secretbox.KeySize))
	require.NoError(t, err)

	m := &mfaEnv{env: e, mfa: newFakeMFA(e.users), audit: &fakeAudit{}}
	e.uc = usecase.NewIdentityUseCase(e.users, e.store, e.store, e.mailer, e.events, passTx{}, e.issuer,
		usecase.WithClock(func() time.Time { return *e.clock }),
		usecase.WithMFA(m.mfa, box), usecase.WithAudit(m.audit))
	m.owner = e.register(t).User.ID

	return m
}

// codeOf is the authenticator app's current code for a base32 secret.
func (m *mfaEnv) codeOf(t *testing.T, secret string) string {
	t.Helper()

	raw, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	require.NoError(t, err)

	return domain.TOTPCode(raw, domain.TOTPStep(*m.clock))
}

// enable turns MFA on for the owner and returns the secret and recovery
// codes. The confirming code spends the current step, so the clock moves on
// to the next one.
func (m *mfaEnv) enable(t *testing.T) (string, []string) {
	t.Helper()

	enrollment, err := m.uc.EnrollMFA(t.Context(), m.owner)
	require.NoError(t, err)

	codes, err := m.uc.ConfirmMFAEnrollment(t.Context(), m.owner, m.codeOf(t, enrollment.Secret))
	require.NoError(t, err)

	m.advance(domain.TOTPPeriod)

	return enrollment.Secret, codes
}

func (m *mfaEnv) login(t *testing.T) *usecase.Session {
	t.Helper()

	session, err := m.uc.Login(t.Context(), "owner@example.com", "secret123")
	require.NoError(t, err)

	return session
}

func TestMFAEnrolmentAndSignIn(t *testing.T) {
	t.Parallel()

	m := newMFAEnv(t)

	status, err := m.uc.GetMFAStatus(t.Context(), m.owner)
	require.NoError(t, err)
	assert.False(t, status.Enabled)
	assert.False(t, m.login(t).MFARequired, "no second factor until enrolment is confirmed")

	enrollment, err := m.uc.EnrollMFA(t.Context(), m.owner)
	require.NoError(t, err)
	assert.Contains(t, enrollment.OTPAuthURI, "otpauth://totp/VietGara:owner@example.com?")
	assert.Contains(t, enrollment.OTPAuthURI, "secret="+enrollment.Secret)
	assert.False(t, m.login(t).MFARequired, "a pending enrolment does not count")

	codes, err := m.uc.ConfirmMFAEnrollment(t.Context(), m.owner, m.codeOf(t, enrollment.Secret))
	require.NoError(t, err)
	require.Len(t, codes, domain.RecoveryCodeCount)

	status, err = m.uc.GetMFAStatus(t.Context(), m.owner)
	require.NoError(t, err)
	assert.True(t, status.Enabled)
	assert.Equal(t, domain.RecoveryCodeCount, status.RecoveryCodesRemaining)
	assert.Equal(t, []string{"mfa.enabled"}, m.audit.actions)

	challenge := m.login(t)
	require.True(t, challenge.MFARequired)
	assert.NotEmpty(t, challenge.ChallengeToken)
	assert.Empty(t, challenge.AccessToken)
	assert.Empty(t, challenge.RefreshToken)
	assert.True(t, challenge.User.MFAEnabled)
	assert.WithinDuration(t, m.clock.Add(domain.MFAChallengeTTL), challenge.ChallengeExpiresAt, time.Second)

	// The code that confirmed the enrolment is spent; the next step works.
	_, err = m.uc.VerifyMFAChallenge(t.Context(), challenge.ChallengeToken, m.codeOf(t, enrollment.Secret))
	requireReason(t, err, domain.ReasonMFACodeInvalid)

	m.advance(domain.TOTPPeriod)

	session, err := m.uc.VerifyMFAChallenge(t.Context(), challenge.ChallengeToken, m.codeOf(t, enrollment.Secret))
	require.NoError(t, err)
	assert.NotEmpty(t, session.AccessToken)
	assert.NotEmpty(t, session.RefreshToken)
	assert.False(t, session.MFARequired)

	_, err = m.uc.VerifyMFAChallenge(t.Context(), challenge.ChallengeToken, m.codeOf(t, enrollment.Secret))
	requireReason(t, err, domain.ReasonMFAChallengeInvalid)

	refreshed, err := m.uc.Refresh(t.Context(), session.RefreshToken)
	require.NoError(t, err)
	assert.NotEmpty(t, refreshed.AccessToken, "refresh does not ask again")
}

func TestMFACodeWorksOnce(t *testing.T) {
	t.Parallel()

	m := newMFAEnv(t)
	secret, _ := m.enable(t)

	first := m.login(t)
	code := m.codeOf(t, secret)

	_, err := m.uc.VerifyMFAChallenge(t.Context(), first.ChallengeToken, code)
	require.NoError(t, err)

	second := m.login(t)
	_, err = m.uc.VerifyMFAChallenge(t.Context(), second.ChallengeToken, code)
	requireReason(t, err, domain.ReasonMFACodeInvalid)
}

func TestMFARecoveryCodeWorksOnce(t *testing.T) {
	t.Parallel()

	m := newMFAEnv(t)
	_, codes := m.enable(t)

	session, err := m.uc.VerifyMFAChallenge(t.Context(), m.login(t).ChallengeToken, strings.ToLower(codes[0]))
	require.NoError(t, err)
	assert.NotEmpty(t, session.AccessToken)

	_, err = m.uc.VerifyMFAChallenge(t.Context(), m.login(t).ChallengeToken, codes[0])
	requireReason(t, err, domain.ReasonMFACodeInvalid)

	_, err = m.uc.VerifyMFAChallenge(t.Context(), m.login(t).ChallengeToken, codes[1])
	require.NoError(t, err, "the other codes still work")

	status, err := m.uc.GetMFAStatus(t.Context(), m.owner)
	require.NoError(t, err)
	assert.Equal(t, domain.RecoveryCodeCount-2, status.RecoveryCodesRemaining)
}

func TestMFALocksAfterFiveWrongCodes(t *testing.T) {
	t.Parallel()

	m := newMFAEnv(t)
	secret, _ := m.enable(t)
	challenge := m.login(t)

	for i := 1; i < domain.MFAMaxFailures; i++ {
		_, err := m.uc.VerifyMFAChallenge(t.Context(), challenge.ChallengeToken, "000000")
		requireReason(t, err, domain.ReasonMFACodeInvalid)
	}

	_, err := m.uc.VerifyMFAChallenge(t.Context(), challenge.ChallengeToken, "000000")
	requireReason(t, err, domain.ReasonMFALocked)

	// Locked means locked: the right code, a fresh sign-in and the other
	// endpoints that check a code all stay refused.
	_, err = m.uc.VerifyMFAChallenge(t.Context(), challenge.ChallengeToken, m.codeOf(t, secret))
	requireReason(t, err, domain.ReasonMFALocked)

	_, err = m.uc.VerifyMFAChallenge(t.Context(), m.login(t).ChallengeToken, m.codeOf(t, secret))
	requireReason(t, err, domain.ReasonMFALocked)

	requireReason(t, m.uc.DisableMFA(t.Context(), m.owner, m.codeOf(t, secret)), domain.ReasonMFALocked)
}

func TestMFASuccessResetsFailures(t *testing.T) {
	t.Parallel()

	m := newMFAEnv(t)
	secret, _ := m.enable(t)

	for range domain.MFAMaxFailures - 1 {
		_, err := m.uc.VerifyMFAChallenge(t.Context(), m.login(t).ChallengeToken, "000000")
		requireReason(t, err, domain.ReasonMFACodeInvalid)
	}

	_, err := m.uc.VerifyMFAChallenge(t.Context(), m.login(t).ChallengeToken, m.codeOf(t, secret))
	require.NoError(t, err)

	m.advance(domain.TOTPPeriod)

	for range domain.MFAMaxFailures - 1 {
		_, err := m.uc.VerifyMFAChallenge(t.Context(), m.login(t).ChallengeToken, "000000")
		requireReason(t, err, domain.ReasonMFACodeInvalid, "the count started over")
	}
}

func TestMFAEnrolmentRules(t *testing.T) {
	t.Parallel()

	m := newMFAEnv(t)

	_, err := m.uc.ConfirmMFAEnrollment(t.Context(), m.owner, "123456")
	requireReason(t, err, domain.ReasonMFAEnrollmentNotStarted)

	enrollment, err := m.uc.EnrollMFA(t.Context(), m.owner)
	require.NoError(t, err)

	_, err = m.uc.ConfirmMFAEnrollment(t.Context(), m.owner, "000000")
	requireReason(t, err, domain.ReasonMFACodeInvalid)

	second, err := m.uc.EnrollMFA(t.Context(), m.owner)
	require.NoError(t, err)
	assert.NotEqual(t, enrollment.Secret, second.Secret, "a new enrolment replaces the pending secret")

	m.enable(t)

	_, err = m.uc.EnrollMFA(t.Context(), m.owner)
	requireReason(t, err, domain.ReasonMFAAlreadyEnabled)

	_, err = m.uc.ConfirmMFAEnrollment(t.Context(), m.owner, "123456")
	requireReason(t, err, domain.ReasonMFAAlreadyEnabled)
}

func TestMFANotForGoogleOnlyUsers(t *testing.T) {
	t.Parallel()

	m := newMFAEnv(t)
	google := &domain.User{
		Email: "g@example.com", AuthProvider: domain.AuthProviderGoogle, FullName: "G", Status: domain.UserStatusActive,
	}
	require.NoError(t, m.users.Create(t.Context(), google))

	_, err := m.uc.EnrollMFA(t.Context(), google.ID)
	requireReason(t, err, domain.ReasonMFANotAvailable)
}

func TestMFANotConfigured(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	owner := e.register(t).User.ID

	_, err := e.uc.EnrollMFA(t.Context(), owner)
	requireReason(t, err, domain.ReasonMFANotConfigured)

	_, err = e.uc.ConfirmMFAEnrollment(t.Context(), owner, "123456")
	requireReason(t, err, domain.ReasonMFANotConfigured)

	requireReason(t, e.uc.DisableMFA(t.Context(), owner, "123456"), domain.ReasonMFANotConfigured)

	status, err := e.uc.GetMFAStatus(t.Context(), owner)
	require.NoError(t, err)
	assert.False(t, status.Enabled)
}

func TestMFAWhenNotEnabled(t *testing.T) {
	t.Parallel()

	m := newMFAEnv(t)

	requireReason(t, m.uc.DisableMFA(t.Context(), m.owner, "123456"), domain.ReasonMFANotEnabled)

	_, err := m.uc.RegenerateRecoveryCodes(t.Context(), m.owner, "123456")
	requireReason(t, err, domain.ReasonMFANotEnabled)

	_, err = m.uc.VerifyMFAChallenge(t.Context(), "unknown", "123456")
	requireReason(t, err, domain.ReasonMFAChallengeInvalid)
}

func TestMFARegenerateRecoveryCodes(t *testing.T) {
	t.Parallel()

	m := newMFAEnv(t)
	secret, old := m.enable(t)

	_, err := m.uc.RegenerateRecoveryCodes(t.Context(), m.owner, old[0])
	requireReason(t, err, domain.ReasonMFACodeInvalid, "a recovery code cannot regenerate recovery codes")

	fresh, err := m.uc.RegenerateRecoveryCodes(t.Context(), m.owner, m.codeOf(t, secret))
	require.NoError(t, err)
	require.Len(t, fresh, domain.RecoveryCodeCount)
	assert.Contains(t, m.audit.actions, "mfa.recovery_codes_regenerated")

	_, err = m.uc.VerifyMFAChallenge(t.Context(), m.login(t).ChallengeToken, old[1])
	requireReason(t, err, domain.ReasonMFACodeInvalid)

	_, err = m.uc.VerifyMFAChallenge(t.Context(), m.login(t).ChallengeToken, fresh[0])
	require.NoError(t, err)
}

func TestMFADisable(t *testing.T) {
	t.Parallel()

	m := newMFAEnv(t)
	secret, codes := m.enable(t)

	requireReason(t, m.uc.DisableMFA(t.Context(), m.owner, "000000"), domain.ReasonMFACodeInvalid)
	require.NoError(t, m.uc.DisableMFA(t.Context(), m.owner, codes[0]), "a recovery code can turn MFA off")

	assert.False(t, m.login(t).MFARequired)
	assert.Equal(t, []string{"mfa.enabled", "mfa.disabled"}, m.audit.actions)

	status, err := m.uc.GetMFAStatus(t.Context(), m.owner)
	require.NoError(t, err)
	assert.False(t, status.Enabled)

	m.advance(domain.TOTPPeriod)
	enrollment, err := m.uc.EnrollMFA(t.Context(), m.owner)
	require.NoError(t, err)
	assert.NotEqual(t, secret, enrollment.Secret, "enrolling again starts from a new secret")
}

func TestMFASurvivesPasswordReset(t *testing.T) {
	t.Parallel()

	m := newMFAEnv(t)
	m.enable(t)

	_, err := m.uc.RequestPasswordReset(t.Context(), "owner@example.com")
	require.NoError(t, err)

	token, _, err := m.uc.VerifyPasswordResetOTP(t.Context(), "owner@example.com", m.mailer.lastCode(t, "owner@example.com"))
	require.NoError(t, err)
	require.NoError(t, m.uc.ResetPassword(t.Context(), token, "brand-new-pass"))

	session, err := m.uc.Login(t.Context(), "owner@example.com", "brand-new-pass")
	require.NoError(t, err)
	assert.True(t, session.MFARequired, "a password reset does not turn the second factor off")
}

func TestMFAChallengeOfSuspendedUser(t *testing.T) {
	t.Parallel()

	m := newMFAEnv(t)
	m.enable(t)
	m.users.get(m.owner).Status = domain.UserStatusSuspended

	_, err := m.uc.Login(t.Context(), "owner@example.com", "secret123")
	requireReason(t, err, domain.ReasonUserSuspended)
}
