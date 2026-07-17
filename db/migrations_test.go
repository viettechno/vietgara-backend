package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// PostgresContainer represents a running PostgreSQL container for testing
type PostgresContainer struct {
	Container testcontainers.Container
	Host      string
	Port      string
	DB        *sql.DB
	ctx       context.Context
}

// StartPostgresContainer starts a PostgreSQL container and returns a connection
func StartPostgresContainer(ctx context.Context) (*PostgresContainer, error) {
	req := testcontainers.ContainerRequest{
		Image:        "postgres:15-alpine",
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_DB":       "vietgara_test",
			"POSTGRES_USER":     "postgres",
			"POSTGRES_PASSWORD": "postgres",
		},
		WaitingFor: wait.ForLog("database system is ready to accept connections").
			WithOccurrence(2).
			WithStartupTimeout(30 * time.Second),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to start container: %w", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		container.Terminate(ctx)
		return nil, fmt.Errorf("failed to get host: %w", err)
	}

	port, err := container.MappedPort(ctx, "5432")
	if err != nil {
		container.Terminate(ctx)
		return nil, fmt.Errorf("failed to get port: %w", err)
	}

	// Give container a moment to be fully ready
	time.Sleep(1 * time.Second)

	dsn := fmt.Sprintf("postgres://postgres:postgres@%s:%s/vietgara_test?sslmode=disable", host, port.Port())
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		container.Terminate(ctx)
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Test the connection
	if err := db.PingContext(ctx); err != nil {
		container.Terminate(ctx)
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &PostgresContainer{
		Container: container,
		Host:      host,
		Port:      port.Port(),
		DB:        db,
		ctx:       ctx,
	}, nil
}

// Close closes the database connection and terminates the container
func (pc *PostgresContainer) Close() error {
	if pc.DB != nil {
		pc.DB.Close()
	}
	if pc.Container != nil {
		return pc.Container.Terminate(pc.ctx)
	}
	return nil
}

// RunMigrations runs all migration files in order
func (pc *PostgresContainer) RunMigrations(migrationsDir string) error {
	// Read migration files and execute them in order
	files := []string{
		"000001_create_accounts_table.up.sql",
		"000002_create_garages_table.up.sql",
		"000003_create_garage_staff_table.up.sql",
	}

	for _, file := range files {
		filePath := filepath.Join(migrationsDir, file)
		content, err := os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("failed to read migration file %s: %w", file, err)
		}

		if _, err := pc.DB.ExecContext(pc.ctx, string(content)); err != nil {
			return fmt.Errorf("failed to execute migration %s: %w", file, err)
		}
	}

	return nil
}

// TestRLS_QueryFailsWithoutSessionVariable verifies that queries are blocked when session variable is not set
func TestRLS_QueryFailsWithoutSessionVariable(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	// Start PostgreSQL container
	pgContainer, err := StartPostgresContainer(ctx)
	if err != nil {
		t.Fatalf("failed to start postgres container: %v", err)
	}
	defer pgContainer.Close()

	// Get the migrations directory relative to this test file
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to determine test file location")
	}
	migrationsDir := filepath.Join(filepath.Dir(testFile), "migrations")

	// Run migrations
	if err := pgContainer.RunMigrations(migrationsDir); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}

	// Insert test data
	accountID := "00000000-0000-0000-0000-000000000001"
	garageID := "00000000-0000-0000-0000-000000000002"

	insertAccount := fmt.Sprintf(
		`INSERT INTO accounts (id, email, auth_provider, identity_platform_uid, full_name, status) 
		 VALUES ('%s', 'test@example.com', 'local', 'uid-001', 'Test User', 'active')`,
		accountID,
	)
	if _, err := pgContainer.DB.ExecContext(ctx, insertAccount); err != nil {
		t.Fatalf("failed to insert account: %v", err)
	}

	insertGarage := fmt.Sprintf(
		`INSERT INTO garages (id, account_id, name, timezone, currency, status) 
		 VALUES ('%s', '%s', 'Test Garage', 'UTC', 'USD', 'active')`,
		garageID, accountID,
	)
	if _, err := pgContainer.DB.ExecContext(ctx, insertGarage); err != nil {
		t.Fatalf("failed to insert garage: %v", err)
	}

	// Test: Query garages table WITHOUT setting session variable - should return 0 rows
	var count int
	err = pgContainer.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM garages").Scan(&count)
	if err != nil {
		t.Fatalf("failed to query garages: %v", err)
	}

	if count != 0 {
		t.Errorf("expected 0 rows without session variable, got %d rows. RLS is not working correctly", count)
	}
	t.Logf("✓ Query without session variable correctly returned 0 rows")
}

// TestRLS_QuerySucceedsWithCorrectSessionVariable verifies that queries work when session variable matches
func TestRLS_QuerySucceedsWithCorrectSessionVariable(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	// Start PostgreSQL container
	pgContainer, err := StartPostgresContainer(ctx)
	if err != nil {
		t.Fatalf("failed to start postgres container: %v", err)
	}
	defer pgContainer.Close()

	// Get the migrations directory
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to determine test file location")
	}
	migrationsDir := filepath.Join(filepath.Dir(testFile), "migrations")

	// Run migrations
	if err := pgContainer.RunMigrations(migrationsDir); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}

	// Insert test data
	accountID := "00000000-0000-0000-0000-000000000003"
	garageID := "00000000-0000-0000-0000-000000000004"

	insertAccount := fmt.Sprintf(
		`INSERT INTO accounts (id, email, auth_provider, identity_platform_uid, full_name, status) 
		 VALUES ('%s', 'test2@example.com', 'local', 'uid-002', 'Test User 2', 'active')`,
		accountID,
	)
	if _, err := pgContainer.DB.ExecContext(ctx, insertAccount); err != nil {
		t.Fatalf("failed to insert account: %v", err)
	}

	insertGarage := fmt.Sprintf(
		`INSERT INTO garages (id, account_id, name, timezone, currency, status) 
		 VALUES ('%s', '%s', 'Test Garage 2', 'UTC', 'USD', 'active')`,
		garageID, accountID,
	)
	if _, err := pgContainer.DB.ExecContext(ctx, insertGarage); err != nil {
		t.Fatalf("failed to insert garage: %v", err)
	}

	// Set session variable and query - should return 1 row
	setSession := fmt.Sprintf("SELECT set_config('app.current_garage_id', '%s'::text, false)", garageID)
	if _, err := pgContainer.DB.ExecContext(ctx, setSession); err != nil {
		t.Fatalf("failed to set session variable: %v", err)
	}

	var count int
	err = pgContainer.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM garages").Scan(&count)
	if err != nil {
		t.Fatalf("failed to query garages with session variable: %v", err)
	}

	if count != 1 {
		t.Errorf("expected 1 row with correct session variable, got %d rows", count)
	}
	t.Logf("✓ Query with correct session variable returned 1 row")

	// Verify we can read the data
	var name string
	err = pgContainer.DB.QueryRowContext(ctx, "SELECT name FROM garages").Scan(&name)
	if err != nil {
		t.Fatalf("failed to read garage name: %v", err)
	}
	if name != "Test Garage 2" {
		t.Errorf("expected name 'Test Garage 2', got '%s'", name)
	}
	t.Logf("✓ Successfully read garage data with correct session variable")
}

// TestRLS_QueryBlockedWithWrongSessionVariable verifies that queries are blocked when session variable is wrong
func TestRLS_QueryBlockedWithWrongSessionVariable(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	// Start PostgreSQL container
	pgContainer, err := StartPostgresContainer(ctx)
	if err != nil {
		t.Fatalf("failed to start postgres container: %v", err)
	}
	defer pgContainer.Close()

	// Get the migrations directory
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to determine test file location")
	}
	migrationsDir := filepath.Join(filepath.Dir(testFile), "migrations")

	// Run migrations
	if err := pgContainer.RunMigrations(migrationsDir); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}

	// Insert test data for two different garages
	accountID1 := "00000000-0000-0000-0000-000000000005"
	accountID2 := "00000000-0000-0000-0000-000000000006"
	garageID1 := "00000000-0000-0000-0000-000000000007"
	garageID2 := "00000000-0000-0000-0000-000000000008"

	// Insert accounts
	for id, email := range map[string]string{accountID1: "test3@example.com", accountID2: "test4@example.com"} {
		insertAccount := fmt.Sprintf(
			`INSERT INTO accounts (id, email, auth_provider, identity_platform_uid, full_name, status) 
			 VALUES ('%s', '%s', 'local', 'uid-%s', 'Test User', 'active')`,
			id, email, id[:8],
		)
		if _, err := pgContainer.DB.ExecContext(ctx, insertAccount); err != nil {
			t.Fatalf("failed to insert account: %v", err)
		}
	}

	// Insert garages
	insertGarage1 := fmt.Sprintf(
		`INSERT INTO garages (id, account_id, name, timezone, currency, status) 
		 VALUES ('%s', '%s', 'Garage 1', 'UTC', 'USD', 'active')`,
		garageID1, accountID1,
	)
	if _, err := pgContainer.DB.ExecContext(ctx, insertGarage1); err != nil {
		t.Fatalf("failed to insert garage 1: %v", err)
	}

	insertGarage2 := fmt.Sprintf(
		`INSERT INTO garages (id, account_id, name, timezone, currency, status) 
		 VALUES ('%s', '%s', 'Garage 2', 'UTC', 'USD', 'active')`,
		garageID2, accountID2,
	)
	if _, err := pgContainer.DB.ExecContext(ctx, insertGarage2); err != nil {
		t.Fatalf("failed to insert garage 2: %v", err)
	}

	// Set session to garage 1
	setSession := fmt.Sprintf("SELECT set_config('app.current_garage_id', '%s'::text, false)", garageID1)
	if _, err := pgContainer.DB.ExecContext(ctx, setSession); err != nil {
		t.Fatalf("failed to set session variable: %v", err)
	}

	// Query should only return garage 1
	var count int
	err = pgContainer.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM garages").Scan(&count)
	if err != nil {
		t.Fatalf("failed to query garages: %v", err)
	}

	if count != 1 {
		t.Errorf("expected 1 row (garage 1 only), got %d rows", count)
	}

	// Verify the returned garage is garage 1
	var name string
	err = pgContainer.DB.QueryRowContext(ctx, "SELECT name FROM garages").Scan(&name)
	if err != nil {
		t.Fatalf("failed to read garage name: %v", err)
	}
	if name != "Garage 1" {
		t.Errorf("expected 'Garage 1', got '%s'", name)
	}

	t.Logf("✓ RLS correctly restricts to garage 1 when session variable is set to garage 1")
	t.Logf("✓ Cannot see Garage 2 - RLS is working correctly")
}

// TestRLS_GarageStaffRowLevelSecurity verifies RLS on garage_staff table
func TestRLS_GarageStaffRowLevelSecurity(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	// Start PostgreSQL container
	pgContainer, err := StartPostgresContainer(ctx)
	if err != nil {
		t.Fatalf("failed to start postgres container: %v", err)
	}
	defer pgContainer.Close()

	// Get the migrations directory
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to determine test file location")
	}
	migrationsDir := filepath.Join(filepath.Dir(testFile), "migrations")

	// Run migrations
	if err := pgContainer.RunMigrations(migrationsDir); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}

	// Insert test data
	accountID := "00000000-0000-0000-0000-000000000009"
	garageID := "00000000-0000-0000-0000-00000000000a"
	staffID := "00000000-0000-0000-0000-00000000000b"

	// Insert account
	insertAccount := fmt.Sprintf(
		`INSERT INTO accounts (id, email, auth_provider, identity_platform_uid, full_name, status) 
		 VALUES ('%s', 'staff@example.com', 'local', 'uid-staff-001', 'Staff User', 'active')`,
		accountID,
	)
	if _, err := pgContainer.DB.ExecContext(ctx, insertAccount); err != nil {
		t.Fatalf("failed to insert account: %v", err)
	}

	// Insert garage
	insertGarage := fmt.Sprintf(
		`INSERT INTO garages (id, account_id, name, timezone, currency, status) 
		 VALUES ('%s', '%s', 'Staff Garage', 'UTC', 'USD', 'active')`,
		garageID, accountID,
	)
	if _, err := pgContainer.DB.ExecContext(ctx, insertGarage); err != nil {
		t.Fatalf("failed to insert garage: %v", err)
	}

	// Insert garage_staff
	insertStaff := fmt.Sprintf(
		`INSERT INTO garage_staff (id, garage_id, account_id, role, status) 
		 VALUES ('%s', '%s', '%s', 'owner', 'active')`,
		staffID, garageID, accountID,
	)
	if _, err := pgContainer.DB.ExecContext(ctx, insertStaff); err != nil {
		t.Fatalf("failed to insert garage_staff: %v", err)
	}

	// Test 1: Query garage_staff WITHOUT session variable - should return 0 rows
	var count int
	err = pgContainer.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM garage_staff").Scan(&count)
	if err != nil {
		t.Fatalf("failed to query garage_staff: %v", err)
	}

	if count != 0 {
		t.Errorf("expected 0 rows without session variable, got %d rows", count)
	}
	t.Logf("✓ garage_staff: Query without session variable correctly returned 0 rows")

	// Test 2: Set session variable and query - should return 1 row
	setSession := fmt.Sprintf("SELECT set_config('app.current_garage_id', '%s'::text, false)", garageID)
	if _, err := pgContainer.DB.ExecContext(ctx, setSession); err != nil {
		t.Fatalf("failed to set session variable: %v", err)
	}

	err = pgContainer.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM garage_staff").Scan(&count)
	if err != nil {
		t.Fatalf("failed to query garage_staff with session: %v", err)
	}

	if count != 1 {
		t.Errorf("expected 1 row with session variable, got %d rows", count)
	}
	t.Logf("✓ garage_staff: Query with session variable returned 1 row")

	// Verify we can read the role
	var role string
	err = pgContainer.DB.QueryRowContext(ctx, "SELECT role FROM garage_staff").Scan(&role)
	if err != nil {
		t.Fatalf("failed to read staff role: %v", err)
	}
	if role != "owner" {
		t.Errorf("expected role 'owner', got '%s'", role)
	}
	t.Logf("✓ garage_staff: Successfully read role with session variable")
}

// TestRLS_SessionVariableHandling verifies proper session variable handling
func TestRLS_SessionVariableHandling(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	// Start PostgreSQL container
	pgContainer, err := StartPostgresContainer(ctx)
	if err != nil {
		t.Fatalf("failed to start postgres container: %v", err)
	}
	defer pgContainer.Close()

	// Get the migrations directory
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to determine test file location")
	}
	migrationsDir := filepath.Join(filepath.Dir(testFile), "migrations")

	// Run migrations
	if err := pgContainer.RunMigrations(migrationsDir); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}

	// Test setting and checking session variable
	garageID := "00000000-0000-0000-0000-00000000000c"
	setSession := fmt.Sprintf("SELECT set_config('app.current_garage_id', '%s'::text, false)", garageID)
	if _, err := pgContainer.DB.ExecContext(ctx, setSession); err != nil {
		t.Fatalf("failed to set session variable: %v", err)
	}

	// Verify session variable is set
	var retrievedValue string
	err = pgContainer.DB.QueryRowContext(ctx, "SELECT current_setting('app.current_garage_id')").Scan(&retrievedValue)
	if err != nil {
		t.Fatalf("failed to retrieve session variable: %v", err)
	}

	if retrievedValue != garageID {
		t.Errorf("expected session variable '%s', got '%s'", garageID, retrievedValue)
	}
	t.Logf("✓ Session variable correctly set and retrieved: %s", retrievedValue)
}
