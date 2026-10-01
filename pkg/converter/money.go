package converter

import "strconv"

// FormatVND renders an integer VND amount with the Vietnamese thousands
// separator ("."), e.g. 730000 -> "730.000". Used in e-mails and other
// plain-text contexts where the API's raw integer would be hard to read;
// API responses themselves keep sending the plain integer (money as
// integer VND strings, API Specification Section 1.1).
func FormatVND(amount int64) string {
	sign := ""
	if amount < 0 {
		sign = "-"
		amount = -amount
	}

	digits := strconv.FormatInt(amount, 10)

	var grouped []byte

	for i, d := range []byte(digits) {
		if i > 0 && (len(digits)-i)%3 == 0 {
			grouped = append(grouped, '.')
		}

		grouped = append(grouped, d)
	}

	return sign + string(grouped)
}
