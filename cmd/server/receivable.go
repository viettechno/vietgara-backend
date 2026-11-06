package main

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	billingDomain "github.com/viettechno/vietgara-backend/internal/billing/domain"
	billingUsecase "github.com/viettechno/vietgara-backend/internal/billing/usecase"
	customerDomain "github.com/viettechno/vietgara-backend/internal/customer/domain"
	customerRepository "github.com/viettechno/vietgara-backend/internal/customer/repository"
	"github.com/viettechno/vietgara-backend/internal/document/printmodel"
	"github.com/viettechno/vietgara-backend/internal/document/render"
	documentUsecase "github.com/viettechno/vietgara-backend/internal/document/usecase"
	identityRepo "github.com/viettechno/vietgara-backend/internal/identity/repository"
	notificationUsecase "github.com/viettechno/vietgara-backend/internal/notification/usecase"
	"github.com/viettechno/vietgara-backend/internal/platform/audit"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platform/numbering"
	receivableDomain "github.com/viettechno/vietgara-backend/internal/receivable/domain"
	receivableHandler "github.com/viettechno/vietgara-backend/internal/receivable/handler"
	receivableRepository "github.com/viettechno/vietgara-backend/internal/receivable/repository"
	receivableUsecase "github.com/viettechno/vietgara-backend/internal/receivable/usecase"
	repairorderUsecase "github.com/viettechno/vietgara-backend/internal/repairorder/usecase"
	supplierDomain "github.com/viettechno/vietgara-backend/internal/supplier/domain"
	supplierRepository "github.com/viettechno/vietgara-backend/internal/supplier/repository"
	tenantRepo "github.com/viettechno/vietgara-backend/internal/tenant/repository"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
	"github.com/viettechno/vietgara-backend/pkg/issuer"
)

// receivableModule is the wired Receivables module (HLD module #9, Release
// 13.0): the receivables fed by the events of Billing & Settlement, the
// scheduled reminders and the reconciliation.
type receivableModule struct {
	uc             *receivableUsecase.ReceivableUseCase
	server         *receivableHandler.Server
	payables       *receivableUsecase.PayableUseCase
	payablesServer *receivableHandler.PayableServer
}

func buildReceivable(
	pool *platformdb.Pool,
	tx *platformdb.TxManager,
	billing *billingUsecase.SettlementUseCase,
	repairOrders *repairorderUsecase.RepairOrderUseCase,
	vehicles *customerRepository.PostgresVehicleRepository,
	garages *tenantRepo.PostgresGarageRepository,
	notification *notificationUsecase.NotificationUseCase,
	suppliers *supplierRepository.PostgresRepository,
	users *identityRepo.PostgresUserRepository,
	auditLog *audit.Logger,
) *receivableModule {
	repo := receivableRepository.NewPostgresRepository(pool)

	uc := receivableUsecase.NewReceivableUseCase(receivableUsecase.Deps{
		Repo:        repo,
		Settlements: receivableSettlements{billing: billing, repairOrders: repairOrders, vehicles: vehicles},
		Garages:     receivableGarages{garages: garages},
		Sender:      receivableReminders{uc: notification},
		PaidOut:     repo,
		Tx:          garageTransactor{tx: tx},
		Audit:       auditLog,
	})

	payables := receivableUsecase.NewPayableUseCase(receivableUsecase.PayableDeps{
		Repo: repo, Suppliers: receivableSuppliers{suppliers: suppliers}, Garages: receivableGarages{garages: garages},
		Issuers: receivableIssuers{garages: garages}, Staff: receivableStaff{users: users},
		Sequences: numbering.NewSequencer(pool), Audit: auditLog,
	})

	return &receivableModule{
		uc: uc, server: receivableHandler.NewServer(uc), payables: payables, payablesServer: receivableHandler.NewPayableServer(payables),
	}
}

// receivableSuppliers lets Receivables read the supplier a voucher pays.
type receivableSuppliers struct {
	suppliers *supplierRepository.PostgresRepository
}

func (a receivableSuppliers) Get(ctx context.Context, garageID, supplierID string) (receivableUsecase.SupplierView, error) {
	s, err := a.suppliers.FindByID(ctx, garageID, supplierID)
	if errors.Is(err, supplierDomain.ErrSupplierNotFound) {
		return receivableUsecase.SupplierView{}, receivableDomain.ErrSupplierNotFound
	}

	if err != nil {
		return receivableUsecase.SupplierView{}, err
	}

	return receivableUsecase.SupplierView{Name: s.Name, Address: deref(s.Contact.Address)}, nil
}

// receivableIssuers gives a voucher the garage's legal identity as it is now.
type receivableIssuers struct {
	garages *tenantRepo.PostgresGarageRepository
}

func (a receivableIssuers) Issuer(ctx context.Context, garageID string) (issuer.Issuer, error) {
	garage, err := a.garages.FindByID(ctx, garageID)
	if err != nil {
		return issuer.Issuer{}, err
	}

	return issuerOf(garage), nil
}

// receivableStaff names the staff member who issues a voucher.
type receivableStaff struct {
	users *identityRepo.PostgresUserRepository
}

func (a receivableStaff) FullName(ctx context.Context, userID string) (string, error) {
	return billingCollectors(a).FullName(ctx, userID)
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
	// without a customer: it is listed but never reminded. Any other failure is
	// returned, so that the event is delivered again and the customer found.
	ro, err := a.repairOrders.Get(ctx, garageID, s.RepairOrderID)
	if appErr, ok := apperror.As(err); ok && appErr.Code == apperror.CodeNotFound {
		return view, nil
	}

	if err != nil {
		return receivableUsecase.SettlementView{}, err
	}

	vehicle, err := a.vehicles.FindByID(ctx, garageID, ro.VehicleID)
	if errors.Is(err, customerDomain.ErrVehicleNotFound) {
		return view, nil
	}

	if err != nil {
		return receivableUsecase.SettlementView{}, err
	}

	view.CustomerID = vehicle.CustomerID

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

// documentPayables lets Document/Print export the supplier payables as Excel
// (FR-DOC-02) and print the payment-out voucher (FR-BIL-07).
type documentPayables struct {
	uc *receivableUsecase.PayableUseCase
}

func (a documentPayables) SupplierPayables(ctx context.Context, garageID, supplierID string, includePaid bool) ([]render.PayableRow, error) {
	var rows []render.PayableRow

	for page := 1; ; page++ {
		res, err := a.uc.ListPayables(ctx, receivableUsecase.ListPayablesRequest{
			GarageID: garageID, SupplierID: supplierID, IncludePaid: includePaid,
			PageRequest: receivableUsecase.PageRequest{Page: page, PageSize: receivableUsecase.MaxPageSize},
		})
		if err != nil {
			return nil, err
		}

		for _, item := range res.Items {
			p := item.Payable
			rows = append(rows, render.PayableRow{
				Receipt: p.GoodsReceiptNumber, Supplier: p.SupplierName, ReceiptDate: p.ReceiptDate.Format(time.DateOnly), AgeDays: item.AgeDays,
				Total: p.TotalAmount, Paid: p.PaidAmount, Remaining: p.Remaining(), Status: string(p.Status),
			})
		}

		if page*res.PageSize >= res.Total {
			return rows, nil
		}
	}
}

func (a documentPayables) PaymentOutVoucher(ctx context.Context, garageID, voucherID string) (documentUsecase.PaymentOutInfo, error) {
	v, err := a.uc.GetVoucher(ctx, garageID, voucherID)
	if err != nil {
		return documentUsecase.PaymentOutInfo{}, err
	}

	var legal issuer.Issuer
	if err := json.Unmarshal(v.Issuer, &legal); err != nil {
		return documentUsecase.PaymentOutInfo{}, apperror.Internal("read voucher issuer", err)
	}

	info := documentUsecase.PaymentOutInfo{
		Number: v.Number, Date: v.CreatedAt.In(vietnamTime).Format("02/01/2006"), PayeeName: v.PayeeName, PayeeAddress: v.PayeeAddress,
		Reason: v.Reason, Amount: v.Amount, Method: voucherMethod(billingDomain.PaymentMethod(v.Method)), IssuerName: v.IssuerName,
		Issuer: legal, Voided: v.Voided(), VoidReason: v.VoidReason,
	}

	for _, al := range v.Allocations {
		info.Receipts = append(info.Receipts, printmodel.PaymentOutReceiptRow{Number: al.GoodsReceiptNumber, Amount: al.Amount})
	}

	return info, nil
}
