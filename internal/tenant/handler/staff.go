package handler

import (
	"context"

	"github.com/viettechno/vietgara-backend/internal/tenant/domain"
	"github.com/viettechno/vietgara-backend/internal/tenant/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
	"github.com/viettechno/vietgara-backend/pkg/converter"
	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"
)

// AddStaff adds the given account as a staff member of a garage.
func (s *Server) AddStaff(ctx context.Context, req *tenantv1.AddStaffRequest) (*tenantv1.AddStaffResponse, error) {
	st, err := s.staff.Add(ctx, usecase.AddStaffRequest{
		GarageID:  req.GetGarageId(),
		AccountID: req.GetAccountId(),
		Role:      roleFromProto(req.GetRole()),
	})
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &tenantv1.AddStaffResponse{Staff: staffToProto(st)}, nil
}

// ListStaff returns the active staff members of a garage.
func (s *Server) ListStaff(ctx context.Context, req *tenantv1.ListStaffRequest) (*tenantv1.ListStaffResponse, error) {
	list, err := s.staff.List(ctx, req.GetGarageId())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	resp := &tenantv1.ListStaffResponse{}
	for _, st := range list {
		resp.Staff = append(resp.Staff, staffToProto(st))
	}

	return resp, nil
}

// UpdateStaff changes the role/status of a staff membership; absent fields
// keep their current value.
func (s *Server) UpdateStaff(ctx context.Context, req *tenantv1.UpdateStaffRequest) (*tenantv1.UpdateStaffResponse, error) {
	st, err := s.staff.Update(ctx, usecase.UpdateStaffRequest{
		StaffID:  req.GetStaffId(),
		GarageID: req.GetGarageId(),
		Role:     rolePtrFromProto(req.Role),
		Status:   staffStatusPtrFromProto(req.Status),
	})
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &tenantv1.UpdateStaffResponse{Staff: staffToProto(st)}, nil
}

// RemoveStaff deactivates a staff membership (sets its status to inactive).
func (s *Server) RemoveStaff(ctx context.Context, req *tenantv1.RemoveStaffRequest) (*tenantv1.RemoveStaffResponse, error) {
	st, err := s.staff.Deactivate(ctx, req.GetGarageId(), req.GetStaffId())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &tenantv1.RemoveStaffResponse{GarageId: st.GarageID, StaffId: st.ID}, nil
}

func staffToProto(s *domain.GarageStaff) *tenantv1.GarageStaff {
	return &tenantv1.GarageStaff{
		Id:        s.ID,
		GarageId:  s.GarageID,
		AccountId: s.AccountID,
		Role:      roleToProto(s.Role),
		Status:    staffStatusToProto(s.Status),
		CreatedAt: converter.TimeToProto(s.CreatedAt),
		UpdatedAt: converter.TimeToProto(s.UpdatedAt),
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

// rolePtrFromProto maps an optional role field to a domain pointer; nil (or
// UNSPECIFIED) means "keep the current role".
func rolePtrFromProto(r *tenantv1.Role) *domain.Role {
	if r == nil || *r == tenantv1.Role_ROLE_UNSPECIFIED {
		return nil
	}

	role := roleFromProto(*r)

	return &role
}

// staffStatusPtrFromProto maps an optional status field to a domain pointer;
// nil (or UNSPECIFIED) means "keep the current status".
func staffStatusPtrFromProto(s *tenantv1.StaffStatus) *domain.StaffStatus {
	if s == nil {
		return nil
	}

	var status domain.StaffStatus

	switch *s {
	case tenantv1.StaffStatus_STAFF_STATUS_ACTIVE:
		status = domain.StaffStatusActive
	case tenantv1.StaffStatus_STAFF_STATUS_INACTIVE:
		status = domain.StaffStatusInactive
	case tenantv1.StaffStatus_STAFF_STATUS_SUSPENDED:
		status = domain.StaffStatusSuspended
	case tenantv1.StaffStatus_STAFF_STATUS_UNSPECIFIED:
		return nil
	}

	return &status
}
