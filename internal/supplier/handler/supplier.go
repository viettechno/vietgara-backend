package handler

import (
	"context"
	"strings"

	supplierv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/supplier/v1"

	"github.com/viettechno/vietgara-backend/internal/platform/httpapi"
	"github.com/viettechno/vietgara-backend/internal/supplier/usecase"
)

// CreateSupplier adds a supplier (FR-SUP-01); answers 201.
func (s *Server) CreateSupplier(ctx context.Context, req *supplierv1.CreateSupplierRequest) (*supplierv1.CreateSupplierResponse, error) {
	return run(ctx, func(actorID string) (*supplierv1.CreateSupplierResponse, error) {
		in := req.GetSupplier()

		created, err := s.suppliers.Create(ctx, usecase.CreateRequest{
			GarageID: req.GetGarageId(),
			ActorID:  actorID,
			Name:     in.GetName(),
			TaxCode:  in.TaxCode,
			Contact:  contactFromProto(in.GetContact()),
			Bank:     bankFromProto(in.GetBank()),
			Note:     in.Note,
		})
		if err != nil {
			return nil, err
		}

		httpapi.Created(ctx)

		return &supplierv1.CreateSupplierResponse{Supplier: supplierToProto(created)}, nil
	})
}

// GetSupplier returns one supplier.
func (s *Server) GetSupplier(ctx context.Context, req *supplierv1.GetSupplierRequest) (*supplierv1.GetSupplierResponse, error) {
	return run(ctx, func(string) (*supplierv1.GetSupplierResponse, error) {
		got, err := s.suppliers.Get(ctx, req.GetGarageId(), req.GetSupplierId())
		if err != nil {
			return nil, err
		}

		return &supplierv1.GetSupplierResponse{Supplier: supplierToProto(got)}, nil
	})
}

// UpdateSupplier changes the fields named by the update mask; "contact" and
// "bank" replace the whole block.
func (s *Server) UpdateSupplier(ctx context.Context, req *supplierv1.UpdateSupplierRequest) (*supplierv1.UpdateSupplierResponse, error) {
	return run(ctx, func(actorID string) (*supplierv1.UpdateSupplierResponse, error) {
		in := req.GetSupplier()
		update := usecase.UpdateRequest{GarageID: req.GetGarageId(), SupplierID: req.GetSupplierId(), ActorID: actorID}

		paths := req.GetUpdateMask().GetPaths()
		if len(paths) == 0 {
			paths = []string{"name", "tax_code", "contact", "bank", "note"}
		}

		for _, path := range wholeBlockPaths(paths, "contact", "bank") {
			switch path {
			case "name":
				name := in.GetName()
				update.Name = &name
			case "tax_code":
				update.TaxCode = stringField(in.TaxCode)
			case "contact":
				contact := contactFromProto(in.GetContact())
				update.Contact = &contact
			case "bank":
				bank := bankFromProto(in.GetBank())
				update.Bank = &bank
			case "note":
				update.Note = stringField(in.Note)
			default:
				return nil, badMask(path)
			}
		}

		updated, err := s.suppliers.Update(ctx, update)
		if err != nil {
			return nil, err
		}

		return &supplierv1.UpdateSupplierResponse{Supplier: supplierToProto(updated)}, nil
	})
}

// wholeBlockPaths folds "<block>.*" sub-paths into "<block>" (each block is
// replaced as a whole).
func wholeBlockPaths(paths []string, blocks ...string) []string {
	out := make([]string, 0, len(paths))
	seen := map[string]bool{}

	for _, p := range paths {
		folded := p

		for _, block := range blocks {
			if strings.HasPrefix(p, block+".") {
				folded = block
			}
		}

		if seen[folded] {
			continue
		}

		seen[folded] = true
		out = append(out, folded)
	}

	return out
}

// ListSuppliers lists a garage's suppliers.
func (s *Server) ListSuppliers(ctx context.Context, req *supplierv1.ListSuppliersRequest) (*supplierv1.ListSuppliersResponse, error) {
	return run(ctx, func(string) (*supplierv1.ListSuppliersResponse, error) {
		page, err := s.suppliers.List(ctx, usecase.ListRequest{
			PageRequest: pageRequest(req.GetPage(), req.GetPageSize()),
			GarageID:    req.GetGarageId(),
			Query:       req.GetQuery(),

			IncludeInactive: req.GetIncludeInactive(),
		})
		if err != nil {
			return nil, err
		}

		resp := &supplierv1.ListSuppliersResponse{
			Data: make([]*supplierv1.Supplier, 0, len(page.Items)), Pagination: paginationToProto(page),
		}
		for _, item := range page.Items {
			resp.Data = append(resp.Data, supplierToProto(item))
		}

		return resp, nil
	})
}

// DeleteSupplier deletes or deactivates a supplier (FR-CAT-07) and answers
// which; with dry_run it only says which.
func (s *Server) DeleteSupplier(ctx context.Context, req *supplierv1.DeleteSupplierRequest) (*supplierv1.DeleteSupplierResponse, error) {
	return run(ctx, func(actorID string) (*supplierv1.DeleteSupplierResponse, error) {
		outcome, err := s.suppliers.Delete(ctx, usecase.DeleteRequest{
			GarageID: req.GetGarageId(), SupplierID: req.GetSupplierId(), ActorID: actorID, DryRun: req.GetDryRun(),
		})
		if err != nil {
			return nil, err
		}

		return &supplierv1.DeleteSupplierResponse{Outcome: httpapi.DeleteOutcome(outcome)}, nil
	})
}

// ActivateSupplier reactivates a deactivated supplier.
func (s *Server) ActivateSupplier(ctx context.Context, req *supplierv1.ActivateSupplierRequest) (*supplierv1.ActivateSupplierResponse, error) {
	return run(ctx, func(actorID string) (*supplierv1.ActivateSupplierResponse, error) {
		activated, err := s.suppliers.Activate(ctx, req.GetGarageId(), req.GetSupplierId(), actorID)
		if err != nil {
			return nil, err
		}

		return &supplierv1.ActivateSupplierResponse{Supplier: supplierToProto(activated)}, nil
	})
}
