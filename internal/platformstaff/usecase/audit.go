package usecase

import (
	"context"

	"github.com/google/uuid"

	"github.com/viettechno/vietgara-backend/internal/platformstaff/domain"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// ListAuditLog returns the platform audit log, newest first, with the actor's
// name where the user still exists. The interceptor has checked audit.read.
func (uc *UseCase) ListAuditLog(ctx context.Context, filter domain.AuditFilter, page domain.Page) ([]*domain.AuditEntry, int, error) {
	if err := validAuditFilter(filter); err != nil {
		return nil, 0, err
	}

	entries, total, err := uc.Repo.ListAuditLog(ctx, filter, page)
	if err != nil {
		return nil, 0, toAppError("list audit log", err)
	}

	if err := uc.nameActors(ctx, entries); err != nil {
		return nil, 0, err
	}

	return entries, total, nil
}

func validAuditFilter(filter domain.AuditFilter) error {
	if filter.ActorUserID != "" {
		if _, err := uuid.Parse(filter.ActorUserID); err != nil {
			return apperror.Validation("actor_user_id", "invalid", "actor_user_id must be a user id")
		}
	}

	if filter.From != nil && filter.To != nil && !filter.From.Before(*filter.To) {
		return apperror.Validation("from", "invalid", "from must be before to")
	}

	return nil
}

// nameActors sets ActorName on the entries whose actor still exists.
func (uc *UseCase) nameActors(ctx context.Context, entries []*domain.AuditEntry) error {
	var ids []string

	for _, e := range entries {
		if e.ActorUserID != "" && !containsID(ids, e.ActorUserID) {
			ids = append(ids, e.ActorUserID)
		}
	}

	if len(ids) == 0 {
		return nil
	}

	users, err := uc.Users.ListByIDs(ctx, ids)
	if err != nil {
		return toAppError("look up audit actors", err)
	}

	for _, e := range entries {
		if u := users[e.ActorUserID]; u != nil {
			e.ActorName = u.FullName
		}
	}

	return nil
}
