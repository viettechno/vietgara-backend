package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/viettechno/vietgara-backend/internal/printagent/protocol"
)

// Errors that stop a connection for longer than a reconnection.
var (
	// ErrRevoked: the server refused the key; the agent waits for a new
	// pairing.
	ErrRevoked = errors.New("the agent's key was revoked: pair it again")
	// ErrReplaced: another copy of the agent connected with the same key.
	ErrReplaced = errors.New("another copy of the agent is connected with the same key")
	// ErrFormat is a job of a format this agent does not know.
	ErrFormat = errors.New("this agent cannot print the job's format: update it")
	// ErrServer is an address of VietGara that is not https.
	ErrServer = errors.New("the server address must be https://… (http only on this machine)")
)

// Spooler is the PC's printing: its printers, ESC/POS bytes sent raw, and a
// PDF printed on its page size without scaling.
type Spooler interface {
	Printers(ctx context.Context) ([]protocol.Printer, error)
	PrintRaw(ctx context.Context, printer, title string, data []byte) error
	PrintPDF(ctx context.Context, printer, title, paper string, data []byte) error
}

// Agent connects to VietGara and prints its jobs.
type Agent struct {
	State   *Store
	Spooler Spooler
	Version string
	Log     *slog.Logger

	// The pace of the agent; the defaults when zero.
	MinBackoff, MaxBackoff time.Duration
	// PrintersEvery is how often the PC's printers are listed again.
	PrintersEvery time.Duration
	// RepairEvery is how often a revoked agent reads its state again,
	// waiting for a new pairing.
	RepairEvery time.Duration
	// PrintTimeout bounds one job.
	PrintTimeout time.Duration
}

func (a *Agent) defaults() {
	if a.MinBackoff == 0 {
		a.MinBackoff = time.Second
	}

	if a.MaxBackoff == 0 {
		a.MaxBackoff = time.Minute
	}

	if a.PrintersEvery == 0 {
		a.PrintersEvery = time.Minute
	}

	if a.RepairEvery == 0 {
		a.RepairEvery = 30 * time.Second
	}

	if a.PrintTimeout == 0 {
		a.PrintTimeout = 2 * time.Minute
	}

	if a.Log == nil {
		a.Log = slog.Default()
	}
}

// Run keeps the agent connected until ctx ends, reconnecting with a growing
// pause after a failure. A revoked key waits until the state file holds a
// new one (the agent was paired again), so the service never has to restart.
func (a *Agent) Run(ctx context.Context) error {
	a.defaults()

	backoff := a.MinBackoff

	for {
		st, err := a.State.Load()
		if err != nil {
			return err
		}

		started := time.Now()
		err = a.connect(ctx, st)

		if ctx.Err() != nil {
			return nil //nolint:nilerr // stopped: the connection's error does not matter
		}

		switch {
		case errors.Is(err, ErrRevoked):
			a.Log.Error("the key was refused; pair the agent again with a new pairing code", "agent", st.AgentName)

			if !a.waitForNewKey(ctx, st.Key) {
				return nil // ctx ended
			}

			backoff = a.MinBackoff

			continue
		case errors.Is(err, ErrReplaced):
			a.Log.Warn("another copy of the agent took over the connection; stop one of them")

			backoff = a.MaxBackoff
		case err != nil:
			a.Log.Warn("disconnected", "error", err, "retry_in", backoff)
		}

		if time.Since(started) > a.MaxBackoff {
			backoff = a.MinBackoff // it was connected for a while: start over
		}

		if !sleep(ctx, jitter(backoff)) {
			return nil
		}

		backoff = min(backoff*2, a.MaxBackoff)
	}
}

// waitForNewKey waits until the state holds another key; false when ctx
// ends first.
func (a *Agent) waitForNewKey(ctx context.Context, revoked string) bool {
	for {
		if !sleep(ctx, a.RepairEvery) {
			return false
		}

		if st, err := a.State.Load(); err == nil && st.Key != revoked {
			return true
		}
	}
}

// connect serves one connection until it fails.
func (a *Agent) connect(ctx context.Context, st State) error {
	endpoint, err := connectURL(st.Server)
	if err != nil {
		return err
	}

	c, resp, err := websocket.Dial(ctx, endpoint, &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": {"Bearer " + st.Key}, "User-Agent": {"vietgara-agent/" + a.Version}},
	})
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}

	if resp != nil && resp.StatusCode == http.StatusUnauthorized {
		return ErrRevoked
	}

	if err != nil {
		return fmt.Errorf("connect to %s: %w", st.Server, err)
	}

	defer func() { _ = c.CloseNow() }()

	c.SetReadLimit(protocol.MaxServerMessage)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	jobs := make(chan protocol.Job, 32)

	var wg sync.WaitGroup

	wg.Go(func() { a.work(ctx, c, jobs) })
	wg.Go(func() { a.watchPrinters(ctx, c) })

	err = a.read(ctx, c, jobs)

	cancel()
	close(jobs)
	wg.Wait()

	if code := websocket.CloseStatus(err); code == protocol.CloseRevoked {
		return ErrRevoked
	} else if code == protocol.CloseReplaced {
		return ErrReplaced
	}

	return err
}

// read receives the server's messages and queues the jobs for the worker,
// so that the pings are answered while a job prints.
func (a *Agent) read(ctx context.Context, c *websocket.Conn, jobs chan<- protocol.Job) error {
	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			return err //nolint:wrapcheck // the connection's end, read by connect
		}

		var msg protocol.Message
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}

		switch msg.Type {
		case protocol.TypeHello:
			a.Log.Info("connected", "agent_id", msg.AgentID)
		case protocol.TypeJob:
			if msg.Job == nil {
				continue
			}

			select {
			case jobs <- *msg.Job:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
}

// work prints the jobs one at a time and sends each result.
func (a *Agent) work(ctx context.Context, c *websocket.Conn, jobs <-chan protocol.Job) {
	for job := range jobs {
		result := protocol.Message{Type: protocol.TypeResult, JobID: job.ID, Status: protocol.StatusPrinted}

		if !a.State.Printed(job.ID) {
			if err := a.print(ctx, job); err != nil {
				a.Log.Warn("print failed", "job_id", job.ID, "printer", job.Printer, "error", err)
				result.Status, result.Error = protocol.StatusFailed, err.Error()
			} else {
				a.Log.Info("printed", "job_id", job.ID, "printer", job.Printer)

				if err := a.State.MarkPrinted(job.ID); err != nil {
					a.Log.Error("could not remember a printed job", "job_id", job.ID, "error", err)
				}
			}
		}

		if err := write(ctx, c, result); err != nil {
			return // the reader sees the connection fail
		}
	}
}

// print prints a job to the end even if the connection drops meanwhile: a
// job cut in the middle would come out half printed, then whole again.
func (a *Agent) print(ctx context.Context, job protocol.Job) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), a.PrintTimeout)
	defer cancel()

	switch job.Format {
	case protocol.FormatEscPos:
		return a.Spooler.PrintRaw(ctx, job.Printer, job.Title, job.Data) //nolint:wrapcheck // the spooler's message is the result
	case protocol.FormatPDF:
		return a.Spooler.PrintPDF(ctx, job.Printer, job.Title, job.PaperSize, job.Data) //nolint:wrapcheck // as above
	default:
		return fmt.Errorf("%w (%q)", ErrFormat, job.Format)
	}
}

// watchPrinters reports the PC's printers now and whenever they change.
func (a *Agent) watchPrinters(ctx context.Context, c *websocket.Conn) {
	var last []protocol.Printer

	sent := false

	for {
		printers, err := a.Spooler.Printers(ctx)
		if err != nil {
			a.Log.Warn("could not list the printers", "error", err)
		} else if !sent || !slices.Equal(printers, last) {
			hostname, _ := os.Hostname()
			msg := protocol.Message{
				Type: protocol.TypePrinters, Hostname: hostname, OS: runtime.GOOS + "/" + runtime.GOARCH, Version: a.Version,
				Printers: printers,
			}

			if err := write(ctx, c, msg); err != nil {
				return
			}

			last, sent = printers, true
		}

		if !sleep(ctx, a.PrintersEvery) {
			return
		}
	}
}

func write(ctx context.Context, c *websocket.Conn, msg protocol.Message) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err //nolint:wrapcheck // a message always encodes
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	return c.Write(ctx, websocket.MessageText, data) //nolint:wrapcheck // the connection's end
}

// connectURL is the WebSocket address of the server: wss, or ws for a
// server on this machine (development).
func connectURL(server string) (string, error) {
	u, err := CheckServer(server)
	if err != nil {
		return "", err
	}

	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}

	u.Path = strings.TrimSuffix(u.Path, "/") + protocol.ConnectPath

	return u.String(), nil
}

// CheckServer parses VietGara's address: https, or http on this machine
// only, without a query.
func CheckServer(server string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSuffix(strings.TrimSpace(server), "/"))
	if err != nil || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("%w: %q", ErrServer, server)
	}

	local := u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1"

	if u.Scheme != "https" && (u.Scheme != "http" || !local) {
		return nil, fmt.Errorf("%w: %q", ErrServer, server)
	}

	return u, nil
}

func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// jitter spreads the reconnections of many agents after a restart.
func jitter(d time.Duration) time.Duration {
	return d/2 + time.Duration(rand.Int64N(int64(d/2)+1)) //nolint:gosec // not a secret
}
