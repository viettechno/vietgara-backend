package access_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"

	"github.com/viettechno/vietgara-backend/internal/platform/access"
)

func TestCatalog(t *testing.T) {
	t.Parallel()

	all := access.All()
	require.Len(t, all, 18)
	assert.Equal(t, access.GarageWrite, all[0])
	assert.True(t, access.CustomerRead.IsValid())
	assert.False(t, access.Permission("customer.delete").IsValid())

	assert.Equal(t,
		[]access.Permission{access.StaffRead, access.CustomerRead, access.ReportRead},
		access.Normalize([]access.Permission{access.ReportRead, "bogus", access.CustomerRead, access.StaffRead, access.CustomerRead}))

	// A write permission includes its module's read permission, when the
	// module has one (garage has no read: every member may read it).
	assert.Equal(t,
		[]access.Permission{access.GarageWrite, access.StaffRead, access.StaffWrite, access.BillingRead, access.BillingWrite},
		access.Normalize([]access.Permission{access.BillingWrite, access.StaffWrite, access.GarageWrite}))

	assert.Equal(t, "read", access.CustomerRead.Action())
	assert.Equal(t, "write", access.GarageWrite.Action())
}

func TestProtoMapping(t *testing.T) {
	t.Parallel()

	for _, p := range access.All() {
		pb := access.ToProto(p)
		require.NotEqual(t, tenantv1.Permission_PERMISSION_UNSPECIFIED, pb, p)

		back, ok := access.FromProto(pb)
		require.True(t, ok)
		assert.Equal(t, p, back)
	}

	assert.Equal(t, tenantv1.Permission_PERMISSION_REPAIR_ORDER_WRITE, access.ToProto(access.RepairOrderWrite))

	_, ok := access.FromProto(tenantv1.Permission_PERMISSION_UNSPECIFIED)
	assert.False(t, ok)
	_, ok = access.FromProto(tenantv1.Permission(999))
	assert.False(t, ok)

	assert.Equal(t, []tenantv1.Permission{tenantv1.Permission_PERMISSION_STAFF_READ}, access.ListToProto([]access.Permission{access.StaffRead}))
}

func TestAccessCheck(t *testing.T) {
	t.Parallel()

	owner := &access.Access{Owner: true, Active: false}
	member := &access.Access{Active: true, Permissions: []access.Permission{access.CustomerRead}}
	closed := &access.Access{Active: false, Permissions: []access.Permission{access.CustomerRead}}

	tests := []struct {
		name string
		user *access.Access
		rule access.Rule
		want error
	}{
		{name: "owner may do anything, even when inactive", user: owner, rule: access.Requires(access.StaffWrite)},
		{name: "owner-only for owner", user: owner, rule: access.OwnerOnly()},
		{name: "member with permission", user: member, rule: access.Requires(access.CustomerRead)},
		{name: "member without permission", user: member, rule: access.Requires(access.CustomerWrite), want: access.ErrRoleDenied},
		{name: "member on owner-only", user: member, rule: access.OwnerOnly(), want: access.ErrOwnerOnly},
		{name: "any member", user: member, rule: access.Member()},
		{name: "inactive garage blocks members", user: closed, rule: access.Member(), want: access.ErrGarageClosed},
		{name: "inactive allowed explicitly", user: closed, rule: access.Rule{Permission: access.CustomerRead, AllowInactive: true}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.ErrorIs(t, tt.user.Check(tt.rule), tt.want)
		})
	}

	assert.True(t, owner.Has(access.BillingWrite))
	assert.Len(t, owner.Effective(), 18)
	assert.Equal(t, []access.Permission{access.CustomerRead}, member.Effective())
}

func TestContext(t *testing.T) {
	t.Parallel()

	_, ok := access.FromContext(t.Context())
	assert.False(t, ok)

	a := &access.Access{GarageID: "g1"}
	got, ok := access.FromContext(access.WithAccess(t.Context(), a))
	require.True(t, ok)
	assert.Same(t, a, got)
}

func TestCustomerCorrectIncludesRead(t *testing.T) {
	t.Parallel()

	assert.True(t, access.CustomerCorrect.IsValid())
	assert.Equal(t, "correct", access.CustomerCorrect.Action())
	assert.Equal(t, []access.Permission{access.CustomerRead, access.CustomerCorrect},
		access.Normalize([]access.Permission{access.CustomerCorrect}))
	assert.Equal(t, tenantv1.Permission_PERMISSION_CUSTOMER_CORRECT, access.ToProto(access.CustomerCorrect))

	back, ok := access.FromProto(tenantv1.Permission_PERMISSION_CUSTOMER_CORRECT)
	assert.True(t, ok)
	assert.Equal(t, access.CustomerCorrect, back)
}
