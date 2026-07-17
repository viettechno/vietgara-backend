package db

import (
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

// RunMigrations runs all pending migrations
// connectionString should be in format: postgres://user:password@host:port/dbname?sslmode=disable
// migrationsPath should be the path to migrations directory: file://path/to/migrations
func RunMigrations(connectionString string, migrationsPath string) error {
	m, err := migrate.New(migrationsPath, connectionString)
	if err != nil {
		return fmt.Errorf("failed to create migration instance: %w", err)
	}
	defer m.Close()

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	version, dirty, err := m.Version()
	if err != nil {
		return fmt.Errorf("failed to get migration version: %w", err)
	}

	fmt.Printf("Migrations completed. Current version: %d (dirty: %v)\n", version, dirty)
	return nil
}

// RollbackLastMigration rolls back the last migration
func RollbackLastMigration(connectionString string, migrationsPath string) error {
	m, err := migrate.New(migrationsPath, connectionString)
	if err != nil {
		return fmt.Errorf("failed to create migration instance: %w", err)
	}
	defer m.Close()

	if err := m.Steps(-1); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("failed to rollback migration: %w", err)
	}

	return nil
}

// RollbackAllMigrations rolls back all migrations
func RollbackAllMigrations(connectionString string, migrationsPath string) error {
	m, err := migrate.New(migrationsPath, connectionString)
	if err != nil {
		return fmt.Errorf("failed to create migration instance: %w", err)
	}
	defer m.Close()

	if err := m.Down(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("failed to rollback all migrations: %w", err)
	}

	return nil
}

// GetMigrationVersion returns the current migration version
func GetMigrationVersion(connectionString string, migrationsPath string) (uint, bool, error) {
	m, err := migrate.New(migrationsPath, connectionString)
	if err != nil {
		return 0, false, fmt.Errorf("failed to create migration instance: %w", err)
	}
	defer m.Close()

	version, dirty, err := m.Version()
	if err != nil {
		return 0, false, fmt.Errorf("failed to get migration version: %w", err)
	}

	return version, dirty, nil
}

// ForceMigrationVersion forces the migration version without running migrations
func ForceMigrationVersion(connectionString string, migrationsPath string, version int) error {
	m, err := migrate.New(migrationsPath, connectionString)
	if err != nil {
		return fmt.Errorf("failed to create migration instance: %w", err)
	}
	defer m.Close()

	if err := m.Force(version); err != nil {
		return fmt.Errorf("failed to force migration version: %w", err)
	}

	return nil
}
