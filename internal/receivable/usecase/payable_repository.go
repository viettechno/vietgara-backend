package usecase

import (
	"context"
	"time"

	"github.com/viettechno/vietgara-backend/internal/receivable/domain"
	"github.com/viettechno/vietgara-backend/pkg/issuer"
)

// PayableFilter selects one page of a garage's supplier payables.
type PayableFilter struct {
	SupplierID string
	// IncludePaid also lists the paid ones; cancelled ones are never listed.
	IncludePaid bool
	Limit       int
	Offset      int
}

// PayableSummary totals the open payables a list matched, across all its
// pages.
type PayableSummary struct {
	OpenCount         int
	OutstandingAmount int64
}

// VoucherFilter selects one page of a garage's payment-out vouchers.
type VoucherFilter struct {
	SupplierID    string
	IncludeVoided bool
	Limit         int
	Offset        int
}

// PayableRepository defines the persistence contract of supplier payables and
// payment-out vouchers. The tables are garage-scoped by row-level security.
type PayableRepository interface {
	// AddPayable inserts the payable unless the receipt already made one for
	// the supplier, so that a redelivered event adds nothing.
	AddPayable(ctx context.Context, p *domain.Payable) error
	// CancelPayables marks the receipt's payables cancelled.
	CancelPayables(ctx context.Context, garageID, goodsReceiptID string) error
	// ListPayables returns one page, oldest receipt first, and the summary of
	// the open payables the filter matches.
	ListPayables(ctx context.Context, garageID string, f PayableFilter) ([]*domain.Payable, int, PayableSummary, error)
	// Aging returns, per supplier, the remaining amount of the open payables
	// by age of the receipt as of today.
	Aging(ctx context.Context, garageID string, today time.Time) ([]domain.Aging, error)
	// LockPayables returns the payables of the ids, locked until the end of
	// the transaction, in id order; ones that do not exist are left out.
	LockPayables(ctx context.Context, garageID string, ids []string) ([]*domain.Payable, error)
	// SavePayable writes the paid amount and status of the payable.
	SavePayable(ctx context.Context, p *domain.Payable) error

	// AddVoucher inserts the voucher with its allocations.
	AddVoucher(ctx context.Context, v *domain.PaymentOutVoucher) error
	// FindVoucher returns the voucher with its allocations, or
	// domain.ErrVoucherNotFound.
	FindVoucher(ctx context.Context, garageID, voucherID string) (*domain.PaymentOutVoucher, error)
	// ListVouchers returns one page, newest first.
	ListVouchers(ctx context.Context, garageID string, f VoucherFilter) ([]*domain.PaymentOutVoucher, int, error)
	// VoidVoucher marks an unvoided voucher voided, or returns
	// domain.ErrVoucherNotFound or domain.ErrPaymentOutAlreadyVoided.
	VoidVoucher(ctx context.Context, garageID, voucherID, reason string, at time.Time) error
}

// SupplierView is what a voucher needs of its supplier.
type SupplierView struct {
	Name    string
	Address string
}

// SupplierReader reads a supplier; implemented by an adapter over the
// Supplier module, returning domain.ErrSupplierNotFound for an unknown one.
type SupplierReader interface {
	Get(ctx context.Context, garageID, supplierID string) (SupplierView, error)
}

// IssuerReader gives the garage's legal identity as it is now, which a
// voucher snapshots when issued.
type IssuerReader interface {
	Issuer(ctx context.Context, garageID string) (issuer.Issuer, error)
}

// StaffReader names the staff member who issues a voucher.
type StaffReader interface {
	FullName(ctx context.Context, userID string) (string, error)
}

// Sequencer hands out document sequence numbers; implemented by
// internal/platform/numbering.Sequencer.
type Sequencer interface {
	Next(ctx context.Context, garageID, docType, period string) (int, error)
}
