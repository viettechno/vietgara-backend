package render

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/image/font"
)

func TestWrap(t *testing.T) {
	t.Parallel()

	c, err := newCanvas(200)
	require.NoError(t, err)

	defer c.close()

	f, err := c.face(sizeBody, false)
	require.NoError(t, err)

	lines := wrap(f, "Thay dầu động cơ và lọc gió\nGhi chú "+string(make([]rune, 0))+"Rấtdàikhôngcókhoảngtrắngnàocảđâu", 200)
	require.Greater(t, len(lines), 3)

	for _, line := range lines {
		assert.LessOrEqual(t, font.MeasureString(f, line).Ceil(), 200, line)
	}

	assert.Empty(t, wrap(f, "   ", 200))
}

func TestBarcodeImage(t *testing.T) {
	t.Parallel()

	img, ok := barcodeImage("LD-001", 376, 72)
	require.True(t, ok)
	assert.LessOrEqual(t, img.Rect.Dx(), 376)
	assert.Equal(t, 72, img.Rect.Dy())
	assert.Equal(t, uint8(0), img.GrayAt(0, 0).Y, "a Code 128 barcode starts with a bar")

	_, ok = barcodeImage("Mã", 376, 72)
	assert.False(t, ok, "not ASCII")

	_, ok = barcodeImage("ABCDEFGHIJKLMNOPQRSTUVWXYZABCDEFGHIJKLMNOPQRSTUVWXYZ", 376, 72)
	assert.False(t, ok, "wider than the label at one dot a module")
}
