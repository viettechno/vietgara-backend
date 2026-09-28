package db

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// TxKey is the context key used to carry the active transaction, so
// repositories can pick it up transparently via QuerierFromContext.
type TxKey struct{}

// txFromContext returns the transaction carried in ctx.
func txFromContext(ctx context.Context) (pgx.Tx, bool) {
	tx, ok := ctx.Value(TxKey{}).(pgx.Tx)

	return tx, ok
}

// InTransaction reports whether ctx carries a transaction.
func InTransaction(ctx context.Context) bool {
	_, ok := txFromContext(ctx)

	return ok
}
