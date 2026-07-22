package db_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"
	"vietgara/db"

	_ "github.com/lib/pq"
	"github.com/stretchr/testify/suite"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	MIGRATIONS_PATH = "file://migrations"

	POSTGRES_TEST_DB       = "vietgara_test"
	POSTGRES_TEST_USER     = "postgres"
	POSTGRES_TEST_PASSWORD = "postgres"

	rlsTestRole = "rls_test_reader"
)

type PostgresContainer struct {
	Container testcontainers.Container
	Host      string
	Port      string
	DB        *sql.DB
	ConnStr   string
	ctx       context.Context
}

func StartPostgresContainer(ctx context.Context) (*PostgresContainer, error) {
	req := testcontainers.ContainerRequest{
		Image:        "postgres:15-alpine",
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_DB":       POSTGRES_TEST_DB,
			"POSTGRES_USER":     POSTGRES_TEST_USER,
			"POSTGRES_PASSWORD": POSTGRES_TEST_PASSWORD,
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
		_ = container.Terminate(ctx)
		return nil, fmt.Errorf("failed to get host: %w", err)
	}

	port, err := container.MappedPort(ctx, "5432")
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, fmt.Errorf("failed to get port: %w", err)
	}

	time.Sleep(1 * time.Second)

	dsn := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", POSTGRES_TEST_USER, POSTGRES_TEST_PASSWORD, host, port.Port(), POSTGRES_TEST_DB)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := db.PingContext(ctx); err != nil {
		_ = container.Terminate(ctx)
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &PostgresContainer{
		Container: container,
		Host:      host,
		Port:      port.Port(),
		DB:        db,
		ConnStr:   dsn,
		ctx:       ctx,
	}, nil
}

func (pc *PostgresContainer) Close() error {
	if pc.DB != nil {
		_ = pc.DB.Close()
	}

	if pc.Container != nil {
		return pc.Container.Terminate(pc.ctx)
	}

	return nil
}

func (pc *PostgresContainer) OpenRLSConnection(ctx context.Context) (*sql.Conn, error) {
	conn, err := pc.DB.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("open RLS test connection: %w", err)
	}

	if _, err := conn.ExecContext(ctx, "SET ROLE "+rlsTestRole); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("set RLS test role: %w", err)
	}

	return conn, nil
}

type MigrationsTestSuite struct {
	suite.Suite
	ctx         context.Context
	cancel      context.CancelFunc
	pgContainer *PostgresContainer
}

// SetupSuite runs once before any test in the suite starts.
func (s *MigrationsTestSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 120*time.Second)
	pgContainer, err := StartPostgresContainer(s.ctx)
	s.Require().NoError(err)
	s.pgContainer = pgContainer
	err = db.RunMigrations(s.pgContainer.ConnStr, MIGRATIONS_PATH)
	s.Require().NoError(err)

	for _, statement := range []string{
		"CREATE ROLE " + rlsTestRole + " NOLOGIN NOSUPERUSER NOBYPASSRLS",
		"GRANT USAGE ON SCHEMA public TO " + rlsTestRole,
		"GRANT SELECT ON garages, garage_staff TO " + rlsTestRole,
	} {
		_, err = s.pgContainer.DB.ExecContext(s.ctx, statement)
		s.Require().NoError(err)
	}
}

// TearDownSuite runs once after all tests in the suite finish.
func (s *MigrationsTestSuite) TearDownSuite() {
	if s.pgContainer != nil {
		s.pgContainer.Close()
	}
	s.cancel()
}

func (s *MigrationsTestSuite) openRLSConnection() *sql.Conn {
	conn, err := s.pgContainer.OpenRLSConnection(s.ctx)
	s.Require().NoError(err)
	return conn
}

func (s *MigrationsTestSuite) TestRLS_QueryFailsWithoutSessionVariable() {
	accountID := "00000000-0000-0000-0000-000000000001"
	garageID := "00000000-0000-0000-0000-000000000002"

	insertAccount := fmt.Sprintf(
		`INSERT INTO accounts (id, email, auth_provider, identity_platform_uid, full_name, status)
		 VALUES ('%s', 'test@example.com', 'local', 'uid-001', 'Test User', 'active')`,
		accountID,
	)
	_, err := s.pgContainer.DB.ExecContext(s.ctx, insertAccount)
	s.Require().NoError(err)

	insertGarage := fmt.Sprintf(
		`INSERT INTO garages (id, account_id, name, timezone, currency, status)
		 VALUES ('%s', '%s', 'Test Garage', 'UTC', 'USD', 'active')`,
		garageID, accountID,
	)
	_, err = s.pgContainer.DB.ExecContext(s.ctx, insertGarage)
	s.Require().NoError(err)

	rlsConn := s.openRLSConnection()

	var count int

	err = rlsConn.QueryRowContext(s.ctx, "SELECT COUNT(*) FROM garages").Scan(&count)
	s.Require().NoError(err)
	s.Assert().Equalf(0, count, "expected 0 rows without session variable. RLS is not working correctly")
}

func (s *MigrationsTestSuite) TestRLS_QuerySucceedsWithCorrectSessionVariable() {
	accountID := "00000000-0000-0000-0000-000000000003"
	garageID := "00000000-0000-0000-0000-000000000004"

	insertAccount := fmt.Sprintf(
		`INSERT INTO accounts (id, email, auth_provider, identity_platform_uid, full_name, status)
		 VALUES ('%s', 'test2@example.com', 'local', 'uid-002', 'Test User 2', 'active')`,
		accountID,
	)
	_, err := s.pgContainer.DB.ExecContext(s.ctx, insertAccount)
	s.Require().NoError(err)

	insertGarage := fmt.Sprintf(
		`INSERT INTO garages (id, account_id, name, timezone, currency, status)
		 VALUES ('%s', '%s', 'Test Garage 2', 'UTC', 'USD', 'active')`,
		garageID, accountID,
	)
	_, err = s.pgContainer.DB.ExecContext(s.ctx, insertGarage)
	s.Require().NoError(err)

	rlsConn := s.openRLSConnection()

	setSession := fmt.Sprintf("SELECT set_config('app.current_garage_id', '%s'::text, false)", garageID)
	_, err = rlsConn.ExecContext(s.ctx, setSession)
	s.Require().NoError(err)

	var count int

	err = rlsConn.QueryRowContext(s.ctx, "SELECT COUNT(*) FROM garages").Scan(&count)
	s.Require().NoError(err)
	s.Assert().Equalf(1, count, "expected 1 row with correct session variable")

	var name string

	err = rlsConn.QueryRowContext(s.ctx, "SELECT name FROM garages").Scan(&name)
	s.Require().NoError(err)
	s.Assert().Equalf("Test Garage 2", name, "expected garage name to match")
}

func (s *MigrationsTestSuite) TestRLS_QueryBlockedWithWrongSessionVariable() {
	accountID1 := "00000000-0000-0000-0000-000000000005"
	accountID2 := "00000000-0000-0000-0000-000000000006"
	garageID1 := "00000000-0000-0000-0000-000000000007"
	garageID2 := "00000000-0000-0000-0000-000000000008"

	for _, account := range []struct {
		id    string
		email string
		uid   string
	}{
		{id: accountID1, email: "test3@example.com", uid: "uid-003"},
		{id: accountID2, email: "test4@example.com", uid: "uid-004"},
	} {
		_, err := s.pgContainer.DB.ExecContext(s.ctx,
			`INSERT INTO accounts (id, email, auth_provider, identity_platform_uid, full_name, status)
			 VALUES ($1, $2, 'local', $3, 'Test User', 'active')`,
			account.id, account.email, account.uid,
		)
		s.Require().NoError(err)
	}

	insertGarage1 := fmt.Sprintf(
		`INSERT INTO garages (id, account_id, name, timezone, currency, status)
		 VALUES ('%s', '%s', 'Garage 1', 'UTC', 'USD', 'active')`,
		garageID1, accountID1,
	)
	_, err := s.pgContainer.DB.ExecContext(s.ctx, insertGarage1)
	s.Require().NoError(err)

	insertGarage2 := fmt.Sprintf(
		`INSERT INTO garages (id, account_id, name, timezone, currency, status)
		 VALUES ('%s', '%s', 'Garage 2', 'UTC', 'USD', 'active')`,
		garageID2, accountID2,
	)
	_, err = s.pgContainer.DB.ExecContext(s.ctx, insertGarage2)
	s.Require().NoError(err)

	rlsConn := s.openRLSConnection()

	setSession := fmt.Sprintf("SELECT set_config('app.current_garage_id', '%s'::text, false)", garageID1)
	_, err = rlsConn.ExecContext(s.ctx, setSession)
	s.Require().NoError(err)

	var count int

	err = rlsConn.QueryRowContext(s.ctx, "SELECT COUNT(*) FROM garages").Scan(&count)
	s.Require().NoError(err)
	s.Assert().Equalf(1, count, "expected 1 row (garage 1 only)")

	var name string

	err = rlsConn.QueryRowContext(s.ctx, "SELECT name FROM garages").Scan(&name)
	s.Require().NoError(err)
	s.Assert().Equalf("Garage 1", name, "expected the visible garage to be Garage 1")
}

func (s *MigrationsTestSuite) TestRLS_GarageStaffRowLevelSecurity() {
	accountID := "00000000-0000-0000-0000-000000000009"
	garageID := "00000000-0000-0000-0000-00000000000a"
	staffID := "00000000-0000-0000-0000-00000000000b"

	insertAccount := fmt.Sprintf(
		`INSERT INTO accounts (id, email, auth_provider, identity_platform_uid, full_name, status)
		 VALUES ('%s', 'staff@example.com', 'local', 'uid-staff-001', 'Staff User', 'active')`,
		accountID,
	)
	_, err := s.pgContainer.DB.ExecContext(s.ctx, insertAccount)
	s.Require().NoError(err)

	insertGarage := fmt.Sprintf(
		`INSERT INTO garages (id, account_id, name, timezone, currency, status)
		 VALUES ('%s', '%s', 'Staff Garage', 'UTC', 'USD', 'active')`,
		garageID, accountID,
	)
	_, err = s.pgContainer.DB.ExecContext(s.ctx, insertGarage)
	s.Require().NoError(err)

	insertStaff := fmt.Sprintf(
		`INSERT INTO garage_staff (id, garage_id, account_id, role, status)
		 VALUES ('%s', '%s', '%s', 'owner', 'active')`,
		staffID, garageID, accountID,
	)
	_, err = s.pgContainer.DB.ExecContext(s.ctx, insertStaff)
	s.Require().NoError(err)

	rlsConn := s.openRLSConnection()

	var count int

	err = rlsConn.QueryRowContext(s.ctx, "SELECT COUNT(*) FROM garage_staff").Scan(&count)
	s.Require().NoError(err)
	s.Assert().Equalf(0, count, "expected 0 rows without session variable")

	setSession := fmt.Sprintf("SELECT set_config('app.current_garage_id', '%s'::text, false)", garageID)
	_, err = rlsConn.ExecContext(s.ctx, setSession)
	s.Require().NoError(err)

	err = rlsConn.QueryRowContext(s.ctx, "SELECT COUNT(*) FROM garage_staff").Scan(&count)
	s.Require().NoError(err)
	s.Assert().Equalf(1, count, "expected 1 row with session variable")

	var role string

	err = rlsConn.QueryRowContext(s.ctx, "SELECT role FROM garage_staff").Scan(&role)
	s.Require().NoError(err)
	s.Assert().Equalf("owner", role, "expected staff role to be 'owner'")
}

func (s *MigrationsTestSuite) TestRLS_SessionVariableHandling() {
	garageID := "00000000-0000-0000-0000-00000000000c"

	setSession := fmt.Sprintf("SELECT set_config('app.current_garage_id', '%s'::text, false)", garageID)
	_, err := s.pgContainer.DB.ExecContext(s.ctx, setSession)
	s.Require().NoError(err)

	var retrievedValue string

	err = s.pgContainer.DB.QueryRowContext(s.ctx, "SELECT current_setting('app.current_garage_id')").Scan(&retrievedValue)
	s.Require().NoError(err)
	s.Assert().Equalf(garageID, retrievedValue, "expected session variable to match")
}

func (s *MigrationsTestSuite) TestGetMigrationVersion() {
	tests := []struct {
		name      string
		path      string
		wantErr   bool
		wantVer   uint
		wantDirty bool
	}{
		{
			name:      "successful with valid path",
			path:      MIGRATIONS_PATH,
			wantErr:   false,
			wantVer:   3,
			wantDirty: false,
		},
		{
			name:    "failed with invalid migration path",
			path:    "file://nonexistent",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			version, dirty, err := db.GetMigrationVersion(s.pgContainer.ConnStr, tt.path)
			if tt.wantErr {
				s.Assert().Error(err)
				return
			}
			s.Require().NoError(err)
			s.Assert().Equalf(tt.wantVer, version, "expected version %d", tt.wantVer)
			s.Assert().Equalf(tt.wantDirty, dirty, "expected dirty=%v", tt.wantDirty)
		})
	}
}

func (s *MigrationsTestSuite) TestRollbackLastMigration() {
	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{
			name:    "successful rolling back one step",
			path:    MIGRATIONS_PATH,
			wantErr: false,
		},
		{
			name:    "failed with invalid migration path",
			path:    "file://nonexistent",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			err := db.RollbackLastMigration(s.pgContainer.ConnStr, tt.path)
			if tt.wantErr {
				s.Assert().Error(err)
				return
			}
			s.Require().NoError(err)

			version, _, err := db.GetMigrationVersion(s.pgContainer.ConnStr, MIGRATIONS_PATH)
			s.Require().NoError(err)
			s.Assert().Equalf(uint(2), version, "expected version 2 after rolling back one step")

			s.Require().NoError(db.RunMigrations(s.pgContainer.ConnStr, MIGRATIONS_PATH))
		})
	}
}

func (s *MigrationsTestSuite) TestRollbackAllMigrations() {
	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{
			name:    "successful rolling back all migrations",
			path:    MIGRATIONS_PATH,
			wantErr: false,
		},
		{
			name:    "failed with invalid migration path",
			path:    "file://nonexistent",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			err := db.RollbackAllMigrations(s.pgContainer.ConnStr, tt.path)
			if tt.wantErr {
				s.Assert().Error(err)
				return
			}
			s.Require().NoError(err)

			_, _, err = db.GetMigrationVersion(s.pgContainer.ConnStr, MIGRATIONS_PATH)
			s.Assert().Errorf(err, "expected error after rolling back all migrations")

			s.Require().NoError(db.RunMigrations(s.pgContainer.ConnStr, MIGRATIONS_PATH))

			version, dirty, err := db.GetMigrationVersion(s.pgContainer.ConnStr, MIGRATIONS_PATH)
			s.Require().NoError(err)
			s.Assert().Equalf(uint(3), version, "expected version restored to 3")
			s.Assert().False(dirty, "expected migration state not dirty")
		})
	}
}

func (s *MigrationsTestSuite) TestForceMigrationVersion() {
	tests := []struct {
		name    string
		path    string
		version int
		wantErr bool
	}{
		{
			name:    "successful force to version 2",
			path:    MIGRATIONS_PATH,
			version: 2,
			wantErr: false,
		},
		{
			name:    "failed with negative version",
			path:    MIGRATIONS_PATH,
			version: -2,
			wantErr: true,
		},
		{
			name:    "failed with invalid migration path",
			path:    "file://nonexistent",
			version: 2,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			err := db.ForceMigrationVersion(s.pgContainer.ConnStr, tt.path, tt.version)
			if tt.wantErr {
				s.Assert().Error(err)
				return
			}
			s.Require().NoError(err)

			version, dirty, err := db.GetMigrationVersion(s.pgContainer.ConnStr, MIGRATIONS_PATH)
			s.Require().NoError(err)
			s.Assert().Equalf(uint(tt.version), version, "expected version forced to %d", tt.version)
			s.Assert().False(dirty, "expected migration state not dirty after Force")

			s.Require().NoError(db.ForceMigrationVersion(s.pgContainer.ConnStr, MIGRATIONS_PATH, 3))

			version, dirty, err = db.GetMigrationVersion(s.pgContainer.ConnStr, MIGRATIONS_PATH)
			s.Require().NoError(err)
			s.Assert().Equalf(uint(3), version, "expected version forced back to 3")
			s.Assert().False(dirty, "expected migration state not dirty after Force")
		})
	}
}

func TestMigrationsSuite(t *testing.T) {
	suite.Run(t, new(MigrationsTestSuite))
}
