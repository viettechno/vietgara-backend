package platform_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"
	platformv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/platform/v1"
	supportv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/support/v1"
	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/client"
	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

const (
	supportRead  = platformv1.PlatformPermission_PLATFORM_PERMISSION_SUPPORT_READ
	supportWrite = platformv1.PlatformPermission_PLATFORM_PERMISSION_SUPPORT_WRITE
	directory    = platformv1.PlatformPermission_PLATFORM_PERMISSION_GARAGE_DIRECTORY_READ

	statusActive  = supportv1.SupportGrantStatus_SUPPORT_GRANT_STATUS_ACTIVE
	statusPending = supportv1.SupportGrantStatus_SUPPORT_GRANT_STATUS_PENDING
	statusRevoked = supportv1.SupportGrantStatus_SUPPORT_GRANT_STATUS_REVOKED
)

// supportCase is an owner with a garage and a customer, and an admin staff
// member holding the support permissions.
type supportCase struct {
	owner    *fixture.Actor
	garage   *tenantv1.Garage
	customer *customerv1.Customer
	staff    *fixture.Actor
	team     *team
}

func newSupportCase(t *testing.T, label string) *supportCase {
	t.Helper()

	ctx := t.Context()
	c := &supportCase{owner: fixture.NewVerifiedOwner(t, label+"-o"), staff: fixture.NewVerifiedOwner(t, label+"-s")}

	var err error

	c.garage, err = c.owner.API.CreateGarage(ctx, &tenantv1.Garage{Name: uniqueName("Support " + label)})
	require.NoError(t, err)

	phone := "09" + label[len(label)-1:] + "1234567"
	c.customer, err = c.owner.API.CreateCustomer(ctx, c.garage.GetId(), &customerv1.Customer{FullName: "Khách hỗ trợ", Phone: &phone, PersonalDataConsent: true})
	require.NoError(t, err)

	c.team = newTeam(t, "support", supportRead, supportWrite, directory)
	c.team.join(t, fixture.PlatformAdmin(t), c.staff)

	return c
}

// Scenario: E2E-PLT-006: the Definition of Done of Release 8.2. Admin staff
// request access to a garage, the owner is e-mailed and approves, the staff
// member reads in a read-only session, the owner sees the reads in the
// Support access log, revokes the grant, and the next request is refused.
func TestSupportGrantApprovedReadAndRevoked(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	c := newSupportCase(t, "plt006")
	gid := c.garage.GetId()

	_, err := c.staff.API.GetCustomer(ctx, gid, c.customer.GetId())
	fixture.RequireAPIError(t, err, http.StatusForbidden, "GARAGE_ACCESS_DENIED")

	found, err := c.staff.API.ListGarageDirectory(ctx, c.garage.GetName())
	require.NoError(t, err)
	require.NotEmpty(t, found)
	assert.Equal(t, gid, found[0].GetGarageId())
	assert.Equal(t, c.owner.Email, found[0].GetOwnerEmail())

	grant, err := c.staff.API.RequestSupportGrant(ctx, gid, "ticket 4711", 2)
	require.NoError(t, err)
	assert.Equal(t, statusPending, grant.GetStatus())
	assert.EqualValues(t, 2, grant.GetTtlHours())

	message, err := fixture.Mailpit(t).WaitForMatching(ctx, c.owner.Email, func(m *client.Message) bool {
		return strings.Contains(m.Text, "ticket 4711")
	})
	require.NoError(t, err)
	assert.Contains(t, message.Text, "support-access?garageId="+gid)

	_, err = c.staff.API.GetCustomer(ctx, gid, c.customer.GetId())
	fixture.RequireAPIError(t, err, http.StatusForbidden, "GARAGE_ACCESS_DENIED")

	_, err = c.staff.API.ApproveSupportGrant(ctx, gid, grant.GetId())
	fixture.RequireAPIError(t, err, http.StatusForbidden, "GARAGE_ACCESS_DENIED")

	approved, err := c.owner.API.ApproveSupportGrant(ctx, gid, grant.GetId())
	require.NoError(t, err)
	assert.Equal(t, statusActive, approved.GetStatus())
	assert.NotNil(t, approved.GetExpiresAt())

	mine, err := c.staff.API.ListSupportGrants(ctx)
	require.NoError(t, err)
	require.Len(t, mine, 1)
	assert.Equal(t, statusActive, mine[0].GetStatus())
	assert.Equal(t, c.garage.GetName(), mine[0].GetGarageName())

	sessions, err := c.staff.API.ListMySupportSessions(ctx)
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	assert.Equal(t, gid, sessions[0].GetGarageId())

	garage, err := c.staff.API.GetGarage(ctx, gid)
	require.NoError(t, err)
	assert.False(t, garage.GetAccess().GetOwner(), "a session is never the owner")
	assert.NotContains(t, garage.GetAccess().GetPermissions(), tenantv1.Permission_PERMISSION_QUOTE_WRITE)

	got, err := c.staff.API.GetCustomer(ctx, gid, c.customer.GetId())
	require.NoError(t, err)
	assert.Equal(t, c.customer.GetFullName(), got.GetFullName())

	_, err = c.staff.API.ListSettlements(ctx, gid)
	require.NoError(t, err)

	log, err := c.owner.API.ListSupportAccessLog(ctx, gid, 50)
	require.NoError(t, err)
	require.Len(t, log, 3)
	assert.Equal(t, "ListSettlements", log[0].GetMethod())
	assert.Equal(t, "GetCustomer", log[1].GetMethod())
	assert.Equal(t, c.customer.GetId(), log[1].GetEntityId())
	assert.Equal(t, "customer", log[1].GetEntityType())
	assert.Equal(t, grant.GetId(), log[1].GetGrantId())
	assert.Equal(t, "GetGarage", log[2].GetMethod())

	revoked, err := c.owner.API.RevokeSupportGrant(ctx, gid, grant.GetId())
	require.NoError(t, err)
	assert.Equal(t, statusRevoked, revoked.GetStatus())

	_, err = c.staff.API.GetCustomer(ctx, gid, c.customer.GetId())
	fixture.RequireAPIError(t, err, http.StatusForbidden, "GARAGE_ACCESS_DENIED")

	grants, err := c.owner.API.ListGarageSupportGrants(ctx, gid)
	require.NoError(t, err)
	require.Len(t, grants, 1)
	assert.Equal(t, statusRevoked, grants[0].GetStatus())
}

// Scenario: E2E-PLT-007: a Platform Admin opens a garage with a reason only:
// the grant is active at once, the owner is notified and sees the reads.
func TestPlatformAdminOpensGarageWithAReason(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	admin := fixture.PlatformAdmin(t)
	owner := fixture.NewVerifiedOwner(t, "plt007")

	garage, err := owner.API.CreateGarage(ctx, &tenantv1.Garage{Name: "Support plt007"})
	require.NoError(t, err)

	_, err = admin.API.RequestSupportGrant(ctx, garage.GetId(), "  ", 1)
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "VALIDATION_ERROR")

	_, err = admin.API.RequestSupportGrant(ctx, garage.GetId(), "incident 12", 200)
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "VALIDATION_ERROR")

	grant, err := admin.API.RequestSupportGrant(ctx, garage.GetId(), "incident 12", 0)
	require.NoError(t, err)
	assert.Equal(t, statusActive, grant.GetStatus())
	assert.True(t, grant.GetGrantedByPlatformAdmin())
	assert.EqualValues(t, 24, grant.GetTtlHours())

	_, err = admin.API.RequestSupportGrant(ctx, garage.GetId(), "again", 1)
	fixture.RequireAPIError(t, err, http.StatusConflict, "GRANT_ALREADY_OPEN")

	message, err := fixture.Mailpit(t).WaitForMatching(ctx, owner.Email, func(m *client.Message) bool {
		return strings.Contains(m.Text, "incident 12")
	})
	require.NoError(t, err)
	assert.Contains(t, message.Text, "support-access?garageId="+garage.GetId())

	_, err = admin.API.GetGarage(ctx, garage.GetId())
	require.NoError(t, err)

	log, err := owner.API.ListSupportAccessLog(ctx, garage.GetId(), 50)
	require.NoError(t, err)
	require.NotEmpty(t, log)
	assert.Equal(t, "GetGarage", log[0].GetMethod())

	ended, err := admin.API.EndSupportGrant(ctx, grant.GetId())
	require.NoError(t, err)
	assert.Equal(t, statusRevoked, ended.GetStatus())

	_, err = admin.API.GetGarage(ctx, garage.GetId())
	fixture.RequireAPIError(t, err, http.StatusForbidden, "GARAGE_ACCESS_DENIED")
}

// Scenario: E2E-PLT-008: a support session is read-only and cannot act as
// the owner; it ends when the staff member loses the support permission.
func TestSupportSessionIsReadOnly(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	c := newSupportCase(t, "plt008")
	gid := c.garage.GetId()

	grant, err := c.staff.API.RequestSupportGrant(ctx, gid, "ticket 8", 1)
	require.NoError(t, err)

	_, err = c.owner.API.ApproveSupportGrant(ctx, gid, grant.GetId())
	require.NoError(t, err)

	_, err = c.staff.API.UpdateCustomer(ctx, gid, c.customer.GetId(), &customerv1.Customer{FullName: "Changed"}, "full_name")
	fixture.RequireAPIError(t, err, http.StatusForbidden, "SUPPORT_READ_ONLY")

	phone := "0911111111"
	_, err = c.staff.API.CreateCustomer(ctx, gid, &customerv1.Customer{FullName: "New", Phone: &phone, PersonalDataConsent: true})
	fixture.RequireAPIError(t, err, http.StatusForbidden, "SUPPORT_READ_ONLY")

	_, err = c.staff.API.UpdateGarage(ctx, gid, &tenantv1.Garage{Name: "Hijacked"}, "name")
	fixture.RequireAPIError(t, err, http.StatusForbidden, "SUPPORT_READ_ONLY")

	// What only the owner may read stays the owner's.
	_, err = c.staff.API.ListGarageSupportGrants(ctx, gid)
	fixture.RequireAPIError(t, err, http.StatusForbidden, "OWNER_ONLY")

	_, err = c.staff.API.ListSupportAccessLog(ctx, gid, 10)
	fixture.RequireAPIError(t, err, http.StatusForbidden, "OWNER_ONLY")

	unchanged, err := c.owner.API.GetCustomer(ctx, gid, c.customer.GetId())
	require.NoError(t, err)
	assert.Equal(t, c.customer.GetFullName(), unchanged.GetFullName())

	// Another garage is not covered by the grant.
	other, err := fixture.NewVerifiedOwner(t, "plt008-b").API.CreateGarage(ctx, &tenantv1.Garage{Name: "Support plt008 b"})
	require.NoError(t, err)

	_, err = c.staff.API.GetGarage(ctx, other.GetId())
	fixture.RequireAPIError(t, err, http.StatusForbidden, "GARAGE_ACCESS_DENIED")

	// Removed from the group: the session ends with the next request.
	require.NoError(t, fixture.PlatformAdmin(t).API.RemovePlatformStaffMember(ctx, c.team.group.GetId(), c.staff.Session.GetUser().GetId()))

	_, err = c.staff.API.GetCustomer(ctx, gid, c.customer.GetId())
	fixture.RequireAPIError(t, err, http.StatusForbidden, "GARAGE_ACCESS_DENIED")
}

// Scenario: E2E-PLT-009: the owner can decline a request; a declined or
// another garage's grant cannot be approved; the staff cannot ask twice.
func TestOwnerDeclinesAndTheRulesOfTheLifecycle(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	c := newSupportCase(t, "plt009")
	gid := c.garage.GetId()

	grant, err := c.staff.API.RequestSupportGrant(ctx, gid, "ticket 9", 1)
	require.NoError(t, err)

	_, err = c.staff.API.RequestSupportGrant(ctx, gid, "ticket 9 again", 1)
	fixture.RequireAPIError(t, err, http.StatusConflict, "GRANT_ALREADY_OPEN")

	stranger := fixture.NewVerifiedOwner(t, "plt009-x")
	_, err = stranger.API.ApproveSupportGrant(ctx, gid, grant.GetId())
	fixture.RequireAPIError(t, err, http.StatusForbidden, "GARAGE_ACCESS_DENIED")

	declined, err := c.owner.API.RevokeSupportGrant(ctx, gid, grant.GetId())
	require.NoError(t, err)
	assert.Equal(t, statusRevoked, declined.GetStatus())

	_, err = c.owner.API.ApproveSupportGrant(ctx, gid, grant.GetId())
	fixture.RequireAPIError(t, err, http.StatusConflict, "GRANT_NOT_PENDING")

	_, err = c.owner.API.RevokeSupportGrant(ctx, gid, grant.GetId())
	fixture.RequireAPIError(t, err, http.StatusConflict, "GRANT_NOT_OPEN")

	_, err = c.staff.API.EndSupportGrant(ctx, grant.GetId())
	fixture.RequireAPIError(t, err, http.StatusConflict, "GRANT_NOT_OPEN")

	_, err = c.staff.API.GetCustomer(ctx, gid, c.customer.GetId())
	fixture.RequireAPIError(t, err, http.StatusForbidden, "GARAGE_ACCESS_DENIED")

	// A user without the support permissions cannot ask at all.
	_, err = stranger.API.RequestSupportGrant(ctx, gid, "no right", 1)
	fixture.RequireAPIError(t, err, http.StatusForbidden, "PLATFORM_PERMISSION_REQUIRED")

	_, err = stranger.API.ListGarageDirectory(ctx, "x")
	fixture.RequireAPIError(t, err, http.StatusForbidden, "PLATFORM_PERMISSION_REQUIRED")
}

// Scenario: E2E-PLT-010: the diagnostic views of a session show the outbox
// and the notification log, need an active grant of the caller, and are
// logged like every other read.
func TestSupportDiagnosticViews(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	c := newSupportCase(t, "plt010")
	gid := c.garage.GetId()

	grant, err := c.staff.API.RequestSupportGrant(ctx, gid, "ticket 10", 1)
	require.NoError(t, err)

	_, err = c.staff.API.ListSupportOutboxEvents(ctx, grant.GetId())
	fixture.RequireAPIError(t, err, http.StatusConflict, "GRANT_NOT_ACTIVE")

	_, err = c.owner.API.ApproveSupportGrant(ctx, gid, grant.GetId())
	require.NoError(t, err)

	_, err = c.staff.API.ListSupportOutboxEvents(ctx, grant.GetId())
	require.NoError(t, err)

	// The log row is written after the e-mail is sent, so it can trail the
	// verification the owner already completed. Every poll is a read
	// through the grant and is audited (even one that then fails).
	require.Eventually(t, func() bool {
		notifications, listErr := c.staff.API.ListSupportNotifications(ctx, grant.GetId())

		return listErr == nil && len(notifications) > 0
	}, 10*time.Second, 200*time.Millisecond, "the owner's registration e-mails are in the log")

	// Another staff member cannot use this grant.
	other := fixture.NewVerifiedOwner(t, "plt010-x")
	c.team.join(t, fixture.PlatformAdmin(t), other)

	_, err = other.API.ListSupportNotifications(ctx, grant.GetId())
	fixture.RequireAPIError(t, err, http.StatusNotFound, "")

	// Newest first: the notification polls, then the one outbox read. The
	// other staff member's refused read is not on the grant's log.
	log, err := c.owner.API.ListSupportAccessLog(ctx, gid, 100)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(log), 2)
	require.Less(t, len(log), 100, "the whole log fits in the page")
	assert.Equal(t, "ListSupportOutboxEvents", log[len(log)-1].GetMethod())

	for _, entry := range log[:len(log)-1] {
		assert.Equal(t, "ListSupportNotifications", entry.GetMethod())
	}
}
