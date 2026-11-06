// Package handler exposes the data requests as gRPC services, served as REST
// through grpc-gateway (FR-CUS-05).
package handler

import (
	"context"

	commonv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/common/v1"
	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"

	"github.com/viettechno/vietgara-backend/internal/datarequest/domain"
	"github.com/viettechno/vietgara-backend/internal/datarequest/usecase"
	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/platform/access"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
	"github.com/viettechno/vietgara-backend/pkg/converter"
)

// Server implements customerv1.DataRequestServiceServer.
type Server struct {
	customerv1.UnimplementedDataRequestServiceServer

	uc *usecase.StaffUseCase
}

// NewServer wires the handler to its use case.
func NewServer(uc *usecase.StaffUseCase) *Server { return &Server{uc: uc} }

// Policies declares the permission every RPC requires in its garage: reading
// needs customer.read, recording and completing customer.write.
func Policies() middleware.Policies {
	read, write := access.Requires(access.CustomerRead), access.Requires(access.CustomerWrite)

	return middleware.Policies{
		customerv1.DataRequestService_ListDataRequests_FullMethodName:    middleware.GarageMethod(read),
		customerv1.DataRequestService_GetDataRequest_FullMethodName:      middleware.GarageMethod(read),
		customerv1.DataRequestService_CreateDataRequest_FullMethodName:   middleware.GarageMethod(write),
		customerv1.DataRequestService_CompleteDataRequest_FullMethodName: middleware.GarageMethod(write),
	}
}

// CreateDataRequest records a request made in person and acts on it.
func (s *Server) CreateDataRequest(ctx context.Context, req *customerv1.CreateDataRequestRequest) (*customerv1.DataRequest, error) {
	r, err := s.uc.Create(ctx, usecase.CreateRequest{
		GarageID: req.GetGarageId(), CustomerID: req.GetCustomerId(), ActorID: middleware.UserID(ctx),
		Type: typeFromProto(req.GetType()), Details: req.GetDetails(),
	})
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return requestToProto(r), nil
}

// ListDataRequests lists the garage's requests.
func (s *Server) ListDataRequests(ctx context.Context, req *customerv1.ListDataRequestsRequest) (*customerv1.ListDataRequestsResponse, error) {
	page, err := s.uc.List(ctx, usecase.ListRequest{
		GarageID: req.GetGarageId(), CustomerID: req.GetCustomerId(), Type: typeFromProto(req.GetType()),
		Status: statusFromProto(req.GetStatus()), Page: int(req.GetPage()), PageSize: int(req.GetPageSize()),
	})
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	resp := &customerv1.ListDataRequestsResponse{
		Data:       make([]*customerv1.DataRequest, 0, len(page.Items)),
		Pagination: &commonv1.Pagination{Page: toInt32(page.Page), PageSize: toInt32(page.PageSize), Total: toInt32(page.Total)},
	}

	for _, r := range page.Items {
		resp.Data = append(resp.Data, requestToProto(r))
	}

	return resp, nil
}

// GetDataRequest returns one request.
func (s *Server) GetDataRequest(ctx context.Context, req *customerv1.GetDataRequestRequest) (*customerv1.DataRequest, error) {
	r, err := s.uc.Get(ctx, req.GetGarageId(), req.GetRequestId())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return requestToProto(r), nil
}

// CompleteDataRequest completes or declines a waiting correction.
func (s *Server) CompleteDataRequest(ctx context.Context, req *customerv1.CompleteDataRequestRequest) (*customerv1.DataRequest, error) {
	r, err := s.uc.Complete(ctx, usecase.CompleteRequest{
		GarageID: req.GetGarageId(), RequestID: req.GetRequestId(), ActorID: middleware.UserID(ctx),
		Outcome: req.GetOutcome(), Declined: req.GetDeclined(),
	})
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return requestToProto(r), nil
}

func requestToProto(r *domain.Request) *customerv1.DataRequest {
	return &customerv1.DataRequest{
		Id: r.ID, GarageId: r.GarageID, CustomerId: r.CustomerID, Type: typeToProto(r.Type), Status: statusToProto(r.Status),
		Source: sourceToProto(r.Source), Details: r.Details, Outcome: r.Outcome, ExportJson: r.ExportJSON,
		RequestedAt: converter.TimeToProto(r.RequestedAt), CompletedAt: converter.TimePtrToProto(r.CompletedAt),
		CompletedByName: r.CompletedByName,
	}
}

func typeToProto(t domain.Type) customerv1.DataRequestType {
	switch t {
	case domain.TypeView:
		return customerv1.DataRequestType_DATA_REQUEST_TYPE_VIEW
	case domain.TypeCorrect:
		return customerv1.DataRequestType_DATA_REQUEST_TYPE_CORRECT
	case domain.TypeDelete:
		return customerv1.DataRequestType_DATA_REQUEST_TYPE_DELETE
	default:
		return customerv1.DataRequestType_DATA_REQUEST_TYPE_UNSPECIFIED
	}
}

func typeFromProto(t customerv1.DataRequestType) domain.Type {
	switch t {
	case customerv1.DataRequestType_DATA_REQUEST_TYPE_VIEW:
		return domain.TypeView
	case customerv1.DataRequestType_DATA_REQUEST_TYPE_CORRECT:
		return domain.TypeCorrect
	case customerv1.DataRequestType_DATA_REQUEST_TYPE_DELETE:
		return domain.TypeDelete
	case customerv1.DataRequestType_DATA_REQUEST_TYPE_UNSPECIFIED:
		return ""
	default:
		return ""
	}
}

func statusToProto(s domain.Status) customerv1.DataRequestStatus {
	switch s {
	case domain.StatusPending:
		return customerv1.DataRequestStatus_DATA_REQUEST_STATUS_PENDING
	case domain.StatusCompleted:
		return customerv1.DataRequestStatus_DATA_REQUEST_STATUS_COMPLETED
	case domain.StatusRejected:
		return customerv1.DataRequestStatus_DATA_REQUEST_STATUS_REJECTED
	default:
		return customerv1.DataRequestStatus_DATA_REQUEST_STATUS_UNSPECIFIED
	}
}

func statusFromProto(s customerv1.DataRequestStatus) domain.Status {
	switch s {
	case customerv1.DataRequestStatus_DATA_REQUEST_STATUS_PENDING:
		return domain.StatusPending
	case customerv1.DataRequestStatus_DATA_REQUEST_STATUS_COMPLETED:
		return domain.StatusCompleted
	case customerv1.DataRequestStatus_DATA_REQUEST_STATUS_REJECTED:
		return domain.StatusRejected
	case customerv1.DataRequestStatus_DATA_REQUEST_STATUS_UNSPECIFIED:
		return ""
	default:
		return ""
	}
}

func sourceToProto(s domain.Source) customerv1.DataRequestSource {
	switch s {
	case domain.SourceStaff:
		return customerv1.DataRequestSource_DATA_REQUEST_SOURCE_STAFF
	case domain.SourcePublic:
		return customerv1.DataRequestSource_DATA_REQUEST_SOURCE_PUBLIC
	default:
		return customerv1.DataRequestSource_DATA_REQUEST_SOURCE_UNSPECIFIED
	}
}

func toInt32(n int) int32 { return int32(min(max(n, 0), 1<<31-1)) } //nolint:gosec // clamped
