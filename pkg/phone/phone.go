// Package phone normalizes phone numbers to E.164, the one form the whole
// platform stores and compares (users, customers, suppliers), so a
// customer's phone can be matched with a user's verified phone.
package phone

import (
	"errors"
	"strings"
)

// ErrInvalid is returned for a value that is not a phone number.
var ErrInvalid = errors.New("phone number is invalid")

const (
	// E.164 allows at most 15 digits after the "+".
	minE164Digits = 8
	maxE164Digits = 15
	// A Vietnamese national number is 0 followed by 9 or 10 digits.
	minNationalDigits = 10
	maxNationalDigits = 11
	countryCode       = "84"
)

var formatting = strings.NewReplacer(" ", "", "-", "", ".", "", "(", "", ")", "")

// Normalize returns the E.164 form of a phone number, e.g.
// "+84 90 123 4567", "0901234567" and "84901234567" all become
// "+84901234567". Spaces, dashes, dots and brackets are ignored. A
// Vietnamese number may be given in national form (leading 0), with the
// country code and no "+", or in international form; any other
// international number needs its "+".
func Normalize(raw string) (string, error) {
	p := formatting.Replace(strings.TrimSpace(raw))

	switch {
	case strings.HasPrefix(p, "+"):
		return international(p)
	case strings.HasPrefix(p, "0"):
		return national(p)
	case strings.HasPrefix(p, countryCode):
		return withCountryCode(p)
	default:
		return "", ErrInvalid
	}
}

// international accepts "+" and 8 to 15 digits, the first not 0.
func international(p string) (string, error) {
	digits := p[1:]
	if !isDigits(digits) || len(digits) < minE164Digits || len(digits) > maxE164Digits || digits[0] == '0' {
		return "", ErrInvalid
	}

	return p, nil
}

// national accepts a Vietnamese number with its leading 0.
func national(p string) (string, error) {
	if !isDigits(p) || len(p) < minNationalDigits || len(p) > maxNationalDigits {
		return "", ErrInvalid
	}

	return "+" + countryCode + p[1:], nil
}

// withCountryCode accepts 84 followed by a 9 or 10 digit national number
// without its leading 0.
func withCountryCode(p string) (string, error) {
	rest := len(p) - len(countryCode)
	if !isDigits(p) || rest < minNationalDigits-1 || rest > maxNationalDigits-1 {
		return "", ErrInvalid
	}

	return "+" + p, nil
}

// SearchKey reduces what a person typed in a search box to the digits to
// look for inside stored E.164 numbers: "0912 345" and "+84 912 345" both
// give "912345", which is a substring of "+84912345678". It returns "" when
// there is nothing to search for or the query is not phone-like (it has
// letters, such as a license plate).
func SearchKey(query string) string {
	q := strings.TrimSpace(query)
	international := strings.HasPrefix(q, "+")

	var b strings.Builder

	for _, r := range formatting.Replace(strings.TrimPrefix(q, "+")) {
		if r < '0' || r > '9' {
			return ""
		}

		b.WriteRune(r)
	}

	key := b.String()

	switch {
	case international:
		key = strings.TrimPrefix(key, countryCode)
	case strings.HasPrefix(key, "0"):
		key = key[1:]
	}

	return key
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}

	return s != ""
}
