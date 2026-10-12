// Package main seeds the supported baseline into a running stubbed stack.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/mail"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	identityv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/identity/v1"
	licensev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/license/v1"
	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"
	testdata "github.com/viettechno/vietgara-testdata/go"

	"github.com/viettechno/vietgara-backend/testing/blackbox/client"
	"github.com/viettechno/vietgara-backend/testing/blackbox/seed"
)

var (
	errUnexpectedPositionalArguments    = errors.New("unexpected positional arguments")
	errDatasetHasNoStubbedPassword      = errors.New("dataset has no stubbed password")
	errDatasetHasNoStubbedAdminPassword = errors.New("dataset has no stubbed admin password")
	errInvalidLoopbackURL               = errors.New("must be a plain HTTP URL without credentials, path, query, or fragment")
	errNonLoopbackURL                   = errors.New("must resolve to a loopback host; remote targets are not supported")
	errInvalidReservedTestDomain        = errors.New("must be a valid reserved test domain")
	errTestDomainMissingSuffix          = errors.New("must end in .test, .example, .invalid, or .localhost")
)

type apiClient struct {
	client *client.Client
}

func (a apiClient) Login(ctx context.Context, email, password string) (*identityv1.Session, error) {
	return a.client.Login(ctx, email, password)
}

func (a apiClient) Register(ctx context.Context, request *identityv1.RegisterRequest) (*identityv1.Session, error) {
	return a.client.Register(ctx, request)
}

func (a apiClient) VerifyEmail(ctx context.Context, otp, refreshToken string) (*identityv1.Session, error) {
	return a.client.VerifyEmail(ctx, otp, refreshToken)
}

func (a apiClient) MySubscription(ctx context.Context) (*licensev1.Subscription, error) {
	return a.client.MySubscription(ctx)
}

func (a apiClient) ListPlans(ctx context.Context) ([]*licensev1.Plan, error) {
	return a.client.ListPlans(ctx)
}

func (a apiClient) CreatePlanPaymentRequest(ctx context.Context, planID string,
	kind licensev1.PlanPaymentKind,
) (*licensev1.PlanPaymentRequest, error) {
	return a.client.CreatePlanPaymentRequest(ctx, planID, kind)
}

func (a apiClient) SubmitPlanPayment(ctx context.Context,
	req *licensev1.SubmitPlanPaymentRequest,
) (*licensev1.PlanPaymentRequest, error) {
	return a.client.SubmitPlanPayment(ctx, req)
}

func (a apiClient) ApprovePlanPayment(ctx context.Context, requestID, note string) (*licensev1.Subscription, error) {
	return a.client.ApprovePlanPayment(ctx, requestID, note)
}

func (a apiClient) ListGarages(ctx context.Context) ([]*tenantv1.Garage, error) {
	return a.client.ListGarages(ctx)
}

func (a apiClient) CreateGarage(ctx context.Context, garage *tenantv1.Garage) (*tenantv1.Garage, error) {
	return a.client.CreateGarage(ctx, garage)
}

func (a apiClient) WithToken(token string) seed.API {
	return apiClient{client: a.client.WithToken(token)}
}

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

	if err := validateLoopbackURL(*apiURL); err != nil {
		return fmt.Errorf("api url: %w", err)
	}

	if err := validateLoopbackURL(*mailpitURL); err != nil {
		return fmt.Errorf("mailpit url: %w", err)
	}

	if err := validateTestDomain(*emailDomain); err != nil {
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

	summary, err := seed.Run(ctx, dataset, apiClient{client: client.New(*apiURL)}, client.NewMailpit(*mailpitURL),
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

func validateLoopbackURL(raw string) error {
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

func validateTestDomain(domain string) error {
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
