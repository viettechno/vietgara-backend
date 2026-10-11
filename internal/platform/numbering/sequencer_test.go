package numbering_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/platform/db/dbtest"
	"github.com/viettechno/vietgara-backend/internal/platform/numbering"
)

func TestSequencer_Next(t *testing.T) {
	t.Parallel()

	boom := errSequencerTestBoom
	seq := numbering.NewSequencer(nil)

	tx := dbtest.New(t).
		ExpectQueryRow("ON CONFLICT (garage_id, doc_type, period)", dbtest.Row(1)).
		ExpectQueryRow("ON CONFLICT (garage_id, doc_type, period)", dbtest.Row(2)).
		ExpectQueryRow("ON CONFLICT (garage_id, doc_type, period)", dbtest.ErrRow(boom))
	ctx := tx.Context(t.Context())

	n, err := seq.Next(ctx, "g-1", "quote", "202609")
	require.NoError(t, err)
	require.Equal(t, 1, n)

	n, err = seq.Next(ctx, "g-1", "quote", "202609")
	require.NoError(t, err)
	require.Equal(t, 2, n)

	_, err = seq.Next(ctx, "g-1", "quote", "202609")
	require.ErrorIs(t, err, boom)
}
