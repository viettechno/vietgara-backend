package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/viettechno/vietgara-backend/internal/datarequest/domain"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// Page sizes.
const (
	DefaultPageSize = 20
	MaxPageSize     = 100
)

// StaffUseCase records and answers the requests a garage's customers make in
// person (FR-CUS-05).
type StaffUseCase struct {
	repo      Repository
	customers Customers
	documents Documents
	names     Names
	now       func() time.Time
}

// NewStaffUseCase wires the use case.
func NewStaffUseCase(repo Repository, customers Customers, documents Documents, names Names) *StaffUseCase {
	return &StaffUseCase{repo: repo, customers: customers, documents: documents, names: names, now: time.Now}
}

// CreateRequest is the input of Create.
type CreateRequest struct {
	GarageID   string
	CustomerID string
	ActorID    string
	Type       domain.Type
	Details    string
}

// Create records a request a customer made in person and acts on it at once: a
// view is answered with the data held, a deletion anonymizes the customer, and
// a correction waits for the garage to make it and complete the request.
func (uc *StaffUseCase) Create(ctx context.Context, req CreateRequest) (*domain.Request, error) {
	const op = "create data request"

	if !req.Type.IsValid() {
		return nil, toAppError(op, domain.ErrInvalidType)
	}

	details, err := normalizeDetails(req.Type, req.Details)
	if err != nil {
		return nil, toAppError(op, err)
	}

	if err := uc.customers.Check(ctx, req.GarageID, req.CustomerID); err != nil {
		return nil, err
	}

	now := uc.now().UTC()
	r := &domain.Request{
		ID: uuid.NewString(), GarageID: req.GarageID, CustomerID: req.CustomerID, Type: req.Type, Status: domain.StatusPending,
		Source: domain.SourceStaff, Details: details, RequestedBy: ref(req.ActorID), RequestedAt: now,
	}

	if err := act(ctx, uc.customers, uc.documents, r, req.ActorID, now); err != nil {
		return nil, err
	}

	if err := uc.repo.Create(ctx, r); err != nil {
		return nil, toAppError(op, err)
	}

	if r.Type == domain.TypeDelete {
		if err := uc.repo.ClearDetails(ctx, req.GarageID, req.CustomerID); err != nil {
			return nil, toAppError(op, err)
		}
	}

	return r, nil
}

// act does what a request asks that needs no waiting and completes it: a view
// is answered, a deletion carried out; a correction is left pending.
func act(ctx context.Context, customers Customers, documents Documents, r *domain.Request, actorID string, now time.Time) error {
	switch r.Type {
	case domain.TypeView:
		export, err := exportOf(ctx, customers, documents, r.GarageID, r.CustomerID)
		if err != nil {
			return err
		}

		r.ExportJSON, r.Outcome = export, "Data provided"
	case domain.TypeDelete:
		counts, err := customers.Anonymize(ctx, r.GarageID, r.CustomerID, actorID)
		if err != nil {
			return err
		}

		r.Outcome = fmt.Sprintf("Anonymized %d customer, %d vehicle(s) and %d tax profile(s)", counts.Customers, counts.Vehicles, counts.TaxProfiles)
	case domain.TypeCorrect:
		return nil
	}

	r.Status, r.CompletedAt = domain.StatusCompleted, &now

	return nil
}

// exportOf is the data held about a customer with the numbers of the
// customer's documents, as JSON text.
func exportOf(ctx context.Context, customers Customers, documents Documents, garageID, customerID string) (string, error) {
	data, err := customers.Export(ctx, garageID, customerID)
	if err != nil {
		return "", err
	}

	numbers, err := documents.Numbers(ctx, garageID, customerID)
	if err != nil {
		return "", toAppError("list the customer's documents", err)
	}

	out, err := json.MarshalIndent(map[string]any{"data": data, "documents": numbers}, "", "  ")
	if err != nil {
		return "", toAppError("encode the export", err)
	}

	return string(out), nil
}

// normalizeDetails trims what a person asks to correct and drops it for the
// other types; a correction needs it, within the limit.
func normalizeDetails(t domain.Type, details string) (string, error) {
	if t != domain.TypeCorrect {
		return "", nil
	}

	details = strings.TrimSpace(details)

	switch {
	case details == "":
		return "", domain.ErrDetailsRequired
	case utf8.RuneCountInString(details) > domain.MaxTextLen:
		return "", domain.ErrTextTooLong
	default:
		return details, nil
	}
}

// CompleteRequest is the input of Complete.
type CompleteRequest struct {
	GarageID  string
	RequestID string
	ActorID   string
	Outcome   string
	Declined  bool
}

// Complete completes a waiting correction with what was done, or declines it.
func (uc *StaffUseCase) Complete(ctx context.Context, req CompleteRequest) (*domain.Request, error) {
	const op = "complete data request"

	outcome := strings.TrimSpace(req.Outcome)
	if outcome == "" {
		return nil, toAppError(op, domain.ErrOutcomeRequired)
	}

	if utf8.RuneCountInString(outcome) > domain.MaxTextLen {
		return nil, toAppError(op, domain.ErrTextTooLong)
	}

	r, err := uc.repo.FindForUpdate(ctx, req.GarageID, req.RequestID)
	if err != nil {
		return nil, toAppError(op, err)
	}

	if !r.Pending() {
		return nil, toAppError(op, domain.ErrNotPending)
	}

	now := uc.now().UTC()
	r.Status, r.Outcome, r.CompletedAt, r.CompletedBy = domain.StatusCompleted, outcome, &now, ref(req.ActorID)

	if req.Declined {
		r.Status = domain.StatusRejected
	}

	if err := uc.repo.Save(ctx, r); err != nil {
		return nil, toAppError(op, err)
	}

	if err := uc.name(ctx, r); err != nil {
		return nil, toAppError(op, err)
	}

	return r, nil
}

// Get returns one request.
func (uc *StaffUseCase) Get(ctx context.Context, garageID, requestID string) (*domain.Request, error) {
	const op = "get data request"

	r, err := uc.repo.Find(ctx, garageID, requestID)
	if err != nil {
		return nil, toAppError(op, err)
	}

	if err := uc.name(ctx, r); err != nil {
		return nil, toAppError(op, err)
	}

	return r, nil
}

// ListRequest is the input of List.
type ListRequest struct {
	GarageID   string
	CustomerID string
	Type       domain.Type
	Status     domain.Status
	Page       int
	PageSize   int
}

// List returns one page of the garage's requests, newest first.
func (uc *StaffUseCase) List(ctx context.Context, req ListRequest) (*Page, error) {
	const op = "list data requests"

	page, size := max(req.Page, 1), req.PageSize
	if size <= 0 {
		size = DefaultPageSize
	}

	size = min(size, MaxPageSize)

	items, total, err := uc.repo.List(ctx, req.GarageID, Filter{
		CustomerID: req.CustomerID, Type: req.Type, Status: req.Status, Limit: size, Offset: (page - 1) * size,
	})
	if err != nil {
		return nil, toAppError(op, err)
	}

	if err := uc.name(ctx, items...); err != nil {
		return nil, toAppError(op, err)
	}

	return &Page{Items: items, Total: total, Page: page, PageSize: size}, nil
}

// name fills the name of the staff member who completed each request.
func (uc *StaffUseCase) name(ctx context.Context, requests ...*domain.Request) error {
	var ids []string

	for _, r := range requests {
		if r.CompletedBy != nil {
			ids = append(ids, *r.CompletedBy)
		}
	}

	if len(ids) == 0 {
		return nil
	}

	names, err := uc.names.UserNames(ctx, ids)
	if err != nil {
		return err
	}

	for _, r := range requests {
		if r.CompletedBy != nil {
			r.CompletedByName = names[*r.CompletedBy]
		}
	}

	return nil
}

func ref(s string) *string {
	if s == "" {
		return nil
	}

	return &s
}

// toAppError classifies err; app errors of the collaborators pass through.
func toAppError(op string, err error) error {
	if _, ok := apperror.As(err); ok {
		return err
	}

	switch {
	case errors.Is(err, domain.ErrRequestNotFound), errors.Is(err, domain.ErrTokenInvalid):
		return apperror.Wrap(apperror.CodeNotFound, err.Error(), err)
	case errors.Is(err, domain.ErrNotPending):
		return apperror.Wrap(apperror.CodeFailedPrecondition, err.Error(), err).WithReason(domain.ReasonNotPending)
	case errors.Is(err, domain.ErrDetailsRequired):
		return apperror.Validation("details", apperror.IssueRequired, err.Error())
	case errors.Is(err, domain.ErrOutcomeRequired):
		return apperror.Validation("outcome", apperror.IssueRequired, err.Error())
	case errors.Is(err, domain.ErrTextTooLong):
		return apperror.Validation("details", apperror.IssueTooLong, err.Error())
	case errors.Is(err, domain.ErrInvalidType):
		return apperror.Validation("type", apperror.IssueInvalid, err.Error())
	case errors.Is(err, domain.ErrInvalidEmail):
		return apperror.Validation("email", apperror.IssueInvalid, err.Error())
	case errors.Is(err, domain.ErrTooManyRequests):
		return apperror.Wrap(apperror.CodeResourceExhausted, err.Error(), err)
	default:
		return apperror.Wrap(apperror.CodeInternal, op, err)
	}
}
