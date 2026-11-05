package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/receivable/domain"
)

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

func TestReceivableAmountsAndStatus(t *testing.T) {
	t.Parallel()

	r := &domain.Receivable{TotalAmount: 1_000_000, PaidAmount: 400_000}
	r.Derive()
	assert.Equal(t, domain.StatusOpen, r.Status)
	assert.EqualValues(t, 600_000, r.Remaining())

	r.PaidAmount = 1_000_000
	r.Derive()
	assert.Equal(t, domain.StatusPaid, r.Status)
	assert.EqualValues(t, 0, r.Remaining())

	r.PaidAmount = 1_200_000
	assert.EqualValues(t, 0, r.Remaining(), "never negative")
}

func TestDaysOverdue(t *testing.T) {
	t.Parallel()

	r := &domain.Receivable{Status: domain.StatusOpen, DueDate: day(2026, 11, 10)}

	assert.Equal(t, 0, r.DaysOverdue(day(2026, 11, 9)))
	assert.Equal(t, 0, r.DaysOverdue(day(2026, 11, 10)), "due today is not overdue")
	assert.Equal(t, 3, r.DaysOverdue(day(2026, 11, 13)))
	assert.True(t, r.Overdue(day(2026, 11, 11)))
	assert.False(t, r.Overdue(day(2026, 11, 10)))

	r.Status = domain.StatusPaid
	assert.Equal(t, 0, r.DaysOverdue(day(2026, 12, 1)), "a paid receivable is never overdue")
}

func TestDueDateAndToday(t *testing.T) {
	t.Parallel()

	hcm, err := time.LoadLocation("Asia/Ho_Chi_Minh")
	require.NoError(t, err)

	// 20:00 UTC on the 9th is already the 10th in Ho Chi Minh City.
	created := time.Date(2026, 11, 9, 20, 0, 0, 0, time.UTC)

	assert.Equal(t, day(2026, 11, 17), domain.DueDateFor(created, hcm, 7))
	assert.Equal(t, day(2026, 11, 10), domain.DueDateFor(created, hcm, 0))
	assert.Equal(t, day(2026, 11, 16), domain.DueDateFor(created, time.UTC, 7))
	assert.Equal(t, day(2026, 11, 10), domain.Today(created, hcm))
}

func TestSettingsNormalize(t *testing.T) {
	t.Parallel()

	def := domain.DefaultSettings()
	assert.Equal(t, 7, def.PaymentTermsDays)
	assert.True(t, def.RemindersEnabled)
	assert.Equal(t, []int{3, 7}, def.ReminderOffsetDays)

	got, err := domain.Settings{PaymentTermsDays: 0, ReminderOffsetDays: []int{10, 2, 5}}.Normalize()
	require.NoError(t, err)
	assert.Equal(t, []int{2, 5, 10}, got.ReminderOffsetDays)

	for name, s := range map[string]domain.Settings{
		"negative terms": {PaymentTermsDays: -1},
		"long terms":     {PaymentTermsDays: 366},
	} {
		_, err := s.Normalize()
		require.ErrorIs(t, err, domain.ErrInvalidTerms, name)
	}

	for name, offsets := range map[string][]int{
		"duplicate": {1, 1}, "six": {1, 2, 3, 4, 5, 6}, "zero": {0}, "over 90": {91}, "negative": {-3},
	} {
		_, err := domain.Settings{PaymentTermsDays: 7, ReminderOffsetDays: offsets}.Normalize()
		require.ErrorIs(t, err, domain.ErrInvalidOffsets, name)
	}

	_, err = domain.Settings{PaymentTermsDays: 365, ReminderOffsetDays: []int{1, 2, 3, 4, 90}}.Normalize()
	require.NoError(t, err)
}

func TestDueOffsets(t *testing.T) {
	t.Parallel()

	s := domain.Settings{ReminderOffsetDays: []int{3, 7, 14}}

	assert.Empty(t, s.DueOffsets(2, nil))
	assert.Equal(t, []int{3}, s.DueOffsets(3, nil))
	assert.Equal(t, []int{3, 7}, s.DueOffsets(10, nil))
	assert.Equal(t, []int{7}, s.DueOffsets(10, map[int]bool{3: true}))
	assert.Empty(t, s.DueOffsets(10, map[int]bool{3: true, 7: true}))
}

func TestEntryBalance(t *testing.T) {
	t.Parallel()

	assert.EqualValues(t, 100, domain.Entry{Kind: domain.EntryCharge, Amount: 100}.Balance())
	assert.EqualValues(t, -100, domain.Entry{Kind: domain.EntryPayment, Amount: 100}.Balance())
	assert.EqualValues(t, 100, domain.Entry{Kind: domain.EntryVoid, Amount: 100}.Balance())
}

func TestParsePeriod(t *testing.T) {
	t.Parallel()

	from, to, err := domain.ParsePeriod("2026-11-01", "2026-12-01")
	require.NoError(t, err)
	assert.Equal(t, day(2026, 11, 1), from)
	assert.Equal(t, day(2026, 12, 1), to)

	for _, p := range [][2]string{
		{"2026-11-01", "2026-11-01"}, {"2026-12-01", "2026-11-01"}, {"bad", "2026-11-01"}, {"2026-11-01", "bad"}, {"", ""},
	} {
		_, _, err := domain.ParsePeriod(p[0], p[1])
		require.ErrorIs(t, err, domain.ErrInvalidPeriod, p)
	}
}
