package domain

import "slices"

// Type is one e-mail a garage can switch off (FR-NOT-05, FR-INV-04).
type Type string

// The notification types.
const (
	TypeQuoteReady        Type = "quote_ready"
	TypeQuoteDecided      Type = "quote_decided"
	TypeVehicleReady      Type = "vehicle_ready"
	TypePaymentReceived   Type = "payment_received"
	TypeSettlementReceipt Type = "settlement_receipt"
	TypeSettlementPaid    Type = "settlement_paid"
	TypeLowStock          Type = "low_stock"
)

// AllTypes lists the types in the order the settings show them.
var AllTypes = []Type{
	TypeQuoteReady, TypeQuoteDecided, TypeVehicleReady, TypePaymentReceived,
	TypeSettlementReceipt, TypeSettlementPaid, TypeLowStock,
}

// Audience is who an e-mail is for.
type Audience string

// The audiences.
const (
	AudienceCustomer Audience = "customer"
	AudienceStaff    Audience = "staff"
)

// Valid reports whether t is a known type.
func (t Type) Valid() bool { return slices.Contains(AllTypes, t) }

// Audience is who receives the type.
func (t Type) Audience() Audience {
	if slices.Contains([]Type{TypeQuoteReady, TypeVehicleReady, TypePaymentReceived, TypeSettlementReceipt}, t) {
		return AudienceCustomer
	}

	return AudienceStaff
}

// Essential e-mails carry what the customer needs to act (the approval
// link, the receipt) and ignore a customer's opt-out. A garage can still
// switch them off.
func (t Type) Essential() bool {
	return t == TypeQuoteReady || t == TypeSettlementReceipt
}

// MaxSenderNameLength is the longest sender name a garage may set.
const MaxSenderNameLength = 100

// Settings are a garage's e-mail choices. A garage with no settings sends
// every type under the garage's name.
type Settings struct {
	// SenderName is the display name of the sender; empty means the garage's
	// name.
	SenderName string
	// Off holds the types the garage switched off.
	Off map[Type]bool
}

// Enabled reports whether the garage sends t.
func (s Settings) Enabled(t Type) bool { return !s.Off[t] }
