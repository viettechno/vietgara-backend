package handler

import (
	"context"

	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"

	"github.com/viettechno/vietgara-backend/internal/customer/usecase"
	"github.com/viettechno/vietgara-backend/internal/platform/httpapi"
)

// CreateVehicle registers a vehicle for a customer (FR-CUS-02); answers 201.
func (s *Server) CreateVehicle(ctx context.Context, req *customerv1.CreateVehicleRequest) (*customerv1.CreateVehicleResponse, error) {
	return run(ctx, func(actorID string) (*customerv1.CreateVehicleResponse, error) {
		in := req.GetVehicle()

		v, err := s.vehicles.Create(ctx, usecase.CreateVehicleRequest{
			GarageID:     req.GetGarageId(),
			ActorID:      actorID,
			CustomerID:   in.GetCustomerId(),
			LicensePlate: in.GetLicensePlate(),
			Make:         in.Make,
			Model:        in.Model,
			Year:         intPtr(in.Year),
			VIN:          in.Vin,
			EngineNumber: in.EngineNumber,
		})
		if err != nil {
			return nil, err
		}

		httpapi.Created(ctx)

		return &customerv1.CreateVehicleResponse{Vehicle: vehicleToProto(v)}, nil
	})
}

// GetVehicle returns one vehicle.
func (s *Server) GetVehicle(ctx context.Context, req *customerv1.GetVehicleRequest) (*customerv1.GetVehicleResponse, error) {
	return run(ctx, func(string) (*customerv1.GetVehicleResponse, error) {
		v, err := s.vehicles.Get(ctx, req.GetGarageId(), req.GetVehicleId())
		if err != nil {
			return nil, err
		}

		return &customerv1.GetVehicleResponse{Vehicle: vehicleToProto(v)}, nil
	})
}

// UpdateVehicle changes the fields named by the update mask.
func (s *Server) UpdateVehicle(ctx context.Context, req *customerv1.UpdateVehicleRequest) (*customerv1.UpdateVehicleResponse, error) {
	return run(ctx, func(actorID string) (*customerv1.UpdateVehicleResponse, error) {
		in := req.GetVehicle()
		update := usecase.UpdateVehicleRequest{GarageID: req.GetGarageId(), VehicleID: req.GetVehicleId(), ActorID: actorID}

		settable := []string{"customer_id", "license_plate", "make", "model", "year", "vin", "engine_number"}
		for _, path := range maskPaths(req.GetUpdateMask().GetPaths(), settable...) {
			switch path {
			case "customer_id":
				customerID := in.GetCustomerId()
				update.CustomerID = &customerID
			case "license_plate":
				plate := in.GetLicensePlate()
				update.LicensePlate = &plate
			case "make":
				update.Make = stringField(in.Make)
			case "model":
				update.Model = stringField(in.Model)
			case "year":
				year := int(in.GetYear())
				update.Year = &year
			case "vin":
				update.VIN = stringField(in.Vin)
			case "engine_number":
				update.EngineNumber = stringField(in.EngineNumber)
			default:
				return nil, badMask(path)
			}
		}

		v, err := s.vehicles.Update(ctx, update)
		if err != nil {
			return nil, err
		}

		return &customerv1.UpdateVehicleResponse{Vehicle: vehicleToProto(v)}, nil
	})
}

// ListVehicles lists a garage's (or one customer's) vehicles.
func (s *Server) ListVehicles(ctx context.Context, req *customerv1.ListVehiclesRequest) (*customerv1.ListVehiclesResponse, error) {
	return run(ctx, func(string) (*customerv1.ListVehiclesResponse, error) {
		page, err := s.vehicles.List(ctx, usecase.ListVehiclesRequest{
			PageRequest: pageRequest(req.GetPage(), req.GetPageSize()),
			GarageID:    req.GetGarageId(),
			CustomerID:  req.GetCustomerId(),
			Query:       req.GetQuery(),

			IncludeInactive: req.GetIncludeInactive(),
		})
		if err != nil {
			return nil, err
		}

		resp := &customerv1.ListVehiclesResponse{Data: make([]*customerv1.Vehicle, 0, len(page.Items)), Pagination: paginationToProto(page)}
		for _, v := range page.Items {
			resp.Data = append(resp.Data, vehicleToProto(v))
		}

		return resp, nil
	})
}

// DeleteVehicle deletes or deactivates a vehicle (FR-CAT-07) and answers
// which; with dry_run it only says which.
//
//nolint:dupl // one RPC per resource, each with its own request and response types
func (s *Server) DeleteVehicle(ctx context.Context, req *customerv1.DeleteVehicleRequest) (*customerv1.DeleteVehicleResponse, error) {
	return run(ctx, func(actorID string) (*customerv1.DeleteVehicleResponse, error) {
		outcome, err := s.vehicles.Delete(ctx, usecase.DeleteVehicleRequest{
			GarageID: req.GetGarageId(), VehicleID: req.GetVehicleId(), ActorID: actorID, DryRun: req.GetDryRun(),
		})
		if err != nil {
			return nil, err
		}

		return &customerv1.DeleteVehicleResponse{Outcome: httpapi.DeleteOutcome(outcome)}, nil
	})
}

// ActivateVehicle reactivates a deactivated vehicle.
func (s *Server) ActivateVehicle(ctx context.Context, req *customerv1.ActivateVehicleRequest) (*customerv1.ActivateVehicleResponse, error) {
	return run(ctx, func(actorID string) (*customerv1.ActivateVehicleResponse, error) {
		v, err := s.vehicles.Activate(ctx, req.GetGarageId(), req.GetVehicleId(), actorID)
		if err != nil {
			return nil, err
		}

		return &customerv1.ActivateVehicleResponse{Vehicle: vehicleToProto(v)}, nil
	})
}

// GetVehicleHistory returns a vehicle's quotes, repair orders and
// settlements, newest first (FR-CUS-03).
func (s *Server) GetVehicleHistory(ctx context.Context, req *customerv1.GetVehicleHistoryRequest) (*customerv1.GetVehicleHistoryResponse, error) {
	return run(ctx, func(string) (*customerv1.GetVehicleHistoryResponse, error) {
		entries, err := s.vehicles.GetHistory(ctx, req.GetGarageId(), req.GetVehicleId())
		if err != nil {
			return nil, err
		}

		resp := &customerv1.GetVehicleHistoryResponse{Data: make([]*customerv1.VehicleHistoryEntry, 0, len(entries))}
		for _, e := range entries {
			resp.Data = append(resp.Data, historyEntryToProto(e))
		}

		return resp, nil
	})
}
