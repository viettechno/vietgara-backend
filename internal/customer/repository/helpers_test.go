package repository_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/mock"

	"github.com/viettechno/vietgara-backend/internal/mocks"
	dbpkg "github.com/viettechno/vietgara-backend/internal/platform/db"
)

const (
	garageID   = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	customerID = "11111111-1111-1111-1111-111111111111"
	vehicleID  = "22222222-2222-2222-2222-222222222222"
	partnerID  = "44444444-4444-4444-4444-444444444444"
	actorID    = "55555555-5555-5555-5555-555555555555"
)

var (
	testTime = time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	errBoom  = errors.New("boom")
)

// assignScanValues copies values[i] into the i-th Scan destination, leaving
// nil values untouched so pointer fields stay nil.
func assignScanValues(dest, values []any) error {
	if len(dest) != len(values) {
		return errors.New("scan: column count mismatch")
	}

	for i := range dest {
		if values[i] == nil {
			continue
		}

		dv := reflect.ValueOf(dest[i]).Elem()
		sv := reflect.ValueOf(values[i])

		if !sv.Type().AssignableTo(dv.Type()) {
			return errors.New("scan: value type mismatch")
		}

		dv.Set(sv)
	}

	return nil
}

func errRow(t *testing.T, err error) *mocks.MockRow {
	t.Helper()

	row := mocks.NewMockRow(t)
	row.EXPECT().Scan(mock.Anything).Return(err)

	return row
}

func valuesRow(t *testing.T, values ...any) *mocks.MockRow {
	t.Helper()

	row := mocks.NewMockRow(t)
	row.EXPECT().Scan(mock.Anything).RunAndReturn(func(dest ...any) error {
		return assignScanValues(dest, values)
	})

	return row
}

// resultRows yields data row by row; a row whose first value is an error
// makes Scan fail with it.
func resultRows(t *testing.T, data ...[]any) *mocks.MockRows {
	t.Helper()

	rows := mocks.NewMockRows(t)
	rows.EXPECT().Close().Return()
	rows.EXPECT().Err().Return(nil).Maybe()

	idx := 0

	rows.EXPECT().Next().RunAndReturn(func() bool { return idx < len(data) }).Maybe()
	rows.EXPECT().Scan(mock.Anything).RunAndReturn(func(dest ...any) error {
		values := data[idx]
		idx++

		if err, ok := values[0].(error); ok {
			return err
		}

		return assignScanValues(dest, values)
	}).Maybe()

	return rows
}

func sqlContaining(fragment string) any {
	return mock.MatchedBy(func(sql string) bool { return strings.Contains(sql, fragment) })
}

func newPool(t *testing.T) (*mocks.MockConnPool, *dbpkg.Pool) {
	t.Helper()

	conn := mocks.NewMockConnPool(t)

	return conn, dbpkg.NewPool(t.Context(), conn)
}

func uniqueViolation(index string) error {
	return &pgconn.PgError{Code: "23505", ConstraintName: index}
}

func foreignKeyViolation() error {
	return &pgconn.PgError{Code: "23503", ConstraintName: "vehicles_garage_customer_fkey"}
}

func commandTag(affected string) pgconn.CommandTag {
	return pgconn.NewCommandTag("UPDATE " + affected)
}
