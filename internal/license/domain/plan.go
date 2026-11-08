// Package domain contains pure business types for the License Management
// module (HLD module #3). See LLD Section 3.1.
package domain

import (
	"regexp"
	"slices"
	"strings"
	"time"
)

// TrialPlanCode is the plan every new owner starts on (FR-LIC-01).
const TrialPlanCode = "trial"

// TransactionKind is a kind of business document a plan may count against
// its daily transaction limit (the Postgres transaction_kind enum). A new
// module that should count adds a value here and one call in its use case.
type TransactionKind string

// Valid TransactionKind values.
const (
	TransactionRepairOrder TransactionKind = "repair_order"
	TransactionQuote       TransactionKind = "quote"
	TransactionSettlement  TransactionKind = "settlement"
)

// AllTransactionKinds lists every kind in display order.
var AllTransactionKinds = []TransactionKind{TransactionRepairOrder, TransactionQuote, TransactionSettlement}

// IsValid reports whether k is a defined kind.
func (k TransactionKind) IsValid() bool { return slices.Contains(AllTransactionKinds, k) }

// NormalizeTransactionKinds de-duplicates kinds in display order.
func NormalizeTransactionKinds(kinds []TransactionKind) []TransactionKind {
	out := make([]TransactionKind, 0, len(kinds))

	for _, k := range AllTransactionKinds {
		if slices.Contains(kinds, k) {
			out = append(out, k)
		}
	}

	return out
}

// BillingCycle is how often a plan is billed.
type BillingCycle string

// Valid BillingCycle values.
const (
	BillingCycleMonthly BillingCycle = "monthly"
	BillingCycleYearly  BillingCycle = "yearly"
)

// IsValid reports whether b is a defined cycle.
func (b BillingCycle) IsValid() bool {
	return b == BillingCycleMonthly || b == BillingCycleYearly
}

// PeriodEnd returns when a billing period starting at start ends.
func (b BillingCycle) PeriodEnd(start time.Time) time.Time {
	if b == BillingCycleYearly {
		return start.AddDate(1, 0, 0)
	}

	return start.AddDate(0, 1, 0)
}

// Feature is a module a plan may include.
type Feature string

// Features (the Postgres plan_feature enum).
const (
	FeatureCustomers    Feature = "customers"
	FeatureQuotes       Feature = "quotes"
	FeatureRepairOrders Feature = "repair_orders"
	FeatureInventory    Feature = "inventory"
	FeatureBilling      Feature = "billing"
	FeatureDocuments    Feature = "documents"
	FeatureReports      Feature = "reports"
)

// AllFeatures lists every feature in display order.
var AllFeatures = []Feature{
	FeatureCustomers, FeatureQuotes, FeatureRepairOrders, FeatureInventory, FeatureBilling, FeatureDocuments, FeatureReports,
}

// IsValid reports whether f is a defined feature.
func (f Feature) IsValid() bool { return slices.Contains(AllFeatures, f) }

// Plan is a subscription plan offered by the platform (Database Design
// Section 6.3). Price is in the smallest currency unit. Limits apply to
// all garages of the subscribing owner.
type Plan struct {
	ID                    string
	Code                  string
	Name                  string
	Description           string
	Price                 int64
	Currency              string
	BillingCycle          BillingCycle
	MaxGarages            int
	MaxStaff              int
	MaxTransactionsPerDay int
	// CountedKinds are the document kinds that count against
	// MaxTransactionsPerDay; other kinds are neither counted nor limited.
	CountedKinds []TransactionKind
	// TrialDays is the length of the trial in days; only the trial plan has
	// it, and it applies to trials started afterwards.
	TrialDays int
	Features  []Feature
	// Public plans are offered to owners; the trial is not.
	Public bool
	// Inactive plans cannot be chosen; subscribers keep them.
	Active    bool
	SortOrder int
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Selectable reports whether owners may switch to the plan.
func (p *Plan) Selectable() bool {
	return p.Public && p.Active && p.Code != TrialPlanCode
}

// CountsTransaction reports whether a document of kind counts against the
// plan's daily transaction limit.
func (p *Plan) CountsTransaction(kind TransactionKind) bool {
	return slices.Contains(p.CountedKinds, kind)
}

// HasFeature reports whether the plan includes f.
func (p *Plan) HasFeature(f Feature) bool { return slices.Contains(p.Features, f) }

var planCode = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{1,49}$`)

// Validate checks a plan an admin creates or edits (FR-LIC-05).
func (p *Plan) Validate() error {
	switch {
	case !planCode.MatchString(p.Code):
		return InvalidPlanError("code")
	case strings.TrimSpace(p.Name) == "" || len(p.Name) > 100:
		return InvalidPlanError("name")
	case p.Price < 0:
		return InvalidPlanError("price")
	case len(p.Currency) != 3:
		return InvalidPlanError("currency")
	case !p.BillingCycle.IsValid():
		return InvalidPlanError("billingCycle")
	case p.MaxGarages < 1:
		return InvalidPlanError("limits.maxGarages")
	case p.MaxStaff < 0:
		return InvalidPlanError("limits.maxStaff")
	case p.MaxTransactionsPerDay < 1:
		return InvalidPlanError("limits.maxTransactionsPerDay")
	}

	if err := p.validateTrialAndKinds(); err != nil {
		return err
	}

	for _, f := range p.Features {
		if !f.IsValid() {
			return InvalidPlanError("features")
		}
	}

	return nil
}

// validateTrialAndKinds checks the trial length (only the trial plan has one)
// and the counted transaction kinds.
func (p *Plan) validateTrialAndKinds() error {
	if p.Code == TrialPlanCode && p.TrialDays < 1 || p.Code != TrialPlanCode && p.TrialDays != 0 {
		return InvalidPlanError("trialDays")
	}

	for _, k := range p.CountedKinds {
		if !k.IsValid() {
			return InvalidPlanError("countedTransactionKinds")
		}
	}

	return nil
}

// NormalizeFeatures de-duplicates features in display order.
func NormalizeFeatures(features []Feature) []Feature {
	out := make([]Feature, 0, len(features))

	for _, f := range AllFeatures {
		if slices.Contains(features, f) {
			out = append(out, f)
		}
	}

	return out
}
