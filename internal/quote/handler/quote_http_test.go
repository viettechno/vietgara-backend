package handler_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	commonv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/common/v1"
	quotev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/quote/v1"

	"github.com/viettechno/vietgara-backend/internal/quote/domain"
	"github.com/viettechno/vietgara-backend/internal/quote/handler"
	"github.com/viettechno/vietgara-backend/internal/quote/usecase"
)

func draftQuote() *domain.Quote {
	return &domain.Quote{
		ID: quoteID, GarageID: garageID, CustomerID: customerID, CustomerName: "An Nguyen", VehicleID: vehicleID,
		VehicleLicensePlate: "51F-123.45", Number: "BG-202609-0001", Status: domain.StatusDraft, ApprovalMode: domain.ApprovalModeLink,
		Lines: []domain.Line{
			{ID: "line-1", Position: 1, ItemType: domain.ItemTypeLabor, Description: "Oil change", Quantity: "1", UnitPrice: 250_000, LineTotal: 250_000},
		},
		TotalAmount: 250_000,
	}
}

func TestPolicies(t *testing.T) {
	t.Parallel()

	p := handler.Policies()
	assert.Len(t, p, 11)
	assert.True(t, p[quotev1.PublicQuoteService_GetPublicQuote_FullMethodName].Public)
	assert.True(t, p[quotev1.PublicQuoteService_DecidePublicQuote_FullMethodName].Public)
	assert.NotNil(t, p[quotev1.QuoteService_CreateQuote_FullMethodName].Garage)
}

func TestCreateQuote(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.garages.EXPECT().Get(anyArg, garageID).Return(usecase.GarageInfo{
		OwnerUserID: ownerID, Timezone: "Asia/Ho_Chi_Minh", QuoteNumberTemplate: "BG-{SEQ:4}",
		DefaultQuoteApprovalMode: domain.ApprovalModeLink,
	}, nil).Once()
	e.vehicles.EXPECT().Find(anyArg, garageID, customerID, vehicleID).Return("An Nguyen", "51F-123.45", nil).Once()
	e.limits.EXPECT().ConsumeTransaction(anyArg, ownerID).Return(nil).Once()
	e.sequences.EXPECT().Next(anyArg, garageID, "quote", "BG-{SEQ:4}").Return(1, nil).Once()
	e.quotes.EXPECT().Create(anyArg, anyArg).Return(nil).Once()

	resp, err := e.srv.CreateQuote(signedIn(t), &quotev1.CreateQuoteRequest{
		GarageId: garageID,
		Quote: &quotev1.Quote{
			CustomerId: customerID, VehicleId: vehicleID,
			Lines: []*quotev1.QuoteLine{{ItemType: quotev1.QuoteItemType_QUOTE_ITEM_TYPE_LABOR, Description: "Oil change", Quantity: "1", UnitPrice: 250_000}},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "BG-0001", resp.GetQuote().GetNumber())
	assert.Equal(t, quotev1.QuoteStatus_QUOTE_STATUS_DRAFT, resp.GetQuote().GetStatus())
	assert.Equal(t, int64(250_000), resp.GetQuote().GetTotalAmount())
}

func TestGetQuote_NotFound(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.quotes.EXPECT().FindByID(anyArg, garageID, "missing").Return(nil, domain.ErrQuoteNotFound).Once()

	_, err := e.srv.GetQuote(signedIn(t), &quotev1.GetQuoteRequest{GarageId: garageID, QuoteId: "missing"})
	require.Error(t, err)
}

func TestUpdateQuote_MaskedFields(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.quotes.EXPECT().FindByID(anyArg, garageID, quoteID).Return(draftQuote(), nil).Once()
	e.quotes.EXPECT().ReplaceLines(anyArg, anyArg).Return(nil).Once()

	resp, err := e.srv.UpdateQuote(signedIn(t), &quotev1.UpdateQuoteRequest{
		GarageId: garageID, QuoteId: quoteID,
		Quote:      &quotev1.Quote{Notes: "call before pickup"},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"notes"}},
	})
	require.NoError(t, err)
	assert.Equal(t, "call before pickup", resp.GetQuote().GetNotes())
}

func TestUpdateQuote_NoMaskUpdatesEverySettableField(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.quotes.EXPECT().FindByID(anyArg, garageID, quoteID).Return(draftQuote(), nil).Once()
	e.vehicles.EXPECT().Find(anyArg, garageID, customerID, vehicleID).Return("An Nguyen", "51F-123.45", nil).Once()
	e.quotes.EXPECT().ReplaceLines(anyArg, anyArg).Return(nil).Once()

	resp, err := e.srv.UpdateQuote(signedIn(t), &quotev1.UpdateQuoteRequest{
		GarageId: garageID, QuoteId: quoteID,
		Quote: &quotev1.Quote{
			CustomerId: customerID, VehicleId: vehicleID, ApprovalMode: commonv1.QuoteApprovalMode_QUOTE_APPROVAL_MODE_MANUAL,
			Notes: "updated", Lines: []*quotev1.QuoteLine{{ItemType: quotev1.QuoteItemType_QUOTE_ITEM_TYPE_LABOR, Description: "Brake check", Quantity: "1", UnitPrice: 100_000}},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, commonv1.QuoteApprovalMode_QUOTE_APPROVAL_MODE_MANUAL, resp.GetQuote().GetApprovalMode())
	assert.Equal(t, "updated", resp.GetQuote().GetNotes())
}

func TestUpdateQuote_BadMask(t *testing.T) {
	t.Parallel()

	e := newEnv(t)

	_, err := e.srv.UpdateQuote(signedIn(t), &quotev1.UpdateQuoteRequest{
		GarageId: garageID, QuoteId: quoteID, Quote: &quotev1.Quote{},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"unknown_field"}},
	})
	require.Error(t, err)
}

func TestDeleteQuote(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.quotes.EXPECT().SoftDelete(anyArg, garageID, quoteID, actorID).Return(nil).Once()

	_, err := e.srv.DeleteQuote(signedIn(t), &quotev1.DeleteQuoteRequest{GarageId: garageID, QuoteId: quoteID})
	require.NoError(t, err)
}

func TestDeleteQuote_NotEditable(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.quotes.EXPECT().SoftDelete(anyArg, garageID, quoteID, actorID).Return(domain.ErrQuoteNotFound).Once()

	_, err := e.srv.DeleteQuote(signedIn(t), &quotev1.DeleteQuoteRequest{GarageId: garageID, QuoteId: quoteID})
	require.Error(t, err)
}

func TestListQuotes(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.quotes.EXPECT().List(anyArg, garageID, usecase.ListFilter{Limit: 20, Offset: 0}).
		Return([]*domain.Quote{draftQuote()}, 1, nil).Once()

	resp, err := e.srv.ListQuotes(signedIn(t), &quotev1.ListQuotesRequest{GarageId: garageID})
	require.NoError(t, err)
	assert.Len(t, resp.GetData(), 1)
	assert.Equal(t, int32(1), resp.GetPagination().GetTotal())
}

func TestListQuotes_FilterByStatus(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	rejected := draftQuote()
	rejected.Status = domain.StatusRejected
	e.quotes.EXPECT().List(anyArg, garageID, usecase.ListFilter{
		CustomerID: customerID, VehicleID: vehicleID, Status: domain.StatusRejected, Limit: 20,
	}).Return([]*domain.Quote{rejected}, 1, nil).Once()

	resp, err := e.srv.ListQuotes(signedIn(t), &quotev1.ListQuotesRequest{
		GarageId: garageID, CustomerId: customerID, VehicleId: vehicleID, Status: quotev1.QuoteStatus_QUOTE_STATUS_REJECTED,
	})
	require.NoError(t, err)
	assert.Equal(t, quotev1.QuoteStatus_QUOTE_STATUS_REJECTED, resp.GetData()[0].GetStatus())
}

func TestCreateQuote_PartLineAndManualMode(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.garages.EXPECT().Get(anyArg, garageID).Return(usecase.GarageInfo{
		OwnerUserID: ownerID, Timezone: "Asia/Ho_Chi_Minh", QuoteNumberTemplate: "BG-{SEQ:4}",
		DefaultQuoteApprovalMode: domain.ApprovalModeLink,
	}, nil).Once()
	e.vehicles.EXPECT().Find(anyArg, garageID, customerID, vehicleID).Return("An Nguyen", "51F-123.45", nil).Once()
	e.limits.EXPECT().ConsumeTransaction(anyArg, ownerID).Return(nil).Once()
	e.sequences.EXPECT().Next(anyArg, garageID, "quote", "BG-{SEQ:4}").Return(1, nil).Once()
	e.quotes.EXPECT().Create(anyArg, anyArg).Return(nil).Once()

	resp, err := e.srv.CreateQuote(signedIn(t), &quotev1.CreateQuoteRequest{
		GarageId: garageID,
		Quote: &quotev1.Quote{
			CustomerId: customerID, VehicleId: vehicleID, ApprovalMode: commonv1.QuoteApprovalMode_QUOTE_APPROVAL_MODE_MANUAL,
			Lines: []*quotev1.QuoteLine{{ItemType: quotev1.QuoteItemType_QUOTE_ITEM_TYPE_PART, Description: "Air filter", Quantity: "1", UnitPrice: 480_000}},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, commonv1.QuoteApprovalMode_QUOTE_APPROVAL_MODE_MANUAL, resp.GetQuote().GetApprovalMode())
	assert.Equal(t, quotev1.QuoteItemType_QUOTE_ITEM_TYPE_PART, resp.GetQuote().GetLines()[0].GetItemType())
}
