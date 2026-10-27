package usecase_test

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/mocks"
	"github.com/viettechno/vietgara-backend/internal/reporting/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

const garageID = "11111111-1111-1111-1111-111111111111"

var errBoomReporting = errors.New("boom")

// hcm is the default garage time zone (FR-TEN-05), UTC+7 without DST.
const hcm = "Asia/Ho_Chi_Minh"

// deps are the mocked dependencies of one ReportingUseCase under test.
type deps struct {
	Settlements *mocks.MockReportingSettlementReader
	Garages     *mocks.MockReportingGarageReader
}

func newUseCase(t *testing.T) (*usecase.ReportingUseCase, *deps) {
	t.Helper()

	d := &deps{Settlements: mocks.NewMockReportingSettlementReader(t), Garages: mocks.NewMockReportingGarageReader(t)}

	return usecase.NewReportingUseCase(usecase.Deps{Settlements: d.Settlements, Garages: d.Garages}), d
}

// localMidnight matches the instant that starts day (YYYY-MM-DD) in zone.
func localMidnight(t *testing.T, day, zone string) any {
	t.Helper()

	loc, err := time.LoadLocation(zone)
	require.NoError(t, err)

	want, err := time.ParseInLocation("2006-01-02", day, loc)
	require.NoError(t, err)

	return mock.MatchedBy(func(got time.Time) bool { return got.Equal(want) })
}

// inZone matches a *time.Location named zone.
func inZone(zone string) any {
	return mock.MatchedBy(func(loc *time.Location) bool { return loc.String() == zone })
}

func date(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}

	return t
}

func assertAppError(t *testing.T, err error, code apperror.Code) {
	t.Helper()

	require.Error(t, err)

	appErr, ok := apperror.As(err)
	require.True(t, ok, "expected an *apperror.AppError, got %T: %v", err, err)
	require.Equal(t, code, appErr.Code)
}
