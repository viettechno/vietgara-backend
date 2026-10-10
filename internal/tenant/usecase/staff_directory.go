package usecase

import (
	"context"
	"errors"

	"github.com/viettechno/vietgara-backend/internal/tenant/domain"
)

// Candidate is an existing user found to be invited.
type Candidate struct {
	User       *UserSummary
	Membership string // none, member, invited
}

// Candidate memberships.
const (
	CandidateNone    = "none"
	CandidateMember  = "member"
	CandidateInvited = "invited"
)

// LookupCandidate finds an existing user by exact e-mail or phone
// (FR-TEN-04: only existing users can be added).
func (uc *TenantUseCase) LookupCandidate(ctx context.Context, garageID, query string) (*Candidate, error) {
	user, err := uc.findUserByContact(ctx, query)
	if err != nil {
		return nil, err
	}

	candidate := &Candidate{User: user, Membership: CandidateNone}

	member, err := uc.Staff.FindByUser(ctx, garageID, user.ID)
	switch {
	case err == nil && member.Status == domain.StaffStatusActive:
		candidate.Membership = CandidateMember
	case err != nil && !errors.Is(err, domain.ErrStaffNotFound):
		return nil, toAppError("lookup staff", err)
	}

	if candidate.Membership == CandidateNone {
		invited, err := uc.hasPendingInvitation(ctx, garageID, user.ID)
		if err != nil {
			return nil, err
		}

		if invited {
			candidate.Membership = CandidateInvited
		}
	}

	return candidate, nil
}

func (uc *TenantUseCase) findUserByContact(ctx context.Context, query string) (*UserSummary, error) {
	var (
		user *UserSummary
		err  error
	)

	if containsAt(query) {
		user, err = uc.Users.FindByEmail(ctx, query)
	} else {
		user, err = uc.Users.FindByPhone(ctx, query)
	}

	if err != nil {
		return nil, toAppError("lookup user", err)
	}

	if !user.Active {
		return nil, toAppError("lookup user", domain.ErrUserInactive)
	}

	return user, nil
}

// withUsers loads the user details of members.
func (uc *TenantUseCase) withUsers(ctx context.Context, members ...*domain.Staff) error {
	ids := make([]string, 0, len(members))
	for _, m := range members {
		ids = append(ids, m.UserID)
	}

	users, err := uc.Users.ListByIDs(ctx, ids)
	if err != nil {
		return toAppError("load staff users", err)
	}

	for _, m := range members {
		if user, ok := users[m.UserID]; ok {
			m.FullName, m.Email, m.Phone = user.FullName, user.Email, user.Phone
		}
	}

	return nil
}

func containsAt(s string) bool {
	for _, r := range s {
		if r == '@' {
			return true
		}
	}

	return false
}
