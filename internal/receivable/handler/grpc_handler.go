// Package handler exposes the Receivables module (HLD module #9) as a gRPC
// service, served as REST through grpc-gateway, and registers its event
// consumers. Authorization happens in the request middleware from the
// policies below.
package handler

import (
	"context"

	commonv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/common/v1"
	receivablev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/receivable/v1"

	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/platform/access"
	"github.com/viettechno/vietgara-backend/internal/receivable/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// Server implements receivablev1.ReceivableServiceServer.
type Server struct {
	receivablev1.UnimplementedReceivableServiceServer

	uc *usecase.ReceivableUseCase
}

// NewServer wires the handler to its use case.
func NewServer(uc *usecase.ReceivableUseCase) *Server {
	return &Server{uc: uc}
}

// Policies declares the permission every RPC requires in its garage.
func Policies() middleware.Policies {
	read, write := access.Requires(access.BillingRead), access.Requires(access.BillingWrite)

	return middleware.Policies{
		receivablev1.ReceivableService_ListReceivables_FullMethodName:          middleware.GarageMethod(read),
		receivablev1.ReceivableService_GetReceivable_FullMethodName:            middleware.GarageMethod(read),
		receivablev1.ReceivableService_GetReceivableSettings_FullMethodName:    middleware.GarageMethod(read),
		receivablev1.ReceivableService_UpdateReceivableSettings_FullMethodName: middleware.GarageMethod(write),
		receivablev1.ReceivableService_GetReconciliationReport_FullMethodName:  middleware.GarageMethod(read),
	}
}

// run calls fn with the caller's user id and converts use case errors to gRPC
// status errors.
func run[T any](ctx context.Context, fn func(actorID string) (T, error)) (T, error) {
	resp, err := fn(middleware.UserID(ctx))
	if err != nil {
		var zero T

		return zero, apperror.ToGRPCError(err)
	}

	return resp, nil
}

// ListReceivables lists a garage's receivables, most overdue first (FR-REC-01).
func (s *Server) ListReceivables(ctx context.Context, req *receivablev1.ListReceivablesRequest) (*receivablev1.ListReceivablesResponse, error) {
	return run(ctx, func(string) (*receivablev1.ListReceivablesResponse, error) {
		res, err := s.uc.List(ctx, usecase.ListRequest{
			GarageID: req.GetGarageId(), CustomerID: req.GetCustomerId(), OverdueOnly: req.GetOverdueOnly(),
			IncludePaid: req.GetIncludePaid(), Query: req.GetQuery(),
			PageRequest: usecase.PageRequest{Page: int(req.GetPage()), PageSize: int(req.GetPageSize())},
		})
		if err != nil {
			return nil, err
		}

		resp := &receivablev1.ListReceivablesResponse{
			Data:       make([]*receivablev1.Receivable, 0, len(res.Items)),
			Pagination: &commonv1.Pagination{Total: toInt32(res.Total), Page: toInt32(res.Page), PageSize: toInt32(res.PageSize)},
			Summary: &receivablev1.ReceivablesSummary{
				OpenCount: toInt32(res.Summary.OpenCount), OutstandingAmount: res.Summary.OutstandingAmount,
				OverdueCount: toInt32(res.Summary.OverdueCount), OverdueAmount: res.Summary.OverdueAmount,
			},
		}
		for _, item := range res.Items {
			resp.Data = append(resp.Data, receivableToProto(item))
		}

		return resp, nil
	})
}

// GetReceivable returns one receivable with its ledger and reminders.
func (s *Server) GetReceivable(ctx context.Context, req *receivablev1.GetReceivableRequest) (*receivablev1.Receivable, error) {
	return run(ctx, func(string) (*receivablev1.Receivable, error) {
		item, err := s.uc.Get(ctx, req.GetGarageId(), req.GetReceivableId())
		if err != nil {
			return nil, err
		}

		out := receivableToProto(item)
		out.Entries, out.Reminders = entriesToProto(item.Receivable.Entries), remindersToProto(item.Receivable.Reminders)

		return out, nil
	})
}

// GetReceivableSettings returns the payment terms and reminder schedule.
func (s *Server) GetReceivableSettings(ctx context.Context, req *receivablev1.GetReceivableSettingsRequest) (*receivablev1.ReceivableSettings, error) {
	return run(ctx, func(string) (*receivablev1.ReceivableSettings, error) {
		settings, err := s.uc.GetSettings(ctx, req.GetGarageId())
		if err != nil {
			return nil, err
		}

		return settingsToProto(settings), nil
	})
}

// UpdateReceivableSettings replaces the payment terms and reminder schedule.
func (s *Server) UpdateReceivableSettings(ctx context.Context, req *receivablev1.UpdateReceivableSettingsRequest) (*receivablev1.ReceivableSettings, error) {
	return run(ctx, func(actorID string) (*receivablev1.ReceivableSettings, error) {
		in := req.GetSettings()

		update := usecase.UpdateSettingsRequest{GarageID: req.GetGarageId(), ActorID: actorID}
		if in != nil {
			update.ReminderOffsetDays = offsetsFromProto(in.GetReminderOffsetDays())

			if in.PaymentTermsDays != nil {
				days := int(in.GetPaymentTermsDays())
				update.PaymentTermsDays = &days
			}

			if in.RemindersEnabled != nil {
				enabled := in.GetRemindersEnabled()
				update.RemindersEnabled = &enabled
			}
		}

		settings, err := s.uc.UpdateSettings(ctx, update)
		if err != nil {
			return nil, err
		}

		return settingsToProto(settings), nil
	})
}

// GetReconciliationReport reconciles a period (FR-REC-04).
func (s *Server) GetReconciliationReport(ctx context.Context, req *receivablev1.GetReconciliationReportRequest) (*receivablev1.ReconciliationReport, error) {
	return run(ctx, func(string) (*receivablev1.ReconciliationReport, error) {
		rep, err := s.uc.Reconciliation(ctx, req.GetGarageId(), req.GetFrom(), req.GetTo())
		if err != nil {
			return nil, err
		}

		return reconciliationToProto(rep), nil
	})
}
