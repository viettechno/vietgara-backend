// Scenario: E2E-PO-001 to E2E-PO-007
// docs: vietgara-docs/docs/05-Testing/E2E_Scenarios/16_Purchase_Orders.md
package inventory_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	cataloguev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/catalogue/v1"
	documentv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/document/v1"
	identityv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/identity/v1"
	inventoryv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/inventory/v1"
	notificationv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/notification/v1"
	supplierv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/supplier/v1"
	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/client"
	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

// orderKit is a garage that buys oil, a filter and a wiper: oil and filter
// from one supplier, the wiper from another, and a loose part from none.
type orderKit struct {
	owner    *fixture.Actor
	garageID string
	unit     *cataloguev1.Unit

	sup1, sup2, noMail        *supplierv1.Supplier
	email1                    string
	oil, filter, wiper, loose *cataloguev1.Part
}

func newOrderKit(t *testing.T, label string) *orderKit {
	t.Helper()

	ctx := t.Context()
	// English, so the default group names are the ones joinAs looks for.
	owner := fixture.NewVerifiedOwnerWithLocale(t, label, identityv1.Locale_LOCALE_EN)

	garage, err := owner.API.CreateGarage(ctx, &tenantv1.Garage{Name: "Blackbox " + label})
	require.NoError(t, err)

	k := &orderKit{owner: owner, garageID: garage.GetId(), email1: fixture.UniqueEmail(t, "po-supplier")}
	k.unit = fixture.NewUnit(t, owner.API, k.garageID, "Piece", "pc")

	k.sup1, err = owner.API.CreateSupplier(ctx, k.garageID, &supplierv1.Supplier{Name: "Sao Mai " + sku(), Contact: &supplierv1.SupplierContact{Email: &k.email1}})
	require.NoError(t, err)

	email2 := fixture.UniqueEmail(t, "po-supplier2")
	k.sup2, err = owner.API.CreateSupplier(ctx, k.garageID, &supplierv1.Supplier{Name: "Bolt " + sku(), Contact: &supplierv1.SupplierContact{Email: &email2}})
	require.NoError(t, err)

	k.noMail, err = owner.API.CreateSupplier(ctx, k.garageID, &supplierv1.Supplier{Name: "No mail " + sku()})
	require.NoError(t, err)

	k.oil = k.part(t, "Dầu nhớt 5W-30", "10", "4", k.sup1)
	k.filter = k.part(t, "Lọc dầu", "5", "2", k.sup1)
	k.wiper = k.part(t, "Gạt mưa 24", "4", "4", k.sup2)
	k.loose = k.part(t, "Phụ tùng lẻ", "3", "1", nil)

	return k
}

// part adds a part with a reorder threshold and stock; its default supplier
// is supplier when it has one.
func (k *orderKit) part(t *testing.T, name, threshold, stock string, supplier *supplierv1.Supplier) *cataloguev1.Part {
	t.Helper()

	ctx := t.Context()
	p := fixture.NewPart(t, k.owner.API, k.garageID, sku(), name, k.unit, fixture.WithThreshold(threshold))

	_, err := k.owner.API.ReceiveStock(ctx, k.garageID, p.GetId(), stock, "", 90_000)
	require.NoError(t, err)

	if supplier != nil {
		_, err = k.owner.API.PutPartSuppliers(ctx, k.garageID, p.GetId(), []*inventoryv1.PartSupplier{{SupplierId: supplier.GetId(), IsDefault: true}})
		require.NoError(t, err)
	}

	return p
}

func (k *orderKit) draft(t *testing.T, supplier *supplierv1.Supplier, lines ...*inventoryv1.PurchaseOrderLine) *inventoryv1.PurchaseOrder {
	t.Helper()

	o, err := k.owner.API.CreatePurchaseOrder(t.Context(), k.garageID, &inventoryv1.PurchaseOrder{SupplierId: supplier.GetId(), Lines: lines})
	require.NoError(t, err)

	return o
}

func poLine(part *cataloguev1.Part, quantity string, cost int64) *inventoryv1.PurchaseOrderLine {
	l := &inventoryv1.PurchaseOrderLine{PartId: part.GetId(), EnteredQuantity: quantity}
	if cost > 0 {
		l.UnitCost = &cost
	}

	return l
}

func bySupplier(orders []*inventoryv1.PurchaseOrder) map[string]*inventoryv1.PurchaseOrder {
	out := map[string]*inventoryv1.PurchaseOrder{}
	for _, o := range orders {
		out[o.GetSupplierId()] = o
	}

	return out
}

func quantities(o *inventoryv1.PurchaseOrder) map[string]string {
	out := map[string]string{}
	for _, l := range o.GetLines() {
		out[l.GetPartId()] = l.GetQuantity()
	}

	return out
}

// Scenario: E2E-PO-001: the low-stock list becomes one draft per default
// supplier, for the quantity that brings the available stock plus what is on
// order back to twice the threshold; asking twice orders nothing twice.
func TestPurchaseOrdersFromLowStock(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	k := newOrderKit(t, "po-low")

	res, err := k.owner.API.CreatePurchaseOrdersFromLowStock(ctx, k.garageID, nil, "")
	require.NoError(t, err)
	require.Len(t, res.GetOrders(), 2)

	orders := bySupplier(res.GetOrders())
	first, second := orders[k.sup1.GetId()], orders[k.sup2.GetId()]
	require.NotNil(t, first)
	require.NotNil(t, second)

	assert.Equal(t, map[string]string{k.oil.GetId(): "16", k.filter.GetId(): "8"}, quantities(first))
	assert.Equal(t, map[string]string{k.wiper.GetId(): "4"}, quantities(second))

	for _, o := range res.GetOrders() {
		assert.Equal(t, inventoryv1.PurchaseOrderStatus_PURCHASE_ORDER_STATUS_DRAFT, o.GetStatus())
		assert.Equal(t, time.Now().Format(time.DateOnly), o.GetOrderDate())
		assert.Regexp(t, `^DH-\d{4}-\d{5}$`, o.GetNumber())
	}

	assert.NotEqual(t, first.GetNumber(), second.GetNumber())

	require.Len(t, res.GetUnassignedParts(), 1)
	assert.Equal(t, k.loose.GetId(), res.GetUnassignedParts()[0].GetPartId())

	// Again: what is on the drafts already covers every part.
	again, err := k.owner.API.CreatePurchaseOrdersFromLowStock(ctx, k.garageID, nil, "")
	require.NoError(t, err)
	assert.Empty(t, again.GetOrders())

	// Cancel the drafts and restock the wiper: only the first supplier's order comes back.
	for _, o := range res.GetOrders() {
		_, err = k.owner.API.CancelPurchaseOrder(ctx, k.garageID, o.GetId())
		require.NoError(t, err)
	}

	_, err = k.owner.API.ReceiveStock(ctx, k.garageID, k.wiper.GetId(), "6", "", 90_000)
	require.NoError(t, err)

	third, err := k.owner.API.CreatePurchaseOrdersFromLowStock(ctx, k.garageID, nil, "")
	require.NoError(t, err)
	require.Len(t, third.GetOrders(), 1)
	assert.Equal(t, k.sup1.GetId(), third.GetOrders()[0].GetSupplierId())

	// Naming one part limits the order to it.
	named, err := k.owner.API.CreatePurchaseOrdersFromLowStock(ctx, k.garageID, []string{k.loose.GetId()}, "")
	require.NoError(t, err)
	assert.Empty(t, named.GetOrders())
	assert.Len(t, named.GetUnassignedParts(), 1)
}

// groupWith adds a custom role holding permissions and a group on it, and a
// member of that group.
func (k *orderKit) memberWith(t *testing.T, label string, permissions ...tenantv1.Permission) *fixture.Actor {
	t.Helper()

	ctx := t.Context()

	role, err := k.owner.API.CreateRole(ctx, &tenantv1.Role{Name: "PO " + label + " " + sku(), Permissions: permissions})
	require.NoError(t, err)

	group, err := k.owner.API.CreateStaffGroup(ctx, k.garageID, &tenantv1.StaffGroup{Name: "PO " + label + " " + sku(), RoleId: role.GetId()})
	require.NoError(t, err)

	return joinAs(t, k.owner, k.garageID, group.GetName(), label)
}

// Scenario: E2E-PO-002: a draft is replaced as a whole; quantities in packaging
// units are converted; prices follow the cost permission, and a caller without
// it keeps the stored costs of the lines it sends back.
func TestPurchaseOrderEditsADraft(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	k := newOrderKit(t, "po-edit")
	can := fixture.NewUnit(t, k.owner.API, k.garageID, "Can", "can")
	carton := fixture.NewUnit(t, k.owner.API, k.garageID, "Case", "case")

	_, err := k.owner.API.SetPartUnits(ctx, k.garageID, k.oil.GetId(), []*cataloguev1.PackagingUnit{{UnitId: can.GetId(), Factor: "4"}})
	require.NoError(t, err)

	_, err = k.owner.API.SetPartUnits(ctx, k.garageID, k.filter.GetId(), []*cataloguev1.PackagingUnit{{UnitId: carton.GetId(), Factor: "24"}})
	require.NoError(t, err)

	order := k.draft(t, k.sup1, poLine(k.oil, "1", 0))
	tomorrow := time.Now().AddDate(0, 0, 1).Format(time.DateOnly)

	oilLine := poLine(k.oil, "3", 460_000)
	oilLine.UnitId = new(can.GetId())
	filterLine := poLine(k.filter, "1", 0)
	filterLine.UnitId = new(carton.GetId())

	edited, err := k.owner.API.UpdatePurchaseOrder(ctx, k.garageID, order.GetId(), &inventoryv1.PurchaseOrder{
		SupplierId: k.sup1.GetId(), ExpectedDate: tomorrow, Notes: "Giao buổi sáng", Lines: []*inventoryv1.PurchaseOrderLine{oilLine, filterLine},
	})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{k.oil.GetId(): "12", k.filter.GetId(): "24"}, quantities(edited))
	assert.Equal(t, tomorrow, edited.GetExpectedDate())
	assert.Equal(t, int64(1_380_000), edited.GetLines()[0].GetLineAmount())
	assert.Equal(t, int64(1_380_000), edited.GetTotalAmount())

	// A reader without the cost permission sees quantities only.
	advisor := joinAs(t, k.owner, k.garageID, "Service Advisor", "po-advisor")

	read, err := advisor.API.GetPurchaseOrder(ctx, k.garageID, order.GetId())
	require.NoError(t, err)
	assert.Nil(t, read.TotalAmount)
	assert.Nil(t, read.GetLines()[0].UnitCost)
	assert.Nil(t, read.GetLines()[0].LineAmount)

	// A writer without it keeps the stored cost of the line it sends back; a line whose unit changed costs 0.
	clerk := k.memberWith(t, "po-clerk", tenantv1.Permission_PERMISSION_INVENTORY_WRITE)

	kept := read.GetLines()
	kept[0].EnteredQuantity = "5"

	saved, err := clerk.API.UpdatePurchaseOrder(ctx, k.garageID, order.GetId(), &inventoryv1.PurchaseOrder{SupplierId: k.sup1.GetId(), Lines: kept})
	require.NoError(t, err)
	assert.Equal(t, "20", quantities(saved)[k.oil.GetId()])

	asOwner, err := k.owner.API.GetPurchaseOrder(ctx, k.garageID, order.GetId())
	require.NoError(t, err)
	assert.Equal(t, int64(460_000), asOwner.GetLines()[0].GetUnitCost(), "the stored cost stays")
	assert.Equal(t, int64(2_300_000), asOwner.GetLines()[0].GetLineAmount())

	changed := asOwner.GetLines()
	changed[0].UnitId, changed[0].EnteredQuantity = nil, "8"

	saved, err = clerk.API.UpdatePurchaseOrder(ctx, k.garageID, order.GetId(), &inventoryv1.PurchaseOrder{SupplierId: k.sup1.GetId(), Lines: stripCosts(changed)})
	require.NoError(t, err)
	assert.Equal(t, "8", quantities(saved)[k.oil.GetId()])

	asOwner, err = k.owner.API.GetPurchaseOrder(ctx, k.garageID, order.GetId())
	require.NoError(t, err)
	assert.Zero(t, asOwner.GetLines()[0].GetUnitCost(), "a line whose unit changed costs nothing")

	// Invalid input is refused and the order is unchanged.
	past := time.Now().AddDate(0, 0, -1).Format(time.DateOnly)
	foreign := fixture.NewBusiness(t, "po-foreign")
	otherSupplier, err := foreign.Owner.API.CreateSupplier(ctx, foreign.GarageID, &supplierv1.Supplier{Name: "Elsewhere"})
	require.NoError(t, err)

	wrongUnit := poLine(k.oil, "1", 0)
	wrongUnit.UnitId = new(carton.GetId())

	invalid := map[string]struct {
		order  *inventoryv1.PurchaseOrder
		status int
		code   string
	}{
		"a part twice":       {&inventoryv1.PurchaseOrder{SupplierId: k.sup1.GetId(), Lines: []*inventoryv1.PurchaseOrderLine{poLine(k.oil, "1", 0), poLine(k.oil, "2", 0)}}, http.StatusUnprocessableEntity, ""},
		"zero quantity":      {&inventoryv1.PurchaseOrder{SupplierId: k.sup1.GetId(), Lines: []*inventoryv1.PurchaseOrderLine{poLine(k.oil, "0", 0)}}, http.StatusUnprocessableEntity, ""},
		"another part unit":  {&inventoryv1.PurchaseOrder{SupplierId: k.sup1.GetId(), Lines: []*inventoryv1.PurchaseOrderLine{wrongUnit}}, http.StatusUnprocessableEntity, ""},
		"a date in the past": {&inventoryv1.PurchaseOrder{SupplierId: k.sup1.GetId(), ExpectedDate: past, Lines: []*inventoryv1.PurchaseOrderLine{poLine(k.oil, "1", 0)}}, http.StatusUnprocessableEntity, ""},
		"no lines":           {&inventoryv1.PurchaseOrder{SupplierId: k.sup1.GetId()}, http.StatusUnprocessableEntity, ""},
		"a foreign supplier": {&inventoryv1.PurchaseOrder{SupplierId: otherSupplier.GetId(), Lines: []*inventoryv1.PurchaseOrderLine{poLine(k.oil, "1", 0)}}, http.StatusNotFound, "SUPPLIER_NOT_FOUND"},
	}

	for name, c := range invalid {
		_, err = k.owner.API.UpdatePurchaseOrder(ctx, k.garageID, order.GetId(), c.order)
		fixture.RequireAPIError(t, err, c.status, c.code)

		_ = name
	}

	unchanged, err := k.owner.API.GetPurchaseOrder(ctx, k.garageID, order.GetId())
	require.NoError(t, err)
	assert.Equal(t, "8", quantities(unchanged)[k.oil.GetId()])

	// Once sent, the order cannot be replaced.
	_, err = k.owner.API.SendPurchaseOrder(ctx, k.garageID, order.GetId(), "")
	require.NoError(t, err)

	_, err = k.owner.API.UpdatePurchaseOrder(ctx, k.garageID, order.GetId(), &inventoryv1.PurchaseOrder{SupplierId: k.sup1.GetId(), Lines: kept})
	fixture.RequireAPIError(t, err, http.StatusConflict, "PURCHASE_ORDER_NOT_DRAFT")
}

// stripCosts drops the prices of lines a caller without the cost permission sends.
func stripCosts(lines []*inventoryv1.PurchaseOrderLine) []*inventoryv1.PurchaseOrderLine {
	for _, l := range lines {
		l.UnitCost, l.LineAmount = nil, nil
	}

	return lines
}

// Scenario: E2E-PO-003: the order goes to the supplier's address on file as a
// PDF, may be sent again, and a supplier without an address or a cancelled
// order is refused.
func TestPurchaseOrderIsEmailedToTheSupplier(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	k := newOrderKit(t, "po-send")
	inbox := fixture.Mailpit(t)
	order := k.draft(t, k.sup1, poLine(k.oil, "10", 90_000))

	sent, err := k.owner.API.SendPurchaseOrder(ctx, k.garageID, order.GetId(), "Giao trước thứ sáu nhé")
	require.NoError(t, err)
	assert.Equal(t, inventoryv1.PurchaseOrderStatus_PURCHASE_ORDER_STATUS_SENT, sent.GetStatus())
	assert.Equal(t, k.email1, sent.GetSentTo())
	assert.NotNil(t, sent.GetSentAt())

	msg, err := inbox.WaitForMatching(ctx, k.email1, func(m *client.Message) bool { return strings.Contains(m.Subject, order.GetNumber()) })
	require.NoError(t, err)
	assert.Contains(t, msg.Text, "Giao trước thứ sáu nhé")
	require.Len(t, msg.Attachments, 1)
	assert.Equal(t, "application/pdf", msg.Attachments[0].ContentType)
	assert.Contains(t, msg.Attachments[0].FileName, "purchase-order-")
	assert.Contains(t, msg.From.Name, "Blackbox")

	log, err := k.owner.API.ListNotificationLog(ctx, k.garageID, notificationv1.NotificationType_NOTIFICATION_TYPE_PURCHASE_ORDER,
		notificationv1.NotificationStatus_NOTIFICATION_STATUS_UNSPECIFIED)
	require.NoError(t, err)
	assert.NotEmpty(t, log)

	// Sent again: a second e-mail, still SENT.
	again, err := k.owner.API.SendPurchaseOrder(ctx, k.garageID, order.GetId(), "")
	require.NoError(t, err)
	assert.Equal(t, inventoryv1.PurchaseOrderStatus_PURCHASE_ORDER_STATUS_SENT, again.GetStatus())

	deadline, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	for {
		n, countErr := inbox.Count(deadline, k.email1, order.GetNumber())
		require.NoError(t, countErr)

		if n == 2 {
			break
		}

		select {
		case <-deadline.Done():
			require.Failf(t, "the resend did not arrive", "e-mails: %d", n)
		case <-time.After(250 * time.Millisecond):
		}
	}

	// A supplier without an address is refused and sends nothing; so is a cancelled order or a long message.
	mute := k.draft(t, k.noMail, poLine(k.filter, "1", 0))

	_, err = k.owner.API.SendPurchaseOrder(ctx, k.garageID, mute.GetId(), "")
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "SUPPLIER_EMAIL_REQUIRED")

	_, err = k.owner.API.SendPurchaseOrder(ctx, k.garageID, order.GetId(), strings.Repeat("m", 1001))
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "")

	_, err = k.owner.API.CancelPurchaseOrder(ctx, k.garageID, mute.GetId())
	require.NoError(t, err)

	_, err = k.owner.API.SendPurchaseOrder(ctx, k.garageID, mute.GetId(), "")
	fixture.RequireAPIError(t, err, http.StatusConflict, "PURCHASE_ORDER_NOT_SENDABLE")
}

// Scenario: E2E-PO-004: receiving creates a pre-filled draft goods receipt
// linked to the order; completing and reversing receipts moves the order
// between partially received and received.
func TestPurchaseOrderIsReceivedIntoGoodsReceipts(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	k := newOrderKit(t, "po-receive")
	order := k.draft(t, k.sup1, poLine(k.oil, "16", 100_000), poLine(k.filter, "8", 0))

	// A draft cannot be received.
	_, err := k.owner.API.ReceivePurchaseOrder(ctx, k.garageID, order.GetId())
	fixture.RequireAPIError(t, err, http.StatusConflict, "PURCHASE_ORDER_NOTHING_TO_RECEIVE")

	_, err = k.owner.API.SendPurchaseOrder(ctx, k.garageID, order.GetId(), "")
	require.NoError(t, err)

	receipt, err := k.owner.API.ReceivePurchaseOrder(ctx, k.garageID, order.GetId())
	require.NoError(t, err)
	assert.Equal(t, inventoryv1.GoodsReceiptStatus_GOODS_RECEIPT_STATUS_DRAFT, receipt.GetStatus())
	assert.Equal(t, order.GetId(), receipt.GetPurchaseOrderId())
	assert.Equal(t, order.GetNumber(), receipt.GetPurchaseOrderNumber())
	require.Len(t, receipt.GetLines(), 2)

	for _, l := range receipt.GetLines() {
		assert.Equal(t, k.sup1.GetId(), l.GetSupplierId())
	}

	// A second receipt is refused while the first is open.
	_, err = k.owner.API.ReceivePurchaseOrder(ctx, k.garageID, order.GetId())
	fixture.RequireAPIError(t, err, http.StatusConflict, "PURCHASE_ORDER_RECEIPT_OPEN")

	_, err = k.owner.API.CancelPurchaseOrder(ctx, k.garageID, order.GetId())
	fixture.RequireAPIError(t, err, http.StatusConflict, "PURCHASE_ORDER_NOT_CANCELLABLE")

	// Receive less oil than ordered.
	lines := receipt.GetLines()
	for _, l := range lines {
		if l.GetPartId() == k.oil.GetId() {
			l.EnteredQuantity = "10"
		}
	}

	_, err = k.owner.API.UpdateGoodsReceipt(ctx, k.garageID, receipt.GetId(), &inventoryv1.GoodsReceipt{Lines: lines}, "lines")
	require.NoError(t, err)

	_, err = k.owner.API.CompleteGoodsReceipt(ctx, k.garageID, receipt.GetId())
	require.NoError(t, err)

	partial, err := k.owner.API.GetPurchaseOrder(ctx, k.garageID, order.GetId())
	require.NoError(t, err)
	assert.Equal(t, inventoryv1.PurchaseOrderStatus_PURCHASE_ORDER_STATUS_PARTIALLY_RECEIVED, partial.GetStatus())

	received := map[string]string{}
	for _, l := range partial.GetLines() {
		received[l.GetPartId()] = l.GetReceivedQuantity()
	}

	assert.Equal(t, map[string]string{k.oil.GetId(): "10", k.filter.GetId(): "8"}, received)

	// The next receipt holds what is outstanding: 6 of oil, none of the filter.
	next, err := k.owner.API.ReceivePurchaseOrder(ctx, k.garageID, order.GetId())
	require.NoError(t, err)
	require.Len(t, next.GetLines(), 1)
	assert.Equal(t, k.oil.GetId(), next.GetLines()[0].GetPartId())
	assert.Equal(t, "6", next.GetLines()[0].GetEnteredQuantity())

	_, err = k.owner.API.CompleteGoodsReceipt(ctx, k.garageID, next.GetId())
	require.NoError(t, err)

	done, err := k.owner.API.GetPurchaseOrder(ctx, k.garageID, order.GetId())
	require.NoError(t, err)
	assert.Equal(t, inventoryv1.PurchaseOrderStatus_PURCHASE_ORDER_STATUS_RECEIVED, done.GetStatus())

	_, err = k.owner.API.ReceivePurchaseOrder(ctx, k.garageID, order.GetId())
	fixture.RequireAPIError(t, err, http.StatusConflict, "PURCHASE_ORDER_NOTHING_TO_RECEIVE")

	// Reversing the second receipt takes its quantities back.
	_, err = k.owner.API.CancelGoodsReceipt(ctx, k.garageID, next.GetId())
	require.NoError(t, err)

	back, err := k.owner.API.GetPurchaseOrder(ctx, k.garageID, order.GetId())
	require.NoError(t, err)
	assert.Equal(t, inventoryv1.PurchaseOrderStatus_PURCHASE_ORDER_STATUS_PARTIALLY_RECEIVED, back.GetStatus())
}

// Scenario: E2E-PO-005: cancelling, and the states that refuse it.
func TestPurchaseOrderCancelAndStateRules(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	k := newOrderKit(t, "po-cancel")

	draft := k.draft(t, k.sup1, poLine(k.oil, "1", 0))
	cancelled, err := k.owner.API.CancelPurchaseOrder(ctx, k.garageID, draft.GetId())
	require.NoError(t, err)
	assert.Equal(t, inventoryv1.PurchaseOrderStatus_PURCHASE_ORDER_STATUS_CANCELLED, cancelled.GetStatus())
	assert.NotNil(t, cancelled.GetCancelledAt())

	// A cancelled order refuses every further change.
	_, err = k.owner.API.CancelPurchaseOrder(ctx, k.garageID, draft.GetId())
	fixture.RequireAPIError(t, err, http.StatusConflict, "PURCHASE_ORDER_NOT_CANCELLABLE")

	_, err = k.owner.API.ReceivePurchaseOrder(ctx, k.garageID, draft.GetId())
	fixture.RequireAPIError(t, err, http.StatusConflict, "PURCHASE_ORDER_NOTHING_TO_RECEIVE")

	_, err = k.owner.API.UpdatePurchaseOrder(ctx, k.garageID, draft.GetId(), &inventoryv1.PurchaseOrder{SupplierId: k.sup1.GetId(), Lines: []*inventoryv1.PurchaseOrderLine{poLine(k.oil, "1", 0)}})
	fixture.RequireAPIError(t, err, http.StatusConflict, "PURCHASE_ORDER_NOT_DRAFT")

	// A fully received order cannot be cancelled.
	full := k.draft(t, k.sup1, poLine(k.oil, "2", 0))
	_, err = k.owner.API.SendPurchaseOrder(ctx, k.garageID, full.GetId(), "")
	require.NoError(t, err)

	receipt, err := k.owner.API.ReceivePurchaseOrder(ctx, k.garageID, full.GetId())
	require.NoError(t, err)

	_, err = k.owner.API.CompleteGoodsReceipt(ctx, k.garageID, receipt.GetId())
	require.NoError(t, err)

	_, err = k.owner.API.CancelPurchaseOrder(ctx, k.garageID, full.GetId())
	fixture.RequireAPIError(t, err, http.StatusConflict, "PURCHASE_ORDER_NOT_CANCELLABLE")

	// Cancelling a part-received order leaves what was received in stock.
	partial := k.draft(t, k.sup1, poLine(k.filter, "10", 0))
	_, err = k.owner.API.SendPurchaseOrder(ctx, k.garageID, partial.GetId(), "")
	require.NoError(t, err)

	before, err := k.owner.API.GetStockLevel(ctx, k.garageID, k.filter.GetId())
	require.NoError(t, err)

	receipt, err = k.owner.API.ReceivePurchaseOrder(ctx, k.garageID, partial.GetId())
	require.NoError(t, err)

	receipt.GetLines()[0].EnteredQuantity = "4"
	_, err = k.owner.API.UpdateGoodsReceipt(ctx, k.garageID, receipt.GetId(), &inventoryv1.GoodsReceipt{Lines: receipt.GetLines()}, "lines")
	require.NoError(t, err)

	_, err = k.owner.API.CompleteGoodsReceipt(ctx, k.garageID, receipt.GetId())
	require.NoError(t, err)

	closed, err := k.owner.API.CancelPurchaseOrder(ctx, k.garageID, partial.GetId())
	require.NoError(t, err)
	assert.Equal(t, inventoryv1.PurchaseOrderStatus_PURCHASE_ORDER_STATUS_CANCELLED, closed.GetStatus())

	after, err := k.owner.API.GetStockLevel(ctx, k.garageID, k.filter.GetId())
	require.NoError(t, err)
	assert.NotEqual(t, before.GetQuantityOnHand(), after.GetQuantityOnHand(), "what was received stays in stock")

	// The list filters by status and supplier.
	list, err := k.owner.API.ListPurchaseOrders(ctx, k.garageID, url.Values{"status": {"PURCHASE_ORDER_STATUS_CANCELLED"}, "supplierId": {k.sup1.GetId()}})
	require.NoError(t, err)
	assert.Len(t, list.GetData(), 2)
}

// Scenario: E2E-PO-006: the order as a PDF on A4 and A5, with prices only for
// a caller who may see costs; prints are logged.
func TestPurchaseOrderPDF(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	k := newOrderKit(t, "po-pdf")
	order := k.draft(t, k.sup1, poLine(k.oil, "16", 90_000))
	advisor := joinAs(t, k.owner, k.garageID, "Service Advisor", "po-pdf-advisor")

	contentType, a4, err := k.owner.API.DownloadPurchaseOrderPDF(ctx, k.garageID, order.GetId())
	require.NoError(t, err)
	assert.Contains(t, contentType, "pdf")
	assert.Contains(t, string(a4), "/MediaBox [0 0 595.28 841.89]")

	text := fixture.PDFText(t, a4)
	assert.Contains(t, text, "ĐƠN ĐẶT HÀNG")
	assert.Contains(t, text, order.GetNumber())
	assert.Contains(t, text, k.sup1.GetName())
	assert.Contains(t, text, "Dầu nhớt 5W-30")
	assert.Contains(t, text, "1,440,000", "the owner sees the total")

	_, plain, err := advisor.API.DownloadPurchaseOrderPDF(ctx, k.garageID, order.GetId())
	require.NoError(t, err)
	assert.NotContains(t, fixture.PDFText(t, plain), "1,440,000", "a caller without the cost permission gets no prices")

	_, first, err := k.owner.API.PrintPurchaseOrderPDF(ctx, k.garageID, order.GetId())
	require.NoError(t, err)
	assert.NotContains(t, fixture.PDFText(t, first), "BẢN SAO")

	_, err = k.owner.API.UpdatePrintSetting(ctx, k.garageID, documentv1.DocumentType_DOCUMENT_TYPE_PURCHASE_ORDER, documentv1.PaperSize_PAPER_SIZE_A5, true)
	require.NoError(t, err)

	_, second, err := k.owner.API.PrintPurchaseOrderPDF(ctx, k.garageID, order.GetId())
	require.NoError(t, err)
	assert.Contains(t, string(second), "/MediaBox [0 0 420.94 595.28]")
	assert.Contains(t, fixture.PDFText(t, second), "BẢN SAO")
}

// Scenario: E2E-PO-007: reading needs inventory.read, changing inventory.write,
// and another garage's order is out of reach.
func TestPurchaseOrderPermissionsAndIsolation(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	k := newOrderKit(t, "po-perms")
	order := k.draft(t, k.sup1, poLine(k.oil, "5", 0))

	staff := joinAs(t, k.owner, k.garageID, "Inventory Staff", "po-staff")
	advisor := joinAs(t, k.owner, k.garageID, "Service Advisor", "po-perm-advisor")
	tech := joinAs(t, k.owner, k.garageID, "Technician", "po-perm-tech")

	// Inventory staff do all of it.
	_, err := staff.API.GetPurchaseOrder(ctx, k.garageID, order.GetId())
	require.NoError(t, err)

	made, err := staff.API.CreatePurchaseOrder(ctx, k.garageID, &inventoryv1.PurchaseOrder{SupplierId: k.sup2.GetId(), Lines: []*inventoryv1.PurchaseOrderLine{poLine(k.wiper, "2", 0)}})
	require.NoError(t, err)

	_, err = staff.API.CancelPurchaseOrder(ctx, k.garageID, made.GetId())
	require.NoError(t, err)

	// The advisor reads and is refused every change.
	_, err = advisor.API.ListPurchaseOrders(ctx, k.garageID, nil)
	require.NoError(t, err)

	_, err = advisor.API.CreatePurchaseOrder(ctx, k.garageID, &inventoryv1.PurchaseOrder{SupplierId: k.sup1.GetId(), Lines: []*inventoryv1.PurchaseOrderLine{poLine(k.oil, "1", 0)}})
	fixture.RequireAPIError(t, err, http.StatusForbidden, "")

	_, err = advisor.API.UpdatePurchaseOrder(ctx, k.garageID, order.GetId(), &inventoryv1.PurchaseOrder{SupplierId: k.sup1.GetId()})
	fixture.RequireAPIError(t, err, http.StatusForbidden, "")

	_, err = advisor.API.SendPurchaseOrder(ctx, k.garageID, order.GetId(), "")
	fixture.RequireAPIError(t, err, http.StatusForbidden, "")

	_, err = advisor.API.CancelPurchaseOrder(ctx, k.garageID, order.GetId())
	fixture.RequireAPIError(t, err, http.StatusForbidden, "")

	_, err = advisor.API.ReceivePurchaseOrder(ctx, k.garageID, order.GetId())
	fixture.RequireAPIError(t, err, http.StatusForbidden, "")

	_, err = advisor.API.CreatePurchaseOrdersFromLowStock(ctx, k.garageID, nil, "")
	fixture.RequireAPIError(t, err, http.StatusForbidden, "")

	// The technician has inventory read in the default roles: reads, cannot change.
	_, err = tech.API.GetPurchaseOrder(ctx, k.garageID, order.GetId())
	require.NoError(t, err)

	_, err = tech.API.CancelPurchaseOrder(ctx, k.garageID, order.GetId())
	fixture.RequireAPIError(t, err, http.StatusForbidden, "")

	// Another garage's owner gets 404 through his own garage and 403 through this one.
	foreign := fixture.NewBusiness(t, "po-foreign-owner")

	_, err = foreign.Owner.API.GetPurchaseOrder(ctx, foreign.GarageID, order.GetId())
	fixture.RequireAPIError(t, err, http.StatusNotFound, "PURCHASE_ORDER_NOT_FOUND")

	_, err = foreign.Owner.API.GetPurchaseOrder(ctx, k.garageID, order.GetId())
	fixture.RequireAPIError(t, err, http.StatusForbidden, "")

	_, err = foreign.Owner.API.ListPurchaseOrders(ctx, k.garageID, nil)
	fixture.RequireAPIError(t, err, http.StatusForbidden, "")
}
