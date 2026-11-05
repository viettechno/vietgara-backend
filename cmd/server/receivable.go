package main

import (
	"context"
	"time"

	billingUsecase "github.com/viettechno/vietgara-backend/internal/billing/usecase"
	customerRepository "github.com/viettechno/vietgara-backend/internal/customer/repository"
	"github.com/viettechno/vietgara-backend/internal/document/render"
	notificationUsecase "github.com/viettechno/vietgara-backend/internal/notification/usecase"
	"github.com/viettechno/vietgara-backend/internal/platform/audit"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	receivableHandler "github.com/viettechno/vietgara-backend/internal/receivable/handler"
	receivableRepository "github.com/viettechno/vietgara-backend/internal/receivable/repository"
	receivableUsecase "github.com/viettechno/vietgara-backend/internal/receivable/usecase"
	repairorderUsecase "github.com/viettechno/vietgara-backend/internal/repairorder/usecase"
	tenantRepo "github.com/viettechno/vietgara-backend/internal/tenant/repository"
)

// receivableModule is the wired Receivables module (HLD module #9, Release
// 13.0): the receivables fed by the events of Billing & Settlement, the
// scheduled reminders and the reconciliation.
type receivableModule struct {
	uc     *receivableUsecase.ReceivableUseCase
	server *receivableHandler.Server
}

func buildReceivable(
	pool *platformdb.Pool,
	tx *platformdb.TxManager,
	billing *billingUsecase.SettlementUseCase,
	repairOrders *repairorderUsecase.RepairOrderUseCase,
	vehicles *customerRepository.PostgresVehicleRepository,
	garages *tenantRepo.PostgresGarageRepository,
	notification *notificationUsecase.NotificationUseCase,
	auditLog *audit.Logger,
) *receivableModule {
	uc := receivableUsecase.NewReceivableUseCase(receivableUsecase.Deps{
		Repo:        receivableRepository.NewPostgresRepository(pool),
		Settlements: receivableSettlements{billing: billing, repairOrders: repairOrders, vehicles: vehicles},
		Garages:     receivableGarages{garages: garages},
		Sender:      receivableReminders{uc: notification},
		PaidOut:     receivablePaidOut{},
		Tx:          garageTransactor{tx: tx},
		Audit:       auditLog,
	})

	return &receivableModule{uc: uc, server: receivableHandler.NewServer(uc)}
}

// receivableSettlements lets Receivables read a settlement with the customer
// it is billed through (the repair order's vehicle's customer).
type receivableSettlements struct {
	billing      *billingUsecase.SettlementUseCase
	repairOrders *repairorderUsecase.RepairOrderUseCase
	vehicles     *customerRepository.PostgresVehicleRepository
}

func (a receivableSettlements) Get(ctx context.Context, garageID, settlementID string) (receivableUsecase.SettlementView, error) {
	s, err := a.billing.Get(ctx, garageID, settlementID)
	if err != nil {
		return receivableUsecase.SettlementView{}, err
	}

	view := receivableUsecase.SettlementView{
		ID: s.ID, Number: s.Number, CustomerName: s.CustomerName, VehicleLicensePlate: s.VehicleLicensePlate,
		TotalAmount: s.TotalAmount, PaidAmount: s.PaidAmount, CreatedAt: s.CreatedAt,
	}

	for _, p := range s.Payments {
		view.Payments = append(view.Payments, receivableUsecase.PaymentView{
			ID: p.ID, VoucherNumber: p.VoucherNumber, Amount: p.Amount, Method: string(p.Method), CreatedAt: p.CreatedAt, VoidedAt: p.VoidedAt,
		})
	}

	// A settlement whose repair order or vehicle is gone keeps its receivable
	// without a customer: it is listed but never reminded.
	if ro, err := a.repairOrders.Get(ctx, garageID, s.RepairOrderID); err == nil {
		if vehicle, err := a.vehicles.FindByID(ctx, garageID, ro.VehicleID); err == nil {
			view.CustomerID = vehicle.CustomerID
		}
	}

	return view, nil
}

// receivableGarages gives Receivables the garage's timezone.
type receivableGarages struct {
	garages *tenantRepo.PostgresGarageRepository
}

func (a receivableGarages) Timezone(ctx context.Context, garageID string) (string, error) {
	garage, err := a.garages.FindByID(ctx, garageID)
	if err != nil {
		return "", err
	}

	return garage.Timezone, nil
}

// receivableReminders sends a payment reminder through Notification.
type receivableReminders struct {
	uc *notificationUsecase.NotificationUseCase
}

func (a receivableReminders) SendPaymentReminder(ctx context.Context, r receivableUsecase.Reminder) (receivableUsecase.Delivery, error) {
	res, err := a.uc.PaymentReminder(ctx, notificationUsecase.PaymentReminderRequest{
		GarageID: r.GarageID, CustomerID: r.CustomerID, ReceivableID: r.ReceivableID, Number: r.SettlementNumber,
		Remaining: r.Remaining, DueDate: r.DueDate, DaysOverdue: r.DaysOverdue,
	})

	return receivableUsecase.Delivery{Sent: res.Sent, Reason: res.Reason}, err
}

// receivablePaidOut totals the payment-out vouchers of a period for the
// reconciliation. The vouchers arrive with the payables of this release.
type receivablePaidOut struct{}

func (receivablePaidOut) PaidOut(context.Context, string, time.Time, time.Time) (int64, error) {
	return 0, nil
}

// documentReceivables lets Document/Print export the receivables and their
// reconciliation as Excel (FR-DOC-02, FR-REC-04).
type documentReceivables struct {
	uc *receivableUsecase.ReceivableUseCase
}

func (a documentReceivables) Receivables(
	ctx context.Context, garageID, customerID, query string, overdueOnly, includePaid bool,
) ([]render.ReceivableRow, error) {
	var rows []render.ReceivableRow

	for page := 1; ; page++ {
		res, err := a.uc.List(ctx, receivableUsecase.ListRequest{
			GarageID: garageID, CustomerID: customerID, Query: query, OverdueOnly: overdueOnly, IncludePaid: includePaid,
			PageRequest: receivableUsecase.PageRequest{Page: page, PageSize: receivableUsecase.MaxPageSize},
		})
		if err != nil {
			return nil, err
		}

		for _, item := range res.Items {
			r := item.Receivable
			rows = append(rows, render.ReceivableRow{
				Number: r.SettlementNumber, Customer: r.CustomerName, Plate: r.VehicleLicensePlate, DueDate: r.DueDate.Format(time.DateOnly),
				DaysOverdue: item.DaysOverdue, Total: r.TotalAmount, Paid: r.PaidAmount, Remaining: r.Remaining(), Status: string(r.Status),
			})
		}

		if page*res.PageSize >= res.Total {
			return rows, nil
		}
	}
}

func (a documentReceivables) Reconciliation(ctx context.Context, garageID, from, to string) (render.ReconciliationData, error) {
	rep, err := a.uc.Reconciliation(ctx, garageID, from, to)
	if err != nil {
		return render.ReconciliationData{}, err
	}

	out := render.ReconciliationData{
		From: rep.From, To: rep.To, Opening: rep.OpeningBalance, Charged: rep.ChargedAmount, Collected: rep.CollectedAmount,
		Voided: rep.VoidedAmount, Closing: rep.ClosingBalance, PaidOut: rep.PaidOutAmount, Matches: rep.Matches, Difference: rep.Difference,
	}

	for _, m := range rep.CollectedByMethod {
		out.CollectedMethod = append(out.CollectedMethod, render.MethodAmount{Method: m.Method, Amount: m.Amount})
	}

	for _, l := range rep.Lines {
		out.Lines = append(out.Lines, render.ReconciliationLineRow{
			Date: l.Date, Kind: string(l.Kind), Reference: l.Reference, Customer: l.CustomerName, Method: l.Method, Amount: l.Amount,
		})
	}

	return out, nil
}
