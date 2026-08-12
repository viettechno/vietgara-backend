// Package db provides a shared pgx connection pool and a transaction
// manager used by every service's repository layer.
//
// See: LLD Section 4.1 (TxManager pattern), Coding_Standards.docx Section 2.1.
package db

import (
	"context"
	"errors"
	"fmt"
)

var (
	ErrBeginTransaction  = errors.New("failed to begin transaction")
	ErrSetTenantContext  = errors.New("failed to set tenant context")
	ErrCommitTransaction = errors.New("failed to commit transaction")
)

const SetTenantSQL = "SELECT set_config('app.current_garage_id', $1, true)"

// TxManager runs fn inside a single Postgres transaction. If a
// garageID is provided (non-nil), it sets the app.current_garage_id
// session variable for Row-Level Security BEFORE running fn -- this is
// the single place in the codebase where that variable is set, so every
// repository call inside fn is automatically tenant-scoped.
//
// See: Database Design Section 1.2, LLD Section 6 (TenantContextInterceptor).
type TxManager struct {
	pool *Pool
}

func NewTxManager(pool *Pool) *TxManager {
	return &TxManager{pool: pool}
}

func (m *TxManager) WithTransaction(ctx context.Context, garageID *string, fn func(ctx context.Context) error) error {
	tx, err := m.pool.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrBeginTransaction, err)
	}
	defer tx.Rollback(ctx) // no-op if already committed

	if garageID != nil {
		if _, err := tx.Exec(ctx, SetTenantSQL, *garageID); err != nil {
			return fmt.Errorf("%w: %w", ErrSetTenantContext, err)
		}
	}

	txCtx := context.WithValue(ctx, TxKey{}, tx)
	if err := fn(txCtx); err != nil {
		return err // rollback happens via defer
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("%w: %w", ErrCommitTransaction, err)
	}

	return nil
}
