// Package supplier_test covers Suppliers (garage-scoped partners
// pre-5.0) at API level: CRUD, search, per-garage name uniqueness and
// activation. The type filter is gone since the insurer type was
// removed; the sidebar and role screens are web-E2E.
package supplier_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	supplierv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/supplier/v1"
	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

// garageWithOwner returns a fresh owner with one garage.
func garageWithOwner(t *testing.T, label string) (*fixture.Actor, *tenantv1.Garage) {
	t.Helper()

	owner := fixture.NewVerifiedOwner(t, label)

	garage, err := owner.API.CreateGarage(t.Context(), &tenantv1.Garage{Name: "Blackbox " + label})
	require.NoError(t, err)

	return owner, garage
}

// Scenario: E2E-CUS-010 (partial): partners are managed — created,
// searched, edited and deleted at API level; the confirm copy and the
// type filter are web-E2E (the insurer type is gone post-5.0).
func TestManageSuppliers(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage := garageWithOwner(t, "sup-manage")

	created, err := owner.API.CreateSupplier(ctx, garage.GetId(), &supplierv1.Supplier{
		Name: "QA Supplier Dau Nhot Mien Nam",
		Contact: &supplierv1.SupplierContact{
			ContactPerson: new("Mr. Tuan"), Phone: new("0912000003"), Email: new("sales.daunhot@vietgara.test"),
		},
		TaxCode: new("0309876543"),
	})
	require.NoError(t, err)
	assert.Equal(t, "QA Supplier Dau Nhot Mien Nam", created.GetName())
	assert.True(t, created.GetIsActive())

	got, err := owner.API.GetSupplier(ctx, garage.GetId(), created.GetId())
	require.NoError(t, err)
	assert.Equal(t, created.GetId(), got.GetId())

	found, err := owner.API.SearchSuppliers(ctx, garage.GetId(), "Dau Nhot")
	require.NoError(t, err)
	require.NotEmpty(t, found)

	all, err := owner.API.ListSuppliers(ctx, garage.GetId())
	require.NoError(t, err)
	require.NotEmpty(t, all)

	updated, err := owner.API.UpdateSupplier(ctx, garage.GetId(), created.GetId(),
		&supplierv1.Supplier{Contact: &supplierv1.SupplierContact{ContactPerson: new("Ms. Thu")}}, "contact")
	require.NoError(t, err)
	assert.Equal(t, "Ms. Thu", updated.GetContact().GetContactPerson())

	require.NoError(t, owner.API.DeleteSupplier(ctx, garage.GetId(), created.GetId()))

	_, err = owner.API.GetSupplier(ctx, garage.GetId(), created.GetId())
	fixture.RequireAPIError(t, err, http.StatusNotFound, "")
}

// Scenario: E2E-CUS-011 (partial): a supplier name is unique per garage,
// ignoring letter case — asserted at API level.
func TestSupplierNameUniquePerGarage(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwner(t, "sup-unique")

	plans, err := owner.API.ListPlans(ctx)
	require.NoError(t, err)

	var proID string

	for _, p := range plans {
		if p.GetName() == "Pro" {
			proID = p.GetId()
		}
	}

	require.NotEmpty(t, proID)

	_, err = owner.API.ChangeMyPlan(ctx, proID)
	require.NoError(t, err)

	gA, err := owner.API.CreateGarage(ctx, &tenantv1.Garage{Name: "Blackbox Sup A"})
	require.NoError(t, err)

	gB, err := owner.API.CreateGarage(ctx, &tenantv1.Garage{Name: "Blackbox Sup B"})
	require.NoError(t, err)

	_, err = owner.API.CreateSupplier(ctx, gA.GetId(), &supplierv1.Supplier{Name: "QA Supplier Phu Tung Sai Gon"})
	require.NoError(t, err)

	_, err = owner.API.CreateSupplier(ctx, gA.GetId(), &supplierv1.Supplier{Name: "qa supplier phu tung sai gon"})
	fixture.RequireAPIError(t, err, http.StatusConflict, "")

	// Other garage: saved.
	other, err := owner.API.CreateSupplier(ctx, gB.GetId(), &supplierv1.Supplier{Name: "QA Supplier Phu Tung Sai Gon"})
	require.NoError(t, err)

	require.NoError(t, owner.API.DeleteSupplier(ctx, gB.GetId(), other.GetId()))
}

// Scenario: E2E-CUS-010 (partial): deleting a supplier removes it
// from the directory — the API substance; the confirm copy is web-E2E.
// (Reactivation is not reachable through the API: the server's usage
// check always reports a supplier as unused, so delete always
// soft-deletes and Get filters soft-deleted rows.)
func TestDeleteSupplier(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage := garageWithOwner(t, "sup-delete")

	created, err := owner.API.CreateSupplier(ctx, garage.GetId(), &supplierv1.Supplier{Name: "QA Supplier Delete Me"})
	require.NoError(t, err)

	require.NoError(t, owner.API.DeleteSupplier(ctx, garage.GetId(), created.GetId()))

	_, err = owner.API.GetSupplier(ctx, garage.GetId(), created.GetId())
	fixture.RequireAPIError(t, err, http.StatusNotFound, "")

	// A supplier name needs a value.
	_, err = owner.API.CreateSupplier(ctx, garage.GetId(), &supplierv1.Supplier{Name: ""})
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "")
}
