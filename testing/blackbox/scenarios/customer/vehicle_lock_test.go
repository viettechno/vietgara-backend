// Vehicle identity lock (backend #43 behavior): while a live document
// refers to a vehicle, its identity fields cannot be edited directly
// (412 VEHICLE_FIELD_LOCKED); a correction with a reason, or an owner
// change with a reason, is the way. Non-identity edits stay allowed.
package customer_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"
	quotev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/quote/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

// Scenario: E2E-CUS-005 (partial): a vehicle on a live quote
// locks its identity — the API substance of backend #43.
func TestVehicleIdentityLockAndCorrection(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage := garageWithOwner(t, "cus-lock")

	phoneA := "0911000111"
	phoneB := "0911000112"

	customerA, err := owner.API.CreateCustomer(ctx, garage.GetId(),
		&customerv1.Customer{FullName: "QA Customer Lock A", Phone: &phoneA})
	require.NoError(t, err)

	customerB, err := owner.API.CreateCustomer(ctx, garage.GetId(),
		&customerv1.Customer{FullName: "QA Customer Lock B", Phone: &phoneB})
	require.NoError(t, err)

	vehicle, err := owner.API.CreateVehicle(ctx, garage.GetId(), &customerv1.Vehicle{
		CustomerId: customerA.GetId(), LicensePlate: "51L-12345", Make: new("Toyota"),
	})
	require.NoError(t, err)

	// A draft quote makes the vehicle live.
	quote, err := owner.API.CreateQuote(ctx, garage.GetId(), &quotev1.Quote{
		CustomerId: customerA.GetId(), VehicleId: vehicle.GetId(),
		Lines: []*quotev1.QuoteLine{{
			ItemType: quotev1.QuoteItemType_QUOTE_ITEM_TYPE_LABOR, Description: "Service", Quantity: "1", UnitPrice: 100000,
		}},
	})
	require.NoError(t, err)
	assert.NotEmpty(t, quote.GetId())

	locked, err := owner.API.GetVehicle(ctx, garage.GetId(), vehicle.GetId())
	require.NoError(t, err)
	assert.False(t, locked.GetEditable())
	assert.NotEmpty(t, locked.GetLockedFields())

	// Direct identity edit is refused.
	_, err = owner.API.UpdateVehicle(ctx, garage.GetId(), vehicle.GetId(),
		&customerv1.Vehicle{LicensePlate: "51L-54321"}, "license_plate")
	fixture.RequireAPIError(t, err, http.StatusPreconditionFailed, "VEHICLE_FIELD_LOCKED")

	// A correction with a reason works.
	corrected, err := owner.API.CorrectVehicle(ctx, garage.GetId(), vehicle.GetId(),
		customerv1.VehicleIdentityField_VEHICLE_IDENTITY_FIELD_LICENSE_PLATE, "51L-54321", "Plate typo at registration")
	require.NoError(t, err)
	assert.Equal(t, "51L-54321", corrected.GetLicensePlate())

	// A correction without a reason is refused.
	_, err = owner.API.CorrectVehicle(ctx, garage.GetId(), vehicle.GetId(),
		customerv1.VehicleIdentityField_VEHICLE_IDENTITY_FIELD_LICENSE_PLATE, "51L-99999", "")
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "")

	// An owner change with a reason works.
	moved, err := owner.API.ChangeVehicleOwner(ctx, garage.GetId(), vehicle.GetId(), customerB.GetId(), "Sold to B")
	require.NoError(t, err)
	assert.Equal(t, customerB.GetId(), moved.GetCustomerId())

	// Moving to the same owner is refused.
	_, err = owner.API.ChangeVehicleOwner(ctx, garage.GetId(), vehicle.GetId(), customerB.GetId(), "Sold to B")
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "")
}
