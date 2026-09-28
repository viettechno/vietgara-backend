package usecase_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/customer/domain"
	"github.com/viettechno/vietgara-backend/internal/customer/usecase"
	"github.com/viettechno/vietgara-backend/internal/mocks"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

const partnerID = "44444444-4444-4444-4444-444444444444"

func existingPartner() *domain.Partner {
	return &domain.Partner{
		ID: partnerID, GarageID: garageID, Type: domain.PartnerTypeSupplier, Name: "Phu Tung Sai Gon",
		Contact: domain.PartnerContact{ContactPerson: new("Ms. Lan")},
	}
}

func TestPartnerUseCase_Create(t *testing.T) {
	t.Parallel()

	valid := func() usecase.CreatePartnerRequest {
		return usecase.CreatePartnerRequest{
			GarageID: garageID, ActorID: actorID, Type: domain.PartnerTypeInsurer, Name: " Bao Viet ", TaxCode: new("01-001 abc"),
			Contact: usecase.PartnerContactInput{ContactPerson: new(" Mr. Minh "), Phone: new("+84 28 1234 5678"), Email: new("Claims@BaoViet.vn"), Address: new("")},
			Note:    new("  "),
		}
	}

	tests := []struct {
		name      string
		mutate    func(r *usecase.CreatePartnerRequest)
		createErr error
		wantCode  apperror.Code
	}{
		{name: "normalizes and stores the partner"},
		{name: "requires a type", mutate: func(r *usecase.CreatePartnerRequest) { r.Type = "" }, wantCode: apperror.CodeInvalidArgument},
		{name: "requires a name", mutate: func(r *usecase.CreatePartnerRequest) { r.Name = "" }, wantCode: apperror.CodeInvalidArgument},
		{name: "rejects a long contact person", mutate: func(r *usecase.CreatePartnerRequest) {
			r.Contact.ContactPerson = new(string(make([]rune, 256)))
		}, wantCode: apperror.CodeInvalidArgument},
		{name: "rejects a bad phone", mutate: func(r *usecase.CreatePartnerRequest) { r.Contact.Phone = new("abc") }, wantCode: apperror.CodeInvalidArgument},
		{name: "rejects a bad email", mutate: func(r *usecase.CreatePartnerRequest) { r.Contact.Email = new("claims") }, wantCode: apperror.CodeInvalidArgument},
		{name: "rejects a long tax code", mutate: func(r *usecase.CreatePartnerRequest) { r.TaxCode = new("0123456789012345678901") }, wantCode: apperror.CodeInvalidArgument},
		{name: "taken name", createErr: domain.ErrPartnerNameTaken, wantCode: apperror.CodeAlreadyExists},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			partners := mocks.NewMockPartnerRepository(t)
			uc := usecase.NewPartnerUseCase(partners)

			req := valid()
			if tt.mutate != nil {
				tt.mutate(&req)
			}

			if tt.wantCode == "" || tt.createErr != nil {
				partners.EXPECT().Create(mock.Anything, mock.Anything).Return(tt.createErr).Once()
			}

			p, err := uc.Create(t.Context(), req)
			if tt.wantCode != "" {
				assertAppError(t, err, tt.wantCode)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, "Bao Viet", p.Name)
			assert.Equal(t, "01-001 ABC", *p.TaxCode)
			assert.Equal(t, "Mr. Minh", *p.Contact.ContactPerson)
			assert.Equal(t, "02812345678", *p.Contact.Phone)
			assert.Equal(t, "claims@baoviet.vn", *p.Contact.Email)
			assert.Nil(t, p.Contact.Address)
			assert.Nil(t, p.Note)
		})
	}
}

func TestPartnerUseCase_Update(t *testing.T) {
	t.Parallel()

	insurer := domain.PartnerTypeInsurer
	bogus := domain.PartnerType("bank")

	tests := []struct {
		name      string
		req       usecase.UpdatePartnerRequest
		findErr   error
		updateErr error
		wantCode  apperror.Code
	}{
		{name: "changes type, replaces contact and sets a note", req: usecase.UpdatePartnerRequest{
			Type: &insurer, Contact: &usecase.PartnerContactInput{Phone: new("0909123456")}, Note: new("VIP"),
		}},
		{name: "unknown partner", findErr: domain.ErrPartnerNotFound, wantCode: apperror.CodeNotFound},
		{name: "invalid type", req: usecase.UpdatePartnerRequest{Type: &bogus}, wantCode: apperror.CodeInvalidArgument},
		{name: "blank name", req: usecase.UpdatePartnerRequest{Name: new(" ")}, wantCode: apperror.CodeInvalidArgument},
		{name: "invalid contact", req: usecase.UpdatePartnerRequest{Contact: &usecase.PartnerContactInput{Email: new("x")}}, wantCode: apperror.CodeInvalidArgument},
		{name: "long note", req: usecase.UpdatePartnerRequest{Note: new(string(make([]rune, 2001)))}, wantCode: apperror.CodeInvalidArgument},
		{name: "taken name", req: usecase.UpdatePartnerRequest{Name: new("Bao Viet")}, updateErr: domain.ErrPartnerNameTaken, wantCode: apperror.CodeAlreadyExists},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			partners := mocks.NewMockPartnerRepository(t)
			uc := usecase.NewPartnerUseCase(partners)
			tt.req.GarageID, tt.req.PartnerID, tt.req.ActorID = garageID, partnerID, actorID

			if tt.findErr != nil {
				partners.EXPECT().FindByID(mock.Anything, garageID, partnerID).Return(nil, tt.findErr).Once()
			} else {
				partners.EXPECT().FindByID(mock.Anything, garageID, partnerID).Return(existingPartner(), nil).Once()
			}

			if tt.wantCode == "" || tt.updateErr != nil {
				partners.EXPECT().Update(mock.Anything, mock.Anything).Return(tt.updateErr).Once()
			}

			p, err := uc.Update(t.Context(), tt.req)
			if tt.wantCode != "" {
				assertAppError(t, err, tt.wantCode)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, domain.PartnerTypeInsurer, p.Type)
			assert.Nil(t, p.Contact.ContactPerson, "a new contact replaces the old one")
			assert.Equal(t, "0909123456", *p.Contact.Phone)
			assert.Equal(t, "VIP", *p.Note)
			assert.Equal(t, actorID, *p.UpdatedBy)
		})
	}
}

func TestPartnerUseCase_GetListDelete(t *testing.T) {
	t.Parallel()

	partners := mocks.NewMockPartnerRepository(t)
	uc := usecase.NewPartnerUseCase(partners)

	partners.EXPECT().FindByID(mock.Anything, garageID, partnerID).Return(existingPartner(), nil).Once()
	partners.EXPECT().FindByID(mock.Anything, garageID, "missing").Return(nil, domain.ErrPartnerNotFound).Once()
	partners.EXPECT().List(mock.Anything, garageID, usecase.PartnerFilter{
		ListFilter: usecase.ListFilter{Query: "sai gon", Limit: 20}, Type: domain.PartnerTypeSupplier,
	}).Return([]*domain.Partner{existingPartner()}, 1, nil).Once()
	partners.EXPECT().List(mock.Anything, "garage-2", mock.Anything).Return(nil, 0, errBoom).Once()
	partners.EXPECT().SoftDelete(mock.Anything, garageID, partnerID, actorID).Return(nil).Once()
	partners.EXPECT().SoftDelete(mock.Anything, garageID, "missing", actorID).Return(domain.ErrPartnerNotFound).Once()

	p, err := uc.Get(t.Context(), garageID, partnerID)
	require.NoError(t, err)
	assert.Equal(t, partnerID, p.ID)

	_, err = uc.Get(t.Context(), garageID, "missing")
	assertAppError(t, err, apperror.CodeNotFound)

	page, err := uc.List(t.Context(), usecase.ListPartnersRequest{GarageID: garageID, Type: domain.PartnerTypeSupplier, Query: "sai gon"})
	require.NoError(t, err)
	assert.Equal(t, 1, page.Total)

	_, err = uc.List(t.Context(), usecase.ListPartnersRequest{GarageID: garageID, Type: "bank"})
	assertAppError(t, err, apperror.CodeInvalidArgument)

	_, err = uc.List(t.Context(), usecase.ListPartnersRequest{GarageID: "garage-2"})
	assertAppError(t, err, apperror.CodeInternal)

	require.NoError(t, uc.Delete(t.Context(), garageID, partnerID, actorID))
	assertAppError(t, uc.Delete(t.Context(), garageID, "missing", actorID), apperror.CodeNotFound)
}
