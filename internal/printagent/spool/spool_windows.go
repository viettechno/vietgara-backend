//go:build windows

package spool

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/viettechno/vietgara-backend/internal/printagent/protocol"
)

var (
	winspool               = windows.NewLazySystemDLL("winspool.drv")
	procEnumPrintersW      = winspool.NewProc("EnumPrintersW")
	procGetDefaultPrinterW = winspool.NewProc("GetDefaultPrinterW")
	procOpenPrinterW       = winspool.NewProc("OpenPrinterW")
	procClosePrinter       = winspool.NewProc("ClosePrinter")
	procStartDocPrinterW   = winspool.NewProc("StartDocPrinterW")
	procEndDocPrinter      = winspool.NewProc("EndDocPrinter")
	procStartPagePrinter   = winspool.NewProc("StartPagePrinter")
	procEndPagePrinter     = winspool.NewProc("EndPagePrinter")
	procWritePrinter       = winspool.NewProc("WritePrinter")
)

const (
	printerEnumLocal       = 0x2
	printerEnumConnections = 0x4
)

// printerInfo4 is PRINTER_INFO_4W.
type printerInfo4 struct {
	PrinterName *uint16
	ServerName  *uint16
	Attributes  uint32
}

// docInfo1 is DOC_INFO_1W.
type docInfo1 struct {
	DocName    *uint16
	OutputFile *uint16
	Datatype   *uint16
}

// Windows prints through the Windows spooler: ESC/POS as a RAW document, a
// PDF through SumatraPDF.
type Windows struct {
	sumatra string
}

// New returns the spooler of this operating system.
func New(opts Options) *Windows {
	path := opts.SumatraPDF
	if path == "" {
		if exe, err := os.Executable(); err == nil {
			path = filepath.Join(filepath.Dir(exe), "SumatraPDF.exe")
		}
	}

	return &Windows{sumatra: path}
}

// Printers lists the local printers and the connections of the account the
// agent runs as.
func (*Windows) Printers(context.Context) ([]protocol.Printer, error) {
	flags := uintptr(printerEnumLocal | printerEnumConnections)

	var needed, returned uint32

	// The first call only asks for the size of the list.
	_, _, _ = procEnumPrintersW.Call(flags, 0, 4, 0, 0,
		uintptr(unsafe.Pointer(&needed)), uintptr(unsafe.Pointer(&returned)))
	if needed == 0 {
		return []protocol.Printer{}, nil
	}

	buf := make([]byte, needed)

	r, _, err := procEnumPrintersW.Call(flags, 0, 4, uintptr(unsafe.Pointer(&buf[0])), uintptr(needed),
		uintptr(unsafe.Pointer(&needed)), uintptr(unsafe.Pointer(&returned)))
	if r == 0 {
		return nil, fmt.Errorf("list the printers: %w", err)
	}

	def := defaultPrinter()
	infos := unsafe.Slice((*printerInfo4)(unsafe.Pointer(&buf[0])), returned)
	printers := make([]protocol.Printer, 0, len(infos))

	for _, info := range infos {
		name := windows.UTF16PtrToString(info.PrinterName)
		printers = append(printers, protocol.Printer{Name: name, Default: strings.EqualFold(name, def)})
	}

	return sortPrinters(printers), nil
}

func defaultPrinter() string {
	var size uint32

	_, _, _ = procGetDefaultPrinterW.Call(0, uintptr(unsafe.Pointer(&size)))
	if size == 0 {
		return ""
	}

	buf := make([]uint16, size)

	r, _, _ := procGetDefaultPrinterW.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
	if r == 0 {
		return ""
	}

	return windows.UTF16ToString(buf)
}

// PrintRaw sends ESC/POS bytes to the printer as a RAW document, which the
// driver passes through untouched (a "Generic / Text Only" or the maker's
// driver).
func (*Windows) PrintRaw(_ context.Context, printer, title string, data []byte) error {
	if len(data) == 0 {
		return ErrNothingToPrint
	}

	name, err := windows.UTF16PtrFromString(printer)
	if err != nil {
		return fmt.Errorf("printer name: %w", err)
	}

	var h windows.Handle

	if r, _, err := procOpenPrinterW.Call(uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(&h)), 0); r == 0 {
		return fmt.Errorf("open the printer %q: %w", printer, err)
	}

	defer procClosePrinter.Call(uintptr(h)) //nolint:errcheck // nothing to do if closing fails

	docName, _ := windows.UTF16PtrFromString(title)
	raw, _ := windows.UTF16PtrFromString("RAW")
	doc := docInfo1{DocName: docName, Datatype: raw}

	if r, _, err := procStartDocPrinterW.Call(uintptr(h), 1, uintptr(unsafe.Pointer(&doc))); r == 0 {
		return fmt.Errorf("start the document on %q: %w", printer, err)
	}

	defer procEndDocPrinter.Call(uintptr(h)) //nolint:errcheck // the job is spooled either way

	if r, _, err := procStartPagePrinter.Call(uintptr(h)); r == 0 {
		return fmt.Errorf("start the page on %q: %w", printer, err)
	}

	var written uint32

	r, _, err := procWritePrinter.Call(uintptr(h), uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)), uintptr(unsafe.Pointer(&written)))
	if r == 0 {
		return fmt.Errorf("send to %q: %w", printer, err)
	}

	if int(written) != len(data) {
		return fmt.Errorf("%w: %d of %d bytes to %q", ErrShortWrite, written, len(data), printer)
	}

	if r, _, err := procEndPagePrinter.Call(uintptr(h)); r == 0 {
		return fmt.Errorf("end the page on %q: %w", printer, err)
	}

	return nil
}

// PrintPDF prints a PDF with SumatraPDF, silently, on its page size and
// without scaling.
func (w *Windows) PrintPDF(ctx context.Context, printer, title, paper string, data []byte) error {
	if _, err := os.Stat(w.sumatra); err != nil {
		return fmt.Errorf("%w (%s)", ErrNoSumatra, w.sumatra)
	}

	path, remove, err := tempPDF(data)
	if err != nil {
		return err
	}
	defer remove()

	settings := "noscale"
	if p := pdfPaper(paper); p != "" {
		settings += ",paper=" + p
	}

	_ = title // SumatraPDF names the job after the file

	args := []string{"-print-to", printer, "-print-settings", settings, "-silent", "-exit-on-print", path}
	cmd := exec.CommandContext(ctx, w.sumatra, args...) //nolint:gosec // SumatraPDF next to the agent, no shell

	var out bytes.Buffer

	cmd.Stdout, cmd.Stderr = &out, &out

	if err := cmd.Run(); err != nil {
		if exit, ok := errors.AsType[*exec.ExitError](err); ok {
			return fmt.Errorf("%w: SumatraPDF on %q (exit %d) %s", ErrPrintFailed, printer, exit.ExitCode(), strings.TrimSpace(out.String()))
		}

		return fmt.Errorf("SumatraPDF: %w", err)
	}

	return nil
}

// tempPDF writes the PDF to a file only its owner reads and returns its
// path and a function that removes it.
func tempPDF(data []byte) (string, func(), error) {
	f, err := os.CreateTemp("", "vietgara-*.pdf")
	if err != nil {
		return "", nil, fmt.Errorf("write the PDF: %w", err)
	}

	remove := func() { _ = os.Remove(f.Name()) }

	if _, err := f.Write(data); err != nil {
		_ = f.Close()

		remove()

		return "", nil, fmt.Errorf("write the PDF: %w", err)
	}

	if err := f.Close(); err != nil {
		remove()

		return "", nil, fmt.Errorf("write the PDF: %w", err)
	}

	return f.Name(), remove, nil
}
