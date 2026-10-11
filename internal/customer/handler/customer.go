package handler

import (
	"context"

	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"

	"github.com/viettechno/vietgara-backend/internal/customer/usecase"
	"github.com/viettechno/vietgara-backend/internal/platform/httpapi"
)

// CreateCustomer creates a customer profile (FR-CUS-01); answers 201.
func (s *Server) CreateCustomer(ctx context.Context, req *customerv1.CreateCustomerRequest) (*customerv1.CreateCustomerResponse, error) {
	return run(ctx, func(actorID string) (*customerv1.CreateCustomerResponse, error) {
		in := req.GetCustomer()

		c, err := s.customers.Create(ctx, usecase.CreateCustomerRequest{
			GarageID:            req.GetGarageId(),
			ActorID:             actorID,
			FullName:            in.GetFullName(),
			Phone:               in.Phone,
			Email:               in.Email,
			Address:             in.Address,
			PersonalDataConsent: in.GetPersonalDataConsent(),
		})
		if err != nil {
			return nil, err
		}

		httpapi.Created(ctx)

		return &customerv1.CreateCustomerResponse{Customer: customerToProto(c)}, nil
	})
}

// GetCustomer returns one customer.
func (s *Server) GetCustomer(ctx context.Context, req *customerv1.GetCustomerRequest) (*customerv1.GetCustomerResponse, error) {
	return run(ctx, func(string) (*customerv1.GetCustomerResponse, error) {
		c, err := s.customers.Get(ctx, req.GetGarageId(), req.GetCustomerId())
		if err != nil {
			return nil, err
		}

		return &customerv1.GetCustomerResponse{Customer: customerToProto(c)}, nil
	})
}

// UpdateCustomer changes the fields named by the update mask.
func (s *Server) UpdateCustomer(ctx context.Context, req *customerv1.UpdateCustomerRequest) (*customerv1.UpdateCustomerResponse, error) {
	return run(ctx, func(actorID string) (*customerv1.UpdateCustomerResponse, error) {
		in := req.GetCustomer()
		update := usecase.UpdateCustomerRequest{GarageID: req.GetGarageId(), CustomerID: req.GetCustomerId(), ActorID: actorID}

		for _, path := range maskPaths(req.GetUpdateMask().GetPaths(), "full_name", "phone", "email", "address", "personal_data_consent") {
			switch path {
			case "full_name":
				name := in.GetFullName()
				update.FullName = &name
			case "phone":
				update.Phone = stringField(in.Phone)
			case "email":
				update.Email = stringField(in.Email)
			case "address":
				update.Address = stringField(in.Address)
			case "personal_data_consent":
				consent := in.GetPersonalDataConsent()
				update.PersonalDataConsent = &consent
			default:
				return nil, badMask(path)
			}
		}

		c, err := s.customers.Update(ctx, update)
		if err != nil {
			return nil, err
		}

		return &customerv1.UpdateCustomerResponse{Customer: customerToProto(c)}, nil
	})
}

// ListCustomers searches the customers of a garage.
func (s *Server) ListCustomers(ctx context.Context, req *customerv1.ListCustomersRequest) (*customerv1.ListCustomersResponse, error) {
	return run(ctx, func(string) (*customerv1.ListCustomersResponse, error) {
		page, err := s.customers.List(ctx, usecase.ListCustomersRequest{
			PageRequest: pageRequest(req.GetPage(), req.GetPageSize()),
			GarageID:    req.GetGarageId(),
			Query:       req.GetQuery(),

			IncludeInactive: req.GetIncludeInactive(),
		})
		if err != nil {
			return nil, err
		}

		resp := &customerv1.ListCustomersResponse{Data: make([]*customerv1.Customer, 0, len(page.Items)), Pagination: paginationToProto(page)}
		for _, c := range page.Items {
			resp.Data = append(resp.Data, customerToProto(c))
		}

		return resp, nil
	})
}

// DeleteCustomer deletes or deactivates a customer (FR-CAT-07) and answers
// which; with dry_run it only says which.
//
//nolint:dupl // one RPC per resource, each with its own request and response types
func (s *Server) DeleteCustomer(ctx context.Context, req *customerv1.DeleteCustomerRequest) (*customerv1.DeleteCustomerResponse, error) {
	return run(ctx, func(actorID string) (*customerv1.DeleteCustomerResponse, error) {
		outcome, err := s.customers.Delete(ctx, usecase.DeleteCustomerRequest{
			GarageID: req.GetGarageId(), CustomerID: req.GetCustomerId(), ActorID: actorID, DryRun: req.GetDryRun(),
		})
		if err != nil {
			return nil, err
		}

		return &customerv1.DeleteCustomerResponse{Outcome: httpapi.DeleteOutcome(outcome)}, nil
	})
}

// ActivateCustomer reactivates a deactivated customer.
func (s *Server) ActivateCustomer(ctx context.Context, req *customerv1.ActivateCustomerRequest) (*customerv1.ActivateCustomerResponse, error) {
	return run(ctx, func(actorID string) (*customerv1.ActivateCustomerResponse, error) {
		c, err := s.customers.Activate(ctx, req.GetGarageId(), req.GetCustomerId(), actorID)
		if err != nil {
			return nil, err
		}

		return &customerv1.ActivateCustomerResponse{Customer: customerToProto(c)}, nil
	})
}
