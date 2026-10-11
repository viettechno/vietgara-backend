package usecase

import (
	"context"
	"errors"

	"github.com/viettechno/vietgara-backend/internal/identity/domain"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// FindVerifiedUserIDByEmail answers which user owns a verified e-mail, for
// the Customer & Vehicle module's link requests (FR-CUS-06). It returns the
// id only -- the garage never learns who the user is -- and false when no
// user has that e-mail verified.
func (uc *IdentityUseCase) FindVerifiedUserIDByEmail(ctx context.Context, email string) (string, bool, error) {
	user, err := uc.users.FindByEmail(ctx, email)
	if errors.Is(err, domain.ErrUserNotFound) {
		return "", false, nil
	}

	if err != nil {
		return "", false, apperror.Internal("find user by e-mail", err)
	}

	if !user.EmailVerified() {
		return "", false, nil
	}

	return user.ID, true, nil
}

// SendCustomerLinkCode e-mails the user a one-time code for a garage's link
// request; the code belongs to the request (linkID), not to the user.
func (uc *IdentityUseCase) SendCustomerLinkCode(ctx context.Context, userID, linkID, garageName string) error {
	user, err := uc.findUser(ctx, userID)
	if err != nil {
		return err
	}

	_, err = uc.sendOTPFor(ctx, domain.OTPCustomerLink, linkID, user, map[string]any{"GarageName": garageName})
	if err != nil {
		if errors.Is(err, domain.ErrOTPResendTooSoon) {
			return otpError(err)
		}

		return apperror.Internal("send customer link code", err)
	}

	return nil
}

// CheckCustomerLinkCode verifies and consumes the code of a link request.
func (uc *IdentityUseCase) CheckCustomerLinkCode(ctx context.Context, linkID, code string) error {
	return uc.checkOTP(ctx, domain.OTPCustomerLink, linkID, code)
}
