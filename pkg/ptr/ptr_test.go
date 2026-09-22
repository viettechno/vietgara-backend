package ptr_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/pkg/ptr"
)

func TestDeref(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ptr  *int
		def  int
		want int
	}{
		{name: "nil pointer uses default", ptr: nil, def: 42, want: 42},
		{name: "non-nil pointer returns value", ptr: ptr.To(7), def: 42, want: 7},
		{name: "zero value pointer returns the pointed value", ptr: ptr.To(0), def: 42, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, ptr.Deref(tt.ptr, tt.def))
		})
	}
}

func TestTo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   int
		want int
	}{
		{name: "zero", in: 0, want: 0},
		{name: "positive", in: 42, want: 42},
		{name: "negative", in: -7, want: -7},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := ptr.To(tt.in)
			require.NotNil(t, got)
			require.Equal(t, tt.want, *got)
		})
	}
}
