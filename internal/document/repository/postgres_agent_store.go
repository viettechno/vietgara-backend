package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/viettechno/vietgara-backend/internal/document/printmodel"
	"github.com/viettechno/vietgara-backend/internal/document/usecase"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
)

// PostgresAgentStore implements usecase.AgentStore and usecase.JobStore on
// the shared pool. print_agents and print_jobs are garage-scoped by
// row-level security: every call but the two Find calls must run in the
// garage's transaction.
type PostgresAgentStore struct {
	pool *platformdb.Pool
}

// NewPostgresAgentStore returns a PostgresAgentStore on pool.
func NewPostgresAgentStore(pool *platformdb.Pool) *PostgresAgentStore {
	return &PostgresAgentStore{pool: pool}
}

// agentColumns are read with the database clock: connected is decided where
// the heartbeat was written.
const agentColumns = `id, name, key_hash IS NOT NULL, pairing_expires_at,
	connection_id IS NOT NULL AND last_seen_at > now() - make_interval(secs => $1),
	last_seen_at, hostname, os, version, printers, created_at, updated_at`

var onlineSeconds = usecase.AgentOnline.Seconds()

// ListAgents returns the garage's agents by name.
func (s *PostgresAgentStore) ListAgents(ctx context.Context, garageID string) ([]usecase.PrintAgent, error) {
	rows, err := s.q(ctx).Query(ctx,
		`SELECT `+agentColumns+` FROM print_agents WHERE garage_id = $2 ORDER BY lower(name), id`, onlineSeconds, garageID)
	if err != nil {
		return nil, fmt.Errorf("query print agents: %w", err)
	}
	defer rows.Close()

	agents := []usecase.PrintAgent{}

	for rows.Next() {
		a, err := scanAgent(rows)
		if err != nil {
			return nil, err
		}

		agents = append(agents, a)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read print agents: %w", err)
	}

	return agents, nil
}

// GetAgent returns one agent.
func (s *PostgresAgentStore) GetAgent(ctx context.Context, garageID, id string) (usecase.PrintAgent, error) {
	if uuid.Validate(id) != nil {
		return usecase.PrintAgent{}, usecase.ErrAgentNotFound
	}

	row := s.q(ctx).QueryRow(ctx,
		`SELECT `+agentColumns+` FROM print_agents WHERE garage_id = $2 AND id = $3`, onlineSeconds, garageID, id)

	return agentResult(scanAgent(row))
}

// LockAgents takes a transaction-scoped advisory lock on the garage's agents.
func (s *PostgresAgentStore) LockAgents(ctx context.Context, garageID string) error {
	if _, err := s.q(ctx).Exec(ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended('print_agents:' || $1, 0))`, garageID); err != nil {
		return fmt.Errorf("lock print agents: %w", err)
	}

	return nil
}

// CountAgents counts the garage's agents.
func (s *PostgresAgentStore) CountAgents(ctx context.Context, garageID string) (int, error) {
	var n int
	if err := s.q(ctx).QueryRow(ctx, `SELECT count(*) FROM print_agents WHERE garage_id = $1`, garageID).Scan(&n); err != nil {
		return 0, fmt.Errorf("count print agents: %w", err)
	}

	return n, nil
}

// CreateAgent inserts an agent waiting to be paired.
func (s *PostgresAgentStore) CreateAgent(
	ctx context.Context, garageID, name string, codeHash []byte, expires time.Time,
) (usecase.PrintAgent, error) {
	row := s.q(ctx).QueryRow(ctx, `
		INSERT INTO print_agents (garage_id, name, pairing_code_hash, pairing_expires_at)
		VALUES ($2, $3, $4, $5)
		RETURNING `+agentColumns, onlineSeconds, garageID, name, codeHash, expires)

	return agentResult(scanAgent(row))
}

// SetPairingCode sets a new pairing code and removes the key.
func (s *PostgresAgentStore) SetPairingCode(
	ctx context.Context, garageID, id string, codeHash []byte, expires time.Time,
) (usecase.PrintAgent, error) {
	if uuid.Validate(id) != nil {
		return usecase.PrintAgent{}, usecase.ErrAgentNotFound
	}

	row := s.q(ctx).QueryRow(ctx, `
		UPDATE print_agents
		SET pairing_code_hash = $4, pairing_expires_at = $5, key_hash = NULL, connection_id = NULL
		WHERE garage_id = $2 AND id = $3
		RETURNING `+agentColumns, onlineSeconds, garageID, id, codeHash, expires)

	return agentResult(scanAgent(row))
}

// DeleteAgent removes an agent; its printers and jobs go with it.
func (s *PostgresAgentStore) DeleteAgent(ctx context.Context, garageID, id string) error {
	if uuid.Validate(id) != nil {
		return usecase.ErrAgentNotFound
	}

	tag, err := s.q(ctx).Exec(ctx, `DELETE FROM print_agents WHERE garage_id = $1 AND id = $2`, garageID, id)
	if err != nil {
		return fmt.Errorf("delete print agent: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return usecase.ErrAgentNotFound
	}

	return nil
}

// FindAgentByPairingCode finds the agent of a valid pairing code.
func (s *PostgresAgentStore) FindAgentByPairingCode(ctx context.Context, codeHash []byte) (string, string, error) {
	return s.find(ctx, `SELECT id, garage_id FROM find_print_agent_by_pairing_code($1)`, codeHash)
}

// FindAgentByKey finds the agent of a key.
func (s *PostgresAgentStore) FindAgentByKey(ctx context.Context, keyHash []byte) (string, string, error) {
	return s.find(ctx, `SELECT id, garage_id FROM find_print_agent_by_key($1)`, keyHash)
}

func (s *PostgresAgentStore) find(ctx context.Context, sql string, hash []byte) (string, string, error) {
	var agentID, garageID string

	err := s.q(ctx).QueryRow(ctx, sql, hash).Scan(&agentID, &garageID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", usecase.ErrAgentNotFound
	}

	if err != nil {
		return "", "", fmt.Errorf("find print agent: %w", err)
	}

	return agentID, garageID, nil
}

// CompletePairing swaps the pairing code for the key, if the code still
// works.
func (s *PostgresAgentStore) CompletePairing(
	ctx context.Context, garageID, agentID string, codeHash, keyHash []byte, report usecase.AgentReport,
) (usecase.PrintAgent, error) {
	printers, err := json.Marshal(report.Printers)
	if err != nil {
		return usecase.PrintAgent{}, fmt.Errorf("encode printers: %w", err)
	}

	row := s.q(ctx).QueryRow(ctx, `
		UPDATE print_agents
		SET key_hash = $5, pairing_code_hash = NULL, pairing_expires_at = NULL, paired_at = now(), connection_id = NULL,
		    hostname = $6, os = $7, version = $8, printers = $9
		WHERE garage_id = $2 AND id = $3 AND pairing_code_hash = $4 AND pairing_expires_at > now()
		RETURNING `+agentColumns,
		onlineSeconds, garageID, agentID, codeHash, keyHash, report.Hostname, report.OS, report.Version, printers)

	return agentResult(scanAgent(row))
}

// SetConnection records the connection and its first heartbeat.
func (s *PostgresAgentStore) SetConnection(ctx context.Context, a usecase.AgentSession) error {
	tag, err := s.q(ctx).Exec(ctx, `UPDATE print_agents SET connection_id = $4, last_seen_at = now()
		WHERE garage_id = $1 AND id = $2 AND key_hash = $3`, a.GarageID, a.AgentID, a.KeyHash, a.ConnectionID)
	if err != nil {
		return fmt.Errorf("print agent connection: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return usecase.ErrAgentRevoked
	}

	return nil
}

// Heartbeat keeps the connection's agent connected, while the key and the
// connection are still the agent's.
func (s *PostgresAgentStore) Heartbeat(ctx context.Context, a usecase.AgentSession) error {
	var current bool

	err := s.q(ctx).QueryRow(ctx, `
		UPDATE print_agents
		SET last_seen_at = CASE WHEN connection_id = $4 THEN now() ELSE last_seen_at END
		WHERE garage_id = $1 AND id = $2 AND key_hash = $3
		RETURNING COALESCE(connection_id = $4, false)`, a.GarageID, a.AgentID, a.KeyHash, a.ConnectionID).Scan(&current)

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return usecase.ErrAgentRevoked
	case err != nil:
		return fmt.Errorf("print agent heartbeat: %w", err)
	case !current:
		return usecase.ErrAgentReplaced
	}

	return nil
}

// ClearConnection forgets the connection if it is still the agent's.
func (s *PostgresAgentStore) ClearConnection(ctx context.Context, a usecase.AgentSession) error {
	if _, err := s.q(ctx).Exec(ctx, `UPDATE print_agents SET connection_id = NULL
		WHERE garage_id = $1 AND id = $2 AND connection_id = $3`, a.GarageID, a.AgentID, a.ConnectionID); err != nil {
		return fmt.Errorf("clear print agent connection: %w", err)
	}

	return nil
}

// SaveReport stores what the agent says about its PC.
func (s *PostgresAgentStore) SaveReport(ctx context.Context, a usecase.AgentSession, report usecase.AgentReport) error {
	printers, err := json.Marshal(report.Printers)
	if err != nil {
		return fmt.Errorf("encode printers: %w", err)
	}

	tag, err := s.q(ctx).Exec(ctx, `UPDATE print_agents SET hostname = $4, os = $5, version = $6, printers = $7
		WHERE garage_id = $1 AND id = $2 AND key_hash = $3`,
		a.GarageID, a.AgentID, a.KeyHash, report.Hostname, report.OS, report.Version, printers)
	if err != nil {
		return fmt.Errorf("save print agent report: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return usecase.ErrAgentRevoked
	}

	return nil
}

const jobColumns = `id, COALESCE(printer_id::text, ''), printer_name, COALESCE(document_type, ''), status, error,
	expires_at, created_at, updated_at`

// CreateJob queues a job.
func (s *PostgresAgentStore) CreateJob(ctx context.Context, garageID string, j usecase.NewJob) (usecase.PrintJob, error) {
	row := s.q(ctx).QueryRow(ctx, `
		INSERT INTO print_jobs (garage_id, agent_id, printer_id, printer_name, queue, document_type, format, paper_size, data, expires_at)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), $7, NULLIF($8, ''), $9, $10)
		RETURNING `+jobColumns,
		garageID, j.AgentID, j.PrinterID, j.PrinterName, j.Queue, string(j.DocumentType), string(j.Format), string(j.Paper),
		j.Data, j.ExpiresAt)

	job, err := scanJob(row)
	if err != nil {
		return usecase.PrintJob{}, fmt.Errorf("create print job: %w", err)
	}

	return job, nil
}

// GetJob returns one job.
func (s *PostgresAgentStore) GetJob(ctx context.Context, garageID, id string) (usecase.PrintJob, error) {
	if uuid.Validate(id) != nil {
		return usecase.PrintJob{}, usecase.ErrJobNotFound
	}

	job, err := scanJob(s.q(ctx).QueryRow(ctx,
		`SELECT `+jobColumns+` FROM print_jobs WHERE garage_id = $1 AND id = $2`, garageID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return usecase.PrintJob{}, usecase.ErrJobNotFound
	}

	if err != nil {
		return usecase.PrintJob{}, fmt.Errorf("get print job: %w", err)
	}

	return job, nil
}

// PurgeJobs deletes the garage's jobs created before the time.
func (s *PostgresAgentStore) PurgeJobs(ctx context.Context, garageID string, before time.Time) error {
	if _, err := s.q(ctx).Exec(ctx, `DELETE FROM print_jobs WHERE garage_id = $1 AND created_at < $2`, garageID, before); err != nil {
		return fmt.Errorf("purge print jobs: %w", err)
	}

	return nil
}

// PendingJobs expires the agent's late jobs and returns the oldest pending
// ones.
func (s *PostgresAgentStore) PendingJobs(
	ctx context.Context, a usecase.AgentSession, now time.Time, limit int,
) ([]usecase.AgentJob, error) {
	if _, err := s.q(ctx).Exec(ctx, `
		UPDATE print_jobs SET status = 'expired', data = '', finished_at = $3
		WHERE garage_id = $1 AND agent_id = $2 AND status IN ('queued', 'sent') AND expires_at <= $3`,
		a.GarageID, a.AgentID, now); err != nil {
		return nil, fmt.Errorf("expire print jobs: %w", err)
	}

	rows, err := s.q(ctx).Query(ctx, `
		SELECT id, queue, COALESCE(document_type, ''), format, COALESCE(paper_size, ''), data
		FROM print_jobs
		WHERE garage_id = $1 AND agent_id = $2 AND (status = 'queued' OR (status = 'sent' AND sent_at < $4))
		ORDER BY created_at, id
		LIMIT $3`, a.GarageID, a.AgentID, limit, a.ConnectedAt)
	if err != nil {
		return nil, fmt.Errorf("query pending print jobs: %w", err)
	}
	defer rows.Close()

	var jobs []usecase.AgentJob

	for rows.Next() {
		var (
			j                   usecase.AgentJob
			docType, format, ps string
		)

		if err := rows.Scan(&j.ID, &j.Queue, &docType, &format, &ps, &j.Data); err != nil {
			return nil, fmt.Errorf("read pending print job: %w", err)
		}

		j.DocumentType, j.Format, j.Paper = printmodel.DocumentType(docType), usecase.JobFormat(format), printmodel.Paper(ps)
		jobs = append(jobs, j)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read pending print jobs: %w", err)
	}

	return jobs, nil
}

// MarkSent records that the jobs were sent.
func (s *PostgresAgentStore) MarkSent(ctx context.Context, a usecase.AgentSession, ids []string, at time.Time) error {
	if _, err := s.q(ctx).Exec(ctx, `
		UPDATE print_jobs SET status = 'sent', sent_at = $4
		WHERE garage_id = $1 AND agent_id = $2 AND id = ANY($3::uuid[]) AND status IN ('queued', 'sent')`,
		a.GarageID, a.AgentID, ids, at); err != nil {
		return fmt.Errorf("mark print jobs sent: %w", err)
	}

	return nil
}

// FinishJob records the agent's result of a pending job of its own.
func (s *PostgresAgentStore) FinishJob(
	ctx context.Context, a usecase.AgentSession, id string, status usecase.JobStatus, message string, at time.Time,
) error {
	if !isUUID(id) {
		return nil // not a job of the agent: nothing to record
	}

	if _, err := s.q(ctx).Exec(ctx, `
		UPDATE print_jobs SET status = $4, error = $5, data = '', finished_at = $6
		WHERE garage_id = $1 AND agent_id = $2 AND id = $3 AND status IN ('queued', 'sent')`,
		a.GarageID, a.AgentID, id, string(status), message, at); err != nil {
		return fmt.Errorf("finish print job: %w", err)
	}

	return nil
}

func isUUID(s string) bool { return uuid.Validate(s) == nil }

func (s *PostgresAgentStore) q(ctx context.Context) platformdb.Querier {
	return platformdb.QuerierFromContext(ctx, s.pool)
}

func scanAgent(row pgx.Row) (usecase.PrintAgent, error) {
	var (
		a        usecase.PrintAgent
		printers []byte
	)

	if err := row.Scan(&a.ID, &a.Name, &a.Paired, &a.PairingExpiresAt, &a.Connected, &a.LastSeenAt,
		&a.Hostname, &a.OS, &a.Version, &printers, &a.CreatedAt, &a.UpdatedAt); err != nil {
		return usecase.PrintAgent{}, err //nolint:wrapcheck // agentResult classifies it
	}

	if err := json.Unmarshal(printers, &a.Printers); err != nil {
		return usecase.PrintAgent{}, fmt.Errorf("decode printers: %w", err)
	}

	return a, nil
}

func scanJob(row pgx.Row) (usecase.PrintJob, error) {
	var (
		j               usecase.PrintJob
		docType, status string
	)

	if err := row.Scan(&j.ID, &j.PrinterID, &j.PrinterName, &docType, &status, &j.Error,
		&j.ExpiresAt, &j.CreatedAt, &j.UpdatedAt); err != nil {
		return usecase.PrintJob{}, err //nolint:wrapcheck // the callers wrap it
	}

	j.DocumentType, j.Status = printmodel.DocumentType(docType), usecase.JobStatus(status)

	return j, nil
}

// agentResult maps a missing row and a duplicate name to the store's errors.
func agentResult(a usecase.PrintAgent, err error) (usecase.PrintAgent, error) {
	var pgErr *pgconn.PgError

	switch {
	case err == nil:
		return a, nil
	case errors.Is(err, pgx.ErrNoRows):
		return usecase.PrintAgent{}, usecase.ErrAgentNotFound
	case errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "idx_print_agents_name":
		return usecase.PrintAgent{}, usecase.ErrAgentNameTaken
	default:
		return usecase.PrintAgent{}, fmt.Errorf("print agent: %w", err)
	}
}
