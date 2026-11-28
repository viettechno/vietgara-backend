package client

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"

	documentv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/document/v1"
)

// ---- Print agents and print jobs (Release 13.1) ------------------------------

func agentPath(garageID, agentID string) string {
	return garagePath(garageID) + "/print-agents/" + url.PathEscape(agentID)
}

// ListPrintAgents lists the garage's print agents.
func (c *Client) ListPrintAgents(ctx context.Context, garageID string) ([]*documentv1.PrintAgent, error) {
	resp, err := call[documentv1.ListPrintAgentsResponse](ctx, c, http.MethodGet, garagePath(garageID)+"/print-agents", nil)

	return resp.GetAgents(), err
}

// CreatePrintAgent adds a print agent and returns its pairing code.
func (c *Client) CreatePrintAgent(ctx context.Context, garageID, name string) (*documentv1.CreatePrintAgentResponse, error) {
	return call[documentv1.CreatePrintAgentResponse](ctx, c, http.MethodPost, garagePath(garageID)+"/print-agents",
		&documentv1.PrintAgent{Name: name})
}

// IssuePrintAgentPairingCode gives an agent a new pairing code.
func (c *Client) IssuePrintAgentPairingCode(ctx context.Context, garageID, agentID string) (*documentv1.CreatePrintAgentResponse, error) {
	return call[documentv1.CreatePrintAgentResponse](ctx, c, http.MethodPost, agentPath(garageID, agentID)+"/pairing-code", nil)
}

// DeletePrintAgent removes an agent.
func (c *Client) DeletePrintAgent(ctx context.Context, garageID, agentID string) error {
	_, err := call[documentv1.DeletePrintAgentResponse](ctx, c, http.MethodDelete, agentPath(garageID, agentID), nil)

	return err
}

// PairPrintAgent is the agent's public pairing call.
func (c *Client) PairPrintAgent(ctx context.Context, req *documentv1.PairPrintAgentRequest) (*documentv1.PairPrintAgentResponse, error) {
	return call[documentv1.PairPrintAgentResponse](ctx, c, http.MethodPost, "/api/v1/public/print-agents:pair", req)
}

// CreatePrintJob prints a document on an agent printer, with a new
// Idempotency-Key unless the client carries one.
func (c *Client) CreatePrintJob(ctx context.Context, garageID string, req *documentv1.CreatePrintJobRequest) (*documentv1.PrintJob, error) {
	if _, ok := c.headers["Idempotency-Key"]; !ok {
		c = c.WithIdempotencyKey(uuid.NewString())
	}

	return call[documentv1.PrintJob](ctx, c, http.MethodPost, garagePath(garageID)+"/print-jobs", req)
}

// GetPrintJob returns a print job.
func (c *Client) GetPrintJob(ctx context.Context, garageID, jobID string) (*documentv1.PrintJob, error) {
	return call[documentv1.PrintJob](ctx, c, http.MethodGet, garagePath(garageID)+"/print-jobs/"+url.PathEscape(jobID), nil)
}

// AgentConnectURL is the print agents' WebSocket on this client's gateway.
func (c *Client) AgentConnectURL() string {
	return "ws" + strings.TrimPrefix(c.baseURL, "http") + "/api/v1/print-agents/connect"
}
