package repository

import "strings"

var phoneFormatting = strings.NewReplacer(" ", "", "-", "", ".", "", "(", "", ")", "")

// PhoneVariants returns the stripped phone plus its "0..." / "+84..." twin.
func PhoneVariants(phone string) []string {
	p := phoneFormatting.Replace(strings.TrimSpace(phone))

	switch {
	case strings.HasPrefix(p, "+84"):
		return []string{p, "0" + strings.TrimPrefix(p, "+84")}
	case strings.HasPrefix(p, "0"):
		return []string{p, "+84" + strings.TrimPrefix(p, "0")}
	default:
		return []string{p}
	}
}
