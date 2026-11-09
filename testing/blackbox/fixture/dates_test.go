package fixture

import (
	"testing"
	"time"
)

func TestGarageDateAtBusinessDateBoundary(t *testing.T) {
	tests := []struct {
		name    string
		now     time.Time
		daysAgo int
		want    string
	}{
		{
			name: "one second before Vietnam midnight",
			now:  time.Date(2026, time.November, 8, 16, 59, 59, 0, time.UTC),
			want: "2026-11-08",
		},
		{
			name: "Vietnam midnight while UTC is the prior day",
			now:  time.Date(2026, time.November, 8, 17, 0, 0, 0, time.UTC),
			want: "2026-11-09",
		},
		{
			name:    "subtracts calendar days in Vietnam",
			now:     time.Date(2026, time.November, 8, 17, 0, 0, 0, time.UTC),
			daysAgo: 1,
			want:    "2026-11-08",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := garageDateAt(tt.now, tt.daysAgo); got != tt.want {
				t.Errorf("garageDateAt(%s, %d) = %q, want %q", tt.now, tt.daysAgo, got, tt.want)
			}
		})
	}
}
