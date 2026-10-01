package handler

import (
	"context"
	"slices"

	inventoryv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/inventory/v1"

	"github.com/viettechno/vietgara-backend/internal/inventory/usecase"
	"github.com/viettechno/vietgara-backend/internal/platform/httpapi"
)

// ListItems lists a garage's items with their current quantity on hand
// (FR-INV-03).
func (s *Server) ListItems(ctx context.Context, req *inventoryv1.ListItemsRequest) (*inventoryv1.ListItemsResponse, error) {
	return run(ctx, func(string) (*inventoryv1.ListItemsResponse, error) {
		page, err := s.uc.ListItems(ctx, usecase.ListItemsRequest{
			PageRequest: pageRequest(req.GetPage(), req.GetPageSize()), GarageID: req.GetGarageId(), Query: req.GetQuery(),
		})
		if err != nil {
			return nil, err
		}

		resp := &inventoryv1.ListItemsResponse{Data: make([]*inventoryv1.Item, 0, len(page.Items)), Pagination: itemPaginationToProto(page)}
		for _, item := range page.Items {
			resp.Data = append(resp.Data, itemToProto(item))
		}

		return resp, nil
	})
}

var itemSettable = []string{"sku", "name", "unit", "reorder_threshold"}

// CreateItem adds a part code to the garage's catalog; answers 201.
func (s *Server) CreateItem(ctx context.Context, req *inventoryv1.CreateItemRequest) (*inventoryv1.CreateItemResponse, error) {
	return run(ctx, func(actorID string) (*inventoryv1.CreateItemResponse, error) {
		item, err := s.uc.CreateItem(ctx, usecase.CreateItemRequest{
			GarageID: req.GetGarageId(), ActorID: actorID, Fields: itemFields(req.GetItem(), itemSettable),
		})
		if err != nil {
			return nil, err
		}

		httpapi.Created(ctx)

		return &inventoryv1.CreateItemResponse{Item: itemToProto(item)}, nil
	})
}

// GetItem returns one item.
func (s *Server) GetItem(ctx context.Context, req *inventoryv1.GetItemRequest) (*inventoryv1.GetItemResponse, error) {
	return run(ctx, func(string) (*inventoryv1.GetItemResponse, error) {
		item, err := s.uc.GetItem(ctx, req.GetGarageId(), req.GetItemId())
		if err != nil {
			return nil, err
		}

		return &inventoryv1.GetItemResponse{Item: itemToProto(item)}, nil
	})
}

// UpdateItem changes the fields named by the update mask.
func (s *Server) UpdateItem(ctx context.Context, req *inventoryv1.UpdateItemRequest) (*inventoryv1.UpdateItemResponse, error) {
	return run(ctx, func(actorID string) (*inventoryv1.UpdateItemResponse, error) {
		paths := maskPaths(req.GetUpdateMask().GetPaths(), itemSettable...)
		for _, path := range paths {
			if !slices.Contains(itemSettable, path) {
				return nil, badMask(path)
			}
		}

		item, err := s.uc.UpdateItem(ctx, usecase.UpdateItemRequest{
			GarageID: req.GetGarageId(), ItemID: req.GetItemId(), ActorID: actorID, Fields: itemFields(req.GetItem(), paths),
		})
		if err != nil {
			return nil, err
		}

		return &inventoryv1.UpdateItemResponse{Item: itemToProto(item)}, nil
	})
}

// DeleteItem soft-deletes an item.
func (s *Server) DeleteItem(ctx context.Context, req *inventoryv1.DeleteItemRequest) (*inventoryv1.DeleteItemResponse, error) {
	return run(ctx, func(actorID string) (*inventoryv1.DeleteItemResponse, error) {
		if err := s.uc.DeleteItem(ctx, req.GetGarageId(), req.GetItemId(), actorID); err != nil {
			return nil, err
		}

		return &inventoryv1.DeleteItemResponse{}, nil
	})
}
