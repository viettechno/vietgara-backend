package handler

import (
	"context"
	"errors"

	commonv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/common/v1"
	notificationv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/notification/v1"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/notification/domain"
	"github.com/viettechno/vietgara-backend/internal/notification/usecase"
	"github.com/viettechno/vietgara-backend/internal/platform/access"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

const (
	defaultPageSize = 20
	maxPageSize     = 100
)

// Server implements notificationv1.NotificationServiceServer: the garage's
// e-mail settings and delivery log (Release 10.1).
type Server struct {
	notificationv1.UnimplementedNotificationServiceServer

	uc *usecase.NotificationUseCase
}

// NewServer wires the handler to its use case.
func NewServer(uc *usecase.NotificationUseCase) *Server { return &Server{uc: uc} }

// Policies is what each RPC requires of its caller: any member reads the
// settings, the garage permission changes them and reads the log, which
// holds customers' e-mail addresses.
func Policies() middleware.Policies {
	return middleware.Policies{
		notificationv1.NotificationService_GetNotificationSettings_FullMethodName:    middleware.GarageMethod(access.Member()),
		notificationv1.NotificationService_UpdateNotificationSettings_FullMethodName: middleware.GarageMethod(access.Requires(access.GarageWrite)),
		notificationv1.NotificationService_ListNotificationLog_FullMethodName:        middleware.GarageMethod(access.Requires(access.GarageWrite)),
	}
}

// GetNotificationSettings returns the garage's choices for every type.
func (s *Server) GetNotificationSettings(
	ctx context.Context, req *notificationv1.GetNotificationSettingsRequest,
) (*notificationv1.NotificationSettings, error) {
	settings, err := s.uc.GetSettings(ctx, req.GetGarageId())
	if err != nil {
		return nil, apperror.ToGRPCError(apperror.Internal("get notification settings", err))
	}

	return settingsToProto(settings), nil
}

// UpdateNotificationSettings replaces the garage's choices.
func (s *Server) UpdateNotificationSettings(
	ctx context.Context, req *notificationv1.UpdateNotificationSettingsRequest,
) (*notificationv1.NotificationSettings, error) {
	var (
		order []domain.Type
		on    = map[domain.Type]bool{}
	)

	for _, entry := range req.GetTypes() {
		t := typeFromProto[entry.GetType()]
		order = append(order, t)
		on[t] = entry.GetEnabled()
	}

	settings, err := s.uc.UpdateSettings(ctx, req.GetGarageId(), req.GetSenderName(), on, order)
	if err != nil {
		return nil, apperror.ToGRPCError(settingsError(err))
	}

	return settingsToProto(settings), nil
}

func settingsError(err error) error {
	switch {
	case errors.Is(err, usecase.ErrInvalidSenderName):
		return apperror.Validation("senderName", apperror.IssueTooLong, err.Error())
	case errors.Is(err, usecase.ErrInvalidType):
		return apperror.Validation("types", apperror.IssueInvalid, err.Error())
	default:
		return apperror.Internal("update notification settings", err)
	}
}

// ListNotificationLog returns a page of the garage's sends, newest first.
func (s *Server) ListNotificationLog(
	ctx context.Context, req *notificationv1.ListNotificationLogRequest,
) (*notificationv1.ListNotificationLogResponse, error) {
	page, size := pageOf(req.GetPage(), req.GetPageSize())
	filter := usecase.LogFilter{Type: typeFromProto[req.GetType()], Status: statusFromProto[req.GetStatus()]}

	logs, total, err := s.uc.ListLog(ctx, req.GetGarageId(), filter, page, size)
	if err != nil {
		return nil, apperror.ToGRPCError(apperror.Internal("list notification log", err))
	}

	data := make([]*notificationv1.NotificationLogEntry, 0, len(logs))
	for i := range logs {
		data = append(data, logToProto(&logs[i]))
	}

	return &notificationv1.ListNotificationLogResponse{
		Data:       data,
		Pagination: &commonv1.Pagination{Total: int32(total), Page: int32(page), PageSize: int32(size)}, //nolint:gosec // bounded by the page size and the row count
	}, nil
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

func logToProto(l *domain.Log) *notificationv1.NotificationLogEntry {
	return &notificationv1.NotificationLogEntry{
		Id: l.ID, Type: typeToProto[l.Type], Recipient: l.Recipient, CustomerId: l.CustomerID,
		EntityType: l.EntityType, EntityId: l.EntityID, Status: statusToProto[l.Status], Reason: reasonToProto[l.SkipReason],
		Error: l.Error, CreatedAt: timestamppb.New(l.CreatedAt),
	}
}

func settingsToProto(s domain.Settings) *notificationv1.NotificationSettings {
	types := make([]*notificationv1.NotificationTypeSetting, 0, len(domain.AllTypes))

	for _, t := range domain.AllTypes {
		audience := notificationv1.NotificationAudience_NOTIFICATION_AUDIENCE_STAFF
		if t.Audience() == domain.AudienceCustomer {
			audience = notificationv1.NotificationAudience_NOTIFICATION_AUDIENCE_CUSTOMER
		}

		types = append(types, &notificationv1.NotificationTypeSetting{
			Type: typeToProto[t], Enabled: s.Enabled(t), Audience: audience, Essential: t.Essential(),
		})
	}

	return &notificationv1.NotificationSettings{SenderName: s.SenderName, Types: types}
}
