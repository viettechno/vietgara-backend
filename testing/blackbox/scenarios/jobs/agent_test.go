package jobs_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	documentv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/document/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/client"
	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

// agentMessage is a message of the print agent's WebSocket (API
// Specification, Section 10.4).
type agentMessage struct {
	Type     string         `json:"type"`
	AgentID  string         `json:"agentId,omitempty"`
	Job      *agentJob      `json:"job,omitempty"`
	Hostname string         `json:"hostname,omitempty"`
	Version  string         `json:"version,omitempty"`
	Printers []agentPrinter `json:"printers,omitempty"`
	JobID    string         `json:"jobId,omitempty"`
	Status   string         `json:"status,omitempty"`
	Error    string         `json:"error,omitempty"`
}

type agentJob struct {
	ID        string `json:"id"`
	Printer   string `json:"printer"`
	Format    string `json:"format"`
	PaperSize string `json:"paperSize"`
	Data      string `json:"data"`
}

type agentPrinter struct {
	Name    string `json:"name"`
	Default bool   `json:"default,omitempty"`
}

// fakeAgent is the agent's side of the connection, played by the test.
type fakeAgent struct {
	t *testing.T
	c *websocket.Conn
}

// connectAgent opens the agent's WebSocket through the gateway; the HTTP
// status is returned when the upgrade is refused.
func connectAgent(t *testing.T, api *client.Client, key string) (*fakeAgent, int) {
	t.Helper()

	c, resp, err := websocket.Dial(t.Context(), api.AgentConnectURL(), &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": {"Bearer " + key}},
	})
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}

	if err != nil {
		require.NotNil(t, resp, "the upgrade failed without an answer: %v", err)

		return nil, resp.StatusCode
	}

	c.SetReadLimit(32 << 20)
	t.Cleanup(func() { _ = c.CloseNow() })

	a := &fakeAgent{t: t, c: c}
	assert.Equal(t, "hello", a.read().Type)

	return a, http.StatusSwitchingProtocols
}

func (a *fakeAgent) read() agentMessage {
	a.t.Helper()

	ctx, cancel := context.WithTimeout(a.t.Context(), 20*time.Second)
	defer cancel()

	_, data, err := a.c.Read(ctx)
	require.NoError(a.t, err)

	var msg agentMessage
	require.NoError(a.t, json.Unmarshal(data, &msg))

	return msg
}

// closed reads until the server closes the connection and returns the code.
func (a *fakeAgent) closed() websocket.StatusCode {
	a.t.Helper()

	ctx, cancel := context.WithTimeout(a.t.Context(), 30*time.Second)
	defer cancel()

	for {
		if _, _, err := a.c.Read(ctx); err != nil {
			return websocket.CloseStatus(err)
		}
	}
}

func (a *fakeAgent) send(msg agentMessage) {
	a.t.Helper()

	data, err := json.Marshal(msg)
	require.NoError(a.t, err)
	require.NoError(a.t, a.c.Write(a.t.Context(), websocket.MessageText, data))
}

func (a *fakeAgent) job() agentJob {
	a.t.Helper()

	msg := a.read()
	require.Equal(a.t, "job", msg.Type)
	require.NotNil(a.t, msg.Job)

	return *msg.Job
}

// pairedAgent creates an agent, pairs it and saves its K80 printer.
func pairedAgent(t *testing.T, k *garageKit, name string) (agentID, key string, printer *documentv1.Printer) {
	t.Helper()

	ctx := t.Context()

	created, err := k.owner.API.CreatePrintAgent(ctx, k.garageID, name)
	require.NoError(t, err)

	paired, err := fixture.Anonymous(t).PairPrintAgent(ctx, &documentv1.PairPrintAgentRequest{
		PairingCode: created.GetPairingCode(), Hostname: "QUAY-PC", Os: "windows/amd64", Version: "blackbox",
	})
	require.NoError(t, err)

	printer, err = k.owner.API.CreatePrinter(ctx, k.garageID, &documentv1.Printer{
		Name: name + " K80", Protocol: documentv1.PrinterProtocol_PRINTER_PROTOCOL_AGENT, AgentId: paired.GetAgentId(),
		Queue: "XP-80C", PaperSize: documentv1.PaperSize_PAPER_SIZE_K80,
		DocumentTypes: []documentv1.DocumentType{documentv1.DocumentType_DOCUMENT_TYPE_QUOTE},
	})
	require.NoError(t, err)

	return paired.GetAgentId(), paired.GetAgentKey(), printer
}

func waitForJob(t *testing.T, api *client.Client, garageID, jobID string, want documentv1.PrintJobStatus) *documentv1.PrintJob {
	t.Helper()

	var job *documentv1.PrintJob

	require.Eventually(t, func() bool {
		var err error

		job, err = api.GetPrintJob(t.Context(), garageID, jobID)

		return err == nil && job.GetStatus() == want
	}, 20*time.Second, 200*time.Millisecond, "job %s never reached %s", jobID, want)

	return job
}

// Scenario: E2E-DOC-011, Release 13.1, a garage pairs a print agent: the code
// works once, the agent reports its printers, a member without garage.write
// lists the agents but cannot add one, another garage sees none, and a new
// pairing code revokes the key (FR-DOC-08).
func TestPrintAgentPairing(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	k := setup(t, "print-011")
	api, garageID := k.owner.API, k.garageID

	created, err := api.CreatePrintAgent(ctx, garageID, "Máy quầy")
	require.NoError(t, err)
	assert.Regexp(t, `^[A-Z2-9]{4}-[A-Z2-9]{4}$`, created.GetPairingCode())
	assert.False(t, created.GetAgent().GetPaired())

	_, err = api.CreatePrintAgent(ctx, garageID, "MÁY QUẦY")
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "VALIDATION_ERROR")

	anonymous := fixture.Anonymous(t)

	paired, err := anonymous.PairPrintAgent(ctx, &documentv1.PairPrintAgentRequest{
		PairingCode: created.GetPairingCode(), Hostname: "QUAY-PC", Os: "windows/amd64", Version: "blackbox",
	})
	require.NoError(t, err)
	assert.Equal(t, "Máy quầy", paired.GetAgentName())
	assert.Equal(t, "Blackbox print-011", paired.GetGarageName())

	_, err = anonymous.PairPrintAgent(ctx, &documentv1.PairPrintAgentRequest{PairingCode: created.GetPairingCode()})
	fixture.RequireAPIError(t, err, http.StatusNotFound, "")

	agent, status := connectAgent(t, api, paired.GetAgentKey())
	require.Equal(t, http.StatusSwitchingProtocols, status)
	agent.send(agentMessage{
		Type: "printers", Hostname: "QUAY-PC", Version: "blackbox",
		Printers: []agentPrinter{{Name: "XP-80C", Default: true}, {Name: "HP LaserJet"}},
	})

	var listed []*documentv1.PrintAgent

	require.Eventually(t, func() bool {
		listed, err = api.ListPrintAgents(ctx, garageID)

		return err == nil && len(listed) == 1 && len(listed[0].GetPrinters()) == 2
	}, 20*time.Second, 200*time.Millisecond)
	assert.True(t, listed[0].GetConnected())
	assert.True(t, listed[0].GetPaired())
	assert.Equal(t, "QUAY-PC", listed[0].GetHostname())

	member := staffIn(t, k, "print-011-member", "Inventory Staff")

	seen, err := member.API.ListPrintAgents(ctx, garageID)
	require.NoError(t, err)
	assert.Len(t, seen, 1)

	_, err = member.API.CreatePrintAgent(ctx, garageID, "Kho")
	fixture.RequireAPIError(t, err, http.StatusForbidden, "")

	other := setup(t, "print-011-other")
	_, err = other.owner.API.ListPrintAgents(ctx, garageID)
	fixture.RequireAPIError(t, err, http.StatusForbidden, "")

	_, err = api.IssuePrintAgentPairingCode(ctx, garageID, paired.GetAgentId())
	require.NoError(t, err)
	assert.Equal(t, websocket.StatusCode(4001), agent.closed(), "the open connection is closed as revoked")

	_, status = connectAgent(t, api, paired.GetAgentKey())
	assert.Equal(t, http.StatusUnauthorized, status, "the old key no longer works")
}

// Scenario: E2E-DOC-012, Release 13.1, a quote prints silently on the K80
// printer of a print agent: the server renders ESC/POS for the roll, logs the
// print, sends the job to the connected agent, and the job reads printed, or
// failed with the agent's message (FR-DOC-08, FR-DOC-06).
func TestPrintToAgent(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	k := setup(t, "print-012")
	api, garageID := k.owner.API, k.garageID
	quote := draft(t, k, quoteLabor("Thay dầu động cơ", 250_000))
	_, key, printer := pairedAgent(t, k, "Quầy")

	agent, status := connectAgent(t, api, key)
	require.Equal(t, http.StatusSwitchingProtocols, status)

	job, err := api.CreatePrintJob(ctx, garageID, &documentv1.CreatePrintJobRequest{
		PrinterId: printer.GetId(), Document: &documentv1.CreatePrintJobRequest_QuoteId{QuoteId: quote.GetId()},
	})
	require.NoError(t, err)
	assert.Equal(t, documentv1.DocumentType_DOCUMENT_TYPE_QUOTE, job.GetDocumentType())

	got := agent.job()
	assert.Equal(t, job.GetId(), got.ID)
	assert.Equal(t, "XP-80C", got.Printer)
	assert.Equal(t, "escpos", got.Format)
	assert.Equal(t, "K80", got.PaperSize)

	data, err := base64.StdEncoding.DecodeString(got.Data)
	require.NoError(t, err)
	assert.True(t, bytes.HasPrefix(data, escInit))
	assert.True(t, bytes.HasSuffix(data, escCut))

	agent.send(agentMessage{Type: "result", JobID: job.GetId(), Status: "printed"})
	waitForJob(t, api, garageID, job.GetId(), documentv1.PrintJobStatus_PRINT_JOB_STATUS_PRINTED)

	test, err := api.CreatePrintJob(ctx, garageID, &documentv1.CreatePrintJobRequest{
		PrinterId: printer.GetId(), Document: &documentv1.CreatePrintJobRequest_TestPage{TestPage: &documentv1.PrintJobTestPage{}},
	})
	require.NoError(t, err)
	assert.Equal(t, test.GetId(), agent.job().ID)

	agent.send(agentMessage{Type: "result", JobID: test.GetId(), Status: "failed", Error: "Máy in hết giấy"})
	failed := waitForJob(t, api, garageID, test.GetId(), documentv1.PrintJobStatus_PRINT_JOB_STATUS_FAILED)
	assert.Equal(t, "Máy in hết giấy", failed.GetError())

	// A roll takes the quote and the voucher only; a raw printer is the app's.
	_, err = api.CreatePrintJob(ctx, garageID, &documentv1.CreatePrintJobRequest{
		PrinterId: printer.GetId(), Document: &documentv1.CreatePrintJobRequest_RepairOrderId{RepairOrderId: quote.GetId()},
	})
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "VALIDATION_ERROR")

	raw, err := api.CreatePrinter(ctx, garageID, &documentv1.Printer{
		Name: "Mạng", Protocol: documentv1.PrinterProtocol_PRINTER_PROTOCOL_RAW, Host: "192.168.1.50", PaperSize: documentv1.PaperSize_PAPER_SIZE_K80,
	})
	require.NoError(t, err)

	_, err = api.CreatePrintJob(ctx, garageID, &documentv1.CreatePrintJobRequest{
		PrinterId: raw.GetId(), Document: &documentv1.CreatePrintJobRequest_TestPage{TestPage: &documentv1.PrintJobTestPage{}},
	})
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "VALIDATION_ERROR")
}

// Scenario: E2E-DOC-013, Release 13.1, a job sent while the agent is away
// waits for it: it reads queued, and the agent receives it when it connects
// again, also after a connection lost before its result (FR-DOC-08).
func TestAgentQueuesWhileAway(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	k := setup(t, "print-013")
	api, garageID := k.owner.API, k.garageID
	_, key, printer := pairedAgent(t, k, "Quầy")

	job, err := api.CreatePrintJob(ctx, garageID, &documentv1.CreatePrintJobRequest{
		PrinterId: printer.GetId(), Document: &documentv1.CreatePrintJobRequest_TestPage{TestPage: &documentv1.PrintJobTestPage{}},
	})
	require.NoError(t, err)
	assert.Equal(t, documentv1.PrintJobStatus_PRINT_JOB_STATUS_QUEUED, job.GetStatus())

	first, status := connectAgent(t, api, key)
	require.Equal(t, http.StatusSwitchingProtocols, status)
	assert.Equal(t, job.GetId(), first.job().ID)
	waitForJob(t, api, garageID, job.GetId(), documentv1.PrintJobStatus_PRINT_JOB_STATUS_SENT)

	// The connection drops before the agent answers: the job is sent again.
	require.NoError(t, first.c.Close(websocket.StatusGoingAway, "network lost"))

	again, status := connectAgent(t, api, key)
	require.Equal(t, http.StatusSwitchingProtocols, status)
	assert.Equal(t, job.GetId(), again.job().ID)

	again.send(agentMessage{Type: "result", JobID: job.GetId(), Status: "printed"})
	waitForJob(t, api, garageID, job.GetId(), documentv1.PrintJobStatus_PRINT_JOB_STATUS_PRINTED)
}

// Scenario: E2E-DOC-014, Release 13.1, removing a print agent stops it: its
// connection closes as revoked, its key is refused, its printers are gone and
// nothing more can be sent to them (FR-DOC-08).
func TestRemovingAnAgentStopsIt(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	k := setup(t, "print-014")
	api, garageID := k.owner.API, k.garageID
	agentID, key, printer := pairedAgent(t, k, "Quầy")

	agent, status := connectAgent(t, api, key)
	require.Equal(t, http.StatusSwitchingProtocols, status)

	require.NoError(t, api.DeletePrintAgent(ctx, garageID, agentID))
	assert.Equal(t, websocket.StatusCode(4001), agent.closed())

	_, status = connectAgent(t, api, key)
	assert.Equal(t, http.StatusUnauthorized, status)

	printers, err := api.ListPrinters(ctx, garageID)
	require.NoError(t, err)
	assert.Empty(t, printers, "the agent's printers are removed with it")

	_, err = api.CreatePrintJob(ctx, garageID, &documentv1.CreatePrintJobRequest{
		PrinterId: printer.GetId(), Document: &documentv1.CreatePrintJobRequest_TestPage{TestPage: &documentv1.PrintJobTestPage{}},
	})
	fixture.RequireAPIError(t, err, http.StatusNotFound, "")
}
