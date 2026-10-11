package db_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	dbpkg "github.com/viettechno/vietgara-backend/db"
)

const rlsTestDriverName = "rls_test_driver"

var (
	rlsTestDriverOnce         sync.Once
	rlsTestStates             = map[string]*rlsTestState{}
	rlsTestStatesMu           sync.Mutex
	rlsTestSequence           int
	errRLSCallbackFailed      = errors.New("callback failed")
	errRLSCallbackInvalidArgs = errors.New("callback received invalid arguments")
	errRLSDatabaseUnavailable = errors.New("database unavailable")
	errRLSCallbackMustNotRun  = errors.New("callback must not run")
	errRLSQueryFailed         = errors.New("query failed")
	errRLSWriteFailed         = errors.New("write failed")
	errRLSResultUnavailable   = errors.New("result unavailable")
	errRLSConstraintViolation = errors.New("constraint violation")
	errUnknownTestDatabase    = errors.New("unknown test database")
	errRLSPreparedStatements  = errors.New("prepared statements are not supported")
	errRLSTransactions        = errors.New("transactions are not supported")
	errRLSNoRowsDefinition    = errors.New("query returned no rows definition")
	errRLSUnexpectedQuery     = errors.New("unexpected")
	errRLSGot                 = errors.New("got")
	errRLSQueryMismatch       = errors.New("query")
	errRLSArgumentMismatch    = errors.New("argument")
)

type rlsTestStep struct {
	kind     string
	contains string
	args     []driver.Value
	result   driver.Result
	rows     *rlsTestRows
	err      error
}

type rlsTestState struct {
	mu    sync.Mutex
	steps []rlsTestStep
}

func newRLSTestDB(t *testing.T, steps ...rlsTestStep) *sql.DB {
	t.Helper()

	rlsTestDriverOnce.Do(func() { sql.Register(rlsTestDriverName, rlsTestDriver{}) })
	rlsTestStatesMu.Lock()
	rlsTestSequence++
	dsn := fmt.Sprintf("rls-test-%d", rlsTestSequence)
	state := &rlsTestState{steps: steps}
	rlsTestStates[dsn] = state
	rlsTestStatesMu.Unlock()

	db, err := sql.Open(rlsTestDriverName, dsn)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}

	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close test database: %v", err)
		}

		state.mu.Lock()
		remaining := len(state.steps)
		state.mu.Unlock()

		if remaining != 0 {
			t.Errorf("%d expected database operations were not performed", remaining)
		}

		rlsTestStatesMu.Lock()
		delete(rlsTestStates, dsn)
		rlsTestStatesMu.Unlock()
	})

	return db
}

func rlsSessionStep(garageID string) rlsTestStep {
	return rlsTestStep{
		kind:     "exec",
		contains: "set_config('app.current_garage_id'",
		args:     []driver.Value{garageID},
		result:   driver.RowsAffected(1),
	}
}

func TestNewRLSContext(t *testing.T) {
	db := newRLSTestDB(t)
	ctx := dbpkg.NewRLSContext(db, "garage-1")

	if ctx.DB != db {
		t.Error("DB was not retained")
	}

	if ctx.GarageID != "garage-1" {
		t.Errorf("GarageID = %q, want garage-1", ctx.GarageID)
	}
}

func TestRLSContext_WithRLSSession(t *testing.T) {
	callbackErr := errRLSCallbackFailed
	tests := []struct {
		name     string
		steps    []rlsTestStep
		callback func(context.Context, *sql.DB) error
		wantErr  string
	}{
		{
			name:  "sets session before invoking callback",
			steps: []rlsTestStep{rlsSessionStep("garage-1")},
			callback: func(ctx context.Context, db *sql.DB) error {
				if ctx == nil || db == nil {
					return errRLSCallbackInvalidArgs
				}

				return nil
			},
		},
		{
			name: "wraps session setup error and skips callback",
			steps: []rlsTestStep{{
				kind: "exec", contains: "set_config", args: []driver.Value{"garage-1"}, err: errRLSDatabaseUnavailable,
			}},
			callback: func(context.Context, *sql.DB) error {
				return errRLSCallbackMustNotRun
			},
			wantErr: "failed to set RLS session variable: database unavailable",
		},
		{
			name:     "returns callback error unchanged",
			steps:    []rlsTestStep{rlsSessionStep("garage-1")},
			callback: func(context.Context, *sql.DB) error { return callbackErr },
			wantErr:  "callback failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rc := dbpkg.NewRLSContext(newRLSTestDB(t, tt.steps...), "garage-1")

			err := rc.WithRLSSession(t.Context(), tt.callback)
			if tt.wantErr == "" && err != nil {
				t.Fatalf("WithRLSSession() error = %v", err)
			}

			if tt.wantErr != "" && (err == nil || err.Error() != tt.wantErr) {
				t.Fatalf("WithRLSSession() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestRLSContext_GetGarage(t *testing.T) {
	garageRows := &rlsTestRows{columns: []string{"id", "user_id", "name", "timezone", "currency", "status"}, values: [][]driver.Value{{"garage-1", "user-1", "Main Garage", "Asia/Ho_Chi_Minh", "VND", "active"}}}

	tests := []struct {
		name    string
		steps   []rlsTestStep
		want    map[string]any
		wantErr string
	}{
		{name: "returns garage", steps: []rlsTestStep{rlsSessionStep("garage-1"), {kind: "query", contains: "FROM garages", rows: garageRows}}, want: map[string]any{"id": "garage-1", "user_id": "user-1", "name": "Main Garage", "timezone": "Asia/Ho_Chi_Minh", "currency": "VND", "status": "active"}},
		{name: "reports missing garage", steps: []rlsTestStep{rlsSessionStep("garage-1"), {kind: "query", contains: "FROM garages", rows: &rlsTestRows{columns: garageRows.columns}}}, wantErr: "garage not found"},
		{name: "wraps query error", steps: []rlsTestStep{rlsSessionStep("garage-1"), {kind: "query", contains: "FROM garages", err: errRLSQueryFailed}}, wantErr: "query failed: query failed"},
		{name: "wraps scan error", steps: []rlsTestStep{rlsSessionStep("garage-1"), {kind: "query", contains: "FROM garages", rows: &rlsTestRows{columns: garageRows.columns, values: [][]driver.Value{{"only-one-column"}}}}}, wantErr: "scan failed:"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := dbpkg.NewRLSContext(newRLSTestDB(t, tt.steps...), "garage-1").GetGarage(t.Context())
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("GetGarage() error = %v, want containing %q", err, tt.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatalf("GetGarage() error = %v", err)
			}

			for key, want := range tt.want {
				if got[key] != want {
					t.Errorf("GetGarage()[%q] = %v, want %v", key, got[key], want)
				}
			}
		})
	}
}

func TestRLSContext_GetGarageStaff(t *testing.T) {
	columns := []string{"id", "garage_id", "user_id", "role", "status", "created_at"}

	tests := []struct {
		name    string
		steps   []rlsTestStep
		wantLen int
		wantErr string
	}{
		{name: "returns all staff", steps: []rlsTestStep{rlsSessionStep("garage-1"), {kind: "query", contains: "FROM garage_staff", rows: &rlsTestRows{columns: columns, values: [][]driver.Value{{"staff-1", "garage-1", "user-1", "owner", "active", "2026-01-01"}, {"staff-2", "garage-1", "user-2", "mechanic", "active", "2026-01-02"}}}}}, wantLen: 2},
		{name: "returns empty staff", steps: []rlsTestStep{rlsSessionStep("garage-1"), {kind: "query", contains: "FROM garage_staff", rows: &rlsTestRows{columns: columns}}}},
		{name: "wraps query error", steps: []rlsTestStep{rlsSessionStep("garage-1"), {kind: "query", contains: "FROM garage_staff", err: errRLSQueryFailed}}, wantErr: "query failed: query failed"},
		{name: "wraps scan error", steps: []rlsTestStep{rlsSessionStep("garage-1"), {kind: "query", contains: "FROM garage_staff", rows: &rlsTestRows{columns: columns, values: [][]driver.Value{{"only-one-column"}}}}}, wantErr: "scan failed:"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := dbpkg.NewRLSContext(newRLSTestDB(t, tt.steps...), "garage-1").GetGarageStaff(t.Context())
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("GetGarageStaff() error = %v, want containing %q", err, tt.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatalf("GetGarageStaff() error = %v", err)
			}

			if len(got) != tt.wantLen {
				t.Errorf("len(GetGarageStaff()) = %d, want %d", len(got), tt.wantLen)
			}

			if tt.wantLen > 0 && got[0]["role"] != "owner" {
				t.Errorf("first staff role = %v, want owner", got[0]["role"])
			}
		})
	}
}

func TestRLSContext_UpdateGarageStatus(t *testing.T) {
	tests := []struct {
		name    string
		steps   []rlsTestStep
		wantErr string
	}{
		{name: "updates garage", steps: []rlsTestStep{rlsSessionStep("garage-1"), {kind: "exec", contains: "UPDATE garages", args: []driver.Value{"inactive", "garage-1"}, result: driver.RowsAffected(1)}}},
		{name: "wraps update error", steps: []rlsTestStep{rlsSessionStep("garage-1"), {kind: "exec", contains: "UPDATE garages", err: errRLSWriteFailed}}, wantErr: "update failed: write failed"},
		{name: "reports no updated garage", steps: []rlsTestStep{rlsSessionStep("garage-1"), {kind: "exec", contains: "UPDATE garages", result: driver.RowsAffected(0)}}, wantErr: "no garage updated"},
		{name: "wraps rows affected error", steps: []rlsTestStep{rlsSessionStep("garage-1"), {kind: "exec", contains: "UPDATE garages", result: rlsTestResult{err: errRLSResultUnavailable}}}, wantErr: "failed to get rows affected: result unavailable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := dbpkg.NewRLSContext(newRLSTestDB(t, tt.steps...), "garage-1").UpdateGarageStatus(t.Context(), "inactive")
			if tt.wantErr == "" && err != nil {
				t.Fatalf("UpdateGarageStatus() error = %v", err)
			}

			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("UpdateGarageStatus() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestRLSContext_AssignStaffToGarage(t *testing.T) {
	tests := []struct {
		name    string
		steps   []rlsTestStep
		wantErr string
	}{
		{name: "assigns staff", steps: []rlsTestStep{rlsSessionStep("garage-1"), {kind: "exec", contains: "INSERT INTO garage_staff", args: []driver.Value{"garage-1", "user-1", "mechanic"}, result: driver.RowsAffected(1)}}},
		{name: "wraps insert error", steps: []rlsTestStep{rlsSessionStep("garage-1"), {kind: "exec", contains: "INSERT INTO garage_staff", err: errRLSConstraintViolation}}, wantErr: "insert/update failed: constraint violation"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := dbpkg.NewRLSContext(newRLSTestDB(t, tt.steps...), "garage-1").AssignStaffToGarage(t.Context(), "user-1", "mechanic")
			if tt.wantErr == "" && err != nil {
				t.Fatalf("AssignStaffToGarage() error = %v", err)
			}

			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("AssignStaffToGarage() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestRLSContext_RemoveStaffFromGarage(t *testing.T) {
	tests := []struct {
		name    string
		steps   []rlsTestStep
		wantErr string
	}{
		{name: "removes staff", steps: []rlsTestStep{rlsSessionStep("garage-1"), {kind: "exec", contains: "UPDATE garage_staff", args: []driver.Value{"garage-1", "user-1"}, result: driver.RowsAffected(1)}}},
		{name: "wraps update error", steps: []rlsTestStep{rlsSessionStep("garage-1"), {kind: "exec", contains: "UPDATE garage_staff", err: errRLSWriteFailed}}, wantErr: "update failed: write failed"},
		{name: "reports missing staff", steps: []rlsTestStep{rlsSessionStep("garage-1"), {kind: "exec", contains: "UPDATE garage_staff", result: driver.RowsAffected(0)}}, wantErr: "staff member not found"},
		{name: "wraps rows affected error", steps: []rlsTestStep{rlsSessionStep("garage-1"), {kind: "exec", contains: "UPDATE garage_staff", result: rlsTestResult{err: errRLSResultUnavailable}}}, wantErr: "failed to get rows affected: result unavailable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := dbpkg.NewRLSContext(newRLSTestDB(t, tt.steps...), "garage-1").RemoveStaffFromGarage(t.Context(), "user-1")
			if tt.wantErr == "" && err != nil {
				t.Fatalf("RemoveStaffFromGarage() error = %v", err)
			}

			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("RemoveStaffFromGarage() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

type rlsTestDriver struct{}

func (rlsTestDriver) Open(name string) (driver.Conn, error) {
	rlsTestStatesMu.Lock()
	state := rlsTestStates[name]
	rlsTestStatesMu.Unlock()

	if state == nil {
		return nil, fmt.Errorf("%w %q", errUnknownTestDatabase, name)
	}

	return rlsTestConn{state: state}, nil
}

type rlsTestConn struct{ state *rlsTestState }

func (rlsTestConn) Prepare(string) (driver.Stmt, error) {
	return nil, errRLSPreparedStatements
}
func (rlsTestConn) Close() error { return nil }
func (rlsTestConn) Begin() (driver.Tx, error) {
	return nil, errRLSTransactions
}

func (c rlsTestConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	step, err := c.state.next("exec", query, args)
	if err != nil {
		return nil, err
	}

	if step.err != nil {
		return nil, step.err
	}

	if step.result == nil {
		return driver.RowsAffected(1), nil
	}

	return step.result, nil
}

func (c rlsTestConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	step, err := c.state.next("query", query, args)
	if err != nil {
		return nil, err
	}

	if step.err != nil {
		return nil, step.err
	}

	if step.rows == nil {
		return nil, errRLSNoRowsDefinition
	}

	return step.rows.clone(), nil
}

func (s *rlsTestState) next(kind, query string, args []driver.NamedValue) (rlsTestStep, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.steps) == 0 {
		return rlsTestStep{}, fmt.Errorf("%w %s query: %s", errRLSUnexpectedQuery, kind, query)
	}

	step := s.steps[0]
	s.steps = s.steps[1:]

	if step.kind != kind {
		return rlsTestStep{}, fmt.Errorf("%w %s query, want %s", errRLSGot, kind, step.kind)
	}

	if !strings.Contains(query, step.contains) {
		return rlsTestStep{}, fmt.Errorf("%w %q does not contain %q", errRLSQueryMismatch, query, step.contains)
	}

	if step.args != nil {
		if len(args) != len(step.args) {
			return rlsTestStep{}, fmt.Errorf("%w %d arguments, want %d", errRLSGot, len(args), len(step.args))
		}

		for i, arg := range args {
			if arg.Value != step.args[i] {
				return rlsTestStep{}, fmt.Errorf("%w %d = %v, want %v", errRLSArgumentMismatch, i, arg.Value, step.args[i])
			}
		}
	}

	return step, nil
}

type rlsTestRows struct {
	columns []string
	values  [][]driver.Value
	index   int
}

func (r *rlsTestRows) Columns() []string { return r.columns }
func (r *rlsTestRows) Close() error      { return nil }
func (r *rlsTestRows) Next(dest []driver.Value) error {
	if r.index == len(r.values) {
		return io.EOF
	}

	copy(dest, r.values[r.index])
	r.index++

	return nil
}
func (r *rlsTestRows) clone() *rlsTestRows { return &rlsTestRows{columns: r.columns, values: r.values} }

type rlsTestResult struct{ err error }

func (r rlsTestResult) LastInsertId() (int64, error) { return 0, r.err }
func (r rlsTestResult) RowsAffected() (int64, error) { return 0, r.err }
