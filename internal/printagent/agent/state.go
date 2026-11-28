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
	"strings"
	"sync"
)

// maxPrinted is how many printed job ids the agent remembers.
const maxPrinted = 200

// State is what the agent keeps between runs, in a JSON file only the
// administrators and the system can read (see the command's state path); the
// ids of the jobs it printed lately are in a file beside it.
type State struct {
	// Server is VietGara's address, "https://app.vietgara.vn".
	Server     string `json:"server"`
	AgentID    string `json:"agentId"`
	AgentName  string `json:"agentName"`
	GarageName string `json:"garageName"`
	Key        string `json:"key"`
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

func (s *Store) save(st State) error { return writeJSON(s.Path, st) }

// writeJSON writes v to path atomically, readable by its owner only.
func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}

	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}

	return nil
}

// Printed reports whether the job was printed lately.
func (s *Store) Printed(jobID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return slices.Contains(s.printed(), jobID)
}

// MarkPrinted remembers a printed job, forgetting the oldest beyond 200. The
// ids live in their own file next to the state, so that remembering one never
// writes back a state that a new pairing (another process) replaced.
func (s *Store) MarkPrinted(jobID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	printed := s.printed()
	if slices.Contains(printed, jobID) {
		return nil
	}

	printed = append(printed, jobID)
	if len(printed) > maxPrinted {
		printed = printed[len(printed)-maxPrinted:]
	}

	return writeJSON(s.printedPath(), printed)
}

func (s *Store) printedPath() string {
	return strings.TrimSuffix(s.Path, filepath.Ext(s.Path)) + ".printed.json"
}

// printed reads the printed ids; none when the file is missing or damaged.
func (s *Store) printed() []string {
	data, err := os.ReadFile(s.printedPath())
	if err != nil {
		return nil
	}

	var ids []string
	if json.Unmarshal(data, &ids) != nil {
		return nil
	}

	return ids
}

// EnsureDir creates the directory of the state file.
func (s *Store) EnsureDir() error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(s.Path), err)
	}

	return nil
}
