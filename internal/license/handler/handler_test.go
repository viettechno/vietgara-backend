package handler_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	licensev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/license/v1"

	"github.com/viettechno/vietgara-backend/internal/license/handler"
	"github.com/viettechno/vietgara-backend/internal/license/licensetest"
	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/platform/auth"
	"github.com/viettechno/vietgara-backend/internal/platform/outbox"
)

func reason(err error) string {
	for _, d := range status.Convert(err).Details() {
		if info, ok := d.(*errdetails.ErrorInfo); ok {
			return info.GetReason()
		}
	}

	return ""
}

func as(t *testing.T, accountID string) context.Context {
	t.Helper()

	claims := &auth.Claims{}
	claims.Subject = accountID

	return middleware.WithClaims(t.Context(), claims)
}

func newServer() (*handler.Server, *licensetest.Env) {
	e := licensetest.NewEnv()

	return handler.NewServer(e.Subscriptions, e.Entitlements), e
}

func TestPoliciesAndWiring(t *testing.T) {
	t.Parallel()

	p := handler.Policies()
	assert.Len(t, p, 9)
	assert.True(t, p[licensev1.LicenseAdminService_CreatePlan_FullMethodName].PlatformAdmin)
	assert.True(t, p[licensev1.LicenseService_GetGarageEntitlements_FullMethodName].SkipLicense)
	assert.True(t, p[licensev1.LicenseService_ListPlans_FullMethodName].AllowUnverified)

	_, e := newServer()
	d := outbox.NewDispatcher()
	handler.Subscribe(d, outbox.NewProcessor(nil, nil), e.Subscriptions)
	assert.Len(t, d.Handlers("account.email_verified"), 1)

	e.Store.Owners["g-1"] = "owner"
	mode, err := handler.NewEnforcement(e.Entitlements).LicenseMode(t.Context(), "g-1")
	require.NoError(t, err)
	assert.Equal(t, middleware.LicenseFull, mode)
}

func TestLicenseService(t *testing.T) {
	t.Parallel()

	srv, e := newServer()
	ctx := as(t, "owner")
	e.Store.Owners["g-1"] = "owner"

	plans, err := srv.ListPlans(ctx, &licensev1.ListPlansRequest{})
	require.NoError(t, err)
	require.Len(t, plans.GetData(), 2)
	assert.Equal(t, licensev1.BillingCycle_BILLING_CYCLE_YEARLY, plans.GetData()[1].GetBillingCycle())
	assert.EqualValues(t, 20, plans.GetData()[1].GetLimits().GetMaxStaff())

	sub, err := srv.GetMySubscription(ctx, &licensev1.GetMySubscriptionRequest{})
	require.NoError(t, err)
	assert.Equal(t, licensev1.SubscriptionStatus_SUBSCRIPTION_STATUS_TRIAL, sub.GetSubscription().GetStatus())
	assert.NotNil(t, sub.GetSubscription().GetTrialEndAt())
	assert.NotNil(t, sub.GetSubscription().GetUsage())

	changed, err := srv.ChangeMyPlan(ctx, &licensev1.ChangeMyPlanRequest{PlanId: "p-pro"})
	require.NoError(t, err)
	assert.Equal(t, "pro", changed.GetSubscription().GetPlan().GetCode())
	assert.Len(t, changed.GetSubscription().GetPlan().GetFeatures(), 7)

	_, err = srv.ChangeMyPlan(ctx, &licensev1.ChangeMyPlanRequest{PlanId: "p-legacy"})
	assert.Equal(t, "PLAN_NOT_AVAILABLE", reason(err))

	ent, err := srv.GetGarageEntitlements(ctx, &licensev1.GetGarageEntitlementsRequest{GarageId: "g-1"})
	require.NoError(t, err)
	assert.Equal(t, "g-1", ent.GetEntitlements().GetGarageId())
	assert.False(t, ent.GetEntitlements().GetReadOnly())

	_, err = srv.GetGarageEntitlements(ctx, &licensev1.GetGarageEntitlementsRequest{GarageId: "unknown"})
	assert.Equal(t, "NOT_FOUND", reason(err))

	e.Store.Fail["plans.list"] = assert.AnError
	_, err = srv.ListPlans(ctx, &licensev1.ListPlansRequest{})
	require.Error(t, err)
	_, err = srv.ListAllPlans(ctx, &licensev1.ListAllPlansRequest{})
	require.Error(t, err)

	e.Store.Fail["subs.find"] = assert.AnError
	_, err = srv.GetMySubscription(ctx, &licensev1.GetMySubscriptionRequest{})
	require.Error(t, err)
}

func TestLicenseAdminService(t *testing.T) {
	t.Parallel()

	srv, e := newServer()
	ctx := as(t, "admin")

	created, err := srv.CreatePlan(ctx, &licensev1.CreatePlanRequest{Plan: &licensev1.Plan{
		Code: "chain", Name: "Chain", Price: 1490000, Currency: "VND", BillingCycle: licensev1.BillingCycle_BILLING_CYCLE_MONTHLY,
		Limits:   &licensev1.PlanLimits{MaxGarages: 10, MaxStaff: 100, MaxTransactionsPerDay: 1000},
		Features: []licensev1.Feature{licensev1.Feature_FEATURE_REPORTS}, IsPublic: true, IsActive: true, SortOrder: 30,
	}})
	require.NoError(t, err)

	planID := created.GetPlan().GetId()
	assert.EqualValues(t, 10, created.GetPlan().GetLimits().GetMaxGarages())

	_, err = srv.CreatePlan(ctx, &licensev1.CreatePlanRequest{Plan: &licensev1.Plan{Code: "x", Features: []licensev1.Feature{licensev1.Feature_FEATURE_UNSPECIFIED}}})
	assert.Equal(t, "VALIDATION_ERROR", reason(err))
	_, err = srv.CreatePlan(ctx, &licensev1.CreatePlanRequest{Plan: &licensev1.Plan{Code: "chain", Name: "Dup", Limits: &licensev1.PlanLimits{MaxGarages: 1, MaxTransactionsPerDay: 1}, Currency: "VND", BillingCycle: licensev1.BillingCycle_BILLING_CYCLE_YEARLY}})
	assert.Equal(t, "PLAN_CODE_TAKEN", reason(err))

	updated, err := srv.UpdatePlan(ctx, &licensev1.UpdatePlanRequest{
		PlanId: planID, Plan: &licensev1.Plan{Limits: &licensev1.PlanLimits{MaxStaff: 50}, IsActive: false},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"limits.max_staff", "is_active"}},
	})
	require.NoError(t, err)
	assert.EqualValues(t, 50, updated.GetPlan().GetLimits().GetMaxStaff())
	assert.EqualValues(t, 10, updated.GetPlan().GetLimits().GetMaxGarages())
	assert.False(t, updated.GetPlan().GetIsActive())

	for _, paths := range [][]string{{"limits.max_garages"}, {"limits.max_transactions_per_day"}, {"billing_cycle"}} {
		_, err = srv.UpdatePlan(ctx, &licensev1.UpdatePlanRequest{PlanId: planID, Plan: &licensev1.Plan{}, UpdateMask: &fieldmaskpb.FieldMask{Paths: paths}})
		assert.Equal(t, "VALIDATION_ERROR", reason(err), paths)
	}

	_, err = srv.UpdatePlan(ctx, &licensev1.UpdatePlanRequest{PlanId: planID, Plan: &licensev1.Plan{}, UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"id"}}})
	assert.Equal(t, "VALIDATION_ERROR", reason(err))
	_, err = srv.UpdatePlan(ctx, &licensev1.UpdatePlanRequest{PlanId: planID, Plan: &licensev1.Plan{Features: []licensev1.Feature{99}}, UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"features"}}})
	assert.Equal(t, "VALIDATION_ERROR", reason(err))
	_, err = srv.UpdatePlan(ctx, &licensev1.UpdatePlanRequest{PlanId: planID, Plan: &licensev1.Plan{}})
	assert.Equal(t, "VALIDATION_ERROR", reason(err), "every field without a mask")

	all, err := srv.ListAllPlans(ctx, &licensev1.ListAllPlansRequest{})
	require.NoError(t, err)
	assert.Len(t, all.GetData(), 5)

	require.NoError(t, e.Subscriptions.EnsureTrial(ctx, "acc-1"))
	e.Store.Emails["acc-1"] = "owner@example.com"

	list, err := srv.ListSubscriptions(ctx, &licensev1.ListSubscriptionsRequest{Query: "owner", PageSize: 500})
	require.NoError(t, err)
	require.Len(t, list.GetData(), 1)
	assert.EqualValues(t, 100, list.GetPagination().GetPageSize())
	assert.EqualValues(t, 1, list.GetPagination().GetPage())
	assert.Equal(t, "owner@example.com", list.GetData()[0].GetAccountEmail())

	list, err = srv.ListSubscriptions(ctx, &licensev1.ListSubscriptionsRequest{Status: licensev1.SubscriptionStatus_SUBSCRIPTION_STATUS_SUSPENDED, Page: 2})
	require.NoError(t, err)
	assert.Empty(t, list.GetData())
	assert.EqualValues(t, 20, list.GetPagination().GetPageSize())

	page, err := srv.ListSubscriptions(ctx, &licensev1.ListSubscriptionsRequest{})
	require.NoError(t, err)

	subID := page.GetData()[0].GetSubscription().GetId()

	extended, err := srv.ExtendSubscription(ctx, &licensev1.ExtendSubscriptionRequest{SubscriptionId: subID, Days: 30})
	require.NoError(t, err)
	assert.Equal(t, licensev1.SubscriptionStatus_SUBSCRIPTION_STATUS_TRIAL, extended.GetSubscription().GetStatus())
	assert.Nil(t, extended.GetSubscription().GetUsage())

	_, err = srv.ExtendSubscription(ctx, &licensev1.ExtendSubscriptionRequest{SubscriptionId: subID})
	assert.Equal(t, "VALIDATION_ERROR", reason(err))

	e.Store.Fail["subs.search"] = assert.AnError
	_, err = srv.ListSubscriptions(ctx, &licensev1.ListSubscriptionsRequest{})
	require.Error(t, err)
}
