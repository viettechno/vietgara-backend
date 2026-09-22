package db

// TxKey is the context key used to carry the active transaction, so
// repositories can pick it up transparently via QuerierFromContext.
type TxKey struct{}
