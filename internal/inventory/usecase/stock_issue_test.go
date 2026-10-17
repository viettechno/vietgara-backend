package usecase_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
	"github.com/viettechno/vietgara-backend/internal/inventory/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

func TestInventoryUseCase_PostStockIssue(t *testing.T) {
	t.Parallel()

	t.Run("posts one movement per part, summing its lines", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindStockIssue(mock.Anything, garageID, issueID, true).
			Return(draftIssue(issueLine("l1", partID, "2"), issueLine("l2", partID, "1.5")), nil).Once()
		expectPosting(d, "3.5", "10000")
		d.Parts.EXPECT().Parts(mock.Anything, garageID, mock.Anything).Return([]usecase.PartInfo{samplePart()}, nil).Once()
		d.Parts.EXPECT().UnitAbbreviations(mock.Anything, garageID, mock.Anything).Return(nil, nil).Maybe()
		d.Directory.EXPECT().UserName(mock.Anything, actorID).Return("Lan Nguyen", nil).Once()

		issue, err := uc.PostStockIssue(t.Context(), garageID, issueID, actorID)
		require.NoError(t, err)
		assert.Equal(t, domain.StockIssuePosted, issue.Status)
		assert.Equal(t, "BEER-330", issue.Lines[0].PartSKU)
	})

	t.Run("names who posted it", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		posted := draftIssue(issueLine("l1", partID, "2"))
		posted.Status, posted.PostedBy = domain.StockIssuePosted, new(actorID)
		d.Repo.EXPECT().FindStockIssueByRepairOrder(mock.Anything, garageID, repairOrderID, false).Return(posted, nil).Once()
		d.Parts.EXPECT().Parts(mock.Anything, garageID, mock.Anything).Return([]usecase.PartInfo{samplePart()}, nil).Once()
		d.Directory.EXPECT().UserName(mock.Anything, actorID).Return("Lan Nguyen", nil).Once()

		issue, err := uc.GetRepairOrderStockIssue(t.Context(), garageID, repairOrderID)
		require.NoError(t, err)
		assert.Equal(t, "Lan Nguyen", issue.PostedByName)
	})

	t.Run("a posted or unknown issue is refused", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		posted := draftIssue()
		posted.Status = domain.StockIssuePosted
		d.Repo.EXPECT().FindStockIssue(mock.Anything, garageID, issueID, true).Return(posted, nil).Once()

		_, err := uc.PostStockIssue(t.Context(), garageID, issueID, actorID)
		assertAppError(t, err, apperror.CodeFailedPrecondition)
		assert.Equal(t, usecase.ReasonStockIssueNotDraft, apperror.ReasonOf(err))

		d.Repo.EXPECT().FindStockIssue(mock.Anything, garageID, issueID, true).Return(nil, domain.ErrStockIssueNotFound).Once()
		_, err = uc.PostStockIssue(t.Context(), garageID, issueID, actorID)
		assertAppError(t, err, apperror.CodeNotFound)
	})

	t.Run("failures while posting", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindStockIssue(mock.Anything, garageID, issueID, true).
			RunAndReturn(func(context.Context, string, string, bool) (*domain.StockIssue, error) {
				return draftIssue(issueLine("l1", partID, "2")), nil
			}).Times(4)

		d.Repo.EXPECT().RecordTransaction(mock.Anything, mock.Anything).Return(errBoom).Once()

		_, err := uc.PostStockIssue(t.Context(), garageID, issueID, actorID)
		assertAppError(t, err, apperror.CodeInternal)

		d.Repo.EXPECT().RecordTransaction(mock.Anything, mock.Anything).Return(nil).Times(3)
		d.Repo.EXPECT().SaveStockIssue(mock.Anything, mock.Anything).Return(errBoom).Once()

		_, err = uc.PostStockIssue(t.Context(), garageID, issueID, actorID)
		assertAppError(t, err, apperror.CodeInternal)

		d.Repo.EXPECT().SaveStockIssue(mock.Anything, mock.Anything).Return(nil).Twice()
		d.Audit.EXPECT().Record(mock.Anything, mock.Anything).Return(errBoom).Once()

		_, err = uc.PostStockIssue(t.Context(), garageID, issueID, actorID)
		assertAppError(t, err, apperror.CodeInternal)

		d.Audit.EXPECT().Record(mock.Anything, mock.Anything).Return(nil).Once()
		d.Events.EXPECT().Publish(mock.Anything, usecase.EventStockIssued, garageID, actorID, mock.Anything).Return(errBoom).Once()
		_, err = uc.PostStockIssue(t.Context(), garageID, issueID, actorID)
		assertAppError(t, err, apperror.CodeInternal)
	})

	t.Run("get failure", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindStockIssueByRepairOrder(mock.Anything, garageID, repairOrderID, false).Return(nil, domain.ErrStockIssueNotFound).Once()

		_, err := uc.GetRepairOrderStockIssue(t.Context(), garageID, repairOrderID)
		assertAppError(t, err, apperror.CodeNotFound)
	})
}
