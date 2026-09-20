package handler

import (
	"context"

	identityv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/identity/v1"

	"github.com/viettechno/vietgara-backend/internal/identity/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Register creates an account from email/password, phone/OTP, or social
// credentials and returns a fresh session.
func (s *Server) Register(ctx context.Context, req *identityv1.RegisterRequest) (*identityv1.RegisterResponse, error) {
	var (
		result *usecase.AuthResult
		err    error
	)

	switch m := req.GetMethod().(type) {
	case *identityv1.RegisterRequest_EmailPassword:
		result, err = s.uc.Register(ctx, usecase.RegisterRequest{Email: &m.EmailPassword.Email, Password: m.EmailPassword.Password, FullName: req.GetFullName()})
	case *identityv1.RegisterRequest_PhoneOtp:
		result, err = s.uc.Register(ctx, usecase.RegisterRequest{Phone: &m.PhoneOtp.Phone, FullName: req.GetFullName()})
	case *identityv1.RegisterRequest_Social:
		profile, verifyErr := s.verifySocialProfile(ctx, m.Social.Provider, m.Social.IdToken)
		if verifyErr != nil {
			return nil, verifyErr
		}

		result, err = s.uc.SocialLogin(ctx, *profile)
	default:
		return nil, status.Error(codes.InvalidArgument, "must provide email_password, phone_otp, or social credentials")
	}

	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &identityv1.RegisterResponse{Auth: s.authToProto(result)}, nil
}

// Login authenticates an existing account and returns a fresh session.
func (s *Server) Login(ctx context.Context, req *identityv1.LoginRequest) (*identityv1.LoginResponse, error) {
	var (
		result *usecase.AuthResult
		err    error
	)

	switch m := req.GetMethod().(type) {
	case *identityv1.LoginRequest_EmailPassword:
		result, err = s.uc.Login(ctx, usecase.LoginRequest{Email: &m.EmailPassword.Email, Password: &m.EmailPassword.Password})
	case *identityv1.LoginRequest_PhoneOtp:
		result, err = s.uc.Login(ctx, usecase.LoginRequest{Phone: &m.PhoneOtp.Phone, OTPCode: &m.PhoneOtp.OtpCode})
	case *identityv1.LoginRequest_Social:
		profile, verifyErr := s.verifySocialProfile(ctx, m.Social.Provider, m.Social.IdToken)
		if verifyErr != nil {
			return nil, verifyErr
		}

		result, err = s.uc.SocialLogin(ctx, *profile)
	default:
		return nil, status.Error(codes.InvalidArgument, "must provide email_password, phone_otp, or social credentials")
	}

	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &identityv1.LoginResponse{Auth: s.authToProto(result)}, nil
}

// RefreshToken rotates the presented refresh token for a fresh token pair.
func (s *Server) RefreshToken(ctx context.Context, req *identityv1.RefreshTokenRequest) (*identityv1.RefreshTokenResponse, error) {
	if req.GetRefreshToken() == "" {
		return nil, status.Error(codes.InvalidArgument, "refresh_token is required")
	}

	result, err := s.uc.Refresh(ctx, req.GetRefreshToken())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &identityv1.RefreshTokenResponse{Auth: s.authToProto(result)}, nil
}

// Logout revokes the presented refresh token.
func (s *Server) Logout(ctx context.Context, req *identityv1.LogoutRequest) (*identityv1.LogoutResponse, error) {
	if req.GetRefreshToken() == "" {
		return nil, status.Error(codes.InvalidArgument, "refresh_token is required")
	}

	if err := s.uc.Logout(ctx, req.GetRefreshToken()); err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &identityv1.LogoutResponse{Revoked: true}, nil
}
