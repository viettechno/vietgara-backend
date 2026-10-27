package seed

import (
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/url"
	"strings"
)

var (
	errInvalidLoopbackURL        = errors.New("must be a plain HTTP URL without credentials, path, query, or fragment")
	errNonLoopbackURL            = errors.New("must resolve to a loopback host; remote targets are not supported")
	errInvalidReservedTestDomain = errors.New("must be a valid reserved test domain")
	errTestDomainMissingSuffix   = errors.New("must end in .test, .example, .invalid, or .localhost")
)

// ValidateLoopbackURL accepts only http://localhost or a loopback address, so a
// seeder pointed at the stubbed stack can never reach another host.
func ValidateLoopbackURL(raw string) error {
	parsed, err := url.ParseRequestURI(raw)
	if err != nil {
		return fmt.Errorf("parse URL: %w", err)
	}

	if parsed.Scheme != "http" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" ||
		(parsed.Path != "" && parsed.Path != "/") {
		return errInvalidLoopbackURL
	}

	host := parsed.Hostname()
	if host != "localhost" {
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return errNonLoopbackURL
		}
	}

	return nil
}

// ValidateTestDomain accepts only a reserved test domain, whose mail the test
// environments capture instead of delivering.
func ValidateTestDomain(domain string) error {
	if strings.TrimSpace(domain) != domain || strings.ContainsAny(domain, "@/ ") {
		return errInvalidReservedTestDomain
	}

	address, err := mail.ParseAddress("seed@" + domain)
	if err != nil || address.Address != "seed@"+domain {
		return errInvalidReservedTestDomain
	}

	for _, suffix := range []string{".test", ".example", ".invalid", ".localhost"} {
		if strings.HasSuffix(strings.ToLower(domain), suffix) {
			return nil
		}
	}

	return errTestDomainMissingSuffix
}
