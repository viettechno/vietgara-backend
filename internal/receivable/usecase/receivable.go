package usecase

import (
	"context"
	"strings"
	"time"

	"github.com/viettechno/vietgara-backend/internal/receivable/domain"
)

// Deps are the dependencies of ReceivableUseCase.
type Deps struct {
	Repo        Repository
	Settlements SettlementReader
	Garages     GarageReader
	Sender      ReminderSender
	PaidOut     PaidOutReader
	Tx          Transactor
	Audit       AuditLogger
	// Now is the clock; time.Now when nil.
	Now func() time.Time
}

// ReceivableUseCase is the Receivables module's application service
// (FR-REC-01, 03, 04).
type ReceivableUseCase struct {
	repo        Repository
	settlements SettlementReader
	garages     GarageReader
	sender      ReminderSender
	paidOut     PaidOutReader
	tx          Transactor
	audit       AuditLogger
	now         clock
}

// NewReceivableUseCase wires the use case.
func NewReceivableUseCase(d Deps) *ReceivableUseCase {
	now := d.Now
	if now == nil {
		now = time.Now
	}

	return &ReceivableUseCase{
		repo: d.Repo, settlements: d.Settlements, garages: d.Garages, sender: d.Sender,
		paidOut: d.PaidOut, tx: d.Tx, audit: d.Audit, now: now,
	}
}

// Item is a receivable with its position against today.
type Item struct {
	Receivable  *domain.Receivable
	DaysOverdue int
}

// Overdue reports whether the receivable is open and past its due date.
func (i Item) Overdue() bool { return i.DaysOverdue > 0 }

// ListRequest is the input of List.
type ListRequest struct {
	GarageID    string
	CustomerID  string
	OverdueOnly bool
	IncludePaid bool
	Query       string
	PageRequest
}

// ListResult is one page of receivables with the summary of the whole match.
type ListResult struct {
	Items    []Item
	Total    int
	Page     int
	PageSize int
	Summary  Summary
}

// location returns the garage's timezone, UTC when it is missing or unknown.
func (uc *ReceivableUseCase) location(ctx context.Context, garageID string) (*time.Location, error) {
	name, err := uc.garages.Timezone(ctx, garageID)
	if err != nil {
		return nil, err
	}

	loc, lerr := time.LoadLocation(name)
	if lerr != nil {
		return time.UTC, nil //nolint:nilerr // an unknown zone name falls back to UTC
	}

	return loc, nil
}

func (uc *ReceivableUseCase) today(ctx context.Context, garageID string) (time.Time, error) {
	loc, err := uc.location(ctx, garageID)
	if err != nil {
		return time.Time{}, err
	}

	return domain.Today(uc.now(), loc), nil
}

// List returns one page of the garage's receivables, most overdue first
// (FR-REC-01).
func (uc *ReceivableUseCase) List(ctx context.Context, req ListRequest) (*ListResult, error) {
	page := req.normalize()

	today, err := uc.today(ctx, req.GarageID)
	if err != nil {
		return nil, toAppError("list receivables", err)
	}

	items, total, summary, err := uc.repo.List(ctx, req.GarageID, ListFilter{
		CustomerID: req.CustomerID, OverdueOnly: req.OverdueOnly, IncludePaid: req.IncludePaid,
		Query: strings.TrimSpace(req.Query), Today: today, Limit: page.PageSize, Offset: page.offset(),
	})
	if err != nil {
		return nil, toAppError("list receivables", err)
	}

	out := make([]Item, len(items))
	for i, r := range items {
		out[i] = Item{Receivable: r, DaysOverdue: r.DaysOverdue(today)}
	}

	return &ListResult{Items: out, Total: total, Page: page.Page, PageSize: page.PageSize, Summary: summary}, nil
}

// Get returns one receivable with its ledger and reminders.
func (uc *ReceivableUseCase) Get(ctx context.Context, garageID, receivableID string) (Item, error) {
	r, err := uc.repo.FindByID(ctx, garageID, receivableID)
	if err != nil {
		return Item{}, toAppError("get receivable", err)
	}

	today, err := uc.today(ctx, garageID)
	if err != nil {
		return Item{}, toAppError("get receivable", err)
	}

	return Item{Receivable: r, DaysOverdue: r.DaysOverdue(today)}, nil
}

// GetSettings returns the garage's payment terms and reminder schedule.
func (uc *ReceivableUseCase) GetSettings(ctx context.Context, garageID string) (domain.Settings, error) {
	s, err := uc.repo.GetSettings(ctx, garageID)
	if err != nil {
		return domain.Settings{}, toAppError("get receivable settings", err)
	}

	return s, nil
}

// UpdateSettingsRequest is the input of UpdateSettings. The terms and the
// switch are pointers so that a request that leaves them out is refused
// instead of switching reminders off.
type UpdateSettingsRequest struct {
	GarageID           string
	ActorID            string
	PaymentTermsDays   *int
	RemindersEnabled   *bool
	ReminderOffsetDays []int
}

// UpdateSettings replaces the settings (FR-REC-03).
func (uc *ReceivableUseCase) UpdateSettings(ctx context.Context, req UpdateSettingsRequest) (domain.Settings, error) {
	if req.PaymentTermsDays == nil || req.RemindersEnabled == nil {
		return domain.Settings{}, toAppError("update receivable settings", domain.ErrIncompleteSettings)
	}

	next, err := domain.Settings{
		PaymentTermsDays: *req.PaymentTermsDays, RemindersEnabled: *req.RemindersEnabled, ReminderOffsetDays: req.ReminderOffsetDays,
	}.Normalize()
	if err != nil {
		return domain.Settings{}, toAppError("update receivable settings", err)
	}

	before, err := uc.repo.GetSettings(ctx, req.GarageID)
	if err != nil {
		return domain.Settings{}, toAppError("update receivable settings", err)
	}

	if err := uc.repo.SaveSettings(ctx, req.GarageID, next); err != nil {
		return domain.Settings{}, toAppError("update receivable settings", err)
	}

	if err := uc.record(ctx, req.ActorID, req.GarageID, ActionSettingsUpdated, entitySettings, req.GarageID,
		settingsAudit(before), settingsAudit(next)); err != nil {
		return domain.Settings{}, err
	}

	return next, nil
}

func settingsAudit(s domain.Settings) map[string]any {
	return map[string]any{
		"payment_terms_days": s.PaymentTermsDays, "reminders_enabled": s.RemindersEnabled, "reminder_offset_days": s.ReminderOffsetDays,
	}
}
