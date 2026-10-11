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
	"github.com/viettechno/vietgara-backend/internal/platform/storage"
	"github.com/viettechno/vietgara-backend/internal/platform/testusers"
)

type testUserRepository interface {
	ListTestUserEmails(context.Context, string, time.Time) ([]string, error)
	ListTestUserGarageIDs(context.Context, string, time.Time) ([]string, error)
	DeleteTestUsers(context.Context, string, time.Time) (int64, error)
}

type mailpitMessageDeleter interface {
	DeleteMessagesForRecipients(context.Context, []string) (int, error)
}

type uploadedFileDeleter interface {
	DeletePrefix(context.Context, string) (int64, error)
}

var (
	errUnexpectedPositionalArguments = errors.New("unexpected positional arguments")
	errMailpitAPIURLRequired         = errors.New("MAILPIT_API_URL is required; purge is disabled")
	errDatabaseURLRequired           = errors.New("DATABASE_URL is required")
	errTestUsersEmailDomainRequired  = errors.New("TEST_USERS_EMAIL_DOMAIN is required; purge is disabled")
	errConfirmDomainMismatch         = errors.New("--confirm-domain must exactly match TEST_USERS_EMAIL_DOMAIN")
)

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
		return errUnexpectedPositionalArguments
	}

	domain, err := confirmedDomain(lookupEnv, *confirmDomain)
	if err != nil {
		return err
	}

	mailpitURL, ok := lookupEnv("MAILPIT_API_URL")
	if !ok || mailpitURL == "" {
		return errMailpitAPIURLRequired
	}

	mailpitUsername, _ := lookupEnv("MAILPIT_API_USERNAME")
	mailpitPassword, _ := lookupEnv("MAILPIT_API_PASSWORD")

	mailpitClient, err := mailpit.NewClient(mailpitURL, mailpitUsername, mailpitPassword)
	if err != nil {
		return fmt.Errorf("configure Mailpit client: %w", err)
	}

	pool, err := newDatabasePool(ctx, lookupEnv)
	if err != nil {
		return err
	}
	defer pool.Close()

	files, err := newFileStore(ctx, lookupEnv)
	if err != nil {
		return fmt.Errorf("configure file storage: %w", err)
	}

	cutoff := time.Now()

	deleted, messages, fileCount, err := purgeTestUsers(
		ctx, repository.NewPostgresUserRepository(pool), mailpitClient, files, domain.String(), cutoff,
	)
	if err != nil {
		return fmt.Errorf("purge test users: %w", err)
	}

	if _, err := fmt.Fprintf(stdout, "Purged %d test users, %d Mailpit messages, and %d uploaded files from %s\n",
		deleted, messages, fileCount, domain.String()); err != nil {
		return fmt.Errorf("write purge summary: %w", err)
	}

	return nil
}

func newDatabasePool(ctx context.Context, lookupEnv func(string) (string, bool)) (*platformdb.Pool, error) {
	dsn, ok := lookupEnv("DATABASE_URL")
	if !ok || dsn == "" {
		return nil, errDatabaseURLRequired
	}

	connPool, err := platformdb.NewPgxPool(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("connect to database: %w", err)
	}

	return platformdb.NewPool(ctx, connPool), nil
}

func newFileStore(ctx context.Context, lookupEnv func(string) (string, bool)) (storage.Store, error) {
	return storage.New(ctx, storage.Config{
		Backend:       envOr(lookupEnv, "STORAGE_BACKEND", "local"),
		LocalDir:      envOr(lookupEnv, "STORAGE_LOCAL_DIR", "./.data/uploads"),
		PublicBaseURL: envOr(lookupEnv, "STORAGE_PUBLIC_BASE_URL", ""),
		Bucket:        envOr(lookupEnv, "STORAGE_GCS_BUCKET", ""),
	})
}

func purgeTestUsers(
	ctx context.Context,
	users testUserRepository,
	messages mailpitMessageDeleter,
	files uploadedFileDeleter,
	emailDomain string,
	cutoff time.Time,
) (int64, int, int64, error) {
	recipients, err := users.ListTestUserEmails(ctx, emailDomain, cutoff)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("list test-user e-mail addresses: %w", err)
	}

	garageIDs, err := users.ListTestUserGarageIDs(ctx, emailDomain, cutoff)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("list test-user garages: %w", err)
	}

	deletedMessages, err := messages.DeleteMessagesForRecipients(ctx, recipients)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("delete Mailpit messages: %w", err)
	}

	var deletedFiles int64

	for _, garageID := range garageIDs {
		count, err := files.DeletePrefix(ctx, "garages/"+garageID+"/")
		if err != nil {
			return 0, deletedMessages, deletedFiles, fmt.Errorf("delete uploaded files for garage %s: %w", garageID, err)
		}

		deletedFiles += count
	}

	deletedUsers, err := users.DeleteTestUsers(ctx, emailDomain, cutoff)
	if err != nil {
		return deletedUsers, deletedMessages, deletedFiles, fmt.Errorf("delete test users: %w", err)
	}

	return deletedUsers, deletedMessages, deletedFiles, nil
}

func confirmedDomain(lookupEnv func(string) (string, bool), confirmation string) (testusers.Domain, error) {
	rawDomain, ok := lookupEnv("TEST_USERS_EMAIL_DOMAIN")
	if !ok || rawDomain == "" {
		return testusers.Domain{}, errTestUsersEmailDomainRequired
	}

	domain, err := testusers.Parse(rawDomain)
	if err != nil {
		return testusers.Domain{}, fmt.Errorf("invalid TEST_USERS_EMAIL_DOMAIN: %w", err)
	}

	if confirmation == "" || confirmation != domain.String() {
		return testusers.Domain{}, fmt.Errorf("%w %q", errConfirmDomainMismatch, domain.String())
	}

	return domain, nil
}

func envOr(lookupEnv func(string) (string, bool), key, fallback string) string {
	value, ok := lookupEnv(key)
	if !ok || value == "" {
		return fallback
	}

	return value
}
