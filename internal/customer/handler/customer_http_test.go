package handler_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"

	"github.com/viettechno/vietgara-backend/internal/customer/domain"
	"github.com/viettechno/vietgara-backend/internal/customer/usecase"
)

const customerJSON = `{"id":"` + customerID + `","garageId":"` + garageID + `","fullName":"Tran Binh","phone":"0912345678",` +
	`"personalDataConsent":true,"personalDataConsentAt":"2026-01-02T03:04:05Z","vehicleCount":1,` +
	`"createdAt":"2026-01-02T03:04:05Z","updatedAt":"2026-01-02T03:04:05Z"}`

func TestServer_RegisterHTTPRoutes(t *testing.T) {
	t.Parallel()

	f := newFixture(t, "owner")
	rec := f.do(t, http.MethodGet, "/garages/"+garageID+"/unknown", "")

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestServer_ListHTTPCustomers(t *testing.T) {
	t.Parallel()

	t.Run("any member can search with paging", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t, "technician")
		f.customers.EXPECT().List(mock.Anything, garageID, usecase.ListFilter{Query: "binh", Limit: 5, Offset: 5}).
			Return([]*domain.Customer{testCustomer()}, 6, nil).Once()

		rec := f.do(t, http.MethodGet, garagePath+"/customers?query=binh&page=2&pageSize=5&unknown=1", "")

		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.JSONEq(t, `{"customers":[`+customerJSON+`],"pagination":{"total":6,"page":2,"pageSize":5}}`, rec.Body.String())
	})

	t.Run("rejects a non-numeric page", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t, "owner")
		rec := f.do(t, http.MethodGet, garagePath+"/customers?page=two", "")

		require.Equal(t, http.StatusBadRequest, rec.Code)
		assert.JSONEq(t, `{"message":"page must be a number"}`, rec.Body.String())
	})

	t.Run("non-members are forbidden", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t, "")
		rec := f.do(t, http.MethodGet, garagePath+"/customers", "")

		require.Equal(t, http.StatusForbidden, rec.Code)
		assert.JSONEq(t, `{"message":"account has no access to this garage"}`, rec.Body.String())
	})

	t.Run("a malformed garage id is rejected", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t, "owner")
		rec := f.do(t, http.MethodGet, "/garages/not-a-uuid/customers", "")

		require.Equal(t, http.StatusBadRequest, rec.Code)
		assert.JSONEq(t, `{"message":"garage_id is invalid"}`, rec.Body.String())
	})

	t.Run("membership lookup failures are internal errors", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t, "")
		f.members.EXPECT().RoleInGarage(mock.Anything, "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb", accountID).Return("", errors.New("db down")).Once()

		rec := f.do(t, http.MethodGet, "/garages/bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb/customers", "")

		require.Equal(t, http.StatusInternalServerError, rec.Code)
		assert.JSONEq(t, `{"message":"internal error"}`, rec.Body.String())
	})
}

func TestServer_CreateHTTPCustomer(t *testing.T) {
	t.Parallel()

	t.Run("a service advisor creates a customer in the path's garage", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t, "service_advisor")
		f.customers.EXPECT().Create(mock.Anything, mock.MatchedBy(func(c *domain.Customer) bool {
			return c.GarageID == garageID && c.FullName == "Le Chi" && *c.Phone == "0901234567" && *c.CreatedBy == accountID
		})).Return(nil).Once()

		rec := f.do(t, http.MethodPost, garagePath+"/customers",
			`{"garageId":"bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb","fullName":"Le Chi","phone":"090 123 4567","personalDataConsent":true,"extra":1}`)

		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		assert.Contains(t, rec.Body.String(), `"fullName":"Le Chi"`)
		assert.Contains(t, rec.Body.String(), `"personalDataConsent":true`)
	})

	t.Run("a technician may not create customers", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t, "technician")
		rec := f.do(t, http.MethodPost, garagePath+"/customers", `{"fullName":"Le Chi"}`)

		require.Equal(t, http.StatusForbidden, rec.Code)
		assert.JSONEq(t, `{"message":"your role cannot perform this action"}`, rec.Body.String())
	})

	t.Run("validation errors are bad requests", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t, "owner")
		rec := f.do(t, http.MethodPost, garagePath+"/customers", `{"fullName":" "}`)

		require.Equal(t, http.StatusBadRequest, rec.Code)
		assert.JSONEq(t, `{"message":"full name is required"}`, rec.Body.String())
	})

	t.Run("malformed JSON is a bad request", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t, "owner")
		rec := f.do(t, http.MethodPost, garagePath+"/customers", `{"fullName":`)

		require.Equal(t, http.StatusBadRequest, rec.Code)
		assert.JSONEq(t, `{"message":"invalid request body"}`, rec.Body.String())
	})

	t.Run("a duplicate phone is a conflict", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t, "owner")
		f.customers.EXPECT().Create(mock.Anything, mock.Anything).Return(domain.ErrPhoneTaken).Once()

		rec := f.do(t, http.MethodPost, garagePath+"/customers", `{"fullName":"Le Chi","phone":"0912345678"}`)

		require.Equal(t, http.StatusConflict, rec.Code)
		assert.JSONEq(t, `{"message":"another customer already uses this phone number"}`, rec.Body.String())
	})
}

func TestServer_GetUpdateDeleteHTTPCustomer(t *testing.T) {
	t.Parallel()

	t.Run("get", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t, "accountant")
		f.customers.EXPECT().FindByID(mock.Anything, garageID, customerID).Return(testCustomer(), nil).Once()

		rec := f.do(t, http.MethodGet, garagePath+"/customers/"+customerID, "")

		require.Equal(t, http.StatusOK, rec.Code)
		assert.JSONEq(t, customerJSON, rec.Body.String())
	})

	t.Run("get unknown", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t, "owner")
		f.customers.EXPECT().FindByID(mock.Anything, garageID, "missing").Return(nil, domain.ErrCustomerNotFound).Once()

		rec := f.do(t, http.MethodGet, garagePath+"/customers/missing", "")

		require.Equal(t, http.StatusNotFound, rec.Code)
		assert.JSONEq(t, `{"message":"customer not found"}`, rec.Body.String())
	})

	t.Run("patch clears the phone and withdraws consent", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t, "owner")
		f.customers.EXPECT().FindByID(mock.Anything, garageID, customerID).Return(testCustomer(), nil).Once()
		f.customers.EXPECT().Update(mock.Anything, mock.MatchedBy(func(c *domain.Customer) bool {
			return c.Phone == nil && len(c.ConsentLog) == 2 && !c.ConsentLog[1].Granted
		})).Return(nil).Once()

		rec := f.do(t, http.MethodPatch, garagePath+"/customers/"+customerID, `{"phone":"","personalDataConsent":false}`)

		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.NotContains(t, rec.Body.String(), `"phone"`)
		assert.NotContains(t, rec.Body.String(), `"personalDataConsent"`)
	})

	t.Run("delete", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t, "service_advisor")
		f.customers.EXPECT().SoftDelete(mock.Anything, garageID, customerID, accountID).Return(nil).Once()
		f.vehicles.EXPECT().SoftDeleteByCustomer(mock.Anything, garageID, customerID, accountID).Return(nil).Once()

		rec := f.do(t, http.MethodDelete, garagePath+"/customers/"+customerID, "")

		require.Equal(t, http.StatusNoContent, rec.Code)
		assert.Empty(t, rec.Body.String())
	})
}

func TestServer_HTTPTransactions(t *testing.T) {
	t.Parallel()

	t.Run("requests run in a transaction scoped to the path's garage", func(t *testing.T) {
		t.Parallel()

		tenant := garageID
		f := newFixture(t, "owner", withTx(t, &tenant))
		f.customers.EXPECT().FindByID(mock.Anything, garageID, customerID).Return(testCustomer(), nil).Once()

		rec := f.do(t, http.MethodGet, garagePath+"/customers/"+customerID, "")

		require.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("a malformed garage id never becomes the tenant", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t, "owner", withTx(t, nil))
		rec := f.do(t, http.MethodGet, "/garages/x/customers/"+customerID, "")

		require.Equal(t, http.StatusBadRequest, rec.Code)
	})
}

func TestServer_GRPCWithoutClaims(t *testing.T) {
	t.Parallel()

	f := newFixture(t, "owner")
	_, err := f.srv.GetCustomer(t.Context(), &customerv1.GetCustomerRequest{GarageId: garageID, CustomerId: customerID})

	assert.Equal(t, codes.Unauthenticated, status.Code(err))
}
