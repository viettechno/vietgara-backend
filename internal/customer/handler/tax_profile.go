package handler

import (
	"context"

	commonv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/common/v1"
	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"

	"github.com/viettechno/vietgara-backend/internal/customer/domain"
	"github.com/viettechno/vietgara-backend/internal/customer/usecase"
	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/platform/access"
	"github.com/viettechno/vietgara-backend/internal/platform/httpapi"
	"github.com/viettechno/vietgara-backend/pkg/converter"
)

// TaxProfileServer implements customerv1.CustomerTaxProfileServiceServer
// (FR-CUS-07).
type TaxProfileServer struct {
	customerv1.UnimplementedCustomerTaxProfileServiceServer

	uc *usecase.TaxProfileUseCase
}

// NewTaxProfileServer wires the handler to its use case.
func NewTaxProfileServer(uc *usecase.TaxProfileUseCase) *TaxProfileServer {
	return &TaxProfileServer{uc: uc}
}

// TaxProfilePolicies declares the permission every tax profile RPC requires
// in its garage.
func TaxProfilePolicies() middleware.Policies {
	read := middleware.GarageMethod(access.Requires(access.CustomerRead))
	write := middleware.GarageMethod(access.Requires(access.CustomerWrite))

	return middleware.Policies{
		customerv1.CustomerTaxProfileService_ListCustomerTaxProfiles_FullMethodName:    read,
		customerv1.CustomerTaxProfileService_GetCustomerTaxProfile_FullMethodName:      read,
		customerv1.CustomerTaxProfileService_CreateCustomerTaxProfile_FullMethodName:   write,
		customerv1.CustomerTaxProfileService_UpdateCustomerTaxProfile_FullMethodName:   write,
		customerv1.CustomerTaxProfileService_DeleteCustomerTaxProfile_FullMethodName:   write,
		customerv1.CustomerTaxProfileService_ActivateCustomerTaxProfile_FullMethodName: write,
	}
}

// ListCustomerTaxProfiles lists the customer's tax profiles.
func (s *TaxProfileServer) ListCustomerTaxProfiles(
	ctx context.Context, req *customerv1.ListCustomerTaxProfilesRequest,
) (*customerv1.ListCustomerTaxProfilesResponse, error) {
	return run(ctx, func(string) (*customerv1.ListCustomerTaxProfilesResponse, error) {
		items, err := s.uc.List(ctx, req.GetGarageId(), req.GetCustomerId(), req.GetIncludeInactive())
		if err != nil {
			return nil, err
		}

		resp := &customerv1.ListCustomerTaxProfilesResponse{Data: make([]*customerv1.CustomerTaxProfile, 0, len(items))}
		for _, p := range items {
			resp.Data = append(resp.Data, TaxProfileToProto(p))
		}

		return resp, nil
	})
}

// CreateCustomerTaxProfile adds a tax profile; answers 201.
func (s *TaxProfileServer) CreateCustomerTaxProfile(
	ctx context.Context, req *customerv1.CreateCustomerTaxProfileRequest,
) (*customerv1.CreateCustomerTaxProfileResponse, error) {
	return run(ctx, func(actorID string) (*customerv1.CreateCustomerTaxProfileResponse, error) {
		in := req.GetTaxProfile()
		isDefault := in.GetIsDefault()

		created, err := s.uc.Create(ctx, usecase.CreateTaxProfileRequest{
			GarageID: req.GetGarageId(), CustomerID: req.GetCustomerId(), ActorID: actorID, Kind: taxProfileKindFromProto(in.GetKind()),
			TaxProfileInput: usecase.TaxProfileInput{
				LegalName: new(in.GetLegalName()), TaxID: in.TaxId, Address: in.Address, InvoiceEmail: in.InvoiceEmail,
				IsDefault: &isDefault,
			},
		})
		if err != nil {
			return nil, err
		}

		httpapi.Created(ctx)

		return &customerv1.CreateCustomerTaxProfileResponse{TaxProfile: TaxProfileToProto(created)}, nil
	})
}

// GetCustomerTaxProfile returns one tax profile.
func (s *TaxProfileServer) GetCustomerTaxProfile(
	ctx context.Context, req *customerv1.GetCustomerTaxProfileRequest,
) (*customerv1.GetCustomerTaxProfileResponse, error) {
	return run(ctx, func(string) (*customerv1.GetCustomerTaxProfileResponse, error) {
		p, err := s.uc.Get(ctx, req.GetGarageId(), req.GetCustomerId(), req.GetTaxProfileId())
		if err != nil {
			return nil, err
		}

		return &customerv1.GetCustomerTaxProfileResponse{TaxProfile: TaxProfileToProto(p)}, nil
	})
}

// UpdateCustomerTaxProfile changes the fields named by the update mask; the
// kind never changes.
func (s *TaxProfileServer) UpdateCustomerTaxProfile(
	ctx context.Context, req *customerv1.UpdateCustomerTaxProfileRequest,
) (*customerv1.UpdateCustomerTaxProfileResponse, error) {
	return run(ctx, func(actorID string) (*customerv1.UpdateCustomerTaxProfileResponse, error) {
		in := req.GetTaxProfile()
		update := usecase.UpdateTaxProfileRequest{
			GarageID: req.GetGarageId(), CustomerID: req.GetCustomerId(), TaxProfileID: req.GetTaxProfileId(), ActorID: actorID,
		}

		for _, path := range maskPaths(req.GetUpdateMask().GetPaths(), "legal_name", "tax_id", "address", "invoice_email", "is_default") {
			switch path {
			case "legal_name":
				update.LegalName = new(in.GetLegalName())
			case "tax_id":
				update.TaxID = stringField(in.TaxId)
			case "address":
				update.Address = stringField(in.Address)
			case "invoice_email":
				update.InvoiceEmail = stringField(in.InvoiceEmail)
			case "is_default":
				update.IsDefault = new(in.GetIsDefault())
			default:
				return nil, badMask(path)
			}
		}

		updated, err := s.uc.Update(ctx, update)
		if err != nil {
			return nil, err
		}

		return &customerv1.UpdateCustomerTaxProfileResponse{TaxProfile: TaxProfileToProto(updated)}, nil
	})
}

// DeleteCustomerTaxProfile deletes or deactivates a tax profile (FR-CAT-07)
// and answers which; with dry_run it only says which.
func (s *TaxProfileServer) DeleteCustomerTaxProfile(
	ctx context.Context, req *customerv1.DeleteCustomerTaxProfileRequest,
) (*customerv1.DeleteCustomerTaxProfileResponse, error) {
	return run(ctx, func(actorID string) (*customerv1.DeleteCustomerTaxProfileResponse, error) {
		outcome, err := s.uc.Delete(ctx, usecase.DeleteTaxProfileRequest{
			GarageID: req.GetGarageId(), CustomerID: req.GetCustomerId(), TaxProfileID: req.GetTaxProfileId(),
			ActorID: actorID, DryRun: req.GetDryRun(),
		})
		if err != nil {
			return nil, err
		}

		return &customerv1.DeleteCustomerTaxProfileResponse{Outcome: httpapi.DeleteOutcome(outcome)}, nil
	})
}

// ActivateCustomerTaxProfile reactivates a deactivated tax profile.
func (s *TaxProfileServer) ActivateCustomerTaxProfile(
	ctx context.Context, req *customerv1.ActivateCustomerTaxProfileRequest,
) (*customerv1.ActivateCustomerTaxProfileResponse, error) {
	return run(ctx, func(actorID string) (*customerv1.ActivateCustomerTaxProfileResponse, error) {
		p, err := s.uc.Activate(ctx, req.GetGarageId(), req.GetCustomerId(), req.GetTaxProfileId(), actorID)
		if err != nil {
			return nil, err
		}

		return &customerv1.ActivateCustomerTaxProfileResponse{TaxProfile: TaxProfileToProto(p)}, nil
	})
}

// TaxProfileToProto maps a tax profile to its wire form.
func TaxProfileToProto(p *domain.TaxProfile) *customerv1.CustomerTaxProfile {
	return &customerv1.CustomerTaxProfile{
		Id: p.ID, GarageId: p.GarageID, CustomerId: p.CustomerID, Kind: taxProfileKindToProto(p.Kind),
		LegalName: p.LegalName, TaxId: p.TaxID, Address: p.Address, InvoiceEmail: p.InvoiceEmail,
		IsDefault: p.IsDefault, IsActive: p.IsActive,
		CreatedAt: converter.TimeToProto(p.CreatedAt), UpdatedAt: converter.TimeToProto(p.UpdatedAt),
	}
}

func taxProfileKindToProto(k domain.TaxProfileKind) commonv1.TaxProfileKind {
	switch k {
	case domain.TaxProfilePersonal:
		return commonv1.TaxProfileKind_TAX_PROFILE_KIND_PERSONAL
	case domain.TaxProfileCompany:
		return commonv1.TaxProfileKind_TAX_PROFILE_KIND_COMPANY
	default:
		return commonv1.TaxProfileKind_TAX_PROFILE_KIND_UNSPECIFIED
	}
}

func taxProfileKindFromProto(k commonv1.TaxProfileKind) domain.TaxProfileKind {
	switch k {
	case commonv1.TaxProfileKind_TAX_PROFILE_KIND_PERSONAL:
		return domain.TaxProfilePersonal
	case commonv1.TaxProfileKind_TAX_PROFILE_KIND_COMPANY:
		return domain.TaxProfileCompany
	case commonv1.TaxProfileKind_TAX_PROFILE_KIND_UNSPECIFIED:
		return ""
	default:
		return ""
	}
}
