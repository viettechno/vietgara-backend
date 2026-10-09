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

// Errors returned by TxManager.WithTransaction when a Postgres operation
// fails; the underlying cause is wrapped via %w.
var (
	ErrBeginTransaction  = errors.New("failed to begin transaction")
	ErrSetTenantContext  = errors.New("failed to set tenant context")
	ErrCommitTransaction = errors.New("failed to commit transaction")
)

// SetScopeSQL (re)sets the session variables read by the Row-Level
// Security policies (app_current_garage_id() / app_current_user_id()):
// the tenant garage and the calling user. Empty strings mean "none".
const SetScopeSQL = "SELECT set_config('app.current_garage_id', $1, true), set_config('app.current_user_id', $2, true)"

// Scope is the Row-Level Security context of a transaction: the garage
// (tenant) whose rows are written, and the user whose own memberships,
// invitations and roles are readable across garages.
type Scope struct {
	GarageID string
	UserID   string
}

// IsZero reports whether the scope sets nothing.
func (s Scope) IsZero() bool { return s == Scope{} }

// Garage returns a copy of s scoped to garageID.
func (s Scope) Garage(garageID string) Scope {
	s.GarageID = garageID

	return s
}

// GarageScope scopes a transaction to one garage.
func GarageScope(garageID string) Scope { return Scope{GarageID: garageID} }

// UserScope scopes a transaction to one calling user.
func UserScope(userID string) Scope { return Scope{UserID: userID} }

type scopeKey struct{}

// ScopeFromContext returns the scope of the transaction carried in ctx.
func ScopeFromContext(ctx context.Context) Scope {
	s, _ := ctx.Value(scopeKey{}).(Scope)

	return s
}

// TxManager runs fn inside a single Postgres transaction with the RLS
// session variables of a Scope set BEFORE fn runs -- this is the single
// place in the codebase where they are set, so every repository call inside
// fn is automatically tenant-scoped.
//
// See: Database Design Section 1.2, LLD Section 6 (TenantContextInterceptor).
type TxManager struct {
	pool *Pool
}

// NewTxManager returns a TxManager bound to the given pool.
func NewTxManager(pool *Pool) *TxManager {
	return &TxManager{pool: pool}
}

// WithTransaction runs fn inside a Postgres transaction scoped to scope,
// committing when fn returns nil and rolling back otherwise.
//
// Called while a transaction is already carried in ctx, fn joins it: the
// scope is switched for fn's duration (and restored afterwards) and the
// outer caller commits.
func (m *TxManager) WithTransaction(ctx context.Context, scope Scope, fn func(ctx context.Context) error) error {
	if outer, ok := txFromContext(ctx); ok {
		return m.join(ctx, outer, scope, fn)
	}

	tx, err := m.pool.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrBeginTransaction, err)
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op if already committed

	if !scope.IsZero() {
		if _, err := tx.Exec(ctx, SetScopeSQL, scope.GarageID, scope.UserID); err != nil {
			return fmt.Errorf("%w: %w", ErrSetTenantContext, err)
		}
	}

	var hooks []func()

	txCtx := context.WithValue(ctx, TxKey{}, tx)
	txCtx = context.WithValue(txCtx, afterCommitKey{}, &hooks)
	txCtx = context.WithValue(txCtx, scopeKey{}, scope)

	if err := fn(txCtx); err != nil {
		return err // rollback happens via defer
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("%w: %w", ErrCommitTransaction, err)
	}

	for _, hook := range hooks {
		hook()
	}

	return nil
}

func (m *TxManager) join(ctx context.Context, tx Querier, scope Scope, fn func(ctx context.Context) error) error {
	previous := ScopeFromContext(ctx)
	if scope.IsZero() || scope == previous {
		return fn(ctx)
	}

	if _, err := tx.Exec(ctx, SetScopeSQL, scope.GarageID, scope.UserID); err != nil {
		return fmt.Errorf("%w: %w", ErrSetTenantContext, err)
	}

	err := fn(context.WithValue(ctx, scopeKey{}, scope))

	if _, restoreErr := tx.Exec(ctx, SetScopeSQL, previous.GarageID, previous.UserID); restoreErr != nil && err == nil {
		err = fmt.Errorf("%w: %w", ErrSetTenantContext, restoreErr)
	}

	return err
}

type afterCommitKey struct{}

// AfterCommit runs fn once the transaction carried in ctx has committed, and
// never if it rolls back -- e.g. to invalidate a cache or send an e-mail
// only for changes that were really persisted. Without a transaction in ctx
// fn runs immediately.
func AfterCommit(ctx context.Context, fn func()) {
	if hooks, ok := ctx.Value(afterCommitKey{}).(*[]func()); ok {
		*hooks = append(*hooks, fn)

		return
	}

	fn()
}
