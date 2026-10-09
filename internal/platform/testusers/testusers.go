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

// ErrDomain is returned for a domain outside the reserved TLDs.
var ErrDomain = errors.New("test user domain must be under .test, .example, .invalid or .localhost")

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
