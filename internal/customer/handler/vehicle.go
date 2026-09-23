package handler

import (
	"context"

	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"

	"github.com/viettechno/vietgara-backend/internal/customer/usecase"
)

// CreateVehicle registers a vehicle to a customer (FR-CUS-02).
func (s *Server) CreateVehicle(ctx context.Context, req *customerv1.CreateVehicleRequest) (*customerv1.CreateVehicleResponse, error) {
	return run(ctx, s, req.GetGarageId(), permManageCustomers, func(actorID string) (*customerv1.CreateVehicleResponse, error) {
		v, err := s.vehicles.Create(ctx, usecase.CreateVehicleRequest{
			GarageID:     req.GetGarageId(),
			ActorID:      actorID,
			CustomerID:   req.GetCustomerId(),
			LicensePlate: req.GetLicensePlate(),
			Make:         req.Make,
			Model:        req.Model,
			Year:         intPtr(req.Year),
			VIN:          req.Vin,
			EngineNumber: req.EngineNumber,
		})
		if err != nil {
			return nil, err
		}

		return &customerv1.CreateVehicleResponse{Vehicle: vehicleToProto(v)}, nil
	})
}

// GetVehicle returns one vehicle.
func (s *Server) GetVehicle(ctx context.Context, req *customerv1.GetVehicleRequest) (*customerv1.GetVehicleResponse, error) {
	return run(ctx, s, req.GetGarageId(), permRead, func(string) (*customerv1.GetVehicleResponse, error) {
		v, err := s.vehicles.Get(ctx, req.GetGarageId(), req.GetVehicleId())
		if err != nil {
			return nil, err
		}

		return &customerv1.GetVehicleResponse{Vehicle: vehicleToProto(v)}, nil
	})
}

// UpdateVehicle applies partial updates to a vehicle.
func (s *Server) UpdateVehicle(ctx context.Context, req *customerv1.UpdateVehicleRequest) (*customerv1.UpdateVehicleResponse, error) {
	return run(ctx, s, req.GetGarageId(), permManageCustomers, func(actorID string) (*customerv1.UpdateVehicleResponse, error) {
		v, err := s.vehicles.Update(ctx, usecase.UpdateVehicleRequest{
			GarageID:     req.GetGarageId(),
			VehicleID:    req.GetVehicleId(),
			ActorID:      actorID,
			CustomerID:   req.CustomerId,
			LicensePlate: req.LicensePlate,
			Make:         req.Make,
			Model:        req.Model,
			Year:         intPtr(req.Year),
			VIN:          req.Vin,
			EngineNumber: req.EngineNumber,
		})
		if err != nil {
			return nil, err
		}

		return &customerv1.UpdateVehicleResponse{Vehicle: vehicleToProto(v)}, nil
	})
}

// ListVehicles searches the vehicles of a garage, optionally of one customer.
func (s *Server) ListVehicles(ctx context.Context, req *customerv1.ListVehiclesRequest) (*customerv1.ListVehiclesResponse, error) {
	return run(ctx, s, req.GetGarageId(), permRead, func(string) (*customerv1.ListVehiclesResponse, error) {
		page, err := s.vehicles.List(ctx, usecase.ListVehiclesRequest{
			PageRequest: pageRequest(req.GetPage(), req.GetPageSize()),
			GarageID:    req.GetGarageId(),
			CustomerID:  req.GetCustomerId(),
			Query:       req.GetQuery(),
		})
		if err != nil {
			return nil, err
		}

		resp := &customerv1.ListVehiclesResponse{Pagination: paginationToProto(page)}
		for _, v := range page.Items {
			resp.Vehicles = append(resp.Vehicles, vehicleToProto(v))
		}

		return resp, nil
	})
}

// DeleteVehicle soft-deletes a vehicle.
func (s *Server) DeleteVehicle(ctx context.Context, req *customerv1.DeleteVehicleRequest) (*customerv1.DeleteVehicleResponse, error) {
	return run(ctx, s, req.GetGarageId(), permManageCustomers, func(actorID string) (*customerv1.DeleteVehicleResponse, error) {
		if err := s.vehicles.Delete(ctx, req.GetGarageId(), req.GetVehicleId(), actorID); err != nil {
			return nil, err
		}

		return &customerv1.DeleteVehicleResponse{}, nil
	})
}
