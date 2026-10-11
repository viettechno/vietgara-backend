package usecase

import (
	"context"
	"errors"

	"golang.org/x/crypto/bcrypt"

	"github.com/viettechno/vietgara-backend/internal/identity/domain"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
	"github.com/viettechno/vietgara-backend/pkg/email"
)

// dummyHash is compared against when the user does not exist, so a
// wrong e-mail takes as long as a wrong password.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("vietgara-timing-equalizer"), bcrypt.DefaultCost)

// Login authenticates a user by e-mail and password (FR-IAM-02).
// Unknown e-mails and wrong passwords return the same error, so the API
// cannot be used to enumerate users.
func (uc *IdentityUseCase) Login(ctx context.Context, rawEmail, password string) (*Session, error) {
	addr, err := email.Normalize(rawEmail)
	if err != nil || password == "" {
		return nil, invalidCredentials()
	}

	user, err := uc.users.FindByEmail(ctx, addr)
	if err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))

			return nil, invalidCredentials()
		}

		return nil, apperror.Internal("find user by email", err)
	}

	if user.PasswordHash == nil || bcrypt.CompareHashAndPassword([]byte(*user.PasswordHash), []byte(password)) != nil {
		return nil, invalidCredentials()
	}

	return uc.issueSession(ctx, user)
}
