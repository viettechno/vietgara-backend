package printmodel_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/document/printmodel"
	"github.com/viettechno/vietgara-backend/internal/document/printmodel/fixture"
)

// TestModelsAreStable pins the JSON of every print model (FR-DOC-04): a field
// changes only together with a new template version and a new golden file.
// Regenerate the files with UPDATE_GOLDEN=1.
func TestModelsAreStable(t *testing.T) {
	t.Parallel()

	for _, m := range fixture.All() {
		want := filepath.Join("testdata", string(m.Type())+".json")

		got, err := json.MarshalIndent(m, "", "  ")
		require.NoError(t, err)

		got = append(got, '\n')

		if os.Getenv("UPDATE_GOLDEN") != "" {
			require.NoError(t, os.WriteFile(want, got, 0o600))
		}

		golden, err := os.ReadFile(want) //nolint:gosec // a fixed test file
		require.NoError(t, err)
		assert.JSONEq(t, string(golden), string(got), m.Type())
	}
}

func TestFixtureCoversEveryType(t *testing.T) {
	t.Parallel()

	var types []printmodel.DocumentType
	for _, m := range fixture.All() {
		types = append(types, m.Type())
	}

	assert.Equal(t, printmodel.Types(), types)
}

func TestDocumentType(t *testing.T) {
	t.Parallel()

	for _, known := range printmodel.Types() {
		assert.True(t, known.Valid(), known)
	}

	assert.False(t, printmodel.DocumentType("invoice").Valid())
	assert.False(t, printmodel.DocumentType("").Valid())
	assert.True(t, printmodel.BillTo{}.IsZero())
	assert.False(t, printmodel.BillTo{Name: "A"}.IsZero())
}
