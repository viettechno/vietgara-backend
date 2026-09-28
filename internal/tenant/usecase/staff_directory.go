package usecase

import (
	"context"
	"errors"

	"github.com/viettechno/vietgara-backend/internal/tenant/domain"
)

// Candidate is an existing account found to be invited.
type Candidate struct {
	Account    *AccountSummary
	Membership string // none, member, invited
}

// Candidate memberships.
const (
	CandidateNone    = "none"
	CandidateMember  = "member"
	CandidateInvited = "invited"
)

// LookupCandidate finds an existing account by exact e-mail or phone
// (FR-TEN-04: only existing accounts can be added).
func (uc *TenantUseCase) LookupCandidate(ctx context.Context, garageID, query string) (*Candidate, error) {
	account, err := uc.findAccountByContact(ctx, query)
	if err != nil {
		return nil, err
	}

	candidate := &Candidate{Account: account, Membership: CandidateNone}

	member, err := uc.Staff.FindByAccount(ctx, garageID, account.ID)
	switch {
	case err == nil && member.Status == domain.StaffStatusActive:
		candidate.Membership = CandidateMember
	case err != nil && !errors.Is(err, domain.ErrStaffNotFound):
		return nil, toAppError("lookup staff", err)
	}

	if candidate.Membership == CandidateNone {
		invited, err := uc.hasPendingInvitation(ctx, garageID, account.ID)
		if err != nil {
			return nil, err
		}

		if invited {
			candidate.Membership = CandidateInvited
		}
	}

	return candidate, nil
}

func (uc *TenantUseCase) findAccountByContact(ctx context.Context, query string) (*AccountSummary, error) {
	var (
		account *AccountSummary
		err     error
	)

	if containsAt(query) {
		account, err = uc.Accounts.FindByEmail(ctx, query)
	} else {
		account, err = uc.Accounts.FindByPhone(ctx, query)
	}

	if err != nil {
		return nil, toAppError("lookup account", err)
	}

	if !account.Active {
		return nil, toAppError("lookup account", domain.ErrAccountInactive)
	}

	return account, nil
}

// withAccounts loads the account details of members.
func (uc *TenantUseCase) withAccounts(ctx context.Context, members ...*domain.Staff) error {
	ids := make([]string, 0, len(members))
	for _, m := range members {
		ids = append(ids, m.AccountID)
	}

	accounts, err := uc.Accounts.ListByIDs(ctx, ids)
	if err != nil {
		return toAppError("load staff accounts", err)
	}

	for _, m := range members {
		if acc, ok := accounts[m.AccountID]; ok {
			m.FullName, m.Email, m.Phone = acc.FullName, acc.Email, acc.Phone
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
