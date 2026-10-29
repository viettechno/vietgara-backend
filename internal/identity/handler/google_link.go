package handler

import (
	"context"

	identityv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/identity/v1"

	"github.com/viettechno/vietgara-backend/internal/identity/usecase"
	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// CompleteGoogleLink links the Google account of a popup proof to the
// signed-in user (FR-IAM-06, web consoles).
func (s *Server) CompleteGoogleLink(
	ctx context.Context, req *identityv1.CompleteGoogleLinkRequest,
) (*identityv1.CompleteGoogleLinkResponse, error) {
	user, err := s.uc.LinkGoogleWithProof(ctx, middleware.UserID(ctx), req.GetPassword(), req.GetLinkProof())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &identityv1.CompleteGoogleLinkResponse{User: userToProto(user)}, nil
}

// LinkGoogle links the Google account of an ID token obtained natively to the
// signed-in user (FR-IAM-06, mobile apps).
func (s *Server) LinkGoogle(ctx context.Context, req *identityv1.LinkGoogleRequest) (*identityv1.LinkGoogleResponse, error) {
	if s.googleID == nil || !s.googleID.Configured() {
		return nil, apperror.ToGRPCError(apperror.New(apperror.CodeFailedPrecondition,
			"Google sign-in is not configured for the mobile apps").WithReason(googleErrorNotConfigured))
	}

	claims, err := s.googleID.Verify(ctx, req.GetIdToken())
	if err != nil {
		// Not a 401: the caller is signed in, and a client takes a 401 for an expired session.
		return nil, apperror.ToGRPCError(apperror.Wrap(apperror.CodeInvalidArgument,
			"the Google sign-in could not be verified", err).WithReason(ReasonGoogleTokenInvalid))
	}

	user, err := s.uc.LinkGoogle(ctx, middleware.UserID(ctx), req.GetPassword(), usecase.GoogleProfile{
		Subject: claims.Subject, Email: claims.Email, EmailVerified: claims.EmailVerified, FullName: claims.Name,
	})
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &identityv1.LinkGoogleResponse{User: userToProto(user)}, nil
}

// UnlinkGoogle removes the signed-in user's Google account and answers a new
// session: every other one is revoked (FR-IAM-06).
func (s *Server) UnlinkGoogle(ctx context.Context, req *identityv1.UnlinkGoogleRequest) (*identityv1.UnlinkGoogleResponse, error) {
	session, err := s.uc.UnlinkGoogle(ctx, middleware.UserID(ctx), req.GetPassword())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &identityv1.UnlinkGoogleResponse{Session: s.sessionToProto(session)}, nil
}
