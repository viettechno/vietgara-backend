package tenant_test

import (
	"bytes"
	"crypto/rand"
	"image"
	"image/png"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"
	identityv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/identity/v1"
	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

// Scenario: E2E-TEN-001 (partial): creating the first garage provisions
// the five default groups and roles and counts usage. The header selector
// and sidebar are web-E2E.
func TestCreateFirstGarage(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwnerWithLocale(t, "first-garage", identityv1.Locale_LOCALE_EN)

	garage, err := owner.API.CreateGarage(ctx, &tenantv1.Garage{Name: "Blackbox First"})
	require.NoError(t, err)
	assert.Equal(t, tenantv1.GarageStatus_GARAGE_STATUS_ACTIVE, garage.GetStatus())

	garages, err := owner.API.ListGarages(ctx)
	require.NoError(t, err)
	require.Len(t, garages, 1)
	assert.Equal(t, garage.GetId(), garages[0].GetId())

	got, err := owner.API.GetGarage(ctx, garage.GetId())
	require.NoError(t, err)
	assert.Equal(t, "Blackbox First", got.GetName())
	assert.Equal(t, owner.Session.GetUser().GetId(), got.GetOwnerUserId())

	groups, err := owner.API.ListStaffGroups(ctx, garage.GetId())
	require.NoError(t, err)

	names := make([]string, 0, len(groups))
	for _, g := range groups {
		names = append(names, g.GetName())
		assert.Zero(t, g.GetMemberCount())
	}

	assert.ElementsMatch(t,
		[]string{"Manager", "Accountant", "Service Advisor", "Technician", "Inventory Staff"}, names)

	roles, err := owner.API.ListRoles(ctx)
	require.NoError(t, err)
	require.Len(t, roles, 5)

	for _, r := range roles {
		assert.True(t, r.GetIsDefault(), r.GetName())
	}

	sub, err := owner.API.MySubscription(ctx)
	require.NoError(t, err)
	assert.Equal(t, int32(1), sub.GetUsage().GetGarages())
}

// Scenario: E2E-TEN-002 (partial): garage profile, configuration and
// opening hours save per field and persist. Per-card copy is web-E2E.
func TestEditGarageProfile(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwnerWithLocale(t, "garage-profile", identityv1.Locale_LOCALE_EN)

	garage, err := owner.API.CreateGarage(ctx, &tenantv1.Garage{Name: "Blackbox Settings"})
	require.NoError(t, err)

	// Configuration defaults on a fresh garage. (Timezone updates are not
	// covered: the stubbed image has no tzdata, so every zone fails
	// validation there; staging/CI images with zoneinfo are unaffected.)
	assert.Equal(t, "Asia/Ho_Chi_Minh", garage.GetTimezone())
	assert.Equal(t, "VND", garage.GetCurrency())

	phone, tax := "0283000199", "0312345678"
	hours := []*tenantv1.DailyHours{
		{Day: 1, Open: "07:30", Close: "17:30"},
		{Day: 2, Open: "07:30", Close: "17:30"},
		{Day: 3, Open: "07:30", Close: "17:30"},
		{Day: 4, Open: "07:30", Close: "17:30"},
		{Day: 5, Open: "07:30", Close: "17:30"},
		{Day: 6, Open: "07:30", Close: "17:30"},
		{Day: 7, Closed: true},
	}
	updated, err := owner.API.UpdateGarage(ctx, garage.GetId(), &tenantv1.Garage{
		Phone: &phone, TaxCode: &tax, Currency: "VND",
		OperatingHours: hours,
	}, "phone", "tax_code", "currency", "operating_hours")
	require.NoError(t, err)
	assert.Equal(t, phone, updated.GetPhone())

	got, err := owner.API.GetGarage(ctx, garage.GetId())
	require.NoError(t, err)
	assert.Equal(t, phone, got.GetPhone())
	assert.Equal(t, tax, got.GetTaxCode())
	assert.Equal(t, "Asia/Ho_Chi_Minh", got.GetTimezone())
	assert.Equal(t, "VND", got.GetCurrency())
	require.Len(t, got.GetOperatingHours(), 7)
	assert.True(t, got.GetOperatingHours()[6].GetClosed())
}

// tinyPNG encodes a 1x1 PNG; bigNoisePNG a ~4 MB noise PNG that fails the
// 1 MB logo limit while decoding as a valid image.
func tinyPNG(t *testing.T) []byte {
	t.Helper()

	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 1, 1))))

	return buf.Bytes()
}

func bigNoisePNG(t *testing.T) []byte {
	t.Helper()

	// ~2.5 MB of noise: over the 1 MB logo limit but under the gateway's
	// 4 MB body cap, so the app (not the edge) refuses it.
	img := image.NewRGBA(image.Rect(0, 0, 800, 800))
	_, err := rand.Read(img.Pix)
	require.NoError(t, err)

	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	require.Greater(t, buf.Len(), 1024*1024)

	return buf.Bytes()
}

// Scenario: E2E-TEN-003 (partial): logo upload, replacement, type and size
// refusal, and removal. Previews and placeholders are web-E2E.
func TestGarageLogo(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwnerWithLocale(t, "garage-logo", identityv1.Locale_LOCALE_EN)

	garage, err := owner.API.CreateGarage(ctx, &tenantv1.Garage{Name: "Blackbox Logo"})
	require.NoError(t, err)

	withLogo, err := owner.API.UploadGarageLogo(ctx, garage.GetId(), tinyPNG(t), "image/png")
	require.NoError(t, err)
	require.NotEmpty(t, withLogo.GetLogoUrl())

	got, err := owner.API.GetGarage(ctx, garage.GetId())
	require.NoError(t, err)
	assert.Equal(t, withLogo.GetLogoUrl(), got.GetLogoUrl())

	replaced, err := owner.API.UploadGarageLogo(ctx, garage.GetId(), tinyPNG(t), "image/png")
	require.NoError(t, err)
	require.NotEmpty(t, replaced.GetLogoUrl())

	_, err = owner.API.UploadGarageLogo(ctx, garage.GetId(), bigNoisePNG(t), "image/png")
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "INVALID_LOGO")

	_, err = owner.API.UploadGarageLogo(ctx, garage.GetId(), []byte("%PDF-1.4 fake"), "image/png")
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "INVALID_LOGO")

	cleared, err := owner.API.DeleteGarageLogo(ctx, garage.GetId())
	require.NoError(t, err)
	assert.Empty(t, cleared.GetLogoUrl())
}

// Scenario: E2E-TEN-004 (partial): a trial owner creating a second garage
// is refused by the plan limit, and no garage is created. The upgrade
// link copy is web-E2E.
func TestTrialGarageLimit(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwnerWithLocale(t, "trial-limit", identityv1.Locale_LOCALE_EN)

	_, err := owner.API.CreateGarage(ctx, &tenantv1.Garage{Name: "Blackbox Trial 1"})
	require.NoError(t, err)

	_, err = owner.API.CreateGarage(ctx, &tenantv1.Garage{Name: "Blackbox Trial 2"})
	fixture.RequireAPIError(t, err, http.StatusPaymentRequired, "PLAN_LIMIT_REACHED")

	garages, err := owner.API.ListGarages(ctx)
	require.NoError(t, err)
	require.Len(t, garages, 1)
}

// Scenario: E2E-TEN-005 (partial): one owner reads each garage's own
// customers — the API substance of switching the active garage. Selector
// persistence is web-E2E.
func TestGarageScopedReads(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwnerWithLocale(t, "scoped-reads", identityv1.Locale_LOCALE_EN)
	upgradeToPro(t, owner)

	gA, err := owner.API.CreateGarage(ctx, &tenantv1.Garage{Name: "Blackbox Scope A"})
	require.NoError(t, err)
	gB, err := owner.API.CreateGarage(ctx, &tenantv1.Garage{Name: "Blackbox Scope B"})
	require.NoError(t, err)

	phoneA, phoneB := "0900000911", "0900000912"
	_, err = owner.API.CreateCustomer(ctx, gA.GetId(), &customerv1.Customer{FullName: "Khách A", Phone: &phoneA, PersonalDataConsent: true})
	require.NoError(t, err)
	_, err = owner.API.CreateCustomer(ctx, gB.GetId(), &customerv1.Customer{FullName: "Khách B", Phone: &phoneB, PersonalDataConsent: true})
	require.NoError(t, err)

	inA, err := owner.API.ListCustomers(ctx, gA.GetId())
	require.NoError(t, err)
	require.Len(t, inA, 1)
	assert.Equal(t, "Khách A", inA[0].GetFullName())

	inB, err := owner.API.ListCustomers(ctx, gB.GetId())
	require.NoError(t, err)
	require.Len(t, inB, 1)
	assert.Equal(t, "Khách B", inB[0].GetFullName())
}

// Scenario: E2E-TEN-006 (partial): deactivating a garage locks staff out
// while the owner still reaches it; reactivating restores access. The
// confirm copy is web-E2E.
func TestDeactivateGarage(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwnerWithLocale(t, "deactivate-garage", identityv1.Locale_LOCALE_EN)

	garage, err := owner.API.CreateGarage(ctx, &tenantv1.Garage{Name: "Blackbox OnOff"})
	require.NoError(t, err)

	groups := groupIDs(t, owner.API, garage.GetId())
	advisor := inviteMember(t, owner.API, garage.GetId(), []string{groups["Service Advisor"]}, "deactivate-staff")

	_, err = advisor.API.ListCustomers(ctx, garage.GetId())
	require.NoError(t, err)

	off, err := owner.API.UpdateGarage(ctx, garage.GetId(),
		&tenantv1.Garage{Status: tenantv1.GarageStatus_GARAGE_STATUS_INACTIVE}, "status")
	require.NoError(t, err)
	assert.Equal(t, tenantv1.GarageStatus_GARAGE_STATUS_INACTIVE, off.GetStatus())

	_, err = advisor.API.ListCustomers(ctx, garage.GetId())
	fixture.RequireAPIError(t, err, http.StatusConflict, "GARAGE_INACTIVE")

	got, err := owner.API.GetGarage(ctx, garage.GetId())
	require.NoError(t, err)
	assert.Equal(t, tenantv1.GarageStatus_GARAGE_STATUS_INACTIVE, got.GetStatus())

	on, err := owner.API.UpdateGarage(ctx, garage.GetId(),
		&tenantv1.Garage{Status: tenantv1.GarageStatus_GARAGE_STATUS_ACTIVE}, "status")
	require.NoError(t, err)
	assert.Equal(t, tenantv1.GarageStatus_GARAGE_STATUS_ACTIVE, on.GetStatus())

	_, err = advisor.API.ListCustomers(ctx, garage.GetId())
	require.NoError(t, err)
}

// Scenario: E2E-TEN-023 (partial): quote numbering templates validate —
// exactly one sequence — and the stored value only changes on valid
// saves. The field hint is web-E2E.
func TestNumberingTemplate(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	owner := fixture.NewVerifiedOwnerWithLocale(t, "numbering", identityv1.Locale_LOCALE_EN)

	garage, err := owner.API.CreateGarage(ctx, &tenantv1.Garage{Name: "Blackbox Numbers"})
	require.NoError(t, err)

	saved, err := owner.API.UpdateGarage(ctx, garage.GetId(), &tenantv1.Garage{
		Numbering: &tenantv1.NumberingTemplates{Quote: "BG-{YYYY}{MM}-{SEQ:4}"},
	}, "numbering.quote")
	require.NoError(t, err)
	assert.Equal(t, "BG-{YYYY}{MM}-{SEQ:4}", saved.GetNumbering().GetQuote())

	for _, bad := range []string{"BG-{YYYY}{MM}", "BG-{SEQ:4}-{SEQ:2}"} {
		_, err = owner.API.UpdateGarage(ctx, garage.GetId(), &tenantv1.Garage{
			Numbering: &tenantv1.NumberingTemplates{Quote: bad},
		}, "numbering.quote")
		fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "VALIDATION_ERROR")
	}

	got, err := owner.API.GetGarage(ctx, garage.GetId())
	require.NoError(t, err)
	assert.Equal(t, "BG-{YYYY}{MM}-{SEQ:4}", got.GetNumbering().GetQuote())
}
