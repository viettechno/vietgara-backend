package usecase

import (
	"context"
	"errors"
	"time"

	"github.com/viettechno/vietgara-backend/internal/document/printmodel"
	"github.com/viettechno/vietgara-backend/internal/platform/access"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// ErrJobNotFound is returned for an unknown print job.
var ErrJobNotFound = errors.New("document: print job not found")

// JobFormat is what the agent receives: a PDF or ESC/POS bytes.
type JobFormat string

// The formats of a print job.
const (
	FormatPDF    JobFormat = "pdf"
	FormatEscPos JobFormat = "escpos"
)

// JobStatus is where a print job is.
type JobStatus string

// The statuses of a print job.
const (
	JobQueued  JobStatus = "queued"
	JobSent    JobStatus = "sent"
	JobPrinted JobStatus = "printed"
	JobFailed  JobStatus = "failed"
	JobExpired JobStatus = "expired"
)

// The limits of the print jobs.
const (
	// PrintJobTTL is how long a job waits for its agent: a voucher should
	// not come out of the printer long after it was asked for.
	PrintJobTTL = time.Hour
	// JobRetention is how long a finished job is kept, for its status.
	JobRetention = 7 * 24 * time.Hour
	// AgentJobBatch is how many jobs a connection takes at a time.
	AgentJobBatch = 10
	// MaxJobBytes is the largest document a job carries: in base64 it stays
	// under the agent's message limit (protocol.MaxServerMessage, 32 MiB).
	MaxJobBytes = 20 << 20
	maxJobError = 500
)

// PrintJob is a print sent to an agent's printer (FR-DOC-08).
type PrintJob struct {
	ID          string
	PrinterID   string
	PrinterName string
	// DocumentType is empty for a test page.
	DocumentType printmodel.DocumentType
	Status       JobStatus
	Error        string
	ExpiresAt    time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// NewJob is a job to queue.
type NewJob struct {
	AgentID, PrinterID, PrinterName, Queue string
	DocumentType                           printmodel.DocumentType
	Format                                 JobFormat
	Paper                                  printmodel.Paper
	Data                                   []byte
	ExpiresAt                              time.Time
}

// AgentJob is a job as its agent receives it.
type AgentJob struct {
	ID           string
	Queue        string
	DocumentType printmodel.DocumentType
	Format       JobFormat
	// Paper is the page size of a PDF (A4, A5) or the roll or label of
	// ESC/POS bytes.
	Paper printmodel.Paper
	Data  []byte
}

// JobStore keeps the print jobs; implemented by repository.PostgresAgentStore.
// Every call runs in the garage's transaction.
type JobStore interface {
	CreateJob(ctx context.Context, garageID string, j NewJob) (PrintJob, error)
	// GetJob returns ErrJobNotFound for an unknown id.
	GetJob(ctx context.Context, garageID, id string) (PrintJob, error)
	// PurgeJobs deletes the garage's jobs created before the time.
	PurgeJobs(ctx context.Context, garageID string, before time.Time) error
	// PendingJobs expires the agent's jobs past their time, then returns the
	// oldest of those to send on the connection, with their bytes: queued,
	// or sent before the connection started (the agent dedupes them). A
	// connection that is no longer the agent's gets none.
	PendingJobs(ctx context.Context, s AgentSession, now time.Time, limit int) ([]AgentJob, error)
	// MarkSent records that the jobs were sent on the connection, if it is
	// still the agent's.
	MarkSent(ctx context.Context, s AgentSession, ids []string) error
	// FinishJob records the agent's result of a job still queued or sent,
	// and clears its bytes; a job that is not the agent's, or is finished,
	// is left as it is.
	FinishJob(ctx context.Context, s AgentSession, id string, status JobStatus, message string, at time.Time) error
}

// JobDocument is what a print job prints: a document (Type with ID, and
// PaymentID for a payment voucher), part labels, or the printer's test page.
type JobDocument struct {
	Type      printmodel.DocumentType
	ID        string
	PaymentID string
	Labels    []LabelCopies
	TestPage  bool
}

// jobPermission is the permission each document's print needs, as its
// print endpoint does.
var jobPermission = map[printmodel.DocumentType]access.Permission{
	printmodel.TypeQuote:          access.QuoteRead,
	printmodel.TypeRepairOrder:    access.RepairOrderRead,
	printmodel.TypeSettlement:     access.BillingRead,
	printmodel.TypePaymentVoucher: access.BillingRead,
	printmodel.TypeGoodsReceipt:   access.InventoryRead,
	printmodel.TypeStockIssue:     access.InventoryRead,
	printmodel.TypePaymentOut:     access.BillingRead,
	printmodel.TypePurchaseOrder:  access.InventoryRead,
	printmodel.TypePartLabel:      access.CatalogueRead,
}

// CreatePrintJob renders a document for an agent's printer and queues it
// for the agent. The print follows the rules of the print endpoints (the
// version, copy mark and log of a document; labels and test pages are not
// logged).
func (uc *DocumentUseCase) CreatePrintJob(ctx context.Context, garageID, printerID string, doc JobDocument) (PrintJob, error) {
	if err := checkJobPermission(ctx, doc); err != nil {
		return PrintJob{}, err
	}

	p, err := uc.PrinterStore.GetPrinter(ctx, garageID, printerID)
	if err != nil {
		return PrintJob{}, printerError("get printer", err)
	}

	if p.Protocol != ProtocolAgent {
		return PrintJob{}, apperror.Validation("printerId", apperror.IssueInvalid, "this printer is printed from the owner app, not through a print agent")
	}

	data, format, paper, err := uc.renderJob(ctx, garageID, p, doc)
	if err != nil {
		return PrintJob{}, err
	}

	if len(data) > MaxJobBytes {
		return PrintJob{}, apperror.Validation("document", apperror.IssueTooLong,
			"the document is too large to send to a print agent: print it through the print dialog")
	}

	now := uc.now()

	if err := uc.JobStore.PurgeJobs(ctx, garageID, now.Add(-JobRetention)); err != nil {
		return PrintJob{}, apperror.Internal("purge print jobs", err)
	}

	job, err := uc.JobStore.CreateJob(ctx, garageID, NewJob{
		AgentID: p.AgentID, PrinterID: p.ID, PrinterName: p.Name, Queue: p.Queue, DocumentType: doc.Type,
		Format: format, Paper: paper, Data: data, ExpiresAt: now.Add(PrintJobTTL),
	})
	if err != nil {
		return PrintJob{}, apperror.Internal("create print job", err)
	}

	platformdb.AfterCommit(ctx, func() { uc.AgentNotifier.JobQueued(p.AgentID) })

	return job, nil
}

// PrintJob returns a job; a job still waiting past its time reads expired.
func (uc *DocumentUseCase) PrintJob(ctx context.Context, garageID, id string) (PrintJob, error) {
	job, err := uc.JobStore.GetJob(ctx, garageID, id)
	if errors.Is(err, ErrJobNotFound) {
		return PrintJob{}, apperror.New(apperror.CodeNotFound, "print job not found")
	}

	if err != nil {
		return PrintJob{}, apperror.Internal("get print job", err)
	}

	if (job.Status == JobQueued || job.Status == JobSent) && !uc.now().Before(job.ExpiresAt) {
		job.Status = JobExpired
	}

	return job, nil
}

// AgentJobs returns the next jobs of the connection's agent.
func (uc *DocumentUseCase) AgentJobs(ctx context.Context, s AgentSession) ([]AgentJob, error) {
	var jobs []AgentJob

	err := uc.inAgentGarage(ctx, s, func(ctx context.Context) error {
		var err error

		jobs, err = uc.JobStore.PendingJobs(ctx, s, uc.now(), AgentJobBatch)

		return err
	})

	return jobs, err
}

// AgentJobsSent records that the jobs were sent to the agent.
func (uc *DocumentUseCase) AgentJobsSent(ctx context.Context, s AgentSession, ids []string) error {
	return uc.inAgentGarage(ctx, s, func(ctx context.Context) error { return uc.JobStore.MarkSent(ctx, s, ids) })
}

// AgentJobDone records the agent's result of a job: printed, or failed
// with its message.
func (uc *DocumentUseCase) AgentJobDone(ctx context.Context, s AgentSession, id string, printed bool, message string) error {
	status, text := JobPrinted, ""
	if !printed {
		status, text = JobFailed, clip(message, maxJobError)
		if text == "" {
			text = "the print agent could not print"
		}
	}

	return uc.inAgentGarage(ctx, s, func(ctx context.Context) error {
		return uc.JobStore.FinishJob(ctx, s, id, status, text, uc.now())
	})
}

func checkJobPermission(ctx context.Context, doc JobDocument) error {
	if doc.TestPage {
		return nil
	}

	t := doc.Type
	if doc.Labels != nil {
		t = printmodel.TypePartLabel
	}

	perm, ok := jobPermission[t]
	if !ok {
		return apperror.Validation("document", apperror.IssueRequired, "name the document to print")
	}

	if a, ok := access.FromContext(ctx); !ok || !a.Has(perm) {
		return apperror.New(apperror.CodePermissionDenied, "missing permission "+string(perm))
	}

	return nil
}

// renderJob renders what the job prints for the printer's paper.
func (uc *DocumentUseCase) renderJob(
	ctx context.Context, garageID string, p Printer, doc JobDocument,
) ([]byte, JobFormat, printmodel.Paper, error) {
	switch {
	case doc.TestPage:
		return uc.testPage(ctx, garageID, p)
	case doc.Labels != nil:
		data, err := uc.PartLabels(ctx, garageID, p.Paper, doc.Labels)

		return data, FormatEscPos, p.Paper, err
	case p.Paper == printmodel.Label5030:
		return nil, "", "", apperror.Validation("printerId", apperror.IssueInvalid, "a label printer prints part labels only")
	case p.Paper.IsRoll():
		data, err := uc.rollJob(ctx, garageID, p.Paper, doc)

		return data, FormatEscPos, p.Paper, err
	}

	paper := p.Paper
	if paper == "" {
		chosen, err := uc.Prints.Settings(ctx, garageID)
		if err != nil {
			return nil, "", "", apperror.Internal("read print settings", err)
		}

		paper = effective(chosen, doc.Type).Paper
	}

	data, err := uc.documentPDF(ctx, garageID, doc, PrintOn(paper))

	return data, FormatPDF, paper, err
}

func (uc *DocumentUseCase) rollJob(ctx context.Context, garageID string, paper printmodel.Paper, doc JobDocument) ([]byte, error) {
	switch doc.Type { //nolint:exhaustive // only these two have roll templates
	case printmodel.TypeQuote:
		return uc.QuoteEscPos(ctx, garageID, doc.ID, paper)
	case printmodel.TypePaymentVoucher:
		return uc.PaymentVoucherEscPos(ctx, garageID, doc.ID, doc.PaymentID, paper)
	default:
		return nil, apperror.Validation("printerId", apperror.IssueInvalid, "this document has no template for "+string(paper))
	}
}

func (uc *DocumentUseCase) documentPDF(ctx context.Context, garageID string, doc JobDocument, out Output) ([]byte, error) {
	switch doc.Type {
	case printmodel.TypeQuote:
		return uc.QuotePDF(ctx, garageID, doc.ID, out)
	case printmodel.TypeRepairOrder:
		return uc.RepairOrderPDF(ctx, garageID, doc.ID, out)
	case printmodel.TypeSettlement:
		return uc.SettlementReceiptPDF(ctx, garageID, doc.ID, out)
	case printmodel.TypePaymentVoucher:
		return uc.PaymentVoucherPDF(ctx, garageID, doc.ID, doc.PaymentID, out)
	case printmodel.TypeGoodsReceipt:
		return uc.GoodsReceiptPDF(ctx, garageID, doc.ID, out)
	case printmodel.TypeStockIssue:
		return uc.StockIssuePDF(ctx, garageID, doc.ID, out)
	case printmodel.TypePaymentOut:
		return uc.PaymentOutVoucherPDF(ctx, garageID, doc.ID, out)
	case printmodel.TypePurchaseOrder:
		return uc.PurchaseOrderPDF(ctx, garageID, doc.ID, out)
	case printmodel.TypePartLabel:
	}

	return nil, apperror.Validation("document", apperror.IssueRequired, "name the document to print")
}

// jobTitle is the job's name in the PC's print queue.
func jobTitle(t printmodel.DocumentType) string {
	if t == "" {
		return "VietGara test page"
	}

	return "VietGara " + string(t)
}

// JobTitle is the name of an agent job in the PC's print queue.
func (j AgentJob) JobTitle() string { return jobTitle(j.DocumentType) }
