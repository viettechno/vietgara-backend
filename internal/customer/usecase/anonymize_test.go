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
)

type publishedEvent struct {
	typ     string
	garage  string
	payload any
}

type eventRecorder struct {
	events []publishedEvent
	err    error
}

func (r *eventRecorder) Publish(_ context.Context, eventType, garageID, _ string, payload any) error {
	r.events = append(r.events, publishedEvent{eventType, garageID, payload})

	return r.err
}

func TestCustomerUseCase_Anonymize(t *testing.T) {
	t.Parallel()

	setup := func(t *testing.T, opts ...usecase.Option) (*usecase.CustomerUseCase, *mocks.MockCustomerRepository) {
		t.Helper()

		customers := mocks.NewMockCustomerRepository(t)

		return usecase.NewCustomerUseCase(customers, mocks.NewMockVehicleRepository(t), nil, opts...), customers
	}

	t.Run("erases, audits and tells the other modules", func(t *testing.T) {
		t.Parallel()

		events, auditLog := &eventRecorder{}, &auditor{}
		uc, customers := setup(t, usecase.WithEvents(events), usecase.WithAuditor(auditLog))
		customers.EXPECT().FindByID(mock.Anything, garageID, customerID).Return(existingCustomer(), nil).Once()
		customers.EXPECT().Anonymize(mock.Anything, garageID, customerID, actorID).Return(domain.AnonymizedCounts{Customers: 1, Vehicles: 2, TaxProfiles: 1}, nil).Once()

		counts, err := uc.Anonymize(t.Context(), garageID, customerID, actorID)
		require.NoError(t, err)
		assert.Equal(t, domain.AnonymizedCounts{Customers: 1, Vehicles: 2, TaxProfiles: 1}, counts)
		require.Len(t, events.events, 1)
		assert.Equal(t, usecase.EventCustomerAnonymized, events.events[0].typ)
		assert.Equal(t, garageID, events.events[0].garage)
		require.Len(t, auditLog.entries, 1)
		assert.Equal(t, "customer.anonymized", auditLog.entries[0].Action)
		assert.Equal(t, customerID, auditLog.entries[0].EntityID)
	})

	t.Run("works without an auditor or an event bus", func(t *testing.T) {
		t.Parallel()

		uc, customers := setup(t)
		customers.EXPECT().FindByID(mock.Anything, garageID, customerID).Return(existingCustomer(), nil).Once()
		customers.EXPECT().Anonymize(mock.Anything, garageID, customerID, actorID).Return(domain.AnonymizedCounts{Customers: 1}, nil).Once()

		_, err := uc.Anonymize(t.Context(), garageID, customerID, actorID)
		require.NoError(t, err)
	})

	t.Run("a customer already anonymized is refused with its reason", func(t *testing.T) {
		t.Parallel()

		uc, customers := setup(t)
		done := existingCustomer()
		done.AnonymizedAt = new(fixedNow)
		customers.EXPECT().FindByID(mock.Anything, garageID, customerID).Return(done, nil).Once()

		_, err := uc.Anonymize(t.Context(), garageID, customerID, actorID)
		assertAppError(t, err, apperror.CodeFailedPrecondition)

		appErr, _ := apperror.As(err)
		assert.Equal(t, domain.ReasonCustomerAnonymized, appErr.Reason)
	})

	failures := map[string]func(c *mocks.MockCustomerRepository) []usecase.Option{
		"unknown customer": func(c *mocks.MockCustomerRepository) []usecase.Option {
			c.EXPECT().FindByID(mock.Anything, garageID, customerID).Return(nil, domain.ErrCustomerNotFound).Once()

			return nil
		},
		"the repository": func(c *mocks.MockCustomerRepository) []usecase.Option {
			c.EXPECT().FindByID(mock.Anything, garageID, customerID).Return(existingCustomer(), nil).Once()
			c.EXPECT().Anonymize(mock.Anything, garageID, customerID, actorID).Return(domain.AnonymizedCounts{}, errBoom).Once()

			return nil
		},
		"the audit log": func(c *mocks.MockCustomerRepository) []usecase.Option {
			c.EXPECT().FindByID(mock.Anything, garageID, customerID).Return(existingCustomer(), nil).Once()
			c.EXPECT().Anonymize(mock.Anything, garageID, customerID, actorID).Return(domain.AnonymizedCounts{Customers: 1}, nil).Once()

			return []usecase.Option{usecase.WithAuditor(&auditor{err: errBoom})}
		},
		"the event bus": func(c *mocks.MockCustomerRepository) []usecase.Option {
			c.EXPECT().FindByID(mock.Anything, garageID, customerID).Return(existingCustomer(), nil).Once()
			c.EXPECT().Anonymize(mock.Anything, garageID, customerID, actorID).Return(domain.AnonymizedCounts{Customers: 1}, nil).Once()

			return []usecase.Option{usecase.WithEvents(&eventRecorder{err: errBoom})}
		},
	}

	for name, arrange := range failures {
		t.Run("fails on "+name, func(t *testing.T) {
			t.Parallel()

			customers := mocks.NewMockCustomerRepository(t)
			uc := usecase.NewCustomerUseCase(customers, mocks.NewMockVehicleRepository(t), nil, arrange(customers)...)

			_, err := uc.Anonymize(t.Context(), garageID, customerID, actorID)
			require.Error(t, err)
		})
	}
}

func TestCustomerUseCase_AnonymizedCustomerCannotBeChanged(t *testing.T) {
	t.Parallel()

	done := func() *domain.Customer {
		c := existingCustomer()
		c.AnonymizedAt = new(fixedNow)

		return c
	}

	customers := mocks.NewMockCustomerRepository(t)
	customers.EXPECT().FindByID(mock.Anything, garageID, customerID).Return(done(), nil)
	uc := usecase.NewCustomerUseCase(customers, mocks.NewMockVehicleRepository(t), nil, clock())

	_, err := uc.Update(t.Context(), usecase.UpdateCustomerRequest{GarageID: garageID, CustomerID: customerID, ActorID: actorID, FullName: new("New name")})
	assertAppError(t, err, apperror.CodeFailedPrecondition)

	vehicles := mocks.NewMockVehicleRepository(t)
	vehicleUC := usecase.NewVehicleUseCase(vehicles, customers, nil, nil, clock())

	_, err = vehicleUC.Create(t.Context(), usecase.CreateVehicleRequest{GarageID: garageID, CustomerID: customerID, LicensePlate: "51F-123.45"})
	assertAppError(t, err, apperror.CodeFailedPrecondition)

	store := &profileStore{}
	profileUC := usecase.NewTaxProfileUseCase(store, customers, profileUsage{})

	_, err = profileUC.Create(t.Context(), usecase.CreateTaxProfileRequest{GarageID: garageID, CustomerID: customerID, Kind: domain.TaxProfilePersonal})
	assertAppError(t, err, apperror.CodeFailedPrecondition)
}

func TestCustomerUseCase_OptOutEmailOfAnAnonymizedCustomerChangesNothing(t *testing.T) {
	t.Parallel()

	done := existingCustomer()
	done.AnonymizedAt = new(fixedNow)

	customers := mocks.NewMockCustomerRepository(t)
	customers.EXPECT().FindByID(mock.Anything, garageID, customerID).Return(done, nil).Once()
	uc := usecase.NewCustomerUseCase(customers, mocks.NewMockVehicleRepository(t), nil, clock())

	require.NoError(t, uc.OptOutEmail(t.Context(), garageID, customerID))
}

func TestCustomerUseCase_Export(t *testing.T) {
	t.Parallel()

	year := 2019
	vehicle := &domain.Vehicle{ID: "v-1", LicensePlate: "51F-123.45", Make: new("Toyota"), Year: &year, VIN: new("VIN1"), EngineNumber: new("E1")}
	profile := &domain.TaxProfile{ID: "p-1", Kind: domain.TaxProfileCompany, LegalName: "Cty A", TaxID: new("0301234567"), Address: new("1 Le Loi")}

	build := func(t *testing.T, withProfiles bool) (*usecase.CustomerUseCase, *mocks.MockCustomerRepository, *mocks.MockVehicleRepository, *profileStore) {
		t.Helper()

		customers, vehicles, store := mocks.NewMockCustomerRepository(t), mocks.NewMockVehicleRepository(t), &profileStore{}

		var opts []usecase.Option
		if withProfiles {
			opts = append(opts, usecase.WithTaxProfiles(store))
		}

		return usecase.NewCustomerUseCase(customers, vehicles, nil, opts...), customers, vehicles, store
	}

	t.Run("gathers the customer, vehicles, tax profiles and consent log", func(t *testing.T) {
		t.Parallel()

		uc, customers, vehicles, store := build(t, true)
		c := existingCustomer()
		c.Email, c.Address, c.Locale, c.CreatedAt = new("binh@example.com"), new("1 Le Loi"), "vi", fixedNow
		customers.EXPECT().FindByID(mock.Anything, garageID, customerID).Return(c, nil).Once()
		vehicles.EXPECT().List(mock.Anything, garageID, mock.Anything).Return([]*domain.Vehicle{vehicle}, 1, nil).Once()

		profile.CustomerID = customerID
		require.NoError(t, store.Create(t.Context(), profile))

		got, err := uc.Export(t.Context(), garageID, customerID)
		require.NoError(t, err)
		assert.Equal(t, "Tran Binh", got.Customer.FullName)
		assert.Equal(t, "binh@example.com", got.Customer.Email)
		require.Len(t, got.Vehicles, 1)
		assert.Equal(t, "VIN1", got.Vehicles[0].VIN)
		assert.Equal(t, 2019, got.Vehicles[0].Year)
		require.Len(t, got.TaxProfiles, 1)
		assert.Equal(t, "0301234567", got.TaxProfiles[0].TaxID)
		require.Len(t, got.ConsentLog, 1)
		assert.True(t, got.ConsentLog[0].Granted)
	})

	t.Run("without a tax profile source it lists none", func(t *testing.T) {
		t.Parallel()

		uc, customers, vehicles, _ := build(t, false)
		customers.EXPECT().FindByID(mock.Anything, garageID, customerID).Return(existingCustomer(), nil).Once()
		vehicles.EXPECT().List(mock.Anything, garageID, mock.Anything).Return(nil, 0, nil).Once()

		got, err := uc.Export(t.Context(), garageID, customerID)
		require.NoError(t, err)
		assert.Empty(t, got.Vehicles)
		assert.Empty(t, got.TaxProfiles)
	})

	t.Run("refuses an anonymized or unknown customer and reports failures", func(t *testing.T) {
		t.Parallel()

		uc, customers, vehicles, _ := build(t, true)
		gone := existingCustomer()
		gone.AnonymizedAt = new(fixedNow)
		customers.EXPECT().FindByID(mock.Anything, garageID, "c-gone").Return(gone, nil).Once()
		customers.EXPECT().FindByID(mock.Anything, garageID, "c-missing").Return(nil, domain.ErrCustomerNotFound).Once()
		customers.EXPECT().FindByID(mock.Anything, garageID, customerID).Return(existingCustomer(), nil).Once()
		vehicles.EXPECT().List(mock.Anything, garageID, mock.Anything).Return(nil, 0, errBoom).Once()

		_, err := uc.Export(t.Context(), garageID, "c-gone")
		assertAppError(t, err, apperror.CodeFailedPrecondition)

		_, err = uc.Export(t.Context(), garageID, "c-missing")
		assertAppError(t, err, apperror.CodeNotFound)

		_, err = uc.Export(t.Context(), garageID, customerID)
		assertAppError(t, err, apperror.CodeInternal)
	})

	t.Run("fails when the tax profiles cannot be read", func(t *testing.T) {
		t.Parallel()

		customers, vehicles := mocks.NewMockCustomerRepository(t), mocks.NewMockVehicleRepository(t)
		customers.EXPECT().FindByID(mock.Anything, garageID, customerID).Return(existingCustomer(), nil).Once()
		vehicles.EXPECT().List(mock.Anything, garageID, mock.Anything).Return(nil, 0, nil).Once()
		uc := usecase.NewCustomerUseCase(customers, vehicles, nil, usecase.WithTaxProfiles(failingProfiles{&profileStore{}}))

		_, err := uc.Export(t.Context(), garageID, customerID)
		assertAppError(t, err, apperror.CodeInternal)
	})
}

// failingProfiles lists no tax profile: it fails.
type failingProfiles struct{ *profileStore }

func (failingProfiles) List(context.Context, string, string, bool) ([]*domain.TaxProfile, error) {
	return nil, errBoom
}
