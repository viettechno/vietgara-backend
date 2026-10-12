// Scenario: E2E-QUO-014
// docs: vietgara-docs/docs/05-Testing/E2E_Scenarios/05_Quote_Pricing.md
package quote_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	commonv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/common/v1"
	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"
	quotev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/quote/v1"
	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

func createQuoteTestEnv(t *testing.T) (context.Context, *fixture.Actor, string, *customerv1.Customer, *customerv1.Vehicle, *quotev1.Quote, []*tenantv1.StaffGroup) {
	t.Helper()

	owner := fixture.NewVerifiedOwner(t, "quo-perms-owner")
	garage, err := owner.API.CreateGarage(t.Context(), &tenantv1.Garage{Name: "Blackbox Quote Perms"})
	require.NoError(t, err)

	garageID := garage.GetId()

	// Create customer and vehicle
	customer, err := owner.API.CreateCustomer(t.Context(), garageID, &customerv1.Customer{FullName: "Khách Quote", PersonalDataConsent: true})
	require.NoError(t, err)
	vehicle, err := owner.API.CreateVehicle(t.Context(), garageID, &customerv1.Vehicle{CustomerId: customer.GetId(), LicensePlate: "51Q-11111"})
	require.NoError(t, err)

	// Create a draft quote as owner
	draft, err := owner.API.CreateQuote(t.Context(), garageID, &quotev1.Quote{
		CustomerId:   customer.GetId(),
		VehicleId:    vehicle.GetId(),
		ApprovalMode: commonv1.QuoteApprovalMode_QUOTE_APPROVAL_MODE_LINK,
		Lines: []*quotev1.QuoteLine{{
			ItemType:    quotev1.QuoteItemType_QUOTE_ITEM_TYPE_LABOR,
			Description: "Labor",
			Quantity:    "1",
			UnitPrice:   500000,
		}},
	})
	require.NoError(t, err)
	assert.Equal(t, quotev1.QuoteStatus_QUOTE_STATUS_DRAFT, draft.GetStatus())

	groups, err := owner.API.ListStaffGroups(t.Context(), garageID)
	require.NoError(t, err)

	return t.Context(), owner, garageID, customer, vehicle, draft, groups
}

func TestQuotePermissionsNoAccess(t *testing.T) {
	t.Parallel()

	ctx, owner, garageID, customer, vehicle, draft, groups := createQuoteTestEnv(t)

	t.Run("no quote permission", func(t *testing.T) {
		t.Parallel()
		noPerm := fixture.NewVerifiedOwner(t, "quo-no-perm")
		cand, err := owner.API.LookupStaffCandidate(ctx, garageID, noPerm.Email)
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
	})
}

func TestQuotePermissionsFullAccess(t *testing.T) {
	t.Parallel()

	ctx, owner, garageID, customer, vehicle, draft, groups := createQuoteTestEnv(t)

	t.Run("full quote access (advisor)", func(t *testing.T) {
		t.Parallel()
		advisor := fixture.NewVerifiedOwner(t, "quo-advisor")
		cand2, err := owner.API.LookupStaffCandidate(ctx, garageID, advisor.Email)
		require.NoError(t, err)

		// Find a group with quote permission (e.g., Service Advisor)
		var advisorGroup *tenantv1.StaffGroup

		for _, g := range groups {
			if g.GetName() == "Service Advisor" {
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
				CustomerId:   customer.GetId(),
				VehicleId:    vehicle.GetId(),
				ApprovalMode: commonv1.QuoteApprovalMode_QUOTE_APPROVAL_MODE_LINK,
				Lines: []*quotev1.QuoteLine{{
					ItemType:    quotev1.QuoteItemType_QUOTE_ITEM_TYPE_LABOR,
					Description: "Advisor Labor",
					Quantity:    "1",
					UnitPrice:   300000,
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
				CustomerId:   customer.GetId(),
				VehicleId:    vehicle.GetId(),
				ApprovalMode: commonv1.QuoteApprovalMode_QUOTE_APPROVAL_MODE_MANUAL,
				Lines: []*quotev1.QuoteLine{{
					ItemType:    quotev1.QuoteItemType_QUOTE_ITEM_TYPE_LABOR,
					Description: "Manual Labor",
					Quantity:    "1",
					UnitPrice:   200000,
				}},
			})
			require.NoError(t, err)

			decided, err := advisor.API.RecordQuoteDecision(ctx, garageID, manualQuote.GetId(), &quotev1.QuoteLineDecisionInput{
				LineId:   manualQuote.GetLines()[0].GetId(),
				Approved: true,
			})
			require.NoError(t, err)
			assert.Equal(t, quotev1.QuoteStatus_QUOTE_STATUS_APPROVED, decided.GetStatus())

			// Advisor can convert quote
			converted, err := advisor.API.ConvertQuote(ctx, garageID, manualQuote.GetId())
			require.NoError(t, err)
			assert.NotEmpty(t, converted.GetRepairOrderId())
		}
	})
}

func TestQuotePermissionsOwner(t *testing.T) {
	t.Parallel()

	ctx, owner, garageID, _, _, draft, _ := createQuoteTestEnv(t)

	t.Run("owner full access", func(t *testing.T) {
		t.Parallel()
		// Owner can delete draft
		err := owner.API.DeleteQuote(ctx, garageID, draft.GetId())
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
	})
}
