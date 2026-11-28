package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	documentv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/document/v1"

	"github.com/viettechno/vietgara-backend/internal/document/agenttest"
	"github.com/viettechno/vietgara-backend/internal/document/handler"
	"github.com/viettechno/vietgara-backend/internal/document/printmodel"
	"github.com/viettechno/vietgara-backend/internal/document/usecase"
	"github.com/viettechno/vietgara-backend/internal/platform/access"
	"github.com/viettechno/vietgara-backend/internal/printagent/protocol"
)

type agentHubEnv struct {
	srv      *handler.Server
	agents   *handler.AgentServer
	hub      *handler.AgentHub
	printers *memPrinters
	store    *agenttest.Store
	http     *httptest.Server
}

func newAgentHubEnv(t *testing.T) *agentHubEnv {
	t.Helper()

	e := newEnv(t)
	e.garages.EXPECT().Get(anyArg, garageID).Return(defaultGarage(), nil).Maybe()

	printers := &memPrinters{}
	uc := usecase.NewDocumentUseCase(e.quotes, e.repairOrders, e.settlements, e.stockMovements, e.revenue, e.garages, e.inventory, e.prints).
		WithPrinting(printers, stubParts{})
	store := agenttest.NewStore(time.Now)
	hub := handler.NewAgentHub(uc)
	hub.Tick = 50 * time.Millisecond
	uc.WithAgents(store, store, agenttest.PassTx{}, hub)

	server := httptest.NewServer(hub)

	t.Cleanup(func() {
		hub.Close()
		server.Close()
	})

	return &agentHubEnv{
		srv: handler.NewServer(uc), agents: handler.NewAgentServer(uc), hub: hub, printers: printers, store: store, http: server,
	}
}

func member(t *testing.T) context.Context {
	t.Helper()

	return access.WithAccess(t.Context(), &access.Access{GarageID: garageID, Active: true, Owner: true})
}

// pair creates and pairs an agent through the RPCs, with an agent printer.
func (e *agentHubEnv) pair(t *testing.T) (agentID, key, printerID string) {
	t.Helper()

	created, err := e.srv.CreatePrintAgent(member(t), &documentv1.CreatePrintAgentRequest{
		GarageId: garageID, Agent: &documentv1.PrintAgent{Name: "Quầy"},
	})
	require.NoError(t, err)

	paired, err := e.agents.PairPrintAgent(t.Context(), &documentv1.PairPrintAgentRequest{PairingCode: created.GetPairingCode(), Hostname: "QUAY-PC"})
	require.NoError(t, err)
	assert.Equal(t, "Quầy", paired.GetAgentName())

	printer, err := e.srv.CreatePrinter(member(t), &documentv1.CreatePrinterRequest{GarageId: garageID, Printer: &documentv1.Printer{
		Name: "Quầy thu ngân", Protocol: documentv1.PrinterProtocol_PRINTER_PROTOCOL_AGENT, AgentId: paired.GetAgentId(),
		Queue: "XP-80C", PaperSize: documentv1.PaperSize_PAPER_SIZE_K80,
	}})
	require.NoError(t, err)
	assert.Equal(t, documentv1.PrinterProtocol_PRINTER_PROTOCOL_AGENT, printer.GetProtocol())
	assert.Equal(t, "XP-80C", printer.GetQueue())

	return paired.GetAgentId(), paired.GetAgentKey(), printer.GetId()
}

// dial connects as the agent; status is the HTTP status of the answer.
func (e *agentHubEnv) dial(t *testing.T, key string) (*websocket.Conn, int, error) {
	t.Helper()

	header := http.Header{}
	if key != "" {
		header.Set("Authorization", "Bearer "+key)
	}

	c, resp, err := websocket.Dial(t.Context(), "ws"+strings.TrimPrefix(e.http.URL, "http"), &websocket.DialOptions{HTTPHeader: header})

	code := 0
	if resp != nil {
		code = resp.StatusCode
		if resp.Body != nil {
			_ = resp.Body.Close()
		}
	}

	if c != nil {
		c.SetReadLimit(protocol.MaxServerMessage)
		t.Cleanup(func() { _ = c.CloseNow() })
	}

	return c, code, err
}

func readMessage(t *testing.T, c *websocket.Conn) protocol.Message {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	_, data, err := c.Read(ctx)
	require.NoError(t, err)

	var msg protocol.Message
	require.NoError(t, json.Unmarshal(data, &msg))

	return msg
}

func send(t *testing.T, c *websocket.Conn, msg protocol.Message) {
	t.Helper()

	data, err := json.Marshal(msg)
	require.NoError(t, err)
	require.NoError(t, c.Write(t.Context(), websocket.MessageText, data))
}

func (e *agentHubEnv) jobStatus(t *testing.T, id string) documentv1.PrintJobStatus {
	t.Helper()

	job, err := e.srv.GetPrintJob(member(t), &documentv1.GetPrintJobRequest{GarageId: garageID, JobId: id})
	require.NoError(t, err)

	return job.GetStatus()
}

func TestAgentConnection(t *testing.T) {
	t.Parallel()

	t.Run("a job queued while the agent is away is sent when it connects, and printed", func(t *testing.T) {
		t.Parallel()

		e := newAgentHubEnv(t)
		agentID, key, printerID := e.pair(t)

		job, err := e.srv.CreatePrintJob(member(t), &documentv1.CreatePrintJobRequest{
			GarageId: garageID, PrinterId: printerID, Document: &documentv1.CreatePrintJobRequest_TestPage{TestPage: &documentv1.PrintJobTestPage{}},
		})
		require.NoError(t, err)
		assert.Equal(t, documentv1.PrintJobStatus_PRINT_JOB_STATUS_QUEUED, job.GetStatus())
		assert.Equal(t, "Quầy thu ngân", job.GetPrinterName())

		c, _, err := e.dial(t, key)
		require.NoError(t, err)

		hello := readMessage(t, c)
		assert.Equal(t, protocol.TypeHello, hello.Type)
		assert.Equal(t, agentID, hello.AgentID)

		msg := readMessage(t, c)
		require.Equal(t, protocol.TypeJob, msg.Type)
		assert.Equal(t, job.GetId(), msg.Job.ID)
		assert.Equal(t, "XP-80C", msg.Job.Printer)
		assert.Equal(t, protocol.FormatEscPos, msg.Job.Format)
		assert.Equal(t, "K80", msg.Job.PaperSize)
		assert.Equal(t, []byte{0x1b, 0x40}, msg.Job.Data[:2])

		send(t, c, protocol.Message{
			Type: protocol.TypePrinters, Hostname: "QUAY-PC", OS: "windows/amd64", Version: "0.1.0",
			Printers: []protocol.Printer{{Name: "XP-80C", Default: true}, {Name: "HP LaserJet"}},
		})
		send(t, c, protocol.Message{Type: protocol.TypeResult, JobID: job.GetId(), Status: protocol.StatusPrinted})

		require.Eventually(t, func() bool {
			return e.jobStatus(t, job.GetId()) == documentv1.PrintJobStatus_PRINT_JOB_STATUS_PRINTED
		}, 5*time.Second, 20*time.Millisecond)

		agents, err := e.srv.ListPrintAgents(member(t), &documentv1.ListPrintAgentsRequest{GarageId: garageID})
		require.NoError(t, err)
		require.Len(t, agents.GetAgents(), 1)
		assert.True(t, agents.GetAgents()[0].GetConnected())
		assert.Equal(t, "0.1.0", agents.GetAgents()[0].GetVersion())
		assert.Len(t, agents.GetAgents()[0].GetPrinters(), 2)
	})

	t.Run("a job queued while the agent is connected is sent at once", func(t *testing.T) {
		t.Parallel()

		e := newAgentHubEnv(t)
		_, key, printerID := e.pair(t)

		c, _, err := e.dial(t, key)
		require.NoError(t, err)
		assert.Equal(t, protocol.TypeHello, readMessage(t, c).Type)

		job, err := e.srv.CreatePrintJob(member(t), &documentv1.CreatePrintJobRequest{
			GarageId: garageID, PrinterId: printerID, Document: &documentv1.CreatePrintJobRequest_TestPage{TestPage: &documentv1.PrintJobTestPage{}},
		})
		require.NoError(t, err)

		msg := readMessage(t, c)
		assert.Equal(t, job.GetId(), msg.Job.ID)

		send(t, c, protocol.Message{Type: protocol.TypeResult, JobID: job.GetId(), Status: protocol.StatusFailed, Error: "Máy in hết giấy"})

		require.Eventually(t, func() bool {
			return e.jobStatus(t, job.GetId()) == documentv1.PrintJobStatus_PRINT_JOB_STATUS_FAILED
		}, 5*time.Second, 20*time.Millisecond)

		got, err := e.srv.GetPrintJob(member(t), &documentv1.GetPrintJobRequest{GarageId: garageID, JobId: job.GetId()})
		require.NoError(t, err)
		assert.Equal(t, "Máy in hết giấy", got.GetError())
	})
}

func TestAgentConnectionRefused(t *testing.T) {
	t.Parallel()

	t.Run("no key or a wrong key is refused before the upgrade", func(t *testing.T) {
		t.Parallel()

		e := newAgentHubEnv(t)

		for _, key := range []string{"", "vgagent_wrong"} {
			_, code, err := e.dial(t, key)
			require.Error(t, err)
			assert.Equal(t, http.StatusUnauthorized, code)
		}
	})

	t.Run("revoking the agent closes its connection with the revoked code", func(t *testing.T) {
		t.Parallel()

		e := newAgentHubEnv(t)
		agentID, key, _ := e.pair(t)

		c, _, err := e.dial(t, key)
		require.NoError(t, err)
		assert.Equal(t, protocol.TypeHello, readMessage(t, c).Type)

		_, err = e.srv.DeletePrintAgent(member(t), &documentv1.DeletePrintAgentRequest{GarageId: garageID, AgentId: agentID})
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()

		_, _, err = c.Read(ctx)
		assert.Equal(t, websocket.StatusCode(protocol.CloseRevoked), websocket.CloseStatus(err))

		_, code, err := e.dial(t, key)
		require.Error(t, err)
		assert.Equal(t, http.StatusUnauthorized, code)
	})

	t.Run("a second connection of the agent replaces the first", func(t *testing.T) {
		t.Parallel()

		e := newAgentHubEnv(t)
		_, key, _ := e.pair(t)

		first, _, err := e.dial(t, key)
		require.NoError(t, err)
		assert.Equal(t, protocol.TypeHello, readMessage(t, first).Type)

		second, _, err := e.dial(t, key)
		require.NoError(t, err)
		assert.Equal(t, protocol.TypeHello, readMessage(t, second).Type)

		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()

		_, _, err = first.Read(ctx)
		assert.Equal(t, websocket.StatusCode(protocol.CloseReplaced), websocket.CloseStatus(err))
	})
}

func TestPrintJobRPCs(t *testing.T) {
	t.Parallel()

	t.Run("a print job needs a document and an agent printer", func(t *testing.T) {
		t.Parallel()

		e := newAgentHubEnv(t)
		e.printers.printers = append(e.printers.printers, usecase.Printer{ID: "raw", Protocol: usecase.ProtocolRaw, Paper: printmodel.K80})

		_, err := e.srv.CreatePrintJob(member(t), &documentv1.CreatePrintJobRequest{GarageId: garageID, PrinterId: "raw"})
		assert.Equal(t, codes.InvalidArgument, status.Code(err))

		_, err = e.srv.CreatePrintJob(member(t), &documentv1.CreatePrintJobRequest{
			GarageId: garageID, PrinterId: "raw", Document: &documentv1.CreatePrintJobRequest_TestPage{TestPage: &documentv1.PrintJobTestPage{}},
		})
		assert.Equal(t, codes.InvalidArgument, status.Code(err))

		_, err = e.srv.GetPrintJob(member(t), &documentv1.GetPrintJobRequest{GarageId: garageID, JobId: "nope"})
		assert.Equal(t, codes.NotFound, status.Code(err))
	})

	t.Run("a wrong pairing code is not found and a new code revokes the key", func(t *testing.T) {
		t.Parallel()

		e := newAgentHubEnv(t)
		agentID, key, _ := e.pair(t)

		_, err := e.agents.PairPrintAgent(t.Context(), &documentv1.PairPrintAgentRequest{PairingCode: "ABCD-EFGH"})
		assert.Equal(t, codes.NotFound, status.Code(err))

		issued, err := e.srv.IssuePrintAgentPairingCode(member(t), &documentv1.IssuePrintAgentPairingCodeRequest{GarageId: garageID, AgentId: agentID})
		require.NoError(t, err)
		assert.False(t, issued.GetAgent().GetPaired())
		assert.NotNil(t, issued.GetAgent().GetPairingExpiresAt())

		_, code, err := e.dial(t, key)
		require.Error(t, err)
		assert.Equal(t, http.StatusUnauthorized, code)
	})
}

func TestAgentPolicies(t *testing.T) {
	t.Parallel()

	p := handler.Policies()
	assert.Equal(t, access.GarageWrite, p[documentv1.DocumentService_CreatePrintAgent_FullMethodName].Garage.Permission)
	assert.Equal(t, access.GarageWrite, p[documentv1.DocumentService_IssuePrintAgentPairingCode_FullMethodName].Garage.Permission)
	assert.Equal(t, access.GarageWrite, p[documentv1.DocumentService_DeletePrintAgent_FullMethodName].Garage.Permission)
	assert.Empty(t, p[documentv1.DocumentService_CreatePrintJob_FullMethodName].Garage.Permission, "checked per document by the use case")
	assert.True(t, p[documentv1.PrintAgentService_PairPrintAgent_FullMethodName].Public)
}
