// Package handler exposes read-only support access (Release 8.2) as gRPC
// services (REST through grpc-gateway): the platform side for admin staff and
// the garage side for the owner.
package handler

import (
	"context"

	supportv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/support/v1"

	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/platform/access"
	"github.com/viettechno/vietgara-backend/internal/platform/httpapi"
	"github.com/viettechno/vietgara-backend/internal/support/domain"
	"github.com/viettechno/vietgara-backend/internal/support/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

const (
	defaultPageSize = 20
	maxPageSize     = 100
)

// Server implements the support gRPC services.
type Server struct {
	supportv1.UnimplementedSupportServiceServer
	supportv1.UnimplementedGarageSupportServiceServer

	uc *usecase.UseCase
}

// NewServer returns a Server backed by uc.
func NewServer(uc *usecase.UseCase) *Server { return &Server{uc: uc} }

// Policies declares what every support RPC requires. The owner's calls are
// for the owner alone, work on an inactive garage and stay reachable when
// the subscription is locked: revoking a grant must always be possible.
func Policies() middleware.Policies {
	owner := middleware.Policy{Garage: &access.Rule{OwnerOnly: true, AllowInactive: true}, SkipLicense: true}

	return middleware.Policies{
		supportv1.SupportService_ListMySupportSessions_FullMethodName:    middleware.UserMethod(),
		supportv1.SupportService_ListGarageDirectory_FullMethodName:      middleware.PlatformMethod(access.PlatformGarageDirectoryRead),
		supportv1.SupportService_RequestSupportGrant_FullMethodName:      middleware.PlatformMethod(access.PlatformSupportWrite),
		supportv1.SupportService_ListSupportGrants_FullMethodName:        middleware.PlatformMethod(access.PlatformSupportRead),
		supportv1.SupportService_EndSupportGrant_FullMethodName:          middleware.PlatformMethod(access.PlatformSupportWrite),
		supportv1.SupportService_ListSupportOutboxEvents_FullMethodName:  middleware.PlatformMethod(access.PlatformSupportWrite),
		supportv1.SupportService_ListSupportNotifications_FullMethodName: middleware.PlatformMethod(access.PlatformSupportWrite),

		supportv1.GarageSupportService_ListGarageSupportGrants_FullMethodName: owner,
		supportv1.GarageSupportService_ApproveSupportGrant_FullMethodName:     owner,
		supportv1.GarageSupportService_RevokeSupportGrant_FullMethodName:      owner,
		supportv1.GarageSupportService_ListSupportAccessLog_FullMethodName:    owner,
	}
}

// ListMySupportSessions lists the garages the caller can read now.
func (s *Server) ListMySupportSessions(ctx context.Context, _ *supportv1.ListMySupportSessionsRequest) (*supportv1.ListMySupportSessionsResponse, error) {
	grants, err := s.uc.ListMine(ctx, domain.StatusActive)
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &supportv1.ListMySupportSessionsResponse{Data: s.grantsToProto(grants)}, nil
}

// ListGarageDirectory searches garages by name and owner.
func (s *Server) ListGarageDirectory(ctx context.Context, req *supportv1.ListGarageDirectoryRequest) (*supportv1.ListGarageDirectoryResponse, error) {
	page := pageOf(req.GetPage(), req.GetPageSize())

	entries, total, err := s.uc.Directory(ctx, req.GetQuery(), page)
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &supportv1.ListGarageDirectoryResponse{Data: mapAll(entries, directoryToProto), Pagination: paginationOf(total, page)}, nil
}

// RequestSupportGrant asks for read-only access to a garage; answers 201.
func (s *Server) RequestSupportGrant(ctx context.Context, req *supportv1.RequestSupportGrantRequest) (*supportv1.RequestSupportGrantResponse, error) {
	grant, err := s.uc.Request(ctx, req.GetGarageId(), req.GetReason(), int(req.GetTtlHours()))
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	httpapi.Created(ctx)

	return &supportv1.RequestSupportGrantResponse{Grant: s.grantToProto(grant)}, nil
}

// ListSupportGrants lists the caller's own grants.
func (s *Server) ListSupportGrants(ctx context.Context, req *supportv1.ListSupportGrantsRequest) (*supportv1.ListSupportGrantsResponse, error) {
	grants, err := s.uc.ListMine(ctx, statusFromProto(req.GetStatus()))
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &supportv1.ListSupportGrantsResponse{Data: s.grantsToProto(grants)}, nil
}

// EndSupportGrant closes the caller's own grant.
func (s *Server) EndSupportGrant(ctx context.Context, req *supportv1.EndSupportGrantRequest) (*supportv1.EndSupportGrantResponse, error) {
	grant, err := s.uc.End(ctx, req.GetGrantId())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &supportv1.EndSupportGrantResponse{Grant: s.grantToProto(grant)}, nil
}

// ListSupportOutboxEvents shows the delivery status of the garage's events.
func (s *Server) ListSupportOutboxEvents(ctx context.Context, req *supportv1.ListSupportOutboxEventsRequest) (*supportv1.ListSupportOutboxEventsResponse, error) {
	page := pageOf(req.GetPage(), req.GetPageSize())

	events, total, err := s.uc.OutboxEvents(ctx, req.GetGrantId(), req.GetStatus(), page)
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &supportv1.ListSupportOutboxEventsResponse{Data: mapAll(events, outboxToProto), Pagination: paginationOf(total, page)}, nil
}

// ListSupportNotifications shows the notification log of the garage's people.
func (s *Server) ListSupportNotifications(ctx context.Context, req *supportv1.ListSupportNotificationsRequest) (*supportv1.ListSupportNotificationsResponse, error) {
	page := pageOf(req.GetPage(), req.GetPageSize())

	notifications, total, err := s.uc.Notifications(ctx, req.GetGrantId(), page)
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &supportv1.ListSupportNotificationsResponse{Data: mapAll(notifications, notificationToProto), Pagination: paginationOf(total, page)}, nil
}

// ListGarageSupportGrants lists the garage's grants.
func (s *Server) ListGarageSupportGrants(ctx context.Context, req *supportv1.ListGarageSupportGrantsRequest) (*supportv1.ListGarageSupportGrantsResponse, error) {
	grants, err := s.uc.ListForGarage(ctx, statusFromProto(req.GetStatus()))
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &supportv1.ListGarageSupportGrantsResponse{Data: s.grantsToProto(grants)}, nil
}

// ApproveSupportGrant is the owner's yes to a pending request.
func (s *Server) ApproveSupportGrant(ctx context.Context, req *supportv1.ApproveSupportGrantRequest) (*supportv1.ApproveSupportGrantResponse, error) {
	grant, err := s.uc.Approve(ctx, req.GetGrantId())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &supportv1.ApproveSupportGrantResponse{Grant: s.grantToProto(grant)}, nil
}

// RevokeSupportGrant ends an active grant, or declines a pending one.
func (s *Server) RevokeSupportGrant(ctx context.Context, req *supportv1.RevokeSupportGrantRequest) (*supportv1.RevokeSupportGrantResponse, error) {
	grant, err := s.uc.Revoke(ctx, req.GetGrantId())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &supportv1.RevokeSupportGrantResponse{Grant: s.grantToProto(grant)}, nil
}

// ListSupportAccessLog lists what support read in the garage.
func (s *Server) ListSupportAccessLog(ctx context.Context, req *supportv1.ListSupportAccessLogRequest) (*supportv1.ListSupportAccessLogResponse, error) {
	page := pageOf(req.GetPage(), req.GetPageSize())

	events, total, err := s.uc.AccessLog(ctx, req.GetGrantId(), page)
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &supportv1.ListSupportAccessLogResponse{Data: mapAll(events, eventToProto), Pagination: paginationOf(total, page)}, nil
}

// pageOf applies the paging defaults of API Specification Section 1.1.
func pageOf(page, pageSize int32) domain.Page {
	p, size := int(page), int(pageSize)
	if p < 1 {
		p = 1
	}

	if size < 1 {
		size = defaultPageSize
	}

	return domain.Page{Page: p, PageSize: min(size, maxPageSize)}
}
