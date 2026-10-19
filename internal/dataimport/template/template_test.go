package template_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"

	"github.com/viettechno/vietgara-backend/internal/dataimport/domain"
	"github.com/viettechno/vietgara-backend/internal/dataimport/template"
)

var allTypes = []domain.Type{domain.TypeParts, domain.TypeGoodsReceipts, domain.TypeStockCounts}

func TestBuildCarriesTheHeadersAndInstructions(t *testing.T) {
	t.Parallel()

	for _, typ := range allTypes {
		t.Run(string(typ), func(t *testing.T) {
			t.Parallel()

			data, name, err := template.Build(typ)
			require.NoError(t, err)
			assert.Equal(t, "vietgara-"+string(typ)+"-template.xlsx", name)

			f, err := excelize.OpenReader(bytes.NewReader(data))
			require.NoError(t, err)

			defer func() { _ = f.Close() }()

			assert.Equal(t, []string{"Data", "Instructions"}, f.GetSheetList())

			rows, err := f.GetRows("Data")
			require.NoError(t, err)
			require.Len(t, rows, 1, "the data sheet has only the header, nothing that could be imported by mistake")
			assert.Equal(t, template.Columns(typ), rows[0])

			help, err := f.GetRows("Instructions")
			require.NoError(t, err)
			require.Len(t, help, len(template.Columns(typ))+1, "one instruction line per column plus the header")
			assert.Equal(t, []string{"Column", "How to fill it", "Cách điền"}, help[0])

			for i, column := range template.Columns(typ) {
				assert.Equal(t, column, help[i+1][0], "instructions follow the column order")
				assert.NotEmpty(t, help[i+1][1], column)
				assert.NotEmpty(t, help[i+1][2], column+" has a Vietnamese instruction")
			}
		})
	}
}

func TestBuildKeepsLeadingZerosOfCodes(t *testing.T) {
	t.Parallel()

	data, _, err := template.Build(domain.TypeParts)
	require.NoError(t, err)

	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)

	defer func() { _ = f.Close() }()

	style, err := f.GetColStyle("Data", "A")
	require.NoError(t, err)

	got, err := f.GetStyle(style)
	require.NoError(t, err)
	assert.Equal(t, 49, got.NumFmt, "the SKU column is text")
}

func TestBuildUnknownType(t *testing.T) {
	t.Parallel()

	_, _, err := template.Build(domain.Type("units"))
	require.Error(t, err)
	assert.Nil(t, template.Columns(domain.Type("units")))
}
