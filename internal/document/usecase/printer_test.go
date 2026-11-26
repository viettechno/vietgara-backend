package usecase_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/document/printmodel"
	"github.com/viettechno/vietgara-backend/internal/document/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// fakePrinters is an in-memory usecase.PrinterStore.
type fakePrinters struct {
	printers []usecase.Printer
	err      error
	locked   bool
}

func (f *fakePrinters) ListPrinters(context.Context, string) ([]usecase.Printer, error) {
	return f.printers, f.err
}

func (f *fakePrinters) GetPrinter(_ context.Context, _, id string) (usecase.Printer, error) {
	for _, p := range f.printers {
		if p.ID == id {
			return p, f.err
		}
	}

	return usecase.Printer{}, usecase.ErrPrinterNotFound
}

func (f *fakePrinters) LockPrinters(context.Context, string) error {
	f.locked = true

	return nil
}

func (f *fakePrinters) CountPrinters(context.Context, string) (int, error) {
	return len(f.printers), f.err
}

func (f *fakePrinters) CreatePrinter(_ context.Context, _ string, p usecase.Printer) (usecase.Printer, error) {
	if f.err != nil {
		return usecase.Printer{}, f.err
	}

	for _, existing := range f.printers {
		if strings.EqualFold(existing.Name, p.Name) {
			return usecase.Printer{}, usecase.ErrPrinterNameTaken
		}
	}

	p.ID = "p-new"
	f.printers = append(f.printers, p)

	return p, nil
}

func (f *fakePrinters) UpdatePrinter(_ context.Context, _ string, p usecase.Printer) (usecase.Printer, error) {
	for i, existing := range f.printers {
		if existing.ID == p.ID {
			f.printers[i] = p

			return p, nil
		}
	}

	return usecase.Printer{}, usecase.ErrPrinterNotFound
}

func (f *fakePrinters) DeletePrinter(_ context.Context, _, id string) error {
	for i, existing := range f.printers {
		if existing.ID == id {
			f.printers = append(f.printers[:i], f.printers[i+1:]...)

			return nil
		}
	}

	return usecase.ErrPrinterNotFound
}

// fakeParts answers every part with a label, or err.
type fakeParts struct {
	err   error
	asked []string
}

func (f *fakeParts) PartLabel(_ context.Context, _, partID string) (printmodel.PartLabel, error) {
	f.asked = append(f.asked, partID)

	return printmodel.PartLabel{Name: "Lọc dầu " + partID, SKU: "SKU-" + partID, Barcode: "893" + partID}, f.err
}

func newPrintingUseCase(t *testing.T) (*usecase.DocumentUseCase, *deps, *fakePrinters, *fakeParts) {
	t.Helper()

	uc, d := newUseCase(t)
	printers, parts := &fakePrinters{}, &fakeParts{}
	uc.WithPrinting(printers, parts)

	return uc, d, printers, parts
}

func rawK80() usecase.Printer {
	return usecase.Printer{
		Name: "  Quầy thu ngân  ", Protocol: usecase.ProtocolRaw, Host: "192.168.1.50", Paper: printmodel.K80,
		DocumentTypes: []printmodel.DocumentType{printmodel.TypePaymentVoucher, printmodel.TypeQuote, printmodel.TypeQuote},
	}
}

func TestCreatePrinter(t *testing.T) {
	t.Parallel()

	t.Run("a raw printer gets the default port, trimmed name and ordered types", func(t *testing.T) {
		t.Parallel()

		uc, _, printers, _ := newPrintingUseCase(t)

		p, err := uc.CreatePrinter(t.Context(), garageID, rawK80())
		require.NoError(t, err)
		assert.Equal(t, "Quầy thu ngân", p.Name)
		assert.Equal(t, 9100, p.Port)
		assert.Empty(t, p.Path)
		assert.Equal(t, []printmodel.DocumentType{printmodel.TypeQuote, printmodel.TypePaymentVoucher}, p.DocumentTypes)
		assert.True(t, printers.locked, "the count is taken under the lock")
	})

	t.Run("an IPP printer gets port 631 and the default path", func(t *testing.T) {
		t.Parallel()

		uc, _, _, _ := newPrintingUseCase(t)

		p, err := uc.CreatePrinter(t.Context(), garageID, usecase.Printer{
			Name: "Văn phòng", Protocol: usecase.ProtocolIPP, Host: "EPSON-L3250.local.", Path: "/",
			DocumentTypes: []printmodel.DocumentType{printmodel.TypeRepairOrder},
		})
		require.NoError(t, err)
		assert.Equal(t, 631, p.Port)
		assert.Equal(t, "ipp/print", p.Path)
	})

	t.Run("a label printer and an IPv6 address", func(t *testing.T) {
		t.Parallel()

		uc, _, _, _ := newPrintingUseCase(t)

		p, err := uc.CreatePrinter(t.Context(), garageID, usecase.Printer{
			Name: "Tem", Protocol: usecase.ProtocolRaw, Host: "fe80::1", Port: 9101, Paper: printmodel.Label5030,
			DocumentTypes: []printmodel.DocumentType{printmodel.TypePartLabel},
		})
		require.NoError(t, err)
		assert.Equal(t, 9101, p.Port)
	})

	invalid := map[string]func(*usecase.Printer){
		"no name":                      func(p *usecase.Printer) { p.Name = " " },
		"a long name":                  func(p *usecase.Printer) { p.Name = strings.Repeat("x", 61) },
		"no host":                      func(p *usecase.Printer) { p.Host = "" },
		"a host with a scheme":         func(p *usecase.Printer) { p.Host = "http://10.0.0.1" },
		"a host with a port":           func(p *usecase.Printer) { p.Host = "10.0.0.1:9100" },
		"a port out of range":          func(p *usecase.Printer) { p.Port = 70000 },
		"a negative port":              func(p *usecase.Printer) { p.Port = -1 },
		"no protocol":                  func(p *usecase.Printer) { p.Protocol = "" },
		"a raw printer with a path":    func(p *usecase.Printer) { p.Path = "ipp/print" },
		"a raw printer on A4":          func(p *usecase.Printer) { p.Paper = printmodel.A4 },
		"a raw printer with no paper":  func(p *usecase.Printer) { p.Paper = "" },
		"a roll for a repair order":    func(p *usecase.Printer) { p.DocumentTypes = []printmodel.DocumentType{printmodel.TypeRepairOrder} },
		"a roll for labels":            func(p *usecase.Printer) { p.DocumentTypes = []printmodel.DocumentType{printmodel.TypePartLabel} },
		"an IPP printer with paper":    func(p *usecase.Printer) { p.Protocol = usecase.ProtocolIPP },
		"an IPP printer for labels":    func(p *usecase.Printer) { *p = ippWith(printmodel.TypePartLabel) },
		"an IPP path with a query":     func(p *usecase.Printer) { *p = ippWith(); p.Path = "ipp/print?x=1" },
		"an IPP path that is too long": func(p *usecase.Printer) { *p = ippWith(); p.Path = strings.Repeat("a", 201) },
		"a label printer for vouchers": func(p *usecase.Printer) { p.Paper = printmodel.Label5030 },
	}

	for name, change := range invalid {
		t.Run("refuses "+name, func(t *testing.T) {
			t.Parallel()

			uc, _, printers, _ := newPrintingUseCase(t)
			p := rawK80()
			change(&p)

			_, err := uc.CreatePrinter(t.Context(), garageID, p)
			assertAppError(t, err, apperror.CodeInvalidArgument)
			assert.Empty(t, printers.printers)
		})
	}

	t.Run("refuses a 21st printer", func(t *testing.T) {
		t.Parallel()

		uc, _, printers, _ := newPrintingUseCase(t)
		printers.printers = make([]usecase.Printer, usecase.MaxPrinters)

		_, err := uc.CreatePrinter(t.Context(), garageID, rawK80())
		assertAppError(t, err, apperror.CodeInvalidArgument)
	})

	t.Run("refuses a name taken", func(t *testing.T) {
		t.Parallel()

		uc, _, printers, _ := newPrintingUseCase(t)
		printers.printers = []usecase.Printer{{ID: "p-1", Name: "QUẦY THU NGÂN"}}

		_, err := uc.CreatePrinter(t.Context(), garageID, rawK80())
		assertAppError(t, err, apperror.CodeInvalidArgument)
	})

	t.Run("store failure", func(t *testing.T) {
		t.Parallel()

		uc, _, printers, _ := newPrintingUseCase(t)
		printers.err = errBoom

		_, err := uc.CreatePrinter(t.Context(), garageID, rawK80())
		assertAppError(t, err, apperror.CodeInternal)
	})
}

func ippWith(types ...printmodel.DocumentType) usecase.Printer {
	return usecase.Printer{Name: "A4", Protocol: usecase.ProtocolIPP, Host: "10.0.0.2", DocumentTypes: types}
}

func TestUpdateListDeletePrinter(t *testing.T) {
	t.Parallel()

	uc, _, printers, _ := newPrintingUseCase(t)
	printers.printers = []usecase.Printer{{ID: "p-1", Name: "Old", Protocol: usecase.ProtocolRaw, Host: "10.0.0.1", Port: 9100, Paper: printmodel.K57}}

	p := rawK80()
	p.ID = "p-1"

	updated, err := uc.UpdatePrinter(t.Context(), garageID, p)
	require.NoError(t, err)
	assert.Equal(t, printmodel.K80, updated.Paper)

	list, err := uc.Printers(t.Context(), garageID)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "Quầy thu ngân", list[0].Name)

	p.ID = "p-404"
	_, err = uc.UpdatePrinter(t.Context(), garageID, p)
	assertAppError(t, err, apperror.CodeNotFound)

	_, err = uc.UpdatePrinter(t.Context(), garageID, usecase.Printer{ID: "p-1"})
	assertAppError(t, err, apperror.CodeInvalidArgument)

	require.NoError(t, uc.DeletePrinter(t.Context(), garageID, "p-1"))
	assertAppError(t, uc.DeletePrinter(t.Context(), garageID, "p-1"), apperror.CodeNotFound)

	printers.err = errBoom
	_, err = uc.Printers(t.Context(), garageID)
	assertAppError(t, err, apperror.CodeInternal)
}

func TestPrinterTestPage(t *testing.T) {
	t.Parallel()

	uc, d, printers, _ := newPrintingUseCase(t)
	printers.printers = []usecase.Printer{
		{ID: "raw", Name: "Quầy", Protocol: usecase.ProtocolRaw, Paper: printmodel.K57},
		{ID: "label", Name: "Tem", Protocol: usecase.ProtocolRaw, Paper: printmodel.Label5030},
		{ID: "ipp", Name: "A4", Protocol: usecase.ProtocolIPP},
	}

	d.Garages.EXPECT().Get(mock.Anything, garageID).Return(defaultGarage(), nil)

	data, contentType, err := uc.PrinterTestPage(t.Context(), garageID, "raw")
	require.NoError(t, err)
	assert.Equal(t, usecase.ContentTypeEscPos, contentType)
	assert.True(t, bytes.HasPrefix(data, []byte{0x1b, 0x40}), "starts with ESC @")

	data, _, err = uc.PrinterTestPage(t.Context(), garageID, "label")
	require.NoError(t, err)
	assert.True(t, bytes.HasSuffix(data, []byte{0x1d, 0x0c}), "ends at the next label")

	data, contentType, err = uc.PrinterTestPage(t.Context(), garageID, "ipp")
	require.NoError(t, err)
	assert.Equal(t, usecase.ContentTypePDF, contentType)
	assert.True(t, bytes.HasPrefix(data, []byte("%PDF")))

	_, _, err = uc.PrinterTestPage(t.Context(), garageID, "missing")
	assertAppError(t, err, apperror.CodeNotFound)
}

func TestPrinterTestPageGarageFailure(t *testing.T) {
	t.Parallel()

	uc, d, printers, _ := newPrintingUseCase(t)
	printers.printers = []usecase.Printer{{ID: "raw", Protocol: usecase.ProtocolRaw, Paper: printmodel.K80}}

	d.Garages.EXPECT().Get(mock.Anything, garageID).Return(usecase.GarageHeaderInfo{}, errBoom)

	_, _, err := uc.PrinterTestPage(t.Context(), garageID, "raw")
	assertAppError(t, err, apperror.CodeInternal)
}

func TestPartLabels(t *testing.T) {
	t.Parallel()

	t.Run("prints the copies of each part in order", func(t *testing.T) {
		t.Parallel()

		uc, _, _, parts := newPrintingUseCase(t)

		data, err := uc.PartLabels(t.Context(), garageID, printmodel.Label5030, []usecase.LabelCopies{{PartID: "a", Copies: 2}, {PartID: "b", Copies: 1}})
		require.NoError(t, err)
		assert.Equal(t, 3, bytes.Count(data, []byte{0x1d, 0x0c}), "one feed per label")
		assert.Equal(t, []string{"a", "b"}, parts.asked)
	})

	invalid := map[string]struct {
		paper printmodel.Paper
		parts []usecase.LabelCopies
	}{
		"on a roll":        {printmodel.K80, []usecase.LabelCopies{{PartID: "a", Copies: 1}}},
		"no part":          {printmodel.Label5030, nil},
		"a part twice":     {printmodel.Label5030, []usecase.LabelCopies{{PartID: "a", Copies: 1}, {PartID: "a", Copies: 1}}},
		"no copies":        {printmodel.Label5030, []usecase.LabelCopies{{PartID: "a"}}},
		"too many copies":  {printmodel.Label5030, []usecase.LabelCopies{{PartID: "a", Copies: 101}}},
		"too many labels":  {printmodel.Label5030, manyLabels(4, 100)},
		"too many parts":   {printmodel.Label5030, manyLabels(51, 1)},
		"no paper at all":  {"", []usecase.LabelCopies{{PartID: "a", Copies: 1}}},
		"the A4 page size": {printmodel.A4, []usecase.LabelCopies{{PartID: "a", Copies: 1}}},
	}

	for name, tc := range invalid {
		t.Run("refuses "+name, func(t *testing.T) {
			t.Parallel()

			uc, _, _, parts := newPrintingUseCase(t)

			_, err := uc.PartLabels(t.Context(), garageID, tc.paper, tc.parts)
			assertAppError(t, err, apperror.CodeInvalidArgument)
			assert.Empty(t, parts.asked)
		})
	}

	t.Run("a part not found", func(t *testing.T) {
		t.Parallel()

		uc, _, _, parts := newPrintingUseCase(t)
		parts.err = apperror.New(apperror.CodeNotFound, "part not found")

		_, err := uc.PartLabels(t.Context(), garageID, printmodel.Label5030, []usecase.LabelCopies{{PartID: "a", Copies: 1}})
		assertAppError(t, err, apperror.CodeNotFound)
	})
}

func manyLabels(parts, copies int) []usecase.LabelCopies {
	out := make([]usecase.LabelCopies, parts)
	for i := range out {
		out[i] = usecase.LabelCopies{PartID: string(rune('A' + i)), Copies: copies}
	}

	return out
}
