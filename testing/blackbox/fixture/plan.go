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
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

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
// the password of datasetAdminPassword. When it cannot sign in (a target that
// purges its test users, staging, deletes it a day after it was created) the
// user is signed up and verified through the test inbox; that address is on
// the test domain, so no real user can be created, and it becomes a platform
// admin only through the deployment's PLATFORM_ADMIN_EMAILS. Targets without
// an inbox sign in BLACKBOX_ADMIN_EMAIL / BLACKBOX_ADMIN_PASSWORD, skipping tb
// when they are not configured.
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

	return signUpAdmin(tb, api, email, password, user.Name)
}

// signUpAdmin registers and verifies the admin. The test packages run as
// separate processes, so another one may be doing the same: when the address
// is taken it waits for that process to finish and signs in.
func signUpAdmin(tb testing.TB, api *client.Client, email, password, fullName string) *Actor {
	tb.Helper()

	known := inboxIDs(tb, email)

	registered, err := api.Register(tb.Context(), &identityv1.RegisterRequest{Email: email, Password: password, FullName: fullName})
	if apiErr := client.AsAPIError(err); apiErr != nil && apiErr.Status == http.StatusConflict {
		return awaitAdmin(tb, api, email, password)
	}

	if err != nil {
		tb.Fatalf("register platform admin %s: %v", email, err)
	}

	admin := verifyFromInbox(tb, api, registered, email, password, known)
	requirePlatformAdmin(tb, admin)

	return admin
}

// awaitAdmin signs in the admin that another process is registering.
func awaitAdmin(tb testing.TB, api *client.Client, email, password string) *Actor {
	tb.Helper()

	ctx, cancel := context.WithTimeout(tb.Context(), time.Minute)
	defer cancel()

	for {
		session, err := api.Login(ctx, email, password)
		if err == nil && session.GetUser().GetEmailVerified() {
			admin := &Actor{Email: email, Password: password, Session: session, API: api.WithToken(session.GetAccessToken())}
			requirePlatformAdmin(tb, admin)

			return admin
		}

		select {
		case <-ctx.Done():
			tb.Fatalf("platform admin %s is taken but cannot sign in (is the password secret right? is it verified?): %v", email, err)
		case <-time.After(time.Second):
		}
	}
}

func requirePlatformAdmin(tb testing.TB, admin *Actor) {
	tb.Helper()

	if !admin.Session.GetUser().GetPlatformAdmin() {
		tb.Fatalf("%s is signed up but is not a platform admin: its address must be in the deployment's PLATFORM_ADMIN_EMAILS", admin.Email)
	}
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
		Evidence:          TransferEvidence(),
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

		tb.Fatalf("the platform admin's password is not set for target %s: set the variable the dataset's adminPasswordRef names", cfg.Target)
	}

	tb.Fatalf("the shared dataset has no admin password for target %s", cfg.Target)

	return ""
}

// inboxIDs lists the e-mails already in the inbox of address. A fixed address
// (the admin's) keeps the e-mails of its earlier sign-ups, so the code of
// this sign-up is the one that is not among them.
func inboxIDs(tb testing.TB, address string) map[string]bool {
	tb.Helper()

	messages, err := Mailpit(tb).Messages(tb.Context(), address)
	if err != nil {
		tb.Fatalf("read the inbox of %s: %v", address, err)
	}

	ids := make(map[string]bool, len(messages))
	for _, msg := range messages {
		ids[msg.ID] = true
	}

	return ids
}

// verifyFromInbox verifies the user with the code of the sign-up e-mail: the
// first e-mail that was not in the inbox before and carries a code.
func verifyFromInbox(tb testing.TB, api *client.Client, registered *identityv1.Session, email, password string, known map[string]bool) *Actor {
	tb.Helper()

	msg, err := Mailpit(tb).WaitForMatching(tb.Context(), email, func(m *client.Message) bool {
		_, otpErr := m.OTP()

		return !known[m.ID] && otpErr == nil
	})
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

// RetirePlanAfterTest takes the plan off sale when the test ends. Plans
// cannot be deleted, so a long-lived target would otherwise offer one more
// test plan to its owners after every run. Owners already on the plan keep it.
func RetirePlanAfterTest(tb testing.TB, admin *Actor, planID string) {
	tb.Helper()

	tb.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if _, err := admin.API.UpdatePlan(ctx, planID, &licensev1.Plan{IsActive: false}, "is_active"); err != nil {
			tb.Errorf("take the test plan off sale: %v", err)
		}
	})
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

// TransferEvidence returns a valid 1x1 PNG transfer screenshot.
func TransferEvidence() *licensev1.PaymentEvidence {
	var buf bytes.Buffer

	_ = png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 1, 1)))

	return &licensev1.PaymentEvidence{Content: buf.Bytes(), ContentType: "image/png"}
}

// LimitedPlan creates a public plan with the given daily transaction
// limit (the other limits are generous) that counts every kind of document,
// and moves owner onto it, so a scenario can reach the limit with a handful
// of calls.
func LimitedPlan(tb testing.TB, owner *Actor, maxTransactionsPerDay int32) *licensev1.Subscription {
	tb.Helper()

	return LimitedPlanCounting(tb, owner, maxTransactionsPerDay,
		licensev1.TransactionKind_TRANSACTION_KIND_REPAIR_ORDER,
		licensev1.TransactionKind_TRANSACTION_KIND_QUOTE,
		licensev1.TransactionKind_TRANSACTION_KIND_SETTLEMENT,
	)
}

// LimitedPlanCounting is LimitedPlan counting only the given kinds of document.
func LimitedPlanCounting(
	tb testing.TB, owner *Actor, maxTransactionsPerDay int32, kinds ...licensev1.TransactionKind,
) *licensev1.Subscription {
	tb.Helper()

	code := fmt.Sprintf("qa-limit-%d-%d-%d", maxTransactionsPerDay, seq.Add(1), time.Now().UnixNano()%1_000_000)

	created, err := PlatformAdmin(tb).API.CreatePlan(tb.Context(), &licensev1.Plan{
		Code: code, Name: "QA " + code, Price: 100000, Currency: "VND",
		BillingCycle:            licensev1.BillingCycle_BILLING_CYCLE_YEARLY,
		Limits:                  &licensev1.PlanLimits{MaxGarages: 5, MaxStaff: 10, MaxTransactionsPerDay: maxTransactionsPerDay},
		CountedTransactionKinds: kinds,
		IsPublic:                true, IsActive: true,
	})
	if err != nil {
		tb.Fatalf("create plan %s: %v", code, err)
	}

	return SwitchPlanByID(tb, owner, created.GetId(), created.GetName())
}
