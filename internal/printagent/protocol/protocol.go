// Package protocol is the language of the print agent's connection
// (FR-DOC-08, Release 13.1), shared by the server and the agent: JSON text
// messages over a WebSocket the agent opens to ConnectPath with its key as a
// bearer token.
//
// The server sends "hello" once, then a "job" for each print of the agent's
// printers. The agent sends its "printers" when it connects and when they
// change, and the "result" of each job. A job may be sent again after a
// reconnection; the agent prints a job id once.
package protocol

// The paths of the agent's API.
const (
	// PairPath exchanges a pairing code for the agent's key
	// (PrintAgentService.PairPrintAgent).
	PairPath = "/api/v1/public/print-agents:pair"
	// ConnectPath is the agent's WebSocket.
	ConnectPath = "/api/v1/print-agents/connect"
)

// The close codes the server uses beyond the standard ones.
const (
	// CloseRevoked: the agent's key no longer works (the agent was removed
	// or paired again). The agent stops; it must be paired again.
	CloseRevoked = 4001
	// CloseReplaced: another connection of the same agent took over.
	CloseReplaced = 4002
)

// The message types.
const (
	TypeHello    = "hello"
	TypeJob      = "job"
	TypePrinters = "printers"
	TypeResult   = "result"
)

// The formats of a job.
const (
	FormatPDF    = "pdf"
	FormatEscPos = "escpos"
)

// The statuses of a result.
const (
	StatusPrinted = "printed"
	StatusFailed  = "failed"
)

// MaxServerMessage is the largest message the agent accepts (a job with
// its document); MaxAgentMessage the largest the server accepts.
const (
	MaxServerMessage = 32 << 20
	MaxAgentMessage  = 64 << 10
)

// Message is every message, by Type; the fields of the other types are
// empty.
type Message struct {
	Type string `json:"type"`

	// hello
	AgentID string `json:"agentId,omitempty"`

	// job
	Job *Job `json:"job,omitempty"`

	// printers
	Hostname string    `json:"hostname,omitempty"`
	OS       string    `json:"os,omitempty"`
	Version  string    `json:"version,omitempty"`
	Printers []Printer `json:"printers,omitempty"`

	// result
	JobID  string `json:"jobId,omitempty"`
	Status string `json:"status,omitempty"`
	Error  string `json:"error,omitempty"`
}

// Job is a print for one of the PC's printers.
type Job struct {
	ID string `json:"id"`
	// Printer is the printer's name in the operating system.
	Printer string `json:"printer"`
	// Format is FormatPDF or FormatEscPos.
	Format string `json:"format"`
	// PaperSize is the page size of a PDF ("A4", "A5"), which it is printed
	// on without scaling, or the roll or label of ESC/POS bytes.
	PaperSize string `json:"paperSize,omitempty"`
	// Title is the job's name in the PC's print queue.
	Title string `json:"title"`
	// Data is the document, base64 in the JSON.
	Data []byte `json:"data"`
}

// Printer is a printer of the PC.
type Printer struct {
	Name    string `json:"name"`
	Default bool   `json:"default,omitempty"`
}
