package handler

import (
	"context"

	inventoryv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/inventory/v1"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
)

// GetPartSuppliers returns the usual suppliers of a part (FR-INV-19).
func (s *Server) GetPartSuppliers(ctx context.Context, req *inventoryv1.GetPartSuppliersRequest) (*inventoryv1.GetPartSuppliersResponse, error) {
	return run(ctx, func(string) (*inventoryv1.GetPartSuppliersResponse, error) {
		list, err := s.uc.GetPartSuppliers(ctx, req.GetGarageId(), req.GetPartId())
		if err != nil {
			return nil, err
		}

		return &inventoryv1.GetPartSuppliersResponse{Data: partSuppliersToProto(ctx, list)}, nil
	})
}

// PutPartSuppliers replaces the part's list of usual suppliers.
func (s *Server) PutPartSuppliers(ctx context.Context, req *inventoryv1.PutPartSuppliersRequest) (*inventoryv1.PutPartSuppliersResponse, error) {
	return run(ctx, func(string) (*inventoryv1.PutPartSuppliersResponse, error) {
		in := make([]domain.PartSupplier, 0, len(req.GetData()))
		for _, p := range req.GetData() {
			in = append(in, domain.PartSupplier{SupplierID: p.GetSupplierId(), SupplierSKU: p.GetSupplierSku(), IsDefault: p.GetIsDefault()})
		}

		list, err := s.uc.PutPartSuppliers(ctx, req.GetGarageId(), req.GetPartId(), in)
		if err != nil {
			return nil, err
		}

		return &inventoryv1.PutPartSuppliersResponse{Data: partSuppliersToProto(ctx, list)}, nil
	})
}

func partSuppliersToProto(ctx context.Context, list []domain.PartSupplier) []*inventoryv1.PartSupplier {
	seeCost := canSeeCost(ctx)
	out := make([]*inventoryv1.PartSupplier, 0, len(list))

	for _, s := range list {
		p := &inventoryv1.PartSupplier{
			SupplierId: s.SupplierID, SupplierName: s.SupplierName, SupplierSku: s.SupplierSKU, IsDefault: s.IsDefault,
		}

		if seeCost {
			p.LastUnitCost = wholeDongRef(s.LastUnitCost)
		}

		out = append(out, p)
	}

	return out
}
