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

// GetMe returns the signed-in user.
func (uc *IdentityUseCase) GetMe(ctx context.Context, userID string) (*domain.User, error) {
	return uc.findUser(ctx, userID)
}

// UpdateMe changes the signed-in user's name, phone or language.
func (uc *IdentityUseCase) UpdateMe(ctx context.Context, userID string, update ProfileUpdate) (*domain.User, error) {
	user, err := uc.findUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	if update.FullName != nil {
		name, ok := domain.NormalizeFullName(*update.FullName)
		if !ok {
			return nil, apperror.Validation("fullName", apperror.IssueRequired, "full name is required")
		}

		if name != user.FullName {
			user.FullNameSource = domain.NameSourceUser
		}

		user.FullName = name
	}

	if update.Phone != nil {
		phone, err := normalizeOptionalPhone(update.Phone)
		if err != nil {
			return nil, err
		}

		if err := uc.ensureAvailable(ctx, "", phone, user.ID); err != nil {
			return nil, err
		}

		user.Phone = phone
	}

	if update.Locale != nil {
		user.Locale = update.Locale.OrDefault()
	}

	if err := uc.users.Update(ctx, user); err != nil {
		return nil, mapCreateError(err)
	}

	return user, nil
}
