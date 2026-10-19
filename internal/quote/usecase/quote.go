package usecase

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/viettechno/vietgara-backend/internal/quote/domain"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
	"github.com/viettechno/vietgara-backend/pkg/numbering"
	"github.com/viettechno/vietgara-backend/pkg/partquantity"
)

// Deps are the dependencies of QuoteUseCase.
type Deps struct {
	Quotes         Repository
	Vehicles       VehicleReader
	Customers      CustomerReader
	BillingParties BillingPartyResolver
	Garages        GarageReader
	Limits         TransactionLimiter
	Sequences      Sequencer
	RepairOrders   RepairOrderCreator
	Mailer         Mailer
	Events         EventPublisher
	Tx             Transactor
	// Parts converts part-line quantities typed in a packaging unit.
	Parts partquantity.Resolver
	// Jobs expands a catalogue job into quote lines (FR-CAT-06).
	Jobs JobCatalogue
	// AppBaseURL is the web admin origin the public approval link is
	// built on, e.g. "https://vietgara.viettechno.com".
	AppBaseURL string
	// Now defaults to time.Now.
	Now clock
}

// QuoteUseCase implements every action of the Quote & Pricing module.
type QuoteUseCase struct {
	Deps
}

// NewQuoteUseCase wires the use case.
func NewQuoteUseCase(deps Deps) *QuoteUseCase {
	if deps.Now == nil {
		deps.Now = newClock()
	}

	return &QuoteUseCase{Deps: deps}
}

// LineInput is one line of a create/update request.
type LineInput struct {
	ItemType    domain.ItemType
	ItemRefID   *string
	Description string
	Quantity    string
	// UnitID and EnteredQuantity are the packaging unit and the amount
	// typed in it (FR-CAT-02); with a unit, Quantity is derived from them.
	UnitID          string
	EnteredQuantity string
	UnitPrice       int64
	// JobID is the catalogue job the line came from (FR-CAT-06).
	JobID *string
	// NonBillable makes the line a hidden consumable (FR-CAT-08): only a
	// catalogue part line can be one, and its price is stored as zero.
	NonBillable bool
}

// CreateQuoteRequest is the input for QuoteUseCase.Create.
type CreateQuoteRequest struct {
	GarageID   string
	ActorID    string
	CustomerID string
	VehicleID  string
	// ApprovalMode defaults to the garage's default_quote_approval_mode
	// (FR-TEN-05) when nil.
	ApprovalMode *domain.ApprovalMode
	Notes        string
	Lines        []LineInput
	// BillingTaxProfileID is the customer's tax profile the quote is billed
	// to (FR-CUS-07): nil takes the customer's default profile, "" is a
	// retail receipt.
	BillingTaxProfileID *string
}

// Create creates a draft quote (FR-QUO-01), numbered from the garage's
// quote_number_template and counted against the plan's daily transaction
// limit (FR-LIC-02).
func (uc *QuoteUseCase) Create(ctx context.Context, req CreateQuoteRequest) (*domain.Quote, error) {
	garage, err := uc.garageInfo(ctx, req.GarageID)
	if err != nil {
		return nil, err
	}

	lines, err := uc.buildLines(ctx, req.GarageID, req.Lines)
	if err != nil {
		return nil, toAppError("create quote", err)
	}

	customerName, plate, err := uc.Vehicles.Find(ctx, req.GarageID, req.CustomerID, req.VehicleID)
	if err != nil {
		return nil, toAppError("create quote", err)
	}

	mode := garage.DefaultQuoteApprovalMode

	if req.ApprovalMode != nil {
		if !req.ApprovalMode.IsValid() {
			return nil, toAppError("create quote", domain.ErrInvalidApprovalMode)
		}

		mode = *req.ApprovalMode
	}

	if err := CheckLength(req.Notes); err != nil {
		return nil, toAppError("create quote", err)
	}

	party, err := uc.BillingParties.Resolve(ctx, req.GarageID, req.CustomerID, req.BillingTaxProfileID)
	if err != nil {
		return nil, toAppError("create quote", err)
	}

	if err := uc.Limits.ConsumeTransaction(ctx, garage.OwnerUserID); err != nil {
		return nil, err
	}

	number, err := uc.nextNumber(ctx, req.GarageID, garage)
	if err != nil {
		return nil, toAppError("create quote", err)
	}

	q := &domain.Quote{
		ID: uuid.NewString(), GarageID: req.GarageID, CustomerID: req.CustomerID, CustomerName: customerName,
		VehicleID: req.VehicleID, VehicleLicensePlate: plate, Number: number, Status: domain.StatusDraft,
		ApprovalMode: mode, Lines: lines, Notes: strings.TrimSpace(req.Notes), Issuer: garage.Issuer, BillingParty: party,
		CreatedBy: actorRef(req.ActorID), UpdatedBy: actorRef(req.ActorID),
	}
	q.Recalculate()

	if err := uc.Quotes.Create(ctx, q); err != nil {
		return nil, toAppError("create quote", err)
	}

	return q, nil
}

// Get returns one quote of the garage, with its lines.
func (uc *QuoteUseCase) Get(ctx context.Context, garageID, quoteID string) (*domain.Quote, error) {
	q, err := uc.Quotes.FindByID(ctx, garageID, quoteID)
	if err != nil {
		return nil, toAppError("get quote", err)
	}

	return q, nil
}

// UpdateQuoteRequest is the input for QuoteUseCase.Update; nil fields keep
// their current value. Lines, when non-nil, replaces the whole set.
type UpdateQuoteRequest struct {
	GarageID     string
	QuoteID      string
	ActorID      string
	CustomerID   *string
	VehicleID    *string
	ApprovalMode *domain.ApprovalMode
	Notes        *string
	Lines        *[]LineInput
	// BillingTaxProfileID, when non-nil, changes who the quote is billed to
	// ("" is a retail receipt). Changing the customer without it takes the
	// new customer's default profile.
	BillingTaxProfileID *string
}

// Update applies the partial updates in req to a quote (FR-QUO-01); only
// while the quote is still a draft (ErrNotEditable otherwise).
func (uc *QuoteUseCase) Update(ctx context.Context, req UpdateQuoteRequest) (*domain.Quote, error) {
	q, err := uc.Quotes.FindByID(ctx, req.GarageID, req.QuoteID)
	if err != nil {
		return nil, toAppError("update quote", err)
	}

	if !q.Status.Editable() {
		return nil, toAppError("update quote", domain.ErrNotEditable)
	}

	if err := uc.applyCustomerVehicle(ctx, q, req); err != nil {
		return nil, err
	}

	if err := uc.applyBillingParty(ctx, q, req); err != nil {
		return nil, err
	}

	if req.ApprovalMode != nil {
		if !req.ApprovalMode.IsValid() {
			return nil, toAppError("update quote", domain.ErrInvalidApprovalMode)
		}

		q.ApprovalMode = *req.ApprovalMode
	}

	if req.Notes != nil {
		if err := CheckLength(*req.Notes); err != nil {
			return nil, toAppError("update quote", err)
		}

		q.Notes = strings.TrimSpace(*req.Notes)
	}

	if req.Lines != nil {
		lines, err := uc.buildLines(ctx, req.GarageID, *req.Lines)
		if err != nil {
			return nil, toAppError("update quote", err)
		}

		q.Lines = lines
	}

	q.Recalculate()
	q.UpdatedBy = actorRef(req.ActorID)

	if err := uc.Quotes.ReplaceLines(ctx, q); err != nil {
		return nil, toAppError("update quote", err)
	}

	return q, nil
}

func (uc *QuoteUseCase) applyCustomerVehicle(ctx context.Context, q *domain.Quote, req UpdateQuoteRequest) error {
	if req.CustomerID == nil && req.VehicleID == nil {
		return nil
	}

	customerID, vehicleID := q.CustomerID, q.VehicleID
	if req.CustomerID != nil {
		customerID = *req.CustomerID
	}

	if req.VehicleID != nil {
		vehicleID = *req.VehicleID
	}

	name, plate, err := uc.Vehicles.Find(ctx, q.GarageID, customerID, vehicleID)
	if err != nil {
		return toAppError("update quote", err)
	}

	q.CustomerID, q.CustomerName, q.VehicleID, q.VehicleLicensePlate = customerID, name, vehicleID, plate

	return nil
}

// applyBillingParty re-resolves who the quote is billed to when the request
// names a profile, or when the customer changed (the old profile is not the
// new customer's).
func (uc *QuoteUseCase) applyBillingParty(ctx context.Context, q *domain.Quote, req UpdateQuoteRequest) error {
	if req.BillingTaxProfileID == nil && req.CustomerID == nil {
		return nil
	}

	party, err := uc.BillingParties.Resolve(ctx, q.GarageID, q.CustomerID, req.BillingTaxProfileID)
	if err != nil {
		return toAppError("update quote", err)
	}

	q.BillingParty = party

	return nil
}

// ListQuotesRequest is the input for QuoteUseCase.List.
type ListQuotesRequest struct {
	PageRequest

	GarageID   string
	CustomerID string
	VehicleID  string
	Status     domain.Status
}

// List returns one page of the garage's quotes matching req.
func (uc *QuoteUseCase) List(ctx context.Context, req ListQuotesRequest) (*Page[*domain.Quote], error) {
	page := req.normalize()
	filter := ListFilter{
		CustomerID: req.CustomerID, VehicleID: req.VehicleID, Status: req.Status,
		Limit: page.PageSize, Offset: (page.Page - 1) * page.PageSize,
	}

	items, total, err := uc.Quotes.List(ctx, req.GarageID, filter)
	if err != nil {
		return nil, toAppError("list quotes", err)
	}

	return &Page[*domain.Quote]{Items: items, Total: total, Page: page.Page, PageSize: page.PageSize}, nil
}

// Delete soft-deletes a draft quote; a quote that was already sent must be
// left to run its course rather than disappear on the customer.
func (uc *QuoteUseCase) Delete(ctx context.Context, garageID, quoteID, actorID string) error {
	if err := uc.Quotes.SoftDelete(ctx, garageID, quoteID, actorID); err != nil {
		return toAppError("delete quote", err)
	}

	return nil
}

// garageInfo reads the garage's numbering and approval-mode configuration.
func (uc *QuoteUseCase) garageInfo(ctx context.Context, garageID string) (GarageInfo, error) {
	garage, err := uc.Garages.Get(ctx, garageID)
	if err != nil {
		return GarageInfo{}, apperror.Internal("read garage", err)
	}

	return garage, nil
}

// nextNumber reserves and renders the quote's document number from the
// garage's quote_number_template (FR-TEN-05), in the garage's timezone.
func (uc *QuoteUseCase) nextNumber(ctx context.Context, garageID string, garage GarageInfo) (string, error) {
	return uc.nextDocumentNumber(ctx, garageID, docTypeQuote, garage.QuoteNumberTemplate, garage.Timezone)
}

// nextRepairOrderNumber reserves and renders a repair order's document
// number from the garage's repair_order_number_template (FR-TEN-05), used
// by Convert.
func (uc *QuoteUseCase) nextRepairOrderNumber(ctx context.Context, garageID string, garage GarageInfo) (string, error) {
	return uc.nextDocumentNumber(ctx, garageID, docTypeRepairOrder, garage.RepairOrderNumberTemplate, garage.Timezone)
}

func (uc *QuoteUseCase) nextDocumentNumber(ctx context.Context, garageID, docType, template, timezone string) (string, error) {
	now := uc.Now()
	if loc, err := time.LoadLocation(timezone); err == nil {
		now = now.In(loc)
	}

	period := numbering.Period(template, now)

	seq, err := uc.Sequences.Next(ctx, garageID, docType, period)
	if err != nil {
		return "", err
	}

	return numbering.Render(template, now, seq), nil
}

// buildLines validates inputs and assigns each line an id, position and
// computed total.
func (uc *QuoteUseCase) buildLines(ctx context.Context, garageID string, inputs []LineInput) ([]domain.Line, error) {
	if len(inputs) == 0 {
		return nil, domain.ErrNoLines
	}

	if len(inputs) > domain.MaxLines {
		return nil, domain.ErrTooManyLines
	}

	lines := make([]domain.Line, 0, len(inputs))
	billable := 0

	for i, in := range inputs {
		description := strings.TrimSpace(in.Description)
		if description == "" {
			return nil, domain.ErrDescriptionRequired
		}

		if err := domain.CheckLength(description, domain.MaxDescriptionLen); err != nil {
			return nil, err
		}

		if !in.ItemType.IsValid() {
			return nil, domain.ErrInvalidItemType
		}

		if in.UnitPrice < 0 {
			return nil, domain.ErrInvalidUnitPrice
		}

		jobID, price, err := lineOrigin(in)
		if err != nil {
			return nil, err
		}

		if !in.NonBillable {
			billable++
		}

		stored, err := partquantity.ForLine(ctx, uc.Parts, garageID, in.ItemType == domain.ItemTypePart, in.ItemRefID,
			in.Quantity, in.UnitID, in.EnteredQuantity)
		if err != nil {
			return nil, err
		}

		scaled, err := domain.ParseQuantity(stored.Quantity)
		if err != nil {
			return nil, err
		}

		lines = append(lines, domain.Line{
			ID: uuid.NewString(), Position: i + 1, ItemType: in.ItemType, ItemRefID: in.ItemRefID,
			Description: description, Quantity: stored.Quantity, UnitID: stored.UnitID, EnteredQuantity: stored.Entered,
			UnitAbbreviation: stored.UnitAbbreviation,
			UnitPrice:        price,
			LineTotal:        domain.LineTotal(scaled, price), Decision: domain.DecisionPending,
			JobID: jobID, NonBillable: in.NonBillable,
		})
	}

	if billable == 0 {
		return nil, domain.ErrInvalidNonBillable
	}

	return lines, nil
}

// lineOrigin checks a line's job reference and hidden-consumable mark and
// returns the job id and the price to store: a hidden consumable is a
// catalogue part line, priced at zero.
func lineOrigin(in LineInput) (*string, int64, error) {
	var jobID *string

	if in.JobID != nil && *in.JobID != "" {
		if uuid.Validate(*in.JobID) != nil {
			return nil, 0, domain.ErrInvalidJobRef
		}

		jobID = in.JobID
	}

	if !in.NonBillable {
		return jobID, in.UnitPrice, nil
	}

	if in.ItemType != domain.ItemTypePart || in.ItemRefID == nil || *in.ItemRefID == "" {
		return nil, 0, domain.ErrInvalidNonBillable
	}

	return jobID, 0, nil
}

// CheckLength validates a quote's free-text notes.
func CheckLength(notes string) error {
	return domain.CheckLength(strings.TrimSpace(notes), domain.MaxNotesLen)
}
