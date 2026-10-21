//go:build integration

package db_test

import (
	"context"
	"sort"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	dbpkg "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platform/db/pgtest"
)

// TestEveryTableIsClassifiedForRowLevelSecurity is the automated part of the
// RLS review (NFR-SE-03, Security Architecture 4): in the migrated schema
// every table either has row-level security enabled, forced (so the table
// owner is bound too) and at least one policy, or is listed in noRLS with
// the reason it holds no data of a single garage. A new table therefore
// fails this test until its author classifies it.
func TestEveryTableIsClassifiedForRowLevelSecurity(t *testing.T) {
	pg := pgtest.StartMigrated(t)

	rows, err := pg.Admin.QueryContext(t.Context(), `
		SELECT c.relname, c.relrowsecurity, c.relforcerowsecurity,
		       EXISTS (SELECT 1 FROM pg_policy p WHERE p.polrelid = c.oid)
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public' AND c.relkind IN ('r', 'p') AND NOT c.relispartition
		ORDER BY c.relname`)
	require.NoError(t, err)

	defer func() { _ = rows.Close() }()

	var (
		tables     int
		violations []string
		unseen     = map[string]bool{}
	)

	for name := range noRLS {
		unseen[name] = true
	}

	for rows.Next() {
		var (
			name                       string
			enabled, forced, hasPolicy bool
		)

		require.NoError(t, rows.Scan(&name, &enabled, &forced, &hasPolicy))

		tables++

		delete(unseen, name)

		_, exempt := noRLS[name]
		protected := enabled && forced && hasPolicy

		switch {
		case exempt && enabled:
			violations = append(violations, name+": listed in noRLS but has row-level security; remove it from the list")
		case !exempt && !protected:
			violations = append(violations, name+": no forced row-level security with a policy, and not listed in noRLS")
		}
	}

	require.NoError(t, rows.Err())
	require.Positive(t, tables, "the migrated schema has tables")

	for name := range unseen {
		violations = append(violations, name+": listed in noRLS but not in the schema")
	}

	sort.Strings(violations)
	require.Empty(t, violations)
}

// noRLS lists the tables without row-level security, each with the reason.
// Everything a garage owns has a garage_id and a policy. What is left is
// either platform data or keyed by user, and read only by code that filters
// by the caller (see the security review report, finding SR-05).
var noRLS = map[string]string{ //nolint:gosec // G101 matches the table name credit_ledger, not a credential
	"users":                     "identity: looked up by e-mail at sign-in, before any session exists",
	"user_mfa":                  "identity: read at the second sign-in step, before any session exists; the TOTP seed is encrypted",
	"user_mfa_recovery_codes":   "identity: hashed codes, read at the second sign-in step",
	"platform_admins":           "platform configuration",
	"plans":                     "platform catalogue, the same for every garage",
	"platform_payment_settings": "platform configuration",
	"subscriptions":             "keyed by the owner's user_id; read by the license module with the caller's id, and by platform admins and the expiry job across owners",
	"entitlements":              "keyed by the owner's user_id, like subscriptions",
	"credit_ledger":             "keyed by the owner's user_id, like subscriptions",
	"plan_payment_requests":     "keyed by the owner's user_id, like subscriptions",
	"outbox_events":             "system: written in the business transaction, read by the relay across garages; no API reads it",
	"processed_events":          "system: consumer de-duplication keys, no business data",
	"notification_log":          "system: written by the notifier, read by no API yet (support diagnostics follow in Release 8.2, behind a scope check)",
	"schema_migrations":         "migration bookkeeping; the application role has no grant",
}

// TestAuditLogIsReadableOnlyWithinItsGarage covers migration 000032: any
// scope may append, a SELECT sees only the scoped garage's entries, and the
// log cannot be rewritten.
func TestAuditLogIsReadableOnlyWithinItsGarage(t *testing.T) {
	pg := pgtest.StartMigrated(t)
	pool := pg.AppPool(t)
	manager := dbpkg.NewTxManager(pool)

	garageA, garageB := uuid.NewString(), uuid.NewString()

	count := func(scope dbpkg.Scope) (n int) {
		t.Helper()

		require.NoError(t, manager.WithTransaction(t.Context(), scope, func(ctx context.Context) error {
			return dbpkg.QuerierFromContext(ctx, pool).QueryRow(ctx, `SELECT count(*) FROM audit_log`).Scan(&n)
		}))

		return n
	}

	appendEntry := func(scope dbpkg.Scope, garage string) {
		t.Helper()

		require.NoError(t, manager.WithTransaction(t.Context(), scope, func(ctx context.Context) error {
			_, err := dbpkg.QuerierFromContext(ctx, pool).Exec(ctx, `
				INSERT INTO audit_log (garage_id, action, entity_type, entity_id)
				VALUES (NULLIF($1, '')::uuid, 'test.recorded', 'test', 'x')`, garage)

			return err
		}))
	}

	appendEntry(dbpkg.GarageScope(garageA), garageA)
	appendEntry(dbpkg.GarageScope(garageA), garageA)
	appendEntry(dbpkg.GarageScope(garageB), garageB)
	appendEntry(dbpkg.UserScope(uuid.NewString()), "") // a platform event

	require.Equal(t, 2, count(dbpkg.GarageScope(garageA)))
	require.Equal(t, 1, count(dbpkg.GarageScope(garageB)))
	require.Zero(t, count(dbpkg.UserScope(uuid.NewString())), "no garage in scope, nothing readable")
	require.Zero(t, count(dbpkg.GarageScope(uuid.NewString())), "an unrelated garage sees nothing")

	err := manager.WithTransaction(t.Context(), dbpkg.GarageScope(garageA), func(ctx context.Context) error {
		_, err := dbpkg.QuerierFromContext(ctx, pool).Exec(ctx, `UPDATE audit_log SET action = 'tampered'`)

		return err
	})
	require.Error(t, err, "the application role cannot rewrite the log")

	err = manager.WithTransaction(t.Context(), dbpkg.GarageScope(garageA), func(ctx context.Context) error {
		_, err := dbpkg.QuerierFromContext(ctx, pool).Exec(ctx, `DELETE FROM audit_log`)

		return err
	})
	require.Error(t, err, "nor delete from it")
}
