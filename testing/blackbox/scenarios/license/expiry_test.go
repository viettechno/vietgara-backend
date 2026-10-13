// Scenario: E2E-LIC-010, E2E-LIC-011, E2E-LIC-012 (partial)
// docs: vietgara-docs/docs/05-Testing/E2E_Scenarios/03_License_Subscription.md
package license_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"
	identityv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/identity/v1"
	licensev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/license/v1"
	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/client"
	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
	"github.com/viettechno/vietgara-backend/testing/blackbox/timeshift"
)

const expiryWait = 90 * time.Second

func mailWithSubject(fragment string) func(*client.Message) bool {
	return func(m *client.Message) bool {
		return strings.Contains(strings.ToLower(m.Subject), strings.ToLower(fragment))
	}
}

// TestSubscriptionExpiryLifecycle walks one owner through the three
// documented states, in order, because each starts where the previous
// one ended: the period ends (past due, read-only), the grace period ends
// (suspended, locked, an admin extension restores it), and the period
// ends again and the owner renews by choosing a plan.
//
// The stored dates are moved with the documented SQL helpers (timeshift),
// so it runs on the stubbed stack only. The banners and the renew button
// are web-E2E.
//
// Scenario: E2E-LIC-010 (partial): the period ends, the garages become
// read-only for the owner and staff, and one e-mail says so.
// Scenario: E2E-LIC-011 (partial): the grace period ends, the garages are
// locked, a second e-mail arrives, and an admin extension restores them.
// Scenario: E2E-LIC-012 (partial): from read-only the owner renews by
// choosing a plan and can write again.
func TestSubscriptionExpiryLifecycle(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	mail := fixture.Mailpit(t)
	admin := fixture.PlatformAdmin(t)

	owner := fixture.NewVerifiedOwnerWithLocale(t, "lic-expiry", identityv1.Locale_LOCALE_EN)

	garage, err := owner.API.CreateGarage(ctx, &tenantv1.Garage{Name: "Blackbox Expiry"})
	require.NoError(t, err)

	phone := "0911000010"
	em, err := owner.API.CreateCustomer(ctx, garage.GetId(),
		&customerv1.Customer{FullName: "QA Customer Em Vo", Phone: &phone, PersonalDataConsent: true})
	require.NoError(t, err)

	// A staff member, to see what staff get.
	groups, err := owner.API.ListStaffGroups(ctx, garage.GetId())
	require.NoError(t, err)

	var advisorGroup string

	for _, g := range groups {
		if g.GetName() == "Service Advisor" {
			advisorGroup = g.GetId()
		}
	}

	require.NotEmpty(t, advisorGroup, "default Service Advisor group")

	staff := fixture.NewVerifiedOwner(t, "lic-expiry-staff")
	cand, err := owner.API.LookupStaffCandidate(ctx, garage.GetId(), staff.Email)
	require.NoError(t, err)

	inv, err := owner.API.CreateInvitation(ctx, garage.GetId(), cand.GetUserId(), []string{advisorGroup})
	require.NoError(t, err)

	_, err = staff.API.AcceptInvitation(ctx, inv.GetId())
	require.NoError(t, err)

	// --- E2E-LIC-010: the period ends -> past due, read-only.
	timeshift.EndPeriod(t, owner.Email)

	sub := fixture.WaitForSubscriptionStatus(t, owner, licensev1.SubscriptionStatus_SUBSCRIPTION_STATUS_PAST_DUE, expiryWait)
	require.NotNil(t, sub.GetGracePeriodEnd())
	grace := sub.GetGracePeriodEnd().AsTime().Sub(sub.GetCurrentPeriodEnd().AsTime())
	assert.InDelta(t, (7 * 24 * time.Hour).Seconds(), grace.Seconds(), 3600, "the grace period is 7 days")

	pastDue, err := mail.WaitForMatching(ctx, owner.Email, mailWithSubject("read-only"))
	require.NoError(t, err)
	assert.Contains(t, pastDue.Text, "read-only")

	list, err := owner.API.ListCustomers(ctx, garage.GetId())
	require.NoError(t, err, "reading is still allowed")
	require.NotEmpty(t, list)

	// The entitlement cache lives for a second (ENTITLEMENT_CACHE_TTL), so
	// the first write after the status change may still be let through.
	other := "0911000011"
	probe := "0911000099"

	require.Eventually(t, func() bool {
		_, createErr := owner.API.CreateCustomer(ctx, garage.GetId(),
			&customerv1.Customer{FullName: "QA Customer Probe", Phone: &probe, PersonalDataConsent: true})

		return createErr != nil
	}, 15*time.Second, time.Second, "writes are refused once the entitlement cache catches up")

	_, err = owner.API.CreateCustomer(ctx, garage.GetId(),
		&customerv1.Customer{FullName: "QA Customer Giang Do", Phone: &other, PersonalDataConsent: true})
	fixture.RequireAPIError(t, err, http.StatusForbidden, "SUBSCRIPTION_READ_ONLY")

	address := "1 Nguyen Hue"
	_, err = owner.API.UpdateCustomer(ctx, garage.GetId(), em.GetId(), &customerv1.Customer{Address: &address}, "address")
	fixture.RequireAPIError(t, err, http.StatusForbidden, "SUBSCRIPTION_READ_ONLY")

	// The garage profile and settings are deliberately exempt from the
	// license check (Get/UpdateGarage skip it), so the owner can still edit
	// them while read-only; its business data cannot be changed.
	newPhone := "0900111222"
	_, err = owner.API.UpdateGarage(ctx, garage.GetId(), &tenantv1.Garage{Phone: &newPhone}, "phone")
	require.NoError(t, err, "garage profile edits stay allowed")

	_, err = staff.API.ListCustomers(ctx, garage.GetId())
	require.NoError(t, err, "staff can read too")

	// --- E2E-LIC-011: the grace period ends -> suspended, locked.
	timeshift.EndGrace(t, owner.Email)

	fixture.WaitForSubscriptionStatus(t, owner, licensev1.SubscriptionStatus_SUBSCRIPTION_STATUS_SUSPENDED, expiryWait)

	_, err = mail.WaitForMatching(ctx, owner.Email, mailWithSubject("suspended"))
	require.NoError(t, err, "a second e-mail when the status becomes suspended")

	_, err = owner.API.ListCustomers(ctx, garage.GetId())
	fixture.RequireAPIError(t, err, http.StatusPaymentRequired, "SUBSCRIPTION_SUSPENDED")

	_, err = staff.API.ListCustomers(ctx, garage.GetId())
	fixture.RequireAPIError(t, err, http.StatusPaymentRequired, "SUBSCRIPTION_SUSPENDED")

	found, err := admin.API.ListSubscriptions(ctx, owner.Email, licensev1.SubscriptionStatus_SUBSCRIPTION_STATUS_SUSPENDED)
	require.NoError(t, err)
	require.NotEmpty(t, found, "the admin list shows the subscription as suspended")

	_, err = admin.API.ExtendSubscription(ctx, found[0].GetSubscription().GetId(), 30)
	require.NoError(t, err)

	restored, err := owner.API.MySubscription(ctx)
	require.NoError(t, err)
	assert.Equal(t, licensev1.SubscriptionStatus_SUBSCRIPTION_STATUS_ACTIVE, restored.GetStatus())

	again, err := owner.API.CreateCustomer(ctx, garage.GetId(),
		&customerv1.Customer{FullName: "QA Customer Giang Do", Phone: &other, PersonalDataConsent: true})
	require.NoError(t, err, "writing works again and the data was kept")
	assert.NotEmpty(t, again.GetId())

	// --- E2E-LIC-012: past due again, renew by choosing a plan.
	timeshift.EndPeriod(t, owner.Email)
	fixture.WaitForSubscriptionStatus(t, owner, licensev1.SubscriptionStatus_SUBSCRIPTION_STATUS_PAST_DUE, expiryWait)

	renewed := fixture.SwitchPlan(t, owner, "starter")
	assert.Equal(t, licensev1.SubscriptionStatus_SUBSCRIPTION_STATUS_ACTIVE, renewed.GetStatus())
	assert.True(t, renewed.GetCurrentPeriodEnd().AsTime().After(time.Now().Add(20*24*time.Hour)),
		"valid for about another month")

	_, err = owner.API.UpdateCustomer(ctx, garage.GetId(), em.GetId(), &customerv1.Customer{Address: &address}, "address")
	require.NoError(t, err, "editing is allowed again")
}

// Scenario: E2E-LIC-009: the expiring-soon reminder is e-mailed once for a
// period end within seven days, in the owner's language.
func TestExpiringSoonReminder(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	mail := fixture.Mailpit(t)

	owner := fixture.NewVerifiedOwnerWithLocale(t, "lic-reminder", identityv1.Locale_LOCALE_EN)
	_, err := owner.API.CreateGarage(ctx, &tenantv1.Garage{Name: "Blackbox Reminder"})
	require.NoError(t, err)

	timeshift.ExpireSoon(t, owner.Email)

	reminder, err := mail.WaitForMatching(ctx, owner.Email, mailWithSubject("plan ends on"))
	require.NoError(t, err)
	assert.Contains(t, strings.ToLower(reminder.Text), "renew")

	// No second reminder for the same period end: let the expiry job run
	// several times, then count.
	time.Sleep(20 * time.Second)

	all, err := mail.Messages(ctx, owner.Email)
	require.NoError(t, err)

	reminders := 0

	for _, m := range all {
		if mailWithSubject("plan ends on")(m) {
			reminders++
		}
	}

	assert.Equal(t, 1, reminders, "exactly one reminder per period end")
}
