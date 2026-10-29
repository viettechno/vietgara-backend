package usecase

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/viettechno/vietgara-backend/internal/notification/domain"
	"github.com/viettechno/vietgara-backend/internal/platform/mail"
	"github.com/viettechno/vietgara-backend/internal/platform/outbox"
	"github.com/viettechno/vietgara-backend/pkg/converter"
)

// Template variables shared by several e-mails.
const (
	keyNumber       = "Number"
	keyVehiclePlate = "VehiclePlate"
)

// permissionInventoryWrite is held by the staff who reorder parts.
const permissionInventoryWrite = "inventory.write"

type quoteDecidedPayload struct {
	QuoteID      string `json:"quote_id"`
	Number       string `json:"number"`
	CustomerName string `json:"customer_name"`
	VehiclePlate string `json:"vehicle_plate"`
	CreatedBy    string `json:"created_by"`
}

// HandleQuoteDecided tells the staff who made a quote, and the owner, that the
// customer approved or rejected it (FR-NOT-05).
func (uc *NotificationUseCase) HandleQuoteDecided(ctx context.Context, event outbox.Event) error {
	var p quoteDecidedPayload
	if err := json.Unmarshal(event.Data, &p); err != nil || p.QuoteID == "" || event.GarageID == "" {
		return nil //nolint:nilerr // a malformed event cannot succeed on retry: drop it
	}

	return uc.guard(event, func() error {
		settings, garage, err := uc.settingsOf(ctx, event.GarageID)
		if err != nil {
			return err
		}

		staff, err := uc.Staff.Recipients(ctx, event.GarageID, creators(p.CreatedBy), "")
		if err != nil {
			return err
		}

		return uc.toStaff(ctx, settings, garage, staff, note{
			typ: domain.TypeQuoteDecided, garageID: event.GarageID, entityType: "quote", entityID: p.QuoteID, eventID: event.ID,
			template: mail.TemplateQuoteDecided,
			data: map[string]any{
				keyNumber: p.Number, "CustomerName": p.CustomerName, keyVehiclePlate: p.VehiclePlate,
				"Approved": event.Type == "quote.approved",
			},
		})
	})
}

type repairOrderPayload struct {
	RepairOrderID string `json:"repair_order_id"`
	VehicleID     string `json:"vehicle_id"`
	Number        string `json:"number"`
	VehiclePlate  string `json:"vehicle_plate"`
}

// HandleVehicleReady tells the customer that the repair is done and the
// vehicle can be picked up (FR-NOT-05).
func (uc *NotificationUseCase) HandleVehicleReady(ctx context.Context, event outbox.Event) error {
	var p repairOrderPayload
	if err := json.Unmarshal(event.Data, &p); err != nil || p.RepairOrderID == "" || event.GarageID == "" {
		return nil //nolint:nilerr // a malformed event cannot succeed on retry: drop it
	}

	return uc.guard(event, func() error {
		settings, garage, err := uc.settingsOf(ctx, event.GarageID)
		if err != nil {
			return err
		}

		customer, err := uc.Customers.ByVehicle(ctx, event.GarageID, p.VehicleID)
		if err != nil {
			return err
		}

		_, err = uc.toCustomer(ctx, settings, garage, customer, note{
			typ: domain.TypeVehicleReady, garageID: event.GarageID, entityType: "repair_order", entityID: p.RepairOrderID,
			eventID: event.ID, template: mail.TemplateVehicleReady, data: map[string]any{keyVehiclePlate: p.VehiclePlate},
		})

		return err
	})
}

type settlementPayload struct {
	SettlementID string `json:"settlement_id"`
	PaymentID    string `json:"payment_id"`
	VehicleID    string `json:"vehicle_id"`
	Number       string `json:"number"`
	CustomerName string `json:"customer_name"`
	VehiclePlate string `json:"vehicle_plate"`
	Amount       int64  `json:"amount"`
	Balance      int64  `json:"balance"`
	Total        int64  `json:"total"`
	CreatedBy    string `json:"created_by"`
}

// HandlePaymentReceived tells the customer that a payment was recorded and
// what is left to pay (FR-NOT-05). The payment that settles the bill is
// answered by the receipt instead (HandleSettlementPaid).
func (uc *NotificationUseCase) HandlePaymentReceived(ctx context.Context, event outbox.Event) error {
	var p settlementPayload
	if err := json.Unmarshal(event.Data, &p); err != nil || p.SettlementID == "" || event.GarageID == "" || p.Balance <= 0 {
		return nil //nolint:nilerr // a malformed event cannot succeed on retry: drop it
	}

	return uc.guard(event, func() error {
		settings, garage, err := uc.settingsOf(ctx, event.GarageID)
		if err != nil {
			return err
		}

		customer, err := uc.Customers.ByVehicle(ctx, event.GarageID, p.VehicleID)
		if err != nil {
			return err
		}

		_, err = uc.toCustomer(ctx, settings, garage, customer, note{
			typ: domain.TypePaymentReceived, garageID: event.GarageID, entityType: "settlement", entityID: p.SettlementID,
			eventID: event.ID, template: mail.TemplatePaymentReceived,
			data: map[string]any{
				keyVehiclePlate: p.VehiclePlate, keyNumber: p.Number,
				"Amount": converter.FormatVND(p.Amount), "Balance": converter.FormatVND(p.Balance),
			},
		})

		return err
	})
}

// HandleSettlementPaid sends the customer the receipt as a PDF and tells the
// staff who made the settlement, and the owner, that it is paid (FR-NOT-05).
func (uc *NotificationUseCase) HandleSettlementPaid(ctx context.Context, event outbox.Event) error {
	var p settlementPayload
	if err := json.Unmarshal(event.Data, &p); err != nil || p.SettlementID == "" || event.GarageID == "" {
		return nil //nolint:nilerr // a malformed event cannot succeed on retry: drop it
	}

	return uc.guard(event, func() error {
		settings, garage, err := uc.settingsOf(ctx, event.GarageID)
		if err != nil {
			return err
		}

		customer, err := uc.Customers.ByVehicle(ctx, event.GarageID, p.VehicleID)
		if err != nil {
			return err
		}

		attachment, attached := uc.receipt(ctx, event.GarageID, p.SettlementID, p.Number)

		if _, err := uc.toCustomer(ctx, settings, garage, customer, note{
			typ: domain.TypeSettlementReceipt, garageID: event.GarageID, entityType: "settlement", entityID: p.SettlementID,
			eventID: event.ID, template: mail.TemplateSettlementReceipt, attach: attachment,
			data: map[string]any{
				keyVehiclePlate: p.VehiclePlate, keyNumber: p.Number, "Total": converter.FormatVND(p.Total), "Attached": attached,
			},
		}); err != nil {
			return err
		}

		staff, err := uc.Staff.Recipients(ctx, event.GarageID, creators(p.CreatedBy), "")
		if err != nil {
			return err
		}

		return uc.toStaff(ctx, settings, garage, staff, note{
			typ: domain.TypeSettlementPaid, garageID: event.GarageID, customerID: customer.ID, entityType: "settlement",
			entityID: p.SettlementID, eventID: event.ID, template: mail.TemplateSettlementPaid,
			data: map[string]any{
				keyNumber: p.Number, "CustomerName": p.CustomerName, keyVehiclePlate: p.VehiclePlate, "Total": converter.FormatVND(p.Total),
			},
		})
	})
}

// receipt renders the receipt PDF to attach. The e-mail goes without it when
// it cannot be made or is too big: the customer still learns the bill is paid.
func (uc *NotificationUseCase) receipt(ctx context.Context, garageID, settlementID, number string) ([]mail.Attachment, bool) {
	pdf, err := uc.Receipts.SettlementReceipt(ctx, garageID, settlementID)
	if err != nil {
		slog.Warn("notification: rendering the receipt failed, sending without it", "settlement_id", settlementID, "error", err)

		return nil, false
	}

	if len(pdf) > mail.MaxAttachmentBytes {
		slog.Warn("notification: the receipt is too big to attach", "settlement_id", settlementID, "bytes", len(pdf))

		return nil, false
	}

	return []mail.Attachment{{Name: "receipt-" + number + ".pdf", ContentType: "application/pdf", Data: pdf}}, true
}

type stockLowPayload struct {
	PartID    string `json:"part_id"`
	PartName  string `json:"part_name"`
	SKU       string `json:"sku"`
	Quantity  string `json:"quantity"`
	Threshold string `json:"threshold"`
}

// HandleStockLow tells the owner and the staff who reorder parts that a part
// fell to or under its threshold (FR-INV-04).
func (uc *NotificationUseCase) HandleStockLow(ctx context.Context, event outbox.Event) error {
	var p stockLowPayload
	if err := json.Unmarshal(event.Data, &p); err != nil || p.PartID == "" || event.GarageID == "" {
		return nil //nolint:nilerr // a malformed event cannot succeed on retry: drop it
	}

	return uc.guard(event, func() error {
		settings, garage, err := uc.settingsOf(ctx, event.GarageID)
		if err != nil {
			return err
		}

		staff, err := uc.Staff.Recipients(ctx, event.GarageID, nil, permissionInventoryWrite)
		if err != nil {
			return err
		}

		return uc.toStaff(ctx, settings, garage, staff, note{
			typ: domain.TypeLowStock, garageID: event.GarageID, entityType: "part", entityID: p.PartID, eventID: event.ID,
			template: mail.TemplateLowStock,
			data:     map[string]any{"PartName": p.PartName, "SKU": p.SKU, "Quantity": p.Quantity, "Threshold": p.Threshold},
		})
	})
}

// guard drops an event whose customer or garage is gone: no retry can send it.
func (uc *NotificationUseCase) guard(event outbox.Event, run func() error) error {
	err := run()
	if isGone(err) {
		slog.Warn("notification: the customer or garage of an event is gone, dropping it", "event_type", event.Type, "event_id", event.ID)

		return nil
	}

	return err
}

// creators lists the user who made a document, when it is known.
func creators(userID string) []string {
	if userID == "" {
		return nil
	}

	return []string{userID}
}
