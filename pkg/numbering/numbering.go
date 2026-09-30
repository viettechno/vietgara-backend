// Package numbering renders a garage's document numbering templates
// (FR-TEN-05, e.g. "BG-{YYYY}{MM}-{SEQ:4}") into concrete document numbers.
// Template syntax is owned and validated by the Tenant module
// (tenant/domain.ValidNumberingTemplate); this package only renders an
// already-valid template, so it can be shared by every module that issues
// numbered documents (Quote & Pricing today; Repair Order, Release 1.3)
// without importing Tenant.
package numbering

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	dateToken = regexp.MustCompile(`\{(YYYY|YY|MM|DD)\}`)
	seqToken  = regexp.MustCompile(`\{SEQ:([1-9])\}`)
)

// Period reduces template to its date placeholders rendered for now,
// dropping the {SEQ:n} placeholder and any surrounding literal text. It is
// the counter key a Sequencer resets on: a template using only {YYYY}{MM}
// yields the same period for every day of one month, so the counter resets
// monthly; one that adds {DD} resets daily.
func Period(template string, now time.Time) string {
	return dateToken.ReplaceAllStringFunc(template, func(token string) string {
		return renderDateToken(token, now)
	})
}

// Render substitutes template's date placeholders for now and its
// {SEQ:n} placeholder for seq, zero-padded to n digits, e.g.
// Render("BG-{YYYY}{MM}-{SEQ:4}", now, 7) -> "BG-202609-0007".
func Render(template string, now time.Time, seq int) string {
	out := dateToken.ReplaceAllStringFunc(template, func(token string) string {
		return renderDateToken(token, now)
	})

	return seqToken.ReplaceAllStringFunc(out, func(token string) string {
		digits, _ := strconv.Atoi(seqToken.FindStringSubmatch(token)[1])

		return renderSeq(seq, digits)
	})
}

func renderDateToken(token string, now time.Time) string {
	switch token {
	case "{YYYY}":
		return now.Format("2006")
	case "{YY}":
		return now.Format("06")
	case "{MM}":
		return now.Format("01")
	case "{DD}":
		return now.Format("02")
	default:
		return token
	}
}

func renderSeq(seq, digits int) string {
	s := strconv.Itoa(seq)
	if pad := digits - len(s); pad > 0 {
		s = strings.Repeat("0", pad) + s
	}

	return s
}
