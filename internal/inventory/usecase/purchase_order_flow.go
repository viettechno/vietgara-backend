package usecase

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
)

// OrderMail is a purchase order to be e-mailed to its supplier.
type OrderMail struct {
	GarageID     string
	OrderID      string
	Number       string
	SupplierName string
	// To is the supplier's address on file: the only recipient there is.
	To      string
	Message string
}

// OrderMailer e-mails a purchase order as a PDF; implemented over the
// Notification and Document modules in cmd/server. It reports false when the
// address could not be mailed (suppressed).
type OrderMailer interface {
	SendPurchaseOrder(ctx context.Context, mail OrderMail) (bool, error)
}

// SetOrderMailer gives the use case the means to e-mail an order. It is set
// once Notification is built, since Inventory is built first.
func (uc *InventoryUseCase) SetOrderMailer(m OrderMailer) { uc.mailer = m }

// CreatePurchaseOrdersFromLowStockRequest is the input of
// CreatePurchaseOrdersFromLowStock.
type CreatePurchaseOrdersFromLowStockRequest struct {
	GarageID     string
	ActorID      string
	PartIDs      []string
	ExpectedDate string
}

// CreatePurchaseOrdersFromLowStockResult is the drafts made and the low parts
// that could not be ordered for want of a default supplier.
type CreatePurchaseOrdersFromLowStockResult struct {
	Orders     []*domain.PurchaseOrder
	Unassigned []*domain.StockLevel
}

// CreatePurchaseOrdersFromLowStock makes one draft per default supplier for
// the parts at or below their reorder threshold (or the ones named): each for
// the quantity that brings the available stock plus what is already on order
// back to twice the threshold. A part already covered is left out, so asking
// twice orders nothing twice.
func (uc *InventoryUseCase) CreatePurchaseOrdersFromLowStock(
	ctx context.Context, req CreatePurchaseOrdersFromLowStockRequest,
) (*CreatePurchaseOrdersFromLowStockResult, error) {
	const op = "create purchase orders from low stock"

	low, err := uc.lowStock(ctx, req.GarageID, PartQuery{})
	if err != nil {
		return nil, err
	}

	if len(req.PartIDs) > 0 {
		low = slices.DeleteFunc(low, func(l *domain.StockLevel) bool { return !slices.Contains(req.PartIDs, l.PartID) })
	}

	ids := make([]string, len(low))
	for i, l := range low {
		ids[i] = l.PartID
	}

	onOrder, err := uc.repo.OnOrder(ctx, req.GarageID, ids)
	if err != nil {
		return nil, toAppError(op, err)
	}

	defaults, err := uc.repo.DefaultSuppliers(ctx, req.GarageID, ids)
	if err != nil {
		return nil, toAppError(op, err)
	}

	bySupplier, order := map[string][]PurchaseOrderLineInput{}, []string{}
	result := &CreatePurchaseOrdersFromLowStockResult{}

	for _, l := range low {
		quantity := domain.SuggestedQuantity(l.ReorderThreshold, l.QuantityAvailable, onOrder[l.PartID])
		if quantity == "" {
			continue
		}

		supplier, ok := defaults[l.PartID]
		if !ok {
			result.Unassigned = append(result.Unassigned, l)

			continue
		}

		if _, seen := bySupplier[supplier]; !seen {
			order = append(order, supplier)
		}

		bySupplier[supplier] = append(bySupplier[supplier], PurchaseOrderLineInput{PartID: l.PartID, EnteredQuantity: quantity})
	}

	for _, supplier := range order {
		o, err := uc.CreatePurchaseOrder(ctx, CreatePurchaseOrderRequest{
			GarageID: req.GarageID, ActorID: req.ActorID, SupplierID: supplier, ExpectedDate: req.ExpectedDate,
			Lines: bySupplier[supplier],
		})
		if err != nil {
			return nil, err
		}

		result.Orders = append(result.Orders, o)
	}

	return result, nil
}

// SendPurchaseOrderRequest is the input of SendPurchaseOrder.
type SendPurchaseOrderRequest struct {
	GarageID string
	OrderID  string
	ActorID  string
	Message  string
}

// SendPurchaseOrder e-mails the order, as a PDF, to the supplier's address on
// file and marks a draft sent. A sent order may be sent again. The mail goes
// out inside the transaction of the request, so a failure leaves the order as
// it was.
func (uc *InventoryUseCase) SendPurchaseOrder(ctx context.Context, req SendPurchaseOrderRequest) (*domain.PurchaseOrder, error) {
	const op = "send purchase order"

	o, err := uc.repo.FindPurchaseOrder(ctx, req.GarageID, req.OrderID, true)
	if err != nil {
		return nil, toAppError(op, err)
	}

	if o.Status != domain.PurchaseOrderDraft && o.Status != domain.PurchaseOrderSent {
		return nil, toAppError(op, domain.ErrPurchaseOrderNotSendable)
	}

	if len([]rune(req.Message)) > domain.MaxOrderMessageLength {
		return nil, toAppError(op, domain.ErrMessageTooLong)
	}

	if err := uc.describeOrders(ctx, req.GarageID, o); err != nil {
		return nil, toAppError(op, err)
	}

	to := strings.TrimSpace(o.SupplierEmail)
	if to == "" || uc.mailer == nil {
		return nil, toAppError(op, domain.ErrSupplierEmailRequired)
	}

	sent, err := uc.mailer.SendPurchaseOrder(ctx, OrderMail{
		GarageID: req.GarageID, OrderID: o.ID, Number: o.Number, SupplierName: o.SupplierName, To: to, Message: req.Message,
	})
	if err != nil {
		return nil, toAppError(op, err)
	}

	if !sent {
		return nil, toAppError(op, domain.ErrSupplierEmailRequired)
	}

	before, now := o.Status, uc.now()
	o.Status, o.SentAt, o.SentTo, o.UpdatedBy = domain.PurchaseOrderSent, &now, to, actorRef(req.ActorID)

	if err := uc.repo.SavePurchaseOrder(ctx, o); err != nil {
		return nil, toAppError(op, err)
	}

	if err := uc.auditOrder(ctx, req.ActorID, ActionPurchaseOrderSent, before, o); err != nil {
		return nil, err
	}

	o.TotalAmount = o.Total()

	return o, nil
}

// CancelPurchaseOrder cancels a draft, sent or partially received order that
// has no draft goods receipt open; what was received stays received.
func (uc *InventoryUseCase) CancelPurchaseOrder(ctx context.Context, garageID, orderID, actorID string) (*domain.PurchaseOrder, error) {
	const op = "cancel purchase order"

	o, err := uc.repo.FindPurchaseOrder(ctx, garageID, orderID, true)
	if err != nil {
		return nil, toAppError(op, err)
	}

	if o.Status == domain.PurchaseOrderReceived || o.Status == domain.PurchaseOrderCancelled {
		return nil, toAppError(op, domain.ErrPurchaseOrderNotCancellable)
	}

	open, err := uc.repo.HasDraftReceipt(ctx, garageID, orderID)
	if err != nil {
		return nil, toAppError(op, err)
	}

	if open {
		return nil, toAppError(op, domain.ErrPurchaseOrderNotCancellable)
	}

	before, now := o.Status, uc.now()
	o.Status, o.CancelledAt, o.UpdatedBy = domain.PurchaseOrderCancelled, &now, actorRef(actorID)

	if err := uc.repo.SavePurchaseOrder(ctx, o); err != nil {
		return nil, toAppError(op, err)
	}

	if err := uc.auditOrder(ctx, actorID, ActionPurchaseOrderCancelled, before, o); err != nil {
		return nil, err
	}

	if err := uc.describeOrders(ctx, garageID, o); err != nil {
		return nil, toAppError(op, err)
	}

	o.TotalAmount = o.Total()

	return o, nil
}

// ReceivePurchaseOrder creates a draft goods receipt for what the order still
// waits for: the supplier on every line, the outstanding quantity of each line
// in its unit and the expected cost, linked to the order. The order must be
// sent or partially received and have no draft receipt open.
func (uc *InventoryUseCase) ReceivePurchaseOrder(ctx context.Context, garageID, orderID, actorID string) (*domain.GoodsReceipt, error) {
	const op = "receive purchase order"

	o, err := uc.repo.FindPurchaseOrder(ctx, garageID, orderID, true)
	if err != nil {
		return nil, toAppError(op, err)
	}

	if !o.CanReceive() {
		return nil, toAppError(op, domain.ErrPurchaseOrderNothingToReceive)
	}

	open, err := uc.repo.HasDraftReceipt(ctx, garageID, orderID)
	if err != nil {
		return nil, toAppError(op, err)
	}

	if open {
		return nil, toAppError(op, domain.ErrPurchaseOrderReceiptOpen)
	}

	inputs := make([]ReceiptLineInput, 0, len(o.Lines))

	for i := range o.Lines {
		l := &o.Lines[i]
		if domain.IsZero(l.Outstanding()) {
			continue
		}

		inputs = append(inputs, ReceiptLineInput{
			PartID: l.PartID, UnitID: derefOr(l.UnitID), EnteredQuantity: l.OutstandingEntered(), SupplierID: o.SupplierID, UnitCost: l.UnitCost,
		})
	}

	if len(inputs) == 0 {
		return nil, toAppError(op, domain.ErrPurchaseOrderNothingToReceive)
	}

	garage, err := uc.garages.Get(ctx, garageID)
	if err != nil {
		return nil, toAppError(op, err)
	}

	number, err := uc.nextNumber(ctx, garageID, docTypeGoodsReceipt, garage.GoodsReceiptNumberTemplate, garage.Timezone)
	if err != nil {
		return nil, toAppError(op, err)
	}

	r := &domain.GoodsReceipt{
		ID: uuid.NewString(), GarageID: garageID, Number: number, Status: domain.GoodsReceiptDraft, DefaultSupplierID: &o.SupplierID,
		ReceiptDate: uc.localNow(garage.Timezone).Format(time.DateOnly), PurchaseOrderID: &o.ID, CreatedBy: actorRef(actorID),
		UpdatedBy: actorRef(actorID),
	}

	if r.Lines, err = uc.buildReceiptLines(ctx, garageID, r.DefaultSupplierID, inputs, nil, true); err != nil {
		return nil, toAppError(op, err)
	}

	if err := uc.repo.CreateGoodsReceipt(ctx, r); err != nil {
		return nil, toAppError(op, err)
	}

	if err := uc.describeReceipts(ctx, garageID, r); err != nil {
		return nil, toAppError(op, err)
	}

	r.PurchaseOrderNumber, r.TotalAmount = o.Number, r.Total()

	return r, nil
}

// countAgainstOrder adds the quantities of a goods receipt that was just
// completed (sign 1), or takes them back when it is reversed (sign -1), on the
// lines of the order it came from and moves the order along. A receipt line of
// a part that is not on the order counts for nothing.
func (uc *InventoryUseCase) countAgainstOrder(ctx context.Context, r *domain.GoodsReceipt, sign int) error {
	if r.PurchaseOrderID == nil {
		return nil
	}

	o, err := uc.repo.FindPurchaseOrder(ctx, r.GarageID, *r.PurchaseOrderID, true)
	if err != nil {
		return err
	}

	for _, rl := range r.Lines {
		for i := range o.Lines {
			l := &o.Lines[i]
			if l.PartID != rl.PartID {
				continue
			}

			if sign > 0 {
				l.ReceivedQuantity = domain.Add(l.ReceivedQuantity, rl.Quantity)
			} else if domain.Less(l.ReceivedQuantity, rl.Quantity) {
				l.ReceivedQuantity = "0"
			} else {
				l.ReceivedQuantity = domain.Subtract(l.ReceivedQuantity, rl.Quantity)
			}
		}
	}

	o.Recount()
	o.UpdatedBy = r.UpdatedBy

	return uc.repo.SavePurchaseOrder(ctx, o)
}
