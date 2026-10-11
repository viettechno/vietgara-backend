package render

import (
	"slices"
	"strings"
	"unicode/utf8"
)

var (
	digitWords = [10]string{"không", "một", "hai", "ba", "bốn", "năm", "sáu", "bảy", "tám", "chín"}
	// groupUnits names each group of three digits, lowest first.
	groupUnits = [...]string{"", "nghìn", "triệu", "tỷ", "nghìn tỷ", "triệu tỷ", "tỷ tỷ"}
)

// AmountInWords writes an integer VND amount in Vietnamese the way the
// voucher's "viết bằng chữ" line does: "Một triệu hai trăm nghìn đồng
// chẵn". A negative amount is written as its absolute value.
func AmountInWords(amount int64) string {
	if amount < 0 {
		amount = -amount
	}

	if amount == 0 {
		return "Không đồng chẵn"
	}

	var groups []int

	for n := amount; n > 0; n /= 1000 {
		groups = append(groups, int(n%1000))
	}

	var words []string

	for i, group := range slices.Backward(groups) {
		if group == 0 {
			continue
		}

		// "không trăm" is spoken only inside a number, never at its start.
		words = append(words, groupWords(group, i < len(groups)-1))
		if groupUnits[i] != "" {
			words = append(words, groupUnits[i])
		}
	}

	text := strings.Join(words, " ") + " đồng chẵn"
	first, size := utf8.DecodeRuneInString(text)

	return strings.ToUpper(string(first)) + text[size:]
}

// groupWords writes one non-zero group of up to three digits. inside is
// true when a higher group precedes it, which makes a missing hundreds or
// tens digit audible ("không trăm", "lẻ").
func groupWords(n int, inside bool) string {
	hundreds, tens, units := n/100, n/10%10, n%10

	var words []string

	if hundreds > 0 || (inside && (tens > 0 || units > 0)) {
		words = append(words, digitWords[hundreds], "trăm")
	}

	words = append(words, tensWords(tens, units, hundreds > 0 || inside)...)

	if units > 0 {
		words = append(words, unitWord(units, tens))
	}

	return strings.Join(words, " ")
}

// tensWords writes the tens digit; spoken is true when the digit before
// it was, so that a zero tens is "lẻ" rather than silent.
func tensWords(tens, units int, spoken bool) []string {
	switch {
	case tens == 0 && units > 0 && spoken:
		return []string{"lẻ"}
	case tens == 1:
		return []string{"mười"}
	case tens > 1:
		return []string{digitWords[tens], "mươi"}
	default:
		return nil
	}
}

// unitWord writes the units digit, which changes after a tens digit:
// "hai mươi mốt", "mười lăm".
func unitWord(units, tens int) string {
	switch {
	case units == 1 && tens > 1:
		return "mốt"
	case units == 5 && tens > 0:
		return "lăm"
	default:
		return digitWords[units]
	}
}
