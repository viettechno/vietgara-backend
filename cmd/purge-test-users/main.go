// Package main provides the domain-scoped test-user purge command.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/viettechno/vietgara-backend/internal/identity/repository"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platform/mailpit"
	"github.com/viettechno/vietgara-backend/internal/platform/testusers"
)

type testUserRepository interface {
	ListTestUserEmails(context.Context, string, time.Time) ([]string, error)
	DeleteTestUsers(context.Context, string, time.Time) (int64, error)
}

type mailpitMessageDeleter interface {
	DeleteMessagesForRecipients(context.Context, []string) (int, error)
}

func main() {
	if err := execute(); err != nil {
		fmt.Fprintln(os.Stderr, "purge-test-users:", err)
		os.Exit(1)
	}
}

func execute() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return run(ctx, os.Args[1:], os.LookupEnv, os.Stdout)
}

func run(ctx context.Context, args []string, lookupEnv func(string) (string, bool), stdout io.Writer) error {
	flags := flag.NewFlagSet("purge-test-users", flag.ContinueOnError)
	confirmDomain := flags.String("confirm-domain", "", "required: exact TEST_USERS_EMAIL_DOMAIN to purge")

	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}

		return err
	}

	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}

	domain, err := confirmedDomain(lookupEnv, *confirmDomain)
	if err != nil {
		return err
	}

	mailpitURL, ok := lookupEnv("MAILPIT_API_URL")
	if !ok || mailpitURL == "" {
		return errors.New("MAILPIT_API_URL is required; purge is disabled")
	}

	mailpitUsername, _ := lookupEnv("MAILPIT_API_USERNAME")
	mailpitPassword, _ := lookupEnv("MAILPIT_API_PASSWORD")

	mailpitClient, err := mailpit.NewClient(mailpitURL, mailpitUsername, mailpitPassword)
	if err != nil {
		return fmt.Errorf("configure Mailpit client: %w", err)
	}

	dsn, ok := lookupEnv("DATABASE_URL")
	if !ok || dsn == "" {
		return errors.New("DATABASE_URL is required")
	}

	connPool, err := platformdb.NewPgxPool(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}

	pool := platformdb.NewPool(ctx, connPool)
	defer pool.Close()

	cutoff := time.Now()

	deleted, messages, err := purgeTestUsers(ctx, repository.NewPostgresUserRepository(pool), mailpitClient, domain.String(), cutoff)
	if err != nil {
		return fmt.Errorf("purge test users: %w", err)
	}

	if _, err := fmt.Fprintf(stdout, "Purged %d test users and %d Mailpit messages from %s\n", deleted, messages, domain.String()); err != nil {
		return fmt.Errorf("write purge summary: %w", err)
	}

	return nil
}

func purgeTestUsers(
	ctx context.Context,
	users testUserRepository,
	messages mailpitMessageDeleter,
	emailDomain string,
	cutoff time.Time,
) (int64, int, error) {
	recipients, err := users.ListTestUserEmails(ctx, emailDomain, cutoff)
	if err != nil {
		return 0, 0, fmt.Errorf("list test-user e-mail addresses: %w", err)
	}

	deletedMessages, err := messages.DeleteMessagesForRecipients(ctx, recipients)
	if err != nil {
		return 0, 0, fmt.Errorf("delete Mailpit messages: %w", err)
	}

	deletedUsers, err := users.DeleteTestUsers(ctx, emailDomain, cutoff)
	if err != nil {
		return deletedUsers, deletedMessages, fmt.Errorf("delete test users: %w", err)
	}

	return deletedUsers, deletedMessages, nil
}

func confirmedDomain(lookupEnv func(string) (string, bool), confirmation string) (testusers.Domain, error) {
	rawDomain, ok := lookupEnv("TEST_USERS_EMAIL_DOMAIN")
	if !ok || rawDomain == "" {
		return testusers.Domain{}, errors.New("TEST_USERS_EMAIL_DOMAIN is required; purge is disabled")
	}

	domain, err := testusers.Parse(rawDomain)
	if err != nil {
		return testusers.Domain{}, fmt.Errorf("invalid TEST_USERS_EMAIL_DOMAIN: %w", err)
	}

	if confirmation == "" || confirmation != domain.String() {
		return testusers.Domain{}, fmt.Errorf("--confirm-domain must exactly match TEST_USERS_EMAIL_DOMAIN %q", domain.String())
	}

	return domain, nil
}
