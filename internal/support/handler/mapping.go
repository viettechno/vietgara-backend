package handler

import (
	commonv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/common/v1"
	supportv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/support/v1"

	"github.com/viettechno/vietgara-backend/internal/support/domain"
	"github.com/viettechno/vietgara-backend/pkg/converter"
)

var statusToProto = map[domain.Status]supportv1.SupportGrantStatus{
	domain.StatusPending: supportv1.SupportGrantStatus_SUPPORT_GRANT_STATUS_PENDING,
	domain.StatusActive:  supportv1.SupportGrantStatus_SUPPORT_GRANT_STATUS_ACTIVE,
	domain.StatusExpired: supportv1.SupportGrantStatus_SUPPORT_GRANT_STATUS_EXPIRED,
	domain.StatusRevoked: supportv1.SupportGrantStatus_SUPPORT_GRANT_STATUS_REVOKED,
}

// statusFromProto maps a filter; unspecified means every status.
func statusFromProto(status supportv1.SupportGrantStatus) domain.Status {
	for k, v := range statusToProto {
		if v == status {
			return k
		}
	}

	return ""
}

func (s *Server) grantToProto(g *domain.Grant) *supportv1.SupportGrant {
	return &supportv1.SupportGrant{
		Id: g.ID, GarageId: g.GarageID, GarageName: g.GarageName,
		StaffUserId: g.StaffUserID, StaffName: g.StaffName, StaffEmail: g.StaffEmail,
		Reason: g.Reason, Status: statusToProto[s.uc.GrantStatus(g)],
		TtlHours:    int32(g.TTL.Hours()),
		RequestedAt: converter.TimeToProto(g.RequestedAt), ApprovedAt: converter.TimePtrToProto(g.ApprovedAt),
		ApprovedByName: g.ApproverName, ExpiresAt: converter.TimePtrToProto(g.ExpiresAt),
		RevokedAt: converter.TimePtrToProto(g.RevokedAt), GrantedByPlatformAdmin: g.GrantedByPlatformAdmin,
	}
}

func (s *Server) grantsToProto(grants []*domain.Grant) []*supportv1.SupportGrant {
	return mapAll(grants, s.grantToProto)
}

// mapAll converts every item; an empty list is an empty array, not null.
func mapAll[T, P any](items []T, convert func(T) P) []P {
	out := make([]P, 0, len(items))
	for _, item := range items {
		out = append(out, convert(item))
	}

	return out
}

func eventToProto(e *domain.AccessEvent) *supportv1.SupportAccessEvent {
	return &supportv1.SupportAccessEvent{
		Id: e.ID, GrantId: e.GrantID, StaffName: e.StaffName, EntityType: e.EntityType, EntityId: e.EntityID,
		Method: e.Method, CreatedAt: converter.TimeToProto(e.CreatedAt),
	}
}

func directoryToProto(e *domain.DirectoryEntry) *supportv1.GarageDirectoryEntry {
	return &supportv1.GarageDirectoryEntry{
		GarageId: e.GarageID, Name: e.Name, OwnerName: e.OwnerName, OwnerEmail: e.OwnerEmail, Active: e.Active,
	}
}

func outboxToProto(e *domain.OutboxEvent) *supportv1.SupportOutboxEvent {
	return &supportv1.SupportOutboxEvent{
		Id: e.ID, EventType: e.EventType, Status: e.Status, Attempts: int32(e.Attempts), //nolint:gosec // retry count
		LastError: e.LastError, CreatedAt: converter.TimeToProto(e.CreatedAt), PublishedAt: converter.TimePtrToProto(e.PublishedAt),
	}
}

func notificationToProto(n *domain.Notification) *supportv1.SupportNotification {
	return &supportv1.SupportNotification{
		Id: n.ID, Channel: n.Channel, Template: n.Template, Recipient: n.Recipient, Status: n.Status,
		Error: n.Error, CreatedAt: converter.TimeToProto(n.CreatedAt),
	}
}

func paginationOf(total int, page domain.Page) *commonv1.Pagination {
	return &commonv1.Pagination{Total: int32(total), Page: int32(page.Page), PageSize: int32(page.PageSize)} //nolint:gosec // bounded
}
