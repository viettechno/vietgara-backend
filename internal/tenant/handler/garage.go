package handler

import (
	"context"

	"github.com/viettechno/vietgara-backend/internal/tenant/domain"
	"github.com/viettechno/vietgara-backend/internal/tenant/usecase"
	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"
)

// CreateGarage validates the request, creates the garage for the caller's
// account, and returns its API representation.
func (s *Server) CreateGarage(ctx context.Context, req *tenantv1.CreateGarageRequest) (*tenantv1.CreateGarageResponse, error) {
	accountID, err := accountIDFromContext(ctx)
	if err != nil {
		return nil, err
	}

	g, err := s.garage.Create(ctx, usecase.CreateGarageRequest{
		AccountID: accountID,
		Name:      req.GetName(),
		TaxCode:   req.TaxCode,
		Address:   req.Address,
		Timezone:  req.Timezone,
		Currency:  req.Currency,
	})
	if err != nil {
		return nil, toGRPCError(err)
	}

	return &tenantv1.CreateGarageResponse{Garage: garageToProto(g)}, nil
}

// GetGarage returns the garage with the given id.
func (s *Server) GetGarage(ctx context.Context, req *tenantv1.GetGarageRequest) (*tenantv1.GetGarageResponse, error) {
	g, err := s.garage.Get(ctx, req.GetGarageId())
	if err != nil {
		return nil, toGRPCError(err)
	}

	return &tenantv1.GetGarageResponse{Garage: garageToProto(g)}, nil
}

// UpdateGarage applies the partial updates in req to an existing garage
// and returns its updated API representation.
func (s *Server) UpdateGarage(ctx context.Context, req *tenantv1.UpdateGarageRequest) (*tenantv1.UpdateGarageResponse, error) {
	g, err := s.garage.Update(ctx, usecase.UpdateGarageRequest{
		GarageID: req.GetGarageId(),
		Name:     req.Name,
		TaxCode:  req.TaxCode,
		Address:  req.Address,
		LogoURL:  req.LogoUrl,
		Timezone: req.Timezone,
		Currency: req.Currency,
	})
	if err != nil {
		return nil, toGRPCError(err)
	}

	return &tenantv1.UpdateGarageResponse{Garage: garageToProto(g)}, nil
}

// ListGarages returns every garage owned by the caller's account.
func (s *Server) ListGarages(ctx context.Context, _ *tenantv1.ListGaragesRequest) (*tenantv1.ListGaragesResponse, error) {
	accountID, err := accountIDFromContext(ctx)
	if err != nil {
		return nil, err
	}

	garages, err := s.garage.List(ctx, accountID)
	if err != nil {
		return nil, toGRPCError(err)
	}

	resp := &tenantv1.ListGaragesResponse{}
	for _, g := range garages {
		resp.Garages = append(resp.Garages, garageToProto(g))
	}

	return resp, nil
}

func garageToProto(g *domain.Garage) *tenantv1.Garage {
	return &tenantv1.Garage{
		Id:        g.ID,
		AccountId: g.AccountID,
		Name:      g.Name,
		TaxCode:   g.TaxCode,
		Address:   g.Address,
		LogoUrl:   g.LogoURL,
		Timezone:  g.Timezone,
		Currency:  g.Currency,
		Status:    garageStatusToProto(g.Status),
		CreatedAt: timeToProto(g.CreatedAt),
		UpdatedAt: timeToProto(g.UpdatedAt),
	}
}

func garageStatusToProto(s domain.GarageStatus) tenantv1.GarageStatus {
	switch s {
	case domain.GarageStatusActive:
		return tenantv1.GarageStatus_GARAGE_STATUS_ACTIVE
	case domain.GarageStatusInactive:
		return tenantv1.GarageStatus_GARAGE_STATUS_INACTIVE
	case domain.GarageStatusSuspended:
		return tenantv1.GarageStatus_GARAGE_STATUS_SUSPENDED
	default:
		return tenantv1.GarageStatus_GARAGE_STATUS_UNSPECIFIED
	}
}
