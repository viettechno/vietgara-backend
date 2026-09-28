package handler

import (
	"context"

	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"

	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/platform/httpapi"
	"github.com/viettechno/vietgara-backend/internal/tenant/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// ListGarages lists the garages the caller owns or works in.
func (s *Server) ListGarages(ctx context.Context, _ *tenantv1.ListGaragesRequest) (*tenantv1.ListGaragesResponse, error) {
	garages, err := s.uc.ListGarages(ctx, middleware.AccountID(ctx))
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	resp := &tenantv1.ListGaragesResponse{Data: make([]*tenantv1.Garage, 0, len(garages))}
	for i := range garages {
		resp.Data = append(resp.Data, garageToProto(&garages[i]))
	}

	return resp, nil
}

// CreateGarage creates a garage owned by the caller; answers 201.
func (s *Server) CreateGarage(ctx context.Context, req *tenantv1.CreateGarageRequest) (*tenantv1.CreateGarageResponse, error) {
	fields, err := garageFields(req.GetGarage(), nil)
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	garage, err := s.uc.CreateGarage(ctx, middleware.AccountID(ctx), fields)
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	httpapi.Created(ctx)

	return &tenantv1.CreateGarageResponse{Garage: garageToProto(garage)}, nil
}

// GetGarage returns a garage.
func (s *Server) GetGarage(ctx context.Context, req *tenantv1.GetGarageRequest) (*tenantv1.GetGarageResponse, error) {
	garage, err := s.uc.GetGarage(ctx, req.GetGarageId())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &tenantv1.GetGarageResponse{Garage: garageToProto(garage)}, nil
}

// UpdateGarage changes the fields named by the update mask.
func (s *Server) UpdateGarage(ctx context.Context, req *tenantv1.UpdateGarageRequest) (*tenantv1.UpdateGarageResponse, error) {
	fields, err := garageFields(req.GetGarage(), req.GetUpdateMask().GetPaths())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	garage, err := s.uc.UpdateGarage(ctx, req.GetGarageId(), fields)
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &tenantv1.UpdateGarageResponse{Garage: garageToProto(garage)}, nil
}

// UploadGarageLogo replaces the logo.
func (s *Server) UploadGarageLogo(ctx context.Context, req *tenantv1.UploadGarageLogoRequest) (*tenantv1.UploadGarageLogoResponse, error) {
	garage, err := s.uc.UploadGarageLogo(ctx, req.GetGarageId(), req.GetContent(), req.GetContentType())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &tenantv1.UploadGarageLogoResponse{Garage: garageToProto(garage)}, nil
}

// DeleteGarageLogo removes the logo.
func (s *Server) DeleteGarageLogo(ctx context.Context, req *tenantv1.DeleteGarageLogoRequest) (*tenantv1.DeleteGarageLogoResponse, error) {
	garage, err := s.uc.DeleteGarageLogo(ctx, req.GetGarageId())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &tenantv1.DeleteGarageLogoResponse{Garage: garageToProto(garage)}, nil
}

const pathName = "name"

var garageSettable = []string{
	pathName, "tax_code", "address", "phone", "email", "timezone", "currency", "status", "operating_hours", "numbering",
}

// garageSetters copy one masked field of the API garage into GarageFields.
var garageSetters = map[string]func(f *usecase.GarageFields, in *tenantv1.Garage){
	pathName:   func(f *usecase.GarageFields, in *tenantv1.Garage) { f.Name = new(in.GetName()) },
	"tax_code": func(f *usecase.GarageFields, in *tenantv1.Garage) { f.TaxCode = new(in.GetTaxCode()) },
	"address":  func(f *usecase.GarageFields, in *tenantv1.Garage) { f.Address = new(in.GetAddress()) },
	"phone":    func(f *usecase.GarageFields, in *tenantv1.Garage) { f.Phone = new(in.GetPhone()) },
	"email":    func(f *usecase.GarageFields, in *tenantv1.Garage) { f.Email = new(in.GetEmail()) },
	"timezone": func(f *usecase.GarageFields, in *tenantv1.Garage) { f.Timezone = new(in.GetTimezone()) },
	"currency": func(f *usecase.GarageFields, in *tenantv1.Garage) { f.Currency = new(in.GetCurrency()) },
	"status": func(f *usecase.GarageFields, in *tenantv1.Garage) {
		f.Status = new(garageStatusFromProto(in.GetStatus()))
	},
	"operating_hours": func(f *usecase.GarageFields, in *tenantv1.Garage) {
		f.OperatingHours = new(hoursFromProto(in.GetOperatingHours()))
	},
	"numbering": func(f *usecase.GarageFields, in *tenantv1.Garage) {
		f.QuoteNumberTemplate = new(in.GetNumbering().GetQuote())
		f.RepairOrderNumberTemplate = new(in.GetNumbering().GetRepairOrder())
	},
	"numbering.quote": func(f *usecase.GarageFields, in *tenantv1.Garage) {
		f.QuoteNumberTemplate = new(in.GetNumbering().GetQuote())
	},
	"numbering.repair_order": func(f *usecase.GarageFields, in *tenantv1.Garage) {
		f.RepairOrderNumberTemplate = new(in.GetNumbering().GetRepairOrder())
	},
}

// garageFields reads the masked fields of in (all settable fields that are
// set when paths is nil, i.e. on create).
func garageFields(in *tenantv1.Garage, paths []string) (usecase.GarageFields, error) {
	var f usecase.GarageFields

	if paths == nil {
		paths = presentGarageFields(in)
	}

	for _, path := range maskPaths(paths, garageSettable...) {
		set, ok := garageSetters[path]
		if !ok {
			return f, apperror.Validation("updateMask", apperror.IssueInvalid, "field "+path+" cannot be updated")
		}

		set(&f, in)
	}

	return f, nil
}

// presentGarageFields lists the fields set on a garage being created.
func presentGarageFields(in *tenantv1.Garage) []string {
	paths := []string{pathName}

	for path, set := range map[string]bool{
		"tax_code": in.TaxCode != nil, "address": in.Address != nil, "phone": in.Phone != nil, "email": in.Email != nil,
		"timezone": in.GetTimezone() != "", "currency": in.GetCurrency() != "",
		"operating_hours": len(in.GetOperatingHours()) > 0,
		"numbering.quote": in.GetNumbering().GetQuote() != "", "numbering.repair_order": in.GetNumbering().GetRepairOrder() != "",
	} {
		if set {
			paths = append(paths, path)
		}
	}

	return paths
}

//go:fix inline
