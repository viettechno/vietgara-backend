package usecase_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/document/agenttest"
	"github.com/viettechno/vietgara-backend/internal/document/printmodel"
	"github.com/viettechno/vietgara-backend/internal/document/usecase"
	"github.com/viettechno/vietgara-backend/internal/platform/access"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

type agentEnv struct {
	uc       *usecase.DocumentUseCase
	d        *deps
	printers *fakePrinters
	store    *agenttest.Store
	notifier *agenttest.Notifier
	now      time.Time
}

func newAgentEnv(t *testing.T) *agentEnv {
	t.Helper()

	uc, d, printers, _ := newPrintingUseCase(t)
	e := &agentEnv{uc: uc, d: d, printers: printers, notifier: &agenttest.Notifier{}, now: time.Date(2026, 11, 28, 9, 0, 0, 0, time.UTC)}
	e.store = agenttest.NewStore(func() time.Time { return e.now })
	uc.WithAgents(e.store, e.store, agenttest.PassTx{}, e.notifier)
	uc.Now = func() time.Time { return e.now }

	return e
}

// asMember is a member of the garage holding perms.
func asMember(ctx context.Context, perms ...access.Permission) context.Context {
	return access.WithAccess(ctx, &access.Access{GarageID: garageID, Active: true, Permissions: perms})
}

// paired creates an agent and pairs it, returning the agent and its key.
func (e *agentEnv) paired(t *testing.T, name string) (usecase.PrintAgent, string) {
	t.Helper()

	e.d.Garages.EXPECT().Get(mock.Anything, garageID).Return(defaultGarage(), nil).Maybe()

	agent, code, err := e.uc.CreateAgent(t.Context(), garageID, name)
	require.NoError(t, err)

	result, err := e.uc.PairAgent(t.Context(), code, usecase.AgentReport{Hostname: "QUAY-PC", OS: "windows/amd64", Version: "0.1.0"})
	require.NoError(t, err)

	return agent, result.Key
}

func (e *agentEnv) agentPrinter(agentID string, paper printmodel.Paper) usecase.Printer {
	p := usecase.Printer{ID: "pr-" + string(paper), Name: "Máy " + string(paper), Protocol: usecase.ProtocolAgent, AgentID: agentID, Queue: "XP-80C", Paper: paper}
	e.printers.printers = append(e.printers.printers, p)

	return p
}

func TestCreateAgent(t *testing.T) {
	t.Parallel()

	t.Run("an agent gets a single-use pairing code valid for 15 minutes", func(t *testing.T) {
		t.Parallel()

		e := newAgentEnv(t)

		agent, code, err := e.uc.CreateAgent(t.Context(), garageID, "  Máy quầy  ")
		require.NoError(t, err)
		assert.Equal(t, "Máy quầy", agent.Name)
		assert.False(t, agent.Paired)
		assert.Regexp(t, `^[A-HJKMNP-Z2-9]{4}-[A-HJKMNP-Z2-9]{4}$`, code)
		assert.Equal(t, e.now.Add(usecase.PairingCodeTTL), *agent.PairingExpiresAt)
	})

	t.Run("names are required, at most 60 characters and unique", func(t *testing.T) {
		t.Parallel()

		e := newAgentEnv(t)

		_, _, err := e.uc.CreateAgent(t.Context(), garageID, "  ")
		assertValidation(t, err, "name")

		_, _, err = e.uc.CreateAgent(t.Context(), garageID, strings.Repeat("a", 61))
		assertValidation(t, err, "name")

		_, _, err = e.uc.CreateAgent(t.Context(), garageID, "Quầy")
		require.NoError(t, err)

		_, _, err = e.uc.CreateAgent(t.Context(), garageID, "QUẦY")
		assertValidation(t, err, "name")
	})

	t.Run("a garage pairs at most 10 agents", func(t *testing.T) {
		t.Parallel()

		e := newAgentEnv(t)
		for i := range usecase.MaxAgents {
			_, _, err := e.uc.CreateAgent(t.Context(), garageID, "PC "+string(rune('A'+i)))
			require.NoError(t, err)
		}

		_, _, err := e.uc.CreateAgent(t.Context(), garageID, "PC 11")
		assertValidation(t, err, "agent")
	})
}

func TestPairAgent(t *testing.T) {
	t.Parallel()

	t.Run("a code pairs once, in any case and without the dash", func(t *testing.T) {
		t.Parallel()

		e := newAgentEnv(t)
		e.d.Garages.EXPECT().Get(mock.Anything, garageID).Return(defaultGarage(), nil)

		agent, code, err := e.uc.CreateAgent(t.Context(), garageID, "Quầy")
		require.NoError(t, err)

		result, err := e.uc.PairAgent(t.Context(), strings.ToLower(strings.ReplaceAll(code, "-", "")),
			usecase.AgentReport{
				Hostname: " QUAY-PC\n", OS: "windows/amd64", Version: "0.1.0",
				Printers: []usecase.AgentPrinter{{Name: "XP-80C", Default: true}, {Name: "xp-80c"}},
			})
		require.NoError(t, err)
		assert.True(t, strings.HasPrefix(result.Key, "vgagent_"))
		assert.Equal(t, agent.ID, result.Agent.ID)
		assert.Equal(t, "District 1 Garage", result.GarageName)
		assert.Equal(t, "QUAY-PC", result.Agent.Hostname, "trimmed, control characters dropped")
		assert.Equal(t, []usecase.AgentPrinter{{Name: "XP-80C", Default: true}}, result.Agent.Printers, "a printer once")

		_, err = e.uc.PairAgent(t.Context(), code, usecase.AgentReport{})
		assertAppError(t, err, apperror.CodeNotFound)
	})

	t.Run("a wrong, short or expired code is not found", func(t *testing.T) {
		t.Parallel()

		e := newAgentEnv(t)

		_, code, err := e.uc.CreateAgent(t.Context(), garageID, "Quầy")
		require.NoError(t, err)

		for _, wrong := range []string{"", "ABCD", "ABCD-EFGH", code + "X"} {
			_, err := e.uc.PairAgent(t.Context(), wrong, usecase.AgentReport{})
			assertAppError(t, err, apperror.CodeNotFound)
		}

		e.now = e.now.Add(usecase.PairingCodeTTL)
		_, err = e.uc.PairAgent(t.Context(), code, usecase.AgentReport{})
		assertAppError(t, err, apperror.CodeNotFound)
	})

	t.Run("printers with odd names are left out, and the list stops at 100", func(t *testing.T) {
		t.Parallel()

		e := newAgentEnv(t)
		e.d.Garages.EXPECT().Get(mock.Anything, garageID).Return(defaultGarage(), nil)

		_, code, err := e.uc.CreateAgent(t.Context(), garageID, "Quầy")
		require.NoError(t, err)

		report := usecase.AgentReport{Printers: []usecase.AgentPrinter{{Name: "a\tb"}, {Name: strings.Repeat("x", 201)}}}
		for i := range 101 {
			report.Printers = append(report.Printers, usecase.AgentPrinter{Name: "P" + strings.Repeat("x", i)})
		}

		result, err := e.uc.PairAgent(t.Context(), code, report)
		require.NoError(t, err)
		require.Len(t, result.Agent.Printers, 100)
		assert.Equal(t, "P", result.Agent.Printers[0].Name)
	})
}

func TestAgentKey(t *testing.T) {
	t.Parallel()

	t.Run("the key authenticates the connection until a new code is issued", func(t *testing.T) {
		t.Parallel()

		e := newAgentEnv(t)
		agent, key := e.paired(t, "Quầy")

		s, err := e.uc.AuthenticateAgent(t.Context(), key, "conn-1")
		require.NoError(t, err)
		assert.Equal(t, agent.ID, s.AgentID)

		listed, err := e.uc.Agents(t.Context(), garageID)
		require.NoError(t, err)
		require.Len(t, listed, 1)
		assert.True(t, listed[0].Connected)
		assert.True(t, listed[0].Paired)

		_, code, err := e.uc.IssuePairingCode(t.Context(), garageID, agent.ID)
		require.NoError(t, err)
		assert.NotEmpty(t, code)
		assert.Equal(t, []string{agent.ID}, e.notifier.Revoked, "the connection is told to close")

		require.ErrorIs(t, e.uc.AgentHeartbeat(t.Context(), s), usecase.ErrAgentRevoked)

		_, err = e.uc.AuthenticateAgent(t.Context(), key, "conn-2")
		require.ErrorIs(t, err, usecase.ErrAgentRevoked)
	})

	t.Run("a newer connection replaces the older", func(t *testing.T) {
		t.Parallel()

		e := newAgentEnv(t)
		_, key := e.paired(t, "Quầy")

		first, err := e.uc.AuthenticateAgent(t.Context(), key, "conn-1")
		require.NoError(t, err)

		second, err := e.uc.AuthenticateAgent(t.Context(), key, "conn-2")
		require.NoError(t, err)

		require.ErrorIs(t, e.uc.AgentHeartbeat(t.Context(), first), usecase.ErrAgentReplaced)
		require.NoError(t, e.uc.AgentHeartbeat(t.Context(), second))

		require.NoError(t, e.uc.AgentDisconnected(t.Context(), first))
		listed, err := e.uc.Agents(t.Context(), garageID)
		require.NoError(t, err)
		assert.True(t, listed[0].Connected, "the older connection does not disconnect the newer")
	})

	t.Run("an unknown or malformed key is refused", func(t *testing.T) {
		t.Parallel()

		e := newAgentEnv(t)

		for _, key := range []string{"", "Bearer", "vgagent_unknown", "x" + strings.Repeat("a", 200)} {
			_, err := e.uc.AuthenticateAgent(t.Context(), key, "conn")
			require.ErrorIs(t, err, usecase.ErrAgentRevoked)
		}
	})

	t.Run("deleting the agent revokes it", func(t *testing.T) {
		t.Parallel()

		e := newAgentEnv(t)
		agent, key := e.paired(t, "Quầy")

		require.NoError(t, e.uc.DeleteAgent(t.Context(), garageID, agent.ID))
		assert.Equal(t, []string{agent.ID}, e.notifier.Revoked)

		_, err := e.uc.AuthenticateAgent(t.Context(), key, "conn")
		require.ErrorIs(t, err, usecase.ErrAgentRevoked)

		assertAppError(t, e.uc.DeleteAgent(t.Context(), garageID, agent.ID), apperror.CodeNotFound)
	})
}

func TestAgentPrinter(t *testing.T) {
	t.Parallel()

	t.Run("an agent printer names the agent and the PC's printer, without an address", func(t *testing.T) {
		t.Parallel()

		e := newAgentEnv(t)
		agent, _ := e.paired(t, "Quầy")

		p, err := e.uc.CreatePrinter(t.Context(), garageID, usecase.Printer{
			Name: "Quầy thu ngân", Protocol: usecase.ProtocolAgent, AgentID: agent.ID, Queue: " XP-80C ", Paper: printmodel.K80,
			DocumentTypes: []printmodel.DocumentType{printmodel.TypePaymentVoucher},
		})
		require.NoError(t, err)
		assert.Equal(t, "XP-80C", p.Queue)
		assert.Zero(t, p.Port)

		pdf, err := e.uc.CreatePrinter(t.Context(), garageID, usecase.Printer{
			Name: "Văn phòng", Protocol: usecase.ProtocolAgent, AgentID: agent.ID, Queue: "HP LaserJet",
			DocumentTypes: []printmodel.DocumentType{printmodel.TypeRepairOrder},
		})
		require.NoError(t, err, "no paper prints PDFs on the garage's setting")
		assert.Equal(t, []printmodel.DocumentType{printmodel.TypeRepairOrder}, pdf.DocumentTypes)
	})

	t.Run("a wrong agent, an address, a missing queue or a document the paper cannot take are refused", func(t *testing.T) {
		t.Parallel()

		e := newAgentEnv(t)
		agent, _ := e.paired(t, "Quầy")
		base := usecase.Printer{Name: "Quầy", Protocol: usecase.ProtocolAgent, AgentID: agent.ID, Queue: "XP-80C", Paper: printmodel.K80}

		for field, p := range map[string]usecase.Printer{
			"agentId": {Name: "Quầy", Protocol: usecase.ProtocolAgent, AgentID: "99999999-9999-9999-9999-999999999999", Queue: "XP", Paper: printmodel.K80},
			"host":    func() usecase.Printer { p := base; p.Host = "192.168.1.5"; return p }(),
			"queue":   func() usecase.Printer { p := base; p.Queue = "a\tb"; return p }(),
			"documentTypes": func() usecase.Printer {
				p := base
				p.DocumentTypes = []printmodel.DocumentType{printmodel.TypeRepairOrder}

				return p
			}(),
		} {
			_, err := e.uc.CreatePrinter(t.Context(), garageID, p)
			assertValidation(t, err, field)
		}
	})
}

func TestCreatePrintJob(t *testing.T) {
	t.Parallel()

	t.Run("a voucher on an agent's K80 roll is rendered, logged and queued for the agent", func(t *testing.T) {
		t.Parallel()

		e := newAgentEnv(t)
		expectVoucher(e.d)
		agent, _ := e.paired(t, "Quầy")
		p := e.agentPrinter(agent.ID, printmodel.K80)

		job, err := e.uc.CreatePrintJob(asMember(t.Context(), access.BillingRead), garageID, p.ID,
			usecase.JobDocument{Type: printmodel.TypePaymentVoucher, ID: settlementID, PaymentID: paymentID})
		require.NoError(t, err)
		assert.Equal(t, usecase.JobQueued, job.Status)
		assert.Equal(t, p.Name, job.PrinterName)
		assert.Equal(t, []string{agent.ID}, e.notifier.Queued, "the agent's connection is woken")

		stored := e.store.Job(job.ID)
		assert.Equal(t, usecase.FormatEscPos, stored.Job.Format)
		assert.Equal(t, "XP-80C", stored.Job.Queue)
		assert.True(t, bytes.HasPrefix(stored.Job.Data, []byte{0x1b, 0x40}))
		assert.Equal(t, e.now.Add(usecase.PrintJobTTL), stored.ExpiresAt)
		require.Len(t, e.d.Prints.log, 1)
		assert.Equal(t, printmodel.K80, e.d.Prints.log[0].Paper)
	})

	t.Run("a PDF without a size prints on the garage's setting and names it", func(t *testing.T) {
		t.Parallel()

		e := newAgentEnv(t)
		expectQuote(e.d)
		e.d.Prints.settings = map[printmodel.DocumentType]usecase.PrintSetting{printmodel.TypeQuote: {Paper: printmodel.A5}}
		agent, _ := e.paired(t, "Quầy")
		p := e.agentPrinter(agent.ID, "")

		job, err := e.uc.CreatePrintJob(asMember(t.Context(), access.QuoteRead), garageID, p.ID,
			usecase.JobDocument{Type: printmodel.TypeQuote, ID: quoteID})
		require.NoError(t, err)

		stored := e.store.Job(job.ID)
		assert.Equal(t, usecase.FormatPDF, stored.Job.Format)
		assert.Equal(t, printmodel.A5, stored.Job.Paper, "the agent prints it on A5, unscaled")
		assert.True(t, bytes.HasPrefix(stored.Job.Data, []byte("%PDF")))
	})

	t.Run("the document's permission is needed, a test page needs none", func(t *testing.T) {
		t.Parallel()

		e := newAgentEnv(t)
		agent, _ := e.paired(t, "Quầy")
		p := e.agentPrinter(agent.ID, printmodel.K80)

		_, err := e.uc.CreatePrintJob(asMember(t.Context(), access.QuoteRead), garageID, p.ID,
			usecase.JobDocument{Type: printmodel.TypePaymentVoucher, ID: settlementID, PaymentID: paymentID})
		assertAppError(t, err, apperror.CodePermissionDenied)

		_, err = e.uc.CreatePrintJob(asMember(t.Context()), garageID, p.ID,
			usecase.JobDocument{Labels: []usecase.LabelCopies{{PartID: "p1", Copies: 1}}})
		assertAppError(t, err, apperror.CodePermissionDenied)

		job, err := e.uc.CreatePrintJob(asMember(t.Context()), garageID, p.ID, usecase.JobDocument{TestPage: true})
		require.NoError(t, err)
		assert.Empty(t, job.DocumentType)
		assert.Empty(t, e.d.Prints.log, "a test page is not logged")
	})

	t.Run("a printer of the app, a document the paper cannot take and no document are refused", func(t *testing.T) {
		t.Parallel()

		e := newAgentEnv(t)
		agent, _ := e.paired(t, "Quầy")
		roll := e.agentPrinter(agent.ID, printmodel.K80)
		label := e.agentPrinter(agent.ID, printmodel.Label5030)
		e.printers.printers = append(e.printers.printers, usecase.Printer{ID: "raw", Protocol: usecase.ProtocolRaw, Paper: printmodel.K80})
		ctx := asMember(t.Context(), access.RepairOrderRead, access.QuoteRead, access.CatalogueRead)

		_, err := e.uc.CreatePrintJob(ctx, garageID, "raw", usecase.JobDocument{TestPage: true})
		assertValidation(t, err, "printerId")

		_, err = e.uc.CreatePrintJob(ctx, garageID, roll.ID, usecase.JobDocument{Type: printmodel.TypeRepairOrder, ID: repairOrderID})
		assertValidation(t, err, "printerId")

		_, err = e.uc.CreatePrintJob(ctx, garageID, label.ID, usecase.JobDocument{Type: printmodel.TypeQuote, ID: quoteID})
		assertValidation(t, err, "printerId")

		_, err = e.uc.CreatePrintJob(ctx, garageID, roll.ID, usecase.JobDocument{Labels: []usecase.LabelCopies{{PartID: "p1", Copies: 1}}})
		assertValidation(t, err, "paperSize")

		_, err = e.uc.CreatePrintJob(ctx, garageID, roll.ID, usecase.JobDocument{})
		assertValidation(t, err, "document")

		_, err = e.uc.CreatePrintJob(ctx, garageID, "missing", usecase.JobDocument{TestPage: true})
		assertAppError(t, err, apperror.CodeNotFound)
		assert.Empty(t, e.notifier.Queued)
	})

	t.Run("jobs older than a week are purged when a job is created", func(t *testing.T) {
		t.Parallel()

		e := newAgentEnv(t)
		agent, _ := e.paired(t, "Quầy")
		p := e.agentPrinter(agent.ID, printmodel.K80)
		ctx := asMember(t.Context())

		old, err := e.uc.CreatePrintJob(ctx, garageID, p.ID, usecase.JobDocument{TestPage: true})
		require.NoError(t, err)

		e.now = e.now.Add(usecase.JobRetention + time.Minute)
		_, err = e.uc.CreatePrintJob(ctx, garageID, p.ID, usecase.JobDocument{TestPage: true})
		require.NoError(t, err)

		_, err = e.uc.PrintJob(t.Context(), garageID, old.ID)
		assertAppError(t, err, apperror.CodeNotFound)
	})
}

func TestAgentJobs(t *testing.T) {
	t.Parallel()

	t.Run("a job is sent once on a connection, again on the next, and finished by the agent", func(t *testing.T) {
		t.Parallel()

		e := newAgentEnv(t)
		agent, key := e.paired(t, "Quầy")
		p := e.agentPrinter(agent.ID, printmodel.K80)

		job, err := e.uc.CreatePrintJob(asMember(t.Context()), garageID, p.ID, usecase.JobDocument{TestPage: true})
		require.NoError(t, err)

		s, err := e.uc.AuthenticateAgent(t.Context(), key, "conn-1")
		require.NoError(t, err)

		jobs, err := e.uc.AgentJobs(t.Context(), s)
		require.NoError(t, err)
		require.Len(t, jobs, 1)
		assert.Equal(t, "VietGara test page", jobs[0].JobTitle())
		require.NoError(t, e.uc.AgentJobsSent(t.Context(), s, []string{job.ID}))

		jobs, err = e.uc.AgentJobs(t.Context(), s)
		require.NoError(t, err)
		assert.Empty(t, jobs, "not sent twice on one connection")

		e.now = e.now.Add(time.Second)
		s2, err := e.uc.AuthenticateAgent(t.Context(), key, "conn-2")
		require.NoError(t, err)

		jobs, err = e.uc.AgentJobs(t.Context(), s2)
		require.NoError(t, err)
		assert.Len(t, jobs, 1, "sent again after a reconnection")

		require.NoError(t, e.uc.AgentJobDone(t.Context(), s2, job.ID, false, ""))

		got, err := e.uc.PrintJob(t.Context(), garageID, job.ID)
		require.NoError(t, err)
		assert.Equal(t, usecase.JobFailed, got.Status)
		assert.Equal(t, "the print agent could not print", got.Error)
	})

	t.Run("a job not printed within an hour expires and is not sent", func(t *testing.T) {
		t.Parallel()

		e := newAgentEnv(t)
		agent, key := e.paired(t, "Quầy")
		p := e.agentPrinter(agent.ID, printmodel.K80)

		job, err := e.uc.CreatePrintJob(asMember(t.Context()), garageID, p.ID, usecase.JobDocument{TestPage: true})
		require.NoError(t, err)

		e.now = e.now.Add(usecase.PrintJobTTL)

		got, err := e.uc.PrintJob(t.Context(), garageID, job.ID)
		require.NoError(t, err)
		assert.Equal(t, usecase.JobExpired, got.Status, "reads expired before the agent comes back")

		s, err := e.uc.AuthenticateAgent(t.Context(), key, "conn")
		require.NoError(t, err)

		jobs, err := e.uc.AgentJobs(t.Context(), s)
		require.NoError(t, err)
		assert.Empty(t, jobs)
	})

	t.Run("another agent's result changes nothing", func(t *testing.T) {
		t.Parallel()

		e := newAgentEnv(t)
		agent, _ := e.paired(t, "Quầy")
		_, otherKey := e.paired(t, "Kho")
		p := e.agentPrinter(agent.ID, printmodel.K80)

		job, err := e.uc.CreatePrintJob(asMember(t.Context()), garageID, p.ID, usecase.JobDocument{TestPage: true})
		require.NoError(t, err)

		other, err := e.uc.AuthenticateAgent(t.Context(), otherKey, "conn")
		require.NoError(t, err)
		require.NoError(t, e.uc.AgentJobDone(t.Context(), other, job.ID, true, ""))

		got, err := e.uc.PrintJob(t.Context(), garageID, job.ID)
		require.NoError(t, err)
		assert.Equal(t, usecase.JobQueued, got.Status)
	})
}

func assertValidation(t *testing.T, err error, field string) {
	t.Helper()

	appErr, ok := apperror.As(err)
	require.True(t, ok, "an app error: %v", err)
	assert.Equal(t, apperror.CodeInvalidArgument, appErr.Code, err.Error())
	require.NotEmpty(t, appErr.Details, err.Error())
	assert.Equal(t, field, appErr.Details[0].Field, err.Error())
}
