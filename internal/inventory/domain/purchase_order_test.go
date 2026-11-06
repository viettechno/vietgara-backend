package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
)

func TestPurchaseOrderStatus_IsValid(t *testing.T) {
	t.Parallel()

	for _, s := range []domain.PurchaseOrderStatus{
		domain.PurchaseOrderDraft, domain.PurchaseOrderSent, domain.PurchaseOrderPartiallyReceived,
		domain.PurchaseOrderReceived, domain.PurchaseOrderCancelled,
	} {
		assert.True(t, s.IsValid(), s)
	}

	assert.False(t, domain.PurchaseOrderStatus("lost").IsValid())
}

func TestPurchaseOrderLine_Quantities(t *testing.T) {
	t.Parallel()

	base := domain.PurchaseOrderLine{EnteredQuantity: "10", Quantity: "10", ReceivedQuantity: "4", UnitCost: 1500}
	assert.Equal(t, "6", base.Outstanding())
	assert.Equal(t, int64(15_000), base.Amount())

	typed, unit := base.OutstandingEntered()
	assert.Equal(t, "6", typed)
	assert.Nil(t, unit, "a base-unit line is typed in the base unit")

	over := domain.PurchaseOrderLine{Quantity: "10", ReceivedQuantity: "12"}
	assert.Equal(t, "0", over.Outstanding(), "a line may receive more than ordered")

	carton := "carton"
	cartons := domain.PurchaseOrderLine{UnitID: &carton, EnteredQuantity: "3", Quantity: "12", ReceivedQuantity: "6"}
	typed, unit = cartons.OutstandingEntered()
	assert.Equal(t, "1.5", typed, "6 of 12 L left of 3 cans is 1.5 cans")
	assert.Equal(t, &carton, unit)

	third := domain.PurchaseOrderLine{UnitID: &carton, EnteredQuantity: "1", Quantity: "3", ReceivedQuantity: "2"}
	typed, unit = third.OutstandingEntered()
	assert.Equal(t, "1", typed, "what a unit cannot show exactly is received in the base unit")
	assert.Nil(t, unit)

	tiny := domain.PurchaseOrderLine{UnitID: &carton, EnteredQuantity: "1", Quantity: "12", ReceivedQuantity: "11.999"}
	typed, unit = tiny.OutstandingEntered()
	assert.Equal(t, "0.001", typed)
	assert.Nil(t, unit, "a tenth of a thousandth of a carton rounds to nothing")

	odd := domain.PurchaseOrderLine{UnitID: &carton, EnteredQuantity: "x", Quantity: "12", ReceivedQuantity: "6"}
	typed, unit = odd.OutstandingEntered()
	assert.Equal(t, "6", typed, "an unreadable quantity falls back to the base amount")
	assert.Nil(t, unit)

	zero := domain.PurchaseOrderLine{UnitID: &carton, EnteredQuantity: "3", Quantity: "0", ReceivedQuantity: "0"}
	typed, _ = zero.OutstandingEntered()
	assert.Equal(t, "0", typed)
}

func TestPurchaseOrder_RecountAndTotals(t *testing.T) {
	t.Parallel()

	order := func(status domain.PurchaseOrderStatus, received ...string) *domain.PurchaseOrder {
		o := &domain.PurchaseOrder{Status: status}
		for _, r := range received {
			o.Lines = append(o.Lines, domain.PurchaseOrderLine{EnteredQuantity: "10", Quantity: "10", ReceivedQuantity: r, UnitCost: 100})
		}

		return o
	}

	cases := map[string]struct {
		o    *domain.PurchaseOrder
		want domain.PurchaseOrderStatus
	}{
		"nothing received is sent":          {order(domain.PurchaseOrderPartiallyReceived, "0", "0"), domain.PurchaseOrderSent},
		"some received is partial":          {order(domain.PurchaseOrderSent, "10", "0"), domain.PurchaseOrderPartiallyReceived},
		"every line reached is received":    {order(domain.PurchaseOrderPartiallyReceived, "10", "12"), domain.PurchaseOrderReceived},
		"received taken back is partial":    {order(domain.PurchaseOrderReceived, "10", "4"), domain.PurchaseOrderPartiallyReceived},
		"a draft keeps its status":          {order(domain.PurchaseOrderDraft, "10"), domain.PurchaseOrderDraft},
		"a cancelled order keeps its state": {order(domain.PurchaseOrderCancelled, "10"), domain.PurchaseOrderCancelled},
		"an order without lines is sent":    {order(domain.PurchaseOrderSent), domain.PurchaseOrderSent},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			c.o.Recount()
			assert.Equal(t, c.want, c.o.Status)
		})
	}

	o := order(domain.PurchaseOrderSent, "0", "0")
	assert.Equal(t, int64(2000), o.Total())
	assert.True(t, o.CanReceive())
	assert.False(t, order(domain.PurchaseOrderDraft).CanReceive())
	assert.True(t, order(domain.PurchaseOrderPartiallyReceived).CanReceive())
	assert.False(t, order(domain.PurchaseOrderReceived).CanReceive())
}

func TestSuggestedQuantity(t *testing.T) {
	t.Parallel()

	cases := []struct{ name, threshold, available, onOrder, want string }{
		{"back to twice the threshold", "10", "4", "", "16"},
		{"what is on order counts", "10", "4", "10", "6"},
		{"already covered", "10", "10", "10", ""},
		{"exactly covered", "10", "20", "", ""},
		{"at least one", "10", "19.5", "", "1"},
		{"decimals", "2.5", "0.25", "", "4.75"},
		{"no threshold, nothing to order", "0", "0", "", ""},
		{"no threshold, but reserved stock", "0", "-3", "", ""},
		{"unreadable threshold", "x", "0", "", ""},
		{"unreadable available", "10", "x", "", ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, c.want, domain.SuggestedQuantity(c.threshold, c.available, c.onOrder))
		})
	}
}
