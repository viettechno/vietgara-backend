package handler

import (
	commonv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/common/v1"
	licensev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/license/v1"

	"github.com/viettechno/vietgara-backend/internal/license/domain"
	"github.com/viettechno/vietgara-backend/internal/license/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
	"github.com/viettechno/vietgara-backend/pkg/converter"
)

var statuses = map[domain.SubscriptionStatus]licensev1.SubscriptionStatus{
	domain.StatusTrial:     licensev1.SubscriptionStatus_SUBSCRIPTION_STATUS_TRIAL,
	domain.StatusActive:    licensev1.SubscriptionStatus_SUBSCRIPTION_STATUS_ACTIVE,
	domain.StatusPastDue:   licensev1.SubscriptionStatus_SUBSCRIPTION_STATUS_PAST_DUE,
	domain.StatusSuspended: licensev1.SubscriptionStatus_SUBSCRIPTION_STATUS_SUSPENDED,
	domain.StatusCancelled: licensev1.SubscriptionStatus_SUBSCRIPTION_STATUS_CANCELLED,
}

func statusFromProto(s licensev1.SubscriptionStatus) domain.SubscriptionStatus {
	for d, p := range statuses {
		if p == s {
			return d
		}
	}

	return ""
}

var features = map[domain.Feature]licensev1.Feature{
	domain.FeatureCustomers:    licensev1.Feature_FEATURE_CUSTOMERS,
	domain.FeatureQuotes:       licensev1.Feature_FEATURE_QUOTES,
	domain.FeatureRepairOrders: licensev1.Feature_FEATURE_REPAIR_ORDERS,
	domain.FeatureInventory:    licensev1.Feature_FEATURE_INVENTORY,
	domain.FeatureBilling:      licensev1.Feature_FEATURE_BILLING,
	domain.FeatureDocuments:    licensev1.Feature_FEATURE_DOCUMENTS,
	domain.FeatureReports:      licensev1.Feature_FEATURE_REPORTS,
}

func featuresToProto(in []domain.Feature) []licensev1.Feature {
	out := make([]licensev1.Feature, 0, len(in))
	for _, f := range in {
		out = append(out, features[f])
	}

	return out
}

func featuresFromProto(in []licensev1.Feature) ([]domain.Feature, bool) {
	out := make([]domain.Feature, 0, len(in))

	for _, p := range in {
		found := false

		for d, candidate := range features {
			if candidate == p {
				out = append(out, d)
				found = true
			}
		}

		if !found {
			return nil, false
		}
	}

	return out, true
}

func cycleToProto(c domain.BillingCycle) licensev1.BillingCycle {
	if c == domain.BillingCycleYearly {
		return licensev1.BillingCycle_BILLING_CYCLE_YEARLY
	}

	return licensev1.BillingCycle_BILLING_CYCLE_MONTHLY
}

func cycleFromProto(c licensev1.BillingCycle) domain.BillingCycle {
	switch c {
	case licensev1.BillingCycle_BILLING_CYCLE_MONTHLY:
		return domain.BillingCycleMonthly
	case licensev1.BillingCycle_BILLING_CYCLE_YEARLY:
		return domain.BillingCycleYearly
	case licensev1.BillingCycle_BILLING_CYCLE_UNSPECIFIED:
		return ""
	default:
		return ""
	}
}

func planToProto(p *domain.Plan) *licensev1.Plan {
	return &licensev1.Plan{
		Id:           p.ID,
		Code:         p.Code,
		Name:         p.Name,
		Description:  p.Description,
		Price:        p.Price,
		Currency:     p.Currency,
		BillingCycle: cycleToProto(p.BillingCycle),
		Limits:       limitsToProto(p.MaxGarages, p.MaxStaff, p.MaxTransactionsPerDay),
		Features:     featuresToProto(p.Features),
		IsPublic:     p.Public,
		IsActive:     p.Active,
		SortOrder:    int32(p.SortOrder), //nolint:gosec // small catalogue ordinal
		CreatedAt:    converter.TimeToProto(p.CreatedAt),
		UpdatedAt:    converter.TimeToProto(p.UpdatedAt),
	}
}

func plansToProto(plans []*domain.Plan) []*licensev1.Plan {
	out := make([]*licensev1.Plan, 0, len(plans))
	for _, p := range plans {
		out = append(out, planToProto(p))
	}

	return out
}

func limitsToProto(garages, staff, transactions int) *licensev1.PlanLimits {
	return &licensev1.PlanLimits{
		MaxGarages:            int32(garages),      //nolint:gosec // plan limits are small
		MaxStaff:              int32(staff),        //nolint:gosec // plan limits are small
		MaxTransactionsPerDay: int32(transactions), //nolint:gosec // plan limits are small
	}
}

func subscriptionToProto(s *domain.Subscription, usage *usecase.Usage) *licensev1.Subscription {
	out := &licensev1.Subscription{
		Id:                 s.ID,
		AccountId:          s.AccountID,
		Status:             statuses[s.Status],
		Plan:               planToProto(s.Plan),
		CurrentPeriodStart: converter.TimeToProto(s.CurrentPeriodStart),
		CurrentPeriodEnd:   converter.TimeToProto(s.CurrentPeriodEnd),
		AutoRenew:          s.AutoRenew,
		CreatedAt:          converter.TimeToProto(s.CreatedAt),
		UpdatedAt:          converter.TimeToProto(s.UpdatedAt),
	}

	if s.TrialEndAt != nil {
		out.TrialEndAt = converter.TimeToProto(*s.TrialEndAt)
	}

	if s.GracePeriodEnd != nil {
		out.GracePeriodEnd = converter.TimeToProto(*s.GracePeriodEnd)
	}

	if usage != nil {
		out.Usage = &licensev1.Usage{
			Garages:           int32(usage.Garages),           //nolint:gosec // bounded by plan limits
			Staff:             int32(usage.Staff),             //nolint:gosec // bounded by plan limits
			TransactionsToday: int32(usage.TransactionsToday), //nolint:gosec // bounded by plan limits
		}
	}

	return out
}

func entitlementsToProto(garageID string, e *domain.Entitlements) *licensev1.Entitlements {
	return &licensev1.Entitlements{
		GarageId:         garageID,
		Status:           statuses[e.Status],
		PlanCode:         e.PlanCode,
		Features:         featuresToProto(e.Features),
		Limits:           limitsToProto(e.MaxGarages, e.MaxStaff, e.MaxTransactionsPerDay),
		ReadOnly:         e.ReadOnly(),
		CurrentPeriodEnd: converter.TimeToProto(e.CurrentPeriodEnd),
	}
}

func listSubscriptionsToProto(page *usecase.SubscriptionPage, pageNo, pageSize int) *licensev1.ListSubscriptionsResponse {
	resp := &licensev1.ListSubscriptionsResponse{
		Data: make([]*licensev1.SubscriptionSummary, 0, len(page.Items)),
		Pagination: &commonv1.Pagination{
			Total: int32(page.Total), Page: int32(pageNo), PageSize: int32(pageSize), //nolint:gosec // bounded
		},
	}

	for _, item := range page.Items {
		resp.Data = append(resp.Data, &licensev1.SubscriptionSummary{
			Subscription: subscriptionToProto(item.Subscription, nil),
			AccountEmail: item.Email,
			AccountName:  item.FullName,
		})
	}

	return resp
}

const pathLimits = "limits"

// planSettable are the plan fields admins may set; code only on create.
var planSettable = []string{
	"code", "name", "description", "price", "currency", "billing_cycle", pathLimits, "features", "is_public", "is_active", "sort_order",
}

// planSetters copy one masked field of the API plan into PlanFields.
var planSetters = map[string]func(f *usecase.PlanFields, in *licensev1.Plan) error{
	"code": func(f *usecase.PlanFields, in *licensev1.Plan) error { f.Code = new(in.GetCode()); return nil },
	"name": func(f *usecase.PlanFields, in *licensev1.Plan) error { f.Name = new(in.GetName()); return nil },
	"description": func(f *usecase.PlanFields, in *licensev1.Plan) error {
		f.Description = new(in.GetDescription())
		return nil
	},
	"price":    func(f *usecase.PlanFields, in *licensev1.Plan) error { f.Price = new(in.GetPrice()); return nil },
	"currency": func(f *usecase.PlanFields, in *licensev1.Plan) error { f.Currency = new(in.GetCurrency()); return nil },
	"billing_cycle": func(f *usecase.PlanFields, in *licensev1.Plan) error {
		f.BillingCycle = new(cycleFromProto(in.GetBillingCycle()))
		return nil
	},
	"is_public": func(f *usecase.PlanFields, in *licensev1.Plan) error { f.Public = new(in.GetIsPublic()); return nil },
	"is_active": func(f *usecase.PlanFields, in *licensev1.Plan) error { f.Active = new(in.GetIsActive()); return nil },
	"sort_order": func(f *usecase.PlanFields, in *licensev1.Plan) error {
		f.SortOrder = new(int(in.GetSortOrder()))
		return nil
	},
	pathLimits: func(f *usecase.PlanFields, in *licensev1.Plan) error {
		setLimits(f, in.GetLimits(), pathLimits)
		return nil
	},
	"limits.max_garages": func(f *usecase.PlanFields, in *licensev1.Plan) error {
		setLimits(f, in.GetLimits(), "limits.max_garages")
		return nil
	},
	"limits.max_staff": func(f *usecase.PlanFields, in *licensev1.Plan) error {
		setLimits(f, in.GetLimits(), "limits.max_staff")
		return nil
	},
	"limits.max_transactions_per_day": func(f *usecase.PlanFields, in *licensev1.Plan) error {
		setLimits(f, in.GetLimits(), "limits.max_transactions_per_day")
		return nil
	},
	"features": func(f *usecase.PlanFields, in *licensev1.Plan) error {
		feats, ok := featuresFromProto(in.GetFeatures())
		if !ok {
			return apperror.Validation("features", apperror.IssueInvalid, "unknown feature")
		}

		f.Features = &feats

		return nil
	},
}

func planFields(in *licensev1.Plan, paths []string) (usecase.PlanFields, error) {
	var f usecase.PlanFields

	for _, path := range paths {
		set, ok := planSetters[path]
		if !ok {
			return f, apperror.Validation("updateMask", apperror.IssueInvalid, "field "+path+" cannot be updated")
		}

		if err := set(&f, in); err != nil {
			return f, err
		}
	}

	return f, nil
}

func setLimits(f *usecase.PlanFields, limits *licensev1.PlanLimits, path string) {
	if path == pathLimits || path == "limits.max_garages" {
		f.MaxGarages = new(int(limits.GetMaxGarages()))
	}

	if path == pathLimits || path == "limits.max_staff" {
		f.MaxStaff = new(int(limits.GetMaxStaff()))
	}

	if path == pathLimits || path == "limits.max_transactions_per_day" {
		f.MaxTransactionsPerDay = new(int(limits.GetMaxTransactionsPerDay()))
	}
}
