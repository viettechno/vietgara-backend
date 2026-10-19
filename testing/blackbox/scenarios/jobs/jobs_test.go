// Package jobs_test covers Release 7.3 at API level: the job catalogue, jobs
// on quotes with their hidden consumables, and the repair order changes the
// reservation needs (editing lines, cancelling, waiting on parts). The pickers,
// the lines editor and the board are web-E2E.
package jobs_test

import (
	"fmt"
	"net/http"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	cataloguev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/catalogue/v1"
	commonv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/common/v1"
	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"
	identityv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/identity/v1"
	inventoryv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/inventory/v1"
	quotev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/quote/v1"
	repairorderv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/repairorder/v1"
	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/client"
	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

var seq atomic.Int64

// garageKit is a fresh owner with a garage, a customer and a vehicle.
type garageKit struct {
	owner    *fixture.Actor
	garageID string
	customer *customerv1.Customer
	vehicle  *customerv1.Vehicle
}

func setup(t *testing.T, label string) *garageKit {
	t.Helper()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwnerWithLocale(t, label, identityv1.Locale_LOCALE_EN)

	garage, err := owner.API.CreateGarage(ctx, &tenantv1.Garage{Name: "Blackbox " + label})
	require.NoError(t, err)

	suffix := fmt.Sprintf("%05d", seq.Add(1)%100000)
	phone := "09140" + suffix

	customer, err := owner.API.CreateCustomer(ctx, garage.GetId(), &customerv1.Customer{
		FullName: "QA Customer " + label, Phone: &phone, PersonalDataConsent: true,
	})
	require.NoError(t, err)

	vehicle, err := owner.API.CreateVehicle(ctx, garage.GetId(), &customerv1.Vehicle{
		CustomerId: customer.GetId(), LicensePlate: "51J-" + suffix,
	})
	require.NoError(t, err)

	return &garageKit{owner: owner, garageID: garage.GetId(), customer: customer, vehicle: vehicle}
}

// catalogue is what the scenarios sell: engine oil by the litre or the 5 L
// jug, and an oil filter by the piece.
type catalogue struct {
	litre, jug, piece *cataloguev1.Unit
	oil, filter       *cataloguev1.Part
}

func (k *garageKit) catalogue(t *testing.T) *catalogue {
	t.Helper()

	api, garageID := k.owner.API, k.garageID
	c := &catalogue{
		litre: fixture.NewUnit(t, api, garageID, "Litre", "L"),
		jug:   fixture.NewUnit(t, api, garageID, "Jug", "jug"),
		piece: fixture.NewUnit(t, api, garageID, "Piece", "pc"),
	}
	c.oil = fixture.NewPart(t, api, garageID, sku("OIL"), "Engine oil 5W-30", c.litre, fixture.WithPrice(90_000))
	c.filter = fixture.NewPart(t, api, garageID, sku("FLT"), "Oil filter", c.piece, fixture.WithPrice(150_000))

	oil, err := api.SetPartUnits(t.Context(), garageID, c.oil.GetId(), []*cataloguev1.PackagingUnit{{UnitId: c.jug.GetId(), Factor: "5"}})
	require.NoError(t, err)

	c.oil = oil

	return c
}

func sku(prefix string) string { return fmt.Sprintf("%s-%05d", prefix, seq.Add(1)%100000) }

func suggest(part *cataloguev1.Part, quantity string, options ...func(*cataloguev1.JobPart)) *cataloguev1.JobPart {
	p := &cataloguev1.JobPart{PartId: part.GetId(), Quantity: quantity}
	for _, option := range options {
		option(p)
	}

	return p
}

func inUnit(unit *cataloguev1.Unit) func(*cataloguev1.JobPart) {
	return func(p *cataloguev1.JobPart) { p.UnitId = new(unit.GetId()) }
}

func hidden(p *cataloguev1.JobPart) { p.NonBillable = true }

func newJob(t *testing.T, k *garageKit, name string, laborPrice int64, parts ...*cataloguev1.JobPart) *cataloguev1.Job {
	t.Helper()

	job, err := k.owner.API.CreateJob(t.Context(), k.garageID, &cataloguev1.Job{
		Name: name, Category: "Maintenance", LaborPrice: laborPrice, DurationMinutes: 45, Parts: parts,
	})
	require.NoError(t, err)

	return job
}

// eventually polls check until it holds; the events that keep the stock issue
// and the order's status in line are handled a moment after the request.
func eventually(t *testing.T, what string, check func() (bool, string)) {
	t.Helper()

	deadline := time.Now().Add(60 * time.Second)

	for {
		ok, got := check()
		if ok {
			return
		}

		if time.Now().After(deadline) {
			t.Fatalf("%s: %s", what, got)
		}

		time.Sleep(500 * time.Millisecond)
	}
}

func waitForReserved(t *testing.T, api *client.Client, garageID, partID, want string) {
	t.Helper()

	eventually(t, "reserved quantity of "+partID, func() (bool, string) {
		level, err := api.GetStockLevel(t.Context(), garageID, partID)
		require.NoError(t, err)

		return level.GetQuantityReserved() == want, fmt.Sprintf("reserved is %q, want %q", level.GetQuantityReserved(), want)
	})
}

func waitForStockIssue(t *testing.T, api *client.Client, garageID, orderID string, ok func(*inventoryv1.StockIssue) bool) *inventoryv1.StockIssue {
	t.Helper()

	var issue *inventoryv1.StockIssue

	eventually(t, "stock issue of "+orderID, func() (bool, string) {
		got, err := api.GetRepairOrderStockIssue(t.Context(), garageID, orderID)
		if err != nil {
			if apiErr := client.AsAPIError(err); apiErr != nil && apiErr.Status == http.StatusNotFound {
				return false, "no stock issue yet"
			}

			require.NoError(t, err)
		}

		issue = got

		return ok(got), fmt.Sprintf("status %v short %v", got.GetStatus(), got.GetIsShort())
	})

	return issue
}

func anyIssue(*inventoryv1.StockIssue) bool { return true }

func waitForStatus(t *testing.T, api *client.Client, garageID, orderID string, want repairorderv1.RepairOrderStatus) *repairorderv1.RepairOrder {
	t.Helper()

	var order *repairorderv1.RepairOrder

	eventually(t, "status of "+orderID, func() (bool, string) {
		got, err := api.GetRepairOrder(t.Context(), garageID, orderID)
		require.NoError(t, err)

		order = got

		return got.GetStatus() == want, fmt.Sprintf("status is %v, want %v", got.GetStatus(), want)
	})

	return order
}

// staffIn invites a new user into the garage with the default group name and
// returns them signed in.
func staffIn(t *testing.T, k *garageKit, label, group string) *fixture.Actor {
	t.Helper()

	ctx := t.Context()
	member := fixture.NewVerifiedOwner(t, label)

	candidate, err := k.owner.API.LookupStaffCandidate(ctx, k.garageID, member.Email)
	require.NoError(t, err)

	groups, err := k.owner.API.ListStaffGroups(ctx, k.garageID)
	require.NoError(t, err)

	var groupID string

	for _, g := range groups {
		if g.GetName() == group {
			groupID = g.GetId()
		}
	}

	require.NotEmpty(t, groupID, "default group %q", group)

	_, err = k.owner.API.CreateInvitation(ctx, k.garageID, candidate.GetUserId(), []string{groupID})
	require.NoError(t, err)

	invitations, err := member.API.ListMyInvitations(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, invitations)

	_, err = member.API.AcceptInvitation(ctx, invitations[0].GetId())
	require.NoError(t, err)

	return member
}

// Scenario: E2E-CAT-010, Release 7.3, the job catalogue (FR-CAT-05, FR-CAT-08): a job
// with suggested parts is described by the catalogue (SKU, name, unit and
// current price); the name is unique without regard to case; every invalid
// field is refused; update changes only what the mask names, and sending parts
// replaces the list; the list filters and pages.
func TestJobsCreateAndValidate(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	k := setup(t, "job-create")
	api, garageID := k.owner.API, k.garageID
	c := k.catalogue(t)

	job := newJob(t, k, "Oil change", 200_000, suggest(c.oil, "4"), suggest(c.filter, "1", hidden))
	assert.Equal(t, "Maintenance", job.GetCategory())
	assert.EqualValues(t, 45, job.GetDurationMinutes())
	assert.True(t, job.GetIsActive())
	require.Len(t, job.GetParts(), 2)

	oil, filter := job.GetParts()[0], job.GetParts()[1]
	assert.Equal(t, c.oil.GetSku(), oil.GetPartSku())
	assert.Equal(t, "Engine oil 5W-30", oil.GetPartName())
	assert.Equal(t, "L", oil.GetUnitAbbreviation())
	assert.EqualValues(t, 90_000, oil.GetUnitPrice())
	assert.True(t, oil.GetPartIsActive())
	assert.True(t, filter.GetNonBillable())
	assert.False(t, oil.GetNonBillable())

	t.Run("a part suggested in a packaging unit is priced per unit", func(t *testing.T) {
		updated, err := api.UpdateJob(ctx, garageID, job.GetId(), &cataloguev1.Job{
			Parts: []*cataloguev1.JobPart{suggest(c.oil, "1", inUnit(c.jug))},
		}, "parts")
		require.NoError(t, err)
		require.Len(t, updated.GetParts(), 1, "sending parts replaces the list")
		assert.Equal(t, "jug", updated.GetParts()[0].GetUnitAbbreviation())
		assert.EqualValues(t, 450_000, updated.GetParts()[0].GetUnitPrice())
		assert.Equal(t, "Oil change", updated.GetName(), "the other fields are untouched")
	})

	t.Run("a partial update keeps the parts; no parts clears them", func(t *testing.T) {
		renamed, err := api.UpdateJob(ctx, garageID, job.GetId(), &cataloguev1.Job{Name: "Full oil change", LaborPrice: 250_000}, "name", "labor_price")
		require.NoError(t, err)
		assert.Equal(t, "Full oil change", renamed.GetName())
		assert.EqualValues(t, 250_000, renamed.GetLaborPrice())
		assert.Len(t, renamed.GetParts(), 1)

		cleared, err := api.UpdateJob(ctx, garageID, job.GetId(), &cataloguev1.Job{}, "parts")
		require.NoError(t, err)
		assert.Empty(t, cleared.GetParts())

		got, err := api.GetJob(ctx, garageID, job.GetId())
		require.NoError(t, err)
		assert.Empty(t, got.GetParts())
	})

	t.Run("an unknown mask path is refused", func(t *testing.T) {
		_, err := api.UpdateJob(ctx, garageID, job.GetId(), &cataloguev1.Job{Name: "x"}, "id")
		fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "")
	})

	t.Run("the name is unique, in any case", func(t *testing.T) {
		_, err := api.CreateJob(ctx, garageID, &cataloguev1.Job{Name: "FULL OIL CHANGE"})
		fixture.RequireAPIError(t, err, http.StatusConflict, "")
	})

	t.Run("invalid jobs are refused", func(t *testing.T) {
		inactive := fixture.NewPart(t, api, garageID, sku("OLD"), "Retired part", c.piece)
		_, err := api.UpdatePart(ctx, garageID, inactive.GetId(), &cataloguev1.Part{IsActive: false}, "is_active")
		require.NoError(t, err)

		many := make([]*cataloguev1.JobPart, 0, 31)
		for range 31 {
			many = append(many, suggest(c.oil, "1"))
		}

		tests := []struct {
			name   string
			job    *cataloguev1.Job
			status int
		}{
			{"no name", &cataloguev1.Job{Name: "  "}, http.StatusUnprocessableEntity},
			{"negative labor price", &cataloguev1.Job{Name: "Neg price", LaborPrice: -1}, http.StatusUnprocessableEntity},
			{"negative duration", &cataloguev1.Job{Name: "Neg time", DurationMinutes: -5}, http.StatusUnprocessableEntity},
			{"the same part twice", &cataloguev1.Job{Name: "Twice", Parts: []*cataloguev1.JobPart{suggest(c.oil, "1"), suggest(c.oil, "2")}}, http.StatusUnprocessableEntity},
			{"an unknown part", &cataloguev1.Job{Name: "Unknown", Parts: []*cataloguev1.JobPart{{PartId: uuid.NewString(), Quantity: "1"}}}, http.StatusNotFound},
			{"an inactive part", &cataloguev1.Job{Name: "Inactive", Parts: []*cataloguev1.JobPart{suggest(inactive, "1")}}, http.StatusConflict},
			{"a unit the part does not have", &cataloguev1.Job{Name: "Bad unit", Parts: []*cataloguev1.JobPart{suggest(c.filter, "1", inUnit(c.jug))}}, http.StatusUnprocessableEntity},
			{"a zero quantity", &cataloguev1.Job{Name: "Zero", Parts: []*cataloguev1.JobPart{suggest(c.oil, "0")}}, http.StatusUnprocessableEntity},
			{"a text quantity", &cataloguev1.Job{Name: "Text", Parts: []*cataloguev1.JobPart{suggest(c.oil, "lots")}}, http.StatusUnprocessableEntity},
			{"too many decimals", &cataloguev1.Job{Name: "Decimals", Parts: []*cataloguev1.JobPart{suggest(c.oil, "1.0005")}}, http.StatusUnprocessableEntity},
			{"31 parts", &cataloguev1.Job{Name: "Many", Parts: many}, http.StatusUnprocessableEntity},
		}

		for _, tt := range tests {
			_, err := api.CreateJob(ctx, garageID, tt.job)
			fixture.RequireAPIError(t, err, tt.status, "")
		}
	})

	t.Run("the list filters by name and category and pages", func(t *testing.T) {
		_, err := api.CreateJob(ctx, garageID, &cataloguev1.Job{Name: "Brake service", Category: "Brakes", LaborPrice: 300_000})
		require.NoError(t, err)

		byName, err := api.ListJobs(ctx, garageID, url.Values{"query": {"oil"}})
		require.NoError(t, err)
		require.Len(t, byName, 1)
		assert.Equal(t, job.GetId(), byName[0].GetId())

		byCategory, err := api.ListJobs(ctx, garageID, url.Values{"category": {"brakes"}})
		require.NoError(t, err)
		require.Len(t, byCategory, 1)
		assert.Equal(t, "Brake service", byCategory[0].GetName())

		none, err := api.ListJobs(ctx, garageID, url.Values{"category": {"Nothing"}})
		require.NoError(t, err)
		assert.Empty(t, none)

		page, err := api.ListJobsPage(ctx, garageID, url.Values{"pageSize": {"1"}})
		require.NoError(t, err)
		assert.Len(t, page.GetData(), 1)
		assert.EqualValues(t, 2, page.GetPagination().GetTotal())
	})
}

// Scenario: E2E-CAT-011, Release 7.3, delete or deactivate (FR-CAT-07): a job nothing
// used is deleted and its name freed; a job a quote line or an order line came
// from is only deactivated; an inactive job is hidden, refused on a quote, and
// can be reactivated; the quote that used it keeps its lines.
func TestJobsDeleteOrDeactivate(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	k := setup(t, "job-delete")
	api, garageID := k.owner.API, k.garageID
	c := k.catalogue(t)

	used := newJob(t, k, "QA Used job", 200_000, suggest(c.oil, "4"))
	unused := newJob(t, k, "QA Unused job", 100_000)

	quote, err := api.CreateQuote(ctx, garageID, &quotev1.Quote{
		CustomerId: k.customer.GetId(), VehicleId: k.vehicle.GetId(),
		Lines: []*quotev1.QuoteLine{{ItemType: quotev1.QuoteItemType_QUOTE_ITEM_TYPE_LABOR, Description: "Check", Quantity: "1", UnitPrice: 50_000}},
	})
	require.NoError(t, err)

	_, err = api.AddQuoteJob(ctx, garageID, quote.GetId(), used.GetId())
	require.NoError(t, err)

	t.Run("the dialog says which it will be", func(t *testing.T) {
		dry, err := api.DeleteJob(ctx, garageID, unused.GetId(), true)
		require.NoError(t, err)
		assert.Equal(t, commonv1.DeleteOutcome_DELETE_OUTCOME_DELETED, dry.GetOutcome())

		dry, err = api.DeleteJob(ctx, garageID, used.GetId(), true)
		require.NoError(t, err)
		assert.Equal(t, commonv1.DeleteOutcome_DELETE_OUTCOME_DEACTIVATED, dry.GetOutcome())

		got, err := api.GetJob(ctx, garageID, used.GetId())
		require.NoError(t, err)
		assert.True(t, got.GetIsActive(), "a dry run changes nothing")
	})

	t.Run("an unused job is deleted and its name is free again", func(t *testing.T) {
		done, err := api.DeleteJob(ctx, garageID, unused.GetId(), false)
		require.NoError(t, err)
		assert.Equal(t, commonv1.DeleteOutcome_DELETE_OUTCOME_DELETED, done.GetOutcome())

		_, err = api.GetJob(ctx, garageID, unused.GetId())
		fixture.RequireAPIError(t, err, http.StatusNotFound, "")

		again, err := api.CreateJob(ctx, garageID, &cataloguev1.Job{Name: "QA Unused job"})
		require.NoError(t, err)
		assert.NotEqual(t, unused.GetId(), again.GetId())
	})

	t.Run("a used job is deactivated and leaves the picker", func(t *testing.T) {
		done, err := api.DeleteJob(ctx, garageID, used.GetId(), false)
		require.NoError(t, err)
		assert.Equal(t, commonv1.DeleteOutcome_DELETE_OUTCOME_DEACTIVATED, done.GetOutcome())

		shown, err := api.ListJobs(ctx, garageID, url.Values{"query": {"QA Used"}})
		require.NoError(t, err)
		assert.Empty(t, shown, "inactive jobs are hidden by default")

		all, err := api.ListJobs(ctx, garageID, url.Values{"query": {"QA Used"}, "includeInactive": {"true"}})
		require.NoError(t, err)
		require.Len(t, all, 1)
		assert.False(t, all[0].GetIsActive())

		fresh, err := api.CreateQuote(ctx, garageID, &quotev1.Quote{
			CustomerId: k.customer.GetId(), VehicleId: k.vehicle.GetId(),
			Lines: []*quotev1.QuoteLine{{ItemType: quotev1.QuoteItemType_QUOTE_ITEM_TYPE_LABOR, Description: "Check", Quantity: "1", UnitPrice: 1}},
		})
		require.NoError(t, err)

		_, err = api.AddQuoteJob(ctx, garageID, fresh.GetId(), used.GetId())
		fixture.RequireAPIError(t, err, http.StatusConflict, "JOB_INACTIVE")

		old, err := api.GetQuote(ctx, garageID, quote.GetId())
		require.NoError(t, err)
		assert.Len(t, old.GetLines(), 3, "the quote that used the job keeps its lines")
	})

	t.Run("it can be reactivated", func(t *testing.T) {
		back, err := api.ActivateJob(ctx, garageID, used.GetId())
		require.NoError(t, err)
		assert.True(t, back.GetIsActive())

		shown, err := api.ListJobs(ctx, garageID, url.Values{"query": {"QA Used"}})
		require.NoError(t, err)
		assert.Len(t, shown, 1)
	})

	t.Run("a job only an order line came from is deactivated too", func(t *testing.T) {
		onOrder := newJob(t, k, "QA Order-only job", 10_000)

		order := newOrder(t, k, laborLine("Work", "1", 1000))

		_, err := api.AddRepairOrderLine(ctx, garageID, order.GetId(), &repairorderv1.RepairOrderLine{
			ItemType:    repairorderv1.RepairOrderItemType_REPAIR_ORDER_ITEM_TYPE_LABOR,
			Description: "From the job", Quantity: "1", UnitPrice: 10_000, JobId: new(onOrder.GetId()),
		})
		require.NoError(t, err)

		done, err := api.DeleteJob(ctx, garageID, onOrder.GetId(), false)
		require.NoError(t, err)
		assert.Equal(t, commonv1.DeleteOutcome_DELETE_OUTCOME_DEACTIVATED, done.GetOutcome())
	})
}

// Scenario: E2E-CAT-012, Release 7.3, protection: a part a job suggests is deactivated
// (not deleted) and its base unit is locked; a packaging unit it suggests
// cannot be removed, though its factor can change; deleting the job releases
// both; a deactivated part is skipped when the job is added to a quote.
func TestJobsProtectTheirPartsAndUnits(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	k := setup(t, "job-protect")
	api, garageID := k.owner.API, k.garageID
	c := k.catalogue(t)

	job := newJob(t, k, "Protect job", 100_000, suggest(c.oil, "1", inUnit(c.jug)), suggest(c.filter, "1"))

	t.Run("the base unit is locked and the part is only deactivated", func(t *testing.T) {
		_, err := api.UpdatePart(ctx, garageID, c.filter.GetId(), &cataloguev1.Part{BaseUnitId: c.litre.GetId()}, "base_unit_id")
		fixture.RequireAPIError(t, err, http.StatusConflict, "")

		dry, err := api.DeletePart(ctx, garageID, c.filter.GetId(), true)
		require.NoError(t, err)
		assert.Equal(t, commonv1.DeleteOutcome_DELETE_OUTCOME_DEACTIVATED, dry.GetOutcome())
	})

	t.Run("the packaging unit a job uses cannot be removed, its factor can change", func(t *testing.T) {
		_, err := api.SetPartUnits(ctx, garageID, c.oil.GetId(), nil)
		fixture.RequireAPIError(t, err, http.StatusConflict, "PACKAGING_UNIT_USED_BY_JOB")

		part, err := api.SetPartUnits(ctx, garageID, c.oil.GetId(), []*cataloguev1.PackagingUnit{{UnitId: c.jug.GetId(), Factor: "4"}})
		require.NoError(t, err)
		require.Len(t, part.GetPackagingUnits(), 1)
		assert.Equal(t, "4", part.GetPackagingUnits()[0].GetFactor())
	})

	t.Run("a deactivated part is skipped when the job is added to a quote", func(t *testing.T) {
		_, err := api.UpdatePart(ctx, garageID, c.filter.GetId(), &cataloguev1.Part{IsActive: false}, "is_active")
		require.NoError(t, err)

		quote, err := api.CreateQuote(ctx, garageID, &quotev1.Quote{
			CustomerId: k.customer.GetId(), VehicleId: k.vehicle.GetId(),
			Lines: []*quotev1.QuoteLine{{ItemType: quotev1.QuoteItemType_QUOTE_ITEM_TYPE_LABOR, Description: "Check", Quantity: "1", UnitPrice: 1}},
		})
		require.NoError(t, err)

		withJob, err := api.AddQuoteJob(ctx, garageID, quote.GetId(), job.GetId())
		require.NoError(t, err)
		require.Len(t, withJob.GetLines(), 3, "the labor line and the oil, not the filter")

		got, err := api.GetJob(ctx, garageID, job.GetId())
		require.NoError(t, err)
		assert.False(t, got.GetParts()[1].GetPartIsActive())
	})

	t.Run("deleting the job releases the unit", func(t *testing.T) {
		other := newJob(t, k, "Free the unit", 0, suggest(c.oil, "1", inUnit(c.jug)))

		_, err := api.DeleteJob(ctx, garageID, other.GetId(), false)
		require.NoError(t, err)

		// The first job still holds it, so delete that one as well (it was used on the quote: deactivated).
		_, err = api.SetPartUnits(ctx, garageID, c.oil.GetId(), nil)
		fixture.RequireAPIError(t, err, http.StatusConflict, "PACKAGING_UNIT_USED_BY_JOB")

		_, err = api.UpdateJob(ctx, garageID, job.GetId(), &cataloguev1.Job{Parts: []*cataloguev1.JobPart{suggest(c.oil, "1")}}, "parts")
		require.NoError(t, err)

		_, err = api.SetPartUnits(ctx, garageID, c.oil.GetId(), nil)
		require.NoError(t, err)
	})
}

// Scenario: E2E-CAT-013, Release 7.3, permissions: jobs are quoting data, so quote.read
// lists and reads, quote.write changes and adds to a quote, a member with
// neither is refused, and another garage cannot see them.
func TestJobsPermissions(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	k := setup(t, "job-perms")
	api, garageID := k.owner.API, k.garageID
	c := k.catalogue(t)
	job := newJob(t, k, "Perm job", 100_000, suggest(c.oil, "1"))

	quote, err := api.CreateQuote(ctx, garageID, &quotev1.Quote{
		CustomerId: k.customer.GetId(), VehicleId: k.vehicle.GetId(),
		Lines: []*quotev1.QuoteLine{{ItemType: quotev1.QuoteItemType_QUOTE_ITEM_TYPE_LABOR, Description: "Check", Quantity: "1", UnitPrice: 1}},
	})
	require.NoError(t, err)

	t.Run("quote.read lists and reads, and changes nothing", func(t *testing.T) {
		reader := staffIn(t, k, "job-reader", "Accountant")

		list, err := reader.API.ListJobs(ctx, garageID, nil)
		require.NoError(t, err)
		assert.Len(t, list, 1)

		got, err := reader.API.GetJob(ctx, garageID, job.GetId())
		require.NoError(t, err)
		assert.Equal(t, "Perm job", got.GetName())

		_, err = reader.API.CreateJob(ctx, garageID, &cataloguev1.Job{Name: "Nope"})
		fixture.RequireAPIError(t, err, http.StatusForbidden, "")

		_, err = reader.API.UpdateJob(ctx, garageID, job.GetId(), &cataloguev1.Job{Name: "Nope"}, "name")
		fixture.RequireAPIError(t, err, http.StatusForbidden, "")

		_, err = reader.API.DeleteJob(ctx, garageID, job.GetId(), false)
		fixture.RequireAPIError(t, err, http.StatusForbidden, "")

		_, err = reader.API.ActivateJob(ctx, garageID, job.GetId())
		fixture.RequireAPIError(t, err, http.StatusForbidden, "")

		_, err = reader.API.AddQuoteJob(ctx, garageID, quote.GetId(), job.GetId())
		fixture.RequireAPIError(t, err, http.StatusForbidden, "")
	})

	t.Run("quote.write does everything", func(t *testing.T) {
		advisor := staffIn(t, k, "job-advisor", "Service Advisor")

		made, err := advisor.API.CreateJob(ctx, garageID, &cataloguev1.Job{Name: "Advisor job", LaborPrice: 1})
		require.NoError(t, err)

		_, err = advisor.API.UpdateJob(ctx, garageID, made.GetId(), &cataloguev1.Job{LaborPrice: 2}, "labor_price")
		require.NoError(t, err)

		_, err = advisor.API.AddQuoteJob(ctx, garageID, quote.GetId(), job.GetId())
		require.NoError(t, err)

		_, err = advisor.API.DeleteJob(ctx, garageID, made.GetId(), false)
		require.NoError(t, err)
	})

	t.Run("a member with no quote permission is refused", func(t *testing.T) {
		keeper := staffIn(t, k, "job-keeper", "Inventory Staff")

		_, err := keeper.API.ListJobs(ctx, garageID, nil)
		fixture.RequireAPIError(t, err, http.StatusForbidden, "")

		_, err = keeper.API.GetJob(ctx, garageID, job.GetId())
		fixture.RequireAPIError(t, err, http.StatusForbidden, "")
	})

	t.Run("another garage sees nothing", func(t *testing.T) {
		other := setup(t, "job-perms-other")

		_, err := other.owner.API.GetJob(ctx, garageID, job.GetId())
		fixture.RequireAPIError(t, err, http.StatusForbidden, "GARAGE_ACCESS_DENIED")

		mine, err := other.owner.API.ListJobs(ctx, other.garageID, nil)
		require.NoError(t, err)
		assert.Empty(t, mine)

		// The same name is free in another garage.
		_, err = other.owner.API.CreateJob(ctx, other.garageID, &cataloguev1.Job{Name: "Perm job"})
		require.NoError(t, err)

		_, err = other.owner.API.GetJob(ctx, other.garageID, job.GetId())
		fixture.RequireAPIError(t, err, http.StatusNotFound, "")
	})

	t.Run("no token is unauthorized", func(t *testing.T) {
		_, err := fixture.Anonymous(t).ListJobs(ctx, garageID, nil)
		fixture.RequireAPIError(t, err, http.StatusUnauthorized, "")
	})
}
