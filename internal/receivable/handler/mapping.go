package handler

import (
	"math"
	"time"

	billingv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/billing/v1"
	receivablev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/receivable/v1"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/viettechno/vietgara-backend/internal/receivable/domain"
	"github.com/viettechno/vietgara-backend/internal/receivable/usecase"
	"github.com/viettechno/vietgara-backend/pkg/converter"
)

func toInt32(n int) int32 {
	return int32(min(max(n, math.MinInt32), math.MaxInt32)) //nolint:gosec // clamped
}

func timePtr(t *time.Time) *timestamppb.Timestamp {
	if t == nil {
		return nil
	}

	return converter.TimeToProto(*t)
}

func receivableToProto(item usecase.Item) *receivablev1.Receivable {
	r := item.Receivable

	return &receivablev1.Receivable{
		Id: r.ID, GarageId: r.GarageID, SettlementId: r.SettlementID, SettlementNumber: r.SettlementNumber,
		CustomerId: r.CustomerID, CustomerName: r.CustomerName, VehicleLicensePlate: r.VehicleLicensePlate,
		TotalAmount: r.TotalAmount, PaidAmount: r.PaidAmount, RemainingAmount: r.Remaining(),
		DueDate: r.DueDate.Format(time.DateOnly), Overdue: item.Overdue(), DaysOverdue: toInt32(item.DaysOverdue),
		Status: statusToProto(r.Status), RemindersSent: toInt32(r.RemindersSent), LastReminderAt: timePtr(r.LastReminderAt),
		CreatedAt: converter.TimeToProto(r.CreatedAt), UpdatedAt: converter.TimeToProto(r.UpdatedAt),
	}
}

func statusToProto(s domain.Status) receivablev1.ReceivableStatus {
	switch s {
	case domain.StatusOpen:
		return receivablev1.ReceivableStatus_RECEIVABLE_STATUS_OPEN
	case domain.StatusPaid:
		return receivablev1.ReceivableStatus_RECEIVABLE_STATUS_PAID
	default:
		return receivablev1.ReceivableStatus_RECEIVABLE_STATUS_UNSPECIFIED
	}
}

func kindToProto(k domain.EntryKind) receivablev1.ReceivableEntryKind {
	switch k {
	case domain.EntryCharge:
		return receivablev1.ReceivableEntryKind_RECEIVABLE_ENTRY_KIND_CHARGE
	case domain.EntryPayment:
		return receivablev1.ReceivableEntryKind_RECEIVABLE_ENTRY_KIND_PAYMENT
	case domain.EntryVoid:
		return receivablev1.ReceivableEntryKind_RECEIVABLE_ENTRY_KIND_VOID
	default:
		return receivablev1.ReceivableEntryKind_RECEIVABLE_ENTRY_KIND_UNSPECIFIED
	}
}

// methodToProto maps the payment method text of the ledger to the enum.
func methodToProto(m string) billingv1.PaymentMethod {
	switch m {
	case "cash":
		return billingv1.PaymentMethod_PAYMENT_METHOD_CASH
	case "bank_transfer":
		return billingv1.PaymentMethod_PAYMENT_METHOD_BANK_TRANSFER
	case "online":
		return billingv1.PaymentMethod_PAYMENT_METHOD_ONLINE
	default:
		return billingv1.PaymentMethod_PAYMENT_METHOD_UNSPECIFIED
	}
}

func outcomeToProto(o domain.Outcome) receivablev1.ReminderOutcome {
	switch o {
	case domain.OutcomeSent:
		return receivablev1.ReminderOutcome_REMINDER_OUTCOME_SENT
	case domain.OutcomeSkipped:
		return receivablev1.ReminderOutcome_REMINDER_OUTCOME_SKIPPED
	case domain.OutcomeFailed:
		return receivablev1.ReminderOutcome_REMINDER_OUTCOME_FAILED
	default:
		return receivablev1.ReminderOutcome_REMINDER_OUTCOME_UNSPECIFIED
	}
}

func entriesToProto(entries []domain.Entry) []*receivablev1.ReceivableEntry {
	out := make([]*receivablev1.ReceivableEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, &receivablev1.ReceivableEntry{
			Id: e.ID, Kind: kindToProto(e.Kind), Amount: e.Amount, Reference: e.Reference, Method: methodToProto(e.Method),
			OccurredAt: converter.TimeToProto(e.OccurredAt),
		})
	}

	return out
}

func remindersToProto(reminders []domain.Reminder) []*receivablev1.ReceivableReminder {
	out := make([]*receivablev1.ReceivableReminder, 0, len(reminders))
	for _, r := range reminders {
		out = append(out, &receivablev1.ReceivableReminder{
			Id: r.ID, OffsetDays: toInt32(r.OffsetDays), Channel: r.Channel, Outcome: outcomeToProto(r.Outcome), Reason: r.Reason,
			CreatedAt: converter.TimeToProto(r.CreatedAt),
		})
	}

	return out
}

func settingsToProto(s domain.Settings) *receivablev1.ReceivableSettings {
	days, enabled := toInt32(s.PaymentTermsDays), s.RemindersEnabled
	out := &receivablev1.ReceivableSettings{PaymentTermsDays: &days, RemindersEnabled: &enabled}

	for _, o := range s.ReminderOffsetDays {
		out.ReminderOffsetDays = append(out.ReminderOffsetDays, toInt32(o))
	}

	return out
}

func offsetsFromProto(in []int32) []int {
	out := make([]int, len(in))
	for i, o := range in {
		out[i] = int(o)
	}

	return out
}

func reconciliationToProto(r *domain.Reconciliation) *receivablev1.ReconciliationReport {
	out := &receivablev1.ReconciliationReport{
		From: r.From, To: r.To, OpeningBalance: r.OpeningBalance, ChargedAmount: r.ChargedAmount,
		CollectedAmount: r.CollectedAmount, VoidedAmount: r.VoidedAmount, ClosingBalance: r.ClosingBalance,
		SettlementCount: toInt32(r.SettlementCount), PaymentCount: toInt32(r.PaymentCount), PaidOutAmount: r.PaidOutAmount,
		Matches: r.Matches, Difference: r.Difference,
	}

	for _, m := range r.CollectedByMethod {
		out.CollectedByMethod = append(out.CollectedByMethod, &receivablev1.ReconciliationMethodTotal{Method: methodToProto(m.Method), Amount: m.Amount})
	}

	for _, l := range r.Lines {
		out.Lines = append(out.Lines, &receivablev1.ReconciliationLine{
			Date: l.Date, Kind: kindToProto(l.Kind), Reference: l.Reference, CustomerName: l.CustomerName, Amount: l.Amount,
			Method: methodToProto(l.Method),
		})
	}

	return out
}
