package usecase_test

import (
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/receivable/domain"
	"github.com/viettechno/vietgara-backend/internal/receivable/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

func TestReceivableUseCase_List(t *testing.T) {
	t.Parallel()

	t.Run("pages, measures overdue against the garage's day and passes the filter", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.timezone()

		overdue, fine := openReceivable(day(2026, 11, 17)), openReceivable(day(2026, 11, 25))
		d.Repo.EXPECT().List(mock.Anything, garageID, usecase.ListFilter{
			CustomerID: customerID, OverdueOnly: true, IncludePaid: true, Query: "51A", Today: day(2026, 11, 20), Limit: 50, Offset: 50,
		}).Return([]*domain.Receivable{overdue, fine}, 77, usecase.Summary{OpenCount: 2, OutstandingAmount: 1_200_000, OverdueCount: 1, OverdueAmount: 600_000}, nil).Once()

		res, err := uc.List(t.Context(), usecase.ListRequest{
			GarageID: garageID, CustomerID: customerID, OverdueOnly: true, IncludePaid: true, Query: "  51A ",
			PageRequest: usecase.PageRequest{Page: 2, PageSize: 50},
		})
		require.NoError(t, err)
		require.Equal(t, 77, res.Total)
		require.Equal(t, 2, res.Page)
		require.Equal(t, 50, res.PageSize)
		require.Len(t, res.Items, 2)
		require.Equal(t, 3, res.Items[0].DaysOverdue)
		require.True(t, res.Items[0].Overdue())
		require.False(t, res.Items[1].Overdue())
		require.EqualValues(t, 600_000, res.Summary.OverdueAmount)
	})

	t.Run("defaults and the page size cap", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.timezone()
		d.Repo.EXPECT().List(mock.Anything, garageID, mock.MatchedBy(func(f usecase.ListFilter) bool {
			return f.Limit == 100 && f.Offset == 0
		})).Return(nil, 0, usecase.Summary{}, nil).Once()

		res, err := uc.List(t.Context(), usecase.ListRequest{GarageID: garageID, PageRequest: usecase.PageRequest{PageSize: 1000}})
		require.NoError(t, err)
		require.Equal(t, 1, res.Page)
		require.Empty(t, res.Items)

		uc, d = newUseCase(t)
		d.timezone()
		d.Repo.EXPECT().List(mock.Anything, garageID, mock.MatchedBy(func(f usecase.ListFilter) bool { return f.Limit == usecase.DefaultPageSize })).
			Return(nil, 0, usecase.Summary{}, nil).Once()
		_, err = uc.List(t.Context(), usecase.ListRequest{GarageID: garageID})
		require.NoError(t, err)
	})

	t.Run("an unknown time zone falls back to UTC", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Garages.EXPECT().Timezone(mock.Anything, garageID).Return("Mars/Olympus", nil).Once()
		d.Repo.EXPECT().List(mock.Anything, garageID, mock.MatchedBy(func(f usecase.ListFilter) bool { return f.Today.Equal(day(2026, 11, 20)) })).
			Return(nil, 0, usecase.Summary{}, nil).Once()

		_, err := uc.List(t.Context(), usecase.ListRequest{GarageID: garageID})
		require.NoError(t, err)
	})

	t.Run("failures are internal errors", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Garages.EXPECT().Timezone(mock.Anything, garageID).Return("", errBoom).Once()
		_, err := uc.List(t.Context(), usecase.ListRequest{GarageID: garageID})
		assertAppError(t, err, apperror.CodeInternal)

		uc, d = newUseCase(t)
		d.timezone()
		d.Repo.EXPECT().List(mock.Anything, garageID, mock.Anything).Return(nil, 0, usecase.Summary{}, errBoom).Once()
		_, err = uc.List(t.Context(), usecase.ListRequest{GarageID: garageID})
		assertAppError(t, err, apperror.CodeInternal)
	})
}

func TestReceivableUseCase_Get(t *testing.T) {
	t.Parallel()

	t.Run("returns the receivable with its position against today", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.timezone()
		d.Repo.EXPECT().FindByID(mock.Anything, garageID, receivableID).Return(openReceivable(day(2026, 11, 10)), nil).Once()

		item, err := uc.Get(t.Context(), garageID, receivableID)
		require.NoError(t, err)
		require.Equal(t, 10, item.DaysOverdue)
		require.True(t, item.Overdue())
	})

	t.Run("not found carries its reason", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindByID(mock.Anything, garageID, receivableID).Return(nil, domain.ErrReceivableNotFound).Once()

		_, err := uc.Get(t.Context(), garageID, receivableID)
		assertAppError(t, err, apperror.CodeNotFound)
		require.Equal(t, usecase.ReasonReceivableNotFound, apperror.ReasonOf(err))
	})

	t.Run("a failed zone lookup is internal", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindByID(mock.Anything, garageID, receivableID).Return(openReceivable(day(2026, 11, 10)), nil).Once()
		d.Garages.EXPECT().Timezone(mock.Anything, garageID).Return("", errBoom).Once()

		_, err := uc.Get(t.Context(), garageID, receivableID)
		assertAppError(t, err, apperror.CodeInternal)
	})
}

func TestReceivableUseCase_Settings(t *testing.T) {
	t.Parallel()

	t.Run("reads the settings", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().GetSettings(mock.Anything, garageID).Return(domain.DefaultSettings(), nil).Once()

		s, err := uc.GetSettings(t.Context(), garageID)
		require.NoError(t, err)
		require.Equal(t, domain.DefaultSettings(), s)

		d.Repo.EXPECT().GetSettings(mock.Anything, garageID).Return(domain.Settings{}, errBoom).Once()
		_, err = uc.GetSettings(t.Context(), garageID)
		assertAppError(t, err, apperror.CodeInternal)
	})

	t.Run("saves, sorts and audits the change", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().GetSettings(mock.Anything, garageID).Return(domain.DefaultSettings(), nil).Once()
		d.Repo.EXPECT().SaveSettings(mock.Anything, garageID, domain.Settings{
			PaymentTermsDays: 14, RemindersEnabled: false, ReminderOffsetDays: []int{2, 9},
		}).Return(nil).Once()
		entry := expectAudit(d)

		s, err := uc.UpdateSettings(t.Context(), usecase.UpdateSettingsRequest{
			GarageID: garageID, ActorID: actorID, PaymentTermsDays: new(14), RemindersEnabled: new(false), ReminderOffsetDays: []int{9, 2},
		})
		require.NoError(t, err)
		require.Equal(t, []int{2, 9}, s.ReminderOffsetDays)
		require.Equal(t, usecase.ActionSettingsUpdated, entry.Action)
		require.Equal(t, actorID, entry.Actor)
		require.Equal(t, garageID, entry.EntityID)
	})

	t.Run("an incomplete or invalid request changes nothing", func(t *testing.T) {
		t.Parallel()

		uc, _ := newUseCase(t)

		for name, req := range map[string]usecase.UpdateSettingsRequest{
			"no terms":     {GarageID: garageID, RemindersEnabled: new(true)},
			"no switch":    {GarageID: garageID, PaymentTermsDays: new(7)},
			"long terms":   {GarageID: garageID, PaymentTermsDays: new(366), RemindersEnabled: new(true)},
			"bad offsets":  {GarageID: garageID, PaymentTermsDays: new(7), RemindersEnabled: new(true), ReminderOffsetDays: []int{0}},
			"on, no days":  {GarageID: garageID, PaymentTermsDays: new(7), RemindersEnabled: new(true)},
			"same offsets": {GarageID: garageID, PaymentTermsDays: new(7), RemindersEnabled: new(true), ReminderOffsetDays: []int{3, 3}},
		} {
			_, err := uc.UpdateSettings(t.Context(), req)
			assertAppError(t, err, apperror.CodeInvalidArgument)
			require.NotEmpty(t, apperror.ReasonOf(err), name)
		}
	})

	t.Run("failures are internal errors", func(t *testing.T) {
		t.Parallel()

		req := usecase.UpdateSettingsRequest{GarageID: garageID, PaymentTermsDays: new(7), RemindersEnabled: new(true), ReminderOffsetDays: []int{3}}

		uc, d := newUseCase(t)
		d.Repo.EXPECT().GetSettings(mock.Anything, garageID).Return(domain.Settings{}, errBoom).Once()

		_, err := uc.UpdateSettings(t.Context(), req)
		assertAppError(t, err, apperror.CodeInternal)

		uc, d = newUseCase(t)
		d.Repo.EXPECT().GetSettings(mock.Anything, garageID).Return(domain.DefaultSettings(), nil).Once()
		d.Repo.EXPECT().SaveSettings(mock.Anything, garageID, mock.Anything).Return(errBoom).Once()

		_, err = uc.UpdateSettings(t.Context(), req)
		assertAppError(t, err, apperror.CodeInternal)

		uc, d = newUseCase(t)
		d.Repo.EXPECT().GetSettings(mock.Anything, garageID).Return(domain.DefaultSettings(), nil).Once()
		d.Repo.EXPECT().SaveSettings(mock.Anything, garageID, mock.Anything).Return(nil).Once()
		d.Audit.EXPECT().Record(mock.Anything, mock.Anything).Return(errBoom).Once()

		_, err = uc.UpdateSettings(t.Context(), req)
		assertAppError(t, err, apperror.CodeInternal)
	})
}

func TestReceivableUseCase_IdsThatAreNotIds(t *testing.T) {
	t.Parallel()

	uc, _ := newUseCase(t)

	_, err := uc.Get(t.Context(), garageID, "abc")
	require.Equal(t, usecase.ReasonReceivableNotFound, apperror.ReasonOf(err))
	assertAppError(t, err, apperror.CodeNotFound)

	_, err = uc.List(t.Context(), usecase.ListRequest{GarageID: garageID, CustomerID: "abc"})
	assertAppError(t, err, apperror.CodeInvalidArgument)
}
