// Scenario: E2E-INV-010
// docs: vietgara-docs/docs/05-Testing/E2E_Scenarios/07_Inventory.md
package inventory_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	cataloguev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/catalogue/v1"
	identityv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/identity/v1"
	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

func createInventoryTestEnv(t *testing.T) (context.Context, *fixture.Actor, string, *cataloguev1.Part, []*tenantv1.StaffGroup) {
	t.Helper()

	owner := fixture.NewVerifiedOwnerWithLocale(t, "inv-perms-owner", identityv1.Locale_LOCALE_EN)
	garage, err := owner.API.CreateGarage(t.Context(), &tenantv1.Garage{Name: "Blackbox Inventory Perms"})
	require.NoError(t, err)

	garageID := garage.GetId()

	// Create a part as owner
	part := fixture.NewPart(t, owner.API, garageID, "INV-PERMS-001", "Test Part",
		fixture.NewUnit(t, owner.API, garageID, "Piece", "pcs"), fixture.WithThreshold("10"))

	// Record stock receipt
	_, err = owner.API.CreateStockReceipt(t.Context(), garageID, part.GetId(), "100", "", 50000)
	require.NoError(t, err)

	groups, err := owner.API.ListStaffGroups(t.Context(), garageID)
	require.NoError(t, err)

	return t.Context(), owner, garageID, part, groups
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

		// Technician CAN list the stock (has InventoryRead)
		levels, err := tech.API.ListStockLevels(ctx, garageID)
		require.NoError(t, err)
		require.NotEmpty(t, levels)

		// Technician CAN get a stock level
		got, err := tech.API.GetStockLevel(ctx, garageID, levels[0].GetPartId())
		require.NoError(t, err)
		assert.Equal(t, levels[0].GetPartId(), got.GetPartId())

		// Technician CAN see the part (has CatalogueRead)
		parts, err := tech.API.ListParts(ctx, garageID, nil)
		require.NoError(t, err)
		require.NotEmpty(t, parts)

		// Technician CANNOT create a part (no CatalogueWrite)
		_, err = tech.API.CreatePart(ctx, garageID, &cataloguev1.Part{Sku: "TEST", Name: "Test", BaseUnitId: parts[0].GetBaseUnitId()})
		require.Error(t, err)
		fixture.RequireAPIError(t, err, http.StatusForbidden, "")

		// Technician CANNOT record stock receipt
		_, err = tech.API.CreateStockReceipt(ctx, garageID, levels[0].GetPartId(), "50", "", 0)
		require.Error(t, err)
		fixture.RequireAPIError(t, err, http.StatusForbidden, "")

		// Technician CANNOT delete a part
		_, err = tech.API.DeletePart(ctx, garageID, levels[0].GetPartId(), false)
		require.Error(t, err)
		fixture.RequireAPIError(t, err, http.StatusForbidden, "")
	})
}

func TestInventoryPermissionsReadWrite(t *testing.T) {
	t.Parallel()

	ctx, owner, garageID, part, groups := createInventoryTestEnv(t)

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

		// Inventory Staff can list the stock
		levels, err := staff.API.ListStockLevels(ctx, garageID)
		require.NoError(t, err)
		require.NotEmpty(t, levels)
		assert.Equal(t, "INV-PERMS-001", levels[0].GetSku())

		// Inventory Staff can get a part
		got, err := staff.API.GetPart(ctx, garageID, part.GetId())
		require.NoError(t, err)
		assert.Equal(t, part.GetId(), got.GetId())

		// Inventory Staff can list stock movements
		movements, err := staff.API.ListStockTransactions(ctx, garageID)
		require.NoError(t, err)
		require.NotEmpty(t, movements)

		// Inventory Staff CAN create a part
		newPart, err := staff.API.CreatePart(ctx, garageID, &cataloguev1.Part{Sku: "STAFF-TEST", Name: "Staff Test", BaseUnitId: part.GetBaseUnitId()})
		require.NoError(t, err)
		assert.Equal(t, "STAFF-TEST", newPart.GetSku())

		// Inventory Staff CAN record stock receipt
		_, err = staff.API.CreateStockReceipt(ctx, garageID, newPart.GetId(), "50", "", 10000)
		require.NoError(t, err)

		// Inventory Staff CAN delete a part (it has stock history, so it is deactivated)
		_, err = staff.API.DeletePart(ctx, garageID, newPart.GetId(), false)
		require.NoError(t, err)
	})
}

func TestInventoryPermissionsOwner(t *testing.T) {
	t.Parallel()

	ctx, owner, garageID, part, _ := createInventoryTestEnv(t)

	t.Run("owner full access", func(t *testing.T) {
		t.Parallel()
		// Owner creates another part
		part2 := fixture.NewPart(t, owner.API, garageID, "OWNER-002", "Owner Item",
			&cataloguev1.Unit{Id: part.GetBaseUnitId()})

		// Owner records stock
		_, err := owner.API.CreateStockReceipt(ctx, garageID, part2.GetId(), "200", "", 60000)
		require.NoError(t, err)

		// Owner updates the part
		updated, err := owner.API.UpdatePart(ctx, garageID, part2.GetId(), &cataloguev1.Part{Name: "Updated Owner Item"}, "name")
		require.NoError(t, err)
		assert.Equal(t, "Updated Owner Item", updated.GetName())

		// Owner deletes the part: it has history, so it is deactivated and leaves the stock list
		_, err = owner.API.DeletePart(ctx, garageID, part2.GetId(), false)
		require.NoError(t, err)

		levelsAfter, err := owner.API.ListStockLevels(ctx, garageID)
		require.NoError(t, err)

		found := false

		for _, l := range levelsAfter {
			if l.GetPartId() == part2.GetId() {
				found = true
				break
			}
		}

		assert.False(t, found, "a deactivated part should not appear in the default list")

		// But past transactions are kept
		movements, err := owner.API.ListStockTransactions(ctx, garageID)
		require.NoError(t, err)

		hasReceipt := false

		for _, m := range movements {
			if m.GetItemId() == part2.GetId() {
				hasReceipt = true
				break
			}
		}

		assert.True(t, hasReceipt, "past transactions of a deactivated part are kept")
	})
}
