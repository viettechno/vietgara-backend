package usecase

import (
	"context"
	"slices"
	"time"

	"github.com/viettechno/vietgara-backend/internal/receivable/domain"
)

// Reconciliation reconciles the receivables of the period [from, to), two
// "YYYY-MM-DD" days in the garage's timezone, with the settlements and
// vouchers behind them (FR-REC-04).
func (uc *ReceivableUseCase) Reconciliation(ctx context.Context, garageID, from, to string) (*domain.Reconciliation, error) {
	f, t, err := domain.ParsePeriod(from, to)
	if err != nil {
		return nil, toAppError("receivable reconciliation", err)
	}

	loc, err := uc.location(ctx, garageID)
	if err != nil {
		return nil, toAppError("receivable reconciliation", err)
	}

	start, end := inLocation(f, loc), inLocation(t, loc)

	opening, err := uc.repo.BalanceBefore(ctx, garageID, start)
	if err != nil {
		return nil, toAppError("receivable reconciliation", err)
	}

	lines, err := uc.repo.Entries(ctx, garageID, start, end)
	if err != nil {
		return nil, toAppError("receivable reconciliation", err)
	}

	rep := &domain.Reconciliation{From: from, To: to, OpeningBalance: opening}
	rep.Lines, rep.CollectedByMethod = fold(rep, lines, loc)
	rep.ClosingBalance = rep.OpeningBalance + rep.ChargedAmount - rep.CollectedAmount + rep.VoidedAmount

	ledger, err := uc.repo.LedgerBalance(ctx, garageID)
	if err != nil {
		return nil, toAppError("receivable reconciliation", err)
	}

	remaining, err := uc.repo.RemainingTotal(ctx, garageID)
	if err != nil {
		return nil, toAppError("receivable reconciliation", err)
	}

	rep.Difference, rep.Matches = ledger-remaining, ledger == remaining

	if rep.PaidOutAmount, err = uc.paidOut.PaidOut(ctx, garageID, start, end); err != nil {
		return nil, toAppError("receivable reconciliation", err)
	}

	return rep, nil
}

// inLocation is the start of a calendar day in loc.
func inLocation(day time.Time, loc *time.Location) time.Time {
	y, m, d := day.Date()

	return time.Date(y, m, d, 0, 0, 0, 0, loc)
}

// fold adds the period's entries to the totals of rep and returns its lines
// and the amounts of the payments by method (gross of voids, so that they add
// up to CollectedAmount), in a fixed order.
func fold(rep *domain.Reconciliation, lines []LedgerLine, loc *time.Location) ([]domain.Line, []domain.MethodTotal) {
	out := make([]domain.Line, 0, len(lines))
	byMethod := map[string]int64{}

	for _, l := range lines {
		e := l.Entry

		switch e.Kind {
		case domain.EntryCharge:
			rep.ChargedAmount += e.Amount
			rep.SettlementCount++
		case domain.EntryPayment:
			rep.CollectedAmount += e.Amount
			rep.PaymentCount++
			byMethod[e.Method] += e.Amount
		case domain.EntryVoid:
			rep.VoidedAmount += e.Amount
		}

		out = append(out, domain.Line{
			Date: e.OccurredAt.In(loc).Format(time.DateOnly), Kind: e.Kind, Reference: e.Reference,
			CustomerName: l.CustomerName, Amount: e.Amount, Method: e.Method,
		})
	}

	methods := make([]domain.MethodTotal, 0, len(byMethod))
	for m, amount := range byMethod {
		methods = append(methods, domain.MethodTotal{Method: m, Amount: amount})
	}

	slices.SortFunc(methods, func(a, b domain.MethodTotal) int {
		if a.Method < b.Method {
			return -1
		}

		return 1
	})

	return out, methods
}
