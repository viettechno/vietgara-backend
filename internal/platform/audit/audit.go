// Package audit writes the append-only audit log (Security Architecture
// Section 7): who changed licensing, roles, permissions and staff, when,
// from where, and the before/after state. The application role may only
// INSERT into audit_log.
package audit

import (
	"context"
	"encoding/json"

	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
)

// Entry is one audited change.
type Entry struct {
	// Actor is the account that made the change ("" for the system).
	Actor      string
	GarageID   string
	Action     string // e.g. "role.updated"
	EntityType string // e.g. "role"
	EntityID   string
	Before     any
	After      any
	IP         string
}

// Logger records entries in the caller's transaction, so an entry exists
// exactly when the change it describes was committed.
type Logger struct {
	pool *platformdb.Pool
}

// NewLogger returns a Logger on pool.
func NewLogger(pool *platformdb.Pool) *Logger {
	return &Logger{pool: pool}
}

// Record inserts e.
func (l *Logger) Record(ctx context.Context, e Entry) error {
	before, err := toJSON(e.Before)
	if err != nil {
		return err
	}

	after, err := toJSON(e.After)
	if err != nil {
		return err
	}

	_, err = platformdb.QuerierFromContext(ctx, l.pool).Exec(ctx, `
		INSERT INTO audit_log (actor_account_id, garage_id, action, entity_type, entity_id, before, after, ip)
		VALUES (NULLIF($1, '')::uuid, NULLIF($2, '')::uuid, $3, $4, $5, $6, $7, NULLIF($8, ''))`,
		e.Actor, e.GarageID, e.Action, e.EntityType, e.EntityID, before, after, e.IP)

	return err
}

func toJSON(v any) ([]byte, error) {
	if v == nil {
		return nil, nil
	}

	return json.Marshal(v)
}
