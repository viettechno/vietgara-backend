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
}

// NewServer returns the License API server.
func NewServer(subscriptions *usecase.SubscriptionUseCase, entitlements *usecase.EntitlementUseCase) *Server {
	return &Server{subscriptions: subscriptions, entitlements: entitlements}
}

// Policies declares what every License RPC requires. The subscription is
// the owner's; any member may read a garage's entitlements (to lock UI),
// and those stay readable while the garage is locked so it can renew.
func Policies() middleware.Policies {
	entitlements := middleware.GarageMethod(access.Member())
	entitlements.SkipLicense = true

	return middleware.Policies{
		licensev1.LicenseService_ListPlans_FullMethodName:             middleware.UnverifiedMethod(),
		licensev1.LicenseService_GetMySubscription_FullMethodName:     middleware.AccountMethod(),
		licensev1.LicenseService_ChangeMyPlan_FullMethodName:          middleware.AccountMethod(),
		licensev1.LicenseService_GetGarageEntitlements_FullMethodName: entitlements,

		licensev1.LicenseAdminService_ListAllPlans_FullMethodName:       middleware.AdminMethod(),
		licensev1.LicenseAdminService_CreatePlan_FullMethodName:         middleware.AdminMethod(),
		licensev1.LicenseAdminService_UpdatePlan_FullMethodName:         middleware.AdminMethod(),
		licensev1.LicenseAdminService_ListSubscriptions_FullMethodName:  middleware.AdminMethod(),
		licensev1.LicenseAdminService_ExtendSubscription_FullMethodName: middleware.AdminMethod(),
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
	sub, usage, err := s.subscriptions.GetMine(ctx, middleware.AccountID(ctx))
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &licensev1.GetMySubscriptionResponse{Subscription: subscriptionToProto(sub, &usage)}, nil
}

// ChangeMyPlan switches the caller's plan.
func (s *Server) ChangeMyPlan(ctx context.Context, req *licensev1.ChangeMyPlanRequest) (*licensev1.ChangeMyPlanResponse, error) {
	sub, usage, err := s.subscriptions.ChangeMyPlan(ctx, middleware.AccountID(ctx), req.GetPlanId())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &licensev1.ChangeMyPlanResponse{Subscription: subscriptionToProto(sub, &usage)}, nil
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
	page, pageSize := int(req.GetPage()), int(req.GetPageSize())
	if page < 1 {
		page = 1
	}

	if pageSize < 1 {
		pageSize = defaultPageSize
	}

	pageSize = min(pageSize, maxPageSize)

	result, err := s.subscriptions.ListSubscriptions(ctx, usecase.SubscriptionFilter{
		Query: req.GetQuery(), Status: statusFromProto(req.GetStatus()), Page: page, PageSize: pageSize,
	})
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return listSubscriptionsToProto(result, page, pageSize), nil
}

// ExtendSubscription renews a subscription manually.
func (s *Server) ExtendSubscription(ctx context.Context, req *licensev1.ExtendSubscriptionRequest) (*licensev1.ExtendSubscriptionResponse, error) {
	sub, err := s.subscriptions.ExtendSubscription(ctx, req.GetSubscriptionId(), int(req.GetDays()))
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &licensev1.ExtendSubscriptionResponse{Subscription: subscriptionToProto(sub, nil)}, nil
}
