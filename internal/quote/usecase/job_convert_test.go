package usecase_test

import (
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/quote/domain"
	"github.com/viettechno/vietgara-backend/pkg/billingparty"
)

func TestQuoteUseCase_ConvertCarriesTheConsumablesOfApprovedJobs(t *testing.T) {
	t.Parallel()

	uc, d := newUseCase(t)
	q := quoteWithJob(new(jobID))
	q.Status = domain.StatusApproved
	q.Lines[0].Decision = domain.DecisionApproved
	q.Lines[1].Decision = domain.DecisionRejected
	q.Lines[2].Decision = domain.DecisionRejected
	q.Lines[3].Decision = domain.DecisionApproved

	d.Quotes.EXPECT().FindByID(mock.Anything, garageID, quoteID).Return(q, nil).Once()
	d.Garages.EXPECT().Get(mock.Anything, garageID).Return(defaultGarage(), nil).Once()
	d.Limits.EXPECT().ConsumeTransaction(mock.Anything, ownerID).Return(nil).Once()
	d.Sequences.EXPECT().Next(mock.Anything, garageID, "repair_order", "LSC-202609-{SEQ:4}").Return(1, nil).Once()
	d.RepairOrders.EXPECT().Create(mock.Anything, garageID, quoteID, vehicleID, "An Nguyen", "51F-123.45", "LSC-202609-0001", actorID,
		billingparty.Party{}, mock.MatchedBy(func(lines []domain.Line) bool {
			return len(lines) == 2 && lines[0].ID == "labor" && lines[1].ID == "grease" && lines[1].NonBillable && lines[1].Position == 2
		}),
	).Return("ro-1", nil).Once()
	d.Quotes.EXPECT().Save(mock.Anything, mock.Anything).Return(nil).Once()

	_, err := uc.Convert(t.Context(), garageID, quoteID, actorID)
	require.NoError(t, err)
}
