package usecase_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"

	"github.com/viettechno/vietgara-backend/internal/mocks"
	"github.com/viettechno/vietgara-backend/internal/platform/audit"
	"github.com/viettechno/vietgara-backend/internal/receivable/domain"
	"github.com/viettechno/vietgara-backend/internal/receivable/usecase"
	"github.com/viettechno/vietgara-backend/pkg/issuer"
)

const (
	supplierID   = "77777777-7777-7777-7777-777777777777"
	otherSupp    = "88888888-8888-8888-8888-888888888888"
	payableID    = "99999999-9999-9999-9999-999999999999"
	payableTwoID = "99999999-9999-9999-9999-999999999998"
	receiptID    = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	voucherID    = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
)

type payableDeps struct {
	Repo      *mocks.MockReceivablePayableRepository
	Suppliers *mocks.MockReceivableSupplierReader
	Garages   *mocks.MockReceivableGarageReader
	Issuers   *mocks.MockReceivableIssuerReader
	Staff     *mocks.MockReceivableStaffReader
	Sequences *mocks.MockReceivableSequencer
	Audit     *mocks.MockReceivableAuditLogger
}

func newPayableUseCase(t *testing.T) (*usecase.PayableUseCase, *payableDeps) {
	t.Helper()

	d := &payableDeps{
		Repo: mocks.NewMockReceivablePayableRepository(t), Suppliers: mocks.NewMockReceivableSupplierReader(t),
		Garages: mocks.NewMockReceivableGarageReader(t), Issuers: mocks.NewMockReceivableIssuerReader(t),
		Staff: mocks.NewMockReceivableStaffReader(t), Sequences: mocks.NewMockReceivableSequencer(t),
		Audit: mocks.NewMockReceivableAuditLogger(t),
	}

	uc := usecase.NewPayableUseCase(usecase.PayableDeps{
		Repo: d.Repo, Suppliers: d.Suppliers, Garages: d.Garages, Issuers: d.Issuers, Staff: d.Staff, Sequences: d.Sequences,
		Audit: d.Audit, Now: func() time.Time { return now },
	})

	return uc, d
}

func (d *payableDeps) timezone() {
	d.Garages.EXPECT().Timezone(mock.Anything, garageID).Return("Asia/Ho_Chi_Minh", nil).Maybe()
}

func (d *payableDeps) expectAudit() *audit.Entry {
	var got audit.Entry

	d.Audit.EXPECT().Record(mock.Anything, mock.Anything).
		Run(func(_ context.Context, e audit.Entry) { got = e }).Return(nil).Once()

	return &got
}

// issuing lets a voucher be numbered, signed and snapshotted.
func (d *payableDeps) issuing() {
	d.timezone()
	d.Sequences.EXPECT().Next(mock.Anything, garageID, "payment_out", "PC-2026-{SEQ:5}").Return(7, nil).Maybe()
	d.Issuers.EXPECT().Issuer(mock.Anything, garageID).Return(issuer.Issuer{Name: "Gara An Phat"}, nil).Maybe()
	d.Staff.EXPECT().FullName(mock.Anything, actorID).Return("Binh", nil).Maybe()
}

func openPayable(id, supplier string, total, paid int64) *domain.Payable {
	p := &domain.Payable{
		ID: id, GarageID: garageID, GoodsReceiptID: receiptID, GoodsReceiptNumber: "NK-" + id[len(id)-2:], SupplierID: supplier,
		SupplierName: "Sao Mai", ReceiptDate: day(2026, 10, 1), TotalAmount: total, PaidAmount: paid, Status: domain.PayableOpen,
	}
	p.Derive()

	return p
}
