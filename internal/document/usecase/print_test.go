package usecase_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/document/printmodel"
	"github.com/viettechno/vietgara-backend/internal/document/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// expectQuote makes the readers answer for a quote any number of times.
func expectQuote(d *deps) {
	d.Garages.EXPECT().Get(mock.Anything, garageID).Return(defaultGarage(), nil).Maybe()
	d.Quotes.EXPECT().Get(mock.Anything, garageID, quoteID).Return(usecase.QuoteInfo{
		Number: "BG-1", Status: "approved", CustomerName: "An Nguyen", VehiclePlate: "51F-123.45",
		Lines: []printmodel.DocumentLine{{Description: "Oil change", Quantity: "1", UnitPrice: 250_000, LineTotal: 250_000}}, TotalAmount: 250_000,
	}, nil).Maybe()
}

func TestPrintSettings(t *testing.T) {
	t.Parallel()

	t.Run("defaults for a garage that never chose", func(t *testing.T) {
		t.Parallel()

		uc, _ := newUseCase(t)

		infos, err := uc.PrintSettings(t.Context(), garageID)
		require.NoError(t, err)
		require.Len(t, infos, len(printmodel.Types()))

		for _, info := range infos {
			assert.Equal(t, printmodel.A4, info.Paper, info.Type)
			assert.False(t, info.CopyMark, info.Type)
		}

		assert.Equal(t, []printmodel.Paper{printmodel.A4, printmodel.A5}, infos[0].Available, "a quote offers both sizes")
		assert.Equal(t, []printmodel.Paper{printmodel.A4}, infos[1].Available, "a repair order is A4 only")
	})

	t.Run("a saved choice is returned", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Prints.settings = map[printmodel.DocumentType]usecase.PrintSetting{printmodel.TypeQuote: {Paper: printmodel.A5, CopyMark: true}}

		infos, err := uc.PrintSettings(t.Context(), garageID)
		require.NoError(t, err)
		assert.Equal(t, printmodel.A5, infos[0].Paper)
		assert.True(t, infos[0].CopyMark)
	})

	t.Run("store failure", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Prints.err = errBoom

		_, err := uc.PrintSettings(t.Context(), garageID)
		assertAppError(t, err, apperror.CodeInternal)
	})
}

func TestUpdatePrintSetting(t *testing.T) {
	t.Parallel()

	t.Run("saves a size the document offers", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)

		info, err := uc.UpdatePrintSetting(t.Context(), garageID, printmodel.TypePaymentVoucher, printmodel.A5, true)
		require.NoError(t, err)
		assert.Equal(t, printmodel.A5, info.Paper)
		assert.True(t, info.CopyMark)
		assert.Equal(t, usecase.PrintSetting{Paper: printmodel.A5, CopyMark: true}, d.Prints.settings[printmodel.TypePaymentVoucher])
	})

	t.Run("refuses what has no template", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)

		_, err := uc.UpdatePrintSetting(t.Context(), garageID, printmodel.TypeRepairOrder, printmodel.A5, false)
		assertAppError(t, err, apperror.CodeInvalidArgument)

		_, err = uc.UpdatePrintSetting(t.Context(), garageID, printmodel.DocumentType("invoice"), printmodel.A4, false)
		assertAppError(t, err, apperror.CodeInvalidArgument)

		_, err = uc.UpdatePrintSetting(t.Context(), garageID, printmodel.TypeQuote, printmodel.Paper("A3"), false)
		assertAppError(t, err, apperror.CodeInvalidArgument)

		assert.Empty(t, d.Prints.settings, "nothing was saved")
	})

	t.Run("store failure", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Prints.err = errBoom

		_, err := uc.UpdatePrintSetting(t.Context(), garageID, printmodel.TypeQuote, printmodel.A4, false)
		assertAppError(t, err, apperror.CodeInternal)
	})
}

func TestPrintLogAndCopyMark(t *testing.T) {
	t.Parallel()

	t.Run("an export is not logged", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		expectQuote(d)

		_, err := uc.QuotePDF(t.Context(), garageID, quoteID, false)
		require.NoError(t, err)
		assert.Empty(t, d.Prints.log)
	})

	t.Run("the first print is logged without a copy mark", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		expectQuote(d)
		d.Prints.settings = map[printmodel.DocumentType]usecase.PrintSetting{printmodel.TypeQuote: {Paper: printmodel.A4, CopyMark: true}}

		_, err := uc.QuotePDF(t.Context(), garageID, quoteID, true)
		require.NoError(t, err)
		assert.Equal(t, []printRecord{{Type: printmodel.TypeQuote, DocumentID: quoteID, Paper: printmodel.A4, Version: 1}}, d.Prints.log)
	})

	t.Run("a reprint carries the mark when the garage wants it", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		expectQuote(d)
		d.Prints.settings = map[printmodel.DocumentType]usecase.PrintSetting{printmodel.TypeQuote: {Paper: printmodel.A4, CopyMark: true}}

		for range 2 {
			_, err := uc.QuotePDF(t.Context(), garageID, quoteID, true)
			require.NoError(t, err)
		}

		require.Len(t, d.Prints.log, 2)
		assert.False(t, d.Prints.log[0].Copy)
		assert.True(t, d.Prints.log[1].Copy)
	})

	t.Run("a reprint has no mark when the garage did not turn it on", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		expectQuote(d)

		for range 2 {
			_, err := uc.QuotePDF(t.Context(), garageID, quoteID, true)
			require.NoError(t, err)
		}

		require.Len(t, d.Prints.log, 2)
		assert.False(t, d.Prints.log[1].Copy)
	})

	t.Run("an export after a print has no mark and adds nothing", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		expectQuote(d)
		d.Prints.settings = map[printmodel.DocumentType]usecase.PrintSetting{printmodel.TypeQuote: {Paper: printmodel.A4, CopyMark: true}}

		_, err := uc.QuotePDF(t.Context(), garageID, quoteID, true)
		require.NoError(t, err)
		_, err = uc.QuotePDF(t.Context(), garageID, quoteID, false)
		require.NoError(t, err)
		assert.Len(t, d.Prints.log, 1)
	})
}

func TestPrintUsesTheChosenPaper(t *testing.T) {
	t.Parallel()

	uc, d := newUseCase(t)
	expectQuote(d)
	d.Prints.settings = map[printmodel.DocumentType]usecase.PrintSetting{printmodel.TypeQuote: {Paper: printmodel.A5}}

	pdf, err := uc.QuotePDF(t.Context(), garageID, quoteID, true)
	require.NoError(t, err)
	assert.Contains(t, string(pdf), "/MediaBox [0 0 420.94 595.28]", "A5")
	require.Len(t, d.Prints.log, 1)
	assert.Equal(t, printmodel.A5, d.Prints.log[0].Paper)

	// A setting that names a size the document no longer offers falls back to A4.
	d.Prints.settings[printmodel.TypeQuote] = usecase.PrintSetting{Paper: printmodel.Paper("A3")}

	pdf, err = uc.QuotePDF(t.Context(), garageID, quoteID, false)
	require.NoError(t, err)
	assert.Contains(t, string(pdf), "/MediaBox [0 0 595.28 841.89]", "A4")
}

func TestReprintKeepsTheFirstPrintsVersion(t *testing.T) {
	t.Parallel()

	t.Run("the version of the first print on that paper is used", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		expectQuote(d)
		d.Prints.log = []printRecord{{Type: printmodel.TypeQuote, DocumentID: quoteID, Paper: printmodel.A4, Version: 1}}

		_, err := uc.QuotePDF(t.Context(), garageID, quoteID, true)
		require.NoError(t, err)
		require.Len(t, d.Prints.log, 2)
		assert.Equal(t, 1, d.Prints.log[1].Version)
	})

	t.Run("a version that left the code is an internal error", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		expectQuote(d)
		d.Prints.log = []printRecord{{Type: printmodel.TypeQuote, DocumentID: quoteID, Paper: printmodel.A4, Version: 9}}

		_, err := uc.QuotePDF(t.Context(), garageID, quoteID, true)
		assertAppError(t, err, apperror.CodeInternal)
		assert.Len(t, d.Prints.log, 1, "nothing was logged")
	})
}

func TestPrintStoreFailures(t *testing.T) {
	t.Parallel()

	for name, fail := range map[string]func(*fakePrints){
		"settings": func(f *fakePrints) { f.err = errBoom },
		"state":    func(f *fakePrints) { f.stateErr = errBoom },
		"log":      func(f *fakePrints) { f.recordErr = errBoom },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			uc, d := newUseCase(t)
			expectQuote(d)
			fail(d.Prints)

			_, err := uc.QuotePDF(t.Context(), garageID, quoteID, true)
			assertAppError(t, err, apperror.CodeInternal)
		})
	}
}
