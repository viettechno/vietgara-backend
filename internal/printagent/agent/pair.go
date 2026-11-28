package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/viettechno/vietgara-backend/internal/printagent/protocol"
)

// ErrPairingCode is a pairing code the server does not know (wrong, used or
// expired).
var ErrPairingCode = errors.New("the pairing code is wrong or expired: create a new one in VietGara")

// Errors of the pairing call.
var (
	ErrTooManyAttempts = errors.New("too many attempts: wait a few minutes and try again")
	ErrServerAnswer    = errors.New("the server did not pair the agent")
)

// Pair exchanges a pairing code for the agent's key and saves it in the
// state, which then names this agent (its printed jobs are forgotten).
func Pair(ctx context.Context, store *Store, server, code, version string) (State, error) {
	u, err := CheckServer(server)
	if err != nil {
		return State{}, err
	}

	hostname, _ := os.Hostname()

	body, err := json.Marshal(map[string]string{
		"pairingCode": strings.TrimSpace(code), "hostname": hostname, "os": runtime.GOOS + "/" + runtime.GOARCH, "version": version,
	})
	if err != nil {
		return State{}, fmt.Errorf("encode pairing: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String()+protocol.PairPath, bytes.NewReader(body))
	if err != nil {
		return State{}, fmt.Errorf("pair: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "vietgara-agent/"+version)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return State{}, fmt.Errorf("connect to %s: %w", u, err)
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return State{}, fmt.Errorf("pair: %w", err)
	}

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return State{}, ErrPairingCode
	case resp.StatusCode == http.StatusTooManyRequests:
		return State{}, ErrTooManyAttempts
	case resp.StatusCode != http.StatusOK:
		return State{}, fmt.Errorf("%w: %s", ErrServerAnswer, resp.Status)
	}

	var paired struct {
		AgentKey   string `json:"agentKey"`
		AgentID    string `json:"agentId"`
		AgentName  string `json:"agentName"`
		GarageName string `json:"garageName"`
	}

	if err := json.Unmarshal(data, &paired); err != nil || paired.AgentKey == "" {
		return State{}, fmt.Errorf("%w: %s", ErrServerAnswer, strings.TrimSpace(string(data)))
	}

	st := State{
		Server: u.String(), AgentID: paired.AgentID, AgentName: paired.AgentName, GarageName: paired.GarageName, Key: paired.AgentKey,
	}

	if err := store.EnsureDir(); err != nil {
		return State{}, err
	}

	if err := store.Save(st); err != nil {
		return State{}, err
	}

	return st, nil
}
