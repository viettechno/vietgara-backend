// Scenario: E2E-QUO-014
// docs: vietgara-docs/docs/05-Testing/E2E_Scenarios/05_Quote_Pricing.md
package quote_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	quotev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/quote/v1"
	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"
	repairorderv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/repairorder/v1"
	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/client"
	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

func TestQuotePermissions(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwner(t, "quo-perms-owner")
	garage, err := owner.API.CreateGarage(ctx, &tenantv1.Garage{Name: "Blackbox Quote Perms"})
	require.NoError(t, err)

	garageID := garage.GetId()

	// Create customer and vehicle
	customer, err := owner.API.CreateCustomer(ctx, garageID, &customerv1.Customer{FullName: "Khách Quote", PersonalDataConsent: true})
	require.NoError(t, err)
	vehicle, err := owner.API.CreateVehicle(ctx, garageID, &customerv1.Vehicle{CustomerId: customer.GetId(), LicensePlate: "51Q-11111"})
	require.NoError(t, err)

	// Create a draft quote as owner
	draft, err := owner.API.CreateQuote(ctx, garageID, &quotev1.Quote{
		CustomerId: customer.GetId(),
		VehicleId:  vehicle.GetId(),
		ApprovalMode: quotev1.QuoteApprovalMode_QUOTE_APPROVAL_MODE_LINK,
		Lines: []*quotev1.QuoteLine{{
			ItemType: quotev1.QuoteItemType_QUOTE_ITEM_TYPE_LABOR,
			Description: "Labor", Quantity: "1", UnitPrice: "500000",
		}},
	})
	require.NoError(t, err)
	assert.Equal(t, quotev1.QuoteStatus_QUOTE_STATUS_DRAFT, draft.GetStatus())

	// --- Test 1: Staff with NO quote permission cannot access ---
	noPerm := fixture.NewVerifiedOwner(t, "quo-no-perm")
	cand, err := owner.API.LookupStaffCandidate(ctx, garageID, noPerm.Email)
	require.NoError(t, err)

	groups, err := owner.API.ListStaffGroups(ctx, garageID)
	require.NoError(t, err)
	_, err = owner.API.CreateInvitation(ctx, garageID, cand.GetUserId(), []string{groups[0].GetId()})
	require.NoError(t, err)

	noPermInvites, err := noPerm.API.ListMyInvitations(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, noPermInvites)
	_, err = noPerm.API.AcceptInvitation(ctx, noPermInvites[0].GetId())
	require.NoError(t, err)

	// No perm staff tries to list quotes - should fail
	_, err = noPerm.API.ListQuotes(ctx, garageID)
	require.Error(t, err)
	fixture.RequireAPIError(t, err, http.StatusForbidden, "")

	// No perm staff tries to create quote - should fail
	_, err = noPerm.API.CreateQuote(ctx, garageID, &quotev1.Quote{CustomerId: customer.GetId(), VehicleId: vehicle.GetId()})
	require.Error(t, err)
	fixture.RequireAPIError(t, err, http.StatusForbidden, "")

	// No perm staff tries to get quote - should fail
	_, err = noPerm.API.GetQuote(ctx, garageID, draft.GetId())
	require.Error(t, err)
	fixture.RequireAPIError(t, err, http.StatusForbidden, "")

	// --- Test 2: Staff with FULL quote access (Advisor/Manager) can do everything ---
	advisor := fixture.NewVerifiedOwner(t, "quo-advisor")
	cand2, err := owner.API.LookupStaffCandidate(ctx, garageID, advisor.Email)
	require.NoError(t, err)

	// Find a group with quote permission (e.g., Service Advisor)
	var advisorGroup *tenantv1.StaffGroup
	for _, g := range groups {
		if g.GetName() == "Service Advisor" || g.GetRole().GetName() == "Service Advisor" {
			advisorGroup = g
			break
		}
	}
	if advisorGroup != nil {
		_, err = owner.API.CreateInvitation(ctx, garageID, cand2.GetUserId(), []string{advisorGroup.GetId()})
		require.NoError(t, err)

		advisorInvites, err := advisor.API.ListMyInvitations(ctx)
		require.NoError(t, err)
		require.NotEmpty(t, advisorInvites)
		_, err = advisor.API.AcceptInvitation(ctx, advisorInvites[0].GetId())
		require.NoError(t, err)

		// Advisor can list quotes
		advisorQuotes, err := advisor.API.ListQuotes(ctx, garageID)
		require.NoError(t, err)
		require.NotEmpty(t, advisorQuotes)

		// Advisor can get quote
		got, err := advisor.API.GetQuote(ctx, garageID, draft.GetId())
		require.NoError(t, err)
		assert.Equal(t, draft.GetId(), got.GetId())

		// Advisor can create quote
		advisorQuote, err := advisor.API.CreateQuote(ctx, garageID, &quotev1.Quote{
			CustomerId: customer.GetId(),
			VehicleId:  vehicle.GetId(),
			ApprovalMode: quotev1.QuoteApprovalMode_QUOTE_APPROVAL_MODE_LINK,
			Lines: []*quotev1.QuoteLine{{
				ItemType: quotev1.QuoteItemType_QUOTE_ITEM_TYPE_LABOR,
				Description: "Advisor Labor", Quantity: "1", UnitPrice: "300000",
			}},
		})
		require.NoError(t, err)
		assert.Equal(t, quotev1.QuoteStatus_QUOTE_STATUS_DRAFT, advisorQuote.GetStatus())

		// Advisor can send quote
		sent, err := advisor.API.SendQuote(ctx, garageID, advisorQuote.GetId())
		require.NoError(t, err)
		assert.NotEmpty(t, sent.GetPublicUrl())

		// Advisor can record decision (MANUAL mode)
		manualQuote, err := advisor.API.CreateQuote(ctx, garageID, &quotev1.Quote{
			CustomerId: customer.GetId(),
			VehicleId:  vehicle.GetId(),
			ApprovalMode: quotev1.QuoteApprovalMode_QUOTE_APPROVAL_MODE_MANUAL,
			Lines: []*quotev1.QuoteLine{{
				ItemType: quotev1.QuoteItemType_QUOTE_ITEM_TYPE_LABOR,
				Description: "Manual Labor", Quantity: "1", UnitPrice: "200000",
			}},
		})
		require.NoError(t, err)

		decided, err := advisor.API.RecordQuoteDecision(ctx, garageID, manualQuote.GetId(), &quotev1.QuoteLineDecisionInput{
			LineId: manualQuote.GetLines()[0].GetId(), Decision: quotev1.QuoteLineDecision_QUOTE_LINE_DECISION_APPROVED,
		})
		require.NoError(t, err)
		assert.Equal(t, quotev1.QuoteStatus_QUOTE_STATUS_APPROVED, decided.GetStatus())

		// Advisor can convert quote
		converted, err := advisor.API.ConvertQuote(ctx, garageID, manualQuote.GetId())
		require.NoError(t, err)
		assert.NotEmpty(t, converted.GetRepairOrderId())
	}

	// --- Test 3: Owner (full access) can do everything ---
	// Owner can delete draft
	err = owner.API.DeleteQuote(ctx, garageID, draft.GetId())
	require.NoError(t, err)

	// Deleted quote not in list
	listed, err := owner.API.ListQuotes(ctx, garageID)
	require.NoError(t, err)
	found := false
	for _, q := range listed {
		if q.GetId() == draft.GetId() {
			found = true
			break
		}
	}
	assert.False(t, found, "deleted quote should not appear in list")
}