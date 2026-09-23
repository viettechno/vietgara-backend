package handler

import (
	"context"

	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"

	"github.com/viettechno/vietgara-backend/internal/customer/usecase"
)

// CreateCustomer creates a customer profile (FR-CUS-01).
func (s *Server) CreateCustomer(ctx context.Context, req *customerv1.CreateCustomerRequest) (*customerv1.CreateCustomerResponse, error) {
	return run(ctx, s, req.GetGarageId(), permManageCustomers, func(actorID string) (*customerv1.CreateCustomerResponse, error) {
		c, err := s.customers.Create(ctx, usecase.CreateCustomerRequest{
			GarageID:            req.GetGarageId(),
			ActorID:             actorID,
			FullName:            req.GetFullName(),
			Phone:               req.Phone,
			Email:               req.Email,
			Address:             req.Address,
			PersonalDataConsent: req.GetPersonalDataConsent(),
		})
		if err != nil {
			return nil, err
		}

		return &customerv1.CreateCustomerResponse{Customer: customerToProto(c)}, nil
	})
}

// GetCustomer returns one customer.
func (s *Server) GetCustomer(ctx context.Context, req *customerv1.GetCustomerRequest) (*customerv1.GetCustomerResponse, error) {
	return run(ctx, s, req.GetGarageId(), permRead, func(string) (*customerv1.GetCustomerResponse, error) {
		c, err := s.customers.Get(ctx, req.GetGarageId(), req.GetCustomerId())
		if err != nil {
			return nil, err
		}

		return &customerv1.GetCustomerResponse{Customer: customerToProto(c)}, nil
	})
}

// UpdateCustomer applies partial updates to a customer.
func (s *Server) UpdateCustomer(ctx context.Context, req *customerv1.UpdateCustomerRequest) (*customerv1.UpdateCustomerResponse, error) {
	return run(ctx, s, req.GetGarageId(), permManageCustomers, func(actorID string) (*customerv1.UpdateCustomerResponse, error) {
		c, err := s.customers.Update(ctx, usecase.UpdateCustomerRequest{
			GarageID:            req.GetGarageId(),
			CustomerID:          req.GetCustomerId(),
			ActorID:             actorID,
			FullName:            req.FullName,
			Phone:               req.Phone,
			Email:               req.Email,
			Address:             req.Address,
			PersonalDataConsent: req.PersonalDataConsent,
		})
		if err != nil {
			return nil, err
		}

		return &customerv1.UpdateCustomerResponse{Customer: customerToProto(c)}, nil
	})
}

// ListCustomers searches the customers of a garage.
func (s *Server) ListCustomers(ctx context.Context, req *customerv1.ListCustomersRequest) (*customerv1.ListCustomersResponse, error) {
	return run(ctx, s, req.GetGarageId(), permRead, func(string) (*customerv1.ListCustomersResponse, error) {
		page, err := s.customers.List(ctx, usecase.ListCustomersRequest{
			PageRequest: pageRequest(req.GetPage(), req.GetPageSize()),
			GarageID:    req.GetGarageId(),
			Query:       req.GetQuery(),
		})
		if err != nil {
			return nil, err
		}

		resp := &customerv1.ListCustomersResponse{Pagination: paginationToProto(page)}
		for _, c := range page.Items {
			resp.Customers = append(resp.Customers, customerToProto(c))
		}

		return resp, nil
	})
}

// DeleteCustomer soft-deletes a customer and their vehicles.
func (s *Server) DeleteCustomer(ctx context.Context, req *customerv1.DeleteCustomerRequest) (*customerv1.DeleteCustomerResponse, error) {
	return run(ctx, s, req.GetGarageId(), permManageCustomers, func(actorID string) (*customerv1.DeleteCustomerResponse, error) {
		if err := s.customers.Delete(ctx, req.GetGarageId(), req.GetCustomerId(), actorID); err != nil {
			return nil, err
		}

		return &customerv1.DeleteCustomerResponse{}, nil
	})
}
