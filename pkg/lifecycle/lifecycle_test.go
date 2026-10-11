package lifecycle_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/pkg/lifecycle"
)

var errBoom = errors.New("boom")

type recorder struct {
	deleted, deactivated int
}

func (r *recorder) rule(inUse bool, inUseErr, deleteErr, deactivateErr error) lifecycle.Rule {
	return lifecycle.Rule{
		InUse:      func(context.Context) (bool, error) { return inUse, inUseErr },
		Delete:     func(context.Context) error { r.deleted++; return deleteErr },
		Deactivate: func(context.Context) error { r.deactivated++; return deactivateErr },
	}
}

func TestApply(t *testing.T) {
	t.Parallel()

	t.Run("an unused object is deleted", func(t *testing.T) {
		t.Parallel()

		r := &recorder{}
		got, err := lifecycle.Apply(t.Context(), false, r.rule(false, nil, nil, nil))
		require.NoError(t, err)
		assert.Equal(t, lifecycle.Deleted, got)
		assert.Equal(t, recorder{deleted: 1}, *r)
	})

	t.Run("an object in use is deactivated", func(t *testing.T) {
		t.Parallel()

		r := &recorder{}
		got, err := lifecycle.Apply(t.Context(), false, r.rule(true, nil, nil, nil))
		require.NoError(t, err)
		assert.Equal(t, lifecycle.Deactivated, got)
		assert.Equal(t, recorder{deactivated: 1}, *r)
	})

	t.Run("a dry run changes nothing", func(t *testing.T) {
		t.Parallel()

		r := &recorder{}

		got, err := lifecycle.Apply(t.Context(), true, r.rule(true, nil, nil, nil))
		require.NoError(t, err)
		assert.Equal(t, lifecycle.Deactivated, got)

		got, err = lifecycle.Apply(t.Context(), true, r.rule(false, nil, nil, nil))
		require.NoError(t, err)
		assert.Equal(t, lifecycle.Deleted, got)
		assert.Equal(t, recorder{}, *r)
	})

	t.Run("failures are returned", func(t *testing.T) {
		t.Parallel()

		r := &recorder{}

		_, err := lifecycle.Apply(t.Context(), false, r.rule(false, errBoom, nil, nil))
		require.ErrorIs(t, err, errBoom)

		_, err = lifecycle.Apply(t.Context(), false, r.rule(false, nil, errBoom, nil))
		require.ErrorIs(t, err, errBoom)

		_, err = lifecycle.Apply(t.Context(), false, r.rule(true, nil, nil, errBoom))
		require.ErrorIs(t, err, errBoom)
	})
}

func TestAnyInUse(t *testing.T) {
	t.Parallel()

	no := func(context.Context) (bool, error) { return false, nil }
	yes := func(context.Context) (bool, error) { return true, nil }
	boom := func(context.Context) (bool, error) { return false, errBoom }

	got, err := lifecycle.AnyInUse(t.Context())
	require.NoError(t, err)
	assert.False(t, got, "no check, nothing in use")

	got, err = lifecycle.AnyInUse(t.Context(), no, yes, boom)
	require.NoError(t, err)
	assert.True(t, got, "stops at the first check that finds a reference")

	_, err = lifecycle.AnyInUse(t.Context(), no, boom)
	require.ErrorIs(t, err, errBoom)
}
