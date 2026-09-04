package handler

import (
	"github.com/viettechno/vietgara-backend/internal/identity/domain"
	"github.com/viettechno/vietgara-backend/pkg/apperror"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	identityv1 "github.com/viettechno/vietgara-proto/gen/go/vietgara/identity/v1"
)

// authProviderFromProto maps the proto AuthProvider enum to the domain
// value, rejecting unknown values so an unauthenticated user cannot force
// an arbitrary provider through.
func authProviderFromProto(p identityv1.AuthProvider) (domain.AuthProvider, error) {
	switch p {
	case identityv1.AuthProvider_AUTH_PROVIDER_EMAIL:
		return domain.AuthProviderEmail, nil
	case identityv1.AuthProvider_AUTH_PROVIDER_PHONE:
		return domain.AuthProviderPhone, nil
	case identityv1.AuthProvider_AUTH_PROVIDER_GOOGLE:
		return domain.AuthProviderGoogle, nil
	case identityv1.AuthProvider_AUTH_PROVIDER_FACEBOOK:
		return domain.AuthProviderFacebook, nil
	case identityv1.AuthProvider_AUTH_PROVIDER_UNSPECIFIED:
		return "", status.Error(codes.InvalidArgument, "auth provider is required")
	default:
		return "", status.Error(codes.InvalidArgument, "unsupported auth provider")
	}
}

// toGRPCError translates *apperror.AppError into a gRPC status error,
// mirroring the Tenant module's handler so every service maps error codes
// consistently. Unknown errors collapse to codes.Internal to avoid leaking
// internals to clients.
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
