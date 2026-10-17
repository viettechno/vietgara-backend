package usecase_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/tenant/domain"
	"github.com/viettechno/vietgara-backend/internal/tenant/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

func TestCreateGarage(t *testing.T) {
	t.Parallel()

	e := newEnv(t, limits{Garages: 1, Staff: 5})
	ctx := e.as(t, owner, "")

	g, err := e.UC.CreateGarage(ctx, owner, usecase.GarageFields{
		Name: new(" Gara Minh "), TaxCode: new("0101"), Address: new("1 Lê Lợi"), Phone: new("0901"), Email: new("A@B.VN"),
		Status: new(domain.GarageStatusInactive),
	})
	require.NoError(t, err)
	assert.Equal(t, "Gara Minh", g.Garage.Name)
	assert.Equal(t, domain.DefaultTimezone, g.Garage.Timezone)
	assert.Equal(t, domain.GarageStatusActive, g.Garage.Status, "status is ignored on create")
	assert.Equal(t, "a@b.vn", *g.Garage.Email)
	assert.True(t, g.Access.Owner)

	roles, err := e.UC.ListRoles(ctx, owner)
	require.NoError(t, err)
	assert.Len(t, roles, 5)
	assert.Equal(t, "Accountant", roles[0].Name, "owner's locale")

	groups, err := e.UC.ListGroups(ctx, g.Garage.ID)
	require.NoError(t, err)
	assert.Len(t, groups, 5)

	members, err := e.UC.ListStaff(ctx, g.Garage.ID)
	require.NoError(t, err)
	require.Len(t, members, 1)
	assert.True(t, members[0].Owner)
	assert.Equal(t, "Minh", members[0].FullName)

	assert.Contains(t, e.Events.Types, usecase.EventGarageCreated)
	assert.Contains(t, e.Audit.Actions, "garage.created")

	_, err = e.UC.CreateGarage(ctx, owner, usecase.GarageFields{Name: new("Second")})
	requireReason(t, err, "PLAN_LIMIT_REACHED")

	list, err := e.UC.ListGarages(ctx, owner)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.True(t, list[0].Access.Owner)
}

func TestCreateGarageValidation(t *testing.T) {
	t.Parallel()

	e := newEnv(t, limits{Garages: 5, Staff: 5})
	ctx := e.as(t, owner, "")

	for _, f := range []usecase.GarageFields{
		{},
		{Name: new(" ")},
		{Name: new("G"), TaxCode: new("012345678901234567890")},
		{Name: new("G"), Phone: new("012345678901234567890")},
		{Name: new("G"), Email: new("nope")},
		{Name: new("G"), Timezone: new("Mars/Base")},
		{Name: new("G"), Currency: new("dong")},
		{Name: new("G"), OperatingHours: &[]domain.DailyHours{{Day: 9}}},
		{Name: new("G"), QuoteNumberTemplate: new("{SEQ}")},
		{Name: new("G"), RepairOrderNumberTemplate: new("x")},
	} {
		_, err := e.UC.CreateGarage(ctx, owner, f)
		requireReason(t, err, apperror.ReasonValidation)
	}

	e.Store.Fail["usage"] = errBoom
	_, err := e.UC.CreateGarage(ctx, owner, usecase.GarageFields{Name: new("G")})
	requireReason(t, err, apperror.ReasonInternal)

	delete(e.Store.Fail, "usage")
	e.Store.Fail["garage.create"] = errBoom
	_, err = e.UC.CreateGarage(ctx, owner, usecase.GarageFields{Name: new("G")})
	requireReason(t, err, apperror.ReasonInternal)
}

func TestUpdateGarage(t *testing.T) {
	t.Parallel()

	e := newEnv(t, limits{Garages: 1, Staff: 5})
	id := e.garage(t)
	ctx := e.as(t, owner, id)

	hours := []domain.DailyHours{{Day: 1, Open: "07:00", Close: "19:00"}}
	g, err := e.UC.UpdateGarage(ctx, id, usecase.GarageFields{
		Timezone: new("Asia/Bangkok"), Currency: new(" usd "), OperatingHours: &hours,
		QuoteNumberTemplate: new("Q-{SEQ:3}"), RepairOrderNumberTemplate: new("R-{YY}-{SEQ:5}"), Address: new(""),
	})
	require.NoError(t, err)
	assert.Equal(t, "USD", g.Garage.Currency)
	assert.Nil(t, g.Garage.Address)
	assert.Equal(t, hours, g.Garage.OperatingHours)
	assert.Equal(t, owner, *g.Garage.UpdatedBy)

	g, err = e.UC.UpdateGarage(ctx, id, usecase.GarageFields{Status: new(domain.GarageStatusInactive)})
	require.NoError(t, err)
	assert.Equal(t, domain.GarageStatusInactive, g.Garage.Status)

	_, err = e.UC.UpdateGarage(ctx, id, usecase.GarageFields{Status: new(domain.GarageStatus("bogus"))})
	requireReason(t, err, apperror.ReasonValidation)

	_, err = e.UC.UpdateGarage(ctx, id, usecase.GarageFields{Status: new(domain.GarageStatusActive)})
	require.NoError(t, err, "reactivating within the limit")

	_, err = e.UC.UpdateGarage(e.as(t, staff, ""), id, usecase.GarageFields{Status: new(domain.GarageStatusInactive)})
	requireReason(t, err, "OWNER_ONLY")

	got, err := e.UC.GetGarage(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, "Asia/Bangkok", got.Garage.Timezone)

	_, err = e.UC.GetGarage(ctx, "missing")
	requireReason(t, err, apperror.ReasonNotFound)
	_, err = e.UC.UpdateGarage(ctx, "missing", usecase.GarageFields{})
	requireReason(t, err, apperror.ReasonNotFound)

	e.Store.Fail["garage.update"] = errBoom
	_, err = e.UC.UpdateGarage(ctx, id, usecase.GarageFields{})
	requireReason(t, err, apperror.ReasonInternal)

	delete(e.Store.Fail, "garage.update")
	e.Audit.Err = errBoom
	_, err = e.UC.UpdateGarage(ctx, id, usecase.GarageFields{})
	requireReason(t, err, apperror.ReasonInternal)
}

func TestReactivationRespectsLimit(t *testing.T) {
	t.Parallel()

	e := newEnv(t, limits{Garages: 1, Staff: 5})
	first := e.garage(t)
	ctx := e.as(t, owner, first)

	_, err := e.UC.UpdateGarage(ctx, first, usecase.GarageFields{Status: new(domain.GarageStatusInactive)})
	require.NoError(t, err)

	second, err := e.UC.CreateGarage(e.as(t, owner, ""), owner, usecase.GarageFields{Name: new("Second")})
	require.NoError(t, err)
	require.NotNil(t, second)

	_, err = e.UC.UpdateGarage(ctx, first, usecase.GarageFields{Status: new(domain.GarageStatusActive)})
	requireReason(t, err, "PLAN_LIMIT_REACHED")
}

var png = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01")

func TestLogo(t *testing.T) {
	t.Parallel()

	e := newEnv(t, limits{Garages: 1, Staff: 5})
	id := e.garage(t)
	ctx := e.as(t, owner, id)

	g, err := e.UC.UploadGarageLogo(ctx, id, png, "IMAGE/PNG")
	require.NoError(t, err)
	require.NotNil(t, g.Garage.LogoKey)
	first := *g.Garage.LogoKey
	assert.Equal(t, "/files/"+first, *e.UC.LogoURL(g.Garage.LogoKey), "the URL is derived from the key")

	g, err = e.UC.UploadGarageLogo(ctx, id, png, "image/png")
	require.NoError(t, err)
	assert.NotEqual(t, first, *g.Garage.LogoKey)
	assert.Equal(t, []string{first}, e.Logos.Retired, "old logo retired")

	g, err = e.UC.DeleteGarageLogo(ctx, id)
	require.NoError(t, err)
	assert.Nil(t, g.Garage.LogoKey)
	assert.Nil(t, e.UC.LogoURL(g.Garage.LogoKey))
	assert.Len(t, e.Logos.Retired, 2)

	for _, tc := range []struct {
		content []byte
		kind    string
	}{
		{png, "image/gif"},
		{nil, "image/png"},
		{[]byte("not an image"), "image/png"},
	} {
		_, err := e.UC.UploadGarageLogo(ctx, id, tc.content, tc.kind)
		requireReason(t, err, domain.ReasonInvalidLogo)
	}

	e.Logos.Err = errBoom
	_, err = e.UC.UploadGarageLogo(ctx, id, png, "image/png")
	requireReason(t, err, apperror.ReasonInternal)

	e.Logos.Err = nil
	e.Store.Fail["garage.update"] = errBoom
	_, err = e.UC.UploadGarageLogo(ctx, id, png, "image/png")
	requireReason(t, err, apperror.ReasonInternal)
	assert.Len(t, e.Logos.Discarded, 1, "the stored object of a failed change is discarded")
}

func TestListGaragesFailures(t *testing.T) {
	t.Parallel()

	e := newEnv(t, limits{Garages: 1, Staff: 5})
	e.garage(t)

	e.Store.Fail["access"] = errBoom
	_, err := e.UC.ListGarages(t.Context(), owner)
	requireReason(t, err, apperror.ReasonInternal)

	e.Store.Fail["garage.list"] = errBoom
	_, err = e.UC.ListGarages(t.Context(), owner)
	requireReason(t, err, apperror.ReasonInternal)
}

// TestLegalProfile covers FR-TEN-06: the garage's legal identity, bank
// account and voucher numbering template.
func TestLegalProfile(t *testing.T) {
	t.Parallel()

	e := newEnv(t, limits{Garages: 1, Staff: 5})
	id := e.garage(t)
	ctx := e.as(t, owner, id)

	g, err := e.UC.GetGarage(ctx, id)
	require.NoError(t, err)
	assert.Nil(t, g.Garage.LegalName)
	assert.Equal(t, domain.DefaultPaymentVoucherNumberTemplate, g.Garage.PaymentVoucherNumberTemplate)

	g, err = e.UC.UpdateGarage(ctx, id, usecase.GarageFields{
		LegalName: new("  Cong ty TNHH Gara An Phat "), LegalAddress: new("12 Nguyen Hue, Q1"), Representative: new("Nguyen Van An"),
		Bank:                         &domain.BankDetails{BankName: new("Vietcombank"), AccountNumber: new(" 0011 2233 "), AccountHolder: new("")},
		PaymentVoucherNumberTemplate: new("PT-{YYYY}-{SEQ:5}"),
	})
	require.NoError(t, err)
	assert.Equal(t, "Cong ty TNHH Gara An Phat", *g.Garage.LegalName)
	assert.Equal(t, "Nguyen Van An", *g.Garage.Representative)
	assert.Equal(t, "Vietcombank", *g.Garage.Bank.BankName)
	assert.Equal(t, "0011 2233", *g.Garage.Bank.AccountNumber)
	assert.Nil(t, g.Garage.Bank.AccountHolder, "blank clears")
	assert.Equal(t, "PT-{YYYY}-{SEQ:5}", g.Garage.PaymentVoucherNumberTemplate)

	// A blank value clears; omitted fields are kept; a bank block replaces the account.
	g, err = e.UC.UpdateGarage(ctx, id, usecase.GarageFields{
		LegalAddress: new(" "), Bank: &domain.BankDetails{AccountHolder: new("An Phat")},
	})
	require.NoError(t, err)
	assert.Nil(t, g.Garage.LegalAddress)
	assert.Equal(t, "Cong ty TNHH Gara An Phat", *g.Garage.LegalName)
	assert.Nil(t, g.Garage.Bank.BankName)
	assert.Equal(t, "An Phat", *g.Garage.Bank.AccountHolder)

	long := string(make([]rune, 256))

	for name, fields := range map[string]usecase.GarageFields{
		"legal name":      {LegalName: &long},
		"representative":  {Representative: &long},
		"legal address":   {LegalAddress: new(string(make([]rune, 501)))},
		"bank name":       {Bank: &domain.BankDetails{BankName: new(string(make([]rune, 101)))}},
		"bank account":    {Bank: &domain.BankDetails{AccountNumber: new(string(make([]rune, 101)))}},
		"bank holder":     {Bank: &domain.BankDetails{AccountHolder: new(string(make([]rune, 101)))}},
		"voucher numbers": {PaymentVoucherNumberTemplate: new("PT-no-sequence")},
	} {
		_, err := e.UC.UpdateGarage(ctx, id, fields)
		requireReason(t, err, apperror.ReasonValidation, name)
	}
}

// TestStockSettings covers the Release 7.0 garage configuration: the goods
// receipt and stock issue numbering templates and when a stock issue posts.
func TestStockSettings(t *testing.T) {
	t.Parallel()

	e := newEnv(t, limits{Garages: 1, Staff: 5})
	id := e.garage(t)
	ctx := e.as(t, owner, id)

	g, err := e.UC.GetGarage(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, domain.DefaultGoodsReceiptNumberTemplate, g.Garage.GoodsReceiptNumberTemplate)
	assert.Equal(t, domain.DefaultStockIssueNumberTemplate, g.Garage.StockIssueNumberTemplate)
	assert.Equal(t, domain.StockIssueModeOnClose, g.Garage.StockIssueMode)

	mode := domain.StockIssueModeOnStart

	g, err = e.UC.UpdateGarage(ctx, id, usecase.GarageFields{
		GoodsReceiptNumberTemplate: new("NK-{YYYY}-{SEQ:5}"), StockIssueNumberTemplate: new("XK-{SEQ:3}"), StockIssueMode: &mode,
	})
	require.NoError(t, err)
	assert.Equal(t, "NK-{YYYY}-{SEQ:5}", g.Garage.GoodsReceiptNumberTemplate)
	assert.Equal(t, "XK-{SEQ:3}", g.Garage.StockIssueNumberTemplate)
	assert.Equal(t, domain.StockIssueModeOnStart, g.Garage.StockIssueMode)

	bad := domain.StockIssueMode("sometimes")

	for name, fields := range map[string]usecase.GarageFields{
		"goods receipt numbers": {GoodsReceiptNumberTemplate: new("NK-no-sequence")},
		"stock issue numbers":   {StockIssueNumberTemplate: new("XK-no-sequence")},
		"stock issue mode":      {StockIssueMode: &bad},
	} {
		_, err := e.UC.UpdateGarage(ctx, id, fields)
		requireReason(t, err, apperror.ReasonValidation, name)
	}
}
