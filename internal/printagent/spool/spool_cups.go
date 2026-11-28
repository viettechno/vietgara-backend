//go:build !windows

package spool

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/viettechno/vietgara-backend/internal/printagent/protocol"
)

// CUPS prints through the lp and lpstat commands (Linux, macOS).
type CUPS struct{}

// New returns the spooler of this operating system.
func New(Options) *CUPS { return &CUPS{} }

// Printers lists the CUPS destinations.
func (CUPS) Printers(ctx context.Context) ([]protocol.Printer, error) {
	out, err := exec.CommandContext(ctx, "lpstat", "-e").Output()
	if err != nil {
		return nil, fmt.Errorf("lpstat -e: %w", err)
	}

	def := ""

	if d, err := exec.CommandContext(ctx, "lpstat", "-d").Output(); err == nil {
		if _, name, ok := strings.Cut(strings.TrimSpace(string(d)), ": "); ok {
			def = name
		}
	}

	printers := []protocol.Printer{}

	for line := range strings.SplitSeq(string(out), "\n") {
		if name := strings.TrimSpace(line); name != "" {
			printers = append(printers, protocol.Printer{Name: name, Default: name == def})
		}
	}

	return sortPrinters(printers), nil
}

// PrintRaw sends ESC/POS bytes to the printer as they are.
func (CUPS) PrintRaw(ctx context.Context, printer, title string, data []byte) error {
	return lp(ctx, data, "-d", printer, "-t", title, "-o", "raw")
}

// PrintPDF prints a PDF on its page size, unscaled.
func (CUPS) PrintPDF(ctx context.Context, printer, title, paper string, data []byte) error {
	args := []string{"-d", printer, "-t", title, "-o", "print-scaling=none"}
	if p := pdfPaper(paper); p != "" {
		args = append(args, "-o", "media="+p)
	}

	return lp(ctx, data, args...)
}

func lp(ctx context.Context, data []byte, args ...string) error {
	cmd := exec.CommandContext(ctx, "lp", args...) //nolint:gosec // lp with the job's printer as an argument, no shell
	cmd.Stdin = bytes.NewReader(data)

	var stderr bytes.Buffer

	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if _, ok := errors.AsType[*exec.ExitError](err); ok && stderr.Len() > 0 {
			return fmt.Errorf("%w: %s", ErrPrintFailed, strings.TrimSpace(stderr.String()))
		}

		return fmt.Errorf("lp: %w", err)
	}

	return nil
}
