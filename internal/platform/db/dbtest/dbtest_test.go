package dbtest_test

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platform/db/dbtest"
)

func TestScriptedTx(t *testing.T) {
	t.Parallel()

	now := time.Now()
	boom := errDBTestBoom

	var gotArgs []any

	tx := dbtest.New(t).
		ExpectQueryRow("SELECT one", dbtest.Row("id", nil, "x", 3, now)).
		ExpectQuery("SELECT many", dbtest.NewRows([]any{"a"}, []any{"b"}).WithErr(boom), nil).
		ExpectQuery("SELECT fail", nil, boom).
		ExpectExec("UPDATE", dbtest.Tag(2), nil).WithArgs(func(args []any) { gotArgs = args })

	ctx := tx.Context(t.Context())
	q := platformdb.QuerierFromContext(ctx, nil)

	var (
		id    string
		empty *string
		ptr   *string
		n     int32
		at    *time.Time
	)

	require.NoError(t, q.QueryRow(ctx, "SELECT one").Scan(&id, &empty, &ptr, &n, &at))
	assert.Equal(t, "id", id)
	assert.Nil(t, empty)
	assert.Equal(t, "x", *ptr)
	assert.EqualValues(t, 3, n)
	assert.Equal(t, now, *at)

	rows, err := q.Query(ctx, "SELECT many")
	require.NoError(t, err)

	defer rows.Close()

	var got []string

	for rows.Next() {
		var s string
		require.NoError(t, rows.Scan(&s))
		got = append(got, s)
	}

	assert.Equal(t, []string{"a", "b"}, got)
	require.ErrorIs(t, rows.Err(), boom)
	require.Error(t, rows.Scan(new(string)), "scan after the last row")

	failed, err := q.Query(ctx, "SELECT fail") //nolint:sqlclosecheck // the scripted query fails: no rows to close
	require.ErrorIs(t, err, boom)
	assert.Nil(t, failed)

	tag, err := q.Exec(ctx, "UPDATE x", 1, "two")
	require.NoError(t, err)
	assert.EqualValues(t, 2, tag.RowsAffected())
	assert.Equal(t, []any{1, "two"}, gotArgs)
}

func TestRowsAndErrors(t *testing.T) {
	t.Parallel()

	boom := errDBTestBoom

	require.ErrorIs(t, dbtest.ErrRow(pgx.ErrNoRows).Scan(), pgx.ErrNoRows)
	require.ErrorContains(t, dbtest.Row(1).Scan(new(int), new(int)), "1 values for 2")
	require.ErrorContains(t, dbtest.Row("x").Scan(new(time.Time)), "cannot scan")

	rows := dbtest.NewRows([]any{"a"}).WithScanError(boom)
	require.True(t, rows.Next())
	require.ErrorIs(t, rows.Scan(new(string)), boom)

	var pgErr *pgconn.PgError

	require.ErrorAs(t, dbtest.UniqueViolation("users_email_key"), &pgErr)
	assert.Equal(t, "23505", pgErr.Code)
	require.ErrorAs(t, dbtest.ForeignKeyViolation(), &pgErr)
	assert.Equal(t, "23503", pgErr.Code)
}
