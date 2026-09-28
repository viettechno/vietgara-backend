package usecase

import (
	"context"
	"errors"

	"golang.org/x/crypto/bcrypt"

	"github.com/viettechno/vietgara-backend/internal/identity/domain"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// dummyHash is compared against when the account does not exist, so a
// wrong e-mail takes as long as a wrong password.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("vietgara-timing-equalizer"), bcrypt.DefaultCost)

// Login authenticates an account by e-mail and password (FR-IAM-02).
// Unknown e-mails and wrong passwords return the same error, so the API
// cannot be used to enumerate accounts.
func (uc *IdentityUseCase) Login(ctx context.Context, rawEmail, password string) (*Session, error) {
	email, ok := domain.NormalizeEmail(rawEmail)
	if !ok || password == "" {
		return nil, invalidCredentials()
	}

	acc, err := uc.accounts.FindByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, domain.ErrAccountNotFound) {
			_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))

			return nil, invalidCredentials()
		}

		return nil, apperror.Internal("find account by email", err)
	}

	if acc.PasswordHash == nil || bcrypt.CompareHashAndPassword([]byte(*acc.PasswordHash), []byte(password)) != nil {
		return nil, invalidCredentials()
	}

	return uc.issueSession(ctx, acc)
}
