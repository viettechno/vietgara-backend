//go:build integration

package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/document/printmodel"
	"github.com/viettechno/vietgara-backend/internal/document/repository"
	"github.com/viettechno/vietgara-backend/internal/document/usecase"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platform/db/pgtest"
)

// TestAgentStore runs the print agents and their jobs as the RLS-bound
// application role (migration 100): pairing through the lookup functions, a
// key that stops working, one connection at a time, an agent printer bound
// to an agent of its own garage, and the jobs of a connection.
func TestAgentStore(t *testing.T) { //nolint:funlen // one scenario, step by step
	pg := pgtest.StartMigrated(t)
	ctx := t.Context()
	pool := pg.AppPool(t)
	store := repository.NewPostgresAgentStore(pool)
	printers := repository.NewPostgresPrinterStore(pool)
	txm := platformdb.NewTxManager(pool)

	userID, garageA, garageB := uuid.NewString(), uuid.NewString(), uuid.NewString()
	_, err := pg.Admin.ExecContext(ctx, `INSERT INTO users (id, email, full_name) VALUES ($1, 'owner@example.com', 'Owner')`, userID)
	require.NoError(t, err)

	for _, id := range []string{garageA, garageB} {
		_, err = pg.Admin.ExecContext(ctx, `INSERT INTO garages (id, user_id, name) VALUES ($1, $2, $3)`, id, userID, "garage-"+id[:8])
		require.NoError(t, err)
	}

	in := func(garageID string, fn func(ctx context.Context) error) error {
		t.Helper()

		return txm.WithTransaction(ctx, platformdb.Scope{GarageID: garageID, UserID: userID}, fn)
	}

	code, key := []byte("code-hash"), []byte("key-hash")
	expires := time.Now().Add(15 * time.Minute)

	var agent usecase.PrintAgent

	require.NoError(t, in(garageA, func(ctx context.Context) error {
		agent, err = store.CreateAgent(ctx, garageA, "Quầy", code, expires)
		require.NoError(t, err)
		assert.False(t, agent.Paired)
		assert.False(t, agent.Connected)
		assert.WithinDuration(t, expires, *agent.PairingExpiresAt, time.Millisecond)
		assert.Empty(t, agent.Printers)

		return nil
	}))

	err = in(garageA, func(ctx context.Context) error {
		_, err := store.CreateAgent(ctx, garageA, "QUẦY", []byte("other"), expires)

		return err
	})
	require.ErrorIs(t, err, usecase.ErrAgentNameTaken, "names are unique without regard to case")

	// Pairing: found by the code without a garage, completed in it.
	agentID, garageID, err := store.FindAgentByPairingCode(ctx, code)
	require.NoError(t, err)
	assert.Equal(t, agent.ID, agentID)
	assert.Equal(t, garageA, garageID)

	report := usecase.AgentReport{
		Hostname: "QUAY-PC", OS: "windows/amd64", Version: "0.1.0",
		Printers: []usecase.AgentPrinter{{Name: "XP-80C", Default: true}, {Name: "HP LaserJet"}},
	}

	require.NoError(t, in(garageA, func(ctx context.Context) error {
		paired, err := store.CompletePairing(ctx, garageA, agent.ID, code, key, report)
		require.NoError(t, err)
		assert.True(t, paired.Paired)
		assert.Nil(t, paired.PairingExpiresAt)
		assert.Equal(t, report.Printers, paired.Printers)

		_, err = store.CompletePairing(ctx, garageA, agent.ID, code, []byte("again"), report)
		require.ErrorIs(t, err, usecase.ErrAgentNotFound, "a code is used once")

		return nil
	}))

	_, _, err = store.FindAgentByPairingCode(ctx, code)
	require.ErrorIs(t, err, usecase.ErrAgentNotFound)

	agentID, garageID, err = store.FindAgentByKey(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, agent.ID, agentID)
	assert.Equal(t, garageA, garageID)

	// One connection at a time.
	first := usecase.AgentSession{AgentID: agent.ID, GarageID: garageA, KeyHash: key, ConnectionID: uuid.NewString(), ConnectedAt: time.Now()}
	second := first
	second.ConnectionID = uuid.NewString()

	require.NoError(t, in(garageA, func(ctx context.Context) error {
		require.NoError(t, store.SetConnection(ctx, first))
		require.NoError(t, store.Heartbeat(ctx, first))
		require.NoError(t, store.SetConnection(ctx, second))
		require.ErrorIs(t, store.Heartbeat(ctx, first), usecase.ErrAgentReplaced)
		require.NoError(t, store.ClearConnection(ctx, first), "the older connection leaves the newer in place")

		got, err := store.GetAgent(ctx, garageA, agent.ID)
		require.NoError(t, err)
		assert.True(t, got.Connected)

		return store.SaveReport(ctx, second, usecase.AgentReport{Hostname: "QUAY-PC", Printers: report.Printers[:1]})
	}))

	// The other garage sees nothing and cannot bind its printer to the agent.
	require.NoError(t, in(garageB, func(ctx context.Context) error {
		list, err := store.ListAgents(ctx, garageB)
		require.NoError(t, err)
		assert.Empty(t, list)

		_, err = store.GetAgent(ctx, garageB, agent.ID)
		require.ErrorIs(t, err, usecase.ErrAgentNotFound)

		return nil
	}))

	err = in(garageB, func(ctx context.Context) error {
		_, err := printers.CreatePrinter(ctx, garageB, usecase.Printer{
			Name: "Mượn", Protocol: usecase.ProtocolAgent, AgentID: agent.ID, Queue: "XP-80C", Paper: printmodel.K80,
		})

		return err
	})
	require.Error(t, err, "the foreign key matches the garage")

	// An agent printer, once per PC printer.
	var counter usecase.Printer

	require.NoError(t, in(garageA, func(ctx context.Context) error {
		counter, err = printers.CreatePrinter(ctx, garageA, usecase.Printer{
			Name: "Quầy thu ngân", Protocol: usecase.ProtocolAgent, AgentID: agent.ID, Queue: "XP-80C", Paper: printmodel.K80,
			DocumentTypes: []printmodel.DocumentType{printmodel.TypePaymentVoucher},
		})
		require.NoError(t, err)
		assert.Equal(t, agent.ID, counter.AgentID)
		assert.Equal(t, "XP-80C", counter.Queue)
		assert.Zero(t, counter.Port)

		pdf, err := printers.CreatePrinter(ctx, garageA, usecase.Printer{
			Name: "Văn phòng", Protocol: usecase.ProtocolAgent, AgentID: agent.ID, Queue: "HP LaserJet",
		})
		require.NoError(t, err)
		assert.Empty(t, pdf.Paper, "an agent's PDF printer may print on the garage's setting")

		return nil
	}))

	err = in(garageA, func(ctx context.Context) error {
		_, err := printers.CreatePrinter(ctx, garageA, usecase.Printer{
			Name: "Quầy 2", Protocol: usecase.ProtocolAgent, AgentID: agent.ID, Queue: "xp-80c", Paper: printmodel.K57,
		})

		return err
	})
	require.ErrorIs(t, err, usecase.ErrPrinterQueueTaken)

	// Jobs: queued, sent once on a connection, again on a newer one,
	// finished by the agent only, expired when late, purged after a week.
	now := time.Now()

	var queued, late usecase.PrintJob

	require.NoError(t, in(garageA, func(ctx context.Context) error {
		queued, err = store.CreateJob(ctx, garageA, usecase.NewJob{
			AgentID: agent.ID, PrinterID: counter.ID, PrinterName: counter.Name, Queue: counter.Queue,
			DocumentType: printmodel.TypePaymentVoucher, Format: usecase.FormatEscPos, Paper: printmodel.K80,
			Data: []byte{0x1b, 0x40}, ExpiresAt: now.Add(time.Hour),
		})
		require.NoError(t, err)
		assert.Equal(t, usecase.JobQueued, queued.Status)

		late, err = store.CreateJob(ctx, garageA, usecase.NewJob{
			AgentID: agent.ID, PrinterID: counter.ID, PrinterName: counter.Name, Queue: counter.Queue,
			Format: usecase.FormatPDF, Paper: printmodel.A4, Data: []byte("%PDF"), ExpiresAt: now.Add(-time.Second),
		})
		require.NoError(t, err)

		jobs, err := store.PendingJobs(ctx, second, now, 10)
		require.NoError(t, err)
		require.Len(t, jobs, 1, "the late job expired")
		assert.Equal(t, queued.ID, jobs[0].ID)
		assert.Equal(t, []byte{0x1b, 0x40}, jobs[0].Data)
		assert.Equal(t, printmodel.TypePaymentVoucher, jobs[0].DocumentType)

		require.NoError(t, store.MarkSent(ctx, second, []string{queued.ID}, now))

		jobs, err = store.PendingJobs(ctx, second, now, 10)
		require.NoError(t, err)
		assert.Empty(t, jobs, "sent on this connection")

		third := second
		third.ConnectedAt = now.Add(time.Second)
		jobs, err = store.PendingJobs(ctx, third, now, 10)
		require.NoError(t, err)
		assert.Len(t, jobs, 1, "sent again on a newer connection")

		other := second
		other.AgentID = uuid.NewString()
		require.NoError(t, store.FinishJob(ctx, other, queued.ID, usecase.JobPrinted, "", now))
		require.NoError(t, store.FinishJob(ctx, second, "not-a-uuid", usecase.JobPrinted, "", now))

		got, err := store.GetJob(ctx, garageA, queued.ID)
		require.NoError(t, err)
		assert.Equal(t, usecase.JobSent, got.Status, "another agent's result changes nothing")

		require.NoError(t, store.FinishJob(ctx, second, queued.ID, usecase.JobFailed, "Hết giấy", now))

		got, err = store.GetJob(ctx, garageA, queued.ID)
		require.NoError(t, err)
		assert.Equal(t, usecase.JobFailed, got.Status)
		assert.Equal(t, "Hết giấy", got.Error)

		got, err = store.GetJob(ctx, garageA, late.ID)
		require.NoError(t, err)
		assert.Equal(t, usecase.JobExpired, got.Status)

		return nil
	}))

	var data []byte
	require.NoError(t, pg.Admin.QueryRowContext(ctx, `SELECT data FROM print_jobs WHERE id = $1`, queued.ID).Scan(&data))
	assert.Empty(t, data, "the bytes of a finished job are cleared")

	require.NoError(t, in(garageB, func(ctx context.Context) error {
		_, err := store.GetJob(ctx, garageB, queued.ID)
		require.ErrorIs(t, err, usecase.ErrJobNotFound)

		return nil
	}))

	// A new pairing code revokes the key; deleting the agent removes its
	// printers and jobs.
	require.NoError(t, in(garageA, func(ctx context.Context) error {
		require.NoError(t, store.PurgeJobs(ctx, garageA, now.Add(-time.Hour)))

		_, err := store.GetJob(ctx, garageA, queued.ID)
		require.NoError(t, err, "a recent job is kept")

		_, err = store.SetPairingCode(ctx, garageA, agent.ID, []byte("new-code"), expires)
		require.NoError(t, err)

		require.ErrorIs(t, store.Heartbeat(ctx, second), usecase.ErrAgentRevoked)

		return nil
	}))

	_, _, err = store.FindAgentByKey(ctx, key)
	require.ErrorIs(t, err, usecase.ErrAgentNotFound)

	require.NoError(t, in(garageA, func(ctx context.Context) error {
		require.NoError(t, store.DeleteAgent(ctx, garageA, agent.ID))

		list, err := printers.ListPrinters(ctx, garageA)
		require.NoError(t, err)
		assert.Empty(t, list, "the agent's printers go with it")

		_, err = store.GetJob(ctx, garageA, queued.ID)
		require.ErrorIs(t, err, usecase.ErrJobNotFound, "and its jobs")

		require.ErrorIs(t, store.DeleteAgent(ctx, garageA, agent.ID), usecase.ErrAgentNotFound)

		return nil
	}))
}
