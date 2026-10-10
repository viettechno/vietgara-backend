package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"

	"github.com/viettechno/vietgara-backend/internal/platform/tracing"
)

// Errors returned by NewPgxPool; the underlying cause is wrapped via %w.
var (
	ErrParsePgxConfig = errors.New("failed to parse pgx config")
	ErrCreatePgxPool  = errors.New("failed to create pgx pool")
)

// ConnPool is the minimal contract the package needs from the underlying
// pgx pool: it must run queries, begin transactions, and close. *pgxpool.Pool
// satisfies it; declaring it as an interface lets tests inject fakes.
type ConnPool interface {
	Querier
	Begin(ctx context.Context) (pgx.Tx, error)
	Close()
}

// Pool wraps a pgx connection pool.
type Pool struct {
	pool ConnPool
}

// NewPgxPool parses dsn and creates a pgx connection pool. The caller owns
// the returned pool and must Close it when done.
func NewPgxPool(ctx context.Context, dsn string) (ConnPool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrParsePgxConfig, err)
	}

	// One span per query (NFR-OB-01), so a slow or failing query is
	// traceable within the request that caused it.
	cfg.ConnConfig.Tracer = tracing.NewQueryTracer(otel.GetTracerProvider())

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrCreatePgxPool, err)
	}

	return pool, nil
}

// NewPool wraps an existing ConnPool (typically created by NewPgxPool) into
// a Pool, the concrete type repositories depend on.
func NewPool(_ context.Context, pool ConnPool) *Pool {
	return &Pool{pool: pool}
}

// Close releases the underlying connection pool.
func (p *Pool) Close() {
	p.pool.Close()
}

// Querier is the minimal interface satisfied by both *pgxpool.Pool and
// pgx.Tx, allowing repositories to accept either a pool (outside a
// transaction) or a tx (inside one) without duplicating code.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// QuerierFromContext returns the active transaction if one was started via
// WithTransaction, or falls back to the plain pool otherwise (for simple
// single-statement reads that don't need RLS tenant scoping, e.g. looking
// up a user by email during login before any garage is known).
func QuerierFromContext(ctx context.Context, pool *Pool) Querier {
	if tx, ok := ctx.Value(TxKey{}).(pgx.Tx); ok {
		return tx
	}

	return pool.pool
}
