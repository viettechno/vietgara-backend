package audit_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/platform/audit"
	"github.com/viettechno/vietgara-backend/internal/platform/db/dbtest"
)

func TestLogger(t *testing.T) {
	t.Parallel()

	tx := dbtest.New(t).
		ExpectExec("INSERT INTO audit_log", dbtest.Tag(1), nil).
		WithArgs(func(args []any) {
			assert.Equal(t, []any{"acc", "g", "role.updated", "role", "r1"}, args[:5])
			assert.Nil(t, args[5])
			after, _ := args[6].([]byte)
			assert.JSONEq(t, `{"name":"x"}`, string(after))
			assert.Equal(t, "1.2.3.4", args[7])
		})

	l := audit.NewLogger(nil)
	require.NoError(t, l.Record(tx.Context(t.Context()), audit.Entry{
		Actor: "acc", GarageID: "g", Action: "role.updated", EntityType: "role", EntityID: "r1",
		After: map[string]string{"name": "x"}, IP: "1.2.3.4",
	}))

	require.Error(t, l.Record(t.Context(), audit.Entry{Before: make(chan int)}))
	require.Error(t, l.Record(t.Context(), audit.Entry{After: make(chan int)}))
}
