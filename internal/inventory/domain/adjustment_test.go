package domain_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
)

func countLine(system string, counted, cost *string) *domain.StockCountLine {
	return &domain.StockCountLine{SystemQuantity: system, Counted: counted, EnteredCounted: counted, AverageCost: cost}
}

func TestStockCountLine(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		line       *domain.StockCountLine
		diff       string
		counted    bool
		result     domain.CountResult
		value      *int64
		wantResult string
	}{
		{name: "uncounted", line: countLine("5", nil, new("100")), result: domain.CountUncounted},
		{name: "matched", line: countLine("5", new("5"), new("100")), diff: "0", counted: true, result: domain.CountMatched, value: new(int64(0))},
		{name: "over", line: countLine("5", new("7.5"), new("100")), diff: "2.5", counted: true, result: domain.CountOver, value: new(int64(250))},
		{name: "short", line: countLine("5", new("2"), new("33.3333")), diff: "-3", counted: true, result: domain.CountShort, value: new(int64(-100))},
		{name: "negative system quantity", line: countLine("-2", new("0"), new("10")), diff: "2", counted: true, result: domain.CountOver, value: new(int64(20))},
		{name: "unknown cost", line: countLine("5", new("6"), nil), diff: "1", counted: true, result: domain.CountOver},
		{name: "unparsable cost", line: countLine("5", new("6"), new("abc")), diff: "1", counted: true, result: domain.CountOver},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			diff, ok := tc.line.Difference()
			assert.Equal(t, tc.counted, ok)
			assert.Equal(t, tc.diff, diff)
			assert.Equal(t, tc.result, tc.line.Result())
			assert.Equal(t, tc.value, tc.line.DifferenceValue())
		})
	}
}

func TestStockCount_Summarise(t *testing.T) {
	t.Parallel()

	c := &domain.StockCount{Lines: []domain.StockCountLine{
		*countLine("5", nil, new("1")), *countLine("5", new("5"), new("1")), *countLine("5", new("9"), new("100")), *countLine("5", new("1"), new("10")),
	}}
	c.Lines[0].ID, c.Lines[0].PartID = "a", "pa"
	c.Lines[1].ID, c.Lines[1].PartID = "b", "pb"
	c.Summarise()

	assert.Equal(t, domain.Tally{Uncounted: 1, Matched: 1, Over: 1, Short: 1}, c.Tally)
	assert.EqualValues(t, 4*100-4*10, c.TotalDifferenceValue)

	c.Summarise()
	assert.EqualValues(t, 360, c.TotalDifferenceValue, "summarising twice does not double count")

	assert.Equal(t, 1, c.LineByID("b"))
	assert.Equal(t, -1, c.LineByID("zz"))
	assert.Equal(t, 0, c.LineByPart("pa"))
	assert.Equal(t, -1, c.LineByPart("zz"))
}

func TestStockCountStatusAndCountedQuantity(t *testing.T) {
	t.Parallel()

	assert.True(t, domain.StockCountDraft.IsValid())
	assert.True(t, domain.StockCountCompleted.IsValid())
	assert.True(t, domain.StockCountCancelled.IsValid())
	assert.False(t, domain.StockCountStatus("x").IsValid())

	for _, ok := range []string{"0", "0.000", "12.5", " 3 "} {
		_, err := domain.ParseCountedQuantity(ok)
		require.NoError(t, err, ok)
	}

	for _, bad := range []string{"", "-1", "1.2345", "abc", "12345678901"} {
		_, err := domain.ParseCountedQuantity(bad)
		require.ErrorIs(t, err, domain.ErrInvalidQuantity, bad)
	}
}

func TestAdjustments(t *testing.T) {
	t.Parallel()

	assert.True(t, domain.AdjustmentCompleted.IsValid())
	assert.True(t, domain.AdjustmentCancelled.IsValid())
	assert.False(t, domain.AdjustmentStatus("").IsValid())

	for _, r := range []domain.WriteOffReason{
		domain.WriteOffDamaged, domain.WriteOffLost, domain.WriteOffExpired, domain.WriteOffInternalUse, domain.WriteOffWarrantyReplacement,
	} {
		assert.True(t, r.IsValid(), r)
	}

	assert.False(t, domain.WriteOffReason("stolen").IsValid())

	line := domain.AdjustmentLine{EnteredQuantity: "2", Quantity: "24", AverageCost: new("12.5"), CreditUnitCost: 150}
	assert.EqualValues(t, 300, line.Value())
	assert.EqualValues(t, 300, line.Amount())
	assert.Zero(t, (&domain.AdjustmentLine{Quantity: "24"}).Value(), "an unknown cost is worth nothing")

	w := &domain.WriteOff{Lines: []domain.AdjustmentLine{line, line}}
	assert.EqualValues(t, 600, w.Total())

	s := &domain.SupplierReturn{Lines: []domain.AdjustmentLine{line, {EnteredQuantity: "1", CreditUnitCost: 7}}}
	assert.EqualValues(t, 307, s.Total())
}

func TestCreditPerEnteredUnit(t *testing.T) {
	t.Parallel()

	assert.EqualValues(t, 1200, domain.CreditPerEnteredUnit("100", "12", "1"))
	assert.EqualValues(t, 50, domain.CreditPerEnteredUnit("100", "1", "2"))
	assert.EqualValues(t, 33, domain.CreditPerEnteredUnit("33.3333", "1", "1"))
	assert.Zero(t, domain.CreditPerEnteredUnit("0", "1", "1"))
	assert.Zero(t, domain.CreditPerEnteredUnit("x", "1", "1"))
	assert.Zero(t, domain.CreditPerEnteredUnit("1", "x", "1"))
	assert.Zero(t, domain.CreditPerEnteredUnit("1", "1", "0"))
}

func TestValueDong(t *testing.T) {
	t.Parallel()

	n, ok := domain.ValueDong("2.5", "100.5")
	assert.True(t, ok)
	assert.EqualValues(t, 251, n, "250.5 rounds half up")

	n, ok = domain.ValueDong("-2.5", "100.5")
	assert.True(t, ok)
	assert.EqualValues(t, -251, n)

	_, ok = domain.ValueDong("a", "1")
	assert.False(t, ok)

	_, ok = domain.ValueDong("1", "b")
	assert.False(t, ok)
}

func TestNormalizeReason(t *testing.T) {
	t.Parallel()

	got, err := domain.NormalizeReason("  customer declined ")
	require.NoError(t, err)
	assert.Equal(t, "customer declined", got)

	_, err = domain.NormalizeReason("   ")
	require.ErrorIs(t, err, domain.ErrReasonRequired)

	_, err = domain.NormalizeReason(strings.Repeat("ạ", domain.MaxReasonLength+1))
	require.ErrorIs(t, err, domain.ErrReasonTooLong)

	_, err = domain.NormalizeReason(strings.Repeat("ạ", domain.MaxReasonLength))
	require.NoError(t, err, "the limit counts characters, not bytes")
}

func TestRefType(t *testing.T) {
	t.Parallel()

	all := []domain.RefType{
		domain.RefPurchase, domain.RefRepairOrder, domain.RefGoodsReceipt, domain.RefStockIssue, domain.RefStockCount,
		domain.RefWriteOff, domain.RefSupplierReturn, domain.RefRepairOrderReturn,
	}
	for _, r := range all {
		assert.True(t, r.IsValid(), r)
	}

	assert.False(t, domain.RefType("x").IsValid())

	for _, r := range []domain.RefType{domain.RefStockCount, domain.RefWriteOff, domain.RefSupplierReturn} {
		assert.True(t, r.IsAdjustment(), r)
	}

	for _, r := range []domain.RefType{domain.RefPurchase, domain.RefRepairOrder, domain.RefGoodsReceipt, domain.RefStockIssue, domain.RefRepairOrderReturn, "x"} {
		assert.False(t, r.IsAdjustment(), r)
	}
}
