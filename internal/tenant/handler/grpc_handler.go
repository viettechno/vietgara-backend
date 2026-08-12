// Package handler wires the Tenant/Garage usecases to the gRPC services
// generated from vietgara-proto (proto/vietgara/tenant/v1).
package handler

import (
	"context"
	"time"

	tenantv1 "github.com/viettechno/vietgara-proto/gen/go/vietgara/tenant/v1"

	"github.com/viettechno/vietgara-backend/internal/tenant/domain"
	"github.com/viettechno/vietgara-backend/internal/tenant/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Server implements tenantv1.GarageServiceServer and
// tenantv1.StaffServiceServer.
//
// UpdateStaff and RemoveStaff are not implemented yet: the StaffUseCase /
// repository layers have no update/remove operations, so calls fall through
// to the embedded Unimplemented servers and return codes.Unimplemented.
type Server struct {
	tenantv1.UnimplementedGarageServiceServer
	tenantv1.UnimplementedStaffServiceServer

	garage *usecase.GarageUseCase
	staff  *usecase.StaffUseCase
}

// NewServer wires the handler to its use cases and returns a ready Server.
func NewServer(
	garage *usecase.GarageUseCase,
	staff *usecase.StaffUseCase,
) *Server {
	return &Server{
		garage: garage,
		staff:  staff,
	}
}

// accountIDFromContext reads the account_id (JWT "sub" claim) attached by
// middleware.AuthInterceptor -- see internal/middleware/interceptor.go.
func accountIDFromContext(ctx context.Context) (string, error) {
	// The production middleware stores the subject under accountIDCtxKey;
	// read it inline here to keep this package's import graph minimal.
	if v, ok := ctx.Value(accountIDCtxKey{}).(string); ok {
		return v, nil
	}

	return "", status.Error(codes.Unauthenticated, "missing account context")
}

type accountIDCtxKey struct{}

// CreateGarage validates the request, creates the garage for the caller's
// account, and returns its API representation.
func (s *Server) CreateGarage(ctx context.Context, req *tenantv1.CreateGarageRequest) (*tenantv1.CreateGarageResponse, error) {
	accountID, err := accountIDFromContext(ctx)
	if err != nil {
		return nil, err
	}

	g, err := s.garage.Create(ctx, usecase.CreateGarageRequest{
		AccountID: accountID,
		Name:      req.GetName(),
		TaxCode:   req.TaxCode,
		Address:   req.Address,
		Timezone:  req.Timezone,
		Currency:  req.Currency,
	})
	if err != nil {
		return nil, toGRPCError(err)
	}

	return &tenantv1.CreateGarageResponse{Garage: garageToProto(g)}, nil
}

// GetGarage returns the garage with the given id.
func (s *Server) GetGarage(ctx context.Context, req *tenantv1.GetGarageRequest) (*tenantv1.GetGarageResponse, error) {
	g, err := s.garage.Get(ctx, req.GetGarageId())
	if err != nil {
		return nil, toGRPCError(err)
	}

	return &tenantv1.GetGarageResponse{Garage: garageToProto(g)}, nil
}

// UpdateGarage applies the partial updates in req to an existing garage
// and returns its updated API representation.
func (s *Server) UpdateGarage(ctx context.Context, req *tenantv1.UpdateGarageRequest) (*tenantv1.UpdateGarageResponse, error) {
	g, err := s.garage.Update(ctx, usecase.UpdateGarageRequest{
		GarageID: req.GetGarageId(),
		Name:     req.Name,
		TaxCode:  req.TaxCode,
		Address:  req.Address,
		LogoURL:  req.LogoUrl,
		Timezone: req.Timezone,
		Currency: req.Currency,
	})
	if err != nil {
		return nil, toGRPCError(err)
	}

	return &tenantv1.UpdateGarageResponse{Garage: garageToProto(g)}, nil
}

// ListGarages returns every garage owned by the caller's account.
func (s *Server) ListGarages(ctx context.Context, _ *tenantv1.ListGaragesRequest) (*tenantv1.ListGaragesResponse, error) {
	accountID, err := accountIDFromContext(ctx)
	if err != nil {
		return nil, err
	}

	garages, err := s.garage.List(ctx, accountID)
	if err != nil {
		return nil, toGRPCError(err)
	}

	resp := &tenantv1.ListGaragesResponse{}
	for _, g := range garages {
		resp.Garages = append(resp.Garages, garageToProto(g))
	}

	return resp, nil
}

// AddStaff adds the given account as a staff member of a garage.
func (s *Server) AddStaff(ctx context.Context, req *tenantv1.AddStaffRequest) (*tenantv1.AddStaffResponse, error) {
	st, err := s.staff.Add(ctx, usecase.AddStaffRequest{
		GarageID:  req.GetGarageId(),
		AccountID: req.GetAccountId(),
		Role:      roleFromProto(req.GetRole()),
	})
	if err != nil {
		return nil, toGRPCError(err)
	}

	return &tenantv1.AddStaffResponse{Staff: staffToProto(st)}, nil
}

// ListStaff returns the active staff members of a garage.
func (s *Server) ListStaff(ctx context.Context, req *tenantv1.ListStaffRequest) (*tenantv1.ListStaffResponse, error) {
	list, err := s.staff.List(ctx, req.GetGarageId())
	if err != nil {
		return nil, toGRPCError(err)
	}

	resp := &tenantv1.ListStaffResponse{}
	for _, st := range list {
		resp.Staff = append(resp.Staff, staffToProto(st))
	}

	return resp, nil
}

func garageToProto(g *domain.Garage) *tenantv1.Garage {
	return &tenantv1.Garage{
		Id:        g.ID,
		AccountId: g.AccountID,
		Name:      g.Name,
		TaxCode:   g.TaxCode,
		Address:   g.Address,
		LogoUrl:   g.LogoURL,
		Timezone:  g.Timezone,
		Currency:  g.Currency,
		Status:    garageStatusToProto(g.Status),
		CreatedAt: timeToProto(g.CreatedAt),
		UpdatedAt: timeToProto(g.UpdatedAt),
	}
}

func staffToProto(s *domain.GarageStaff) *tenantv1.GarageStaff {
	return &tenantv1.GarageStaff{
		Id:        s.ID,
		GarageId:  s.GarageID,
		AccountId: s.AccountID,
		Role:      roleToProto(s.Role),
		Status:    staffStatusToProto(s.Status),
		CreatedAt: timeToProto(s.CreatedAt),
		UpdatedAt: timeToProto(s.UpdatedAt),
	}
}

func garageStatusToProto(s domain.GarageStatus) tenantv1.GarageStatus {
	switch s {
	case domain.GarageStatusActive:
		return tenantv1.GarageStatus_GARAGE_STATUS_ACTIVE
	case domain.GarageStatusInactive:
		return tenantv1.GarageStatus_GARAGE_STATUS_INACTIVE
	case domain.GarageStatusSuspended:
		return tenantv1.GarageStatus_GARAGE_STATUS_SUSPENDED
	default:
		return tenantv1.GarageStatus_GARAGE_STATUS_UNSPECIFIED
	}
}

func staffStatusToProto(s domain.StaffStatus) tenantv1.StaffStatus {
	switch s {
	case domain.StaffStatusActive:
		return tenantv1.StaffStatus_STAFF_STATUS_ACTIVE
	case domain.StaffStatusInactive:
		return tenantv1.StaffStatus_STAFF_STATUS_INACTIVE
	case domain.StaffStatusSuspended:
		return tenantv1.StaffStatus_STAFF_STATUS_SUSPENDED
	default:
		return tenantv1.StaffStatus_STAFF_STATUS_UNSPECIFIED
	}
}

func roleToProto(r domain.Role) tenantv1.Role {
	switch r {
	case domain.RoleOwner:
		return tenantv1.Role_ROLE_OWNER
	case domain.RoleAccountant:
		return tenantv1.Role_ROLE_ACCOUNTANT
	case domain.RoleTechnician:
		return tenantv1.Role_ROLE_TECHNICIAN
	case domain.RoleServiceAdvisor:
		return tenantv1.Role_ROLE_SERVICE_ADVISOR
	case domain.RoleInventoryStaff:
		return tenantv1.Role_ROLE_INVENTORY_STAFF
	default:
		return tenantv1.Role_ROLE_UNSPECIFIED
	}
}

func roleFromProto(r tenantv1.Role) domain.Role {
	switch r {
	case tenantv1.Role_ROLE_UNSPECIFIED:
		return domain.Role("")
	case tenantv1.Role_ROLE_OWNER:
		return domain.RoleOwner
	case tenantv1.Role_ROLE_ACCOUNTANT:
		return domain.RoleAccountant
	case tenantv1.Role_ROLE_TECHNICIAN:
		return domain.RoleTechnician
	case tenantv1.Role_ROLE_SERVICE_ADVISOR:
		return domain.RoleServiceAdvisor
	case tenantv1.Role_ROLE_INVENTORY_STAFF:
		return domain.RoleInventoryStaff
	default:
		return domain.Role("")
	}
}

func timeToProto(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}

	return timestamppb.New(t)
}

func toGRPCError(err error) error {
	appErr, ok := apperror.As(err)
	if !ok {
		return status.Error(codes.Internal, "internal error")
	}

	switch appErr.Code {
	case apperror.CodeNotFound:
		return status.Error(codes.NotFound, appErr.Message)
	case apperror.CodeAlreadyExists:
		return status.Error(codes.AlreadyExists, appErr.Message)
	case apperror.CodeInvalidArgument:
		return status.Error(codes.InvalidArgument, appErr.Message)
	case apperror.CodePermissionDenied:
		return status.Error(codes.PermissionDenied, appErr.Message)
	case apperror.CodeUnauthenticated:
		return status.Error(codes.Unauthenticated, appErr.Message)
	case apperror.CodeFailedPrecondition:
		return status.Error(codes.FailedPrecondition, appErr.Message)
	case apperror.CodeInternal:
		return status.Error(codes.Internal, appErr.Message)
	default:
		return status.Error(codes.Internal, "internal error")
	}
}
