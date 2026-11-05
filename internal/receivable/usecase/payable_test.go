package usecase_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/platform/outbox"
	"github.com/viettechno/vietgara-backend/internal/receivable/domain"
	"github.com/viettechno/vietgara-backend/internal/receivable/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

type completedSupplier struct {
	SupplierID   string `json:"supplier_id"`
	SupplierName string `json:"supplier_name"`
	Amount       int64  `json:"amount"`
}

func completed(date string, suppliers ...completedSupplier) map[string]any {
	return map[string]any{"goods_receipt_id": receiptID, "garage_id": garageID, "number": "NK-0001", "receipt_date": date, "suppliers": suppliers}
}

func TestPayableUseCase_HandleReceiptCompleted(t *testing.T) {
	t.Parallel()

	t.Run("makes a payable for each supplier that is owed something", func(t *testing.T) {
		t.Parallel()

		uc, d := newPayableUseCase(t)

		var got []*domain.Payable

		d.Repo.EXPECT().AddPayable(mock.Anything, mock.Anything).Run(func(_ context.Context, p *domain.Payable) { got = append(got, p) }).Return(nil).Twice()

		err := uc.HandleReceiptCompleted(t.Context(), event(t, garageID, completed("2026-10-01",
			completedSupplier{supplierID, "Sao Mai", 700_000}, completedSupplier{"", "Nobody", 5}, completedSupplier{otherSupp, "Bao An", 0},
			completedSupplier{otherSupp, "Bao An", 120_000})))
		require.NoError(t, err)
		require.Len(t, got, 2)
		require.Equal(t, domain.Payable{
			GarageID: garageID, GoodsReceiptID: receiptID, GoodsReceiptNumber: "NK-0001", SupplierID: supplierID, SupplierName: "Sao Mai",
			ReceiptDate: day(2026, 10, 1), TotalAmount: 700_000, Status: domain.PayableOpen,
		}, *got[0])
		require.EqualValues(t, 120_000, got[1].TotalAmount)
	})

	t.Run("drops an event that cannot succeed on retry", func(t *testing.T) {
		t.Parallel()

		uc, _ := newPayableUseCase(t)
		ctx := t.Context()

		require.NoError(t, uc.HandleReceiptCompleted(ctx, outbox.Event{GarageID: garageID, Data: []byte("{")}))
		require.NoError(t, uc.HandleReceiptCompleted(ctx, event(t, "", completed("2026-10-01"))))
		require.NoError(t, uc.HandleReceiptCompleted(ctx, event(t, garageID, map[string]any{"number": "NK-1"})))
		require.NoError(t, uc.HandleReceiptCompleted(ctx, event(t, garageID, completed("1/10/2026", completedSupplier{supplierID, "Sao Mai", 1}))))
	})

	t.Run("returns a failure so the event is delivered again", func(t *testing.T) {
		t.Parallel()

		uc, d := newPayableUseCase(t)
		d.Repo.EXPECT().AddPayable(mock.Anything, mock.Anything).Return(errBoom).Once()

		err := uc.HandleReceiptCompleted(t.Context(), event(t, garageID, completed("2026-10-01", completedSupplier{supplierID, "Sao Mai", 1})))
		require.ErrorIs(t, err, errBoom)
	})
}

func TestPayableUseCase_HandleReceiptCancelled(t *testing.T) {
	t.Parallel()

	t.Run("cancels the payables of the receipt", func(t *testing.T) {
		t.Parallel()

		uc, d := newPayableUseCase(t)
		d.Repo.EXPECT().CancelPayables(mock.Anything, garageID, receiptID).Return(nil).Once()

		require.NoError(t, uc.HandleReceiptCancelled(t.Context(), event(t, garageID, map[string]string{"goods_receipt_id": receiptID})))
	})

	t.Run("drops a malformed event and returns a failure", func(t *testing.T) {
		t.Parallel()

		uc, d := newPayableUseCase(t)
		require.NoError(t, uc.HandleReceiptCancelled(t.Context(), outbox.Event{GarageID: garageID, Data: []byte("{")}))
		require.NoError(t, uc.HandleReceiptCancelled(t.Context(), event(t, garageID, map[string]string{})))

		d.Repo.EXPECT().CancelPayables(mock.Anything, garageID, receiptID).Return(errBoom).Once()
		require.ErrorIs(t, uc.HandleReceiptCancelled(t.Context(), event(t, garageID, map[string]string{"goods_receipt_id": receiptID})), errBoom)
	})
}

func TestPayableUseCase_ListPayables(t *testing.T) {
	t.Parallel()

	t.Run("lists a page with the age of each", func(t *testing.T) {
		t.Parallel()

		uc, d := newPayableUseCase(t)
		d.timezone()
		d.Repo.EXPECT().ListPayables(mock.Anything, garageID, usecase.PayableFilter{SupplierID: supplierID, IncludePaid: true, Limit: 20, Offset: 0}).
			Return([]*domain.Payable{openPayable(payableID, supplierID, 700_000, 0)}, 1, usecase.PayableSummary{OpenCount: 1, OutstandingAmount: 700_000}, nil).Once()

		res, err := uc.ListPayables(t.Context(), usecase.ListPayablesRequest{GarageID: garageID, SupplierID: supplierID, IncludePaid: true})
		require.NoError(t, err)
		require.Equal(t, 1, res.Page)
		require.Equal(t, 20, res.PageSize)
		require.Equal(t, 50, res.Items[0].AgeDays) // 1 October to 20 November
		require.EqualValues(t, 700_000, res.Summary.OutstandingAmount)
	})

	t.Run("refuses a supplier that is not an id", func(t *testing.T) {
		t.Parallel()

		uc, _ := newPayableUseCase(t)
		_, err := uc.ListPayables(t.Context(), usecase.ListPayablesRequest{GarageID: garageID, SupplierID: "x"})
		assertAppError(t, err, apperror.CodeInvalidArgument)
	})

	t.Run("passes on the failures of the timezone and of the repository", func(t *testing.T) {
		t.Parallel()

		uc, d := newPayableUseCase(t)
		d.Garages.EXPECT().Timezone(mock.Anything, garageID).Return("", errBoom).Once()

		_, err := uc.ListPayables(t.Context(), usecase.ListPayablesRequest{GarageID: garageID})
		assertAppError(t, err, apperror.CodeInternal)

		uc, d = newPayableUseCase(t)
		d.timezone()
		d.Repo.EXPECT().ListPayables(mock.Anything, garageID, mock.Anything).Return(nil, 0, usecase.PayableSummary{}, errBoom).Once()

		_, err = uc.ListPayables(t.Context(), usecase.ListPayablesRequest{GarageID: garageID})
		assertAppError(t, err, apperror.CodeInternal)
	})
}

func TestPayableUseCase_Aging(t *testing.T) {
	t.Parallel()

	t.Run("totals the suppliers", func(t *testing.T) {
		t.Parallel()

		uc, d := newPayableUseCase(t)
		d.timezone()
		d.Repo.EXPECT().Aging(mock.Anything, garageID, day(2026, 11, 20)).Return([]domain.Aging{
			{SupplierID: supplierID, SupplierName: "Sao Mai", Total: 1_000, Current: 100, Days31to60: 200, Days61to90: 300, Over90: 400},
			{SupplierID: otherSupp, SupplierName: "Bao An", Total: 50, Current: 50},
		}, nil).Once()

		res, err := uc.Aging(t.Context(), garageID)
		require.NoError(t, err)
		require.Len(t, res.Suppliers, 2)
		require.Equal(t, domain.Aging{Total: 1_050, Current: 150, Days31to60: 200, Days61to90: 300, Over90: 400}, res.Totals)
	})

	t.Run("passes on failures", func(t *testing.T) {
		t.Parallel()

		uc, d := newPayableUseCase(t)
		d.Garages.EXPECT().Timezone(mock.Anything, garageID).Return("", errBoom).Once()

		_, err := uc.Aging(t.Context(), garageID)
		assertAppError(t, err, apperror.CodeInternal)

		uc, d = newPayableUseCase(t)
		d.timezone()
		d.Repo.EXPECT().Aging(mock.Anything, garageID, mock.Anything).Return(nil, errBoom).Once()

		_, err = uc.Aging(t.Context(), garageID)
		assertAppError(t, err, apperror.CodeInternal)
	})
}
