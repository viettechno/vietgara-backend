// Package email normalizes e-mail addresses, so an address typed by a
// customer, a supplier or a user is stored and compared in one form.
package email

import (
	"errors"
	"net/mail"
	"strings"
)

// ErrInvalid is returned for a value that is not a usable e-mail address.
var ErrInvalid = errors.New("email address is invalid")

// MaxLen is the longest address the platform stores.
const MaxLen = 255

// Normalize trims and lower-cases an address and checks that it is a bare
// address (no display name) with a dotted domain, e.g. "  An@Mail.VN " gives
// "an@mail.vn".
func Normalize(raw string) (string, error) {
	e := strings.ToLower(strings.TrimSpace(raw))
	if e == "" || len(e) > MaxLen {
		return "", ErrInvalid
	}

	parsed, err := mail.ParseAddress(e)
	if err != nil || parsed.Address != e {
		return "", ErrInvalid
	}

	if !strings.Contains(e[strings.LastIndex(e, "@"):], ".") {
		return "", ErrInvalid
	}

	return e, nil
}
