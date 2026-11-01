package fixture

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/ledongthuc/pdf"
)

// PDFText returns the text of a PDF the backend rendered, one space between
// words and no line breaks, so a test can look for what a person would read
// ("Một triệu hai trăm nghìn đồng chẵn", a tax ID) without depending on the
// layout. The documents embed a TrueType font with a ToUnicode map, so
// Vietnamese diacritics come back as typed.
func PDFText(tb testing.TB, body []byte) string {
	tb.Helper()

	reader, err := pdf.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		tb.Fatalf("read the PDF: %v", err)
	}

	plain, err := reader.GetPlainText()
	if err != nil {
		tb.Fatalf("extract the text of the PDF: %v", err)
	}

	text, err := io.ReadAll(plain)
	if err != nil {
		tb.Fatalf("read the text of the PDF: %v", err)
	}

	return strings.Join(strings.Fields(string(text)), " ")
}
