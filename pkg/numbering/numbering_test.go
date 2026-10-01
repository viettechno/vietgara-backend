package numbering_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/pkg/numbering"
)

func TestRender(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 30, 10, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		template string
		seq      int
		want     string
	}{
		{name: "quote default", template: "BG-{YYYY}{MM}-{SEQ:4}", seq: 7, want: "BG-202609-0007"},
		{name: "repair order default", template: "LSC-{YYYY}{MM}-{SEQ:4}", seq: 42, want: "LSC-202609-0042"},
		{name: "short year", template: "{YY}{MM}{DD}-{SEQ:3}", seq: 1, want: "260930-001"},
		{name: "no padding needed", template: "{SEQ:1}", seq: 9, want: "9"},
		{name: "sequence wider than digits is not truncated", template: "{SEQ:2}", seq: 12345, want: "12345"},
		{name: "no date placeholders", template: "Q-{SEQ:4}", seq: 3, want: "Q-0003"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, numbering.Render(tt.template, now, tt.seq))
		})
	}
}

func TestPeriod(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 30, 10, 0, 0, 0, time.UTC)
	later := time.Date(2026, time.September, 30, 23, 59, 0, 0, time.UTC)
	nextMonth := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		template string
		now      time.Time
		want     string
	}{
		{name: "monthly template", template: "BG-{YYYY}{MM}-{SEQ:4}", now: now, want: "BG-202609-{SEQ:4}"},
		{name: "monthly template stable within the month", template: "{YYYY}{MM}", now: later, want: "202609"},
		{name: "monthly template resets next month", template: "{YYYY}{MM}", now: nextMonth, want: "202610"},
		{name: "daily template", template: "{YYYY}{MM}{DD}", now: now, want: "20260930"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, numbering.Period(tt.template, tt.now))
		})
	}
}
