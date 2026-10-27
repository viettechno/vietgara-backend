// Package main seeds the supported baseline into a running stubbed stack.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	testdata "github.com/viettechno/vietgara-testdata/go"

	"github.com/viettechno/vietgara-backend/testing/blackbox/client"
	"github.com/viettechno/vietgara-backend/testing/blackbox/seed"
)

var (
	errUnexpectedPositionalArguments    = errors.New("unexpected positional arguments")
	errDatasetHasNoStubbedPassword      = errors.New("dataset has no stubbed password")
	errDatasetHasNoStubbedAdminPassword = errors.New("dataset has no stubbed admin password")
)

func main() {
	if err := run(); err != nil {
		log.New(os.Stderr, "seed: ", 0).Print(err)
		os.Exit(1)
	}
}

func run() error {
	flags := flag.NewFlagSet("seed", flag.ContinueOnError)
	apiURL := flags.String("api-url", "http://localhost:18000", "stubbed API URL (must be loopback)")
	mailpitURL := flags.String("mailpit-url", "http://localhost:18025", "Mailpit API URL (must be loopback)")
	dataDir := flags.String("data-dir", "../../.data/vietgara-testdata/data", "directory containing the pinned dataset YAML files")

	emailDomain := flags.String("email-domain", "blackbox.vietgara.test", "reserved test domain for seeded users")
	if err := flags.Parse(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}

		return err
	}

	if flags.NArg() != 0 {
		return errUnexpectedPositionalArguments
	}

	if err := seed.ValidateLoopbackURL(*apiURL); err != nil {
		return fmt.Errorf("api url: %w", err)
	}

	if err := seed.ValidateLoopbackURL(*mailpitURL); err != nil {
		return fmt.Errorf("mailpit url: %w", err)
	}

	if err := seed.ValidateTestDomain(*emailDomain); err != nil {
		return fmt.Errorf("e-mail domain: %w", err)
	}

	dataset, err := testdata.Load(*dataDir)
	if err != nil {
		return fmt.Errorf("load shared dataset: %w", err)
	}

	environment, ok := dataset.Environments["stubbed"]
	if !ok || environment.Password == "" {
		return errDatasetHasNoStubbedPassword
	}

	if environment.AdminPassword == "" {
		return errDatasetHasNoStubbedAdminPassword
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	summary, err := seed.Run(ctx, dataset, seed.NewClientAPI(client.New(*apiURL)), client.NewMailpit(*mailpitURL),
		*emailDomain, environment.Password, environment.AdminPassword)
	if err != nil {
		return err
	}

	if _, err := fmt.Fprintf(
		os.Stdout,
		"Seed complete: owners created=%d existing=%d; plans changed=%d; garages created=%d existing=%d\n",
		summary.CreatedOwners, summary.ExistingOwners, summary.ChangedPlans, summary.CreatedGarages, summary.ExistingGarages,
	); err != nil {
		return fmt.Errorf("write seed summary: %w", err)
	}

	return nil
}
