package usecase

import (
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	decimalPattern = regexp.MustCompile(`^\d+(\.\d+)?$`)
	barcodePattern = regexp.MustCompile(`^[A-Za-z0-9 \-./+$%]+$`)
)

// splitList splits a semicolon-separated cell. Commas are not used because a
// barcode may contain punctuation and a decimal quantity a dot.
func splitList(s string) []string {
	parts := strings.Split(s, ";")
	out := make([]string, 0, len(parts))

	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}

	return out
}

// parseQuantity parses a non-negative decimal with at most three decimals and
// renders it without trailing zeros. aboveZero refuses zero.
func parseQuantity(s string, aboveZero bool) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" || !decimalPattern.MatchString(s) {
		return "", false
	}

	r, ok := new(big.Rat).SetString(s)
	if !ok || r.Sign() < 0 || (aboveZero && r.Sign() == 0) {
		return "", false
	}

	scaled := new(big.Rat).Mul(r, big.NewRat(1000, 1))
	if !scaled.IsInt() {
		return "", false
	}

	out := r.FloatString(3)
	out = strings.TrimRight(strings.TrimRight(out, "0"), ".")

	if out == "" || out == "-0" {
		out = "0"
	}

	return out, true
}

// parseCost parses a non-negative whole VND amount. Thousands separators are
// accepted because exported spreadsheets carry them.
func parseCost(s string) (int64, bool) {
	s = strings.NewReplacer(".", "", ",", "", " ", "", "đ", "", "₫", "").Replace(strings.TrimSpace(s))
	if s == "" {
		return 0, true
	}

	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 0 {
		return 0, false
	}

	return n, true
}

// parseDate parses YYYY-MM-DD, DD/MM/YYYY, or an Excel serial date, returning
// YYYY-MM-DD.
func parseDate(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", true
	}

	for _, layout := range []string{time.DateOnly, "02/01/2006", "2006/01/02"} {
		if d, err := time.Parse(layout, s); err == nil {
			return d.Format(time.DateOnly), true
		}
	}

	serial, err := strconv.ParseFloat(s, 64)
	if err != nil || serial < 1 || serial > 2958465 {
		return "", false
	}

	d, err := excelDate(serial)
	if err != nil {
		return "", false
	}

	return d.Format(time.DateOnly), true
}

// excelDate converts an Excel serial date (1899-12-30 epoch, with the
// spreadsheet leap-year bug) to a time.Time.
func excelDate(serial float64) (time.Time, error) {
	days := int(serial)
	if serial-float64(days) >= 0.999999 {
		days++
	}

	// Excel treats 1900 as a leap year; serial 60 is the phantom 29 Feb.
	if days > 59 {
		days--
	}

	epoch := time.Date(1899, 12, 31, 0, 0, 0, 0, time.UTC)

	return epoch.AddDate(0, 0, days), nil
}

// validBarcode mirrors the Catalogue's rule: 1..64 characters from letters,
// digits, space and - . / + $ %.
func validBarcode(s string) bool {
	return s != "" && len([]rune(s)) <= 64 && barcodePattern.MatchString(s)
}

// validateUnitName checks the Catalogue's unit limits.
func validateUnitName(name string) bool {
	return name != "" && len([]rune(name)) <= 50
}

// unitAbbreviation derives the abbreviation of a packaging unit from its
// name; nil when the name is too long for the column.
func unitAbbreviation(name string) (string, bool) {
	if len([]rune(name)) > 20 {
		return "", false
	}

	return name, true
}
