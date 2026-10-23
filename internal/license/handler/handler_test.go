package handler_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	licensev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/license/v1"

	"github.com/viettechno/vietgara-backend/internal/license/handler"
	"github.com/viettechno/vietgara-backend/internal/license/licensetest"
	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/platform/access"
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

func as(t *testing.T, userID string) context.Context {
	t.Helper()

	claims := &auth.Claims{}
	claims.Subject = userID

	return middleware.WithClaims(t.Context(), claims)
}

func newServer() (*handler.Server, *licensetest.Env) {
	e := licensetest.NewEnv()

	return handler.NewServer(e.Subscriptions, e.Entitlements, e.Payments), e
}

func TestPoliciesAndWiring(t *testing.T) {
	t.Parallel()

	p := handler.Policies()
	assert.Len(t, p, 23)
	assert.Equal(t, access.PlatformPlanWrite, p[licensev1.LicenseAdminService_CreatePlan_FullMethodName].Platform)
	assert.True(t, p[licensev1.LicenseService_GetGarageEntitlements_FullMethodName].SkipLicense)
	assert.True(t, p[licensev1.LicenseService_ListPlans_FullMethodName].AllowUnverified)

	_, e := newServer()
	d := outbox.NewDispatcher()
	handler.Subscribe(d, outbox.NewProcessor(nil, nil), e.Subscriptions)
	assert.Len(t, d.Handlers("user.email_verified"), 1)

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

	created, err := srv.CreatePlanPaymentRequest(ctx, &licensev1.CreatePlanPaymentRequestRequest{
		PlanId: "p-pro", Kind: licensev1.PlanPaymentKind_PLAN_PAYMENT_KIND_CHANGE,
	})
	require.NoError(t, err)
	assert.EqualValues(t, 599000, created.GetRequest().GetAmountDue())
	assert.Equal(t, licensev1.PlanPaymentStatus_PLAN_PAYMENT_STATUS_AWAITING_PAYMENT, created.GetRequest().GetStatus())

	_, err = srv.CreatePlanPaymentRequest(ctx, &licensev1.CreatePlanPaymentRequestRequest{PlanId: "p-legacy"})
	require.Error(t, err)

	mine, err := srv.ListMyPlanPaymentRequests(ctx, &licensev1.ListMyPlanPaymentRequestsRequest{})
	require.NoError(t, err)
	require.Len(t, mine.GetData(), 1)

	_, err = srv.SubmitPlanPayment(ctx, &licensev1.SubmitPlanPaymentRequest{
		RequestId: created.GetRequest().GetId(), PayerName: "A", TransferredAmount: 599000,
		Evidence: &licensev1.PaymentEvidence{Content: []byte("png"), ContentType: "image/png"},
	})
	require.NoError(t, err)

	queue, err := srv.ListPlanPaymentRequests(as(t, "admin"), &licensev1.ListPlanPaymentRequestsRequest{
		Status: licensev1.PlanPaymentStatus_PLAN_PAYMENT_STATUS_SUBMITTED,
	})
	require.NoError(t, err)
	require.Len(t, queue.GetData(), 1)

	approved, err := srv.ApprovePlanPayment(as(t, "admin"), &licensev1.ApprovePlanPaymentRequest{RequestId: created.GetRequest().GetId()})
	require.NoError(t, err)
	assert.Equal(t, "pro", approved.GetSubscription().GetPlan().GetCode())

	instructions, err := srv.GetPaymentInstructions(ctx, &licensev1.GetPaymentInstructionsRequest{})
	require.NoError(t, err)
	assert.NotNil(t, instructions.GetInstructions())

	settings, err := srv.UpdatePlatformPaymentSettings(as(t, "admin"), &licensev1.UpdatePlatformPaymentSettingsRequest{
		Settings:   &licensev1.PlatformPaymentSettings{BankName: "VCB"},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"bank_name"}},
	})
	require.NoError(t, err)
	assert.Equal(t, "VCB", settings.GetSettings().GetBankName())

	_, err = srv.UpdatePlatformPaymentSettings(as(t, "admin"), &licensev1.UpdatePlatformPaymentSettingsRequest{
		Settings: &licensev1.PlatformPaymentSettings{}, UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"qr_url"}},
	})
	require.Error(t, err)

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

	require.NoError(t, e.Subscriptions.EnsureTrial(ctx, "user-1"))
	e.Store.Emails["user-1"] = "owner@example.com"

	list, err := srv.ListSubscriptions(ctx, &licensev1.ListSubscriptionsRequest{Query: "owner", PageSize: 500})
	require.NoError(t, err)
	require.Len(t, list.GetData(), 1)
	assert.EqualValues(t, 100, list.GetPagination().GetPageSize())
	assert.EqualValues(t, 1, list.GetPagination().GetPage())
	assert.Equal(t, "owner@example.com", list.GetData()[0].GetUserEmail())

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

func TestPaymentReviewHandlers(t *testing.T) {
	t.Parallel()

	srv, e := newServer()
	owner, admin := as(t, "owner"), as(t, "admin")
	e.Store.Owners["g-1"] = "owner"

	_, err := srv.GetMySubscription(owner, &licensev1.GetMySubscriptionRequest{})
	require.NoError(t, err)

	created, err := srv.CreatePlanPaymentRequest(owner, &licensev1.CreatePlanPaymentRequestRequest{
		PlanId: "p-pro", Kind: licensev1.PlanPaymentKind_PLAN_PAYMENT_KIND_CHANGE,
	})
	require.NoError(t, err)

	id := created.GetRequest().GetId()

	got, err := srv.GetMyPlanPaymentRequest(owner, &licensev1.GetMyPlanPaymentRequestRequest{RequestId: id})
	require.NoError(t, err)
	assert.Equal(t, id, got.GetRequest().GetId())

	submit := &licensev1.SubmitPlanPaymentRequest{
		RequestId: id, PayerName: "A", TransferredAmount: 599000, PaidAt: timestamppb.Now(),
		Evidence: &licensev1.PaymentEvidence{Content: []byte("png"), ContentType: "image/png"},
	}
	_, err = srv.SubmitPlanPayment(owner, submit)
	require.NoError(t, err)

	detail, err := srv.GetPlanPaymentRequest(admin, &licensev1.GetPlanPaymentRequestRequest{RequestId: id})
	require.NoError(t, err)
	assert.NotNil(t, detail.GetRequest())

	rejected, err := srv.RejectPlanPayment(admin, &licensev1.RejectPlanPaymentRequest{RequestId: id, Reason: "not received"})
	require.NoError(t, err)
	assert.Equal(t, licensev1.PlanPaymentStatus_PLAN_PAYMENT_STATUS_REJECTED, rejected.GetRequest().GetStatus())

	again, err := srv.CreatePlanPaymentRequest(owner, &licensev1.CreatePlanPaymentRequestRequest{
		PlanId: "p-pro", Kind: licensev1.PlanPaymentKind_PLAN_PAYMENT_KIND_CHANGE,
	})
	require.NoError(t, err)

	cancelled, err := srv.CancelPlanPayment(owner, &licensev1.CancelPlanPaymentRequest{RequestId: again.GetRequest().GetId()})
	require.NoError(t, err)
	assert.Equal(t, licensev1.PlanPaymentStatus_PLAN_PAYMENT_STATUS_CANCELLED, cancelled.GetRequest().GetStatus())

	sub, err := srv.GetMySubscription(owner, &licensev1.GetMySubscriptionRequest{})
	require.NoError(t, err)

	adjusted, err := srv.AdjustCredit(admin, &licensev1.AdjustCreditRequest{SubscriptionId: sub.GetSubscription().GetId(), Amount: 5000, Note: "goodwill"})
	require.NoError(t, err)
	assert.EqualValues(t, 5000, adjusted.GetEntry().GetAmount())

	ledger, err := srv.ListMyCreditLedger(owner, &licensev1.ListMyCreditLedgerRequest{})
	require.NoError(t, err)
	assert.Len(t, ledger.GetData(), 1)

	settings, err := srv.GetPlatformPaymentSettings(admin, &licensev1.GetPlatformPaymentSettingsRequest{})
	require.NoError(t, err)
	assert.NotNil(t, settings.GetSettings())

	qr, err := srv.UploadPaymentQr(admin, &licensev1.UploadPaymentQrRequest{Content: []byte("png"), ContentType: "image/png"})
	require.NoError(t, err)
	assert.NotEmpty(t, qr.GetSettings().GetQrUrl())

	_, err = srv.GetMyPlanPaymentRequest(as(t, "stranger"), &licensev1.GetMyPlanPaymentRequestRequest{RequestId: id})
	assert.Equal(t, "NOT_FOUND", reason(err))
}
