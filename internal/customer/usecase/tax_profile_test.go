package usecase_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/customer/domain"
	"github.com/viettechno/vietgara-backend/internal/customer/usecase"
	"github.com/viettechno/vietgara-backend/internal/mocks"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
	"github.com/viettechno/vietgara-backend/pkg/lifecycle"
)

// profileStore is an in-memory TaxProfileRepository that enforces the
// one-personal and one-default rules like the unique indexes do.
type profileStore struct {
	profiles []*domain.TaxProfile
	deleted  []string
}

func (s *profileStore) Create(_ context.Context, p *domain.TaxProfile) error {
	for _, e := range s.profiles {
		if e.CustomerID == p.CustomerID && e.Kind == domain.TaxProfilePersonal && p.Kind == domain.TaxProfilePersonal {
			return domain.ErrPersonalProfileTaken
		}
	}

	s.profiles = append(s.profiles, p)

	return nil
}

func (s *profileStore) FindByID(_ context.Context, _, customerID, id string) (*domain.TaxProfile, error) {
	for _, p := range s.profiles {
		if p.ID == id && p.CustomerID == customerID {
			return p, nil
		}
	}

	return nil, domain.ErrTaxProfileNotFound
}

func (s *profileStore) Update(context.Context, *domain.TaxProfile) error { return nil }

func (s *profileStore) List(_ context.Context, _, customerID string, includeInactive bool) ([]*domain.TaxProfile, error) {
	var out []*domain.TaxProfile

	for _, p := range s.profiles {
		if p.CustomerID == customerID && (includeInactive || p.IsActive) {
			out = append(out, p)
		}
	}

	return out, nil
}

func (s *profileStore) ClearDefault(_ context.Context, _, customerID, exceptID string) error {
	for _, p := range s.profiles {
		if p.CustomerID == customerID && p.ID != exceptID {
			p.IsDefault = false
		}
	}

	return nil
}

func (s *profileStore) SoftDelete(_ context.Context, _, _, id, _ string) error {
	s.deleted = append(s.deleted, id)

	return nil
}

func (s *profileStore) SetActive(_ context.Context, _, customerID, id string, active bool, _ string) error {
	p, err := s.FindByID(context.Background(), "", customerID, id)
	if err != nil {
		return err
	}

	p.IsActive = active
	p.IsDefault = p.IsDefault && active

	return nil
}

type profileUsage struct{ used bool }

func (u profileUsage) TaxProfileInUse(context.Context, string, string) (bool, error) {
	return u.used, nil
}

func newProfileUseCase(t *testing.T, used bool) (*usecase.TaxProfileUseCase, *profileStore) {
	t.Helper()

	customers := mocks.NewMockCustomerRepository(t)
	customers.EXPECT().FindByID(mock.Anything, garageID, customerID).Return(&domain.Customer{ID: customerID}, nil).Maybe()
	customers.EXPECT().FindByID(mock.Anything, garageID, "missing").Return(nil, domain.ErrCustomerNotFound).Maybe()

	store := &profileStore{}

	return usecase.NewTaxProfileUseCase(store, customers, profileUsage{used: used}), store
}

func companyRequest() usecase.CreateTaxProfileRequest {
	return usecase.CreateTaxProfileRequest{
		GarageID: garageID, CustomerID: customerID, ActorID: actorID, Kind: domain.TaxProfileCompany,
		TaxProfileInput: usecase.TaxProfileInput{
			LegalName: new(" Cong ty An Phat "), TaxID: new("0301 234-567"), Address: new("12 Le Loi"),
			InvoiceEmail: new("Ketoan@AnPhat.vn"), IsDefault: new(true),
		},
	}
}

func TestTaxProfile_Create(t *testing.T) {
	t.Parallel()

	uc, _ := newProfileUseCase(t, false)

	p, err := uc.Create(t.Context(), companyRequest())
	require.NoError(t, err)
	assert.Equal(t, "Cong ty An Phat", p.LegalName)
	assert.Equal(t, "0301234567", *p.TaxID, "separators are stripped")
	assert.Equal(t, "ketoan@anphat.vn", *p.InvoiceEmail)
	assert.True(t, p.IsDefault)
	assert.True(t, p.IsActive)
	assert.True(t, p.InvoiceReady())

	// A second default takes the mark over.
	second := companyRequest()
	second.TaxID = new("0301234567001")
	other, err := uc.Create(t.Context(), second)
	require.NoError(t, err)
	assert.True(t, other.IsDefault)
	assert.False(t, p.IsDefault)

	personal := usecase.CreateTaxProfileRequest{
		GarageID: garageID, CustomerID: customerID, Kind: domain.TaxProfilePersonal,
		TaxProfileInput: usecase.TaxProfileInput{LegalName: new("Tran Binh")},
	}
	got, err := uc.Create(t.Context(), personal)
	require.NoError(t, err)
	assert.False(t, got.InvoiceReady(), "no tax id or address")

	_, err = uc.Create(t.Context(), personal)
	assertAppError(t, err, apperror.CodeInvalidArgument)
}

func TestTaxProfile_CreateValidation(t *testing.T) {
	t.Parallel()

	uc, _ := newProfileUseCase(t, false)

	for _, mutate := range map[string]func(*usecase.CreateTaxProfileRequest){
		"unknown kind":   func(r *usecase.CreateTaxProfileRequest) { r.Kind = "" },
		"no legal name":  func(r *usecase.CreateTaxProfileRequest) { r.LegalName = nil },
		"blank name":     func(r *usecase.CreateTaxProfileRequest) { r.LegalName = new("  ") },
		"short tax id":   func(r *usecase.CreateTaxProfileRequest) { r.TaxID = new("12345") },
		"lettered tax":   func(r *usecase.CreateTaxProfileRequest) { r.TaxID = new("03012345AB") },
		"bad e-mail":     func(r *usecase.CreateTaxProfileRequest) { r.InvoiceEmail = new("not-an-email") },
		"address length": func(r *usecase.CreateTaxProfileRequest) { r.Address = new(string(make([]byte, 501))) },
	} {
		req := companyRequest()
		mutate(&req)

		_, err := uc.Create(t.Context(), req)
		assertAppError(t, err, apperror.CodeInvalidArgument)
	}

	req := companyRequest()
	req.CustomerID = "missing"
	_, err := uc.Create(t.Context(), req)
	assertAppError(t, err, apperror.CodeNotFound)
}

func TestTaxProfile_UpdateGetList(t *testing.T) {
	t.Parallel()

	uc, _ := newProfileUseCase(t, false)

	p, err := uc.Create(t.Context(), companyRequest())
	require.NoError(t, err)

	updated, err := uc.Update(t.Context(), usecase.UpdateTaxProfileRequest{
		GarageID: garageID, CustomerID: customerID, TaxProfileID: p.ID, ActorID: actorID,
		TaxProfileInput: usecase.TaxProfileInput{LegalName: new("Cong ty Moi"), InvoiceEmail: new(""), IsDefault: new(false)},
	})
	require.NoError(t, err)
	assert.Equal(t, "Cong ty Moi", updated.LegalName)
	assert.Nil(t, updated.InvoiceEmail, "a blank clears an optional field")
	assert.False(t, updated.IsDefault)
	assert.Equal(t, "0301234567", *updated.TaxID, "untouched")

	_, err = uc.Update(t.Context(), usecase.UpdateTaxProfileRequest{
		GarageID: garageID, CustomerID: customerID, TaxProfileID: p.ID,
		TaxProfileInput: usecase.TaxProfileInput{TaxID: new("1")},
	})
	assertAppError(t, err, apperror.CodeInvalidArgument)

	_, err = uc.Update(t.Context(), usecase.UpdateTaxProfileRequest{GarageID: garageID, CustomerID: customerID, TaxProfileID: "x"})
	assertAppError(t, err, apperror.CodeNotFound)

	got, err := uc.Get(t.Context(), garageID, customerID, p.ID)
	require.NoError(t, err)
	assert.Equal(t, p.ID, got.ID)

	_, err = uc.Get(t.Context(), garageID, customerID, "x")
	assertAppError(t, err, apperror.CodeNotFound)

	list, err := uc.List(t.Context(), garageID, customerID, false)
	require.NoError(t, err)
	assert.Len(t, list, 1)

	_, err = uc.List(t.Context(), garageID, "missing", false)
	assertAppError(t, err, apperror.CodeNotFound)
}

func TestTaxProfile_DefaultOnUpdateClearsTheOthers(t *testing.T) {
	t.Parallel()

	uc, _ := newProfileUseCase(t, false)

	first, err := uc.Create(t.Context(), companyRequest())
	require.NoError(t, err)

	req := companyRequest()
	req.IsDefault = new(false)
	second, err := uc.Create(t.Context(), req)
	require.NoError(t, err)

	_, err = uc.Update(t.Context(), usecase.UpdateTaxProfileRequest{
		GarageID: garageID, CustomerID: customerID, TaxProfileID: second.ID,
		TaxProfileInput: usecase.TaxProfileInput{IsDefault: new(true)},
	})
	require.NoError(t, err)
	assert.True(t, second.IsDefault)
	assert.False(t, first.IsDefault)
}

func TestTaxProfile_DeleteOrDeactivate(t *testing.T) {
	t.Parallel()

	req := func(id string, dry bool) usecase.DeleteTaxProfileRequest {
		return usecase.DeleteTaxProfileRequest{GarageID: garageID, CustomerID: customerID, TaxProfileID: id, ActorID: actorID, DryRun: dry}
	}

	t.Run("unused is deleted", func(t *testing.T) {
		t.Parallel()

		uc, store := newProfileUseCase(t, false)
		p, err := uc.Create(t.Context(), companyRequest())
		require.NoError(t, err)

		outcome, err := uc.Delete(t.Context(), req(p.ID, true))
		require.NoError(t, err)
		assert.Equal(t, lifecycle.Deleted, outcome)
		assert.Empty(t, store.deleted, "a dry run changes nothing")

		outcome, err = uc.Delete(t.Context(), req(p.ID, false))
		require.NoError(t, err)
		assert.Equal(t, lifecycle.Deleted, outcome)
		assert.Equal(t, []string{p.ID}, store.deleted)
	})

	t.Run("billed is deactivated, and can come back", func(t *testing.T) {
		t.Parallel()

		uc, store := newProfileUseCase(t, true)
		p, err := uc.Create(t.Context(), companyRequest())
		require.NoError(t, err)

		outcome, err := uc.Delete(t.Context(), req(p.ID, false))
		require.NoError(t, err)
		assert.Equal(t, lifecycle.Deactivated, outcome)
		assert.False(t, p.IsActive)
		assert.False(t, p.IsDefault, "an inactive profile is no default")
		assert.Empty(t, store.deleted)

		back, err := uc.Activate(t.Context(), garageID, customerID, p.ID, actorID)
		require.NoError(t, err)
		assert.True(t, back.IsActive)
	})

	t.Run("unknown", func(t *testing.T) {
		t.Parallel()

		uc, _ := newProfileUseCase(t, false)

		_, err := uc.Delete(t.Context(), req("x", false))
		assertAppError(t, err, apperror.CodeNotFound)

		_, err = uc.Activate(t.Context(), garageID, customerID, "x", actorID)
		assertAppError(t, err, apperror.CodeNotFound)
	})
}

func TestTaxProfile_Resolve(t *testing.T) {
	t.Parallel()

	uc, store := newProfileUseCase(t, false)

	// No profile at all: retail.
	party, err := uc.Resolve(t.Context(), garageID, customerID, nil)
	require.NoError(t, err)
	assert.True(t, party.IsZero())

	plain := companyRequest()
	plain.IsDefault = new(false)
	other, err := uc.Create(t.Context(), plain)
	require.NoError(t, err)

	// Profiles but no default: still retail when none is chosen.
	party, err = uc.Resolve(t.Context(), garageID, customerID, nil)
	require.NoError(t, err)
	assert.True(t, party.IsZero())

	def, err := uc.Create(t.Context(), companyRequest())
	require.NoError(t, err)

	party, err = uc.Resolve(t.Context(), garageID, customerID, nil)
	require.NoError(t, err)
	assert.Equal(t, def.ID, party.TaxProfileID)
	assert.Equal(t, "Cong ty An Phat", party.LegalName)
	assert.Equal(t, "0301234567", party.TaxID)
	assert.Equal(t, "12 Le Loi", party.Address)
	assert.Equal(t, "ketoan@anphat.vn", party.InvoiceEmail)
	assert.Equal(t, "company", party.Kind)
	assert.True(t, party.InvoiceReady())

	// Retail on purpose.
	party, err = uc.Resolve(t.Context(), garageID, customerID, new(""))
	require.NoError(t, err)
	assert.True(t, party.IsZero())

	// A named profile.
	party, err = uc.Resolve(t.Context(), garageID, customerID, &other.ID)
	require.NoError(t, err)
	assert.Equal(t, other.ID, party.TaxProfileID)

	// Not the customer's, or deactivated: unusable.
	_, err = uc.Resolve(t.Context(), garageID, customerID, new("nobody"))
	assertAppError(t, err, apperror.CodeInvalidArgument)

	other.IsActive = false
	_, err = uc.Resolve(t.Context(), garageID, customerID, &other.ID)
	assertAppError(t, err, apperror.CodeInvalidArgument)

	_ = store
}
