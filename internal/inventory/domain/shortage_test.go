package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
)

func TestShortages(t *testing.T) {
	t.Parallel()

	line := func(id, part, quantity string) domain.DraftLine {
		return domain.DraftLine{LineID: id, PartID: part, Quantity: quantity}
	}

	tests := []struct {
		name   string
		lines  []domain.DraftLine
		onHand map[string]string
		want   map[string]string
	}{
		{"covered", []domain.DraftLine{line("a", "p1", "4")}, map[string]string{"p1": "5"}, map[string]string{}},
		{"exactly covered", []domain.DraftLine{line("a", "p1", "5")}, map[string]string{"p1": "5"}, map[string]string{}},
		{"short by the difference", []domain.DraftLine{line("a", "p1", "5")}, map[string]string{"p1": "3.5"}, map[string]string{"a": "1.5"}},
		{"no stock at all", []domain.DraftLine{line("a", "p1", "2")}, map[string]string{}, map[string]string{"a": "2"}},
		{"negative stock counts as none", []domain.DraftLine{line("a", "p1", "2")}, map[string]string{"p1": "-3"}, map[string]string{"a": "2"}},
		{
			"first come, first served",
			[]domain.DraftLine{line("a", "p1", "4"), line("b", "p1", "4")},
			map[string]string{"p1": "5"},
			map[string]string{"b": "3"},
		},
		{
			"a short line takes what is left, so the next one gets nothing",
			[]domain.DraftLine{line("a", "p1", "8"), line("b", "p1", "1")},
			map[string]string{"p1": "5"},
			map[string]string{"a": "3", "b": "1"},
		},
		{
			"parts are served from their own stock",
			[]domain.DraftLine{line("a", "p1", "4"), line("b", "p2", "4")},
			map[string]string{"p1": "5", "p2": "1"},
			map[string]string{"b": "3"},
		},
		{"a bad quantity is ignored", []domain.DraftLine{line("a", "p1", "x"), line("b", "p1", "0")}, map[string]string{}, map[string]string{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, domain.Shortages(tt.lines, tt.onHand))
		})
	}
}
