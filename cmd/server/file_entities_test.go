package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// No entity other than the garage has a file kind yet, so nothing is vouched
// for; each future kind adds its case in fileEntities.
func TestFileEntitiesVouchForNothingYet(t *testing.T) {
	t.Parallel()

	exists, err := fileEntities{}.EntityExists(t.Context(), "garage-1", "part", "part-1")
	require.NoError(t, err)
	assert.False(t, exists)
}
