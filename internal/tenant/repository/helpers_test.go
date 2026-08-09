package repository_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	dbpkg "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platform/db/mocks"
)

// ---------------------------------------------------------------------------
// Helpers driving the generated pgx.Row / pgx.Rows mocks
// ---------------------------------------------------------------------------

// assignScanValues copies values[i] into the i-th Scan destination, leaving
// nil values untouched so pointer fields stay nil. It is what the mock
// Run-closures use to emulate a real pgx row.
func assignScanValues(dest, values []any) error {
	for i := range dest {
		value := values[i]

		if value == nil {
			continue
		}

		dv := reflect.ValueOf(dest[i]).Elem()
		sv := reflect.ValueOf(value)

		if !sv.Type().AssignableTo(dv.Type()) {
			return errors.New("scan: value type mismatch")
		}

		dv.Set(sv)
	}

	return nil
}

// errRow is a shortcut for a row whose Scan always fails (e.g. pgx.ErrNoRows).
func errRow(t *testing.T, err error) *mocks.MockRow {
	t.Helper()

	row := mocks.NewMockRow(t)
	row.EXPECT().Scan(mock.Anything).Return(err)

	return row
}

// valuesRow is a row whose Scan copies the given values into the caller's
// destinations (like a single real pgx result row).
func valuesRow(t *testing.T, values []any) *mocks.MockRow {
	t.Helper()

	row := mocks.NewMockRow(t)
	row.EXPECT().Scan(mock.Anything).Run(func(dest ...any) {
		require.NoError(t, assignScanValues(dest, values))
	}).Return(nil)

	return row
}

// rowsState drives a MockRows mock over a fixed slice of rows, with an
// optional Scan failure at a chosen row index (scanAt, default -1 = never).
type rowsState struct {
	data    [][]any
	scanAt  int
	scanErr error
}

func newRowsState(data [][]any) *rowsState {
	return &rowsState{data: data, scanAt: -1}
}

// resultRows builds a pgx.Rows mock that yields the rows in rowsState one by
// one, then signals EOF. Close is asserted strictly; Scan/Err are optional,
// because the repository skips Scan for an empty result and skips Err when a
// row scan aborts mid-iteration.
func resultRows(t *testing.T, st *rowsState) *mocks.MockRows {
	t.Helper()

	rows := mocks.NewMockRows(t)
	rows.EXPECT().Close().Return()
	rows.EXPECT().Err().Return(nil).Maybe()

	idx := 0

	rows.EXPECT().Next().RunAndReturn(func() bool { return idx < len(st.data) })
	rows.EXPECT().Scan(mock.Anything).RunAndReturn(func(dest ...any) error {
		if st.scanErr != nil && idx == st.scanAt {
			return st.scanErr
		}

		// data[idx] is only indexed after Next reported a row.
		if err := assignScanValues(dest, st.data[idx]); err != nil {
			return err
		}

		idx++

		return nil
	}).Maybe()

	return rows
}

// ---------------------------------------------------------------------------
// Unit helpers
// ---------------------------------------------------------------------------

var testTimestamp = time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)

func garageScanValues(id, accountID, name, status string) []any {
	return []any{
		id, accountID, name, nil, nil, nil, "Asia/Ho_Chi_Minh", "VND", status,
		testTimestamp, testTimestamp,
	}
}

func staffScanValues(id, garageID, accountID, role, status string) []any {
	return []any{id, garageID, accountID, role, status, testTimestamp, testTimestamp}
}

func sqlContaining(fragment string) any {
	return mock.MatchedBy(func(sql string) bool {
		return strings.Contains(sql, fragment)
	})
}

func newQuerier(t *testing.T) (*mocks.MockConnPool, *dbpkg.Pool) {
	t.Helper()

	conn := mocks.NewMockConnPool(t)
	pool := dbpkg.NewPool(t.Context(), conn)

	return conn, pool
}
