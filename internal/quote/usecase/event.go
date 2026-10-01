package usecase

// keyGarageID is the garage key of event payloads.
const keyGarageID = "garage_id"

// Events published by the Quote & Pricing module (Event Design Section 4).
const (
	// EventQuoteApproved is published when at least one line was
	// approved (FR-QUO-03); its main consumer is the Repair Order module,
	// indirectly, via the ConvertQuote API rather than the event itself.
	EventQuoteApproved = "quote.approved"
	// EventQuoteRejected is published when every line was rejected; not
	// yet consumed (a future Notification Service subscriber, Release
	// 1.5, would alert the service advisor).
	EventQuoteRejected = "quote.rejected"
)
