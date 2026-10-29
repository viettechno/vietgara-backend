package usecase

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
)

// ErrInvalidOptOutToken is returned for a token that does not verify, or
// names a customer that is gone.
var ErrInvalidOptOutToken = errors.New("invalid opt-out link")

// optOutPath is where the opt-out page of a customer e-mail lives.
const optOutPath = "/api/v1/public/email-opt-out/"

// optOutToken signs the opt-out link of one customer of one garage. It does
// not expire: an old e-mail must still work.
func (uc *NotificationUseCase) optOutToken(garageID, customerID string) string {
	return garageID + "." + customerID + "." + uc.optOutSignature(garageID, customerID)
}

func (uc *NotificationUseCase) optOutSignature(garageID, customerID string) string {
	mac := hmac.New(sha256.New, uc.OptOutKey)
	mac.Write([]byte("email-opt-out\n" + garageID + "\n" + customerID))

	return hex.EncodeToString(mac.Sum(nil)[:16])
}

// optOutURL is the absolute link in a customer e-mail.
func (uc *NotificationUseCase) optOutURL(garageID, customerID string) string {
	return strings.TrimSuffix(uc.BaseURL, "/") + optOutPath + uc.optOutToken(garageID, customerID)
}

// VerifyOptOutToken reports the garage and customer a token was signed for.
func (uc *NotificationUseCase) VerifyOptOutToken(token string) (garageID, customerID string, ok bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || len(uc.OptOutKey) == 0 {
		return "", "", false
	}

	if !hmac.Equal([]byte(parts[2]), []byte(uc.optOutSignature(parts[0], parts[1]))) {
		return "", "", false
	}

	return parts[0], parts[1], true
}

// OptOut opts the customer of a verified token out of the non-essential
// e-mails of its garage (PD-04, PD-15). It is idempotent.
func (uc *NotificationUseCase) OptOut(ctx context.Context, token string) error {
	garageID, customerID, ok := uc.VerifyOptOutToken(token)
	if !ok {
		return ErrInvalidOptOutToken
	}

	return uc.Tx.InGarage(ctx, garageID, func(ctx context.Context) error {
		if err := uc.Customers.OptOut(ctx, garageID, customerID); err != nil {
			if errors.Is(err, ErrCustomerNotFound) {
				return ErrInvalidOptOutToken
			}

			return err
		}

		return nil
	})
}

// OptOutTokenForTest signs a token for tests of the opt-out page.
func (uc *NotificationUseCase) OptOutTokenForTest(garageID, customerID string) string {
	return uc.optOutToken(garageID, customerID)
}
