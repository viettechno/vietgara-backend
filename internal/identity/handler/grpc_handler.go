// Package handler exposes the Identity module: the AuthService and
// UserService gRPC services (REST through grpc-gateway) and the Google
// OAuth redirect routes.
package handler

import (
	"context"

	identityv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/identity/v1"

	"github.com/viettechno/vietgara-backend/internal/platform/googleid"

	"github.com/viettechno/vietgara-backend/internal/identity/usecase"
	"github.com/viettechno/vietgara-backend/internal/middleware"
)

// Server implements identityv1.AuthServiceServer,
// identityv1.UserServiceServer and identityv1.MfaServiceServer. Embedding the Unimplemented servers
// keeps it forward-compatible with new RPCs.
type Server struct {
	identityv1.UnimplementedAuthServiceServer
	identityv1.UnimplementedUserServiceServer
	identityv1.UnimplementedMfaServiceServer

	uc          *usecase.IdentityUseCase
	accessTTL   int64
	googleOAuth GoogleOAuthConfig
	googleID    GoogleIDTokenVerifier
}

// GoogleIDTokenVerifier checks a Google ID token obtained natively by a
// mobile app (implemented by googleid.Verifier).
type GoogleIDTokenVerifier interface {
	Configured() bool
	Verify(ctx context.Context, idToken string) (googleid.Claims, error)
}

// NewServer returns a Server; accessTTLSeconds is echoed as expires_in.
func NewServer(uc *usecase.IdentityUseCase, accessTTLSeconds int64, google GoogleOAuthConfig) *Server {
	return &Server{uc: uc, accessTTL: accessTTLSeconds, googleOAuth: google}
}

// WithGoogleIDTokenVerifier enables SignInWithGoogleIdToken (mobile apps).
func (s *Server) WithGoogleIDTokenVerifier(v GoogleIDTokenVerifier) *Server {
	s.googleID = v

	return s
}

// Policies declares what every Identity RPC requires (see
// middleware.Policy). Sign-in (with its MFA challenge) and password reset are
// public; the profile
// and e-mail verification work before the e-mail is verified.
func Policies() middleware.Policies {
	return middleware.Policies{
		identityv1.AuthService_Register_FullMethodName:                 middleware.PublicMethod(),
		identityv1.AuthService_Login_FullMethodName:                    middleware.PublicMethod(),
		identityv1.AuthService_SignInWithGoogleIdToken_FullMethodName:  middleware.PublicMethod(),
		identityv1.AuthService_RefreshToken_FullMethodName:             middleware.PublicMethod(),
		identityv1.AuthService_Logout_FullMethodName:                   middleware.PublicMethod(),
		identityv1.AuthService_RequestPasswordReset_FullMethodName:     middleware.PublicMethod(),
		identityv1.AuthService_VerifyPasswordResetOtp_FullMethodName:   middleware.PublicMethod(),
		identityv1.AuthService_ResetPassword_FullMethodName:            middleware.PublicMethod(),
		identityv1.AuthService_SendEmailVerificationOtp_FullMethodName: middleware.UnverifiedMethod(),
		identityv1.AuthService_VerifyEmail_FullMethodName:              middleware.UnverifiedMethod(),
		identityv1.UserService_GetMe_FullMethodName:                    middleware.UnverifiedMethod(),
		identityv1.UserService_UpdateMe_FullMethodName:                 middleware.UnverifiedMethod(),
		identityv1.AuthService_VerifyMfaChallenge_FullMethodName:       middleware.PublicMethod(),
		identityv1.MfaService_GetMfaStatus_FullMethodName:              middleware.UserMethod(),
		identityv1.MfaService_EnrollMfa_FullMethodName:                 middleware.UserMethod(),
		identityv1.MfaService_ConfirmMfaEnrollment_FullMethodName:      middleware.UserMethod(),
		identityv1.MfaService_RegenerateRecoveryCodes_FullMethodName:   middleware.UserMethod(),
		identityv1.MfaService_DisableMfa_FullMethodName:                middleware.UserMethod(),
	}
}
