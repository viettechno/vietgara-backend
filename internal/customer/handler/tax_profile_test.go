package handler_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	commonv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/common/v1"
	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"github.com/viettechno/vietgara-backend/internal/customer/domain"
	"github.com/viettechno/vietgara-backend/internal/customer/handler"
	"github.com/viettechno/vietgara-backend/internal/customer/usecase"
	"github.com/viettechno/vietgara-backend/internal/mocks"
)

const taxCustomerID = "33333333-3333-4333-8333-333333333333"

// memProfiles keeps the profiles of one customer in memory.
type memProfiles struct{ items []*domain.TaxProfile }

func (m *memProfiles) Create(_ context.Context, p *domain.TaxProfile) error {
	m.items = append(m.items, p)

	return nil
}

func (m *memProfiles) FindByID(_ context.Context, _, _, id string) (*domain.TaxProfile, error) {
	for _, p := range m.items {
		if p.ID == id {
			return p, nil
		}
	}

	return nil, domain.ErrTaxProfileNotFound
}

func (m *memProfiles) Update(context.Context, *domain.TaxProfile) error { return nil }

func (m *memProfiles) List(context.Context, string, string, bool) ([]*domain.TaxProfile, error) {
	return m.items, nil
}

func (m *memProfiles) ClearDefault(context.Context, string, string, string) error { return nil }

func (m *memProfiles) SoftDelete(context.Context, string, string, string, string) error { return nil }

func (m *memProfiles) SetActive(_ context.Context, _, _, id string, active bool, _ string) error {
	p, err := m.FindByID(context.Background(), "", "", id)
	if err != nil {
		return err
	}

	p.IsActive = active

	return nil
}

type notBilled struct{}

func (notBilled) TaxProfileInUse(context.Context, string, string) (bool, error) { return false, nil }

func TestTaxProfilePolicies(t *testing.T) {
	t.Parallel()

	p := handler.TaxProfilePolicies()
	assert.Len(t, p, 6)
	assert.NotNil(t, p[customerv1.CustomerTaxProfileService_CreateCustomerTaxProfile_FullMethodName].Garage)
}

func TestTaxProfileRPCs(t *testing.T) {
	t.Parallel()

	customers := mocks.NewMockCustomerRepository(t)
	customers.EXPECT().FindByID(anyArg, garageID, taxCustomerID).Return(&domain.Customer{ID: taxCustomerID}, nil).Maybe()

	srv := handler.NewTaxProfileServer(usecase.NewTaxProfileUseCase(&memProfiles{}, customers, notBilled{}))
	ctx := signedIn(t)

	created, err := srv.CreateCustomerTaxProfile(ctx, &customerv1.CreateCustomerTaxProfileRequest{
		GarageId: garageID, CustomerId: taxCustomerID,
		TaxProfile: &customerv1.CustomerTaxProfile{
			Kind: commonv1.TaxProfileKind_TAX_PROFILE_KIND_COMPANY, LegalName: "Cong ty An Phat",
			TaxId: new("0301234567"), Address: new("12 Le Loi"), IsDefault: true,
		},
	})
	require.NoError(t, err)

	p := created.GetTaxProfile()
	assert.Equal(t, commonv1.TaxProfileKind_TAX_PROFILE_KIND_COMPANY, p.GetKind())
	assert.Equal(t, "0301234567", p.GetTaxId())
	assert.True(t, p.GetIsDefault())
	assert.True(t, p.GetIsActive())

	got, err := srv.GetCustomerTaxProfile(ctx, &customerv1.GetCustomerTaxProfileRequest{
		GarageId: garageID, CustomerId: taxCustomerID, TaxProfileId: p.GetId(),
	})
	require.NoError(t, err)
	assert.Equal(t, p.GetId(), got.GetTaxProfile().GetId())

	list, err := srv.ListCustomerTaxProfiles(ctx, &customerv1.ListCustomerTaxProfilesRequest{GarageId: garageID, CustomerId: taxCustomerID})
	require.NoError(t, err)
	assert.Len(t, list.GetData(), 1)

	updated, err := srv.UpdateCustomerTaxProfile(ctx, &customerv1.UpdateCustomerTaxProfileRequest{
		GarageId: garageID, CustomerId: taxCustomerID, TaxProfileId: p.GetId(),
		TaxProfile: &customerv1.CustomerTaxProfile{LegalName: "Cong ty Moi", Address: new("")},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"legal_name", "address", "invoice_email"}},
	})
	require.NoError(t, err)
	assert.Equal(t, "Cong ty Moi", updated.GetTaxProfile().GetLegalName())
	assert.Empty(t, updated.GetTaxProfile().GetAddress())

	_, err = srv.UpdateCustomerTaxProfile(ctx, &customerv1.UpdateCustomerTaxProfileRequest{
		GarageId: garageID, CustomerId: taxCustomerID, TaxProfileId: p.GetId(),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"kind"}},
	})
	assert.Equal(t, codes.InvalidArgument, status.Code(err))

	dry, err := srv.DeleteCustomerTaxProfile(ctx, &customerv1.DeleteCustomerTaxProfileRequest{
		GarageId: garageID, CustomerId: taxCustomerID, TaxProfileId: p.GetId(), DryRun: true,
	})
	require.NoError(t, err)
	assert.Equal(t, commonv1.DeleteOutcome_DELETE_OUTCOME_DELETED, dry.GetOutcome())

	active, err := srv.ActivateCustomerTaxProfile(ctx, &customerv1.ActivateCustomerTaxProfileRequest{
		GarageId: garageID, CustomerId: taxCustomerID, TaxProfileId: p.GetId(),
	})
	require.NoError(t, err)
	assert.True(t, active.GetTaxProfile().GetIsActive())

	_, err = srv.GetCustomerTaxProfile(ctx, &customerv1.GetCustomerTaxProfileRequest{
		GarageId: garageID, CustomerId: taxCustomerID, TaxProfileId: "missing",
	})
	assert.Equal(t, codes.NotFound, status.Code(err))

	_, err = srv.CreateCustomerTaxProfile(ctx, &customerv1.CreateCustomerTaxProfileRequest{
		GarageId: garageID, CustomerId: taxCustomerID, TaxProfile: &customerv1.CustomerTaxProfile{LegalName: "x"},
	})
	assert.Equal(t, codes.InvalidArgument, status.Code(err), "a kind is required")
}
