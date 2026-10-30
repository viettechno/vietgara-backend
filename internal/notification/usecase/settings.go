package usecase

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/viettechno/vietgara-backend/internal/notification/domain"
)

// Errors for the settings of a garage.
var (
	ErrInvalidSenderName = errors.New("the sender name is at most 100 characters")
	ErrInvalidType       = errors.New("an unknown or repeated notification type")
)

// GetSettings returns the garage's e-mail choices.
func (uc *NotificationUseCase) GetSettings(ctx context.Context, garageID string) (domain.Settings, error) {
	return uc.Settings.Get(ctx, garageID)
}

// UpdateSettings replaces the garage's e-mail choices. A type left out of
// types is on.
func (uc *NotificationUseCase) UpdateSettings(
	ctx context.Context, garageID, senderName string, types map[domain.Type]bool, order []domain.Type,
) (domain.Settings, error) {
	senderName = strings.TrimSpace(senderName)
	if utf8.RuneCountInString(senderName) > domain.MaxSenderNameLength {
		return domain.Settings{}, ErrInvalidSenderName
	}

	off := map[domain.Type]bool{}

	seen := map[domain.Type]bool{}
	for _, t := range order {
		if !t.Valid() || seen[t] {
			return domain.Settings{}, ErrInvalidType
		}

		seen[t] = true

		if !types[t] {
			off[t] = true
		}
	}

	settings := domain.Settings{SenderName: senderName, Off: off}
	if err := uc.Settings.Save(ctx, garageID, settings); err != nil {
		return domain.Settings{}, err
	}

	return settings, nil
}

// ListLog returns a page of the garage's sends, newest first.
func (uc *NotificationUseCase) ListLog(ctx context.Context, garageID string, filter LogFilter, page, pageSize int) ([]domain.Log, int, error) {
	return uc.Log.List(ctx, garageID, filter, page, pageSize)
}

// Suppress stops sending to an address that bounced for good or complained
// (the provider's webhook).
func (uc *NotificationUseCase) Suppress(ctx context.Context, email, reason string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return nil
	}

	return uc.Suppressions.Suppress(ctx, email, reason)
}
