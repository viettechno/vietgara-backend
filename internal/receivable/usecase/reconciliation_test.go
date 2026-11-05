package usecase_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/receivable/domain"
	"github.com/viettechno/vietgara-backend/internal/receivable/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

func line(kind domain.EntryKind, amount int64, ref, method string, at time.Time) usecase.LedgerLine {
	return usecase.LedgerLine{Entry: domain.Entry{Kind: kind, Amount: amount, Reference: ref, Method: method, OccurredAt: at}, CustomerName: "An"}
}

func TestReceivableUseCase_Reconciliation(t *testing.T) {
	t.Parallel()

	hcm, err := time.LoadLocation("Asia/Ho_Chi_Minh")
	require.NoError(t, err)

	start, end := time.Date(2026, 11, 1, 0, 0, 0, 0, hcm), time.Date(2026, 12, 1, 0, 0, 0, 0, hcm)

	t.Run("the closing balance follows the ledger and the check compares it with the receivables", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.timezone()
		d.Repo.EXPECT().BalanceBefore(mock.Anything, garageID, start).Return(400_000, nil).Once()
		d.Repo.EXPECT().Entries(mock.Anything, garageID, start, end).Return([]usecase.LedgerLine{
			line(domain.EntryCharge, 1_000_000, "QT-1", "", start.Add(time.Hour)),
			line(domain.EntryPayment, 1_000_000, "PT-1", "cash", start.Add(2*time.Hour)),
			line(domain.EntryCharge, 2_000_000, "QT-2", "", start.Add(3*time.Hour)),
			line(domain.EntryPayment, 300_000, "PT-2", "cash", start.Add(4*time.Hour)),
			line(domain.EntryVoid, 300_000, "PT-2", "cash", start.Add(5*time.Hour)),
			line(domain.EntryPayment, 500_000, "PT-3", "bank_transfer", start.Add(6*time.Hour)),
			line(domain.EntryCharge, 800_000, "QT-3", "", start.Add(7*time.Hour)),
		}, nil).Once()
		d.Repo.EXPECT().LedgerBalance(mock.Anything, garageID).Return(2_700_000, nil).Once()
		d.Repo.EXPECT().RemainingTotal(mock.Anything, garageID).Return(2_700_000, nil).Once()
		d.PaidOut.EXPECT().PaidOut(mock.Anything, garageID, start, end).Return(250_000, nil).Once()

		rep, err := uc.Reconciliation(t.Context(), garageID, "2026-11-01", "2026-12-01")
		require.NoError(t, err)

		require.EqualValues(t, 400_000, rep.OpeningBalance)
		require.EqualValues(t, 3_800_000, rep.ChargedAmount)
		require.EqualValues(t, 1_800_000, rep.CollectedAmount)
		require.EqualValues(t, 300_000, rep.VoidedAmount)
		require.EqualValues(t, 2_700_000, rep.ClosingBalance)
		require.Equal(t, 3, rep.SettlementCount)
		require.Equal(t, 3, rep.PaymentCount)
		require.EqualValues(t, 250_000, rep.PaidOutAmount)
		require.True(t, rep.Matches)
		require.Zero(t, rep.Difference)
		require.Equal(t, []domain.MethodTotal{{Method: "bank_transfer", Amount: 500_000}, {Method: "cash", Amount: 1_300_000}}, rep.CollectedByMethod)
		require.Len(t, rep.Lines, 7)
		require.Equal(t, "2026-11-01", rep.Lines[0].Date)
		require.Equal(t, domain.EntryCharge, rep.Lines[0].Kind)
	})

	t.Run("a gap between the ledger and the receivables is reported", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.timezone()
		d.Repo.EXPECT().BalanceBefore(mock.Anything, garageID, mock.Anything).Return(0, nil).Once()
		d.Repo.EXPECT().Entries(mock.Anything, garageID, mock.Anything, mock.Anything).Return(nil, nil).Once()
		d.Repo.EXPECT().LedgerBalance(mock.Anything, garageID).Return(1_000_000, nil).Once()
		d.Repo.EXPECT().RemainingTotal(mock.Anything, garageID).Return(700_000, nil).Once()
		d.PaidOut.EXPECT().PaidOut(mock.Anything, garageID, mock.Anything, mock.Anything).Return(0, nil).Once()

		rep, err := uc.Reconciliation(t.Context(), garageID, "2026-11-01", "2026-12-01")
		require.NoError(t, err)
		require.False(t, rep.Matches)
		require.EqualValues(t, 300_000, rep.Difference)
		require.Empty(t, rep.Lines)
		require.Empty(t, rep.CollectedByMethod)
	})
}

func TestReceivableUseCase_Reconciliation_Part2(t *testing.T) {
	t.Parallel()

	t.Run("a bad period is a validation error", func(t *testing.T) {
		t.Parallel()

		uc, _ := newUseCase(t)

		for _, p := range [][2]string{{"2026-11-01", "2026-11-01"}, {"x", "2026-11-01"}, {"2026-12-01", "2026-11-01"}} {
			_, err := uc.Reconciliation(t.Context(), garageID, p[0], p[1])
			assertAppError(t, err, apperror.CodeInvalidArgument)
		}
	})

	t.Run("each failing read is an internal error", func(t *testing.T) {
		t.Parallel()

		steps := []string{"zone", "before", "entries", "ledger", "remaining", "paidout"}
		for _, failAt := range steps {
			uc, d := newUseCase(t)

			if failAt == "zone" {
				d.Garages.EXPECT().Timezone(mock.Anything, garageID).Return("", errBoom).Once()
			} else {
				d.timezone()
			}

			if failAt != "zone" {
				call := d.Repo.EXPECT().BalanceBefore(mock.Anything, garageID, mock.Anything)
				if failAt == "before" {
					call.Return(0, errBoom).Once()
				} else {
					call.Return(0, nil).Once()
				}
			}

			if failAt != "zone" && failAt != "before" {
				call := d.Repo.EXPECT().Entries(mock.Anything, garageID, mock.Anything, mock.Anything)
				if failAt == "entries" {
					call.Return(nil, errBoom).Once()
				} else {
					call.Return(nil, nil).Once()
				}
			}

			switch failAt {
			case "ledger":
				d.Repo.EXPECT().LedgerBalance(mock.Anything, garageID).Return(0, errBoom).Once()
			case "remaining", "paidout":
				d.Repo.EXPECT().LedgerBalance(mock.Anything, garageID).Return(0, nil).Once()
			}

			switch failAt {
			case "remaining":
				d.Repo.EXPECT().RemainingTotal(mock.Anything, garageID).Return(0, errBoom).Once()
			case "paidout":
				d.Repo.EXPECT().RemainingTotal(mock.Anything, garageID).Return(0, nil).Once()
				d.PaidOut.EXPECT().PaidOut(mock.Anything, garageID, mock.Anything, mock.Anything).Return(0, errBoom).Once()
			}

			_, err := uc.Reconciliation(t.Context(), garageID, "2026-11-01", "2026-12-01")
			assertAppError(t, err, apperror.CodeInternal)
		}
	})
}
