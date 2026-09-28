package handler

import (
	"google.golang.org/protobuf/types/known/timestamppb"

	identityv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/identity/v1"

	"github.com/viettechno/vietgara-backend/internal/identity/domain"
	"github.com/viettechno/vietgara-backend/internal/identity/usecase"
	"github.com/viettechno/vietgara-backend/pkg/converter"
)

func (s *Server) sessionToProto(session *usecase.Session) *identityv1.Session {
	return &identityv1.Session{
		AccessToken:  session.AccessToken,
		RefreshToken: session.RefreshToken,
		TokenType:    "Bearer",
		ExpiresIn:    s.accessTTL,
		Account:      accountToProto(session.Account),
	}
}

func challengeToProto(c usecase.Challenge) *identityv1.OtpChallenge {
	return &identityv1.OtpChallenge{
		ExpiresAt:         timestamppb.New(c.ExpiresAt),
		ResendAvailableAt: timestamppb.New(c.ResendAvailableAt),
	}
}

func accountToProto(acc *domain.Account) *identityv1.Account {
	return &identityv1.Account{
		Id:            acc.ID,
		Email:         acc.Email,
		Phone:         acc.Phone,
		AuthProvider:  authProviderToProto(acc.AuthProvider),
		FullName:      acc.FullName,
		Status:        accountStatusToProto(acc.Status),
		EmailVerified: acc.EmailVerified(),
		Locale:        localeToProto(acc.Locale),
		PlatformAdmin: acc.PlatformAdmin,
		CreatedAt:     converter.TimeToProto(acc.CreatedAt),
		UpdatedAt:     converter.TimeToProto(acc.UpdatedAt),
	}
}

func authProviderToProto(p domain.AuthProvider) identityv1.AuthProvider {
	if p == domain.AuthProviderGoogle {
		return identityv1.AuthProvider_AUTH_PROVIDER_GOOGLE
	}

	return identityv1.AuthProvider_AUTH_PROVIDER_EMAIL
}

func accountStatusToProto(s domain.AccountStatus) identityv1.AccountStatus {
	if s == domain.AccountStatusSuspended {
		return identityv1.AccountStatus_ACCOUNT_STATUS_SUSPENDED
	}

	return identityv1.AccountStatus_ACCOUNT_STATUS_ACTIVE
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
