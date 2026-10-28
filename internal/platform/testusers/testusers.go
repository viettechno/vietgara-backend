// Package testusers recognises the users an automated suite signs up
// on a shared deployment (staging): every address on one e-mail domain,
// which must be under a TLD reserved for testing (RFC 2606, RFC 6761) so
// no real person can own one. The backend treats these users like any
// other, except that their e-mail goes to a test mailbox (mail.RouteDomain)
// and they are purged after a retention period.
package testusers

import (
	"errors"
	"fmt"
	"strings"
)

var reservedTLDs = []string{".test", ".example", ".invalid", ".localhost"}

var (
	// ErrDomain is returned for a domain outside the reserved TLDs.
	ErrDomain = errors.New("test user domain must be under .test, .example, .invalid or .localhost")
	// ErrEnvironment is returned by Allowed outside a developer machine or a
	// shared test deployment.
	ErrEnvironment = errors.New("test users are allowed only when APP_ENV is empty, local or staging")
)

// Allowed reports whether test users may be enabled where APP_ENV is
// appEnv: on a developer machine (empty, "local") or a shared test
// deployment ("staging"). Test users get their e-mail in a test inbox and
// are deleted, which production must never do, so every other value fails:
// "production", and also a value Allowed does not know ("prod", a typo).
// Every entry point that enables test users calls it.
func Allowed(appEnv string) error {
	switch strings.ToLower(strings.TrimSpace(appEnv)) {
	case "", "local", "staging":
		return nil
	default:
		return fmt.Errorf("%w (APP_ENV=%q)", ErrEnvironment, appEnv)
	}
}

// Domain is a validated test user e-mail domain; the zero value owns no
// address.
type Domain struct {
	name string
}

// Parse validates raw (for example "blackbox.vietgara.test").
func Parse(raw string) (Domain, error) {
	name := strings.ToLower(strings.TrimSpace(raw))
	if !valid(name) {
		return Domain{}, fmt.Errorf("%w: %q", ErrDomain, raw)
	}

	return Domain{name: name}, nil
}

func valid(name string) bool {
	if name == "" || strings.HasPrefix(name, ".") || strings.Contains(name, "..") || !hostnameChars(name) {
		return false
	}

	for _, tld := range reservedTLDs {
		if strings.HasSuffix(name, tld) && len(name) > len(tld) {
			return true
		}
	}

	return false
}

func hostnameChars(name string) bool {
	for _, r := range name {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '.' && r != '-' {
			return false
		}
	}

	return true
}

// String returns the domain, without "@".
func (d Domain) String() string { return d.name }

// IsZero reports whether d is unset.
func (d Domain) IsZero() bool { return d.name == "" }

// Owns reports whether email is on the domain.
func (d Domain) Owns(email string) bool {
	return d.name != "" && strings.HasSuffix(strings.ToLower(strings.TrimSpace(email)), "@"+d.name)
}
