package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/quote/domain"
)

func TestStatus(t *testing.T) {
	t.Parallel()

	for _, s := range []domain.Status{domain.StatusDraft, domain.StatusSent, domain.StatusApproved, domain.StatusRejected, domain.StatusConverted} {
		assert.True(t, s.IsValid())
	}

	assert.False(t, domain.Status("pending").IsValid())
	assert.True(t, domain.StatusDraft.Editable())
	assert.False(t, domain.StatusSent.Editable())
	assert.False(t, domain.StatusApproved.Editable())

	assert.True(t, domain.StatusDraft.Sendable())
	assert.True(t, domain.StatusSent.Sendable())
	assert.False(t, domain.StatusApproved.Sendable())
	assert.False(t, domain.StatusConverted.Sendable())

	assert.True(t, domain.StatusSent.Decidable())
	assert.False(t, domain.StatusDraft.Decidable())
	assert.False(t, domain.StatusApproved.Decidable())
}

func TestApprovalMode(t *testing.T) {
	t.Parallel()

	assert.True(t, domain.ApprovalModeLink.IsValid())
	assert.True(t, domain.ApprovalModeManual.IsValid())
	assert.False(t, domain.ApprovalMode("email").IsValid())
}

func TestItemType(t *testing.T) {
	t.Parallel()

	assert.True(t, domain.ItemTypeLabor.IsValid())
	assert.True(t, domain.ItemTypePart.IsValid())
	assert.False(t, domain.ItemType("consumable").IsValid())
}

func TestCheckLength(t *testing.T) {
	t.Parallel()

	require.NoError(t, domain.CheckLength("short", 10))
	require.ErrorIs(t, domain.CheckLength("too long a value", 5), domain.ErrFieldTooLong)
}

func TestParseQuantity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      string
		want    int64
		wantErr bool
	}{
		{name: "integer", in: "1", want: 1000},
		{name: "one decimal", in: "1.5", want: 1500},
		{name: "two decimals", in: "0.25", want: 250},
		{name: "three decimals", in: "0.125", want: 125},
		{name: "trims whitespace", in: " 2 ", want: 2000},
		{name: "zero rejected", in: "0", wantErr: true},
		{name: "negative rejected", in: "-1", wantErr: true},
		{name: "too many decimals rejected", in: "1.2345", wantErr: true},
		{name: "not a number rejected", in: "abc", wantErr: true},
		{name: "empty rejected", in: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := domain.ParseQuantity(tt.in)
			if tt.wantErr {
				require.ErrorIs(t, err, domain.ErrInvalidQuantity)

				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestLineTotal(t *testing.T) {
	t.Parallel()

	assert.Equal(t, int64(250_000), domain.LineTotal(1000, 250_000)) // 1 x 250,000
	assert.Equal(t, int64(375_000), domain.LineTotal(1500, 250_000)) // 1.5 x 250,000
	assert.Equal(t, int64(332_500), domain.LineTotal(1330, 250_000)) // 1.33 x 250,000
	assert.Equal(t, int64(443), domain.LineTotal(1330, 333))         // 1.33 x 333 = 442.89, rounds to 443
	assert.Equal(t, int64(125), domain.LineTotal(125, 1000))         // 0.125 x 1,000
}

func TestQuote_Recalculate(t *testing.T) {
	t.Parallel()

	q := &domain.Quote{Lines: []domain.Line{{LineTotal: 250_000}, {LineTotal: 480_000}}}
	q.Recalculate()
	assert.Equal(t, int64(730_000), q.TotalAmount)

	q = &domain.Quote{}
	q.Recalculate()
	assert.Equal(t, int64(0), q.TotalAmount)
}

func TestPublicToken(t *testing.T) {
	t.Parallel()

	a := domain.NewPublicToken()
	b := domain.NewPublicToken()
	require.NotEqual(t, a, b)
	require.Len(t, a, 64)

	hashA1, hashA2 := domain.HashPublicToken(a), domain.HashPublicToken(a)
	require.Equal(t, hashA1, hashA2, "hashing the same token twice must be deterministic")
	require.NotEqual(t, domain.HashPublicToken(a), domain.HashPublicToken(b))
}
