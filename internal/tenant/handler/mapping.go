package handler

import (
	"google.golang.org/genproto/googleapis/type/dayofweek"

	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"

	"github.com/viettechno/vietgara-backend/internal/platform/access"
	"github.com/viettechno/vietgara-backend/internal/tenant/domain"
	"github.com/viettechno/vietgara-backend/internal/tenant/usecase"
	"github.com/viettechno/vietgara-backend/pkg/converter"
)

func garageToProto(g *usecase.GarageWithAccess) *tenantv1.Garage {
	hours := make([]*tenantv1.DailyHours, 0, len(g.Garage.OperatingHours))
	for _, h := range g.Garage.OperatingHours {
		hours = append(hours, &tenantv1.DailyHours{Day: dayofweek.DayOfWeek(h.Day), Closed: h.Closed, Open: h.Open, Close: h.Close}) //nolint:gosec // G115: day is validated to 1..7
	}

	out := &tenantv1.Garage{
		Id:             g.Garage.ID,
		OwnerAccountId: g.Garage.OwnerAccountID,
		Name:           g.Garage.Name,
		TaxCode:        g.Garage.TaxCode,
		Address:        g.Garage.Address,
		Phone:          g.Garage.Phone,
		Email:          g.Garage.Email,
		LogoUrl:        g.Garage.LogoURL,
		Timezone:       g.Garage.Timezone,
		Currency:       g.Garage.Currency,
		Status:         garageStatusToProto(g.Garage.Status),
		OperatingHours: hours,
		Numbering: &tenantv1.NumberingTemplates{
			Quote: g.Garage.QuoteNumberTemplate, RepairOrder: g.Garage.RepairOrderNumberTemplate,
		},
		CreatedAt: converter.TimeToProto(g.Garage.CreatedAt),
		UpdatedAt: converter.TimeToProto(g.Garage.UpdatedAt),
	}

	if g.Access != nil {
		out.Access = &tenantv1.GarageAccess{Owner: g.Access.Owner, Permissions: access.ListToProto(g.Access.Effective())}
	}

	return out
}

func garageStatusToProto(s domain.GarageStatus) tenantv1.GarageStatus {
	if s == domain.GarageStatusInactive {
		return tenantv1.GarageStatus_GARAGE_STATUS_INACTIVE
	}

	return tenantv1.GarageStatus_GARAGE_STATUS_ACTIVE
}

func garageStatusFromProto(s tenantv1.GarageStatus) domain.GarageStatus {
	switch s {
	case tenantv1.GarageStatus_GARAGE_STATUS_ACTIVE:
		return domain.GarageStatusActive
	case tenantv1.GarageStatus_GARAGE_STATUS_INACTIVE:
		return domain.GarageStatusInactive
	case tenantv1.GarageStatus_GARAGE_STATUS_UNSPECIFIED:
		return ""
	default:
		return ""
	}
}

func hoursFromProto(in []*tenantv1.DailyHours) []domain.DailyHours {
	out := make([]domain.DailyHours, 0, len(in))
	for _, h := range in {
		out = append(out, domain.DailyHours{Day: int(h.GetDay()), Closed: h.GetClosed(), Open: h.GetOpen(), Close: h.GetClose()})
	}

	return out
}

func staffToProto(s *domain.Staff) *tenantv1.Staff {
	perms := s.Permissions
	if s.Owner {
		perms = access.All()
	}

	return &tenantv1.Staff{
		Id:          s.ID,
		GarageId:    s.GarageID,
		AccountId:   s.AccountID,
		FullName:    s.FullName,
		Email:       s.Email,
		Phone:       s.Phone,
		Owner:       s.Owner,
		Status:      staffStatusToProto(s.Status),
		GroupIds:    nonNil(s.GroupIDs),
		Permissions: access.ListToProto(perms),
		CreatedAt:   converter.TimeToProto(s.CreatedAt),
		UpdatedAt:   converter.TimeToProto(s.UpdatedAt),
	}
}

func staffStatusToProto(s domain.StaffStatus) tenantv1.StaffStatus {
	if s == domain.StaffStatusInactive {
		return tenantv1.StaffStatus_STAFF_STATUS_INACTIVE
	}

	return tenantv1.StaffStatus_STAFF_STATUS_ACTIVE
}

func staffStatusFromProto(s tenantv1.StaffStatus) domain.StaffStatus {
	switch s {
	case tenantv1.StaffStatus_STAFF_STATUS_ACTIVE:
		return domain.StaffStatusActive
	case tenantv1.StaffStatus_STAFF_STATUS_INACTIVE:
		return domain.StaffStatusInactive
	case tenantv1.StaffStatus_STAFF_STATUS_UNSPECIFIED:
		return ""
	default:
		return ""
	}
}

var invitationStatuses = map[domain.InvitationStatus]tenantv1.InvitationStatus{
	domain.InvitationPending:  tenantv1.InvitationStatus_INVITATION_STATUS_PENDING,
	domain.InvitationAccepted: tenantv1.InvitationStatus_INVITATION_STATUS_ACCEPTED,
	domain.InvitationDeclined: tenantv1.InvitationStatus_INVITATION_STATUS_DECLINED,
	domain.InvitationRevoked:  tenantv1.InvitationStatus_INVITATION_STATUS_REVOKED,
	domain.InvitationExpired:  tenantv1.InvitationStatus_INVITATION_STATUS_EXPIRED,
}

func invitationStatusFromProto(s tenantv1.InvitationStatus) domain.InvitationStatus {
	for d, p := range invitationStatuses {
		if p == s {
			return d
		}
	}

	return ""
}

func invitationToProto(inv *domain.Invitation) *tenantv1.Invitation {
	out := &tenantv1.Invitation{
		Id:            inv.ID,
		GarageId:      inv.GarageID,
		GarageName:    inv.GarageName,
		AccountId:     inv.AccountID,
		FullName:      inv.FullName,
		Email:         inv.Email,
		GroupIds:      nonNil(inv.GroupIDs),
		Status:        invitationStatuses[inv.Status],
		InvitedByName: inv.InvitedByName,
		ExpiresAt:     converter.TimeToProto(inv.ExpiresAt),
		CreatedAt:     converter.TimeToProto(inv.CreatedAt),
	}

	if inv.RespondedAt != nil {
		out.RespondedAt = converter.TimeToProto(*inv.RespondedAt)
	}

	return out
}

func roleToProto(r *domain.Role) *tenantv1.Role {
	var defaults []tenantv1.Permission

	if r.DefaultKey != nil {
		if tmpl, ok := r.DefaultKey.Template(); ok {
			defaults = access.ListToProto(access.Normalize(tmpl.Permissions))
		}
	}

	return &tenantv1.Role{
		Id:          r.ID,
		Name:        r.Name,
		Description: r.Description,
		Permissions: access.ListToProto(r.Permissions),
		IsDefault:   r.DefaultKey != nil,
		GroupCount:  int32(r.GroupCount), //nolint:gosec // bounded by the number of groups
		CreatedAt:   converter.TimeToProto(r.CreatedAt),
		UpdatedAt:   converter.TimeToProto(r.UpdatedAt),

		DefaultPermissions: defaults,
	}
}

func groupToProto(g *domain.StaffGroup) *tenantv1.StaffGroup {
	return &tenantv1.StaffGroup{
		Id:          g.ID,
		GarageId:    g.GarageID,
		Name:        g.Name,
		Description: g.Description,
		RoleId:      g.RoleID,
		RoleName:    g.RoleName,
		MemberCount: int32(g.MemberCount), //nolint:gosec // bounded by the number of staff
		CreatedAt:   converter.TimeToProto(g.CreatedAt),
		UpdatedAt:   converter.TimeToProto(g.UpdatedAt),
	}
}

func permissionsFromProto(in []tenantv1.Permission) ([]access.Permission, bool) {
	out := make([]access.Permission, 0, len(in))

	for _, p := range in {
		perm, ok := access.FromProto(p)
		if !ok {
			return nil, false
		}

		out = append(out, perm)
	}

	return out, true
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}

	return s
}

// maskPaths returns the update mask paths, or every settable field when
// the mask is empty.
func maskPaths(paths []string, settable ...string) []string {
	if len(paths) == 0 {
		return settable
	}

	return paths
}
