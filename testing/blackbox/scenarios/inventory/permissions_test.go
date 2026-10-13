// Scenario: E2E-INV-010
// docs: vietgara-docs/docs/05-Testing/E2E_Scenarios/07_Inventory.md
package inventory_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	identityv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/identity/v1"
	inventoryv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/inventory/v1"
	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

func createInventoryTestEnv(t *testing.T) (context.Context, *fixture.Actor, string, *inventoryv1.Item, []*tenantv1.StaffGroup) {
	t.Helper()

	owner := fixture.NewVerifiedOwnerWithLocale(t, "inv-perms-owner", identityv1.Locale_LOCALE_EN)
	garage, err := owner.API.CreateGarage(t.Context(), &tenantv1.Garage{Name: "Blackbox Inventory Perms"})
	require.NoError(t, err)

	garageID := garage.GetId()

	// Create an item as owner
	item, err := owner.API.CreateItem(t.Context(), garageID, &inventoryv1.Item{
		Sku:              "INV-PERMS-001",
		Name:             "Test Part",
		Unit:             "pcs",
		ReorderThreshold: 10,
	})
	require.NoError(t, err)

	// Record stock receipt
	_, err = owner.API.CreateStockReceipt(t.Context(), garageID, item.GetId(), "100", 50000)
	require.NoError(t, err)

	groups, err := owner.API.ListStaffGroups(t.Context(), garageID)
	require.NoError(t, err)

	return t.Context(), owner, garageID, item, groups
}

func TestInventoryPermissionsNoAccess(t *testing.T) {
	t.Parallel()

	ctx, owner, garageID, _, groups := createInventoryTestEnv(t)

	t.Run("technician has read-only inventory access", func(t *testing.T) {
		t.Parallel()
		tech := fixture.NewVerifiedOwner(t, "inv-tech-read")
		cand, err := owner.API.LookupStaffCandidate(ctx, garageID, tech.Email)
		require.NoError(t, err)

		// Find Technician group (has InventoryRead, no InventoryWrite)
		var techGroup *tenantv1.StaffGroup

		for _, g := range groups {
			if g.GetName() == "Technician" {
				techGroup = g
				break
			}
		}

		require.NotNil(t, techGroup, "Technician group should exist")

		_, err = owner.API.CreateInvitation(ctx, garageID, cand.GetUserId(), []string{techGroup.GetId()})
		require.NoError(t, err)

		techInvites, err := tech.API.ListMyInvitations(ctx)
		require.NoError(t, err)
		require.NotEmpty(t, techInvites)
		_, err = tech.API.AcceptInvitation(ctx, techInvites[0].GetId())
		require.NoError(t, err)

		// Technician CAN list items (has InventoryRead)
		items, err := tech.API.ListItems(ctx, garageID)
		require.NoError(t, err)
		require.NotEmpty(t, items)

		// Technician CAN get item
		got, err := tech.API.GetItem(ctx, garageID, items[0].GetId())
		require.NoError(t, err)
		assert.Equal(t, items[0].GetId(), got.GetId())

		// Technician CANNOT create item (no InventoryWrite)
		_, err = tech.API.CreateItem(ctx, garageID, &inventoryv1.Item{Sku: "TEST", Name: "Test"})
		require.Error(t, err)
		fixture.RequireAPIError(t, err, http.StatusForbidden, "")

		// Technician CANNOT record stock receipt
		_, err = tech.API.CreateStockReceipt(ctx, garageID, items[0].GetId(), "50", 0)
		require.Error(t, err)
		fixture.RequireAPIError(t, err, http.StatusForbidden, "")

		// Technician CANNOT delete item
		err = tech.API.DeleteItem(ctx, garageID, items[0].GetId())
		require.Error(t, err)
		fixture.RequireAPIError(t, err, http.StatusForbidden, "")
	})
}

func TestInventoryPermissionsReadWrite(t *testing.T) {
	t.Parallel()

	ctx, owner, garageID, item, groups := createInventoryTestEnv(t)

	t.Run("inventory staff has read-write access", func(t *testing.T) {
		t.Parallel()
		staff := fixture.NewVerifiedOwner(t, "inv-staff-rw")
		cand2, err := owner.API.LookupStaffCandidate(ctx, garageID, staff.Email)
		require.NoError(t, err)

		// Find Inventory Staff group (has InventoryRead + InventoryWrite)
		var staffGroup *tenantv1.StaffGroup

		for _, g := range groups {
			if g.GetName() == "Inventory Staff" {
				staffGroup = g
				break
			}
		}

		require.NotNil(t, staffGroup, "Inventory Staff group should exist")

		_, err = owner.API.CreateInvitation(ctx, garageID, cand2.GetUserId(), []string{staffGroup.GetId()})
		require.NoError(t, err)

		staffInvites, err := staff.API.ListMyInvitations(ctx)
		require.NoError(t, err)
		require.NotEmpty(t, staffInvites)
		_, err = staff.API.AcceptInvitation(ctx, staffInvites[0].GetId())
		require.NoError(t, err)

		// Inventory Staff can list items
		items, err := staff.API.ListItems(ctx, garageID)
		require.NoError(t, err)
		require.NotEmpty(t, items)
		assert.Equal(t, "INV-PERMS-001", items[0].GetSku())

		// Inventory Staff can get item
		got, err := staff.API.GetItem(ctx, garageID, item.GetId())
		require.NoError(t, err)
		assert.Equal(t, item.GetId(), got.GetId())

		// Inventory Staff can list stock movements
		movements, err := staff.API.ListStockTransactions(ctx, garageID)
		require.NoError(t, err)
		require.NotEmpty(t, movements)

		// Inventory Staff CAN create item
		newItem, err := staff.API.CreateItem(ctx, garageID, &inventoryv1.Item{Sku: "STAFF-TEST", Name: "Staff Test", Unit: "pcs"})
		require.NoError(t, err)
		assert.Equal(t, "STAFF-TEST", newItem.GetSku())

		// Inventory Staff CAN record stock receipt
		_, err = staff.API.CreateStockReceipt(ctx, garageID, newItem.GetId(), "50", 10000)
		require.NoError(t, err)

		// Inventory Staff CAN delete item
		err = staff.API.DeleteItem(ctx, garageID, newItem.GetId())
		require.NoError(t, err)
	})
}

func TestInventoryPermissionsOwner(t *testing.T) {
	t.Parallel()

	ctx, owner, garageID, _, _ := createInventoryTestEnv(t)

	t.Run("owner full access", func(t *testing.T) {
		t.Parallel()
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
		movements, err := owner.API.ListStockTransactions(ctx, garageID)
		require.NoError(t, err)

		hasReceipt := false

		for _, m := range movements {
			if m.GetItemId() == item2.GetId() {
				hasReceipt = true
				break
			}
		}

		assert.True(t, hasReceipt, "past transactions of deleted item are kept")
	})
}
