// Package handler exposes the Reporting module (FRD 3.15) as a gRPC
// service, served as REST through grpc-gateway.
package handler

import (
	"context"

	reportingv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/reporting/v1"

	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/platform/access"
	"github.com/viettechno/vietgara-backend/internal/reporting/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// Server implements reportingv1.ReportingServiceServer.
type Server struct {
	reportingv1.UnimplementedReportingServiceServer

	uc *usecase.ReportingUseCase
}

// NewServer wires the handler to its use case.
func NewServer(uc *usecase.ReportingUseCase) *Server {
	return &Server{uc: uc}
}

// Policies declares the permission the RPC requires in its garage.
func Policies() middleware.Policies {
	read := access.Requires(access.ReportRead)

	return middleware.Policies{
		reportingv1.ReportingService_GetRevenueReport_FullMethodName: middleware.GarageMethod(read),
	}
}

// GetRevenueReport serves the Web Admin Dashboard's revenue chart and KPI
// card (UI Design Spec 3.1, FR-RPT-01).
func (s *Server) GetRevenueReport(ctx context.Context, req *reportingv1.GetRevenueReportRequest) (*reportingv1.GetRevenueReportResponse, error) {
	in, err := requestFromProto(req)
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	report, err := s.uc.GetRevenueReport(ctx, in)
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return reportToProto(report), nil
}
