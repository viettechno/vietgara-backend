package repository_test

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/document/printmodel"
	"github.com/viettechno/vietgara-backend/internal/document/repository"
	"github.com/viettechno/vietgara-backend/internal/document/usecase"
	"github.com/viettechno/vietgara-backend/internal/platform/db/dbtest"
)

const agentID = "33333333-3333-3333-3333-333333333333"

var at = time.Date(2026, 11, 28, 9, 0, 0, 0, time.UTC)

func agentRow(name string) []any {
	return []any{agentID, name, true, nil, true, at, "QUAY-PC", "windows/amd64", "0.1.0", []byte(`[{"name":"XP-80C","default":true}]`), at, at}
}

var session = usecase.AgentSession{AgentID: agentID, GarageID: garageID, KeyHash: []byte("k"), ConnectionID: "c1", ConnectedAt: at}

func TestAgentStoreReads(t *testing.T) {
	t.Parallel()

	store := repository.NewPostgresAgentStore(nil)

	tx := dbtest.New(t).
		ExpectQuery("FROM print_agents", dbtest.NewRows(agentRow("Quầy")), nil).
		ExpectQuery("FROM print_agents", nil, errBoom).
		ExpectQuery("FROM print_agents", dbtest.NewRows(agentRow("Quầy")).WithScanError(errBoom), nil).
		ExpectQuery("FROM print_agents", dbtest.NewRows(agentRow("Quầy")).WithErr(errBoom), nil).
		ExpectQuery("FROM print_agents", dbtest.NewRows([]any{agentID, "x", true, nil, true, nil, "", "", "", []byte(`{`), at, at}), nil).
		ExpectQueryRow("FROM print_agents", dbtest.Row(agentRow("Quầy")...)).
		ExpectQueryRow("FROM print_agents", dbtest.ErrRow(pgx.ErrNoRows)).
		ExpectQueryRow("FROM print_agents", dbtest.ErrRow(errBoom)).
		ExpectQueryRow("count(*)", dbtest.Row(2)).
		ExpectQueryRow("count(*)", dbtest.ErrRow(errBoom)).
		ExpectExec("pg_advisory_xact_lock", dbtest.Tag(1), nil).
		ExpectExec("pg_advisory_xact_lock", dbtest.Tag(0), errBoom)
	ctx := tx.Context(t.Context())

	agents, err := store.ListAgents(ctx, garageID)
	require.NoError(t, err)
	require.Len(t, agents, 1)
	assert.Equal(t, []usecase.AgentPrinter{{Name: "XP-80C", Default: true}}, agents[0].Printers)
	assert.True(t, agents[0].Connected)

	for range 4 {
		_, err = store.ListAgents(ctx, garageID)
		require.Error(t, err)
	}

	got, err := store.GetAgent(ctx, garageID, agentID)
	require.NoError(t, err)
	assert.Equal(t, "Quầy", got.Name)

	_, err = store.GetAgent(ctx, garageID, agentID)
	require.ErrorIs(t, err, usecase.ErrAgentNotFound)
	_, err = store.GetAgent(ctx, garageID, agentID)
	require.ErrorIs(t, err, errBoom)
	_, err = store.GetAgent(ctx, garageID, "not-a-uuid")
	require.ErrorIs(t, err, usecase.ErrAgentNotFound, "never reaches the database")

	n, err := store.CountAgents(ctx, garageID)
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	_, err = store.CountAgents(ctx, garageID)
	require.ErrorIs(t, err, errBoom)

	require.NoError(t, store.LockAgents(ctx, garageID))
	require.ErrorIs(t, store.LockAgents(ctx, garageID), errBoom)
}

func TestAgentStoreWrites(t *testing.T) {
	t.Parallel()

	store := repository.NewPostgresAgentStore(nil)
	duplicate := &pgconn.PgError{Code: "23505", ConstraintName: "idx_print_agents_name"}

	tx := dbtest.New(t).
		ExpectQueryRow("INSERT INTO print_agents", dbtest.Row(agentRow("Quầy")...)).
		ExpectQueryRow("INSERT INTO print_agents", dbtest.ErrRow(duplicate)).
		ExpectQueryRow("SET pairing_code_hash", dbtest.Row(agentRow("Quầy")...)).
		ExpectQueryRow("paired_at = now()", dbtest.Row(agentRow("Quầy")...)).
		ExpectExec("DELETE FROM print_agents", dbtest.Tag(1), nil).
		ExpectExec("DELETE FROM print_agents", dbtest.Tag(0), nil).
		ExpectExec("DELETE FROM print_agents", dbtest.Tag(0), errBoom).
		ExpectQueryRow("find_print_agent_by_pairing_code", dbtest.Row(agentID, garageID)).
		ExpectQueryRow("find_print_agent_by_key", dbtest.ErrRow(pgx.ErrNoRows)).
		ExpectQueryRow("find_print_agent_by_key", dbtest.ErrRow(errBoom))
	ctx := tx.Context(t.Context())

	_, err := store.CreateAgent(ctx, garageID, "Quầy", []byte("c"), at)
	require.NoError(t, err)
	_, err = store.CreateAgent(ctx, garageID, "QUẦY", []byte("c"), at)
	require.ErrorIs(t, err, usecase.ErrAgentNameTaken)

	_, err = store.SetPairingCode(ctx, garageID, agentID, []byte("c"), at)
	require.NoError(t, err)
	_, err = store.SetPairingCode(ctx, garageID, "bad", []byte("c"), at)
	require.ErrorIs(t, err, usecase.ErrAgentNotFound)

	_, err = store.CompletePairing(ctx, garageID, agentID, []byte("c"), []byte("k"), usecase.AgentReport{Printers: []usecase.AgentPrinter{{Name: "XP"}}})
	require.NoError(t, err)

	require.NoError(t, store.DeleteAgent(ctx, garageID, agentID))
	require.ErrorIs(t, store.DeleteAgent(ctx, garageID, agentID), usecase.ErrAgentNotFound)
	require.ErrorIs(t, store.DeleteAgent(ctx, garageID, agentID), errBoom)
	require.ErrorIs(t, store.DeleteAgent(ctx, garageID, "bad"), usecase.ErrAgentNotFound)

	id, garage, err := store.FindAgentByPairingCode(ctx, []byte("c"))
	require.NoError(t, err)
	assert.Equal(t, []string{agentID, garageID}, []string{id, garage})

	_, _, err = store.FindAgentByKey(ctx, []byte("k"))
	require.ErrorIs(t, err, usecase.ErrAgentNotFound)
	_, _, err = store.FindAgentByKey(ctx, []byte("k"))
	require.ErrorIs(t, err, errBoom)
}

func TestAgentStoreConnection(t *testing.T) {
	t.Parallel()

	store := repository.NewPostgresAgentStore(nil)

	tx := dbtest.New(t).
		ExpectExec("SET connection_id = $4", dbtest.Tag(1), nil).
		ExpectExec("SET connection_id = $4", dbtest.Tag(0), nil).
		ExpectExec("SET connection_id = $4", dbtest.Tag(0), errBoom).
		ExpectQueryRow("SET last_seen_at", dbtest.Row(true)).
		ExpectQueryRow("SET last_seen_at", dbtest.Row(false)).
		ExpectQueryRow("SET last_seen_at", dbtest.ErrRow(pgx.ErrNoRows)).
		ExpectQueryRow("SET last_seen_at", dbtest.ErrRow(errBoom)).
		ExpectExec("SET connection_id = NULL", dbtest.Tag(1), nil).
		ExpectExec("SET connection_id = NULL", dbtest.Tag(0), errBoom).
		ExpectExec("SET hostname", dbtest.Tag(1), nil).
		ExpectExec("SET hostname", dbtest.Tag(0), nil).
		ExpectExec("SET hostname", dbtest.Tag(0), errBoom)
	ctx := tx.Context(t.Context())

	require.NoError(t, store.SetConnection(ctx, session))
	require.ErrorIs(t, store.SetConnection(ctx, session), usecase.ErrAgentRevoked)
	require.ErrorIs(t, store.SetConnection(ctx, session), errBoom)

	require.NoError(t, store.Heartbeat(ctx, session))
	require.ErrorIs(t, store.Heartbeat(ctx, session), usecase.ErrAgentReplaced)
	require.ErrorIs(t, store.Heartbeat(ctx, session), usecase.ErrAgentRevoked)
	require.ErrorIs(t, store.Heartbeat(ctx, session), errBoom)

	require.NoError(t, store.ClearConnection(ctx, session))
	require.ErrorIs(t, store.ClearConnection(ctx, session), errBoom)

	report := usecase.AgentReport{Hostname: "QUAY-PC", Printers: []usecase.AgentPrinter{{Name: "XP"}}}
	require.NoError(t, store.SaveReport(ctx, session, report))
	require.ErrorIs(t, store.SaveReport(ctx, session, report), usecase.ErrAgentRevoked)
	require.ErrorIs(t, store.SaveReport(ctx, session, report), errBoom)
}

func TestJobStore(t *testing.T) {
	t.Parallel()

	store := repository.NewPostgresAgentStore(nil)
	jobRow := []any{documentID, "p1", "Quầy", "quote", "queued", "", at, at, at}

	tx := dbtest.New(t).
		ExpectQueryRow("INSERT INTO print_jobs", dbtest.Row(jobRow...)).
		ExpectQueryRow("INSERT INTO print_jobs", dbtest.ErrRow(errBoom)).
		ExpectQueryRow("FROM print_jobs", dbtest.Row(jobRow...)).
		ExpectQueryRow("FROM print_jobs", dbtest.ErrRow(pgx.ErrNoRows)).
		ExpectQueryRow("FROM print_jobs", dbtest.ErrRow(errBoom)).
		ExpectExec("DELETE FROM print_jobs", dbtest.Tag(3), nil).
		ExpectExec("DELETE FROM print_jobs", dbtest.Tag(0), errBoom).
		ExpectExec("status = 'expired'", dbtest.Tag(1), nil).
		ExpectQuery("FROM print_jobs", dbtest.NewRows([]any{documentID, "XP-80C", "quote", "escpos", "K80", []byte{0x1b}}), nil).
		ExpectExec("status = 'expired'", dbtest.Tag(0), errBoom).
		ExpectExec("status = 'expired'", dbtest.Tag(0), nil).
		ExpectQuery("FROM print_jobs", nil, errBoom).
		ExpectExec("status = 'expired'", dbtest.Tag(0), nil).
		ExpectQuery("FROM print_jobs", dbtest.NewRows([]any{documentID}).WithScanError(errBoom), nil).
		ExpectExec("status = 'expired'", dbtest.Tag(0), nil).
		ExpectQuery("FROM print_jobs", dbtest.NewRows().WithErr(errBoom), nil).
		ExpectExec("status = 'sent'", dbtest.Tag(1), nil).
		ExpectExec("status = 'sent'", dbtest.Tag(0), errBoom).
		ExpectExec("SET status = $4", dbtest.Tag(1), nil).
		ExpectExec("SET status = $4", dbtest.Tag(0), errBoom)
	ctx := tx.Context(t.Context())

	job, err := store.CreateJob(ctx, garageID, usecase.NewJob{AgentID: agentID, Format: usecase.FormatEscPos, Paper: printmodel.K80})
	require.NoError(t, err)
	assert.Equal(t, usecase.JobQueued, job.Status)

	_, err = store.CreateJob(ctx, garageID, usecase.NewJob{})
	require.ErrorIs(t, err, errBoom)

	got, err := store.GetJob(ctx, garageID, documentID)
	require.NoError(t, err)
	assert.Equal(t, printmodel.TypeQuote, got.DocumentType)

	_, err = store.GetJob(ctx, garageID, documentID)
	require.ErrorIs(t, err, usecase.ErrJobNotFound)
	_, err = store.GetJob(ctx, garageID, documentID)
	require.ErrorIs(t, err, errBoom)
	_, err = store.GetJob(ctx, garageID, "bad")
	require.ErrorIs(t, err, usecase.ErrJobNotFound)

	require.NoError(t, store.PurgeJobs(ctx, garageID, at))
	require.ErrorIs(t, store.PurgeJobs(ctx, garageID, at), errBoom)

	jobs, err := store.PendingJobs(ctx, session, at, 10)
	require.NoError(t, err)
	require.Len(t, jobs, 1)
	assert.Equal(t, usecase.AgentJob{
		ID: documentID, Queue: "XP-80C", DocumentType: printmodel.TypeQuote, Format: usecase.FormatEscPos, Paper: printmodel.K80, Data: []byte{0x1b},
	}, jobs[0])

	for range 4 {
		_, err = store.PendingJobs(ctx, session, at, 10)
		require.ErrorIs(t, err, errBoom)
	}

	require.NoError(t, store.MarkSent(ctx, session, []string{documentID}, at))
	require.ErrorIs(t, store.MarkSent(ctx, session, []string{documentID}, at), errBoom)

	require.NoError(t, store.FinishJob(ctx, session, documentID, usecase.JobPrinted, "", at))
	require.ErrorIs(t, store.FinishJob(ctx, session, documentID, usecase.JobFailed, "x", at), errBoom)
	require.NoError(t, store.FinishJob(ctx, session, "bad", usecase.JobPrinted, "", at), "not a job id: nothing to record")
}
