package handler

import (
	"context"

	identityv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/identity/v1"

	"github.com/viettechno/vietgara-backend/internal/identity/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// ReasonGoogleTokenInvalid is the error code of a Google ID token that is
// malformed, forged, expired or issued to another app.
const ReasonGoogleTokenInvalid = "GOOGLE_TOKEN_INVALID"

// SignInWithGoogleIdToken signs in with a Google ID token obtained natively
// by a mobile app (FR-IAM-01, FR-CAPP-01), with the same account rules as
// the web's Google redirect sign-in (usecase.GoogleLogin).
func (s *Server) SignInWithGoogleIdToken( //nolint:revive // named by the generated proto service
	ctx context.Context, req *identityv1.SignInWithGoogleIdTokenRequest,
) (*identityv1.SignInWithGoogleIdTokenResponse, error) {
	if s.googleID == nil || !s.googleID.Configured() {
		return nil, apperror.ToGRPCError(apperror.New(apperror.CodeFailedPrecondition,
			"Google sign-in is not configured for the mobile apps").WithReason(googleErrorNotConfigured))
	}

	claims, err := s.googleID.Verify(ctx, req.GetIdToken())
	if err != nil {
		return nil, apperror.ToGRPCError(apperror.Wrap(apperror.CodeUnauthenticated,
			"the Google sign-in could not be verified", err).WithReason(ReasonGoogleTokenInvalid))
	}

	session, err := s.uc.GoogleLogin(ctx, usecase.GoogleProfile{
		Subject:       claims.Subject,
		Email:         claims.Email,
		EmailVerified: claims.EmailVerified,
		FullName:      claims.Name,
		Locale:        localeFromProto(req.GetLocale()),
	})
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &identityv1.SignInWithGoogleIdTokenResponse{Session: s.sessionToProto(session)}, nil
}
