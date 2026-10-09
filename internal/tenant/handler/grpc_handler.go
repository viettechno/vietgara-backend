// Package handler exposes the Tenant module as gRPC services (REST through
// grpc-gateway): garages, staff, invitations, roles and staff groups.
package handler

import (
	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"

	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/platform/access"
	"github.com/viettechno/vietgara-backend/internal/tenant/usecase"
)

// Server implements the Tenant module's gRPC services.
type Server struct {
	tenantv1.UnimplementedGarageServiceServer
	tenantv1.UnimplementedStaffServiceServer
	tenantv1.UnimplementedInvitationServiceServer
	tenantv1.UnimplementedRoleServiceServer
	tenantv1.UnimplementedStaffGroupServiceServer

	uc *usecase.TenantUseCase
}

// NewServer returns a Server backed by uc.
func NewServer(uc *usecase.TenantUseCase) *Server {
	return &Server{uc: uc}
}

// Policies declares what every Tenant RPC requires (see middleware.Policy).
func Policies() middleware.Policies {
	return middleware.Policies{
		tenantv1.GarageService_ListGarages_FullMethodName:      middleware.UserMethod(),
		tenantv1.GarageService_CreateGarage_FullMethodName:     middleware.UserMethod(),
		tenantv1.GarageService_GetGarage_FullMethodName:        withSkipLicense(middleware.GarageMethod(access.Member())),
		tenantv1.GarageService_UpdateGarage_FullMethodName:     withSkipLicense(middleware.GarageMethod(access.Requires(access.GarageWrite))),
		tenantv1.GarageService_UploadGarageLogo_FullMethodName: middleware.GarageMethod(access.Requires(access.GarageWrite)),
		tenantv1.GarageService_DeleteGarageLogo_FullMethodName: middleware.GarageMethod(access.Requires(access.GarageWrite)),

		tenantv1.StaffService_ListStaff_FullMethodName:            middleware.GarageMethod(access.Requires(access.StaffRead)),
		tenantv1.StaffService_UpdateStaff_FullMethodName:          middleware.GarageMethod(access.Requires(access.StaffWrite)),
		tenantv1.StaffService_DeleteStaff_FullMethodName:          middleware.GarageMethod(access.Requires(access.StaffWrite)),
		tenantv1.StaffService_LookupStaffCandidate_FullMethodName: middleware.GarageMethod(access.Requires(access.StaffWrite)),

		tenantv1.InvitationService_CreateInvitation_FullMethodName:  middleware.GarageMethod(access.Requires(access.StaffWrite)),
		tenantv1.InvitationService_ListInvitations_FullMethodName:   middleware.GarageMethod(access.Requires(access.StaffRead)),
		tenantv1.InvitationService_RevokeInvitation_FullMethodName:  middleware.GarageMethod(access.Requires(access.StaffWrite)),
		tenantv1.InvitationService_ListMyInvitations_FullMethodName: middleware.UserMethod(),
		tenantv1.InvitationService_GetMyInvitation_FullMethodName:   middleware.UserMethod(),
		tenantv1.InvitationService_AcceptInvitation_FullMethodName:  middleware.UserMethod(),
		tenantv1.InvitationService_DeclineInvitation_FullMethodName: middleware.UserMethod(),

		tenantv1.RoleService_ListPermissions_FullMethodName: middleware.UserMethod(),
		tenantv1.RoleService_ListRoles_FullMethodName:       middleware.UserMethod(),
		tenantv1.RoleService_GetRole_FullMethodName:         middleware.UserMethod(),
		tenantv1.RoleService_CreateRole_FullMethodName:      middleware.UserMethod(),
		tenantv1.RoleService_UpdateRole_FullMethodName:      middleware.UserMethod(),
		tenantv1.RoleService_DeleteRole_FullMethodName:      middleware.UserMethod(),

		tenantv1.StaffGroupService_ListStaffGroups_FullMethodName:  middleware.GarageMethod(access.Requires(access.StaffRead)),
		tenantv1.StaffGroupService_CreateStaffGroup_FullMethodName: middleware.GarageMethod(access.OwnerOnly()),
		tenantv1.StaffGroupService_UpdateStaffGroup_FullMethodName: middleware.GarageMethod(access.OwnerOnly()),
		tenantv1.StaffGroupService_DeleteStaffGroup_FullMethodName: middleware.GarageMethod(access.OwnerOnly()),
	}
}

func withSkipLicense(p middleware.Policy) middleware.Policy {
	p.SkipLicense = true

	return p
}
