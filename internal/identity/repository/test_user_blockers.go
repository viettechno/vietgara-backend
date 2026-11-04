package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
)

// Raised when a row is deleted while another row still refers to it through a
// foreign key: restrict_violation (ON DELETE RESTRICT) or foreign_key_violation
// (the default, NO ACTION).
const (
	pgRestrictViolation   = "23001"
	pgForeignKeyViolation = "23503"
)

// maxBlockersPerUser bounds the tables cleared for one user: each round clears
// a different one.
const maxBlockersPerUser = 100

// maxBlockerDepth bounds how far one blocker is followed: clearing a table can
// meet a table that refers to it.
const maxBlockerDepth = 10

var errTestUserBlocked = errors.New("the rows that block it could not be cleared")

// deleteTestUser deletes one user and, by cascade, what it owns. Deleting a
// garage cascades to dozens of tables, and a child whose foreign key is
// ON DELETE RESTRICT (a part's unit) or has no ON DELETE (the lines of a stock
// count) makes Postgres refuse the cascade when the parent row happens to be
// deleted first: each cascaded delete is checked when it ends, not when the
// whole statement does. When that happens the rows that refer to the parent
// are deleted from the user's garages and the deletion is tried again.
func (r *PostgresUserRepository) deleteTestUser(ctx context.Context, userID string) error {
	q := platformdb.QuerierFromContext(ctx, r.pool)

	for range maxBlockersPerUser {
		_, err := q.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID)
		if err == nil {
			return nil
		}

		pgErr := foreignKeyViolation(err)
		if pgErr == nil {
			return err
		}

		removed, clearErr := r.clearBlockers(ctx, userID, pgErr)
		if clearErr != nil {
			return fmt.Errorf("%w (clearing what blocks it: %w)", err, clearErr)
		}

		if removed == 0 {
			return fmt.Errorf("%w: %w", errTestUserBlocked, err)
		}
	}

	return errTestUserBlocked
}

// foreignKeyViolation returns err as the Postgres error of a refused delete
// (restrict_violation or foreign_key_violation), or nil.
func foreignKeyViolation(err error) *pgconn.PgError {
	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	if !ok || (pgErr.Code != pgRestrictViolation && pgErr.Code != pgForeignKeyViolation) {
		return nil
	}

	return pgErr
}

// clearBlockers deletes, in each garage of the user, the rows of the table that
// holds the foreign key pgErr names (the table that refers to the row being
// deleted). It returns how many it deleted.
func (r *PostgresUserRepository) clearBlockers(ctx context.Context, userID string, pgErr *pgconn.PgError) (int64, error) {
	garageIDs, err := r.garageIDsOf(ctx, userID)
	if err != nil {
		return 0, err
	}

	var removed int64

	for _, garageID := range garageIDs {
		n, err := r.clearTable(ctx, garageID, pgx.Identifier{pgErr.SchemaName, pgErr.TableName}, 0)
		removed += n

		if err != nil {
			return removed, fmt.Errorf("clear %s of garage %s: %w", pgErr.TableName, garageID, err)
		}
	}

	return removed, nil
}

// clearTable deletes the rows of a garage in table. When another table refers
// to them it is cleared first (a part's unit is referred to by its barcodes).
func (r *PostgresUserRepository) clearTable(ctx context.Context, garageID string, table pgx.Identifier, depth int) (int64, error) {
	txManager := platformdb.NewTxManager(r.pool)

	for range maxBlockersPerUser {
		var removed int64

		err := txManager.WithTransaction(ctx, platformdb.GarageScope(garageID), func(garageCtx context.Context) error {
			tag, err := platformdb.QuerierFromContext(garageCtx, r.pool).
				Exec(garageCtx, `DELETE FROM `+table.Sanitize()+` WHERE garage_id = $1`, garageID)
			removed = tag.RowsAffected()

			return err
		})
		if err == nil {
			return removed, nil
		}

		pgErr := foreignKeyViolation(err)
		if pgErr == nil || depth >= maxBlockerDepth {
			return 0, err
		}

		if _, err := r.clearTable(ctx, garageID, pgx.Identifier{pgErr.SchemaName, pgErr.TableName}, depth+1); err != nil {
			return 0, err
		}
	}

	return 0, errTestUserBlocked
}

// garageIDsOf lists the garages the user owns.
func (r *PostgresUserRepository) garageIDsOf(ctx context.Context, userID string) ([]string, error) {
	txManager := platformdb.NewTxManager(r.pool)

	var ids []string

	err := txManager.WithTransaction(ctx, platformdb.UserScope(userID), func(userCtx context.Context) error {
		rows, err := platformdb.QuerierFromContext(userCtx, r.pool).Query(userCtx, `SELECT id::text FROM garages WHERE user_id = $1 ORDER BY id`, userID)
		if err != nil {
			return err
		}

		ids, err = pgx.CollectRows(rows, pgx.RowTo[string])

		return err
	})
	if err != nil {
		return nil, fmt.Errorf("list the garages of %s: %w", userID, err)
	}

	return ids, nil
}
