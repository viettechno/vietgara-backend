// Package db provides database migration and row-level-security helpers.
package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres" // Register the PostgreSQL migration driver.
	_ "github.com/golang-migrate/migrate/v4/source/file"
	_ "github.com/lib/pq" // database/sql driver for EnsureAppLogin.
)

var errInvalidDatabaseUserName = errors.New("invalid database user name")

// RunMigrations runs all pending migrations
// connectionString should be in format: postgres://user:password@host:port/dbname?sslmode=disable
// migrationsPath should be the path to migrations directory: file://path/to/migrations
func RunMigrations(connectionString, migrationsPath string) error {
	m, err := migrate.New(migrationsPath, connectionString)
	if err != nil {
		return fmt.Errorf("failed to create migration instance: %w", err)
	}
	defer func() { _, _ = m.Close() }()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	version, dirty, err := m.Version()
	if err != nil {
		return fmt.Errorf("failed to get migration version: %w", err)
	}

	fmt.Printf("Migrations completed. Current version: %d (dirty: %v)\n", version, dirty)

	return nil
}

// RollbackLastMigration rolls back the last migration.
func RollbackLastMigration(connectionString, migrationsPath string) error {
	m, err := migrate.New(migrationsPath, connectionString)
	if err != nil {
		return fmt.Errorf("failed to create migration instance: %w", err)
	}
	defer func() { _, _ = m.Close() }()

	if err := m.Steps(-1); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("failed to rollback migration: %w", err)
	}

	return nil
}

// RollbackAllMigrations rolls back all migrations.
func RollbackAllMigrations(connectionString, migrationsPath string) error {
	m, err := migrate.New(migrationsPath, connectionString)
	if err != nil {
		return fmt.Errorf("failed to create migration instance: %w", err)
	}
	defer func() { _, _ = m.Close() }()

	if err := m.Down(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("failed to rollback all migrations: %w", err)
	}

	return nil
}

// GetMigrationVersion returns the current migration version.
func GetMigrationVersion(connectionString, migrationsPath string) (uint, bool, error) {
	m, err := migrate.New(migrationsPath, connectionString)
	if err != nil {
		return 0, false, fmt.Errorf("failed to create migration instance: %w", err)
	}
	defer func() { _, _ = m.Close() }()

	version, dirty, err := m.Version()
	if err != nil {
		return 0, false, fmt.Errorf("failed to get migration version: %w", err)
	}

	return version, dirty, nil
}

// ForceMigrationVersion forces the migration version without running migrations.
func ForceMigrationVersion(connectionString, migrationsPath string, version int) error {
	m, err := migrate.New(migrationsPath, connectionString)
	if err != nil {
		return fmt.Errorf("failed to create migration instance: %w", err)
	}
	defer func() { _, _ = m.Close() }()

	if err := m.Force(version); err != nil {
		return fmt.Errorf("failed to force migration version: %w", err)
	}

	return nil
}

// EnsureAppLogin creates (or updates the password of) the LOGIN role the
// backend connects as and makes it a member of vietgara_app, the
// NOBYPASSRLS role the migrations grant privileges to. For local and CI
// environments; staging and production manage database users with
// Terraform. connectionString must belong to a role allowed to create
// roles (the migration owner).
func EnsureAppLogin(connectionString, user, password string) error {
	if !validRoleName(user) {
		return fmt.Errorf("%w %q", errInvalidDatabaseUserName, user)
	}

	conn, err := sql.Open("postgres", connectionString)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() { _ = conn.Close() }()

	quotedPassword := "'" + strings.ReplaceAll(password, "'", "''") + "'"

	// DDL takes no bind parameters: the role name is validated above and the
	// password is quoted as a SQL literal.
	//nolint:gosec // G201: identifiers cannot be bound; inputs are validated/quoted above.
	statement := fmt.Sprintf(`DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = '%[1]s') THEN
        CREATE ROLE %[1]s LOGIN NOSUPERUSER NOBYPASSRLS PASSWORD %[2]s;
    ELSE
        ALTER ROLE %[1]s WITH LOGIN NOSUPERUSER NOBYPASSRLS PASSWORD %[2]s;
    END IF;
    GRANT vietgara_app TO %[1]s;
END
$$;`, user, quotedPassword)

	if _, err := conn.ExecContext(context.Background(), statement); err != nil {
		return fmt.Errorf("ensure database user %s: %w", user, err)
	}

	return nil
}

func validRoleName(name string) bool {
	if name == "" || len(name) > 63 {
		return false
	}

	for i, r := range name {
		switch {
		case r == '_', r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9' && i > 0:
		default:
			return false
		}
	}

	return true
}
