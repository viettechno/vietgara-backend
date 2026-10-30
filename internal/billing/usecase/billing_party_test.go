package usecase_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/billing/domain"
	"github.com/viettechno/vietgara-backend/internal/billing/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
	"github.com/viettechno/vietgara-backend/pkg/billingparty"
)

var (
	completeParty = billingparty.Party{
		TaxProfileID: "tp-1", Kind: "company", LegalName: "Cong ty Van Tai A", TaxID: "0301234567", Address: "12 Le Loi",
	}
	incompleteParty = billingparty.Party{TaxProfileID: "tp-2", Kind: "personal", LegalName: "Tran Binh"}
)

func expectCreate(d *deps, ro usecase.RepairOrderInfo) {
	d.Settlements.EXPECT().FindByRepairOrderID(mock.Anything, garageID, repairOrderID).Return(nil, domain.ErrSettlementNotFound).Once()
	d.RepairOrders.EXPECT().Get(mock.Anything, garageID, repairOrderID).Return(ro, nil).Once()
	d.Garages.EXPECT().Get(mock.Anything, garageID).Return(defaultGarage(), nil).Once()
	d.Limits.EXPECT().ConsumeTransaction(mock.Anything, ownerID).Return(nil).Once()
	d.Sequences.EXPECT().Next(mock.Anything, garageID, "settlement", "QT-202609-{SEQ:4}").Return(1, nil).Once()
	d.Settlements.EXPECT().Create(mock.Anything, mock.Anything).Return(nil).Once()
	d.Events.EXPECT().Publish(mock.Anything, usecase.EventSettlementCreated, garageID, "", mock.Anything).Return(nil).Once()
	expectAudit(d)
}

func TestSettlementUseCase_Create_BillingParty(t *testing.T) {
	t.Parallel()

	t.Run("starts from the repair order's party", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		ro := closedRepairOrder()
		ro.BillingParty = completeParty
		expectCreate(d, ro)

		s, err := uc.Create(t.Context(), usecase.CreateSettlementRequest{GarageID: garageID, ActorID: actorID, RepairOrderID: repairOrderID})
		require.NoError(t, err)
		assert.Equal(t, completeParty, s.BillingParty)
		assert.Empty(t, d.resolved, "nothing to resolve")
	})

	t.Run("an ad-hoc repair order takes the customer's default", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.party = func(*string) (billingparty.Party, error) { return completeParty, nil }
		expectCreate(d, closedRepairOrder())

		s, err := uc.Create(t.Context(), usecase.CreateSettlementRequest{GarageID: garageID, RepairOrderID: repairOrderID})
		require.NoError(t, err)
		assert.Equal(t, completeParty, s.BillingParty)
		require.Len(t, d.resolved, 1)
		assert.Nil(t, d.resolved[0])
	})

	t.Run("the request's profile wins, and an invoice may be requested", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.party = func(*string) (billingparty.Party, error) { return completeParty, nil }
		ro := closedRepairOrder()
		ro.BillingParty = incompleteParty
		expectCreate(d, ro)

		s, err := uc.Create(t.Context(), usecase.CreateSettlementRequest{
			GarageID: garageID, RepairOrderID: repairOrderID, BillingTaxProfileID: new("tp-1"), InvoiceRequested: true,
		})
		require.NoError(t, err)
		assert.Equal(t, completeParty, s.BillingParty)
		assert.True(t, s.InvoiceRequested)
	})

	t.Run("an invoice for an incomplete party is refused before anything is consumed", func(t *testing.T) {
		t.Parallel()

		for name, party := range map[string]billingparty.Party{"incomplete": incompleteParty, "retail": {}} {
			uc, d := newUseCase(t)
			ro := closedRepairOrder()
			ro.BillingParty = party

			d.Settlements.EXPECT().FindByRepairOrderID(mock.Anything, garageID, repairOrderID).Return(nil, domain.ErrSettlementNotFound).Once()
			d.RepairOrders.EXPECT().Get(mock.Anything, garageID, repairOrderID).Return(ro, nil).Once()
			d.party = func(*string) (billingparty.Party, error) { return party, nil }

			_, err := uc.Create(t.Context(), usecase.CreateSettlementRequest{
				GarageID: garageID, RepairOrderID: repairOrderID, InvoiceRequested: true,
			})
			require.Error(t, err, name)
			assert.Equal(t, usecase.ReasonInvoiceDataIncomplete, apperror.ReasonOf(err), name)
			assertAppError(t, err, apperror.CodeFailedPrecondition)
		}
	})

	t.Run("an unusable profile", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.party = func(*string) (billingparty.Party, error) {
			return billingparty.Party{}, apperror.New(apperror.CodeInvalidArgument, "not the customer's")
		}
		d.Settlements.EXPECT().FindByRepairOrderID(mock.Anything, garageID, repairOrderID).Return(nil, domain.ErrSettlementNotFound).Once()
		d.RepairOrders.EXPECT().Get(mock.Anything, garageID, repairOrderID).Return(closedRepairOrder(), nil).Once()

		_, err := uc.Create(t.Context(), usecase.CreateSettlementRequest{
			GarageID: garageID, RepairOrderID: repairOrderID, BillingTaxProfileID: new("x"),
		})
		assertAppError(t, err, apperror.CodeInvalidArgument)
	})
}

func TestSettlementUseCase_Update(t *testing.T) {
	t.Parallel()

	t.Run("changes the party and requests an invoice, audited", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.party = func(*string) (billingparty.Party, error) { return completeParty, nil }
		d.Settlements.EXPECT().FindByID(mock.Anything, garageID, settlementID).Return(openSettlement(), nil).Once()
		d.RepairOrders.EXPECT().Get(mock.Anything, garageID, repairOrderID).Return(closedRepairOrder(), nil).Once()
		d.Settlements.EXPECT().SaveBilling(mock.Anything, mock.Anything).Return(nil).Once()
		entry := expectAudit(d)

		s, err := uc.Update(t.Context(), usecase.UpdateSettlementRequest{
			GarageID: garageID, SettlementID: settlementID, ActorID: actorID,
			BillingTaxProfileID: new("tp-1"), InvoiceRequested: new(true),
		})
		require.NoError(t, err)
		assert.Equal(t, completeParty, s.BillingParty)
		assert.True(t, s.InvoiceRequested)
		assert.Equal(t, usecase.ActionBillingUpdated, entry.Action)

		after, ok := entry.After.(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "tp-1", after["billing_tax_profile_id"])
		assert.Equal(t, true, after["invoice_requested"])
	})

	t.Run("an invoice needs a complete party", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Settlements.EXPECT().FindByID(mock.Anything, garageID, settlementID).Return(openSettlement(), nil).Once()

		_, err := uc.Update(t.Context(), usecase.UpdateSettlementRequest{
			GarageID: garageID, SettlementID: settlementID, InvoiceRequested: new(true),
		})
		assert.Equal(t, usecase.ReasonInvoiceDataIncomplete, apperror.ReasonOf(err))
	})

	t.Run("failures", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Settlements.EXPECT().FindByID(mock.Anything, garageID, "missing").Return(nil, domain.ErrSettlementNotFound).Once()

		_, err := uc.Update(t.Context(), usecase.UpdateSettlementRequest{GarageID: garageID, SettlementID: "missing"})
		assertAppError(t, err, apperror.CodeNotFound)

		uc, d = newUseCase(t)
		d.Settlements.EXPECT().FindByID(mock.Anything, garageID, settlementID).Return(openSettlement(), nil).Once()
		d.RepairOrders.EXPECT().Get(mock.Anything, garageID, repairOrderID).Return(usecase.RepairOrderInfo{}, errBoom).Once()

		_, err = uc.Update(t.Context(), usecase.UpdateSettlementRequest{
			GarageID: garageID, SettlementID: settlementID, BillingTaxProfileID: new(""),
		})
		assertAppError(t, err, apperror.CodeInternal)

		uc, d = newUseCase(t)
		d.Settlements.EXPECT().FindByID(mock.Anything, garageID, settlementID).Return(openSettlement(), nil).Once()
		d.Settlements.EXPECT().SaveBilling(mock.Anything, mock.Anything).Return(errBoom).Once()

		_, err = uc.Update(t.Context(), usecase.UpdateSettlementRequest{GarageID: garageID, SettlementID: settlementID})
		assertAppError(t, err, apperror.CodeInternal)

		uc, d = newUseCase(t)
		d.Settlements.EXPECT().FindByID(mock.Anything, garageID, settlementID).Return(openSettlement(), nil).Once()
		d.Settlements.EXPECT().SaveBilling(mock.Anything, mock.Anything).Return(nil).Once()
		d.Audit.EXPECT().Record(mock.Anything, mock.Anything).Return(errBoom).Once()

		_, err = uc.Update(t.Context(), usecase.UpdateSettlementRequest{GarageID: garageID, SettlementID: settlementID})
		assertAppError(t, err, apperror.CodeInternal)
	})
}

func TestSettlementUseCase_RecordPayment_PayerDefaultsToTheBillingParty(t *testing.T) {
	t.Parallel()

	uc, d := newUseCase(t)
	s := openSettlement()
	s.BillingParty = completeParty
	d.Settlements.EXPECT().FindByID(mock.Anything, garageID, settlementID).Return(s, nil).Once()
	expectVoucher(d)

	var issued *domain.Payment

	updated := openSettlement()
	updated.PaidAmount = 100_000
	d.Settlements.EXPECT().RecordPayment(mock.Anything, garageID, settlementID, mock.Anything).
		Run(func(_ context.Context, _, _ string, p *domain.Payment) { issued = p }).Return(updated, nil).Once()
	d.Settlements.EXPECT().SaveStatus(mock.Anything, garageID, settlementID, domain.StatusPartiallyPaid).Return(nil).Once()
	expectAudit(d)
	expectPaymentEvents(d, false)
	d.Settlements.EXPECT().FindByID(mock.Anything, garageID, settlementID).Return(updated, nil).Once()

	_, err := uc.RecordPayment(t.Context(), usecase.RecordPaymentRequest{
		GarageID: garageID, SettlementID: settlementID, ActorID: actorID, Amount: 100_000, Method: domain.MethodCash,
	})
	require.NoError(t, err)
	require.NotNil(t, issued)
	assert.Equal(t, "Cong ty Van Tai A", issued.PayerName)
	assert.Equal(t, "12 Le Loi", issued.PayerAddress)
}
