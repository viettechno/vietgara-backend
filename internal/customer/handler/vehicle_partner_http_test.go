package handler_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/customer/domain"
	"github.com/viettechno/vietgara-backend/internal/customer/usecase"
)

const vehicleJSON = `{"id":"` + vehicleID + `","garageId":"` + garageID + `","customerId":"` + customerID + `",` +
	`"customerName":"Tran Binh","licensePlate":"51F-123.45","make":"Toyota","year":2020,` +
	`"createdAt":"2026-01-02T03:04:05Z","updatedAt":"2026-01-02T03:04:05Z"}`

func TestServer_HTTPVehicles(t *testing.T) {
	t.Parallel()

	t.Run("register a vehicle for the customer in the path", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t, "service_advisor")
		f.customers.EXPECT().FindByID(mock.Anything, garageID, customerID).Return(testCustomer(), nil).Once()
		f.vehicles.EXPECT().Create(mock.Anything, mock.MatchedBy(func(v *domain.Vehicle) bool {
			return v.CustomerID == customerID && v.LicensePlate == "51F-123.45" && *v.Year == 2020
		})).Return(nil).Once()

		rec := f.do(t, http.MethodPost, garagePath+"/customers/"+customerID+"/vehicles",
			`{"customerId":"someone-else","licensePlate":"51f-123.45","year":2020}`)

		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		assert.Contains(t, rec.Body.String(), `"customerName":"Tran Binh"`)
	})

	t.Run("list a customer's vehicles", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t, "technician")
		f.vehicles.EXPECT().List(mock.Anything, garageID, usecase.VehicleFilter{
			ListFilter: usecase.ListFilter{Limit: 20}, CustomerID: customerID,
		}).Return([]*domain.Vehicle{testVehicle()}, 1, nil).Once()

		rec := f.do(t, http.MethodGet, garagePath+"/customers/"+customerID+"/vehicles", "")

		require.Equal(t, http.StatusOK, rec.Code)
		assert.JSONEq(t, `{"vehicles":[`+vehicleJSON+`],"pagination":{"total":1,"page":1,"pageSize":20}}`, rec.Body.String())
	})

	t.Run("search the garage's vehicles by plate", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t, "owner")
		f.vehicles.EXPECT().List(mock.Anything, garageID, usecase.VehicleFilter{
			ListFilter: usecase.ListFilter{Query: "51F", Limit: 20},
		}).Return(nil, 0, nil).Once()

		rec := f.do(t, http.MethodGet, garagePath+"/vehicles?query=51F", "")

		require.Equal(t, http.StatusOK, rec.Code)
		assert.JSONEq(t, `{"pagination":{"page":1,"pageSize":20}}`, rec.Body.String(), "protojson omits zero values")
	})

	t.Run("get, update and delete a vehicle", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t, "owner")
		f.vehicles.EXPECT().FindByID(mock.Anything, garageID, vehicleID).Return(testVehicle(), nil).Twice()
		f.vehicles.EXPECT().Update(mock.Anything, mock.MatchedBy(func(v *domain.Vehicle) bool { return v.Year == nil })).Return(nil).Once()
		f.vehicles.EXPECT().SoftDelete(mock.Anything, garageID, vehicleID, accountID).Return(nil).Once()

		rec := f.do(t, http.MethodGet, garagePath+"/vehicles/"+vehicleID, "")
		require.Equal(t, http.StatusOK, rec.Code)
		assert.JSONEq(t, vehicleJSON, rec.Body.String())

		rec = f.do(t, http.MethodPatch, garagePath+"/vehicles/"+vehicleID, `{"year":0}`)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.NotContains(t, rec.Body.String(), `"year"`)

		rec = f.do(t, http.MethodDelete, garagePath+"/vehicles/"+vehicleID, "")
		require.Equal(t, http.StatusNoContent, rec.Code)
	})

	t.Run("a duplicate plate is a conflict", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t, "owner")
		f.customers.EXPECT().FindByID(mock.Anything, garageID, customerID).Return(testCustomer(), nil).Once()
		f.vehicles.EXPECT().Create(mock.Anything, mock.Anything).Return(domain.ErrLicensePlateTaken).Once()

		rec := f.do(t, http.MethodPost, garagePath+"/vehicles", `{"customerId":"`+customerID+`","licensePlate":"51F12345"}`)

		require.Equal(t, http.StatusConflict, rec.Code)
		assert.JSONEq(t, `{"message":"another vehicle already uses this license plate"}`, rec.Body.String())
	})
}

func TestServer_HTTPPartners(t *testing.T) {
	t.Parallel()

	t.Run("the owner adds a supplier", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t, "owner")
		f.partners.EXPECT().Create(mock.Anything, mock.MatchedBy(func(p *domain.Partner) bool {
			return p.Type == domain.PartnerTypeSupplier && p.Name == "Phu Tung Sai Gon" && *p.Contact.ContactPerson == "Ms. Lan"
		})).Return(nil).Once()

		rec := f.do(t, http.MethodPost, garagePath+"/partners",
			`{"type":"PARTNER_TYPE_SUPPLIER","name":"Phu Tung Sai Gon","contact":{"contactPerson":"Ms. Lan"}}`)

		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		assert.Contains(t, rec.Body.String(), `"type":"PARTNER_TYPE_SUPPLIER"`)
	})

	t.Run("service advisors cannot change partners", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t, "service_advisor")
		rec := f.do(t, http.MethodPost, garagePath+"/partners", `{"type":"PARTNER_TYPE_SUPPLIER","name":"X"}`)

		require.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("a partner type is required", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t, "owner")
		rec := f.do(t, http.MethodPost, garagePath+"/partners", `{"name":"X"}`)

		require.Equal(t, http.StatusBadRequest, rec.Code)
		assert.JSONEq(t, `{"message":"partner type must be supplier or insurer"}`, rec.Body.String())
	})

	t.Run("list filters by type given in short or full form", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t, "inventory_staff")
		filter := usecase.PartnerFilter{ListFilter: usecase.ListFilter{Limit: 20}, Type: domain.PartnerTypeInsurer}
		f.partners.EXPECT().List(mock.Anything, garageID, filter).Return([]*domain.Partner{testPartner()}, 1, nil).Twice()

		for _, param := range []string{"insurer", "PARTNER_TYPE_INSURER"} {
			rec := f.do(t, http.MethodGet, garagePath+"/partners?type="+param, "")
			require.Equal(t, http.StatusOK, rec.Code, param)
			assert.Contains(t, rec.Body.String(), `"contact":{"phone":"0281234567"}`)
		}

		rec := f.do(t, http.MethodGet, garagePath+"/partners?type=bank", "")
		require.Equal(t, http.StatusBadRequest, rec.Code)
		assert.JSONEq(t, `{"message":"type is invalid"}`, rec.Body.String())
	})

	t.Run("get, update and delete a partner", func(t *testing.T) {
		t.Parallel()

		f := newFixture(t, "owner")
		f.partners.EXPECT().FindByID(mock.Anything, garageID, partnerID).Return(testPartner(), nil).Twice()
		f.partners.EXPECT().Update(mock.Anything, mock.MatchedBy(func(p *domain.Partner) bool {
			return p.Type == domain.PartnerTypeInsurer && p.Contact.Phone == nil && *p.Contact.Email == "claims@example.com"
		})).Return(nil).Once()
		f.partners.EXPECT().SoftDelete(mock.Anything, garageID, partnerID, accountID).Return(nil).Once()

		rec := f.do(t, http.MethodGet, garagePath+"/partners/"+partnerID, "")
		require.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), `"name":"Phu Tung Sai Gon"`)

		rec = f.do(t, http.MethodPatch, garagePath+"/partners/"+partnerID,
			`{"type":"PARTNER_TYPE_INSURER","contact":{"email":"claims@example.com"}}`)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		rec = f.do(t, http.MethodDelete, garagePath+"/partners/"+partnerID, "")
		require.Equal(t, http.StatusNoContent, rec.Code)
	})
}

func TestServer_HTTPUseCaseErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		method string
		path   string
		body   string
		setup  func(f fixture)
		want   int
	}{
		{name: "list customers fails", method: http.MethodGet, path: "/customers", want: http.StatusInternalServerError, setup: func(f fixture) {
			f.customers.EXPECT().List(mock.Anything, garageID, mock.Anything).Return(nil, 0, errBoom).Once()
		}},
		{name: "update unknown customer", method: http.MethodPatch, path: "/customers/" + customerID, body: `{}`, want: http.StatusNotFound, setup: func(f fixture) {
			f.customers.EXPECT().FindByID(mock.Anything, garageID, customerID).Return(nil, domain.ErrCustomerNotFound).Once()
		}},
		{name: "delete unknown customer", method: http.MethodDelete, path: "/customers/" + customerID, want: http.StatusNotFound, setup: func(f fixture) {
			f.customers.EXPECT().SoftDelete(mock.Anything, garageID, customerID, accountID).Return(domain.ErrCustomerNotFound).Once()
		}},
		{name: "list vehicles fails", method: http.MethodGet, path: "/vehicles", want: http.StatusInternalServerError, setup: func(f fixture) {
			f.vehicles.EXPECT().List(mock.Anything, garageID, mock.Anything).Return(nil, 0, errBoom).Once()
		}},
		{name: "get unknown vehicle", method: http.MethodGet, path: "/vehicles/" + vehicleID, want: http.StatusNotFound, setup: func(f fixture) {
			f.vehicles.EXPECT().FindByID(mock.Anything, garageID, vehicleID).Return(nil, domain.ErrVehicleNotFound).Once()
		}},
		{name: "update unknown vehicle", method: http.MethodPatch, path: "/vehicles/" + vehicleID, body: `{}`, want: http.StatusNotFound, setup: func(f fixture) {
			f.vehicles.EXPECT().FindByID(mock.Anything, garageID, vehicleID).Return(nil, domain.ErrVehicleNotFound).Once()
		}},
		{name: "delete unknown vehicle", method: http.MethodDelete, path: "/vehicles/" + vehicleID, want: http.StatusNotFound, setup: func(f fixture) {
			f.vehicles.EXPECT().SoftDelete(mock.Anything, garageID, vehicleID, accountID).Return(domain.ErrVehicleNotFound).Once()
		}},
		{name: "list partners fails", method: http.MethodGet, path: "/partners", want: http.StatusInternalServerError, setup: func(f fixture) {
			f.partners.EXPECT().List(mock.Anything, garageID, mock.Anything).Return(nil, 0, errBoom).Once()
		}},
		{name: "get unknown partner", method: http.MethodGet, path: "/partners/" + partnerID, want: http.StatusNotFound, setup: func(f fixture) {
			f.partners.EXPECT().FindByID(mock.Anything, garageID, partnerID).Return(nil, domain.ErrPartnerNotFound).Once()
		}},
		{name: "update unknown partner", method: http.MethodPatch, path: "/partners/" + partnerID, body: `{"name":"X"}`, want: http.StatusNotFound, setup: func(f fixture) {
			f.partners.EXPECT().FindByID(mock.Anything, garageID, partnerID).Return(nil, domain.ErrPartnerNotFound).Once()
		}},
		{name: "delete unknown partner", method: http.MethodDelete, path: "/partners/" + partnerID, want: http.StatusNotFound, setup: func(f fixture) {
			f.partners.EXPECT().SoftDelete(mock.Anything, garageID, partnerID, accountID).Return(domain.ErrPartnerNotFound).Once()
		}},
		{name: "create partner with an unknown type name", method: http.MethodPost, path: "/partners", body: `{"type":"PARTNER_TYPE_BANK","name":"X"}`, want: http.StatusBadRequest},
		{name: "list with an out-of-range page size", method: http.MethodGet, path: "/partners?pageSize=99999999999", want: http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newFixture(t, "owner")
			if tt.setup != nil {
				tt.setup(f)
			}

			rec := f.do(t, tt.method, garagePath+tt.path, tt.body)

			assert.Equal(t, tt.want, rec.Code, rec.Body.String())
		})
	}
}
