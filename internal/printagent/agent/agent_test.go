package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/printagent/agent"
	"github.com/viettechno/vietgara-backend/internal/printagent/protocol"
)

var errOutOfPaper = errors.New("out of paper")

// fakeSpooler records what it prints.
type fakeSpooler struct {
	mu      sync.Mutex
	printed []string // "raw:<printer>" or "pdf:<printer>:<paper>"
	fail    error
}

func (f *fakeSpooler) Printers(context.Context) ([]protocol.Printer, error) {
	return []protocol.Printer{{Name: "XP-80C", Default: true}}, nil
}

func (f *fakeSpooler) PrintRaw(_ context.Context, printer, _ string, _ []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.fail != nil {
		return f.fail
	}

	f.printed = append(f.printed, "raw:"+printer)

	return nil
}

func (f *fakeSpooler) PrintPDF(_ context.Context, printer, _, paper string, _ []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.printed = append(f.printed, "pdf:"+printer+":"+paper)

	return nil
}

func (f *fakeSpooler) Printed() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string(nil), f.printed...)
}

// fakeServer is the server's side of the protocol: each connection gets
// hello and the jobs, and its messages are collected.
type fakeServer struct {
	t    *testing.T
	key  string
	jobs []protocol.Job
	// closeWith closes each connection with this code after the jobs.
	closeWith websocket.StatusCode

	mu       sync.Mutex
	messages []protocol.Message
	conns    int
}

func (s *fakeServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer "+s.key {
		http.Error(w, "unauthorized", http.StatusUnauthorized)

		return
	}

	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}

	s.mu.Lock()
	s.conns++
	s.mu.Unlock()

	ctx := r.Context()
	s.send(ctx, c, protocol.Message{Type: protocol.TypeHello, AgentID: "agent-1"})

	for _, j := range s.jobs {
		s.send(ctx, c, protocol.Message{Type: protocol.TypeJob, Job: &j})
	}

	if s.closeWith != 0 {
		_ = c.Close(s.closeWith, "")

		return
	}

	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			return
		}

		var msg protocol.Message
		if json.Unmarshal(data, &msg) == nil {
			s.mu.Lock()
			s.messages = append(s.messages, msg)
			s.mu.Unlock()
		}
	}
}

func (s *fakeServer) send(ctx context.Context, c *websocket.Conn, msg protocol.Message) {
	data, err := json.Marshal(msg)
	require.NoError(s.t, err)

	_ = c.Write(ctx, websocket.MessageText, data)
}

func (s *fakeServer) results() []protocol.Message {
	s.mu.Lock()
	defer s.mu.Unlock()

	var out []protocol.Message

	for _, m := range s.messages {
		if m.Type == protocol.TypeResult {
			out = append(out, m)
		}
	}

	return out
}

func (s *fakeServer) printersReported() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, m := range s.messages {
		if m.Type == protocol.TypePrinters && len(m.Printers) == 1 && m.Version == "test" {
			return true
		}
	}

	return false
}

func (s *fakeServer) connections() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.conns
}

func newAgent(t *testing.T, server *httptest.Server, key string, spooler agent.Spooler) (*agent.Agent, *agent.Store) {
	t.Helper()

	store := &agent.Store{Path: filepath.Join(t.TempDir(), "state.json")}
	require.NoError(t, store.Save(agent.State{Server: server.URL, AgentID: "agent-1", Key: key}))

	return &agent.Agent{
		State: store, Spooler: spooler, Version: "test", Log: slog.New(slog.DiscardHandler),
		MinBackoff: 10 * time.Millisecond, MaxBackoff: 50 * time.Millisecond, RepairEvery: 20 * time.Millisecond,
	}, store
}

func start(t *testing.T, a *agent.Agent) (stop func() error) {
	t.Helper()

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)

	go func() { done <- a.Run(ctx) }()

	return func() error {
		cancel()

		return <-done
	}
}

func TestAgentPrintsItsJobs(t *testing.T) {
	t.Parallel()

	srv := &fakeServer{t: t, key: "vgagent_k", jobs: []protocol.Job{
		{ID: "j1", Printer: "XP-80C", Format: protocol.FormatEscPos, PaperSize: "K80", Data: []byte{0x1b, 0x40}},
		{ID: "j2", Printer: "HP", Format: protocol.FormatPDF, PaperSize: "A5", Data: []byte("%PDF")},
		{ID: "j3", Printer: "HP", Format: "zpl", Data: []byte("x")},
	}}
	server := httptest.NewServer(srv)
	t.Cleanup(server.Close)

	spooler := &fakeSpooler{}
	a, _ := newAgent(t, server, "vgagent_k", spooler)
	stop := start(t, a)

	require.Eventually(t, func() bool { return len(srv.results()) == 3 }, 5*time.Second, 10*time.Millisecond)
	require.NoError(t, stop())

	assert.Equal(t, []string{"raw:XP-80C", "pdf:HP:A5"}, spooler.Printed())

	results := srv.results()
	assert.Equal(t, protocol.StatusPrinted, results[0].Status)
	assert.Equal(t, protocol.StatusPrinted, results[1].Status)
	assert.Equal(t, protocol.StatusFailed, results[2].Status, "an unknown format is not printed")
	assert.Contains(t, results[2].Error, "update it")
	assert.True(t, srv.printersReported(), "the PC's printers are reported")
}

func TestAgentPrintsAJobOnce(t *testing.T) {
	t.Parallel()

	// The server sends the same job on every connection and closes it, as
	// after a lost connection before the result arrived.
	srv := &fakeServer{t: t, key: "vgagent_k", closeWith: websocket.StatusGoingAway, jobs: []protocol.Job{
		{ID: "j1", Printer: "XP-80C", Format: protocol.FormatEscPos, Data: []byte{0x1b, 0x40}},
	}}
	server := httptest.NewServer(srv)
	t.Cleanup(server.Close)

	spooler := &fakeSpooler{}
	a, store := newAgent(t, server, "vgagent_k", spooler)
	stop := start(t, a)

	require.Eventually(t, func() bool { return srv.connections() >= 3 }, 5*time.Second, 10*time.Millisecond)
	require.NoError(t, stop())

	assert.Equal(t, []string{"raw:XP-80C"}, spooler.Printed(), "printed once over the reconnections")
	assert.True(t, store.Printed("j1"))
}

func TestAgentReportsAFailedPrint(t *testing.T) {
	t.Parallel()

	srv := &fakeServer{t: t, key: "vgagent_k", jobs: []protocol.Job{
		{ID: "j1", Printer: "XP-80C", Format: protocol.FormatEscPos, Data: []byte{0x1b, 0x40}},
	}}
	server := httptest.NewServer(srv)
	t.Cleanup(server.Close)

	a, store := newAgent(t, server, "vgagent_k", &fakeSpooler{fail: errOutOfPaper})
	stop := start(t, a)

	require.Eventually(t, func() bool { return len(srv.results()) == 1 }, 5*time.Second, 10*time.Millisecond)
	require.NoError(t, stop())

	assert.Equal(t, protocol.StatusFailed, srv.results()[0].Status)
	assert.Equal(t, "out of paper", srv.results()[0].Error)
	assert.False(t, store.Printed("j1"), "a failed job may be printed when sent again")
}

func TestARevokedAgentWaitsForANewPairing(t *testing.T) {
	t.Parallel()

	srv := &fakeServer{t: t, key: "vgagent_new"}
	server := httptest.NewServer(srv)
	t.Cleanup(server.Close)

	a, store := newAgent(t, server, "vgagent_old", &fakeSpooler{})
	stop := start(t, a)

	time.Sleep(100 * time.Millisecond)
	assert.Zero(t, srv.connections(), "the old key is refused and not retried in a loop")

	st, err := store.Load()
	require.NoError(t, err)

	st.Key = "vgagent_new"
	require.NoError(t, store.Save(st))

	require.Eventually(t, func() bool { return srv.connections() == 1 }, 5*time.Second, 10*time.Millisecond)
	require.NoError(t, stop())
}

func TestCheckServer(t *testing.T) {
	t.Parallel()

	for _, ok := range []string{"https://app.vietgara.vn", "https://app.vietgara.vn/", "http://localhost:8000", "http://127.0.0.1:8000"} {
		_, err := agent.CheckServer(ok)
		require.NoError(t, err, ok)
	}

	for _, bad := range []string{"", "app.vietgara.vn", "http://app.vietgara.vn", "ftp://x", "https://x?y=1"} {
		_, err := agent.CheckServer(bad)
		require.Error(t, err, bad)
	}
}

func TestPair(t *testing.T) {
	t.Parallel()

	var got map[string]string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != protocol.PairPath {
			http.NotFound(w, r)

			return
		}

		_ = json.NewDecoder(r.Body).Decode(&got)

		if got["pairingCode"] != "ABCD-EFGH" {
			http.Error(w, `{"code":"NOT_FOUND"}`, http.StatusNotFound)

			return
		}

		_, _ = w.Write([]byte(`{"agentKey":"vgagent_k","agentId":"a1","agentName":"Quầy","garageName":"Gara An"}`))
	}))
	t.Cleanup(server.Close)

	store := &agent.Store{Path: filepath.Join(t.TempDir(), "agent", "state.json")}

	st, err := agent.Pair(t.Context(), store, server.URL, " ABCD-EFGH ", "0.1.0")
	require.NoError(t, err)
	assert.Equal(t, "Quầy", st.AgentName)
	assert.Equal(t, "0.1.0", got["version"])

	saved, err := store.Load()
	require.NoError(t, err)
	assert.Equal(t, "vgagent_k", saved.Key)

	_, err = agent.Pair(t.Context(), store, server.URL, "WRONG", "0.1.0")
	require.ErrorIs(t, err, agent.ErrPairingCode)
}
