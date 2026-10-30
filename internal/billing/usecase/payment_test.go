package usecase_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/billing/domain"
	"github.com/viettechno/vietgara-backend/internal/billing/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

func TestSettlementUseCase_RecordPayment_Success(t *testing.T) {
	t.Parallel()

	t.Run("records a partial payment from the customer", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Settlements.EXPECT().FindByID(mock.Anything, garageID, settlementID).Return(openSettlement(), nil).Once()
		expectVoucher(d)

		updated := openSettlement()
		updated.PaidAmount = 300_000
		d.Settlements.EXPECT().RecordPayment(mock.Anything, garageID, settlementID, mock.Anything).Return(updated, nil).Once()
		d.Settlements.EXPECT().SaveStatus(mock.Anything, garageID, settlementID, domain.StatusPartiallyPaid).Return(nil).Once()
		entry := expectAudit(d)
		voucher := expectPaymentEvents(d, false)

		final := openSettlement()
		final.PaidAmount, final.Status = 300_000, domain.StatusPartiallyPaid
		d.Settlements.EXPECT().FindByID(mock.Anything, garageID, settlementID).Return(final, nil).Once()

		s, err := uc.RecordPayment(t.Context(), usecase.RecordPaymentRequest{
			GarageID: garageID, SettlementID: settlementID, ActorID: actorID,
			Amount: 300_000, Method: domain.MethodCash,
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
		require.Equal(t, int64(0), before["paid_amount"])
		require.Equal(t, int64(300_000), after["paid_amount"])
		require.Equal(t, domain.StatusPartiallyPaid, after["status"])
		require.Equal(t, map[string]any{
			"voucher_number": "PT-202609-0007", "amount": int64(300_000), "method": domain.MethodCash, "note": "",
			"payer_name": "An Nguyen",
		}, after["payment"])
		require.Equal(t, domain.StatusPartiallyPaid, s.Status)
		require.Equal(t, int64(300_000), (*voucher)["amount"])
		require.Equal(t, int64(700_000), (*voucher)["balance"])
		require.Equal(t, "vehicle-1", (*voucher)["vehicle_id"])
		require.NotContains(t, *voucher, "created_by", "no creator is known")
	})

	t.Run("fully paid", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Settlements.EXPECT().FindByID(mock.Anything, garageID, settlementID).Return(openSettlement(), nil).Once()
		expectVoucher(d)

		updated := openSettlement()
		updated.PaidAmount = 1_000_000
		d.Settlements.EXPECT().RecordPayment(mock.Anything, garageID, settlementID, mock.Anything).Return(updated, nil).Once()
		d.Settlements.EXPECT().SaveStatus(mock.Anything, garageID, settlementID, domain.StatusPaid).Return(nil).Once()
		expectAudit(d)
		expectPaymentEvents(d, true)

		final := openSettlement()
		final.PaidAmount, final.Status = 1_000_000, domain.StatusPaid
		d.Settlements.EXPECT().FindByID(mock.Anything, garageID, settlementID).Return(final, nil).Once()

		s, err := uc.RecordPayment(t.Context(), usecase.RecordPaymentRequest{
			GarageID: garageID, SettlementID: settlementID, Amount: 1_000_000, Method: domain.MethodBankTransfer,
		})
		require.NoError(t, err)
		require.Equal(t, domain.StatusPaid, s.Status)
	})
}

func TestSettlementUseCase_RecordPayment_Errors(t *testing.T) {
	t.Parallel()

	t.Run("invalid payment method", func(t *testing.T) {
		t.Parallel()

		uc, _ := newUseCase(t)

		_, err := uc.RecordPayment(t.Context(), usecase.RecordPaymentRequest{
			GarageID: garageID, SettlementID: settlementID, Amount: 1, Method: "check",
		})
		assertAppError(t, err, apperror.CodeInvalidArgument)
	})

	t.Run("settlement not found", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Settlements.EXPECT().FindByID(mock.Anything, garageID, "missing").Return(nil, domain.ErrSettlementNotFound).Once()

		_, err := uc.RecordPayment(t.Context(), usecase.RecordPaymentRequest{
			GarageID: garageID, SettlementID: "missing", Amount: 1, Method: domain.MethodCash,
		})
		assertAppError(t, err, apperror.CodeNotFound)
	})

	t.Run("amount exceeds what is still due", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Settlements.EXPECT().FindByID(mock.Anything, garageID, settlementID).Return(openSettlement(), nil).Once()

		_, err := uc.RecordPayment(t.Context(), usecase.RecordPaymentRequest{
			GarageID: garageID, SettlementID: settlementID, Amount: 1_100_000, Method: domain.MethodCash,
		})
		assertAppError(t, err, apperror.CodeInvalidArgument)
	})

	t.Run("non-positive amount", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Settlements.EXPECT().FindByID(mock.Anything, garageID, settlementID).Return(openSettlement(), nil).Once()

		_, err := uc.RecordPayment(t.Context(), usecase.RecordPaymentRequest{
			GarageID: garageID, SettlementID: settlementID, Amount: 0, Method: domain.MethodCash,
		})
		assertAppError(t, err, apperror.CodeInvalidArgument)
	})

	t.Run("record payment repository failure", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Settlements.EXPECT().FindByID(mock.Anything, garageID, settlementID).Return(openSettlement(), nil).Once()
		expectVoucher(d)
		d.Settlements.EXPECT().RecordPayment(mock.Anything, garageID, settlementID, mock.Anything).Return(nil, errBoom).Once()

		_, err := uc.RecordPayment(t.Context(), usecase.RecordPaymentRequest{
			GarageID: garageID, SettlementID: settlementID, Amount: 300_000, Method: domain.MethodCash,
		})
		assertAppError(t, err, apperror.CodeInternal)
	})

	t.Run("save status failure", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Settlements.EXPECT().FindByID(mock.Anything, garageID, settlementID).Return(openSettlement(), nil).Once()
		expectVoucher(d)

		updated := openSettlement()
		updated.PaidAmount = 300_000
		d.Settlements.EXPECT().RecordPayment(mock.Anything, garageID, settlementID, mock.Anything).Return(updated, nil).Once()
		d.Settlements.EXPECT().SaveStatus(mock.Anything, garageID, settlementID, domain.StatusPartiallyPaid).Return(errBoom).Once()

		_, err := uc.RecordPayment(t.Context(), usecase.RecordPaymentRequest{
			GarageID: garageID, SettlementID: settlementID, Amount: 300_000, Method: domain.MethodCash,
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
	d.Settlements.EXPECT().FindByID(mock.Anything, garageID, settlementID).Return(openSettlement(), nil).Once()
	expectVoucher(d)

	updated := openSettlement()
	updated.PaidAmount = 300_000
	d.Settlements.EXPECT().RecordPayment(mock.Anything, garageID, settlementID, mock.Anything).Return(updated, nil).Once()
	d.Settlements.EXPECT().SaveStatus(mock.Anything, garageID, settlementID, domain.StatusPartiallyPaid).Return(nil).Once()
	d.Audit.EXPECT().Record(mock.Anything, mock.Anything).Return(errBoom).Once()

	_, err := uc.RecordPayment(t.Context(), usecase.RecordPaymentRequest{
		GarageID: garageID, SettlementID: settlementID, ActorID: actorID,
		Amount: 300_000, Method: domain.MethodCash,
	})
	assertAppError(t, err, apperror.CodeInternal)
}

func TestSettlementUseCase_RecordPayment_Voucher(t *testing.T) {
	t.Parallel()

	t.Run("issues a numbered voucher with the garage's identity and the collector", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Settlements.EXPECT().FindByID(mock.Anything, garageID, settlementID).Return(openSettlement(), nil).Once()
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(defaultGarage(), nil).Once()
		d.Sequences.EXPECT().Next(mock.Anything, garageID, "payment_voucher", "PT-202609-{SEQ:4}").Return(12, nil).Once()
		d.Collectors.EXPECT().FullName(mock.Anything, actorID).Return("Le Thu Ngan", nil).Once()

		var issued domain.Payment

		d.Settlements.EXPECT().RecordPayment(mock.Anything, garageID, settlementID, mock.Anything).
			Run(func(_ context.Context, _, _ string, p *domain.Payment) { issued = *p }).
			Return(func() *domain.Settlement { s := openSettlement(); s.PaidAmount = 100_000; return s }(), nil).Once()
		d.Settlements.EXPECT().SaveStatus(mock.Anything, garageID, settlementID, domain.StatusPartiallyPaid).Return(nil).Once()
		expectAudit(d)
		expectPaymentEvents(d, false)
		d.Settlements.EXPECT().FindByID(mock.Anything, garageID, settlementID).Return(openSettlement(), nil).Once()

		_, err := uc.RecordPayment(t.Context(), usecase.RecordPaymentRequest{
			GarageID: garageID, SettlementID: settlementID, ActorID: actorID, Amount: 100_000, Method: domain.MethodCash,
		})
		require.NoError(t, err)

		assert.Equal(t, "PT-202609-0012", issued.VoucherNumber)
		assert.Equal(t, "An Nguyen", issued.PayerName, "defaults to the customer's name")
		assert.Empty(t, issued.PayerAddress)
		assert.Equal(t, "Thanh toán quyết toán QT-202609-0001", issued.Reason)
		assert.Equal(t, "Le Thu Ngan", issued.CollectorName)
		assert.Equal(t, "Cong ty TNHH An Phat", issued.Issuer.Name)
		assert.Equal(t, "0301234567", issued.Issuer.TaxCode)
	})

	t.Run("takes the payer and reason from the request", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Settlements.EXPECT().FindByID(mock.Anything, garageID, settlementID).Return(openSettlement(), nil).Once()
		expectVoucher(d)

		var issued domain.Payment

		d.Settlements.EXPECT().RecordPayment(mock.Anything, garageID, settlementID, mock.Anything).
			Run(func(_ context.Context, _, _ string, p *domain.Payment) { issued = *p }).
			Return(func() *domain.Settlement { s := openSettlement(); s.PaidAmount = 100_000; return s }(), nil).Once()
		d.Settlements.EXPECT().SaveStatus(mock.Anything, garageID, settlementID, domain.StatusPartiallyPaid).Return(nil).Once()
		expectAudit(d)
		expectPaymentEvents(d, false)
		d.Settlements.EXPECT().FindByID(mock.Anything, garageID, settlementID).Return(openSettlement(), nil).Once()

		_, err := uc.RecordPayment(t.Context(), usecase.RecordPaymentRequest{
			GarageID: garageID, SettlementID: settlementID, ActorID: actorID, Amount: 100_000, Method: domain.MethodCash,
			PayerName: "  Cong ty Van Tai A ", PayerAddress: " 5 Le Loi ", Reason: " Dat coc sua chua ",
		})
		require.NoError(t, err)

		assert.Equal(t, "Cong ty Van Tai A", issued.PayerName)
		assert.Equal(t, "5 Le Loi", issued.PayerAddress)
		assert.Equal(t, "Dat coc sua chua", issued.Reason)
	})

	t.Run("a payer, address or reason that is too long", func(t *testing.T) {
		t.Parallel()

		long := string(make([]rune, 600))

		for name, req := range map[string]usecase.RecordPaymentRequest{
			"payer":   {PayerName: long},
			"address": {PayerAddress: long},
			"reason":  {Reason: long},
		} {
			uc, d := newUseCase(t)
			d.Settlements.EXPECT().FindByID(mock.Anything, garageID, settlementID).Return(openSettlement(), nil).Once()

			req.GarageID, req.SettlementID, req.Amount, req.Method = garageID, settlementID, 100_000, domain.MethodCash

			_, err := uc.RecordPayment(t.Context(), req)
			assertAppError(t, err, apperror.CodeInvalidArgument)
			require.ErrorIs(t, err, domain.ErrInvalidVoucher, name)
		}
	})

	t.Run("voucher reads that fail", func(t *testing.T) {
		t.Parallel()

		req := usecase.RecordPaymentRequest{
			GarageID: garageID, SettlementID: settlementID, ActorID: actorID, Amount: 100_000, Method: domain.MethodCash,
		}

		uc, d := newUseCase(t)
		d.Settlements.EXPECT().FindByID(mock.Anything, garageID, settlementID).Return(openSettlement(), nil).Once()
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(usecase.GarageInfo{}, errBoom).Once()

		_, err := uc.RecordPayment(t.Context(), req)
		assertAppError(t, err, apperror.CodeInternal)

		uc, d = newUseCase(t)
		d.Settlements.EXPECT().FindByID(mock.Anything, garageID, settlementID).Return(openSettlement(), nil).Once()
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(defaultGarage(), nil).Once()
		d.Sequences.EXPECT().Next(mock.Anything, garageID, "payment_voucher", mock.Anything).Return(0, errBoom).Once()

		_, err = uc.RecordPayment(t.Context(), req)
		assertAppError(t, err, apperror.CodeInternal)

		uc, d = newUseCase(t)
		d.Settlements.EXPECT().FindByID(mock.Anything, garageID, settlementID).Return(openSettlement(), nil).Once()
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(defaultGarage(), nil).Once()
		d.Sequences.EXPECT().Next(mock.Anything, garageID, "payment_voucher", mock.Anything).Return(1, nil).Once()
		d.Collectors.EXPECT().FullName(mock.Anything, actorID).Return("", errBoom).Once()

		_, err = uc.RecordPayment(t.Context(), req)
		assertAppError(t, err, apperror.CodeInternal)
	})
}

func TestSettlementUseCase_VoidPayment(t *testing.T) {
	t.Parallel()

	paid := func() *domain.Settlement {
		s := openSettlement()
		s.PaidAmount, s.Status = 400_000, domain.StatusPartiallyPaid
		s.Payments = []domain.Payment{{ID: "p-1", Amount: 400_000, Method: domain.MethodCash, VoucherNumber: "PT-0001", PayerName: "An"}}

		return s
	}
	request := usecase.VoidPaymentRequest{
		GarageID: garageID, SettlementID: settlementID, PaymentID: "p-1", ActorID: actorID, Reason: "  nhập nhầm  ",
	}

	t.Run("voids it, recalculates the status and audits who did", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Settlements.EXPECT().FindByID(mock.Anything, garageID, settlementID).Return(paid(), nil).Once()
		d.Settlements.EXPECT().VoidPayment(mock.Anything, garageID, settlementID, "p-1", "nhập nhầm").Return(openSettlement(), nil).Once()
		d.Settlements.EXPECT().SaveStatus(mock.Anything, garageID, settlementID, domain.StatusPending).Return(nil).Once()
		entry := expectAudit(d)
		d.Settlements.EXPECT().FindByID(mock.Anything, garageID, settlementID).Return(openSettlement(), nil).Once()

		s, err := uc.VoidPayment(t.Context(), request)
		require.NoError(t, err)
		require.Equal(t, domain.StatusPending, s.Status)
		require.Equal(t, usecase.ActionPaymentVoided, entry.Action)
		require.Equal(t, "p-1", entry.EntityID)
		require.Equal(t, actorID, entry.Actor)

		before, ok := entry.Before.(map[string]any)
		require.True(t, ok)
		after, ok := entry.After.(map[string]any)
		require.True(t, ok)
		require.Equal(t, int64(400_000), before["paid_amount"])
		require.Equal(t, int64(0), after["paid_amount"])
		require.Equal(t, domain.StatusPending, after["status"])
	})

	t.Run("a reason is required", func(t *testing.T) {
		t.Parallel()

		uc, _ := newUseCase(t)
		req := request
		req.Reason = "   "

		_, err := uc.VoidPayment(t.Context(), req)
		assertAppError(t, err, apperror.CodeInvalidArgument)

		req.Reason = strings.Repeat("x", domain.MaxVoidReasonLength+1)
		_, err = uc.VoidPayment(t.Context(), req)
		assertAppError(t, err, apperror.CodeInvalidArgument)
	})

	t.Run("unknown settlement", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Settlements.EXPECT().FindByID(mock.Anything, garageID, settlementID).Return(nil, domain.ErrSettlementNotFound).Once()

		_, err := uc.VoidPayment(t.Context(), request)
		assertAppError(t, err, apperror.CodeNotFound)
	})

	t.Run("unknown payment", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Settlements.EXPECT().FindByID(mock.Anything, garageID, settlementID).Return(openSettlement(), nil).Once()

		_, err := uc.VoidPayment(t.Context(), request)
		assertAppError(t, err, apperror.CodeNotFound)
	})

	t.Run("already void", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Settlements.EXPECT().FindByID(mock.Anything, garageID, settlementID).Return(paid(), nil).Once()
		d.Settlements.EXPECT().VoidPayment(mock.Anything, garageID, settlementID, "p-1", "nhập nhầm").
			Return(nil, domain.ErrPaymentAlreadyVoided).Once()

		_, err := uc.VoidPayment(t.Context(), request)
		assertAppError(t, err, apperror.CodeAlreadyExists)
	})

	t.Run("status save failure", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Settlements.EXPECT().FindByID(mock.Anything, garageID, settlementID).Return(paid(), nil).Once()
		d.Settlements.EXPECT().VoidPayment(mock.Anything, garageID, settlementID, "p-1", "nhập nhầm").Return(openSettlement(), nil).Once()
		d.Settlements.EXPECT().SaveStatus(mock.Anything, garageID, settlementID, domain.StatusPending).Return(errBoom).Once()

		_, err := uc.VoidPayment(t.Context(), request)
		assertAppError(t, err, apperror.CodeInternal)
	})

	t.Run("audit failure", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Settlements.EXPECT().FindByID(mock.Anything, garageID, settlementID).Return(paid(), nil).Once()
		d.Settlements.EXPECT().VoidPayment(mock.Anything, garageID, settlementID, "p-1", "nhập nhầm").Return(openSettlement(), nil).Once()
		d.Settlements.EXPECT().SaveStatus(mock.Anything, garageID, settlementID, domain.StatusPending).Return(nil).Once()
		d.Audit.EXPECT().Record(mock.Anything, mock.Anything).Return(errBoom).Once()

		_, err := uc.VoidPayment(t.Context(), request)
		assertAppError(t, err, apperror.CodeInternal)
	})
}
