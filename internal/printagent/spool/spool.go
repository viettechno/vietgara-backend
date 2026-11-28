// Package spool prints on the PC's printers for the print agent: through the
// Windows spooler (ESC/POS sent raw, a PDF through SumatraPDF), or through
// CUPS on Linux and macOS.
package spool

import (
	"errors"
	"slices"
	"strings"

	"github.com/viettechno/vietgara-backend/internal/printagent/protocol"
)

// Errors of the spoolers.
var (
	ErrNothingToPrint = errors.New("nothing to print")
	ErrShortWrite     = errors.New("the printer took part of the job only")
	ErrNoSumatra      = errors.New("SumatraPDF.exe is missing next to vietgara-agent.exe: reinstall the agent")
	ErrPrintFailed    = errors.New("the print failed")
)

// Options configure the spooler.
type Options struct {
	// SumatraPDF is the path of SumatraPDF.exe (Windows), which prints the
	// PDFs; next to the agent's executable when empty.
	SumatraPDF string
}

// pdfPaper is the page size of a PDF the printer is told to use, or "" to
// leave the printer's own: the documents are rendered on A4 or A5.
func pdfPaper(paper string) string {
	if paper == "A4" || paper == "A5" {
		return paper
	}

	return ""
}

// sortPrinters orders the printers by name, the default first.
func sortPrinters(printers []protocol.Printer) []protocol.Printer {
	slices.SortFunc(printers, func(a, b protocol.Printer) int {
		if a.Default != b.Default {
			if a.Default {
				return -1
			}

			return 1
		}

		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})

	return printers
}
