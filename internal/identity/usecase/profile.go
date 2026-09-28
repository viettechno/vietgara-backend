package usecase

import (
	"context"

	"github.com/viettechno/vietgara-backend/internal/identity/domain"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// ProfileUpdate lists the profile fields to change; nil fields are kept.
// An empty Phone clears it.
type ProfileUpdate struct {
	FullName *string
	Phone    *string
	Locale   *domain.Locale
}

// GetMe returns the signed-in account.
func (uc *IdentityUseCase) GetMe(ctx context.Context, accountID string) (*domain.Account, error) {
	return uc.findAccount(ctx, accountID)
}

// UpdateMe changes the signed-in account's name, phone or language.
func (uc *IdentityUseCase) UpdateMe(ctx context.Context, accountID string, update ProfileUpdate) (*domain.Account, error) {
	acc, err := uc.findAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}

	if update.FullName != nil {
		name, ok := domain.NormalizeFullName(*update.FullName)
		if !ok {
			return nil, apperror.Validation("fullName", apperror.IssueRequired, "full name is required")
		}

		acc.FullName = name
	}

	if update.Phone != nil {
		phone, err := normalizeOptionalPhone(update.Phone)
		if err != nil {
			return nil, err
		}

		if err := uc.ensureAvailable(ctx, "", phone, acc.ID); err != nil {
			return nil, err
		}

		acc.Phone = phone
	}

	if update.Locale != nil {
		acc.Locale = update.Locale.OrDefault()
	}

	if err := uc.accounts.Update(ctx, acc); err != nil {
		return nil, mapCreateError(err)
	}

	return acc, nil
}
