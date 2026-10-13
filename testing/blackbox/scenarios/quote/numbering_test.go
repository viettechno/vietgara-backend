package quote_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	commonv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/common/v1"
	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"
	quotev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/quote/v1"
	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

// businessMonth is the YYYYMM of the garage's business day (UTC+7, the
// default time zone).
func businessMonth() string {
	return time.Now().In(time.FixedZone("ICT", 7*3600)).Format("200601")
}

// Scenario: E2E-QUO-011 (partial): a quote number is rendered from the
// garage's template, sequences are consecutive, and each garage counts
// on its own, even for the same owner.
func TestQuoteNumberFromTemplate(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage1, customer1, vehicle1 := setup(t, "quo-number", fixture.UniqueEmail(t, "quo-number"))

	_, err := owner.API.UpdateGarage(ctx, garage1.GetId(), &tenantv1.Garage{
		Numbering: &tenantv1.NumberingTemplates{Quote: "BG-{YYYY}{MM}-{SEQ:4}"},
	}, "numbering.quote")
	require.NoError(t, err)

	first := draftQuote(t, owner.API, garage1.GetId(), customer1.GetId(), vehicle1.GetId())
	second := draftQuote(t, owner.API, garage1.GetId(), customer1.GetId(), vehicle1.GetId())

	prefix := "BG-" + businessMonth() + "-"
	assert.Equal(t, prefix+"0001", first.GetNumber())
	assert.Equal(t, prefix+"0002", second.GetNumber())

	// A second garage of the same owner has its own sequence.
	fixture.SwitchPlan(t, owner, "pro") // two garages
	garage2, err := owner.API.CreateGarage(ctx, &tenantv1.Garage{Name: "Blackbox Numbers 2"})
	require.NoError(t, err)

	_, err = owner.API.UpdateGarage(ctx, garage2.GetId(), &tenantv1.Garage{
		Numbering: &tenantv1.NumberingTemplates{Quote: "BG-{YYYY}{MM}-{SEQ:4}"},
	}, "numbering.quote")
	require.NoError(t, err)

	phone := fmt.Sprintf("09130%05d", seq.Add(1)%100000)
	customer2, err := owner.API.CreateCustomer(ctx, garage2.GetId(),
		&customerv1.Customer{FullName: "QA Customer Two", Phone: &phone, PersonalDataConsent: true})
	require.NoError(t, err)

	vehicle2, err := owner.API.CreateVehicle(ctx, garage2.GetId(),
		&customerv1.Vehicle{CustomerId: customer2.GetId(), LicensePlate: fmt.Sprintf("51S-%05d", seq.Add(1)%100000)})
	require.NoError(t, err)

	other := draftQuote(t, owner.API, garage2.GetId(), customer2.GetId(), vehicle2.GetId())
	assert.Equal(t, prefix+"0001", other.GetNumber(), "each garage counts independently")
}

// Scenario: E2E-QUO-009 (partial): a quote takes the garage's default
// approval mode, a quote can override it, and the garage's own default is
// unchanged. The pre-selected field and its hint are web-E2E.
func TestDefaultApprovalMode(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner, garage, customer, vehicle := setup(t, "quo-mode", fixture.UniqueEmail(t, "quo-mode"))

	saved, err := owner.API.UpdateGarage(ctx, garage.GetId(), &tenantv1.Garage{
		DefaultQuoteApprovalMode: commonv1.QuoteApprovalMode_QUOTE_APPROVAL_MODE_MANUAL,
	}, "default_quote_approval_mode")
	require.NoError(t, err)
	require.Equal(t, commonv1.QuoteApprovalMode_QUOTE_APPROVAL_MODE_MANUAL, saved.GetDefaultQuoteApprovalMode())

	create := func(mode commonv1.QuoteApprovalMode) *quotev1.Quote {
		q, createErr := owner.API.CreateQuote(ctx, garage.GetId(), &quotev1.Quote{
			CustomerId: customer.GetId(), VehicleId: vehicle.GetId(), ApprovalMode: mode,
			Lines: []*quotev1.QuoteLine{laborLine("Inspection", "1", 100000)},
		})
		require.NoError(t, createErr)

		return q
	}

	inherited := create(commonv1.QuoteApprovalMode_QUOTE_APPROVAL_MODE_UNSPECIFIED)
	assert.Equal(t, commonv1.QuoteApprovalMode_QUOTE_APPROVAL_MODE_MANUAL, inherited.GetApprovalMode(),
		"a quote without a mode takes the garage default")

	overridden := create(commonv1.QuoteApprovalMode_QUOTE_APPROVAL_MODE_LINK)
	assert.Equal(t, commonv1.QuoteApprovalMode_QUOTE_APPROVAL_MODE_LINK, overridden.GetApprovalMode())

	garageAfter, err := owner.API.GetGarage(ctx, garage.GetId())
	require.NoError(t, err)
	assert.Equal(t, commonv1.QuoteApprovalMode_QUOTE_APPROVAL_MODE_MANUAL, garageAfter.GetDefaultQuoteApprovalMode(),
		"the garage default is unchanged by a per-quote choice")

	again := create(commonv1.QuoteApprovalMode_QUOTE_APPROVAL_MODE_UNSPECIFIED)
	assert.Equal(t, commonv1.QuoteApprovalMode_QUOTE_APPROVAL_MODE_MANUAL, again.GetApprovalMode())
}
