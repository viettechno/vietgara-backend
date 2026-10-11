// Package repository implements the persistence layer of the Customer &
// Vehicle module on top of the shared Postgres pool. Every query filters on
// garage_id explicitly on top of the Row-Level Security policies and skips
// soft-deleted rows.
// See: Database Design Section 3, LLD Section 2.
package repository

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/viettechno/vietgara-backend/internal/customer/domain"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/pkg/phone"
)

// Postgres error codes the repositories translate into domain errors.
const (
	pgUniqueViolation     = "23505"
	pgForeignKeyViolation = "23503"
)

var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// containsPattern turns free text into an ILIKE "contains" pattern, or ""
// when there is nothing to match (the queries treat "" as "no filter").
func containsPattern(query string) string {
	if query == "" {
		return ""
	}

	return "%" + likeEscaper.Replace(query) + "%"
}

// compactPattern is containsPattern over the query's letters and digits
// only, for columns stored without separators (plate keys, phone numbers).
func compactPattern(query string) string {
	return containsPattern(domain.LicensePlateKey(query))
}

// phonePattern is containsPattern over the digits to look for inside stored
// E.164 numbers, so "0912 345" finds "+84912345678".
func phonePattern(query string) string {
	return containsPattern(phone.SearchKey(query))
}

// isUUID reports whether id can be compared with a uuid column; anything
// else cannot match a row, so lookups short-circuit to "not found" instead
// of failing with a Postgres cast error.
func isUUID(id string) bool {
	return uuid.Validate(id) == nil
}

// nullable maps an empty string to NULL.
func nullable(s string) *string {
	if s == "" {
		return nil
	}

	return &s
}

// translate maps a unique violation on one of the given indexes, and any
// foreign-key violation, to the matching domain error.
func translate(err error, uniqueIndexes map[string]error, fkErr error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}

	switch pgErr.Code {
	case pgUniqueViolation:
		if mapped, ok := uniqueIndexes[pgErr.ConstraintName]; ok {
			return mapped
		}
	case pgForeignKeyViolation:
		if fkErr != nil {
			return fkErr
		}
	}

	return err
}

// scanWrite scans the RETURNING columns of an INSERT/UPDATE into dest,
// mapping "no row" to notFound and constraint violations via translate.
func scanWrite(row pgx.Row, notFound error, uniqueIndexes map[string]error, fkErr error, dest ...any) error {
	err := row.Scan(dest...)
	if errors.Is(err, pgx.ErrNoRows) {
		return notFound
	}

	return translate(err, uniqueIndexes, fkErr)
}

// findOne runs a single-row query and maps pgx.ErrNoRows to notFound.
func findOne[T any](ctx context.Context, q platformdb.Querier, notFound error, scan func(pgx.Row) (T, error), sql string, args ...any) (T, error) {
	item, err := scan(q.QueryRow(ctx, sql, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return item, notFound
	}

	return item, err
}

// listPage counts the rows matching countSQL, then fetches one page with
// listSQL, which takes the same args followed by LIMIT and OFFSET.
func listPage[T any](
	ctx context.Context, q platformdb.Querier, scan func(pgx.Row) (T, error),
	countSQL, listSQL string, args []any, limit, offset int,
) ([]T, int, error) {
	var total int
	if err := q.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	if total == 0 {
		return nil, 0, nil
	}

	rows, err := q.Query(ctx, listSQL, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]T, 0, limit)

	for rows.Next() {
		item, err := scan(rows)
		if err != nil {
			return nil, 0, err
		}

		items = append(items, item)
	}

	return items, total, rows.Err()
}

// execAffectingOne runs a write that must hit exactly one live row,
// returning notFound when it hit none.
func execAffectingOne(ctx context.Context, q platformdb.Querier, notFound error, sql string, args ...any) error {
	tag, err := q.Exec(ctx, sql, args...)
	if err != nil {
		return err
	}

	if tag.RowsAffected() == 0 {
		return notFound
	}

	return nil
}
