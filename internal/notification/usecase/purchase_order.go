package usecase

import (
	"context"
	"errors"
	"fmt"

	"github.com/viettechno/vietgara-backend/internal/notification/domain"
	"github.com/viettechno/vietgara-backend/internal/platform/mail"
)

// ErrAttachmentTooBig is returned for an order whose PDF is too big to mail.
var ErrAttachmentTooBig = errors.New("the attachment is too big to mail")

// PurchaseOrderRequest is a purchase order the Inventory module asks
// Notification to e-mail to its supplier (FR-INV-16).
type PurchaseOrderRequest struct {
	GarageID     string
	OrderID      string
	Number       string
	SupplierName string
	// To is the supplier's address on file; nothing else is ever mailed.
	To      string
	Message string
}

// PurchaseOrder e-mails the order as a PDF to the supplier, under the garage's
// name, and logs it. It is a call and not an event because the caller needs to
// know the order went out. It reports false when the address is on the
// suppression list. A garage cannot switch it off.
func (uc *NotificationUseCase) PurchaseOrder(ctx context.Context, req PurchaseOrderRequest) (bool, error) {
	pdf, err := uc.Receipts.PurchaseOrder(ctx, req.GarageID, req.OrderID)
	if err != nil {
		return false, fmt.Errorf("render the purchase order: %w", err)
	}

	if len(pdf) > mail.MaxAttachmentBytes {
		return false, fmt.Errorf("the purchase order PDF is %d bytes: %w", len(pdf), ErrAttachmentTooBig)
	}

	settings, garage, err := uc.settingsOf(ctx, req.GarageID)
	if err != nil {
		return false, err
	}

	n := note{
		typ: domain.TypePurchaseOrder, garageID: req.GarageID, entityType: "purchase_order", entityID: req.OrderID,
		template: mail.TemplatePurchaseOrder,
		data: map[string]any{
			keyNumber: req.Number, "SupplierName": req.SupplierName, "Message": req.Message, "GarageName": garage.Name,
			"GaragePhone": garage.Phone,
		},
		attachments: func() ([]mail.Attachment, bool) {
			return []mail.Attachment{{Name: "purchase-order-" + safeFileName(req.Number) + ".pdf", ContentType: "application/pdf", Data: pdf}}, true
		},
	}

	return uc.deliver(ctx, settings, garage, n, recipient{name: req.SupplierName, email: req.To}, nil)
}
