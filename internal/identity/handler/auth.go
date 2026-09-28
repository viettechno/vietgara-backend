package handler

import (
	"context"

	"google.golang.org/protobuf/types/known/timestamppb"

	identityv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/identity/v1"

	"github.com/viettechno/vietgara-backend/internal/identity/usecase"
	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/platform/httpapi"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// Register creates an owner account (FR-IAM-01); answers 201.
func (s *Server) Register(ctx context.Context, req *identityv1.RegisterRequest) (*identityv1.RegisterResponse, error) {
	session, err := s.uc.Register(ctx, usecase.RegisterRequest{
		Email:    req.GetEmail(),
		Password: req.GetPassword(),
		FullName: req.GetFullName(),
		Phone:    req.Phone,
		Locale:   localeFromProto(req.GetLocale()),
	})
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	httpapi.Created(ctx)

	return &identityv1.RegisterResponse{Session: s.sessionToProto(session)}, nil
}

// Login signs in with e-mail and password (FR-IAM-02).
func (s *Server) Login(ctx context.Context, req *identityv1.LoginRequest) (*identityv1.LoginResponse, error) {
	session, err := s.uc.Login(ctx, req.GetEmail(), req.GetPassword())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &identityv1.LoginResponse{Session: s.sessionToProto(session)}, nil
}

// RefreshToken rotates a refresh token.
func (s *Server) RefreshToken(ctx context.Context, req *identityv1.RefreshTokenRequest) (*identityv1.RefreshTokenResponse, error) {
	session, err := s.uc.Refresh(ctx, req.GetRefreshToken())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &identityv1.RefreshTokenResponse{Session: s.sessionToProto(session)}, nil
}

// Logout revokes a refresh token.
func (s *Server) Logout(ctx context.Context, req *identityv1.LogoutRequest) (*identityv1.LogoutResponse, error) {
	if err := s.uc.Logout(ctx, req.GetRefreshToken()); err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &identityv1.LogoutResponse{}, nil
}

// SendEmailVerificationOtp e-mails a verification OTP.
func (s *Server) SendEmailVerificationOtp(ctx context.Context, _ *identityv1.SendEmailVerificationOtpRequest) (*identityv1.SendEmailVerificationOtpResponse, error) {
	challenge, err := s.uc.SendEmailVerificationOTP(ctx, middleware.AccountID(ctx))
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &identityv1.SendEmailVerificationOtpResponse{Challenge: challengeToProto(challenge)}, nil
}

// VerifyEmail confirms the e-mail with the OTP.
func (s *Server) VerifyEmail(ctx context.Context, req *identityv1.VerifyEmailRequest) (*identityv1.VerifyEmailResponse, error) {
	session, err := s.uc.VerifyEmail(ctx, middleware.AccountID(ctx), req.GetOtpCode(), req.GetRefreshToken())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &identityv1.VerifyEmailResponse{Session: s.sessionToProto(session)}, nil
}

// RequestPasswordReset e-mails a password-reset OTP (FR-IAM-04).
func (s *Server) RequestPasswordReset(ctx context.Context, req *identityv1.RequestPasswordResetRequest) (*identityv1.RequestPasswordResetResponse, error) {
	challenge, err := s.uc.RequestPasswordReset(ctx, req.GetEmail())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &identityv1.RequestPasswordResetResponse{Challenge: challengeToProto(challenge)}, nil
}

// VerifyPasswordResetOtp exchanges the OTP for a reset token.
func (s *Server) VerifyPasswordResetOtp(ctx context.Context, req *identityv1.VerifyPasswordResetOtpRequest) (*identityv1.VerifyPasswordResetOtpResponse, error) {
	token, expiresAt, err := s.uc.VerifyPasswordResetOTP(ctx, req.GetEmail(), req.GetOtpCode())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &identityv1.VerifyPasswordResetOtpResponse{ResetToken: token, ExpiresAt: timestamppb.New(expiresAt)}, nil
}

// ResetPassword sets a new password.
func (s *Server) ResetPassword(ctx context.Context, req *identityv1.ResetPasswordRequest) (*identityv1.ResetPasswordResponse, error) {
	if err := s.uc.ResetPassword(ctx, req.GetResetToken(), req.GetNewPassword()); err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &identityv1.ResetPasswordResponse{}, nil
}

// GetMe returns the signed-in account.
func (s *Server) GetMe(ctx context.Context, _ *identityv1.GetMeRequest) (*identityv1.GetMeResponse, error) {
	acc, err := s.uc.GetMe(ctx, middleware.AccountID(ctx))
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &identityv1.GetMeResponse{Account: accountToProto(acc)}, nil
}

// UpdateMe changes the fields named by the update mask (all settable
// fields when it is empty).
func (s *Server) UpdateMe(ctx context.Context, req *identityv1.UpdateMeRequest) (*identityv1.UpdateMeResponse, error) {
	in := req.GetAccount()
	update := usecase.ProfileUpdate{}

	for _, path := range maskPaths(req.GetUpdateMask().GetPaths(), "full_name", "phone", "locale") {
		switch path {
		case "full_name":
			name := in.GetFullName()
			update.FullName = &name
		case "phone":
			phone := in.GetPhone()
			update.Phone = &phone
		case "locale":
			locale := localeFromProto(in.GetLocale())
			update.Locale = &locale
		default:
			return nil, apperror.ToGRPCError(apperror.Validation("updateMask", apperror.IssueInvalid, "field "+path+" cannot be updated"))
		}
	}

	acc, err := s.uc.UpdateMe(ctx, middleware.AccountID(ctx), update)
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &identityv1.UpdateMeResponse{Account: accountToProto(acc)}, nil
}

// maskPaths returns paths, or every settable field when the mask is empty.
func maskPaths(paths []string, settable ...string) []string {
	if len(paths) == 0 {
		return settable
	}

	return paths
}
