// Package agenttest provides in-memory fakes of the print agents' store
// (Release 13.1) for tests of the use case and the connection hub. The SQL
// they stand for is tested against Postgres in the repository package.
package agenttest

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/viettechno/vietgara-backend/internal/document/usecase"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
)

// Agent is a stored agent.
type Agent struct {
	usecase.PrintAgent

	GarageID     string
	KeyHash      []byte
	CodeHash     []byte
	ConnectionID string
}

// Job is a stored job.
type Job struct {
	usecase.PrintJob

	GarageID string
	AgentID  string
	Job      usecase.NewJob
	SentAt   time.Time
}

// Store implements usecase.AgentStore and usecase.JobStore in memory.
type Store struct {
	mu     sync.Mutex
	Agents []*Agent
	Jobs   []*Job
	// Now is the store's clock (the database's now()).
	Now func() time.Time
}

// NewStore returns an empty store on the given clock.
func NewStore(now func() time.Time) *Store { return &Store{Now: now} }

// Agent returns the stored agent with the id, or nil.
func (s *Store) Agent(id string) *Agent {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.agent(id)
}

// Job returns the stored job with the id, or nil.
func (s *Store) Job(id string) *Job {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, j := range s.Jobs {
		if j.ID == id {
			return j
		}
	}

	return nil
}

func (s *Store) agent(id string) *Agent {
	for _, a := range s.Agents {
		if a.ID == id {
			return a
		}
	}

	return nil
}

func (s *Store) view(a *Agent) usecase.PrintAgent {
	v := a.PrintAgent
	v.Paired = a.KeyHash != nil
	v.Connected = a.ConnectionID != "" && a.LastSeenAt != nil && s.Now().Sub(*a.LastSeenAt) < usecase.AgentOnline
	v.Printers = slices.Clone(a.Printers)

	return v
}

// ListAgents implements usecase.AgentStore.
func (s *Store) ListAgents(_ context.Context, garageID string) ([]usecase.PrintAgent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := []usecase.PrintAgent{}

	for _, a := range s.Agents {
		if a.GarageID == garageID {
			out = append(out, s.view(a))
		}
	}

	slices.SortFunc(out, func(a, b usecase.PrintAgent) int {
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})

	return out, nil
}

// GetAgent implements usecase.AgentStore.
func (s *Store) GetAgent(_ context.Context, garageID, id string) (usecase.PrintAgent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if a := s.agent(id); a != nil && a.GarageID == garageID {
		return s.view(a), nil
	}

	return usecase.PrintAgent{}, usecase.ErrAgentNotFound
}

// LockAgents implements usecase.AgentStore.
func (s *Store) LockAgents(context.Context, string) error { return nil }

// CountAgents implements usecase.AgentStore.
func (s *Store) CountAgents(_ context.Context, garageID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	n := 0

	for _, a := range s.Agents {
		if a.GarageID == garageID {
			n++
		}
	}

	return n, nil
}

// CreateAgent implements usecase.AgentStore.
func (s *Store) CreateAgent(_ context.Context, garageID, name string, codeHash []byte, expires time.Time) (usecase.PrintAgent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, a := range s.Agents {
		if a.GarageID == garageID && strings.EqualFold(a.Name, name) {
			return usecase.PrintAgent{}, usecase.ErrAgentNameTaken
		}
	}

	now := s.Now()
	a := &Agent{
		ID: uuid.NewString(), Name: name, PairingExpiresAt: &expires, CreatedAt: now, UpdatedAt: now,
		GarageID: garageID, CodeHash: codeHash,
	}
	s.Agents = append(s.Agents, a)

	return s.view(a), nil
}

// SetPairingCode implements usecase.AgentStore.
func (s *Store) SetPairingCode(_ context.Context, garageID, id string, codeHash []byte, expires time.Time) (usecase.PrintAgent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	a := s.agent(id)
	if a == nil || a.GarageID != garageID {
		return usecase.PrintAgent{}, usecase.ErrAgentNotFound
	}

	a.CodeHash, a.PairingExpiresAt, a.KeyHash, a.ConnectionID = codeHash, &expires, nil, ""

	return s.view(a), nil
}

// DeleteAgent implements usecase.AgentStore; the agent's jobs go with it.
func (s *Store) DeleteAgent(_ context.Context, garageID, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	a := s.agent(id)
	if a == nil || a.GarageID != garageID {
		return usecase.ErrAgentNotFound
	}

	s.Agents = slices.DeleteFunc(s.Agents, func(x *Agent) bool { return x == a })
	s.Jobs = slices.DeleteFunc(s.Jobs, func(j *Job) bool { return j.AgentID == id })

	return nil
}

// FindAgentByPairingCode implements usecase.AgentStore.
func (s *Store) FindAgentByPairingCode(_ context.Context, codeHash []byte) (string, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, a := range s.Agents {
		if a.CodeHash != nil && bytes.Equal(a.CodeHash, codeHash) && a.PairingExpiresAt.After(s.Now()) {
			return a.ID, a.GarageID, nil
		}
	}

	return "", "", usecase.ErrAgentNotFound
}

// FindAgentByKey implements usecase.AgentStore.
func (s *Store) FindAgentByKey(_ context.Context, keyHash []byte) (string, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, a := range s.Agents {
		if a.KeyHash != nil && bytes.Equal(a.KeyHash, keyHash) {
			return a.ID, a.GarageID, nil
		}
	}

	return "", "", usecase.ErrAgentNotFound
}

// CompletePairing implements usecase.AgentStore.
func (s *Store) CompletePairing(
	_ context.Context, garageID, agentID string, codeHash, keyHash []byte, report usecase.AgentReport,
) (usecase.PrintAgent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	a := s.agent(agentID)
	if a == nil || a.GarageID != garageID || !bytes.Equal(a.CodeHash, codeHash) || !a.PairingExpiresAt.After(s.Now()) {
		return usecase.PrintAgent{}, usecase.ErrAgentNotFound
	}

	a.KeyHash, a.CodeHash, a.PairingExpiresAt, a.ConnectionID = keyHash, nil, nil, ""
	a.Hostname, a.OS, a.Version, a.Printers = report.Hostname, report.OS, report.Version, report.Printers

	return s.view(a), nil
}

// SetConnection implements usecase.AgentStore.
func (s *Store) SetConnection(_ context.Context, sess usecase.AgentSession) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	a := s.keyed(sess)
	if a == nil {
		return usecase.ErrAgentRevoked
	}

	now := s.Now()
	a.ConnectionID, a.LastSeenAt = sess.ConnectionID, &now

	return nil
}

// Heartbeat implements usecase.AgentStore.
func (s *Store) Heartbeat(_ context.Context, sess usecase.AgentSession) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	a := s.keyed(sess)
	if a == nil {
		return usecase.ErrAgentRevoked
	}

	if a.ConnectionID != sess.ConnectionID {
		return usecase.ErrAgentReplaced
	}

	now := s.Now()
	a.LastSeenAt = &now

	return nil
}

// ClearConnection implements usecase.AgentStore.
func (s *Store) ClearConnection(_ context.Context, sess usecase.AgentSession) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if a := s.agent(sess.AgentID); a != nil && a.ConnectionID == sess.ConnectionID {
		a.ConnectionID = ""
	}

	return nil
}

// SaveReport implements usecase.AgentStore.
func (s *Store) SaveReport(_ context.Context, sess usecase.AgentSession, report usecase.AgentReport) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	a := s.keyed(sess)
	if a == nil {
		return usecase.ErrAgentRevoked
	}

	a.Hostname, a.OS, a.Version, a.Printers = report.Hostname, report.OS, report.Version, report.Printers

	return nil
}

func (s *Store) keyed(sess usecase.AgentSession) *Agent {
	if a := s.agent(sess.AgentID); a != nil && a.GarageID == sess.GarageID && a.KeyHash != nil && bytes.Equal(a.KeyHash, sess.KeyHash) {
		return a
	}

	return nil
}

// CreateJob implements usecase.JobStore.
func (s *Store) CreateJob(_ context.Context, garageID string, j usecase.NewJob) (usecase.PrintJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.Now()
	job := &Job{
		ID: uuid.NewString(), PrinterID: j.PrinterID, PrinterName: j.PrinterName, DocumentType: j.DocumentType,
		Status: usecase.JobQueued, ExpiresAt: j.ExpiresAt, CreatedAt: now, UpdatedAt: now,
		GarageID: garageID, AgentID: j.AgentID, Job: j,
	}
	s.Jobs = append(s.Jobs, job)

	return job.PrintJob, nil
}

// GetJob implements usecase.JobStore.
func (s *Store) GetJob(_ context.Context, garageID, id string) (usecase.PrintJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, j := range s.Jobs {
		if j.ID == id && j.GarageID == garageID {
			return j.PrintJob, nil
		}
	}

	return usecase.PrintJob{}, usecase.ErrJobNotFound
}

// PurgeJobs implements usecase.JobStore.
func (s *Store) PurgeJobs(_ context.Context, garageID string, before time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Jobs = slices.DeleteFunc(s.Jobs, func(j *Job) bool { return j.GarageID == garageID && j.CreatedAt.Before(before) })

	return nil
}

// PendingJobs implements usecase.JobStore.
func (s *Store) PendingJobs(_ context.Context, sess usecase.AgentSession, now time.Time, limit int) ([]usecase.AgentJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var out []usecase.AgentJob

	for _, j := range s.Jobs {
		if j.AgentID != sess.AgentID || (j.Status != usecase.JobQueued && j.Status != usecase.JobSent) {
			continue
		}

		if !now.Before(j.ExpiresAt) {
			j.Status, j.Job.Data = usecase.JobExpired, nil

			continue
		}

		if j.Status == usecase.JobSent && !j.SentAt.Before(sess.ConnectedAt) {
			continue
		}

		if len(out) < limit {
			out = append(out, usecase.AgentJob{
				ID: j.ID, Queue: j.Job.Queue, DocumentType: j.DocumentType, Format: j.Job.Format, Paper: j.Job.Paper, Data: j.Job.Data,
			})
		}
	}

	return out, nil
}

// MarkSent implements usecase.JobStore.
func (s *Store) MarkSent(_ context.Context, sess usecase.AgentSession, ids []string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, j := range s.Jobs {
		if j.AgentID == sess.AgentID && slices.Contains(ids, j.ID) && (j.Status == usecase.JobQueued || j.Status == usecase.JobSent) {
			j.Status, j.SentAt = usecase.JobSent, at
		}
	}

	return nil
}

// FinishJob implements usecase.JobStore.
func (s *Store) FinishJob(_ context.Context, sess usecase.AgentSession, id string, status usecase.JobStatus, message string, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, j := range s.Jobs {
		if j.ID == id && j.AgentID == sess.AgentID && (j.Status == usecase.JobQueued || j.Status == usecase.JobSent) {
			j.Status, j.Error, j.Job.Data = status, message, nil
		}
	}

	return nil
}

// PassTx is a usecase.Transactor that runs fn at once, ignoring the scope.
type PassTx struct{}

// WithTransaction implements usecase.Transactor.
func (PassTx) WithTransaction(ctx context.Context, _ platformdb.Scope, fn func(ctx context.Context) error) error {
	return fn(ctx)
}

// Notifier records the notifications of the use case.
type Notifier struct {
	mu      sync.Mutex
	Queued  []string
	Revoked []string
}

// JobQueued implements usecase.AgentNotifier.
func (n *Notifier) JobQueued(agentID string) {
	n.mu.Lock()
	defer n.mu.Unlock()

	n.Queued = append(n.Queued, agentID)
}

// AgentRevoked implements usecase.AgentNotifier.
func (n *Notifier) AgentRevoked(agentID string) {
	n.mu.Lock()
	defer n.mu.Unlock()

	n.Revoked = append(n.Revoked, agentID)
}
