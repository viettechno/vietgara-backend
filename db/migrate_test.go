//go:build integration

package db_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/stretchr/testify/suite"

	"github.com/viettechno/vietgara-backend/db"
	"github.com/viettechno/vietgara-backend/internal/platform/db/pgtest"
)

const (
	MIGRATIONS_PATH           = "file://migrations"
	MIGRATION_CURRENT_VERSION = 80

	rlsTestRole = "rls_test_reader"
)

type MigrationsTestSuite struct {
	suite.Suite
	ctx    context.Context
	cancel context.CancelFunc
	pg     *pgtest.Postgres
}

// SetupSuite runs once before any test in the suite starts.
func (s *MigrationsTestSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 120*time.Second)
	s.pg = pgtest.Start(s.T())

	s.Require().NoError(db.RunMigrations(s.pg.SuperDSN(), MIGRATIONS_PATH))

	for _, statement := range []string{
		"CREATE ROLE " + rlsTestRole + " NOLOGIN NOSUPERUSER NOBYPASSRLS",
		"GRANT USAGE ON SCHEMA public TO " + rlsTestRole,
		"GRANT SELECT ON ALL TABLES IN SCHEMA public TO " + rlsTestRole,
	} {
		_, err := s.pg.Admin.ExecContext(s.ctx, statement)
		s.Require().NoError(err)
	}
}

// TearDownSuite runs once after all tests in the suite finish.
func (s *MigrationsTestSuite) TearDownSuite() {
	s.cancel()
}

func (s *MigrationsTestSuite) openRLSConnection() *sql.Conn {
	conn, err := s.pg.Admin.Conn(s.ctx)
	s.Require().NoError(err)

	_, err = conn.ExecContext(s.ctx, "SET ROLE "+rlsTestRole)
	s.Require().NoError(err)

	return conn
}

func (s *MigrationsTestSuite) TestRLS_QueryFailsWithoutSessionVariable() {
	userID := "00000000-0000-0000-0000-000000000001"
	garageID := "00000000-0000-0000-0000-000000000002"

	insertUser := fmt.Sprintf(
		`INSERT INTO users (id, email, auth_provider, identity_platform_uid, full_name, status)
		 VALUES ('%s', 'test@example.com', 'email', 'uid-001', 'Test User', 'active')`,
		userID,
	)
	_, err := s.pg.Admin.ExecContext(s.ctx, insertUser)
	s.Require().NoError(err)

	insertGarage := fmt.Sprintf(
		`INSERT INTO garages (id, user_id, name, timezone, currency, status)
		 VALUES ('%s', '%s', 'Test Garage', 'UTC', 'USD', 'active')`,
		garageID, userID,
	)
	_, err = s.pg.Admin.ExecContext(s.ctx, insertGarage)
	s.Require().NoError(err)

	rlsConn := s.openRLSConnection()

	var count int

	err = rlsConn.QueryRowContext(s.ctx, "SELECT COUNT(*) FROM garages").Scan(&count)
	s.Require().NoError(err)
	s.Equalf(0, count, "expected 0 rows without session variable. RLS is not working correctly")
}

func (s *MigrationsTestSuite) TestRLS_QuerySucceedsWithCorrectSessionVariable() {
	userID := "00000000-0000-0000-0000-000000000003"
	garageID := "00000000-0000-0000-0000-000000000004"

	insertUser := fmt.Sprintf(
		`INSERT INTO users (id, email, auth_provider, identity_platform_uid, full_name, status)
		 VALUES ('%s', 'test2@example.com', 'email', 'uid-002', 'Test User 2', 'active')`,
		userID,
	)
	_, err := s.pg.Admin.ExecContext(s.ctx, insertUser)
	s.Require().NoError(err)

	insertGarage := fmt.Sprintf(
		`INSERT INTO garages (id, user_id, name, timezone, currency, status)
		 VALUES ('%s', '%s', 'Test Garage 2', 'UTC', 'USD', 'active')`,
		garageID, userID,
	)
	_, err = s.pg.Admin.ExecContext(s.ctx, insertGarage)
	s.Require().NoError(err)

	rlsConn := s.openRLSConnection()

	setSession := fmt.Sprintf("SELECT set_config('app.current_garage_id', '%s'::text, false)", garageID)
	_, err = rlsConn.ExecContext(s.ctx, setSession)
	s.Require().NoError(err)

	var count int

	err = rlsConn.QueryRowContext(s.ctx, "SELECT COUNT(*) FROM garages").Scan(&count)
	s.Require().NoError(err)
	s.Equalf(1, count, "expected 1 row with correct session variable")

	var name string

	err = rlsConn.QueryRowContext(s.ctx, "SELECT name FROM garages").Scan(&name)
	s.Require().NoError(err)
	s.Equalf("Test Garage 2", name, "expected garage name to match")
}

func (s *MigrationsTestSuite) TestRLS_QueryBlockedWithWrongSessionVariable() {
	userID1 := "00000000-0000-0000-0000-000000000005"
	userID2 := "00000000-0000-0000-0000-000000000006"
	garageID1 := "00000000-0000-0000-0000-000000000007"
	garageID2 := "00000000-0000-0000-0000-000000000008"

	for _, user := range []struct {
		id    string
		email string
		uid   string
	}{
		{id: userID1, email: "test3@example.com", uid: "uid-003"},
		{id: userID2, email: "test4@example.com", uid: "uid-004"},
	} {
		_, err := s.pg.Admin.ExecContext(s.ctx,
			`INSERT INTO users (id, email, auth_provider, identity_platform_uid, full_name, status)
			 VALUES ($1, $2, 'email', $3, 'Test User', 'active')`,
			user.id, user.email, user.uid,
		)
		s.Require().NoError(err)
	}

	insertGarage1 := fmt.Sprintf(
		`INSERT INTO garages (id, user_id, name, timezone, currency, status)
		 VALUES ('%s', '%s', 'Garage 1', 'UTC', 'USD', 'active')`,
		garageID1, userID1,
	)
	_, err := s.pg.Admin.ExecContext(s.ctx, insertGarage1)
	s.Require().NoError(err)

	insertGarage2 := fmt.Sprintf(
		`INSERT INTO garages (id, user_id, name, timezone, currency, status)
		 VALUES ('%s', '%s', 'Garage 2', 'UTC', 'USD', 'active')`,
		garageID2, userID2,
	)
	_, err = s.pg.Admin.ExecContext(s.ctx, insertGarage2)
	s.Require().NoError(err)

	rlsConn := s.openRLSConnection()

	setSession := fmt.Sprintf("SELECT set_config('app.current_garage_id', '%s'::text, false)", garageID1)
	_, err = rlsConn.ExecContext(s.ctx, setSession)
	s.Require().NoError(err)

	var count int

	err = rlsConn.QueryRowContext(s.ctx, "SELECT COUNT(*) FROM garages").Scan(&count)
	s.Require().NoError(err)
	s.Equalf(1, count, "expected 1 row (garage 1 only)")

	var name string

	err = rlsConn.QueryRowContext(s.ctx, "SELECT name FROM garages").Scan(&name)
	s.Require().NoError(err)
	s.Equalf("Garage 1", name, "expected the visible garage to be Garage 1")
}

func (s *MigrationsTestSuite) TestRLS_GarageStaffRowLevelSecurity() {
	userID := "00000000-0000-0000-0000-000000000009"
	garageID := "00000000-0000-0000-0000-00000000000a"
	staffID := "00000000-0000-0000-0000-00000000000b"

	insertUser := fmt.Sprintf(
		`INSERT INTO users (id, email, auth_provider, identity_platform_uid, full_name, status)
		 VALUES ('%s', 'staff@example.com', 'email', 'uid-staff-001', 'Staff User', 'active')`,
		userID,
	)
	_, err := s.pg.Admin.ExecContext(s.ctx, insertUser)
	s.Require().NoError(err)

	insertGarage := fmt.Sprintf(
		`INSERT INTO garages (id, user_id, name, timezone, currency, status)
		 VALUES ('%s', '%s', 'Staff Garage', 'UTC', 'USD', 'active')`,
		garageID, userID,
	)
	_, err = s.pg.Admin.ExecContext(s.ctx, insertGarage)
	s.Require().NoError(err)

	insertStaff := fmt.Sprintf(
		`INSERT INTO garage_staff (id, garage_id, user_id, status)
		 VALUES ('%s', '%s', '%s', 'active')`,
		staffID, garageID, userID,
	)
	_, err = s.pg.Admin.ExecContext(s.ctx, insertStaff)
	s.Require().NoError(err)

	rlsConn := s.openRLSConnection()

	var count int

	err = rlsConn.QueryRowContext(s.ctx, "SELECT COUNT(*) FROM garage_staff").Scan(&count)
	s.Require().NoError(err)
	s.Equalf(0, count, "expected 0 rows without session variable")

	setSession := fmt.Sprintf("SELECT set_config('app.current_garage_id', '%s'::text, false)", garageID)
	_, err = rlsConn.ExecContext(s.ctx, setSession)
	s.Require().NoError(err)

	err = rlsConn.QueryRowContext(s.ctx, "SELECT COUNT(*) FROM garage_staff").Scan(&count)
	s.Require().NoError(err)
	s.Equalf(1, count, "expected 1 row with session variable")

	var status string

	err = rlsConn.QueryRowContext(s.ctx, "SELECT status FROM garage_staff").Scan(&status)
	s.Require().NoError(err)
	s.Equalf("active", status, "expected staff status to be 'active'")
}

func (s *MigrationsTestSuite) TestRLS_SessionVariableHandling() {
	garageID := "00000000-0000-0000-0000-00000000000c"

	setSession := fmt.Sprintf("SELECT set_config('app.current_garage_id', '%s'::text, false)", garageID)
	_, err := s.pg.Admin.ExecContext(s.ctx, setSession)
	s.Require().NoError(err)

	var retrievedValue string

	err = s.pg.Admin.QueryRowContext(s.ctx, "SELECT current_setting('app.current_garage_id')").Scan(&retrievedValue)
	s.Require().NoError(err)
	s.Equalf(garageID, retrievedValue, "expected session variable to match")
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
			wantVer:   MIGRATION_CURRENT_VERSION,
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
			version, dirty, err := db.GetMigrationVersion(s.pg.SuperDSN(), tt.path)
			if tt.wantErr {
				s.Error(err)
				return
			}

			s.Require().NoError(err)
			s.Equalf(tt.wantVer, version, "expected version %d", tt.wantVer)
			s.Equalf(tt.wantDirty, dirty, "expected dirty=%v", tt.wantDirty)
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
			err := db.RollbackLastMigration(s.pg.SuperDSN(), tt.path)
			if tt.wantErr {
				s.Error(err)
				return
			}

			s.Require().NoError(err)

			version, _, err := db.GetMigrationVersion(s.pg.SuperDSN(), MIGRATIONS_PATH)
			s.Require().NoError(err)
			s.Equalf(uint(MIGRATION_CURRENT_VERSION-1), version, "expected version %d after rolling back one step", MIGRATION_CURRENT_VERSION-1)

			s.Require().NoError(db.RunMigrations(s.pg.SuperDSN(), MIGRATIONS_PATH))
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
			err := db.RollbackAllMigrations(s.pg.SuperDSN(), tt.path)
			if tt.wantErr {
				s.Error(err)
				return
			}

			s.Require().NoError(err)

			_, _, err = db.GetMigrationVersion(s.pg.SuperDSN(), MIGRATIONS_PATH)
			s.Require().Error(err, "expected error after rolling back all migrations")

			s.Require().NoError(db.RunMigrations(s.pg.SuperDSN(), MIGRATIONS_PATH))

			version, dirty, err := db.GetMigrationVersion(s.pg.SuperDSN(), MIGRATIONS_PATH)
			s.Require().NoError(err)
			s.Equalf(uint(MIGRATION_CURRENT_VERSION), version, "expected version restored to %d", MIGRATION_CURRENT_VERSION)
			s.False(dirty, "expected migration state not dirty")
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
			err := db.ForceMigrationVersion(s.pg.SuperDSN(), tt.path, tt.version)
			if tt.wantErr {
				s.Error(err)
				return
			}

			s.Require().NoError(err)

			version, dirty, err := db.GetMigrationVersion(s.pg.SuperDSN(), MIGRATIONS_PATH)
			s.Require().NoError(err)
			s.Equalf(uint(tt.version), version, "expected version forced to %d", tt.version)
			s.False(dirty, "expected migration state not dirty after Force")

			s.Require().NoError(db.ForceMigrationVersion(s.pg.SuperDSN(), MIGRATIONS_PATH, MIGRATION_CURRENT_VERSION))

			version, dirty, err = db.GetMigrationVersion(s.pg.SuperDSN(), MIGRATIONS_PATH)
			s.Require().NoError(err)
			s.Equalf(uint(MIGRATION_CURRENT_VERSION), version, "expected version forced back to %d", MIGRATION_CURRENT_VERSION)
			s.False(dirty, "expected migration state not dirty after Force")
		})
	}
}

func (s *MigrationsTestSuite) TestEnsureAppLogin() {
	s.Require().NoError(db.EnsureAppLogin(s.pg.SuperDSN(), "vietgara_local", "pa'ss"))
	s.Require().NoError(db.EnsureAppLogin(s.pg.SuperDSN(), "vietgara_local", "rotated"), "idempotent")

	var bypass, member bool

	err := s.pg.Admin.QueryRowContext(s.ctx, `
		SELECT r.rolbypassrls, pg_has_role(r.oid, 'vietgara_app', 'member')
		FROM pg_roles r WHERE r.rolname = 'vietgara_local'`).Scan(&bypass, &member)
	s.Require().NoError(err)
	s.False(bypass)
	s.True(member)

	s.Require().Error(db.EnsureAppLogin(s.pg.SuperDSN(), "Bad-Name", "x"))
	s.Require().Error(db.EnsureAppLogin("postgres://nobody@127.0.0.1:1/x?sslmode=disable&connect_timeout=1", "ok_name", "x"))
}

func TestMigrationsSuite(t *testing.T) {
	suite.Run(t, new(MigrationsTestSuite))
}
