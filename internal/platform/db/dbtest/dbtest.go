// Package dbtest fakes the database for repository unit tests: a scripted
// transaction put in the context (see db.QuerierFromContext) answers each
// query with the next expectation, checking the SQL it receives.
//
//	tx := dbtest.New(t).
//		ExpectQueryRow("INSERT INTO users", dbtest.Row("id-1", now, now)).
//		ExpectExec("DELETE FROM roles", dbtest.Tag(1), nil)
//	ctx := tx.Context(t.Context())
package dbtest

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
)

type kind int

var (
	errDBTestValueCount     = errors.New("dbtest")
	errDBTestScanType       = errors.New("dbtest: cannot scan")
	errDBTestScanOutsideRow = errors.New("dbtest: Scan outside a row")
)

const (
	kindExec kind = iota
	kindQuery
	kindQueryRow
)

func (k kind) String() string {
	return [...]string{"Exec", "Query", "QueryRow"}[k]
}

type expectation struct {
	kind     kind
	fragment string
	row      pgx.Row
	rows     pgx.Rows
	tag      pgconn.CommandTag
	err      error
	check    func(args []any)
}

// Tx is a scripted pgx.Tx; only Exec, Query and QueryRow are implemented.
type Tx struct {
	pgx.Tx

	t            testing.TB
	expectations []*expectation
}

// New returns an empty script; unmet expectations fail t at cleanup.
func New(tb testing.TB) *Tx {
	tb.Helper()

	tx := &Tx{t: tb}
	tb.Cleanup(func() {
		if len(tx.expectations) > 0 {
			tb.Errorf("dbtest: %d unmet expectation(s), next: %s %q", len(tx.expectations), tx.expectations[0].kind, tx.expectations[0].fragment)
		}
	})

	return tx
}

// Context returns ctx carrying the fake transaction.
func (tx *Tx) Context(ctx context.Context) context.Context {
	return context.WithValue(ctx, platformdb.TxKey{}, pgx.Tx(tx))
}

// ExpectExec scripts an Exec whose SQL contains fragment.
func (tx *Tx) ExpectExec(fragment string, tag pgconn.CommandTag, err error) *Tx {
	tx.expectations = append(tx.expectations, &expectation{kind: kindExec, fragment: fragment, tag: tag, err: err})

	return tx
}

// ExpectQuery scripts a Query whose SQL contains fragment.
func (tx *Tx) ExpectQuery(fragment string, rows pgx.Rows, err error) *Tx {
	tx.expectations = append(tx.expectations, &expectation{kind: kindQuery, fragment: fragment, rows: rows, err: err})

	return tx
}

// ExpectQueryRow scripts a QueryRow whose SQL contains fragment.
func (tx *Tx) ExpectQueryRow(fragment string, row pgx.Row) *Tx {
	tx.expectations = append(tx.expectations, &expectation{kind: kindQueryRow, fragment: fragment, row: row})

	return tx
}

// WithArgs checks the arguments of the last scripted call.
func (tx *Tx) WithArgs(check func(args []any)) *Tx {
	tx.expectations[len(tx.expectations)-1].check = check

	return tx
}

func (tx *Tx) next(k kind, sql string, args []any) *expectation {
	tx.t.Helper()

	if len(tx.expectations) == 0 {
		tx.t.Fatalf("dbtest: unexpected %s: %s", k, sql)
	}

	e := tx.expectations[0]
	tx.expectations = tx.expectations[1:]

	if e.kind != k || !strings.Contains(sql, e.fragment) {
		tx.t.Fatalf("dbtest: expected %s containing %q, got %s: %s", e.kind, e.fragment, k, sql)
	}

	if e.check != nil {
		e.check(args)
	}

	return e
}

// Exec implements pgx.Tx.
func (tx *Tx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	e := tx.next(kindExec, sql, args)

	return e.tag, e.err
}

// Query implements pgx.Tx.
func (tx *Tx) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	e := tx.next(kindQuery, sql, args)
	if e.err != nil {
		return nil, e.err
	}

	return e.rows, nil
}

// QueryRow implements pgx.Tx.
func (tx *Tx) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	return tx.next(kindQueryRow, sql, args).row
}

// Tag returns a command tag affecting n rows.
func Tag(n int64) pgconn.CommandTag {
	return pgconn.NewCommandTag(fmt.Sprintf("UPDATE %d", n))
}

type row struct {
	values []any
	err    error
}

// Row returns a row scanning values into the destinations; a nil value
// sets the destination to its zero value, a value of type T fills a *T or
// **T destination.
func Row(values ...any) pgx.Row { return row{values: values} }

// ErrRow returns a row whose Scan fails with err (pgx.ErrNoRows for "no
// row").
func ErrRow(err error) pgx.Row { return row{err: err} }

func (r row) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}

	return assign(r.values, dest)
}

func assign(values, dest []any) error {
	if len(values) != len(dest) {
		return fmt.Errorf("%w: %d values for %d destinations", errDBTestValueCount, len(values), len(dest))
	}

	for i, value := range values {
		dv := reflect.ValueOf(dest[i]).Elem()
		if value == nil {
			dv.Set(reflect.Zero(dv.Type()))

			continue
		}

		vv := reflect.ValueOf(value)

		switch {
		case vv.Type().AssignableTo(dv.Type()):
			dv.Set(vv)
		case dv.Kind() == reflect.Pointer && vv.Type().AssignableTo(dv.Type().Elem()):
			p := reflect.New(dv.Type().Elem())
			p.Elem().Set(vv)
			dv.Set(p)
		case vv.Type().ConvertibleTo(dv.Type()):
			dv.Set(vv.Convert(dv.Type()))
		default:
			return fmt.Errorf("%w %T into %s (column %d)", errDBTestScanType, value, dv.Type(), i)
		}
	}

	return nil
}

// Rows is a scripted pgx.Rows.
type Rows struct {
	pgx.Rows

	data    [][]any
	idx     int
	scanErr error
	err     error
}

// NewRows returns rows yielding each value list in turn.
func NewRows(data ...[]any) *Rows { return &Rows{data: data, idx: -1} }

// WithScanError makes every Scan fail.
func (r *Rows) WithScanError(err error) *Rows { r.scanErr = err; return r }

// WithErr makes Err report err after iteration.
func (r *Rows) WithErr(err error) *Rows { r.err = err; return r }

// Next implements pgx.Rows.
func (r *Rows) Next() bool {
	r.idx++

	return r.idx < len(r.data)
}

// Scan implements pgx.Rows.
func (r *Rows) Scan(dest ...any) error {
	if r.scanErr != nil {
		return r.scanErr
	}

	if r.idx < 0 || r.idx >= len(r.data) {
		return errDBTestScanOutsideRow
	}

	return assign(r.data[r.idx], dest)
}

// Close implements pgx.Rows.
func (r *Rows) Close() {}

// Err implements pgx.Rows.
func (r *Rows) Err() error { return r.err }

// UniqueViolation is a Postgres unique_violation on constraint.
func UniqueViolation(constraint string) error {
	return &pgconn.PgError{Code: "23505", ConstraintName: constraint}
}

// ForeignKeyViolation is a Postgres foreign_key_violation.
func ForeignKeyViolation() error {
	return &pgconn.PgError{Code: "23503"}
}
