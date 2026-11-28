package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"

	"github.com/viettechno/vietgara-backend/internal/document/usecase"
	"github.com/viettechno/vietgara-backend/internal/printagent/protocol"
)

// errHubClosed closes the connections when the server shuts down.
var errHubClosed = errors.New("the server is shutting down")

// AgentHub serves the print agents' WebSocket connections (FR-DOC-08,
// Release 13.1) and implements usecase.AgentNotifier. The jobs live in the
// database: a connection sends what is pending when it starts, when a job of
// its agent is queued on this instance, and on every tick, which also catches
// the jobs queued on another instance and a key revoked there.
type AgentHub struct {
	uc *usecase.DocumentUseCase

	// Tick is how often a connection beats, pings the agent and looks for
	// jobs; WriteTimeout bounds each message. Set before serving.
	Tick         time.Duration
	WriteTimeout time.Duration

	mu     sync.Mutex
	conns  map[string]*agentConn
	closed bool
}

type agentConn struct {
	wake   chan struct{}
	cancel context.CancelCauseFunc
}

// NewAgentHub returns a hub for the use case's agents.
func NewAgentHub(uc *usecase.DocumentUseCase) *AgentHub {
	return &AgentHub{uc: uc, Tick: 15 * time.Second, WriteTimeout: time.Minute, conns: map[string]*agentConn{}}
}

// JobQueued wakes the agent's connection, if this instance holds it.
func (h *AgentHub) JobQueued(agentID string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if c := h.conns[agentID]; c != nil {
		select {
		case c.wake <- struct{}{}:
		default: // already woken
		}
	}
}

// AgentRevoked closes the agent's connection, if this instance holds it.
func (h *AgentHub) AgentRevoked(agentID string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if c := h.conns[agentID]; c != nil {
		c.cancel(usecase.ErrAgentRevoked)
	}
}

// Close closes every connection (the agents reconnect to another instance or
// after the restart) and refuses new ones.
func (h *AgentHub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.closed = true

	for _, c := range h.conns {
		c.cancel(errHubClosed)
	}
}

// ServeHTTP accepts an agent's connection: "Authorization: Bearer <key>".
func (h *AgentHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	key, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)

		return
	}

	s, err := h.uc.AuthenticateAgent(r.Context(), strings.TrimSpace(key), uuid.NewString())
	if errors.Is(err, usecase.ErrAgentRevoked) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)

		return
	}

	if err != nil {
		slog.ErrorContext(r.Context(), "print agent authentication failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)

		return
	}

	// The agent is not a browser: it sends no Origin, and one that does is
	// refused by Accept unless it is the server's own host.
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		h.forget(r.Context(), s)

		return
	}

	h.serve(r.Context(), c, s)
}

func (h *AgentHub) serve(parent context.Context, c *websocket.Conn, s usecase.AgentSession) {
	ctx, cancel := context.WithCancelCause(parent)
	defer cancel(nil)

	conn := &agentConn{wake: make(chan struct{}, 1), cancel: cancel}
	if !h.register(s.AgentID, conn) {
		_ = c.Close(websocket.StatusGoingAway, "shutting down")

		return
	}

	defer h.unregister(s.AgentID, conn)
	defer h.forget(ctx, s)

	log := slog.With("agent_id", s.AgentID, "garage_id", s.GarageID)
	log.InfoContext(ctx, "print agent connected")

	c.SetReadLimit(protocol.MaxAgentMessage)

	// The reader runs on the request's context, not on ctx: cancelling a
	// read closes the connection at once, and the close below must still
	// send its code. Close ends the read.
	go h.read(parent, cancel, c, s)

	err := h.run(ctx, c, s, conn.wake)

	code, reason := websocket.StatusInternalError, "internal error"

	switch {
	case errors.Is(err, usecase.ErrAgentRevoked):
		code, reason = protocol.CloseRevoked, "revoked"
	case errors.Is(err, usecase.ErrAgentReplaced):
		code, reason = protocol.CloseReplaced, "replaced"
	case errors.Is(err, errHubClosed):
		code, reason = websocket.StatusGoingAway, "shutting down"
	case websocket.CloseStatus(err) != -1, errors.Is(err, context.Canceled):
		code, reason = websocket.StatusNormalClosure, ""
	}

	_ = c.Close(code, reason)

	log.InfoContext(ctx, "print agent disconnected", "reason", err)
}

// run is the connection's writer: hello, then the jobs as they come, a
// heartbeat and a ping on every tick. It returns why the connection ends.
func (h *AgentHub) run(ctx context.Context, c *websocket.Conn, s usecase.AgentSession, wake <-chan struct{}) error {
	if err := h.write(ctx, c, protocol.Message{Type: protocol.TypeHello, AgentID: s.AgentID}); err != nil {
		return err
	}

	ticker := time.NewTicker(h.Tick)
	defer ticker.Stop()

	for {
		if err := h.deliver(ctx, c, s); err != nil {
			return err
		}

		select {
		case <-ctx.Done():
			return context.Cause(ctx)
		case <-wake:
		case <-ticker.C:
			if err := h.uc.AgentHeartbeat(ctx, s); err != nil {
				return err
			}

			pingCtx, cancel := context.WithTimeout(ctx, h.WriteTimeout)
			err := c.Ping(pingCtx)

			cancel()

			if err != nil {
				return err
			}
		}
	}
}

// deliver sends the agent's pending jobs, a batch at a time.
func (h *AgentHub) deliver(ctx context.Context, c *websocket.Conn, s usecase.AgentSession) error {
	for {
		jobs, err := h.uc.AgentJobs(ctx, s)
		if err != nil || len(jobs) == 0 {
			return err
		}

		ids := make([]string, 0, len(jobs))

		for _, j := range jobs {
			msg := protocol.Message{Type: protocol.TypeJob, Job: &protocol.Job{
				ID: j.ID, Printer: j.Queue, Format: string(j.Format), PaperSize: string(j.Paper), Title: j.JobTitle(), Data: j.Data,
			}}
			if err := h.write(ctx, c, msg); err != nil {
				return err
			}

			ids = append(ids, j.ID)
		}

		if err := h.uc.AgentJobsSent(ctx, s, ids); err != nil {
			return err
		}

		if len(jobs) < usecase.AgentJobBatch {
			return nil
		}
	}
}

// read handles the agent's messages until the connection fails.
func (h *AgentHub) read(ctx context.Context, cancel context.CancelCauseFunc, c *websocket.Conn, s usecase.AgentSession) {
	for {
		typ, data, err := c.Read(ctx)
		if err != nil {
			cancel(err)

			return
		}

		if typ != websocket.MessageText {
			continue
		}

		var msg protocol.Message
		if err := json.Unmarshal(data, &msg); err != nil {
			continue // not ours: ignored
		}

		if err := h.handle(ctx, s, msg); err != nil {
			cancel(err)

			return
		}
	}
}

func (h *AgentHub) handle(ctx context.Context, s usecase.AgentSession, msg protocol.Message) error {
	switch msg.Type {
	case protocol.TypePrinters:
		report := usecase.AgentReport{Hostname: msg.Hostname, OS: msg.OS, Version: msg.Version}
		for _, p := range msg.Printers {
			report.Printers = append(report.Printers, usecase.AgentPrinter{Name: p.Name, Default: p.Default})
		}

		err := h.uc.AgentReported(ctx, s, report)
		if errors.Is(err, usecase.ErrAgentRevoked) {
			return err
		}

		if err != nil {
			slog.WarnContext(ctx, "print agent report refused", "agent_id", s.AgentID, "error", err)
		}

		return nil
	case protocol.TypeResult:
		return h.uc.AgentJobDone(ctx, s, msg.JobID, msg.Status == protocol.StatusPrinted, msg.Error)
	default:
		return nil // a message of a newer agent: ignored
	}
}

func (h *AgentHub) write(ctx context.Context, c *websocket.Conn, msg protocol.Message) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err //nolint:wrapcheck // a message always encodes
	}

	ctx, cancel := context.WithTimeout(ctx, h.WriteTimeout)
	defer cancel()

	return c.Write(ctx, websocket.MessageText, data) //nolint:wrapcheck // the connection's end
}

// register makes conn the agent's connection, closing an older one.
func (h *AgentHub) register(agentID string, conn *agentConn) bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		return false
	}

	if old := h.conns[agentID]; old != nil {
		old.cancel(usecase.ErrAgentReplaced)
	}

	h.conns[agentID] = conn

	return true
}

func (h *AgentHub) unregister(agentID string, conn *agentConn) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.conns[agentID] == conn {
		delete(h.conns, agentID)
	}
}

// forget clears the connection in the database, even when ctx is done.
func (h *AgentHub) forget(ctx context.Context, s usecase.AgentSession) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	if err := h.uc.AgentDisconnected(ctx, s); err != nil && !errors.Is(err, usecase.ErrAgentRevoked) {
		slog.WarnContext(ctx, "print agent disconnect not recorded", "agent_id", s.AgentID, "error", err)
	}
}
