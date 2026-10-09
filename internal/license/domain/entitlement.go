package domain

import (
	"errors"
	"slices"
	"time"
)

// LimitType names a countable resource capped by a plan.
type LimitType string

// Valid LimitType values.
const (
	LimitGarages      LimitType = "max_garages"
	LimitStaff        LimitType = "max_staff"
	LimitTransactions LimitType = "max_transactions_per_day"
)

// ErrUnknownLimit is returned for a LimitType the plan does not define.
var ErrUnknownLimit = errors.New("unknown plan limit")

// Entitlements is what an owner's garages may currently use, derived from
// the owner's subscription. It is the value stored in the entitlement
// cache (LLD Section 3.3).
type Entitlements struct {
	UserID   string             `json:"user_id"`
	Status   SubscriptionStatus `json:"status"`
	PlanCode string             `json:"plan_code"`
	// Features lists the enabled features, in display order.
	Features              []Feature `json:"features"`
	MaxGarages            int       `json:"max_garages"`
	MaxStaff              int       `json:"max_staff"`
	MaxTransactionsPerDay int       `json:"max_transactions_per_day"`
	CurrentPeriodEnd      time.Time `json:"current_period_end"`
}

// NewEntitlements derives an owner's entitlements from the subscription:
// the plan's features and limits, no features while locked.
func NewEntitlements(s *Subscription) *Entitlements {
	e := &Entitlements{
		UserID:                s.UserID,
		Status:                s.Status,
		PlanCode:              s.Plan.Code,
		Features:              []Feature{},
		MaxGarages:            s.Plan.MaxGarages,
		MaxStaff:              s.Plan.MaxStaff,
		MaxTransactionsPerDay: s.Plan.MaxTransactionsPerDay,
		CurrentPeriodEnd:      s.CurrentPeriodEnd,
	}

	if s.Status.AllowsReads() {
		e.Features = NormalizeFeatures(s.Plan.Features)
	}

	return e
}

// ReadOnly reports whether writes are blocked.
func (e *Entitlements) ReadOnly() bool {
	return !e.Status.AllowsWrites()
}

// HasFeature reports whether feature is enabled.
func (e *Entitlements) HasFeature(feature Feature) bool {
	return slices.Contains(e.Features, feature)
}

// Limit returns the value of limit.
func (e *Entitlements) Limit(limit LimitType) (int, error) {
	switch limit {
	case LimitGarages:
		return e.MaxGarages, nil
	case LimitStaff:
		return e.MaxStaff, nil
	case LimitTransactions:
		return e.MaxTransactionsPerDay, nil
	default:
		return 0, ErrUnknownLimit
	}
}

// WithinLimit reports whether an owner currently using count units of
// limit may add one more.
func (e *Entitlements) WithinLimit(limit LimitType, count int) (bool, error) {
	allowed, err := e.Limit(limit)
	if err != nil {
		return false, err
	}

	return count < allowed, nil
}

// FeatureFlags returns the per-feature switches persisted in the
// entitlements table: every feature, enabled only if the owner may
// currently use it.
func FeatureFlags(s *Subscription) map[Feature]bool {
	flags := make(map[Feature]bool, len(AllFeatures))
	for _, f := range AllFeatures {
		flags[f] = s.Plan.HasFeature(f) && s.Status.AllowsReads()
	}

	return flags
}
