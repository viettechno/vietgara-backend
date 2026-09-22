package usecase

import (
	"context"
	"errors"
	"strings"

	"github.com/viettechno/vietgara-backend/internal/tenant/domain"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

const minPhoneDigits = 8

// StaffOption configures a StaffUseCase.
type StaffOption func(*StaffUseCase)

// WithAccountDirectory lets the use case resolve accounts by email or phone,
// show account details on staff lists, and verify accounts before inviting.
func WithAccountDirectory(directory AccountDirectory) StaffOption {
	return func(uc *StaffUseCase) {
		uc.account = directory
	}
}

// StaffMember is a staff membership together with the account behind it.
// Account is nil when no AccountDirectory is configured or the account is gone.
type StaffMember struct {
	Staff   *domain.GarageStaff
	Account *AccountSummary
}

// StaffCandidate is the result of looking up an account to invite.
type StaffCandidate struct {
	Account *AccountSummary
	// AlreadyStaff is true when the account already has a membership in the garage.
	AlreadyStaff bool
}

// ListMembers returns the active staff of garageID with their account details.
func (uc *StaffUseCase) ListMembers(ctx context.Context, garageID string) ([]StaffMember, error) {
	garaStaffs, err := uc.List(ctx, garageID)
	if err != nil {
		return nil, err
	}

	members := make([]StaffMember, 0, len(garaStaffs))
	for _, s := range garaStaffs {
		members = append(members, StaffMember{Staff: s})
	}

	if uc.account == nil || len(garaStaffs) == 0 {
		return members, nil
	}

	ids := make([]string, 0, len(garaStaffs))
	for _, s := range garaStaffs {
		ids = append(ids, s.AccountID)
	}

	accounts, err := uc.account.ListByIDs(ctx, ids)
	if err != nil {
		return nil, apperror.Wrap(apperror.CodeInternal, "load staff accounts", err)
	}

	for i := range members {
		members[i].Account = accounts[members[i].Staff.AccountID]
	}

	return members, nil
}

// FindCandidate looks an account up by its unique email or phone number (an
// identifier containing "@" is treated as an email) and reports whether it is
// already staff at garageID. Only exact matches are returned, so the lookup
// cannot be used to browse accounts.
func (uc *StaffUseCase) FindCandidate(ctx context.Context, garageID, identifier string) (*StaffCandidate, error) {
	if uc.account == nil {
		return nil, apperror.New(apperror.CodeFailedPrecondition, "account lookup is not available")
	}

	identifier = strings.TrimSpace(identifier)

	var (
		account *AccountSummary
		err     error
	)

	switch {
	case strings.Contains(identifier, "@"):
		account, err = uc.account.FindByEmail(ctx, identifier)
	case countDigits(identifier) >= minPhoneDigits:
		account, err = uc.account.FindByPhone(ctx, identifier)
	default:
		return nil, apperror.New(apperror.CodeInvalidArgument, "enter a full email address or phone number")
	}

	if err != nil {
		if errors.Is(err, domain.ErrAccountNotFound) {
			return nil, apperror.Wrap(apperror.CodeNotFound, domain.ErrAccountNotFound.Error(), err)
		}

		return nil, apperror.Wrap(apperror.CodeInternal, "find account", err)
	}

	candidate := &StaffCandidate{Account: account}

	existing, err := uc.staff.FindByGarageAndAccount(ctx, garageID, account.ID)
	if err == nil && existing != nil {
		candidate.AlreadyStaff = existing.Status == domain.StaffStatusActive
	}

	return candidate, nil
}

// requireActiveAccount checks the account exists and is active. It is a no-op
// without an AccountDirectory.
func (uc *StaffUseCase) requireActiveAccount(ctx context.Context, accountID string) error {
	if uc.account == nil {
		return nil
	}

	accounts, err := uc.account.ListByIDs(ctx, []string{accountID})
	if err != nil {
		return apperror.Wrap(apperror.CodeInternal, "find account", err)
	}

	account, ok := accounts[accountID]
	if !ok {
		return apperror.Wrap(apperror.CodeNotFound, domain.ErrAccountNotFound.Error(), domain.ErrAccountNotFound)
	}

	if !account.Active {
		return apperror.Wrap(apperror.CodeFailedPrecondition, domain.ErrAccountInactive.Error(), domain.ErrAccountInactive)
	}

	return nil
}

func countDigits(s string) int {
	n := 0

	for _, r := range s {
		if r >= '0' && r <= '9' {
			n++
		}
	}

	return n
}
