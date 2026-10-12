package domain

import (
	"errors"
	"fmt"
	"time"
)

// Durations of the subscription lifecycle. The trial length is FR-LIC-01;
// the reminder window is LLD Section 3.2. The grace period is not fixed by
// the design documents; 7 days is a placeholder until Product decides.
const (
	TrialPeriod        = 14 * 24 * time.Hour
	ExpiringSoonWindow = 7 * 24 * time.Hour
	GracePeriod        = 7 * 24 * time.Hour
	MaxExtensionDays   = 366
)

// Errors of the License module; their messages reach API responses.
var (
	ErrSubscriptionNotFound  = errors.New("this user has no subscription yet")
	ErrPlanNotFound          = errors.New("plan not found")
	ErrPlanNotAvailable      = errors.New("this plan cannot be chosen")
	ErrPlanCodeTaken         = errors.New("a plan with this code already exists")
	ErrSubscriptionCancelled = errors.New("the subscription is cancelled")
	ErrLimitReached          = errors.New("the plan limit is reached")
	ErrInvalidExtension      = errors.New("extensions must be between 1 and 366 days")
	ErrNoPendingPlanChange   = errors.New("there is no pending plan change")
)

// InvalidPlanError reports an invalid plan field.
type InvalidPlanError string

func (e InvalidPlanError) Error() string { return fmt.Sprintf("plan field %s is invalid", string(e)) }

// Reasons of the errors above.
const (
	ReasonPlanLimitReached = "PLAN_LIMIT_REACHED"
	ReasonPlanNotAvailable = "PLAN_NOT_AVAILABLE"
	ReasonPlanCodeTaken    = "PLAN_CODE_TAKEN"
)

// SubscriptionStatus is the lifecycle state of a subscription (LLD 3.1).
type SubscriptionStatus string

// Valid SubscriptionStatus values.
const (
	StatusTrial     SubscriptionStatus = "trial"
	StatusActive    SubscriptionStatus = "active"
	StatusPastDue   SubscriptionStatus = "past_due"
	StatusSuspended SubscriptionStatus = "suspended"
	StatusCancelled SubscriptionStatus = "cancelled"
)

// AllowsWrites reports whether the owner's garages may change data: only
// while in trial or paid for. Past due is read-only mode (FR-LIC-04).
func (s SubscriptionStatus) AllowsWrites() bool {
	return s == StatusTrial || s == StatusActive
}

// AllowsReads reports whether the garages may be used at all; suspended
// and cancelled owners are locked.
func (s SubscriptionStatus) AllowsReads() bool {
	return s == StatusTrial || s == StatusActive || s == StatusPastDue
}

// Subscription is the license of one owner user, covering all their
// garages (FR-LIC-01..04). See Database Design Section 6.3.
type Subscription struct {
	ID     string
	UserID string
	PlanID string
	// Plan is loaded alongside the subscription when reading.
	Plan               *Plan
	Status             SubscriptionStatus
	TrialEndAt         *time.Time
	CurrentPeriodStart time.Time
	CurrentPeriodEnd   time.Time
	// GracePeriodEnd is set once the current period has expired.
	GracePeriodEnd *time.Time
	AutoRenew      bool
	// ReminderPeriodEnd is the period end an expiring-soon reminder was
	// already sent for.
	ReminderPeriodEnd *time.Time
	// PendingChange is the plan chosen to take effect at the end of the
	// period (FR-LIC-02), loaded alongside the subscription; nil when none.
	PendingChange *PlanChange
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// NewTrial starts a TrialPeriod-long trial on the trial plan at start.
func NewTrial(userID string, plan *Plan, start time.Time) *Subscription {
	end := start.Add(TrialPeriod)

	return &Subscription{
		UserID:             userID,
		PlanID:             plan.ID,
		Plan:               plan,
		Status:             StatusTrial,
		TrialEndAt:         &end,
		CurrentPeriodStart: start,
		CurrentPeriodEnd:   end,
		AutoRenew:          true,
	}
}

// isLive reports whether the subscription still moves through the expiry
// lifecycle (cancelled and suspended ones stay put until a plan change).
func (s *Subscription) isLive() bool {
	return s.Status == StatusTrial || s.Status == StatusActive || s.Status == StatusPastDue
}

// NeedsExpiryReminder reports whether the current trial/paid period ends
// within window and no reminder was sent for it yet (LLD 3.2 step 1).
func (s *Subscription) NeedsExpiryReminder(now time.Time, window time.Duration) bool {
	if s.Status != StatusTrial && s.Status != StatusActive {
		return false
	}

	if !now.Before(s.CurrentPeriodEnd) || s.CurrentPeriodEnd.Sub(now) > window {
		return false
	}

	return s.ReminderPeriodEnd == nil || !s.ReminderPeriodEnd.Equal(s.CurrentPeriodEnd)
}

// MarkReminded records that the reminder for the current period was sent.
func (s *Subscription) MarkReminded() {
	end := s.CurrentPeriodEnd
	s.ReminderPeriodEnd = &end
}

// Expire applies LLD 3.2 step 2 at now: once the current period has ended
// the owner enters a grace period in read-only mode (past_due) and is
// suspended when the grace period is over. Automatic renewal (FR-LIC-03)
// needs the payment gateway (Phase 2). It reports whether the subscription
// changed.
func (s *Subscription) Expire(now time.Time, grace time.Duration) bool {
	if !s.isLive() || now.Before(s.CurrentPeriodEnd) {
		return false
	}

	changed := false

	if s.GracePeriodEnd == nil {
		end := s.CurrentPeriodEnd.Add(grace)
		s.GracePeriodEnd = &end
		changed = true
	}

	next := StatusSuspended
	if s.GracePeriodEnd.After(now) {
		next = StatusPastDue
	}

	if next != s.Status {
		s.Status = next
		changed = true
	}

	return changed
}

// HoldsPeriod reports whether the current period is still running or being
// waited for, so a plan chosen now takes effect at its end (FR-LIC-02): a
// trial or paid subscription. A past-due or suspended one has no period
// left to wait for and switches at once.
func (s *Subscription) HoldsPeriod() bool {
	return s.Status == StatusTrial || s.Status == StatusActive
}

// ChangePlan switches to plan at once, starting a new billing period at
// now (FR-LIC-02). Payment collection arrives in Phase 2, so there is
// nothing to wait for: it is how a past-due or suspended owner recovers,
// and how a scheduled change takes effect.
func (s *Subscription) ChangePlan(plan *Plan, now time.Time) error {
	if !plan.Selectable() {
		return ErrPlanNotAvailable
	}

	if s.Status == StatusCancelled {
		return ErrSubscriptionCancelled
	}

	s.PlanID, s.Plan = plan.ID, plan
	s.Status = StatusActive
	s.TrialEndAt = nil
	s.CurrentPeriodStart = now
	s.CurrentPeriodEnd = plan.BillingCycle.PeriodEnd(now)
	s.GracePeriodEnd = nil

	return nil
}

// Extend adds days to the current period (from now when it already
// ended) and reactivates a past-due or suspended subscription; platform
// admins renew manually until online payment exists (Phase 2).
func (s *Subscription) Extend(days int, now time.Time) error {
	if days < 1 || days > MaxExtensionDays {
		return ErrInvalidExtension
	}

	if s.Status == StatusCancelled {
		return ErrSubscriptionCancelled
	}

	from := s.CurrentPeriodEnd
	if from.Before(now) {
		from = now
	}

	s.CurrentPeriodEnd = from.Add(time.Duration(days) * 24 * time.Hour)
	s.GracePeriodEnd = nil

	if s.Status == StatusPastDue || s.Status == StatusSuspended {
		s.Status = StatusActive
	}

	if s.Status == StatusTrial && s.TrialEndAt != nil {
		end := s.CurrentPeriodEnd
		s.TrialEndAt = &end
	}

	return nil
}

// PlanChangeStatus is the state of a plan change (FR-LIC-02).
type PlanChangeStatus string

// Valid PlanChangeStatus values.
const (
	// PlanChangeScheduled takes effect when the current period ends.
	PlanChangeScheduled PlanChangeStatus = "scheduled"
	// PlanChangePostponed is due, but the new plan's limits are exceeded:
	// it stays pending and the owner is told.
	PlanChangePostponed PlanChangeStatus = "postponed"
	PlanChangeApplied   PlanChangeStatus = "applied"
	PlanChangeCancelled PlanChangeStatus = "cancelled"
)

// Pending reports whether the change has not taken effect or been cancelled.
func (s PlanChangeStatus) Pending() bool {
	return s == PlanChangeScheduled || s == PlanChangePostponed
}

// PlanChange is a plan an owner chose that has not taken effect yet.
type PlanChange struct {
	ID             string
	SubscriptionID string
	PlanID         string
	// Plan is loaded alongside the change when reading.
	Plan        *Plan
	Status      PlanChangeStatus
	EffectiveAt time.Time
	RequestedAt time.Time
	PostponedAt *time.Time
	AppliedAt   *time.Time
	CancelledAt *time.Time
}

// Due reports whether the change should take effect at now.
func (c *PlanChange) Due(now time.Time) bool {
	return c.Status.Pending() && !now.Before(c.EffectiveAt)
}
