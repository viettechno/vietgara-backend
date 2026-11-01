package fixture

import (
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	billingv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/billing/v1"
	commonv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/common/v1"
	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"
	identityv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/identity/v1"
	quotev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/quote/v1"
	repairorderv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/repairorder/v1"
	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"
)

// businessSeq keeps the phones and plates of the garages a run builds apart.
var businessSeq atomic.Int64

// LaborTotal is the total of the quote, order and settlement a Business
// builds, in VND.
const LaborTotal = 500000

// Business is a verified owner with one garage, one customer and one
// vehicle: the data the cross-module scenarios (isolation, read-only,
// races) need a copy of. The helpers walk it along the main flow: quote,
// repair order, close, settlement.
type Business struct {
	Owner      *Actor
	GarageID   string
	CustomerID string
	VehicleID  string
}

// NewBusiness registers an owner and creates the garage, the customer and
// the vehicle.
func NewBusiness(tb testing.TB, label string) *Business {
	tb.Helper()

	ctx := tb.Context()
	// English, so the default group names ("Manager", "Service Advisor") are
	// the ones AddStaff looks for.
	owner := NewVerifiedOwnerWithLocale(tb, label, identityv1.Locale_LOCALE_EN)

	garage, err := owner.API.CreateGarage(ctx, &tenantv1.Garage{Name: "Blackbox " + label})
	if err != nil {
		tb.Fatalf("create garage: %v", err)
	}

	n := businessSeq.Add(1)
	phone := fmt.Sprintf("09%08d", 20000000+n%10000000)
	consent := true

	customer, err := owner.API.CreateCustomer(ctx, garage.GetId(), &customerv1.Customer{
		FullName: "QA Customer " + label, Phone: &phone, PersonalDataConsent: consent,
	})
	if err != nil {
		tb.Fatalf("create customer: %v", err)
	}

	vehicle, err := owner.API.CreateVehicle(ctx, garage.GetId(), &customerv1.Vehicle{
		CustomerId: customer.GetId(), LicensePlate: fmt.Sprintf("51Z-%05d", n%100000),
	})
	if err != nil {
		tb.Fatalf("create vehicle: %v", err)
	}

	return &Business{Owner: owner, GarageID: garage.GetId(), CustomerID: customer.GetId(), VehicleID: vehicle.GetId()}
}

// DraftQuote creates a draft quote with one labor line of LaborTotal.
func (b *Business) DraftQuote(tb testing.TB) *quotev1.Quote {
	tb.Helper()

	quote, err := b.Owner.API.CreateQuote(tb.Context(), b.GarageID, &quotev1.Quote{
		CustomerId: b.CustomerID, VehicleId: b.VehicleID,
		ApprovalMode: commonv1.QuoteApprovalMode_QUOTE_APPROVAL_MODE_MANUAL,
		Lines: []*quotev1.QuoteLine{{
			ItemType: quotev1.QuoteItemType_QUOTE_ITEM_TYPE_LABOR, Description: "Oil change", Quantity: "1", UnitPrice: LaborTotal,
		}},
	})
	if err != nil {
		tb.Fatalf("create quote: %v", err)
	}

	return quote
}

// OpenOrder creates an ad-hoc repair order with one labor line of
// LaborTotal.
func (b *Business) OpenOrder(tb testing.TB) *repairorderv1.RepairOrder {
	tb.Helper()

	return b.OpenOrderFor(tb, b.VehicleID)
}

// OpenOrderFor is OpenOrder for another vehicle of the garage.
func (b *Business) OpenOrderFor(tb testing.TB, vehicleID string) *repairorderv1.RepairOrder {
	tb.Helper()

	order, err := b.Owner.API.CreateRepairOrder(tb.Context(), b.GarageID, &repairorderv1.RepairOrder{
		VehicleId: vehicleID,
		Lines: []*repairorderv1.RepairOrderLine{{
			ItemType: repairorderv1.RepairOrderItemType_REPAIR_ORDER_ITEM_TYPE_LABOR, Description: "Oil change", Quantity: "1", UnitPrice: LaborTotal,
		}},
	})
	if err != nil {
		tb.Fatalf("create repair order: %v", err)
	}

	return order
}

// ClosedOrder creates a repair order, completes its line and closes it.
func (b *Business) ClosedOrder(tb testing.TB) *repairorderv1.RepairOrder {
	tb.Helper()

	ctx := tb.Context()
	order := b.OpenOrder(tb)

	_, err := b.Owner.API.UpdateRepairOrderLine(ctx, b.GarageID, order.GetId(), order.GetLines()[0].GetId(),
		&repairorderv1.RepairOrderLine{Status: repairorderv1.RepairOrderLineStatus_REPAIR_ORDER_LINE_STATUS_COMPLETED})
	if err != nil {
		tb.Fatalf("complete line: %v", err)
	}

	closed, err := b.Owner.API.CloseRepairOrder(ctx, b.GarageID, order.GetId())
	if err != nil {
		tb.Fatalf("close repair order: %v", err)
	}

	return closed
}

// Settlement settles a new closed repair order.
func (b *Business) Settlement(tb testing.TB) *billingv1.Settlement {
	tb.Helper()

	order := b.ClosedOrder(tb)

	settlement, err := b.Owner.API.WithIdempotencyKey(uuid.NewString()).CreateSettlement(tb.Context(), b.GarageID, order.GetId())
	if err != nil {
		tb.Fatalf("create settlement: %v", err)
	}

	return settlement
}

// AddStaff registers a user, invites them to the garage into the default
// group called group ("Manager", "Service Advisor", "Technician", ...) and
// accepts as them.
func (b *Business) AddStaff(tb testing.TB, group string) *Actor {
	tb.Helper()

	ctx := tb.Context()

	groups, err := b.Owner.API.ListStaffGroups(ctx, b.GarageID)
	if err != nil {
		tb.Fatalf("list groups: %v", err)
	}

	groupID := ""

	for _, g := range groups {
		if g.GetName() == group {
			groupID = g.GetId()
		}
	}

	if groupID == "" {
		tb.Fatalf("garage has no group %q", group)
	}

	member := NewVerifiedOwner(tb, "staff")

	invitation, err := b.Owner.API.CreateInvitation(ctx, b.GarageID, member.Session.GetUser().GetId(), []string{groupID})
	if err != nil {
		tb.Fatalf("invite: %v", err)
	}

	if _, err := member.API.AcceptInvitation(ctx, invitation.GetId()); err != nil {
		tb.Fatalf("accept: %v", err)
	}

	return member
}

// SettlementBilledTo settles a new repair order that came from a quote
// billed to the customer's tax profile (empty: a retail receipt): quote,
// send, approve every line, convert, complete, close, settle.
func (b *Business) SettlementBilledTo(tb testing.TB, taxProfileID string) *billingv1.Settlement {
	tb.Helper()

	ctx := tb.Context()
	api := b.Owner.API

	quote, err := api.CreateQuote(ctx, b.GarageID, &quotev1.Quote{
		CustomerId: b.CustomerID, VehicleId: b.VehicleID,
		BillingParty: &commonv1.BillingParty{TaxProfileId: taxProfileID},
		Lines: []*quotev1.QuoteLine{{
			ItemType: quotev1.QuoteItemType_QUOTE_ITEM_TYPE_LABOR, Description: "Oil change", Quantity: "1", UnitPrice: LaborTotal,
		}},
	})
	if err != nil {
		tb.Fatalf("create quote: %v", err)
	}

	if _, err = api.SendQuote(ctx, b.GarageID, quote.GetId()); err != nil {
		tb.Fatalf("send quote: %v", err)
	}

	decisions := make([]*quotev1.QuoteLineDecisionInput, 0, len(quote.GetLines()))
	for _, line := range quote.GetLines() {
		decisions = append(decisions, &quotev1.QuoteLineDecisionInput{LineId: line.GetId(), Approved: true})
	}

	if _, err = api.RecordQuoteDecision(ctx, b.GarageID, quote.GetId(), decisions...); err != nil {
		tb.Fatalf("approve quote: %v", err)
	}

	converted, err := api.ConvertQuote(ctx, b.GarageID, quote.GetId())
	if err != nil {
		tb.Fatalf("convert quote: %v", err)
	}

	order, err := api.GetRepairOrder(ctx, b.GarageID, converted.GetRepairOrderId())
	if err != nil {
		tb.Fatalf("get repair order: %v", err)
	}

	for _, line := range order.GetLines() {
		if _, err = api.UpdateRepairOrderLine(ctx, b.GarageID, order.GetId(), line.GetId(),
			&repairorderv1.RepairOrderLine{Status: repairorderv1.RepairOrderLineStatus_REPAIR_ORDER_LINE_STATUS_COMPLETED}); err != nil {
			tb.Fatalf("complete line: %v", err)
		}
	}

	if _, err = api.CloseRepairOrder(ctx, b.GarageID, order.GetId()); err != nil {
		tb.Fatalf("close repair order: %v", err)
	}

	settlement, err := api.WithIdempotencyKey(uuid.NewString()).CreateSettlement(ctx, b.GarageID, order.GetId())
	if err != nil {
		tb.Fatalf("create settlement: %v", err)
	}

	return settlement
}
