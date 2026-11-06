package handler_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"

	"github.com/viettechno/vietgara-backend/internal/datarequest/domain"
	"github.com/viettechno/vietgara-backend/internal/datarequest/handler"
	"github.com/viettechno/vietgara-backend/internal/datarequest/usecase"
	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/mocks"
	"github.com/viettechno/vietgara-backend/internal/platform/access"
	"github.com/viettechno/vietgara-backend/internal/platform/auth"
)

const (
	garageID   = "11111111-1111-1111-1111-111111111111"
	customerID = "22222222-2222-2222-2222-222222222222"
	actorID    = "33333333-3333-3333-3333-333333333333"
	requestID  = "44444444-4444-4444-4444-444444444444"
)

var anyArg = mock.Anything

type env struct {
	srv       *handler.Server
	repo      *mocks.MockDataRequestRepository
	customers *mocks.MockDataRequestCustomers
	documents *mocks.MockDataRequestDocuments
	names     *mocks.MockDataRequestNames
}

func newEnv(t *testing.T) *env {
	t.Helper()

	e := &env{
		repo: mocks.NewMockDataRequestRepository(t), customers: mocks.NewMockDataRequestCustomers(t),
		documents: mocks.NewMockDataRequestDocuments(t), names: mocks.NewMockDataRequestNames(t),
	}
	e.srv = handler.NewServer(usecase.NewStaffUseCase(e.repo, e.customers, e.documents, e.names))

	return e
}

func signedIn(t *testing.T) context.Context {
	t.Helper()

	claims := &auth.Claims{}
	claims.Subject = actorID

	return middleware.WithClaims(t.Context(), claims)
}

func TestPolicies(t *testing.T) {
	t.Parallel()

	p := handler.Policies()
	assert.Len(t, p, 4)
	assert.Equal(t, access.CustomerRead, p[customerv1.DataRequestService_ListDataRequests_FullMethodName].Garage.Permission)
	assert.Equal(t, access.CustomerRead, p[customerv1.DataRequestService_GetDataRequest_FullMethodName].Garage.Permission)
	assert.Equal(t, access.CustomerWrite, p[customerv1.DataRequestService_CreateDataRequest_FullMethodName].Garage.Permission)
	assert.Equal(t, access.CustomerWrite, p[customerv1.DataRequestService_CompleteDataRequest_FullMethodName].Garage.Permission)

	public := handler.PublicPolicies()
	assert.Len(t, public, 2)
	assert.True(t, public[customerv1.PublicDataRequestService_SubmitPublicDataRequest_FullMethodName].Public)
	assert.True(t, public[customerv1.PublicDataRequestService_ConfirmPublicDataRequest_FullMethodName].Public)
}

func TestCreateListGetComplete(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	when := time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC)

	e.customers.EXPECT().Check(anyArg, garageID, customerID).Return(nil).Once()
	e.repo.EXPECT().Create(anyArg, anyArg).Return(nil).Once()

	created, err := e.srv.CreateDataRequest(signedIn(t), &customerv1.CreateDataRequestRequest{
		GarageId: garageID, CustomerId: customerID, Type: customerv1.DataRequestType_DATA_REQUEST_TYPE_CORRECT, Details: "my phone",
	})
	require.NoError(t, err)
	assert.Equal(t, customerv1.DataRequestStatus_DATA_REQUEST_STATUS_PENDING, created.GetStatus())
	assert.Equal(t, customerv1.DataRequestSource_DATA_REQUEST_SOURCE_STAFF, created.GetSource())
	assert.Equal(t, customerv1.DataRequestType_DATA_REQUEST_TYPE_CORRECT, created.GetType())
	assert.Equal(t, "my phone", created.GetDetails())

	stored := &domain.Request{
		ID: requestID, GarageID: garageID, CustomerID: customerID, Type: domain.TypeDelete, Status: domain.StatusRejected, Source: domain.SourcePublic,
		Outcome: "no", RequestedAt: when, CompletedAt: &when, CompletedBy: new(actorID),
	}
	e.repo.EXPECT().List(anyArg, garageID, usecase.Filter{
		CustomerID: customerID, Type: domain.TypeDelete, Status: domain.StatusRejected, Limit: 20,
	}).Return([]*domain.Request{stored}, 1, nil).Once()
	e.names.EXPECT().UserNames(anyArg, anyArg).Return(map[string]string{actorID: "Binh"}, nil).Times(3)

	list, err := e.srv.ListDataRequests(signedIn(t), &customerv1.ListDataRequestsRequest{
		GarageId: garageID, CustomerId: customerID, Type: customerv1.DataRequestType_DATA_REQUEST_TYPE_DELETE,
		Status: customerv1.DataRequestStatus_DATA_REQUEST_STATUS_REJECTED,
	})
	require.NoError(t, err)
	require.Len(t, list.GetData(), 1)
	assert.EqualValues(t, 1, list.GetPagination().GetTotal())
	assert.Equal(t, customerv1.DataRequestSource_DATA_REQUEST_SOURCE_PUBLIC, list.GetData()[0].GetSource())
	assert.Equal(t, "Binh", list.GetData()[0].GetCompletedByName())
	assert.Empty(t, list.GetData()[0].GetExportJson())

	e.repo.EXPECT().Find(anyArg, garageID, requestID).Return(stored, nil).Once()

	got, err := e.srv.GetDataRequest(signedIn(t), &customerv1.GetDataRequestRequest{GarageId: garageID, RequestId: requestID})
	require.NoError(t, err)
	assert.Equal(t, requestID, got.GetId())

	pending := &domain.Request{ID: requestID, GarageID: garageID, Type: domain.TypeCorrect, Status: domain.StatusPending}
	e.repo.EXPECT().FindForUpdate(anyArg, garageID, requestID).Return(pending, nil).Once()
	e.repo.EXPECT().Save(anyArg, anyArg).Return(nil).Once()

	done, err := e.srv.CompleteDataRequest(signedIn(t), &customerv1.CompleteDataRequestRequest{
		GarageId: garageID, RequestId: requestID, Outcome: "Phone changed",
	})
	require.NoError(t, err)
	assert.Equal(t, customerv1.DataRequestStatus_DATA_REQUEST_STATUS_COMPLETED, done.GetStatus())
}

func TestErrorsMapToTheirCodes(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	ctx := signedIn(t)

	_, err := e.srv.CreateDataRequest(ctx, &customerv1.CreateDataRequestRequest{GarageId: garageID, CustomerId: customerID})
	assert.Equal(t, codes.InvalidArgument, status.Code(err))

	e.repo.EXPECT().List(anyArg, garageID, anyArg).Return(nil, 0, errBoom).Once()

	_, err = e.srv.ListDataRequests(ctx, &customerv1.ListDataRequestsRequest{GarageId: garageID})
	assert.Equal(t, codes.Internal, status.Code(err))

	e.repo.EXPECT().Find(anyArg, garageID, requestID).Return(nil, domain.ErrRequestNotFound).Once()

	_, err = e.srv.GetDataRequest(ctx, &customerv1.GetDataRequestRequest{GarageId: garageID, RequestId: requestID})
	assert.Equal(t, codes.NotFound, status.Code(err))

	e.repo.EXPECT().FindForUpdate(anyArg, garageID, requestID).Return(&domain.Request{Status: domain.StatusCompleted}, nil).Once()

	_, err = e.srv.CompleteDataRequest(ctx, &customerv1.CompleteDataRequestRequest{GarageId: garageID, RequestId: requestID, Outcome: "x"})
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
}

func TestAViewIsAnsweredOnceAndNeverStored(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.customers.EXPECT().Check(anyArg, garageID, customerID).Return(nil).Once()
	e.customers.EXPECT().Export(anyArg, garageID, customerID).Return(json.RawMessage(`{"a":1}`), nil).Once()
	e.documents.EXPECT().Numbers(anyArg, garageID, customerID).Return(usecase.DocumentNumbers{}, nil).Once()
	e.repo.EXPECT().Create(anyArg, mock.MatchedBy(func(r *domain.Request) bool { return r.Details == "" })).Return(nil).Once()

	got, err := e.srv.CreateDataRequest(signedIn(t), &customerv1.CreateDataRequestRequest{
		GarageId: garageID, CustomerId: customerID, Type: customerv1.DataRequestType_DATA_REQUEST_TYPE_VIEW,
	})
	require.NoError(t, err)
	assert.Contains(t, got.GetExportJson(), `"a": 1`)
}

type publicEnv struct {
	srv           *handler.PublicServer
	confirmations *mocks.MockDataRequestConfirmations
	customers     *mocks.MockDataRequestCustomers
	mailer        *mocks.MockDataRequestMailer
}

func newPublicEnv(t *testing.T) *publicEnv {
	t.Helper()

	e := &publicEnv{
		confirmations: mocks.NewMockDataRequestConfirmations(t), customers: mocks.NewMockDataRequestCustomers(t), mailer: mocks.NewMockDataRequestMailer(t),
	}
	e.srv = handler.NewPublicServer(usecase.NewPublicUseCase(usecase.PublicDeps{
		Confirmations: e.confirmations, Customers: e.customers, Documents: mocks.NewMockDataRequestDocuments(t), Repo: mocks.NewMockDataRequestRepository(t),
		Names: mocks.NewMockDataRequestNames(t), Tx: mocks.NewMockDataRequestTransactor(t), Mailer: e.mailer, BaseURL: "https://x", SourceKey: []byte("k"),
	}))

	return e
}

func TestPublicSubmitAndConfirm(t *testing.T) {
	t.Parallel()

	e := newPublicEnv(t)
	e.confirmations.EXPECT().CountByEmail(anyArg, "binh@example.com", anyArg).Return(0, nil).Once()
	e.confirmations.EXPECT().CountBySource(anyArg, anyArg, anyArg).Return(0, nil).Maybe()
	e.confirmations.EXPECT().CreateConfirmation(anyArg, anyArg).Return(nil).Once()
	e.mailer.EXPECT().Send(anyArg, anyArg).Return(nil).Once()

	_, err := e.srv.SubmitPublicDataRequest(t.Context(), &customerv1.SubmitPublicDataRequestRequest{
		Email: "Binh@Example.com", Type: customerv1.DataRequestType_DATA_REQUEST_TYPE_VIEW,
	})
	require.NoError(t, err)

	_, err = e.srv.SubmitPublicDataRequest(t.Context(), &customerv1.SubmitPublicDataRequestRequest{Email: "bad", Type: customerv1.DataRequestType_DATA_REQUEST_TYPE_VIEW})
	assert.Equal(t, codes.InvalidArgument, status.Code(err))

	e.confirmations.EXPECT().Consume(anyArg, anyArg, anyArg).Return(&domain.Confirmation{Email: "binh@example.com", Type: domain.TypeCorrect}, nil).Once()
	e.customers.EXPECT().ByEmail(anyArg, "binh@example.com").Return(nil, nil).Once()

	resp, err := e.srv.ConfirmPublicDataRequest(t.Context(), &customerv1.ConfirmPublicDataRequestRequest{Token: "t"})
	require.NoError(t, err)
	assert.Zero(t, resp.GetGarageCount())

	_, err = e.srv.ConfirmPublicDataRequest(t.Context(), &customerv1.ConfirmPublicDataRequestRequest{})
	assert.Equal(t, codes.NotFound, status.Code(err))
}

var errBoom = assert.AnError
