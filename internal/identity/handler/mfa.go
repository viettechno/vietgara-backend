package handler

import (
	"context"

	"google.golang.org/protobuf/types/known/timestamppb"

	identityv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/identity/v1"

	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// VerifyMfaChallenge completes a sign-in that answered mfa_required
// (FR-IAM-09).
func (s *Server) VerifyMfaChallenge(ctx context.Context, req *identityv1.VerifyMfaChallengeRequest) (*identityv1.VerifyMfaChallengeResponse, error) {
	session, err := s.uc.VerifyMFAChallenge(ctx, req.GetChallengeToken(), req.GetCode())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &identityv1.VerifyMfaChallengeResponse{Session: s.sessionToProto(session)}, nil
}

// GetMfaStatus reports whether the signed-in user's second factor is on.
func (s *Server) GetMfaStatus(ctx context.Context, _ *identityv1.GetMfaStatusRequest) (*identityv1.GetMfaStatusResponse, error) {
	status, err := s.uc.GetMFAStatus(ctx, middleware.UserID(ctx))
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	resp := &identityv1.GetMfaStatusResponse{Enabled: status.Enabled, RecoveryCodesRemaining: int32(status.RecoveryCodesRemaining)} //nolint:gosec // at most 10
	if status.EnabledAt != nil {
		resp.EnabledAt = timestamppb.New(*status.EnabledAt)
	}

	return resp, nil
}

// EnrollMfa starts an enrolment.
func (s *Server) EnrollMfa(ctx context.Context, _ *identityv1.EnrollMfaRequest) (*identityv1.EnrollMfaResponse, error) {
	enrollment, err := s.uc.EnrollMFA(ctx, middleware.UserID(ctx))
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &identityv1.EnrollMfaResponse{Secret: enrollment.Secret, OtpauthUri: enrollment.OTPAuthURI}, nil
}

// ConfirmMfaEnrollment turns MFA on and returns the recovery codes.
func (s *Server) ConfirmMfaEnrollment(ctx context.Context, req *identityv1.ConfirmMfaEnrollmentRequest) (*identityv1.ConfirmMfaEnrollmentResponse, error) {
	codes, err := s.uc.ConfirmMFAEnrollment(ctx, middleware.UserID(ctx), req.GetCode())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &identityv1.ConfirmMfaEnrollmentResponse{RecoveryCodes: codes}, nil
}

// RegenerateRecoveryCodes replaces the recovery codes.
func (s *Server) RegenerateRecoveryCodes(ctx context.Context, req *identityv1.RegenerateRecoveryCodesRequest) (*identityv1.RegenerateRecoveryCodesResponse, error) {
	codes, err := s.uc.RegenerateRecoveryCodes(ctx, middleware.UserID(ctx), req.GetCode())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &identityv1.RegenerateRecoveryCodesResponse{RecoveryCodes: codes}, nil
}

// DisableMfa turns the second factor off.
func (s *Server) DisableMfa(ctx context.Context, req *identityv1.DisableMfaRequest) (*identityv1.DisableMfaResponse, error) {
	if err := s.uc.DisableMFA(ctx, middleware.UserID(ctx), req.GetCode()); err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &identityv1.DisableMfaResponse{}, nil
}
