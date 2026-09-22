package repository_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/viettechno/vietgara-backend/internal/identity/repository"
)

func TestPhoneVariants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want []string
	}{
		{in: "0912 345-678", want: []string{"0912345678", "+84912345678"}},
		{in: "+84 (912) 345.678", want: []string{"+84912345678", "0912345678"}},
		{in: "912345678", want: []string{"912345678"}},
	}

	for _, tc := range tests {
		assert.Equal(t, tc.want, repository.PhoneVariants(tc.in), tc.in)
	}
}
