// Package agent is the print agent (FR-DOC-08, Release 13.1): it pairs with
// VietGara once, then keeps an outbound WebSocket and prints the jobs it
// receives on the PC's printers. It keeps only its key and the ids of the
// jobs it printed lately, so that a job sent again is not printed twice.
package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
)

// maxPrinted is how many printed job ids the agent remembers.
const maxPrinted = 200

// State is what the agent keeps between runs, in one JSON file only the
// administrators and the system can read (see the command's state path).
type State struct {
	// Server is VietGara's address, "https://app.vietgara.vn".
	Server     string `json:"server"`
	AgentID    string `json:"agentId"`
	AgentName  string `json:"agentName"`
	GarageName string `json:"garageName"`
	Key        string `json:"key"`
	// Printed are the ids of the jobs printed lately, oldest first.
	Printed []string `json:"printed,omitempty"`
}

// ErrNotPaired is returned when there is no state file yet.
var ErrNotPaired = errors.New("the agent is not paired: run setup with a pairing code")

// Store reads and writes the state file.
type Store struct {
	Path string

	mu sync.Mutex
}

// Load reads the state.
func (s *Store) Load() (State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.load()
}

func (s *Store) load() (State, error) {
	data, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return State{}, ErrNotPaired
	}

	if err != nil {
		return State{}, fmt.Errorf("read %s: %w", s.Path, err)
	}

	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return State{}, fmt.Errorf("read %s: %w", s.Path, err)
	}

	if st.Key == "" || st.Server == "" {
		return State{}, ErrNotPaired
	}

	return st, nil
}

// Save writes the state atomically, readable by its owner only.
func (s *Store) Save(st State) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.save(st)
}

func (s *Store) save(st State) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}

	tmp := s.Path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}

	if err := os.Rename(tmp, s.Path); err != nil {
		return fmt.Errorf("write %s: %w", s.Path, err)
	}

	return nil
}

// Printed reports whether the job was printed lately.
func (s *Store) Printed(jobID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	st, err := s.load()

	return err == nil && slices.Contains(st.Printed, jobID)
}

// MarkPrinted remembers a printed job, forgetting the oldest beyond 200.
func (s *Store) MarkPrinted(jobID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	st, err := s.load()
	if err != nil {
		return err
	}

	if slices.Contains(st.Printed, jobID) {
		return nil
	}

	st.Printed = append(st.Printed, jobID)
	if len(st.Printed) > maxPrinted {
		st.Printed = st.Printed[len(st.Printed)-maxPrinted:]
	}

	return s.save(st)
}

// EnsureDir creates the directory of the state file.
func (s *Store) EnsureDir() error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(s.Path), err)
	}

	return nil
}
