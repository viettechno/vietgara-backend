// Command loadseed creates the load-test population of Release 9.0 (the
// profile of the shared dataset's load section) in a running environment and
// writes the Population file the k6 script reads.
//
// It registers users on the reserved test domain only. Against the stubbed
// stack (loopback) the passwords are the dataset's; against staging they come
// from the environment variables the dataset names (passwordRef,
// adminPasswordRef), and the URLs must be https.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	testdata "github.com/viettechno/vietgara-testdata/go"

	"github.com/viettechno/vietgara-backend/testing/blackbox/client"
	"github.com/viettechno/vietgara-backend/testing/blackbox/loadseed"
	"github.com/viettechno/vietgara-backend/testing/blackbox/seed"
)

var (
	errUnknownTarget   = errors.New("target must be stubbed or staging")
	errNoPassword      = errors.New("the dataset has no password for this target")
	errSecretMissing   = errors.New("environment variable is empty")
	errStagingNotHTTPS = errors.New("staging URLs must be https")
)

func main() {
	if err := run(); err != nil {
		log.New(os.Stderr, "loadseed: ", 0).Print(err)
		os.Exit(1)
	}
}

func run() error {
	flags := flag.NewFlagSet("loadseed", flag.ContinueOnError)
	target := flags.String("target", "stubbed", "stubbed or staging")
	apiURL := flags.String("api-url", "http://localhost:18000", "API gateway URL")
	mailpitURL := flags.String("mailpit-url", "http://localhost:18025", "Mailpit API URL (staging: with the basic-auth credentials)")
	emailDomain := flags.String("email-domain", "blackbox.vietgara.test", "reserved test domain of the environment")
	dataDir := flags.String("data-dir", "../../.data/vietgara-testdata/data", "directory of the pinned dataset YAML files")
	profile := flags.String("profile", "", "load profile key (default: the only profile)")
	out := flags.String("out", "population.json", "where to write the population")

	if err := flags.Parse(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}

		return err
	}

	if err := seed.ValidateTestDomain(*emailDomain); err != nil {
		return fmt.Errorf("e-mail domain: %w", err)
	}

	dataset, err := testdata.Load(*dataDir)
	if err != nil {
		return fmt.Errorf("load shared dataset: %w", err)
	}

	password, adminPassword, err := passwords(*target, dataset, *apiURL, *mailpitURL)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()

	population, err := loadseed.Run(ctx, dataset, loadseed.Options{
		API: client.New(*apiURL), Inbox: client.NewMailpit(*mailpitURL), Profile: *profile,
		EmailDomain: *emailDomain, Password: password, AdminPassword: adminPassword,
		Log: func(format string, args ...any) { log.Printf(format, args...) },
	})
	if err != nil {
		return err
	}

	raw, err := json.MarshalIndent(population, "", "  ")
	if err != nil {
		return fmt.Errorf("encode population: %w", err)
	}

	if err := os.WriteFile(*out, raw, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", *out, err)
	}

	log.Printf("population of %d garages written to %s", len(population.Owners), *out)

	return nil
}

// passwords returns the owner and admin passwords of target after checking
// that the URLs fit it.
func passwords(target string, dataset *testdata.Dataset, apiURL, mailpitURL string) (string, string, error) {
	environment, ok := dataset.Environments[target]
	if !ok {
		return "", "", fmt.Errorf("%w: %q", errUnknownTarget, target)
	}

	switch target {
	case "stubbed":
		for _, raw := range []string{apiURL, mailpitURL} {
			if err := seed.ValidateLoopbackURL(raw); err != nil {
				return "", "", fmt.Errorf("%s: %w", raw, err)
			}
		}

		if environment.Password == "" || environment.AdminPassword == "" {
			return "", "", errNoPassword
		}

		return environment.Password, environment.AdminPassword, nil
	case "staging":
		for _, raw := range []string{apiURL, mailpitURL} {
			if parsed, err := url.Parse(raw); err != nil || parsed.Scheme != "https" {
				return "", "", fmt.Errorf("%w: %s", errStagingNotHTTPS, "api or mailpit")
			}
		}

		password, err := secret(environment.PasswordRef)
		if err != nil {
			return "", "", err
		}

		admin, err := secret(environment.AdminPasswordRef)

		return password, admin, err
	default:
		return "", "", fmt.Errorf("%w: %q", errUnknownTarget, target)
	}
}

func secret(name string) (string, error) {
	if name == "" {
		return "", errNoPassword
	}

	value := os.Getenv(name)
	if value == "" {
		return "", fmt.Errorf("%w: %s", errSecretMissing, name)
	}

	return value, nil
}
