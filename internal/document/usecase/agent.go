package usecase

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"math/big"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// Errors of the agent store.
var (
	ErrAgentNotFound  = errors.New("document: print agent not found")
	ErrAgentNameTaken = errors.New("document: a print agent of the garage has that name")
	// ErrAgentRevoked is returned to a connection whose key no longer
	// works: the agent was removed or paired again.
	ErrAgentRevoked = errors.New("document: the print agent's key was revoked")
	// ErrAgentReplaced is returned to a connection that a newer connection
	// of the same agent replaced.
	ErrAgentReplaced = errors.New("document: another connection of the print agent took over")
)

// The limits of the print agents (Release 13.1).
const (
	MaxAgents = 10
	// PairingCodeTTL is how long a pairing code works.
	PairingCodeTTL = 15 * time.Minute
	// AgentOnline is how long after its last heartbeat an agent still counts
	// as connected: the connections beat every 30 seconds.
	AgentOnline = 90 * time.Second

	pairingCodeLength  = 8
	pairingCodeLetters = "ABCDEFGHJKMNPQRSTUVWXYZ23456789" // no I, L, O, 0, 1
	agentKeyBytes      = 32
	agentKeyPrefix     = "vgagent_"
	maxAgentPrinters   = 100
	maxAgentQueue      = 200
	maxAgentHostname   = 253
	maxAgentLabel      = 40
)

// Transactor runs fn in one transaction of the scope; implemented by
// platformdb.TxManager. The agent's own calls run outside an RPC, so they
// open their garage's transaction themselves.
type Transactor interface {
	WithTransaction(ctx context.Context, scope platformdb.Scope, fn func(ctx context.Context) error) error
}

// AgentNotifier tells the connection of an agent that it has a job, or that
// it must close because its key was revoked; implemented by the handler's
// hub. It is called after the transaction commits.
type AgentNotifier interface {
	JobQueued(agentID string)
	AgentRevoked(agentID string)
}

// AgentPrinter is a printer of an agent's PC.
type AgentPrinter struct {
	Name    string `json:"name"`
	Default bool   `json:"default,omitempty"`
}

// PrintAgent is a print agent of a garage (FR-DOC-08).
type PrintAgent struct {
	ID               string
	Name             string
	Paired           bool
	PairingExpiresAt *time.Time
	Connected        bool
	LastSeenAt       *time.Time
	Hostname         string
	OS               string
	Version          string
	Printers         []AgentPrinter
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// AgentReport is what an agent says about its PC.
type AgentReport struct {
	Hostname, OS, Version string
	Printers              []AgentPrinter
}

// AgentSession is an authenticated agent connection.
type AgentSession struct {
	AgentID  string
	GarageID string
	KeyHash  []byte
	// ConnectionID tells this connection from a newer one of the agent.
	ConnectionID string
	// ConnectedAt is when the connection started: a job sent since was
	// sent on it, one sent before is sent again.
	ConnectedAt time.Time
}

// AgentStore keeps the print agents and their jobs; implemented by
// repository.PostgresAgentStore. Every call but the two Find calls runs in
// the garage's transaction.
type AgentStore interface {
	ListAgents(ctx context.Context, garageID string) ([]PrintAgent, error)
	// GetAgent returns ErrAgentNotFound for an unknown id.
	GetAgent(ctx context.Context, garageID, id string) (PrintAgent, error)
	LockAgents(ctx context.Context, garageID string) error
	CountAgents(ctx context.Context, garageID string) (int, error)
	// CreateAgent returns ErrAgentNameTaken for a duplicate name.
	CreateAgent(ctx context.Context, garageID, name string, codeHash []byte, expires time.Time) (PrintAgent, error)
	// SetPairingCode sets a new code and removes the key and the
	// connection; ErrAgentNotFound for an unknown id.
	SetPairingCode(ctx context.Context, garageID, id string, codeHash []byte, expires time.Time) (PrintAgent, error)
	DeleteAgent(ctx context.Context, garageID, id string) error

	// FindAgentByPairingCode and FindAgentByKey run before the garage is
	// known; ErrAgentNotFound when nothing matches (a code also when it
	// expired).
	FindAgentByPairingCode(ctx context.Context, codeHash []byte) (agentID, garageID string, err error)
	FindAgentByKey(ctx context.Context, keyHash []byte) (agentID, garageID string, err error)
	// CompletePairing swaps a valid code for the key; ErrAgentNotFound when
	// the code no longer works.
	CompletePairing(ctx context.Context, garageID, agentID string, codeHash, keyHash []byte, report AgentReport) (PrintAgent, error)
	// SetConnection records the connection serving the agent and its
	// heartbeat; ClearConnection forgets it if it is still that one.
	// SetConnection and Heartbeat return ErrAgentRevoked when the key no
	// longer matches; Heartbeat ErrAgentReplaced when another connection
	// took over.
	SetConnection(ctx context.Context, s AgentSession) error
	Heartbeat(ctx context.Context, s AgentSession) error
	ClearConnection(ctx context.Context, s AgentSession) error
	SaveReport(ctx context.Context, s AgentSession, report AgentReport) error
}

// Agents lists the garage's print agents by name.
func (uc *DocumentUseCase) Agents(ctx context.Context, garageID string) ([]PrintAgent, error) {
	agents, err := uc.AgentStore.ListAgents(ctx, garageID)
	if err != nil {
		return nil, apperror.Internal("list print agents", err)
	}

	return agents, nil
}

// CreateAgent adds a print agent and returns it with its pairing code.
func (uc *DocumentUseCase) CreateAgent(ctx context.Context, garageID, name string) (PrintAgent, string, error) {
	name, err := agentName(name)
	if err != nil {
		return PrintAgent{}, "", err
	}

	if err := uc.AgentStore.LockAgents(ctx, garageID); err != nil {
		return PrintAgent{}, "", apperror.Internal("lock print agents", err)
	}

	count, err := uc.AgentStore.CountAgents(ctx, garageID)
	if err != nil {
		return PrintAgent{}, "", apperror.Internal("count print agents", err)
	}

	if count >= MaxAgents {
		return PrintAgent{}, "", apperror.Validation("agent", apperror.IssueInvalid, "a garage can pair at most 10 print agents")
	}

	code, hash, err := newPairingCode()
	if err != nil {
		return PrintAgent{}, "", apperror.Internal("pairing code", err)
	}

	agent, err := uc.AgentStore.CreateAgent(ctx, garageID, name, hash, uc.now().Add(PairingCodeTTL))
	if err != nil {
		return PrintAgent{}, "", agentError("create print agent", err)
	}

	return agent, code, nil
}

// IssuePairingCode gives an agent a new pairing code; its key stops working
// and its connection closes.
func (uc *DocumentUseCase) IssuePairingCode(ctx context.Context, garageID, id string) (PrintAgent, string, error) {
	code, hash, err := newPairingCode()
	if err != nil {
		return PrintAgent{}, "", apperror.Internal("pairing code", err)
	}

	agent, err := uc.AgentStore.SetPairingCode(ctx, garageID, id, hash, uc.now().Add(PairingCodeTTL))
	if err != nil {
		return PrintAgent{}, "", agentError("issue pairing code", err)
	}

	platformdb.AfterCommit(ctx, func() { uc.AgentNotifier.AgentRevoked(agent.ID) })

	return agent, code, nil
}

// DeleteAgent removes an agent with its printers and jobs; its connection
// closes.
func (uc *DocumentUseCase) DeleteAgent(ctx context.Context, garageID, id string) error {
	if err := uc.AgentStore.DeleteAgent(ctx, garageID, id); err != nil {
		return agentError("delete print agent", err)
	}

	platformdb.AfterCommit(ctx, func() { uc.AgentNotifier.AgentRevoked(id) })

	return nil
}

// PairResult is what a paired agent gets: its key, shown once.
type PairResult struct {
	Key        string
	Agent      PrintAgent
	GarageName string
}

// PairAgent exchanges a pairing code for the agent's key (the public call of
// the agent's setup).
func (uc *DocumentUseCase) PairAgent(ctx context.Context, code string, report AgentReport) (PairResult, error) {
	notFound := apperror.New(apperror.CodeNotFound, "the pairing code is wrong or expired")

	code = normalizePairingCode(code)
	if len(code) != pairingCodeLength {
		return PairResult{}, notFound
	}

	report, err := normalizeReport(report)
	if err != nil {
		return PairResult{}, err
	}

	codeHash := hashSecret(code)

	agentID, garageID, err := uc.AgentStore.FindAgentByPairingCode(ctx, codeHash)
	if errors.Is(err, ErrAgentNotFound) {
		return PairResult{}, notFound
	}

	if err != nil {
		return PairResult{}, apperror.Internal("find print agent", err)
	}

	key, err := newAgentKey()
	if err != nil {
		return PairResult{}, apperror.Internal("agent key", err)
	}

	var result PairResult

	err = uc.Tx.WithTransaction(ctx, platformdb.GarageScope(garageID), func(ctx context.Context) error {
		agent, err := uc.AgentStore.CompletePairing(ctx, garageID, agentID, codeHash, hashSecret(key), report)
		if err != nil {
			return err
		}

		garage, err := uc.Garages.Get(ctx, garageID)
		if err != nil {
			return err
		}

		result = PairResult{Key: key, Agent: agent, GarageName: garage.Name}

		return nil
	})

	switch {
	case errors.Is(err, ErrAgentNotFound):
		return PairResult{}, notFound
	case err != nil:
		return PairResult{}, apperror.Internal("pair print agent", err)
	}

	return result, nil
}

// AuthenticateAgent finds the agent of a key, for its connection.
func (uc *DocumentUseCase) AuthenticateAgent(ctx context.Context, key, connectionID string) (AgentSession, error) {
	if !strings.HasPrefix(key, agentKeyPrefix) || len(key) > 100 {
		return AgentSession{}, ErrAgentRevoked
	}

	hash := hashSecret(key)

	agentID, garageID, err := uc.AgentStore.FindAgentByKey(ctx, hash)
	if errors.Is(err, ErrAgentNotFound) {
		return AgentSession{}, ErrAgentRevoked
	}

	if err != nil {
		return AgentSession{}, apperror.Internal("find print agent", err)
	}

	s := AgentSession{AgentID: agentID, GarageID: garageID, KeyHash: hash, ConnectionID: connectionID, ConnectedAt: uc.now()}

	if err := uc.inAgentGarage(ctx, s, func(ctx context.Context) error { return uc.AgentStore.SetConnection(ctx, s) }); err != nil {
		return AgentSession{}, err
	}

	return s, nil
}

// AgentHeartbeat keeps the connection's agent connected; ErrAgentRevoked
// when its key no longer works, ErrAgentReplaced when a newer connection took
// over.
func (uc *DocumentUseCase) AgentHeartbeat(ctx context.Context, s AgentSession) error {
	return uc.inAgentGarage(ctx, s, func(ctx context.Context) error { return uc.AgentStore.Heartbeat(ctx, s) })
}

// AgentDisconnected forgets the connection.
func (uc *DocumentUseCase) AgentDisconnected(ctx context.Context, s AgentSession) error {
	return uc.inAgentGarage(ctx, s, func(ctx context.Context) error { return uc.AgentStore.ClearConnection(ctx, s) })
}

// AgentReported saves what the agent says about its PC and printers.
func (uc *DocumentUseCase) AgentReported(ctx context.Context, s AgentSession, report AgentReport) error {
	report, err := normalizeReport(report)
	if err != nil {
		return err
	}

	return uc.inAgentGarage(ctx, s, func(ctx context.Context) error { return uc.AgentStore.SaveReport(ctx, s, report) })
}

func (uc *DocumentUseCase) inAgentGarage(ctx context.Context, s AgentSession, fn func(ctx context.Context) error) error {
	err := uc.Tx.WithTransaction(ctx, platformdb.GarageScope(s.GarageID), fn)
	if err == nil || errors.Is(err, ErrAgentRevoked) || errors.Is(err, ErrAgentReplaced) {
		return err
	}

	if _, ok := apperror.As(err); ok {
		return err
	}

	return apperror.Internal("print agent", err)
}

func agentError(op string, err error) error {
	switch {
	case errors.Is(err, ErrAgentNotFound):
		return apperror.New(apperror.CodeNotFound, "print agent not found")
	case errors.Is(err, ErrAgentNameTaken):
		return apperror.Validation("name", apperror.IssueTaken, "a print agent of the garage has that name")
	default:
		return apperror.Internal(op, err)
	}
}

func agentName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", apperror.Validation("name", apperror.IssueRequired, "name is required")
	}

	if utf8.RuneCountInString(name) > maxPrinterName {
		return "", apperror.Validation("name", apperror.IssueTooLong, "name is at most 60 characters")
	}

	return name, nil
}

// normalizeReport trims what an agent reports and refuses what does not
// fit: at most 100 printers, each named once.
func normalizeReport(r AgentReport) (AgentReport, error) {
	r.Hostname = clip(r.Hostname, maxAgentHostname)
	r.OS = clip(r.OS, maxAgentLabel)
	r.Version = clip(r.Version, maxAgentLabel)

	if len(r.Printers) > maxAgentPrinters {
		return AgentReport{}, apperror.Validation("printers", apperror.IssueInvalid, "at most 100 printers")
	}

	printers := make([]AgentPrinter, 0, len(r.Printers))
	seen := map[string]bool{}

	for _, p := range r.Printers {
		name, err := queueName(p.Name)
		if err != nil {
			return AgentReport{}, err
		}

		if seen[strings.ToLower(name)] {
			continue
		}

		seen[strings.ToLower(name)] = true
		printers = append(printers, AgentPrinter{Name: name, Default: p.Default})
	}

	r.Printers = printers

	return r, nil
}

// queueName checks the name of a PC's printer: 1 to 200 characters, no
// control characters.
func queueName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > maxAgentQueue || strings.ContainsFunc(name, unicode.IsControl) {
		return "", apperror.Validation("queue", apperror.IssueInvalid, "a printer name is 1 to 200 characters without control characters")
	}

	return name, nil
}

// clip trims s, drops its control characters and cuts it to n runes.
func clip(s string, n int) string {
	s = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}

		return r
	}, s))

	if utf8.RuneCountInString(s) > n {
		s = string([]rune(s)[:n])
	}

	return s
}

// newPairingCode returns a code to show (XXXX-XXXX) and the hash to store.
func newPairingCode() (string, []byte, error) {
	letters := big.NewInt(int64(len(pairingCodeLetters)))

	var b strings.Builder

	for range pairingCodeLength {
		n, err := rand.Int(rand.Reader, letters)
		if err != nil {
			return "", nil, err //nolint:wrapcheck // the caller wraps it
		}

		b.WriteByte(pairingCodeLetters[n.Int64()])
	}

	code := b.String()

	return code[:4] + "-" + code[4:], hashSecret(code), nil
}

// normalizePairingCode removes dashes and spaces and upper-cases the code.
func normalizePairingCode(code string) string {
	return strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(strings.TrimSpace(code)))
}

func newAgentKey() (string, error) {
	b := make([]byte, agentKeyBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err //nolint:wrapcheck // the caller wraps it
	}

	return agentKeyPrefix + base64.RawURLEncoding.EncodeToString(b), nil
}

func hashSecret(s string) []byte {
	sum := sha256.Sum256([]byte(s))

	return sum[:]
}
