package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/customer/domain"
)

func TestNormalizeLicensePlate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      string
		want    string
		wantKey string
		wantErr error
	}{
		{name: "car plate", in: " 51f-123.45 ", want: "51F-123.45", wantKey: "51F12345"},
		{name: "collapses whitespace", in: "59  x1   123.45", want: "59 X1 123.45", wantKey: "59X112345"},
		{name: "blank", in: "   ", wantErr: domain.ErrLicensePlateRequired},
		{name: "too few characters", in: "5-1F", wantErr: domain.ErrInvalidLicensePlate},
		{name: "too many characters", in: "51F1234567890", wantErr: domain.ErrInvalidLicensePlate},
		{name: "too long with separators", in: "51F-1-2-3-4-5-6-7-8-9", wantErr: domain.ErrInvalidLicensePlate},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := domain.NormalizeLicensePlate(tt.in)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.wantKey, domain.LicensePlateKey(got))
		})
	}
}

func TestValidateYear(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC)

	require.NoError(t, domain.ValidateYear(domain.MinVehicleYear, now))
	require.NoError(t, domain.ValidateYear(2027, now))
	require.ErrorIs(t, domain.ValidateYear(2028, now), domain.ErrInvalidYear)
	require.ErrorIs(t, domain.ValidateYear(1800, now), domain.ErrInvalidYear)
}

func TestCustomerConsent(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	c := &domain.Customer{}
	assert.Nil(t, c.LatestConsent())

	c.RecordConsent(true, at, "user-1")
	c.RecordConsent(true, at.Add(time.Hour), "user-2")
	require.Len(t, c.ConsentLog, 1, "an unchanged decision is not recorded twice")

	c.RecordConsent(false, at.Add(2*time.Hour), "user-2")
	require.Len(t, c.ConsentLog, 2)
	assert.Equal(t, domain.ConsentRecord{Granted: false, RecordedAt: at.Add(2 * time.Hour), RecordedBy: "user-2"}, *c.LatestConsent())
}

func TestCheckLength(t *testing.T) {
	t.Parallel()

	require.NoError(t, domain.CheckLength("name", "Nguyễn", 6), "counts characters, not bytes")

	err := domain.CheckLength("name", "Nguyễn Văn", 6)
	require.ErrorIs(t, err, domain.ErrFieldTooLong)
	assert.EqualError(t, err, "name must be at most 6 characters")
}
