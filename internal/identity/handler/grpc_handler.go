// Package handler exposes the Identity module: the AuthService and
// AccountService gRPC services (REST through grpc-gateway) and the Google
// OAuth redirect routes.
package handler

import (
	identityv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/identity/v1"

	"github.com/viettechno/vietgara-backend/internal/identity/usecase"
	"github.com/viettechno/vietgara-backend/internal/middleware"
)

// Server implements identityv1.AuthServiceServer and
// identityv1.AccountServiceServer. Embedding the Unimplemented servers
// keeps it forward-compatible with new RPCs.
type Server struct {
	identityv1.UnimplementedAuthServiceServer
	identityv1.UnimplementedAccountServiceServer

	uc          *usecase.IdentityUseCase
	accessTTL   int64
	googleOAuth GoogleOAuthConfig
}

// NewServer returns a Server; accessTTLSeconds is echoed as expires_in.
func NewServer(uc *usecase.IdentityUseCase, accessTTLSeconds int64, google GoogleOAuthConfig) *Server {
	return &Server{uc: uc, accessTTL: accessTTLSeconds, googleOAuth: google}
}

// Policies declares what every Identity RPC requires (see
// middleware.Policy). Sign-in and password reset are public; the profile
// and e-mail verification work before the e-mail is verified.
func Policies() middleware.Policies {
	return middleware.Policies{
		identityv1.AuthService_Register_FullMethodName:                 middleware.PublicMethod(),
		identityv1.AuthService_Login_FullMethodName:                    middleware.PublicMethod(),
		identityv1.AuthService_RefreshToken_FullMethodName:             middleware.PublicMethod(),
		identityv1.AuthService_Logout_FullMethodName:                   middleware.PublicMethod(),
		identityv1.AuthService_RequestPasswordReset_FullMethodName:     middleware.PublicMethod(),
		identityv1.AuthService_VerifyPasswordResetOtp_FullMethodName:   middleware.PublicMethod(),
		identityv1.AuthService_ResetPassword_FullMethodName:            middleware.PublicMethod(),
		identityv1.AuthService_SendEmailVerificationOtp_FullMethodName: middleware.UnverifiedMethod(),
		identityv1.AuthService_VerifyEmail_FullMethodName:              middleware.UnverifiedMethod(),
		identityv1.AccountService_GetMe_FullMethodName:                 middleware.UnverifiedMethod(),
		identityv1.AccountService_UpdateMe_FullMethodName:              middleware.UnverifiedMethod(),
	}
}
