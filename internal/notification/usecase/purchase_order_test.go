package usecase_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/notification/domain"
	"github.com/viettechno/vietgara-backend/internal/notification/usecase"
	"github.com/viettechno/vietgara-backend/internal/platform/mail"
)

func TestPurchaseOrder(t *testing.T) {
	t.Parallel()

	req := usecase.PurchaseOrderRequest{
		GarageID: garageID, OrderID: "po-1", Number: "DH-2026-00001", SupplierName: "Sao Mai", To: "sao@mai.test", Message: "Giao trước thứ sáu",
	}

	t.Run("e-mails the supplier the order as a PDF under the garage's name and logs it", func(t *testing.T) {
		t.Parallel()

		uc, d := newGarageUseCase(t)
		d.Receipts.EXPECT().PurchaseOrder(mock.Anything, garageID, "po-1").Return([]byte("%PDF-order"), nil).Once()
		d.garageReads(domain.Settings{})
		d.notSuppressed("sao@mai.test")
		msg := d.sentMessage()
		rows := d.logged()

		sent, err := uc.PurchaseOrder(t.Context(), req)
		require.NoError(t, err)
		assert.True(t, sent)
		assert.Equal(t, "sao@mai.test", msg.ToEmail)
		assert.Equal(t, "gara@example.com", msg.ReplyTo)
		assert.Equal(t, "Gara Anh Minh", msg.FromName)
		assert.Contains(t, msg.Subject, "DH-2026-00001")
		assert.Contains(t, msg.Text, "Giao trước thứ sáu")
		assert.NotContains(t, msg.Text, "opt-out", "a supplier is not a customer: no opt-out link")
		require.Len(t, msg.Attachments, 1)
		assert.Equal(t, "purchase-order-DH-2026-00001.pdf", msg.Attachments[0].Name)
		assert.Equal(t, []byte("%PDF-order"), msg.Attachments[0].Data)
		require.Len(t, *rows, 1)
		assert.Equal(t, domain.TypePurchaseOrder, (*rows)[0].Type)
		assert.Equal(t, "purchase_order", (*rows)[0].EntityType)
		assert.Equal(t, "po-1", (*rows)[0].EntityID)
		assert.Empty(t, (*rows)[0].CustomerID)
	})

	t.Run("an address on the suppression list is not mailed", func(t *testing.T) {
		t.Parallel()

		uc, d := newGarageUseCase(t)
		d.Receipts.EXPECT().PurchaseOrder(mock.Anything, garageID, "po-1").Return([]byte("%PDF"), nil).Once()
		d.garageReads(domain.Settings{})
		d.Suppressions.EXPECT().Suppressed(mock.Anything, "sao@mai.test").Return(true, nil).Once()
		rows := d.logged()

		sent, err := uc.PurchaseOrder(t.Context(), req)
		require.NoError(t, err)
		assert.False(t, sent)
		require.Len(t, *rows, 1)
		assert.Equal(t, domain.StatusSkipped, (*rows)[0].Status)
	})

	t.Run("a garage cannot switch it off", func(t *testing.T) {
		t.Parallel()

		uc, d := newGarageUseCase(t)
		d.Receipts.EXPECT().PurchaseOrder(mock.Anything, garageID, "po-1").Return([]byte("%PDF"), nil).Once()
		d.garageReads(domain.Settings{Off: map[domain.Type]bool{domain.TypePurchaseOrder: true}})
		d.notSuppressed("sao@mai.test")
		d.sentMessage()
		d.logged()

		sent, err := uc.PurchaseOrder(t.Context(), req)
		require.NoError(t, err)
		assert.True(t, sent, "the switch of a type applies to settings; this one is not a setting")
	})

	t.Run("failures", func(t *testing.T) {
		t.Parallel()

		uc, d := newGarageUseCase(t)
		d.Receipts.EXPECT().PurchaseOrder(mock.Anything, garageID, "po-1").Return(nil, errBoom2).Once()

		_, err := uc.PurchaseOrder(t.Context(), req)
		require.ErrorIs(t, err, errBoom2)

		uc, d = newGarageUseCase(t)
		d.Receipts.EXPECT().PurchaseOrder(mock.Anything, garageID, "po-1").Return(make([]byte, mail.MaxAttachmentBytes+1), nil).Once()

		_, err = uc.PurchaseOrder(t.Context(), req)
		require.ErrorIs(t, err, usecase.ErrAttachmentTooBig)

		uc, d = newGarageUseCase(t)
		d.Receipts.EXPECT().PurchaseOrder(mock.Anything, garageID, "po-1").Return([]byte("%PDF"), nil).Once()
		d.Settings.EXPECT().Get(mock.Anything, garageID).Return(domain.Settings{}, errBoom2).Once()

		_, err = uc.PurchaseOrder(t.Context(), req)
		require.ErrorIs(t, err, errBoom2)

		uc, d = newGarageUseCase(t)
		d.Receipts.EXPECT().PurchaseOrder(mock.Anything, garageID, "po-1").Return([]byte("%PDF"), nil).Once()
		d.garageReads(domain.Settings{})
		d.notSuppressed("sao@mai.test")
		d.Mailer.EXPECT().Send(mock.Anything, mock.Anything).Return(errBoom2).Once()
		d.Log.EXPECT().Failed(mock.Anything, mock.Anything).Return(nil).Once()

		sent, err := uc.PurchaseOrder(t.Context(), req)
		require.ErrorIs(t, err, errBoom2)
		assert.False(t, sent)
	})
}
