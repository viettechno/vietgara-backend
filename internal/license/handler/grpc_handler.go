// Package handler exposes the License Management module: the garage-facing
// LicenseService and the platform admins' LicenseAdminService (REST through
// grpc-gateway), the license enforcement used by the request middleware,
// and the module's event subscriptions.
package handler

import (
	"context"

	licensev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/license/v1"

	"github.com/viettechno/vietgara-backend/internal/license/usecase"
	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/platform/access"
	"github.com/viettechno/vietgara-backend/internal/platform/httpapi"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// Pagination bounds of the admin subscription list (API Specification 1.1).
const (
	defaultPageSize = 20
	maxPageSize     = 100
)

// Server implements licensev1.LicenseServiceServer and
// licensev1.LicenseAdminServiceServer.
type Server struct {
	licensev1.UnimplementedLicenseServiceServer
	licensev1.UnimplementedLicenseAdminServiceServer

	subscriptions *usecase.SubscriptionUseCase
	entitlements  *usecase.EntitlementUseCase
	payments      *usecase.PaymentUseCase
}

// NewServer returns the License API server.
func NewServer(
	subscriptions *usecase.SubscriptionUseCase, entitlements *usecase.EntitlementUseCase, payments *usecase.PaymentUseCase,
) *Server {
	return &Server{subscriptions: subscriptions, entitlements: entitlements, payments: payments}
}

// Policies declares what every License RPC requires. The subscription is
// the owner's; any member may read a garage's entitlements (to lock UI),
// and those stay readable while the garage is locked so it can renew.
func Policies() middleware.Policies {
	entitlements := middleware.GarageMethod(access.Member())
	entitlements.SkipLicense = true

	return middleware.Policies{
		licensev1.LicenseService_ListPlans_FullMethodName:                 middleware.UnverifiedMethod(),
		licensev1.LicenseService_GetMySubscription_FullMethodName:         middleware.UserMethod(),
		licensev1.LicenseService_CreatePlanPaymentRequest_FullMethodName:  middleware.UserMethod(),
		licensev1.LicenseService_ListMyPlanPaymentRequests_FullMethodName: middleware.UserMethod(),
		licensev1.LicenseService_GetMyPlanPaymentRequest_FullMethodName:   middleware.UserMethod(),
		licensev1.LicenseService_SubmitPlanPayment_FullMethodName:         middleware.UserMethod(),
		licensev1.LicenseService_CancelPlanPayment_FullMethodName:         middleware.UserMethod(),
		licensev1.LicenseService_ListMyCreditLedger_FullMethodName:        middleware.UserMethod(),
		licensev1.LicenseService_GetPaymentInstructions_FullMethodName:    middleware.UserMethod(),
		licensev1.LicenseService_GetGarageEntitlements_FullMethodName:     entitlements,

		licensev1.LicenseAdminService_ListAllPlans_FullMethodName:       middleware.PlatformMethod(access.PlatformPlanRead),
		licensev1.LicenseAdminService_CreatePlan_FullMethodName:         middleware.PlatformMethod(access.PlatformPlanWrite),
		licensev1.LicenseAdminService_UpdatePlan_FullMethodName:         middleware.PlatformMethod(access.PlatformPlanWrite),
		licensev1.LicenseAdminService_ListSubscriptions_FullMethodName:  middleware.PlatformMethod(access.PlatformSubscriptionRead),
		licensev1.LicenseAdminService_ExtendSubscription_FullMethodName: middleware.PlatformMethod(access.PlatformSubscriptionWrite),

		licensev1.LicenseAdminService_ListPlanPaymentRequests_FullMethodName:       middleware.PlatformMethod(access.PlatformSubscriptionRead),
		licensev1.LicenseAdminService_GetPlanPaymentRequest_FullMethodName:         middleware.PlatformMethod(access.PlatformSubscriptionRead),
		licensev1.LicenseAdminService_ApprovePlanPayment_FullMethodName:            middleware.PlatformMethod(access.PlatformSubscriptionWrite),
		licensev1.LicenseAdminService_RejectPlanPayment_FullMethodName:             middleware.PlatformMethod(access.PlatformSubscriptionWrite),
		licensev1.LicenseAdminService_AdjustCredit_FullMethodName:                  middleware.PlatformMethod(access.PlatformSubscriptionWrite),
		licensev1.LicenseAdminService_GetPlatformPaymentSettings_FullMethodName:    middleware.PlatformMethod(access.PlatformPlanRead),
		licensev1.LicenseAdminService_UpdatePlatformPaymentSettings_FullMethodName: middleware.PlatformMethod(access.PlatformPlanWrite),
		licensev1.LicenseAdminService_UploadPaymentQr_FullMethodName:               middleware.PlatformMethod(access.PlatformPlanWrite),
	}
}

// ListPlans returns the plans owners may choose.
func (s *Server) ListPlans(ctx context.Context, _ *licensev1.ListPlansRequest) (*licensev1.ListPlansResponse, error) {
	plans, err := s.subscriptions.ListPlans(ctx)
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &licensev1.ListPlansResponse{Data: plansToProto(plans)}, nil
}

// GetMySubscription returns the caller's subscription and usage.
func (s *Server) GetMySubscription(ctx context.Context, _ *licensev1.GetMySubscriptionRequest) (*licensev1.GetMySubscriptionResponse, error) {
	sub, usage, err := s.subscriptions.GetMine(ctx, middleware.UserID(ctx))
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	out := subscriptionToProto(sub, &usage)

	open, err := s.payments.OpenRequest(ctx, middleware.UserID(ctx))
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	if open != nil {
		out.OpenRequest = paymentRequestToProto(*open)
	}

	return &licensev1.GetMySubscriptionResponse{Subscription: out}, nil
}

// GetGarageEntitlements returns what the garage may use.
func (s *Server) GetGarageEntitlements(ctx context.Context, req *licensev1.GetGarageEntitlementsRequest) (*licensev1.GetGarageEntitlementsResponse, error) {
	e, err := s.entitlements.ForGarage(ctx, req.GetGarageId())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &licensev1.GetGarageEntitlementsResponse{Entitlements: entitlementsToProto(req.GetGarageId(), e)}, nil
}

// ListAllPlans returns every plan.
func (s *Server) ListAllPlans(ctx context.Context, _ *licensev1.ListAllPlansRequest) (*licensev1.ListAllPlansResponse, error) {
	plans, err := s.subscriptions.ListAllPlans(ctx)
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &licensev1.ListAllPlansResponse{Data: plansToProto(plans)}, nil
}

// CreatePlan adds a plan; answers 201.
func (s *Server) CreatePlan(ctx context.Context, req *licensev1.CreatePlanRequest) (*licensev1.CreatePlanResponse, error) {
	fields, err := planFields(req.GetPlan(), planSettable)
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	plan, err := s.subscriptions.CreatePlan(ctx, fields)
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	httpapi.Created(ctx)

	return &licensev1.CreatePlanResponse{Plan: planToProto(plan)}, nil
}

// UpdatePlan changes the fields named by the update mask.
func (s *Server) UpdatePlan(ctx context.Context, req *licensev1.UpdatePlanRequest) (*licensev1.UpdatePlanResponse, error) {
	paths := req.GetUpdateMask().GetPaths()
	if len(paths) == 0 {
		paths = planSettable[1:]
	}

	fields, err := planFields(req.GetPlan(), paths)
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	plan, err := s.subscriptions.UpdatePlan(ctx, req.GetPlanId(), fields)
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &licensev1.UpdatePlanResponse{Plan: planToProto(plan)}, nil
}

// ListSubscriptions lists subscriptions for platform admins.
func (s *Server) ListSubscriptions(ctx context.Context, req *licensev1.ListSubscriptionsRequest) (*licensev1.ListSubscriptionsResponse, error) {
	page, pageSize := pageOf(req.GetPage(), req.GetPageSize())

	result, err := s.subscriptions.ListSubscriptions(ctx, usecase.SubscriptionFilter{
		Query: req.GetQuery(), Status: statusFromProto(req.GetStatus()), Page: page, PageSize: pageSize,
	})
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	ids := make([]string, 0, len(result.Items))
	for _, item := range result.Items {
		ids = append(ids, item.Subscription.ID)
	}

	open, err := s.payments.WithOpenRequests(ctx, ids)
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	resp := listSubscriptionsToProto(result, page, pageSize)

	for i, item := range result.Items {
		if v, ok := open[item.Subscription.ID]; ok {
			resp.Data[i].Subscription.OpenRequest = paymentRequestToProto(v)
		}
	}

	return resp, nil
}

// ExtendSubscription renews a subscription manually.
func (s *Server) ExtendSubscription(ctx context.Context, req *licensev1.ExtendSubscriptionRequest) (*licensev1.ExtendSubscriptionResponse, error) {
	sub, err := s.subscriptions.ExtendSubscription(ctx, req.GetSubscriptionId(), int(req.GetDays()))
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &licensev1.ExtendSubscriptionResponse{Subscription: subscriptionToProto(sub, nil)}, nil
}

// pageOf applies the paging defaults of API Specification Section 1.1.
func pageOf(page, pageSize int32) (int, int) {
	p, size := int(page), int(pageSize)
	if p < 1 {
		p = 1
	}

	if size < 1 {
		size = defaultPageSize
	}

	return p, min(size, maxPageSize)
}
