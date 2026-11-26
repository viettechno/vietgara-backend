package usecase

import (
	"context"
	"errors"
	"net/netip"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/viettechno/vietgara-backend/internal/document/printmodel"
	"github.com/viettechno/vietgara-backend/internal/document/render"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// Errors of the printer store.
var (
	ErrPrinterNotFound  = errors.New("document: printer not found")
	ErrPrinterNameTaken = errors.New("document: a printer of the garage has that name")
)

// PrinterProtocol is how the apps send a job to a printer (FR-DOC-07).
type PrinterProtocol string

// The protocols: raw ESC/POS over TCP, or a PDF over IPP.
const (
	ProtocolRaw PrinterProtocol = "raw"
	ProtocolIPP PrinterProtocol = "ipp"
)

// The limits of a printer.
const (
	MaxPrinters       = 20
	maxPrinterName    = 60
	maxPrinterPath    = 200
	defaultRawPort    = 9100
	defaultIPPPort    = 631
	defaultIPPPath    = "ipp/print"
	maxHostNameLength = 253
)

// Printer is a network printer a garage saved (Release 13.0). The server
// never connects to it.
type Printer struct {
	ID       string
	Name     string
	Protocol PrinterProtocol
	Host     string
	Port     int
	// Path is the IPP resource path; empty for a raw printer.
	Path string
	// Paper is the roll or the label of a raw printer; empty for IPP.
	Paper printmodel.Paper
	// DocumentTypes are the documents the apps send to it first.
	DocumentTypes []printmodel.DocumentType
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// PrinterStore keeps the garage's printers; implemented by
// repository.PostgresPrinterStore. Every call runs in the garage's
// transaction.
type PrinterStore interface {
	ListPrinters(ctx context.Context, garageID string) ([]Printer, error)
	// GetPrinter returns ErrPrinterNotFound for an unknown id.
	GetPrinter(ctx context.Context, garageID, id string) (Printer, error)
	// LockPrinters holds off the other changes of the garage's printers
	// until the transaction ends, so that the count of CreatePrinter holds.
	LockPrinters(ctx context.Context, garageID string) error
	CountPrinters(ctx context.Context, garageID string) (int, error)
	// CreatePrinter and UpdatePrinter return ErrPrinterNameTaken for a
	// duplicate name; UpdatePrinter and DeletePrinter return
	// ErrPrinterNotFound for an unknown id.
	CreatePrinter(ctx context.Context, garageID string, p Printer) (Printer, error)
	UpdatePrinter(ctx context.Context, garageID string, p Printer) (Printer, error)
	DeletePrinter(ctx context.Context, garageID, id string) error
}

// Printers lists the garage's printers, by name.
func (uc *DocumentUseCase) Printers(ctx context.Context, garageID string) ([]Printer, error) {
	printers, err := uc.PrinterStore.ListPrinters(ctx, garageID)
	if err != nil {
		return nil, apperror.Internal("list printers", err)
	}

	return printers, nil
}

// CreatePrinter saves a printer; the garage keeps at most MaxPrinters.
func (uc *DocumentUseCase) CreatePrinter(ctx context.Context, garageID string, p Printer) (Printer, error) {
	p, err := normalizePrinter(p)
	if err != nil {
		return Printer{}, err
	}

	if err := uc.PrinterStore.LockPrinters(ctx, garageID); err != nil {
		return Printer{}, apperror.Internal("lock printers", err)
	}

	count, err := uc.PrinterStore.CountPrinters(ctx, garageID)
	if err != nil {
		return Printer{}, apperror.Internal("count printers", err)
	}

	if count >= MaxPrinters {
		return Printer{}, apperror.Validation("printer", apperror.IssueInvalid, "a garage can save at most 20 printers")
	}

	created, err := uc.PrinterStore.CreatePrinter(ctx, garageID, p)
	if err != nil {
		return Printer{}, printerError("create printer", err)
	}

	return created, nil
}

// UpdatePrinter replaces a printer.
func (uc *DocumentUseCase) UpdatePrinter(ctx context.Context, garageID string, p Printer) (Printer, error) {
	p, err := normalizePrinter(p)
	if err != nil {
		return Printer{}, err
	}

	updated, err := uc.PrinterStore.UpdatePrinter(ctx, garageID, p)
	if err != nil {
		return Printer{}, printerError("update printer", err)
	}

	return updated, nil
}

// DeletePrinter removes a printer.
func (uc *DocumentUseCase) DeletePrinter(ctx context.Context, garageID, id string) error {
	if err := uc.PrinterStore.DeletePrinter(ctx, garageID, id); err != nil {
		return printerError("delete printer", err)
	}

	return nil
}

// PrinterTestPage returns a printer's test page and its content type: ESC/POS
// on the roll or label of a raw printer, an A4 PDF for an IPP printer.
func (uc *DocumentUseCase) PrinterTestPage(ctx context.Context, garageID, id string) ([]byte, string, error) {
	p, err := uc.PrinterStore.GetPrinter(ctx, garageID, id)
	if err != nil {
		return nil, "", printerError("get printer", err)
	}

	garage, err := uc.Garages.Get(ctx, garageID)
	if err != nil {
		return nil, "", apperror.Internal("read garage", err)
	}

	if p.Protocol == ProtocolIPP {
		data, err := render.TestPagePDF(garage.Name, p.Name)
		if err != nil {
			return nil, "", apperror.Internal("render test page", err)
		}

		return data, ContentTypePDF, nil
	}

	data, err := render.TestPageEscPos(p.Paper, garage.Name, p.Name)
	if err != nil {
		return nil, "", apperror.Internal("render test page", err)
	}

	return data, ContentTypeEscPos, nil
}

// The content types of the print output.
const (
	ContentTypePDF    = "application/pdf"
	ContentTypeEscPos = "application/octet-stream"
)

func printerError(op string, err error) error {
	switch {
	case errors.Is(err, ErrPrinterNotFound):
		return apperror.New(apperror.CodeNotFound, "printer not found")
	case errors.Is(err, ErrPrinterNameTaken):
		return apperror.Validation("name", apperror.IssueTaken, "a printer of the garage has that name")
	default:
		return apperror.Internal(op, err)
	}
}

var (
	// hostName is a DNS name, a final dot allowed (an mDNS "printer.local.").
	hostName = regexp.MustCompile(`^(?i)[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*\.?$`)
	ippPath  = regexp.MustCompile(`^[A-Za-z0-9._~/-]+$`)
)

// normalizePrinter trims and checks a printer and fills in the defaults.
func normalizePrinter(p Printer) (Printer, error) {
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" {
		return Printer{}, apperror.Validation("name", apperror.IssueRequired, "name is required")
	}

	if utf8.RuneCountInString(p.Name) > maxPrinterName {
		return Printer{}, apperror.Validation("name", apperror.IssueTooLong, "name is at most 60 characters")
	}

	p.Host = strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(p.Host), "["), "]")
	if !validHost(p.Host) {
		return Printer{}, apperror.Validation("host", apperror.IssueInvalid, "host must be an IP address or a host name")
	}

	if p.Port < 0 || p.Port > 65535 {
		return Printer{}, apperror.Validation("port", apperror.IssueInvalid, "port must be between 1 and 65535")
	}

	p, err := normalizeProtocol(p)
	if err != nil {
		return Printer{}, err
	}

	types, err := printerTypes(p)
	if err != nil {
		return Printer{}, err
	}

	p.DocumentTypes = types

	return p, nil
}

// normalizeProtocol checks the fields that depend on the protocol: the
// default port, the IPP path and the paper of a raw printer.
func normalizeProtocol(p Printer) (Printer, error) {
	switch p.Protocol {
	case ProtocolRaw:
		if p.Port == 0 {
			p.Port = defaultRawPort
		}

		if strings.TrimSpace(p.Path) != "" {
			return Printer{}, apperror.Validation("path", apperror.IssueInvalid, "a path is for an IPP printer only")
		}

		p.Path = ""

		if !p.Paper.IsRoll() && p.Paper != printmodel.Label5030 {
			return Printer{}, apperror.Validation("paperSize", apperror.IssueInvalid, "a raw printer prints on K80, K57 or 50 x 30 mm labels")
		}
	case ProtocolIPP:
		if p.Port == 0 {
			p.Port = defaultIPPPort
		}

		p.Path = strings.TrimLeft(strings.TrimSpace(p.Path), "/")
		if p.Path == "" {
			p.Path = defaultIPPPath
		}

		if len(p.Path) > maxPrinterPath || !ippPath.MatchString(p.Path) {
			return Printer{}, apperror.Validation("path", apperror.IssueInvalid, "path may use letters, digits and - . _ ~ / only")
		}

		if p.Paper != "" {
			return Printer{}, apperror.Validation("paperSize", apperror.IssueInvalid, "an IPP printer prints on the garage's paper setting")
		}
	default:
		return Printer{}, apperror.Validation("protocol", apperror.IssueInvalid, "protocol must be raw or IPP")
	}

	return p, nil
}

// printerTypes checks the default documents of p, without duplicates, in
// the order of the document types.
func printerTypes(p Printer) ([]printmodel.DocumentType, error) {
	var allowed []printmodel.DocumentType

	switch {
	case p.Protocol == ProtocolIPP:
		allowed = printmodel.Types()
	case p.Paper == printmodel.Label5030:
		allowed = []printmodel.DocumentType{printmodel.TypePartLabel}
	default:
		for _, t := range printmodel.Types() {
			if _, ok := render.Latest(t, p.Paper); ok {
				allowed = append(allowed, t)
			}
		}
	}

	for _, t := range p.DocumentTypes {
		if !slices.Contains(allowed, t) {
			return nil, apperror.Validation("documentTypes", apperror.IssueInvalid, "this printer cannot print "+string(t))
		}
	}

	types := make([]printmodel.DocumentType, 0, len(p.DocumentTypes))

	for _, t := range allowed {
		if slices.Contains(p.DocumentTypes, t) {
			types = append(types, t)
		}
	}

	return types, nil
}

func validHost(host string) bool {
	if host == "" || len(host) > maxHostNameLength {
		return false
	}

	if _, err := netip.ParseAddr(host); err == nil {
		return true // an IPv4 or IPv6 address, a zone allowed (fe80::1%en0)
	}

	// A name of digits and dots only is a mistyped IPv4 address
	// (192.168.1.300, 10.0.0), not a host name.
	if strings.Trim(host, "0123456789.") == "" {
		return false
	}

	return hostName.MatchString(host)
}

// LabelCopies is how many labels of one part to print.
type LabelCopies struct {
	PartID string
	Copies int
}

// The limits of one label print.
const (
	maxLabelParts  = 50
	maxLabelCopies = 100
	maxLabels      = 300
)

// PartReader reads a part for its label; implemented by an adapter over the
// Catalogue module in cmd/server. It returns the catalogue's not found
// error for an unknown part.
type PartReader interface {
	PartLabel(ctx context.Context, garageID, partID string) (printmodel.PartLabel, error)
}

// PartLabels prints the labels of parts (FR-INV-13) on a label printer, as
// ESC/POS: copies of each, in the order given. Nothing is logged.
func (uc *DocumentUseCase) PartLabels(ctx context.Context, garageID string, paper printmodel.Paper, parts []LabelCopies) ([]byte, error) {
	total, err := checkLabelCopies(paper, parts)
	if err != nil {
		return nil, err
	}

	labels := make([]printmodel.PartLabel, 0, total)

	for _, p := range parts {
		label, err := uc.Parts.PartLabel(ctx, garageID, p.PartID)
		if err != nil {
			return nil, toAppError("read part", err)
		}

		for range p.Copies {
			labels = append(labels, label)
		}
	}

	data, err := render.PartLabels(paper, labels)
	if errors.Is(err, render.ErrBarcodeUnreadable) {
		return nil, apperror.Validation("parts", apperror.IssueInvalid,
			"a part's code is not ASCII or too long for a readable barcode on the label: "+err.Error())
	}

	if err != nil {
		return nil, apperror.Internal("render labels", err)
	}

	return data, nil
}

// checkLabelCopies checks a label print and returns the number of labels.
func checkLabelCopies(paper printmodel.Paper, parts []LabelCopies) (int, error) {
	if paper != printmodel.Label5030 {
		return 0, apperror.Validation("paperSize", apperror.IssueInvalid, "labels print on 50 x 30 mm labels")
	}

	if len(parts) == 0 || len(parts) > maxLabelParts {
		return 0, apperror.Validation("parts", apperror.IssueInvalid, "print the labels of 1 to 50 parts")
	}

	seen := map[string]bool{}
	total := 0

	for _, p := range parts {
		if seen[p.PartID] {
			return 0, apperror.Validation("parts", apperror.IssueInvalid, "a part is listed twice")
		}

		seen[p.PartID] = true

		if p.Copies < 1 || p.Copies > maxLabelCopies {
			return 0, apperror.Validation("parts", apperror.IssueInvalid, "copies must be between 1 and 100")
		}

		total += p.Copies
	}

	if total > maxLabels {
		return 0, apperror.Validation("parts", apperror.IssueInvalid, "at most 300 labels at a time")
	}

	return total, nil
}
