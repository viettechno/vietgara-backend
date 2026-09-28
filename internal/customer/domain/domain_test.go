package domain_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/customer/domain"
)

func TestNormalizePhone(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      string
		want    string
		wantErr error
	}{
		{name: "plain local number", in: "0912345678", want: "0912345678"},
		{name: "strips formatting", in: " (091) 234-56.78 ", want: "0912345678"},
		{name: "folds +84 to 0", in: "+84 912 345 678", want: "0912345678"},
		{name: "keeps other country codes", in: "+6591234567", want: "+6591234567"},
		{name: "too short", in: "0912", wantErr: domain.ErrInvalidPhone},
		{name: "too long", in: "0123456789012345", wantErr: domain.ErrInvalidPhone},
		{name: "letters", in: "09123abc78", wantErr: domain.ErrInvalidPhone},
		{name: "plus in the middle", in: "0912+345678", wantErr: domain.ErrInvalidPhone},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := domain.NormalizePhone(tt.in)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestNormalizeEmail(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "lower-cases and trims", in: "  Binh@Example.COM ", want: "binh@example.com"},
		{name: "rejects display names", in: "Binh <binh@example.com>", wantErr: true},
		{name: "rejects garbage", in: "not-an-email", wantErr: true},
		{name: "rejects overly long addresses", in: strings.Repeat("a", 250) + "@example.com", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := domain.NormalizeEmail(tt.in)
			if tt.wantErr {
				require.ErrorIs(t, err, domain.ErrInvalidEmail)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

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

	c.RecordConsent(true, at, "acc-1")
	c.RecordConsent(true, at.Add(time.Hour), "acc-2")
	require.Len(t, c.ConsentLog, 1, "an unchanged decision is not recorded twice")

	c.RecordConsent(false, at.Add(2*time.Hour), "acc-2")
	require.Len(t, c.ConsentLog, 2)
	assert.Equal(t, domain.ConsentRecord{Granted: false, RecordedAt: at.Add(2 * time.Hour), RecordedBy: "acc-2"}, *c.LatestConsent())
}

func TestCheckLength(t *testing.T) {
	t.Parallel()

	require.NoError(t, domain.CheckLength("name", "Nguyễn", 6), "counts characters, not bytes")

	err := domain.CheckLength("name", "Nguyễn Văn", 6)
	require.ErrorIs(t, err, domain.ErrFieldTooLong)
	assert.EqualError(t, err, "name must be at most 6 characters")
}

func TestPartnerTypeIsValid(t *testing.T) {
	t.Parallel()

	assert.True(t, domain.PartnerTypeSupplier.IsValid())
	assert.True(t, domain.PartnerTypeInsurer.IsValid())
	assert.False(t, domain.PartnerType("").IsValid())
	assert.False(t, domain.PartnerType("SUPPLIER").IsValid())
}
