package fixture

// Plan fixtures: the dataset platform admin and moving an owner onto a
// plan through the Release 5.2 bank-transfer payment flow.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"strings"
	"sync"
	"testing"

	identityv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/identity/v1"
	licensev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/license/v1"
	testdata "github.com/viettechno/vietgara-testdata/go"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/viettechno/vietgara-backend/testing/blackbox/client"
	"github.com/viettechno/vietgara-backend/testing/blackbox/env"
)

var errNoPublicPlan = errors.New("no public plan matches")

// adminMu serialises provisioning the dataset platform admin across the
// parallel scenarios: exactly one of them registers and verifies it.
var adminMu sync.Mutex

// PlatformAdmin returns the platform admin actor. On targets with a test
// inbox it is the shared dataset's admin user, admin@<email domain>, with
// the password of datasetAdminPassword. The stubbed stack registers and
// verifies it on first use; any other target must already have it (it is a
// platform admin only through the deployment's PLATFORM_ADMIN_EMAILS), so a
// failed sign-in there fails tb instead of registering a user. Targets
// without an inbox sign in BLACKBOX_ADMIN_EMAIL / BLACKBOX_ADMIN_PASSWORD,
// skipping tb when they are not configured.
func PlatformAdmin(tb testing.TB) *Actor {
	tb.Helper()

	cfg := env.Get(tb)
	if cfg.MailpitURL == "" {
		return preProvisionedAdmin(tb, cfg)
	}

	adminMu.Lock()
	defer adminMu.Unlock()

	user := datasetAdmin(tb)
	email := user.Key + "@" + cfg.EmailDomain
	password := datasetAdminPassword(tb)
	api := Anonymous(tb)

	session, err := api.Login(tb.Context(), email, password)
	if err == nil && session.GetUser().GetEmailVerified() {
		return &Actor{Email: email, Password: password, Session: session, API: api.WithToken(session.GetAccessToken())}
	}

	if cfg.Target != env.Stubbed {
		tb.Fatalf("sign in platform admin %s on %s (it must exist, be verified and have the dataset's password secret): verified=%t, err=%v",
			email, cfg.Target, session.GetUser().GetEmailVerified(), err)
	}

	return registerVerifiedUser(tb, api, email, password, user.Name)
}

// SwitchPlan moves owner onto the plan matching planName (code or display
// name) through the payment flow: it opens a plan-change request, declares
// the transfer of the exact amount due with screenshot evidence, and has
// the platform admin approve it. It returns the resulting subscription.
func SwitchPlan(tb testing.TB, owner *Actor, planName string) *licensev1.Subscription {
	tb.Helper()

	planID, err := publicPlanID(tb.Context(), owner.API, planName)
	if err != nil {
		tb.Fatalf("find plan %q: %v", planName, err)
	}

	return SwitchPlanByID(tb, owner, planID, planName)
}

// SwitchPlanByID is SwitchPlan for a plan referenced by ID, e.g. one the
// scenario's platform admin created; planName only labels error messages.
func SwitchPlanByID(tb testing.TB, owner *Actor, planID, planName string) *licensev1.Subscription {
	tb.Helper()

	ctx := tb.Context()

	request, err := owner.API.CreatePlanPaymentRequest(ctx, planID, licensev1.PlanPaymentKind_PLAN_PAYMENT_KIND_CHANGE)
	if err != nil {
		tb.Fatalf("create plan payment request for %q: %v", planName, err)
	}

	_, err = owner.API.SubmitPlanPayment(ctx, &licensev1.SubmitPlanPaymentRequest{
		RequestId:         request.GetId(),
		PayerName:         owner.Session.GetUser().GetFullName(),
		TransferredAmount: request.GetAmountDue(),
		PaidAt:            timestamppb.Now(),
		Evidence:          transferEvidence(),
	})
	if err != nil {
		tb.Fatalf("submit plan payment for %q: %v", planName, err)
	}

	sub, err := PlatformAdmin(tb).API.ApprovePlanPayment(ctx, request.GetId(), "")
	if err != nil {
		tb.Fatalf("approve plan payment for %q: %v", planName, err)
	}

	return sub
}

func preProvisionedAdmin(tb testing.TB, cfg env.Config) *Actor {
	tb.Helper()

	if cfg.AdminEmail == "" || cfg.AdminPassword == "" {
		tb.Skipf("BLACKBOX_ADMIN_EMAIL and BLACKBOX_ADMIN_PASSWORD are not set for target %s", cfg.Target)
	}

	api := Anonymous(tb)

	session, err := api.Login(tb.Context(), cfg.AdminEmail, cfg.AdminPassword)
	if err != nil {
		tb.Fatalf("sign in platform admin %s: %v", cfg.AdminEmail, err)
	}

	return &Actor{Email: cfg.AdminEmail, Password: cfg.AdminPassword, Session: session, API: api.WithToken(session.GetAccessToken())}
}

func datasetAdmin(tb testing.TB) testdata.User {
	tb.Helper()

	for _, user := range Dataset(tb).Users {
		if user.PlatformAdmin {
			return user
		}
	}

	tb.Fatal("shared dataset has no platform admin user")

	return testdata.User{}
}

func datasetAdminPassword(tb testing.TB) string {
	tb.Helper()

	cfg := env.Get(tb)
	if cfg.AdminPassword != "" {
		return cfg.AdminPassword
	}

	// The dataset gives each target its admin password: a literal for the
	// stubbed stack (public, like the dataset), or the name of the
	// environment variable holding the secret (staging: ADMIN_PASSWORD).
	environment, ok := Dataset(tb).Environments[string(cfg.Target)]
	if !ok {
		tb.Fatalf("the shared dataset has no environment %q", cfg.Target)
	}

	if environment.AdminPassword != "" {
		return environment.AdminPassword
	}

	if ref := environment.AdminPasswordRef; ref != "" {
		if password := os.Getenv(ref); password != "" {
			return password
		}

		tb.Fatalf("%s is not set: it holds the platform admin's password on target %s", ref, cfg.Target)
	}

	tb.Fatalf("the shared dataset has no admin password for target %s", cfg.Target)

	return ""
}

func registerVerifiedUser(tb testing.TB, api *client.Client, email, password, fullName string) *Actor {
	tb.Helper()

	registered, err := api.Register(tb.Context(), &identityv1.RegisterRequest{
		Email: email, Password: password, FullName: fullName,
	})
	if err != nil {
		tb.Fatalf("register %s: %v", email, err)
	}

	mail := Mailpit(tb)

	msg, err := mail.WaitForMessage(tb.Context(), email)
	if err != nil {
		tb.Fatalf("verification e-mail for %s: %v", email, err)
	}

	otp, err := msg.OTP()
	if err != nil {
		tb.Fatal(err)
	}

	verified, err := api.WithToken(registered.GetAccessToken()).VerifyEmail(tb.Context(), otp, registered.GetRefreshToken())
	if err != nil {
		tb.Fatalf("verify %s: %v", email, err)
	}

	return &Actor{Email: email, Password: password, Session: verified, API: api.WithToken(verified.GetAccessToken())}
}

func publicPlanID(ctx context.Context, api *client.Client, planName string) (string, error) {
	plans, err := api.ListPlans(ctx)
	if err != nil {
		return "", err
	}

	for _, plan := range plans {
		if strings.EqualFold(plan.GetName(), planName) || strings.EqualFold(plan.GetCode(), planName) {
			return plan.GetId(), nil
		}
	}

	return "", fmt.Errorf("%w: %q", errNoPublicPlan, planName)
}

// transferEvidence returns a valid 1x1 PNG transfer screenshot.
func transferEvidence() *licensev1.PaymentEvidence {
	var buf bytes.Buffer

	_ = png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 1, 1)))

	return &licensev1.PaymentEvidence{Content: buf.Bytes(), ContentType: "image/png"}
}

// LimitedPlan creates a public plan with the given daily transaction
// limit (the other limits are generous) and moves owner onto it, so a
// scenario can reach the limit with a handful of calls.
func LimitedPlan(tb testing.TB, owner *Actor, maxTransactionsPerDay int32) *licensev1.Subscription {
	tb.Helper()

	code := fmt.Sprintf("qa-limit-%d-%d", maxTransactionsPerDay, seq.Add(1))

	created, err := PlatformAdmin(tb).API.CreatePlan(tb.Context(), &licensev1.Plan{
		Code: code, Name: "QA " + code, Price: 100000, Currency: "VND",
		BillingCycle: licensev1.BillingCycle_BILLING_CYCLE_MONTHLY,
		Limits:       &licensev1.PlanLimits{MaxGarages: 5, MaxStaff: 10, MaxTransactionsPerDay: maxTransactionsPerDay},
		IsPublic:     true, IsActive: true,
	})
	if err != nil {
		tb.Fatalf("create plan %s: %v", code, err)
	}

	return SwitchPlanByID(tb, owner, created.GetId(), created.GetName())
}
