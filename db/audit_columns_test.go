//go:build integration

package db_test

import (
	"database/sql"
	"sort"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/platform/db/pgtest"
)

// auditAllowlist names the tables that carry no row audit columns, with
// the reason. A new table must either have created_at / created_by (plus
// updated_at / updated_by when its rows change) with the matching trigger
// from create_row_audit_functions, or be added here deliberately (NFR-MT-07).
var auditAllowlist = map[string]string{
	"processed_events": "idempotent-consumer ledger of the event bus, keyed by consumer and event",
	"idempotency_keys": "request replay cache keyed by user and key, purged after 24 hours",
}

// TestRowAuditColumns fails when a table has neither the audit columns
// and their trigger nor an allowlist entry.
func TestRowAuditColumns(t *testing.T) {
	pg := pgtest.StartMigrated(t)

	rows, err := pg.Admin.QueryContext(t.Context(), `
		SELECT c.relname,
			bool_or(a.attname = 'created_at'), bool_or(a.attname = 'created_by'),
			bool_or(a.attname = 'updated_at'), bool_or(a.attname = 'updated_by'),
			COALESCE(bool_or(p.proname = 'set_row_audit'), false),
			COALESCE(bool_or(p.proname = 'set_created_audit'), false)
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum > 0 AND NOT a.attisdropped
		LEFT JOIN pg_trigger tg ON tg.tgrelid = c.oid AND NOT tg.tgisinternal
		LEFT JOIN pg_proc p ON p.oid = tg.tgfoid
		WHERE n.nspname = 'public' AND c.relkind = 'r' AND c.relname <> 'schema_migrations'
		GROUP BY c.relname
		ORDER BY c.relname`)
	require.NoError(t, err)

	defer func() { require.NoError(t, rows.Close()) }()

	var (
		problems []string
		seen     = map[string]bool{}
	)

	for rows.Next() {
		var (
			table                                      string
			createdAt, createdBy, updatedAt, updatedBy bool
			rowAudit, createdAudit                     bool
		)

		require.NoError(t, rows.Scan(&table, &createdAt, &createdBy, &updatedAt, &updatedBy, &rowAudit, &createdAudit))

		seen[table] = true

		if reason, ok := auditAllowlist[table]; ok {
			assert.NotEmpty(t, reason)

			continue
		}

		switch {
		case !createdAt || !createdBy:
			problems = append(problems, table+": missing created_at / created_by")
		case updatedAt != updatedBy:
			problems = append(problems, table+": updated_at and updated_by must come together")
		case updatedAt && !rowAudit:
			problems = append(problems, table+": has updated_at / updated_by but no set_row_audit trigger")
		case !updatedAt && !createdAudit:
			problems = append(problems, table+": has no set_created_audit trigger")
		}
	}

	require.NoError(t, rows.Err())
	require.Greater(t, len(seen), 20, "the catalog query found the tables")

	for table := range auditAllowlist {
		assert.True(t, seen[table], "allowlisted table %q no longer exists: remove it from auditAllowlist", table)
	}

	sort.Strings(problems)
	assert.Empty(t, problems, "tables without row audit columns (NFR-MT-07)")
}

// TestRowAuditTriggers checks what the triggers fill: the session user on
// insert and update, immutable created_*, NULL for a system job, and the
// created pair of an append-only table.
func TestRowAuditTriggers(t *testing.T) {
	pg := pgtest.StartMigrated(t)
	alice, bob := uuid.NewString(), uuid.NewString()
	owner, garageID, customerID := uuid.NewString(), uuid.NewString(), uuid.NewString()

	_, err := pg.Admin.ExecContext(t.Context(), `INSERT INTO users (id, email, full_name) VALUES ($1, 'owner@example.com', 'Owner')`, owner)
	require.NoError(t, err)

	_, err = pg.Admin.ExecContext(t.Context(), `INSERT INTO garages (id, user_id, name) VALUES ($1, $2, 'G')`, garageID, owner)
	require.NoError(t, err)

	type audit struct {
		createdBy, updatedBy sql.NullString
	}

	// run executes statements in one transaction acting as user ("" is a
	// system job), then returns the customer row's audit columns.
	run := func(user string, statements ...string) audit {
		t.Helper()

		tx, err := pg.Admin.BeginTx(t.Context(), nil)
		require.NoError(t, err)

		_, err = tx.ExecContext(t.Context(), `SELECT set_config('app.current_user_id', $1, true)`, user)
		require.NoError(t, err)

		for _, statement := range statements {
			_, err = tx.ExecContext(t.Context(), statement, customerID, garageID)
			require.NoError(t, err)
		}

		var got audit

		require.NoError(t, tx.QueryRowContext(t.Context(),
			`SELECT created_by::text, updated_by::text FROM customers WHERE id = $1`, customerID).Scan(&got.createdBy, &got.updatedBy))
		require.NoError(t, tx.Commit())

		return got
	}

	got := run(alice, `INSERT INTO customers (id, garage_id, full_name) VALUES ($1, $2, 'An')`)
	assert.Equal(t, alice, got.createdBy.String)
	assert.Equal(t, alice, got.updatedBy.String, "a new row was last changed by its creator")

	var createdAt string

	require.NoError(t, pg.Admin.QueryRowContext(t.Context(), `SELECT created_at::text FROM customers WHERE id = $1`, customerID).Scan(&createdAt))

	got = run(bob, `UPDATE customers SET full_name = 'Binh' WHERE id = $1 AND garage_id = $2`)
	assert.Equal(t, alice, got.createdBy.String, "created_by never changes")
	assert.Equal(t, bob, got.updatedBy.String)

	got = run("", `UPDATE customers SET full_name = 'Chi' WHERE id = $1 AND garage_id = $2`)
	assert.Equal(t, alice, got.createdBy.String)
	assert.False(t, got.updatedBy.Valid, "a system job writes NULL")

	// A value the code writes itself is overridden by the session user and
	// otherwise only kept when the session has no user.
	got = run(bob, `UPDATE customers SET updated_by = '`+alice+`' WHERE id = $1 AND garage_id = $2`)
	assert.Equal(t, bob, got.updatedBy.String)

	got = run("", `UPDATE customers SET updated_by = '`+alice+`' WHERE id = $1 AND garage_id = $2`)
	assert.Equal(t, alice, got.updatedBy.String)

	// created_at cannot be rewritten.
	_, err = pg.Admin.ExecContext(t.Context(), `UPDATE customers SET created_at = '2000-01-01' WHERE id = $1`, customerID)
	require.NoError(t, err)

	var after string

	require.NoError(t, pg.Admin.QueryRowContext(t.Context(), `SELECT created_at::text FROM customers WHERE id = $1`, customerID).Scan(&after))
	assert.Equal(t, createdAt, after)

	// An append-only table keeps the created pair.
	tx, err := pg.Admin.BeginTx(t.Context(), nil)
	require.NoError(t, err)

	_, err = tx.ExecContext(t.Context(), `SELECT set_config('app.current_user_id', $1, true)`, alice)
	require.NoError(t, err)

	var by string

	require.NoError(t, tx.QueryRowContext(t.Context(), `
		INSERT INTO audit_log (action, entity_type, entity_id) VALUES ('x', 'y', 'z') RETURNING created_by::text`).Scan(&by))
	require.NoError(t, tx.Commit())
	assert.Equal(t, alice, by)
}
