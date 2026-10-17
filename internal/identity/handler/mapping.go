package handler

import (
	"google.golang.org/protobuf/types/known/timestamppb"

	identityv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/identity/v1"

	"github.com/viettechno/vietgara-backend/internal/identity/domain"
	"github.com/viettechno/vietgara-backend/internal/identity/usecase"
	"github.com/viettechno/vietgara-backend/pkg/converter"
)

func (s *Server) sessionToProto(session *usecase.Session) *identityv1.Session {
	out := &identityv1.Session{
		AccessToken:  session.AccessToken,
		RefreshToken: session.RefreshToken,
		TokenType:    "Bearer",
		ExpiresIn:    s.accessTTL,
		User:         userToProto(session.User),
	}

	if session.MFARequired {
		out.ExpiresIn = 0
		out.MfaRequired = true
		out.ChallengeToken = session.ChallengeToken
		out.ChallengeExpiresAt = timestamppb.New(session.ChallengeExpiresAt)
	}

	return out
}

func challengeToProto(c usecase.Challenge) *identityv1.OtpChallenge {
	return &identityv1.OtpChallenge{
		ExpiresAt:         timestamppb.New(c.ExpiresAt),
		ResendAvailableAt: timestamppb.New(c.ResendAvailableAt),
	}
}

func userToProto(user *domain.User) *identityv1.User {
	return &identityv1.User{
		Id:            user.ID,
		Email:         user.Email,
		Phone:         user.Phone,
		AuthProvider:  authProviderToProto(user.AuthProvider),
		FullName:      user.FullName,
		Status:        userStatusToProto(user.Status),
		EmailVerified: user.EmailVerified(),
		Locale:        localeToProto(user.Locale),
		PlatformAdmin: user.PlatformAdmin,
		MfaEnabled:    user.MFAEnabled,
		CreatedAt:     converter.TimeToProto(user.CreatedAt),
		UpdatedAt:     converter.TimeToProto(user.UpdatedAt),
	}
}

func authProviderToProto(p domain.AuthProvider) identityv1.AuthProvider {
	if p == domain.AuthProviderGoogle {
		return identityv1.AuthProvider_AUTH_PROVIDER_GOOGLE
	}

	return identityv1.AuthProvider_AUTH_PROVIDER_EMAIL
}

func userStatusToProto(s domain.UserStatus) identityv1.UserStatus {
	if s == domain.UserStatusSuspended {
		return identityv1.UserStatus_USER_STATUS_SUSPENDED
	}

	return identityv1.UserStatus_USER_STATUS_ACTIVE
}

func localeToProto(l domain.Locale) identityv1.Locale {
	if l.OrDefault() == domain.LocaleEN {
		return identityv1.Locale_LOCALE_EN
	}

	return identityv1.Locale_LOCALE_VI
}

func localeFromProto(l identityv1.Locale) domain.Locale {
	if l == identityv1.Locale_LOCALE_EN {
		return domain.LocaleEN
	}

	return domain.LocaleVI
}
