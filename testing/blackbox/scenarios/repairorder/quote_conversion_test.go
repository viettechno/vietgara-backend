// Scenario: E2E-REP-003
// docs: vietgara-docs/docs/05-Testing/E2E_Scenarios/06_Repair_Order.md
package repairorder_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	commonv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/common/v1"
	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"
	quotev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/quote/v1"
	repairorderv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/repairorder/v1"
	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

func TestRepairOrderFromApprovedQuote(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwner(t, "rep-quote-convert")
	garage, err := owner.API.CreateGarage(ctx, &tenantv1.Garage{Name: "Blackbox RO from Quote"})
	require.NoError(t, err)

	garageID := garage.GetId()

	// Create customer and vehicle
	customer, err := owner.API.CreateCustomer(ctx, garageID, &customerv1.Customer{FullName: "Khách RO Quote", PersonalDataConsent: true})
	require.NoError(t, err)
	vehicle, err := owner.API.CreateVehicle(ctx, garageID, &customerv1.Vehicle{CustomerId: customer.GetId(), LicensePlate: "51R-11111"})
	require.NoError(t, err)

	// Create and approve a quote via API (LINK mode, then approve via public link)
	quote, err := owner.API.CreateQuote(ctx, garageID, &quotev1.Quote{
		CustomerId:   customer.GetId(),
		VehicleId:    vehicle.GetId(),
		ApprovalMode: commonv1.QuoteApprovalMode_QUOTE_APPROVAL_MODE_LINK,
		Lines: []*quotev1.QuoteLine{{
			ItemType:    quotev1.QuoteItemType_QUOTE_ITEM_TYPE_LABOR,
			Description: "Oil change service",
			Quantity:    "1",
			UnitPrice:   250000,
		}, {
			ItemType:    quotev1.QuoteItemType_QUOTE_ITEM_TYPE_PART,
			Description: "Engine oil 5W-30",
			Quantity:    "4",
			UnitPrice:   180000,
		}},
	})
	require.NoError(t, err)
	assert.Equal(t, quotev1.QuoteStatus_QUOTE_STATUS_DRAFT, quote.GetStatus())

	// Send for approval (LINK mode)
	sendResult, err := owner.API.SendQuote(ctx, garageID, quote.GetId())
	require.NoError(t, err)
	assert.NotEmpty(t, sendResult.GetPublicUrl())
	// The quote status is now SENT
	updatedQuote, err := owner.API.GetQuote(ctx, garageID, quote.GetId())
	require.NoError(t, err)
	assert.Equal(t, quotev1.QuoteStatus_QUOTE_STATUS_SENT, updatedQuote.GetStatus())

	// Approve from public link (all lines approved)
	publicQuote, err := owner.API.GetPublicQuote(ctx, sendResult.GetPublicUrl())
	require.NoError(t, err)

	decided, err := owner.API.DecidePublicQuote(ctx, sendResult.GetPublicUrl(),
		&quotev1.QuoteLineDecisionInput{LineId: publicQuote.GetLines()[0].GetId(), Approved: true},
		&quotev1.QuoteLineDecisionInput{LineId: publicQuote.GetLines()[1].GetId(), Approved: true},
	)
	require.NoError(t, err)
	assert.Equal(t, quotev1.QuoteStatus_QUOTE_STATUS_APPROVED, decided.GetStatus())

	// Convert to repair order
	converted, err := owner.API.ConvertQuote(ctx, garageID, quote.GetId())
	require.NoError(t, err)
	assert.NotEmpty(t, converted.GetRepairOrderId())

	roID := converted.GetRepairOrderId()

	// Verify repair order inherits quote data
	ro, err := owner.API.GetRepairOrder(ctx, garageID, roID)
	require.NoError(t, err)

	// Customer and vehicle match
	assert.Equal(t, customer.GetFullName(), ro.GetCustomerName())
	assert.Equal(t, vehicle.GetId(), ro.GetVehicleId())
	assert.Equal(t, vehicle.GetLicensePlate(), ro.GetVehicleLicensePlate())

	// Lines match only approved lines (both approved in this case)
	require.Len(t, ro.GetLines(), 2)
	lineDesc := map[string]bool{}
	for _, l := range ro.GetLines() {
		lineDesc[l.GetDescription()] = true
	}
	assert.True(t, lineDesc["Oil change service"])
	assert.True(t, lineDesc["Engine oil 5W-30"])
	assert.False(t, lineDesc["Unapproved line"])

	// All lines start at Not started with no technician assigned
	for _, l := range ro.GetLines() {
		assert.Equal(t, repairorderv1.RepairOrderLineStatus_REPAIR_ORDER_LINE_STATUS_NOT_STARTED, l.GetStatus())
		assert.Empty(t, l.GetAssignedTechnicianIds())
	}

	// Status is Open (To Do equivalent)
	assert.Equal(t, repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_OPEN, ro.GetStatus())

	// Total matches approved lines only (both approved = full quote total)
	assert.EqualValues(t, 970000, ro.GetTotalAmount())

	// Converting same quote again is refused
	_, err = owner.API.ConvertQuote(ctx, garageID, quote.GetId())
	require.Error(t, err)

	// Vehicle's repair history now lists both the quote (Converted) and the repair order
	history, err := owner.API.GetVehicleHistory(ctx, garageID, vehicle.GetId())
	require.NoError(t, err)
	require.Len(t, history, 2)
	hasConvertedQuote := false
	hasRepairOrder := false
	for _, h := range history {
		if h.GetType() == customerv1.VehicleHistoryEntryType_VEHICLE_HISTORY_ENTRY_TYPE_QUOTE && h.GetStatus() == "Converted" {
			hasConvertedQuote = true
		}
		if h.GetType() == customerv1.VehicleHistoryEntryType_VEHICLE_HISTORY_ENTRY_TYPE_REPAIR_ORDER {
			hasRepairOrder = true
		}
	}
	assert.True(t, hasConvertedQuote)
	assert.True(t, hasRepairOrder)
}

// Scenario: E2E-REP-003 partial: mixed approval converts only approved lines
func TestRepairOrderFromMixedApproval(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwner(t, "rep-mixed-convert")
	garage, err := owner.API.CreateGarage(ctx, &tenantv1.Garage{Name: "Blackbox Mixed Convert"})
	require.NoError(t, err)

	garageID := garage.GetId()

	customer, err := owner.API.CreateCustomer(ctx, garageID, &customerv1.Customer{FullName: "Khách Mixed", PersonalDataConsent: true})
	require.NoError(t, err)
	vehicle, err := owner.API.CreateVehicle(ctx, garageID, &customerv1.Vehicle{CustomerId: customer.GetId(), LicensePlate: "51R-22222"})
	require.NoError(t, err)

	// Create quote with 2 lines
	quote, err := owner.API.CreateQuote(ctx, garageID, &quotev1.Quote{
		CustomerId:   customer.GetId(),
		VehicleId:    vehicle.GetId(),
		ApprovalMode: commonv1.QuoteApprovalMode_QUOTE_APPROVAL_MODE_LINK,
		Lines: []*quotev1.QuoteLine{{
			ItemType:    quotev1.QuoteItemType_QUOTE_ITEM_TYPE_LABOR,
			Description: "Approve Me",
			Quantity:    "1",
			UnitPrice:   100000,
		}, {
			ItemType:    quotev1.QuoteItemType_QUOTE_ITEM_TYPE_PART,
			Description: "Reject Me",
			Quantity:    "1",
			UnitPrice:   200000,
		}},
	})
	require.NoError(t, err)

	// Send for approval
	sendResult, err := owner.API.SendQuote(ctx, garageID, quote.GetId())
	require.NoError(t, err)

	// Approve line 1, reject line 2
	publicQuote, err := owner.API.GetPublicQuote(ctx, sendResult.GetPublicUrl())
	require.NoError(t, err)

	decided, err := owner.API.DecidePublicQuote(ctx, sendResult.GetPublicUrl(),
		&quotev1.QuoteLineDecisionInput{LineId: publicQuote.GetLines()[0].GetId(), Approved: true},
		&quotev1.QuoteLineDecisionInput{LineId: publicQuote.GetLines()[1].GetId(), Approved: false},
	)
	require.NoError(t, err)
	assert.Equal(t, quotev1.QuoteStatus_QUOTE_STATUS_APPROVED, decided.GetStatus())

	// Convert
	converted, err := owner.API.ConvertQuote(ctx, garageID, quote.GetId())
	require.NoError(t, err)

	roID := converted.GetRepairOrderId()
	ro, err := owner.API.GetRepairOrder(ctx, garageID, roID)
	require.NoError(t, err)

	// Repair order has ONLY the approved line
	require.Len(t, ro.GetLines(), 1)
	assert.Equal(t, "Approve Me", ro.GetLines()[0].GetDescription())
	assert.EqualValues(t, 100000, ro.GetTotalAmount())
	assert.Equal(t, repairorderv1.RepairOrderStatus_REPAIR_ORDER_STATUS_OPEN, ro.GetStatus())
}
