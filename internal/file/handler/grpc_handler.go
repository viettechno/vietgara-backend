// Package handler exposes the file registry (Release 5.0) as a gRPC
// service, served as REST through grpc-gateway.
package handler

import (
	"context"

	filev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/file/v1"

	"github.com/viettechno/vietgara-backend/internal/file/domain"
	"github.com/viettechno/vietgara-backend/internal/file/usecase"
	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/platform/access"
	"github.com/viettechno/vietgara-backend/internal/platform/httpapi"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
	"github.com/viettechno/vietgara-backend/pkg/converter"
)

// Server implements filev1.FileServiceServer.
type Server struct {
	filev1.UnimplementedFileServiceServer

	uc *usecase.UseCase
}

// NewServer wires the handler to its use case.
func NewServer(uc *usecase.UseCase) *Server { return &Server{uc: uc} }

// Policies declares what every RPC requires in its garage. Any active
// member may call them; the use case then checks the permission of the
// file's kind (garage.write for a logo, ...).
func Policies() middleware.Policies {
	member := middleware.GarageMethod(access.Member())

	return middleware.Policies{
		filev1.FileService_UploadFile_FullMethodName: member,
		filev1.FileService_GetFileUrl_FullMethodName: member,
		filev1.FileService_DeleteFile_FullMethodName: member,
	}
}

func run[T any](ctx context.Context, fn func(actorID string) (T, error)) (T, error) {
	resp, err := fn(middleware.UserID(ctx))
	if err != nil {
		var zero T

		return zero, apperror.ToGRPCError(err)
	}

	return resp, nil
}

func kindToProto(k domain.Kind) filev1.FileKind {
	switch k {
	case domain.KindLogo:
		return filev1.FileKind_FILE_KIND_LOGO
	case domain.KindPartImage:
		return filev1.FileKind_FILE_KIND_PART_IMAGE
	case domain.KindPaymentEvidence, domain.KindPlatformQR:
		return filev1.FileKind_FILE_KIND_UNSPECIFIED
	default:
		return filev1.FileKind_FILE_KIND_UNSPECIFIED
	}
}

func kindFromProto(k filev1.FileKind) domain.Kind {
	switch k {
	case filev1.FileKind_FILE_KIND_LOGO:
		return domain.KindLogo
	case filev1.FileKind_FILE_KIND_PART_IMAGE:
		return domain.KindPartImage
	case filev1.FileKind_FILE_KIND_UNSPECIFIED, filev1.FileKind_FILE_KIND_PAYMENT_EVIDENCE, filev1.FileKind_FILE_KIND_PLATFORM_QR:
		return ""
	default:
		return ""
	}
}

func fileToProto(r *usecase.Result) *filev1.File {
	f := r.File

	return &filev1.File{
		Id: f.ID, GarageId: f.GarageID, Kind: kindToProto(f.Kind), EntityType: f.EntityType, EntityId: f.EntityID,
		ContentType: f.ContentType, Size: f.Size, Sha256: f.SHA256, Url: r.URL, CreatedAt: converter.TimeToProto(f.CreatedAt),
	}
}

// UploadFile registers and stores a file; answers 201.
func (s *Server) UploadFile(ctx context.Context, req *filev1.UploadFileRequest) (*filev1.UploadFileResponse, error) {
	return run(ctx, func(actorID string) (*filev1.UploadFileResponse, error) {
		result, err := s.uc.Upload(ctx, usecase.UploadRequest{
			GarageID: req.GetGarageId(), ActorID: actorID, Kind: kindFromProto(req.GetKind()),
			EntityType: req.GetEntityType(), EntityID: req.GetEntityId(),
			Content: req.GetContent(), ContentType: req.GetContentType(),
		})
		if err != nil {
			return nil, err
		}

		httpapi.Created(ctx)

		return &filev1.UploadFileResponse{File: fileToProto(result)}, nil
	})
}

// GetFileUrl returns where a file is served from.
func (s *Server) GetFileUrl( //nolint:revive // the name is set by the generated filev1.FileServiceServer
	ctx context.Context, req *filev1.GetFileUrlRequest,
) (*filev1.GetFileUrlResponse, error) {
	return run(ctx, func(string) (*filev1.GetFileUrlResponse, error) {
		url, expires, err := s.uc.URL(ctx, req.GetGarageId(), req.GetFileId())
		if err != nil {
			return nil, err
		}

		resp := &filev1.GetFileUrlResponse{Url: url}
		if !expires.IsZero() {
			resp.ExpiresAt = converter.TimeToProto(expires)
		}

		return resp, nil
	})
}

// DeleteFile deletes or deactivates a file (FR-CAT-07) and answers which;
// with dry_run it only says which.
func (s *Server) DeleteFile(ctx context.Context, req *filev1.DeleteFileRequest) (*filev1.DeleteFileResponse, error) {
	return run(ctx, func(actorID string) (*filev1.DeleteFileResponse, error) {
		outcome, err := s.uc.Delete(ctx, usecase.DeleteRequest{
			GarageID: req.GetGarageId(), FileID: req.GetFileId(), ActorID: actorID, DryRun: req.GetDryRun(),
		})
		if err != nil {
			return nil, err
		}

		return &filev1.DeleteFileResponse{Outcome: httpapi.DeleteOutcome(outcome)}, nil
	})
}
