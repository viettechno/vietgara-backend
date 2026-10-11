// Package tenant_test checks garage isolation from the outside: whatever
// RLS and the tenant interceptor do inside, one owner must never read or
// write another owner's garage data through the API.
package tenant_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"
	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

// Scenario: E2E-TEN-022 (partial): invitations, roles and groups are
// later batches.
func TestOwnersCannotReachEachOthersGarages(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	alice := fixture.NewVerifiedOwner(t, "alice")
	bob := fixture.NewVerifiedOwner(t, "bob")

	aliceGarage, err := alice.API.CreateGarage(ctx, &tenantv1.Garage{Name: "Alice Auto"})
	require.NoError(t, err)

	phone := "0912345678"
	aliceCustomer, err := alice.API.CreateCustomer(ctx, aliceGarage.GetId(), &customerv1.Customer{
		FullName: "Nguyễn Văn Khách", Phone: &phone, PersonalDataConsent: true,
	})
	require.NoError(t, err)

	bobGarage, err := bob.API.CreateGarage(ctx, &tenantv1.Garage{Name: "Bob Motors"})
	require.NoError(t, err)

	t.Run("each owner lists only their own garage", func(t *testing.T) {
		for _, tc := range []struct {
			actor  *fixture.Actor
			garage *tenantv1.Garage
		}{{alice, aliceGarage}, {bob, bobGarage}} {
			garages, err := tc.actor.API.ListGarages(ctx)
			require.NoError(t, err)
			require.Len(t, garages, 1)
			assert.Equal(t, tc.garage.GetId(), garages[0].GetId())
		}
	})

	t.Run("the other owner's garage is off limits", func(t *testing.T) {
		_, err := bob.API.GetGarage(ctx, aliceGarage.GetId())
		fixture.RequireAPIError(t, err, http.StatusForbidden, "GARAGE_ACCESS_DENIED")

		_, err = bob.API.ListCustomers(ctx, aliceGarage.GetId())
		fixture.RequireAPIError(t, err, http.StatusForbidden, "GARAGE_ACCESS_DENIED")

		_, err = bob.API.CreateCustomer(ctx, aliceGarage.GetId(), &customerv1.Customer{FullName: "Planted"})
		fixture.RequireAPIError(t, err, http.StatusForbidden, "GARAGE_ACCESS_DENIED")
	})

	t.Run("another garage's record is not found through one's own garage", func(t *testing.T) {
		_, err := bob.API.GetCustomer(ctx, bobGarage.GetId(), aliceCustomer.GetId())
		fixture.RequireAPIError(t, err, http.StatusNotFound, "")

		customers, err := bob.API.ListCustomers(ctx, bobGarage.GetId())
		require.NoError(t, err)
		assert.Empty(t, customers)
	})

	t.Run("the owner still sees their own customer", func(t *testing.T) {
		got, err := alice.API.GetCustomer(ctx, aliceGarage.GetId(), aliceCustomer.GetId())
		require.NoError(t, err)
		assert.Equal(t, "Nguyễn Văn Khách", got.GetFullName())
	})
}
