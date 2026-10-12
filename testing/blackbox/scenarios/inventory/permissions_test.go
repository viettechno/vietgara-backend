// Scenario: E2E-INV-010
// docs: vietgara-docs/docs/05-Testing/E2E_Scenarios/07_Inventory.md
package inventory_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	inventoryv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/inventory/v1"
	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

func TestInventoryPermissions(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwner(t, "inv-perms-owner")
	garage, err := owner.API.CreateGarage(ctx, &tenantv1.Garage{Name: "Blackbox Inventory Perms"})
	require.NoError(t, err)

	garageID := garage.GetId()

	// Create an item as owner
	item, err := owner.API.CreateItem(ctx, garageID, &inventoryv1.Item{
		Sku:              "INV-PERMS-001",
		Name:             "Test Part",
		Unit:             "pcs",
		ReorderThreshold: 10,
	})
	require.NoError(t, err)

	// Record stock receipt
	_, err = owner.API.CreateStockReceipt(ctx, garageID, item.GetId(), "100", 50000)
	require.NoError(t, err)

	// --- Test 1: Staff with NO inventory permission cannot access ---
	noPerm := fixture.NewVerifiedOwner(t, "inv-no-perm")
	cand, err := owner.API.LookupStaffCandidate(ctx, garageID, noPerm.Email)
	require.NoError(t, err)

	groups, err := owner.API.ListStaffGroups(ctx, garageID)
	require.NoError(t, err)
	// Use a group without inventory permission (e.g., default Technician or custom no-inventory group)
	_, err = owner.API.CreateInvitation(ctx, garageID, cand.GetUserId(), []string{groups[0].GetId()})
	require.NoError(t, err)

	noPermInvites, err := noPerm.API.ListMyInvitations(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, noPermInvites)
	_, err = noPerm.API.AcceptInvitation(ctx, noPermInvites[0].GetId())
	require.NoError(t, err)

	// No perm staff tries to list items - should fail
	_, err = noPerm.API.ListItems(ctx, garageID)
	require.Error(t, err)
	fixture.RequireAPIError(t, err, http.StatusForbidden, "")

	// No perm staff tries to create item - should fail
	_, err = noPerm.API.CreateItem(ctx, garageID, &inventoryv1.Item{Sku: "TEST", Name: "Test"})
	require.Error(t, err)
	fixture.RequireAPIError(t, err, http.StatusForbidden, "")

	// --- Test 2: Staff with VIEW ONLY (Technician role) can read but not write ---
	tech := fixture.NewVerifiedOwner(t, "inv-tech")
	cand2, err := owner.API.LookupStaffCandidate(ctx, garageID, tech.Email)
	require.NoError(t, err)

	_, err = owner.API.CreateInvitation(ctx, garageID, cand2.GetUserId(), []string{groups[0].GetId()})
	require.NoError(t, err)

	techInvites, err := tech.API.ListMyInvitations(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, techInvites)
	_, err = tech.API.AcceptInvitation(ctx, techInvites[0].GetId())
	require.NoError(t, err)

	// Technician can list items
	items, err := tech.API.ListItems(ctx, garageID)
	require.NoError(t, err)
	require.NotEmpty(t, items)
	assert.Equal(t, "INV-PERMS-001", items[0].GetSku())

	// Technician can get item
	got, err := tech.API.GetItem(ctx, garageID, item.GetId())
	require.NoError(t, err)
	assert.Equal(t, item.GetId(), got.GetId())

	// Technician can list stock movements
	movements, err := tech.API.ListStockTransactions(ctx, garageID)
	require.NoError(t, err)
	require.NotEmpty(t, movements)

	// Technician CANNOT create item
	_, err = tech.API.CreateItem(ctx, garageID, &inventoryv1.Item{Sku: "TECH-TEST", Name: "Tech Test"})
	require.Error(t, err)
	fixture.RequireAPIError(t, err, http.StatusForbidden, "")

	// Technician CANNOT record stock receipt
	_, err = tech.API.CreateStockReceipt(ctx, garageID, item.GetId(), "50", 0)
	require.Error(t, err)
	fixture.RequireAPIError(t, err, http.StatusForbidden, "")

	// Technician CANNOT delete item
	err = tech.API.DeleteItem(ctx, garageID, item.GetId())
	require.Error(t, err)
	fixture.RequireAPIError(t, err, http.StatusForbidden, "")

	// --- Test 3: Owner (full access) can do everything ---
	// Owner creates another item
	item2, err := owner.API.CreateItem(ctx, garageID, &inventoryv1.Item{Sku: "OWNER-002", Name: "Owner Item", Unit: "pcs"})
	require.NoError(t, err)

	// Owner records stock
	_, err = owner.API.CreateStockReceipt(ctx, garageID, item2.GetId(), "200", 60000)
	require.NoError(t, err)

	// Owner updates item
	updated, err := owner.API.UpdateItem(ctx, garageID, item2.GetId(), &inventoryv1.Item{Name: "Updated Owner Item"}, "name")
	require.NoError(t, err)
	assert.Equal(t, "Updated Owner Item", updated.GetName())

	// Owner deletes item
	err = owner.API.DeleteItem(ctx, garageID, item2.GetId())
	require.NoError(t, err)

	// Deleted item is not in list
	itemsAfter, err := owner.API.ListItems(ctx, garageID)
	require.NoError(t, err)
	found := false
	for _, i := range itemsAfter {
		if i.GetId() == item2.GetId() {
			found = true
			break
		}
	}
	assert.False(t, found, "deleted item should not appear in list")

	// But past transactions are kept
	movements, err = owner.API.ListStockTransactions(ctx, garageID)
	require.NoError(t, err)
	hasReceipt := false
	for _, m := range movements {
		if m.GetItemId() == item2.GetId() {
			hasReceipt = true
			break
		}
	}
	assert.True(t, hasReceipt, "past transactions of deleted item are kept")
}
