package usecase_test

import (
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/billing/domain"
	"github.com/viettechno/vietgara-backend/internal/billing/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

func allocatedSettlement() *domain.Settlement {
	s := openSettlement()
	s.CustomerAmount, s.InsurerAmount = 600_000, 400_000
	insurerPartnerID := insurerID
	s.InsurerPartnerID = &insurerPartnerID

	return s
}

func TestSettlementUseCase_RecordPayment_Success(t *testing.T) {
	t.Parallel()

	t.Run("records a partial payment from the customer", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Settlements.EXPECT().FindByID(mock.Anything, garageID, settlementID).Return(allocatedSettlement(), nil).Once()

		updated := allocatedSettlement()
		updated.CustomerPaidAmount = 300_000
		d.Settlements.EXPECT().RecordPayment(mock.Anything, garageID, settlementID, mock.Anything).Return(updated, nil).Once()
		d.Settlements.EXPECT().SaveStatus(mock.Anything, garageID, settlementID, domain.StatusPartiallyPaid).Return(nil).Once()
		entry := expectAudit(d)

		final := allocatedSettlement()
		final.CustomerPaidAmount, final.Status = 300_000, domain.StatusPartiallyPaid
		d.Settlements.EXPECT().FindByID(mock.Anything, garageID, settlementID).Return(final, nil).Once()

		s, err := uc.RecordPayment(t.Context(), usecase.RecordPaymentRequest{
			GarageID: garageID, SettlementID: settlementID, ActorID: actorID,
			PayerType: domain.PayerCustomer, Amount: 300_000, Method: domain.MethodCash,
		})
		require.NoError(t, err)
		require.Equal(t, usecase.ActionPaymentRecorded, entry.Action)
		require.Equal(t, "settlement_payment", entry.EntityType)
		require.NotEmpty(t, entry.EntityID, "the payment's id")
		require.Equal(t, actorID, entry.Actor)
		before, ok := entry.Before.(map[string]any)
		require.True(t, ok)
		after, ok := entry.After.(map[string]any)
		require.True(t, ok)
		require.Equal(t, int64(0), before["customer_paid_amount"])
		require.Equal(t, int64(300_000), after["customer_paid_amount"])
		require.Equal(t, domain.StatusPartiallyPaid, after["status"])
		require.Equal(t, map[string]any{
			"payer_type": domain.PayerCustomer, "amount": int64(300_000), "method": domain.MethodCash, "note": "",
		}, after["payment"])
		require.Equal(t, domain.StatusPartiallyPaid, s.Status)
	})

	t.Run("fully paid by insurer", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Settlements.EXPECT().FindByID(mock.Anything, garageID, settlementID).Return(allocatedSettlement(), nil).Once()

		updated := allocatedSettlement()
		updated.CustomerPaidAmount, updated.InsurerPaidAmount = 600_000, 400_000
		d.Settlements.EXPECT().RecordPayment(mock.Anything, garageID, settlementID, mock.Anything).Return(updated, nil).Once()
		d.Settlements.EXPECT().SaveStatus(mock.Anything, garageID, settlementID, domain.StatusPaid).Return(nil).Once()
		expectAudit(d)

		final := allocatedSettlement()
		final.CustomerPaidAmount, final.InsurerPaidAmount, final.Status = 600_000, 400_000, domain.StatusPaid
		d.Settlements.EXPECT().FindByID(mock.Anything, garageID, settlementID).Return(final, nil).Once()

		s, err := uc.RecordPayment(t.Context(), usecase.RecordPaymentRequest{
			GarageID: garageID, SettlementID: settlementID, PayerType: domain.PayerInsurer, Amount: 400_000, Method: domain.MethodBankTransfer,
		})
		require.NoError(t, err)
		require.Equal(t, domain.StatusPaid, s.Status)
	})
}

func TestSettlementUseCase_RecordPayment_Errors(t *testing.T) {
	t.Parallel()

	t.Run("invalid payer type", func(t *testing.T) {
		t.Parallel()

		uc, _ := newUseCase(t)

		_, err := uc.RecordPayment(t.Context(), usecase.RecordPaymentRequest{
			GarageID: garageID, SettlementID: settlementID, PayerType: "vendor", Amount: 1, Method: domain.MethodCash,
		})
		assertAppError(t, err, apperror.CodeInvalidArgument)
	})

	t.Run("invalid payment method", func(t *testing.T) {
		t.Parallel()

		uc, _ := newUseCase(t)

		_, err := uc.RecordPayment(t.Context(), usecase.RecordPaymentRequest{
			GarageID: garageID, SettlementID: settlementID, PayerType: domain.PayerCustomer, Amount: 1, Method: "check",
		})
		assertAppError(t, err, apperror.CodeInvalidArgument)
	})

	t.Run("settlement not found", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Settlements.EXPECT().FindByID(mock.Anything, garageID, "missing").Return(nil, domain.ErrSettlementNotFound).Once()

		_, err := uc.RecordPayment(t.Context(), usecase.RecordPaymentRequest{
			GarageID: garageID, SettlementID: "missing", PayerType: domain.PayerCustomer, Amount: 1, Method: domain.MethodCash,
		})
		assertAppError(t, err, apperror.CodeNotFound)
	})

	t.Run("amount exceeds what is still due", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Settlements.EXPECT().FindByID(mock.Anything, garageID, settlementID).Return(allocatedSettlement(), nil).Once()

		_, err := uc.RecordPayment(t.Context(), usecase.RecordPaymentRequest{
			GarageID: garageID, SettlementID: settlementID, PayerType: domain.PayerCustomer, Amount: 700_000, Method: domain.MethodCash,
		})
		assertAppError(t, err, apperror.CodeInvalidArgument)
	})

	t.Run("non-positive amount", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Settlements.EXPECT().FindByID(mock.Anything, garageID, settlementID).Return(allocatedSettlement(), nil).Once()

		_, err := uc.RecordPayment(t.Context(), usecase.RecordPaymentRequest{
			GarageID: garageID, SettlementID: settlementID, PayerType: domain.PayerCustomer, Amount: 0, Method: domain.MethodCash,
		})
		assertAppError(t, err, apperror.CodeInvalidArgument)
	})

	t.Run("record payment repository failure", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Settlements.EXPECT().FindByID(mock.Anything, garageID, settlementID).Return(allocatedSettlement(), nil).Once()
		d.Settlements.EXPECT().RecordPayment(mock.Anything, garageID, settlementID, mock.Anything).Return(nil, errBoom).Once()

		_, err := uc.RecordPayment(t.Context(), usecase.RecordPaymentRequest{
			GarageID: garageID, SettlementID: settlementID, PayerType: domain.PayerCustomer, Amount: 300_000, Method: domain.MethodCash,
		})
		assertAppError(t, err, apperror.CodeInternal)
	})

	t.Run("save status failure", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Settlements.EXPECT().FindByID(mock.Anything, garageID, settlementID).Return(allocatedSettlement(), nil).Once()

		updated := allocatedSettlement()
		updated.CustomerPaidAmount = 300_000
		d.Settlements.EXPECT().RecordPayment(mock.Anything, garageID, settlementID, mock.Anything).Return(updated, nil).Once()
		d.Settlements.EXPECT().SaveStatus(mock.Anything, garageID, settlementID, domain.StatusPartiallyPaid).Return(errBoom).Once()

		_, err := uc.RecordPayment(t.Context(), usecase.RecordPaymentRequest{
			GarageID: garageID, SettlementID: settlementID, PayerType: domain.PayerCustomer, Amount: 300_000, Method: domain.MethodCash,
		})
		assertAppError(t, err, apperror.CodeInternal)
	})
}

// TestSettlementUseCase_AuditFailure: a change whose audit entry cannot be
// written fails, so its transaction rolls back and no unaudited change of
// money is ever committed.
func TestSettlementUseCase_AuditFailure(t *testing.T) {
	t.Parallel()

	uc, d := newUseCase(t)
	d.Settlements.EXPECT().FindByID(mock.Anything, garageID, settlementID).Return(allocatedSettlement(), nil).Once()

	updated := allocatedSettlement()
	updated.CustomerPaidAmount = 300_000
	d.Settlements.EXPECT().RecordPayment(mock.Anything, garageID, settlementID, mock.Anything).Return(updated, nil).Once()
	d.Settlements.EXPECT().SaveStatus(mock.Anything, garageID, settlementID, domain.StatusPartiallyPaid).Return(nil).Once()
	d.Audit.EXPECT().Record(mock.Anything, mock.Anything).Return(errBoom).Once()

	_, err := uc.RecordPayment(t.Context(), usecase.RecordPaymentRequest{
		GarageID: garageID, SettlementID: settlementID, ActorID: actorID,
		PayerType: domain.PayerCustomer, Amount: 300_000, Method: domain.MethodCash,
	})
	assertAppError(t, err, apperror.CodeInternal)
}
