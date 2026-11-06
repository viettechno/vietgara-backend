// Package domain holds the contact request of the public website (FR-WEB-02,
// FR-WEB-04).
package domain

import (
	"net/mail"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// Status of a request: support marks it handled by hand.
type Status string

// The two statuses of a request.
const (
	StatusNew     Status = "new"
	StatusHandled Status = "handled"
)

// Locales of the website.
const (
	LocaleVI = "vi"
	LocaleEN = "en"
)

// Limits of the fields, in characters; the columns are as wide.
const (
	maxName       = 120
	maxGarageName = 160
	maxEmail      = 254
	maxMessage    = 2000
	minPhoneDigit = 8
	maxPhone      = 32
)

// Field names as the API spells them.
const (
	fieldName       = "name"
	fieldGarageName = "garageName"
	fieldPhone      = "phone"
	fieldEmail      = "email"
	fieldMessage    = "message"
	fieldConsent    = "consent"
	fieldLocale     = "locale"
)

// Request is one contact or demo request.
type Request struct {
	ID         string
	Name       string
	GarageName string
	Phone      string
	Email      string
	Message    string
	Locale     string
	ConsentAt  time.Time
	// SourceIPHash is a keyed hash of the caller's address, never the address.
	SourceIPHash string
	UserAgent    string
	Status       Status
	CreatedAt    time.Time
}

// Input is what the website sends.
type Input struct {
	Name       string
	GarageName string
	Phone      string
	Email      string
	Message    string
	Consent    bool
	Locale     string
}

// Normalize trims the fields, defaults the locale to Vietnamese and returns the
// first invalid field as a validation error. Consent must be given: a request
// without it stores nothing (FR-WEB-02).
func (in Input) Normalize() (Input, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.GarageName = strings.TrimSpace(in.GarageName)
	in.Phone = strings.TrimSpace(in.Phone)
	in.Email = strings.TrimSpace(in.Email)
	in.Message = strings.TrimSpace(in.Message)
	in.Locale = strings.TrimSpace(in.Locale)

	if in.Locale == "" {
		in.Locale = LocaleVI
	}

	checks := []struct {
		field, issue string
		ok           bool
	}{
		{fieldName, apperror.IssueRequired, in.Name != ""},
		{fieldName, apperror.IssueInvalid, singleLine(in.Name, maxName)},
		{fieldGarageName, apperror.IssueInvalid, singleLine(in.GarageName, maxGarageName)},
		{fieldPhone, apperror.IssueRequired, in.Phone != ""},
		{fieldPhone, apperror.IssueInvalid, validPhone(in.Phone)},
		{fieldEmail, apperror.IssueInvalid, in.Email == "" || validEmail(in.Email)},
		{fieldMessage, apperror.IssueInvalid, utf8.RuneCountInString(in.Message) <= maxMessage && !hasControl(in.Message, true)},
		{fieldConsent, apperror.IssueRequired, in.Consent},
		{fieldLocale, apperror.IssueInvalid, in.Locale == LocaleVI || in.Locale == LocaleEN},
	}

	for _, c := range checks {
		if !c.ok {
			return Input{}, apperror.Validation(c.field, c.issue, "invalid contact request")
		}
	}

	return in, nil
}

// singleLine reports whether s is at most maxLen characters on one line,
// without control characters: the name goes in the subject of the e-mail to
// support.
func singleLine(s string, maxLen int) bool {
	return utf8.RuneCountInString(s) <= maxLen && !hasControl(s, false)
}

// hasControl reports control characters and the invisible ones that spoof how text is shown: line
// and paragraph separators, zero-width characters, byte order mark and bidirectional overrides.
func hasControl(s string, allowNewline bool) bool {
	for _, r := range s {
		if allowNewline && (r == '\n' || r == '\r' || r == '\t') {
			continue
		}

		if unicode.IsControl(r) || unicode.In(r, unicode.Zl, unicode.Zp) || spoofing(r) {
			return true
		}
	}

	return false
}

func spoofing(r rune) bool {
	return (r >= 0x200B && r <= 0x200F) || (r >= 0x202A && r <= 0x202E) || (r >= 0x2066 && r <= 0x2069) || r == 0xFEFF
}

func validPhone(s string) bool {
	if len(s) > maxPhone || hasControl(s, false) {
		return false
	}

	digits := 0

	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			digits++
		case strings.ContainsRune("+-.() ", r):
		default:
			return false
		}
	}

	return digits >= minPhoneDigit
}

func validEmail(s string) bool {
	if len(s) > maxEmail || hasControl(s, false) {
		return false
	}

	addr, err := mail.ParseAddress(s)

	return err == nil && addr.Address == s
}
