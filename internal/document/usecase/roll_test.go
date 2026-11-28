package usecase_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/document/printmodel"
	"github.com/viettechno/vietgara-backend/internal/document/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

const paymentID = "66666666-6666-6666-6666-666666666666"

func expectVoucher(d *deps) {
	d.Garages.EXPECT().Get(mock.Anything, garageID).Return(defaultGarage(), nil).Maybe()
	d.Settlements.EXPECT().Get(mock.Anything, garageID, settlementID).Return(usecase.SettlementInfo{
		Number: "QT-1", Vouchers: []usecase.VoucherInfo{{ID: paymentID, Number: "PT-1", Date: "26/11/2026", PayerName: "An", Amount: 200_000, Method: "Tiền mặt"}},
	}, nil).Maybe()
}

func TestQuoteEscPos(t *testing.T) {
	t.Parallel()

	t.Run("a first print and a reprint are logged on the roll, the reprint with the copy mark", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		expectQuote(d)
		d.Prints.settings = map[printmodel.DocumentType]usecase.PrintSetting{printmodel.TypeQuote: {Paper: printmodel.A5, CopyMark: true}}

		first, err := uc.QuoteEscPos(t.Context(), garageID, quoteID, printmodel.K80)
		require.NoError(t, err)
		assert.True(t, bytes.HasPrefix(first, []byte{0x1b, 0x40}))
		assert.True(t, bytes.HasSuffix(first, []byte{0x1d, 0x56, 0x42, 0x00}), "ends with the cut")

		again, err := uc.QuoteEscPos(t.Context(), garageID, quoteID, printmodel.K80)
		require.NoError(t, err)
		assert.NotEqual(t, first, again, "the reprint carries the copy mark")

		assert.Equal(t, []printRecord{
			{Type: printmodel.TypeQuote, DocumentID: quoteID, Paper: printmodel.K80, Version: 1},
			{Type: printmodel.TypeQuote, DocumentID: quoteID, Paper: printmodel.K80, Version: 1, Copy: true},
		}, d.Prints.log)
	})

	t.Run("a K57 roll is narrower", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		expectQuote(d)

		data, err := uc.QuoteEscPos(t.Context(), garageID, quoteID, printmodel.K57)
		require.NoError(t, err)
		// GS v 0 m xL xH: 48 bytes a row on K57.
		assert.Equal(t, []byte{0x1d, 0x76, 0x30, 0x00, 48, 0}, data[2:8])
	})

	for _, paper := range []printmodel.Paper{"", printmodel.A4, printmodel.A5, printmodel.Label5030} {
		t.Run("refuses "+string(paper), func(t *testing.T) {
			t.Parallel()

			uc, d := newUseCase(t)
			expectQuote(d)

			_, err := uc.QuoteEscPos(t.Context(), garageID, quoteID, paper)
			assertAppError(t, err, apperror.CodeInvalidArgument)
			assert.Empty(t, d.Prints.log)
		})
	}

	t.Run("a quote not found", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Quotes.EXPECT().Get(mock.Anything, garageID, quoteID).Return(usecase.QuoteInfo{}, apperror.New(apperror.CodeNotFound, "quote not found"))

		_, err := uc.QuoteEscPos(t.Context(), garageID, quoteID, printmodel.K80)
		assertAppError(t, err, apperror.CodeNotFound)
	})
}

func TestPaymentVoucherEscPos(t *testing.T) {
	t.Parallel()

	uc, d := newUseCase(t)
	expectVoucher(d)

	_, err := uc.PaymentVoucherEscPos(t.Context(), garageID, settlementID, paymentID, printmodel.K57)
	require.NoError(t, err)
	assert.Equal(t, []printRecord{{Type: printmodel.TypePaymentVoucher, DocumentID: paymentID, Paper: printmodel.K57, Version: 1}}, d.Prints.log)

	_, err = uc.PaymentVoucherEscPos(t.Context(), garageID, settlementID, "other", printmodel.K57)
	assertAppError(t, err, apperror.CodeNotFound)

	_, err = uc.PaymentVoucherEscPos(t.Context(), garageID, settlementID, paymentID, printmodel.A4)
	assertAppError(t, err, apperror.CodeInvalidArgument)
}

func TestUpdatePrintSettingRefusesRolls(t *testing.T) {
	t.Parallel()

	uc, _ := newUseCase(t)

	for _, paper := range []printmodel.Paper{printmodel.K80, printmodel.K57, printmodel.Label5030} {
		_, err := uc.UpdatePrintSetting(t.Context(), garageID, printmodel.TypePaymentVoucher, paper, false)
		assertAppError(t, err, apperror.CodeInvalidArgument)
	}
}

func TestPrintOnThePrintersPageSize(t *testing.T) {
	t.Parallel()

	t.Run("an A5 printer prints the A5 template, pinned on A5", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		expectQuote(d)

		_, err := uc.QuotePDF(t.Context(), garageID, quoteID, usecase.PrintOn(printmodel.A5))
		require.NoError(t, err)
		_, err = uc.QuotePDF(t.Context(), garageID, quoteID, usecase.PrintOn(""))
		require.NoError(t, err)

		assert.Equal(t, []printRecord{
			{Type: printmodel.TypeQuote, DocumentID: quoteID, Paper: printmodel.A5, Version: 1},
			{Type: printmodel.TypeQuote, DocumentID: quoteID, Paper: printmodel.A4, Version: 1},
		}, d.Prints.log, "an empty size is the garage's setting (A4 by default)")
	})

	for _, paper := range []printmodel.Paper{printmodel.K80, printmodel.Label5030, "B5"} {
		t.Run("refuses "+string(paper), func(t *testing.T) {
			t.Parallel()

			uc, d := newUseCase(t)
			expectQuote(d)

			_, err := uc.QuotePDF(t.Context(), garageID, quoteID, usecase.PrintOn(paper))
			assertAppError(t, err, apperror.CodeInvalidArgument)
			assert.Empty(t, d.Prints.log)
		})
	}

	t.Run("refuses A5 for a document without an A5 template", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(defaultGarage(), nil).Maybe()
		d.RepairOrders.EXPECT().Get(mock.Anything, garageID, repairOrderID).Return(usecase.RepairOrderInfo{Number: "LSC-1"}, nil)

		_, err := uc.RepairOrderPDF(t.Context(), garageID, repairOrderID, usecase.PrintOn(printmodel.A5))
		assertAppError(t, err, apperror.CodeInvalidArgument)
	})
}
