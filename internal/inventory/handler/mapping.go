package handler

import (
	"math"

	commonv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/common/v1"
	inventoryv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/inventory/v1"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
	"github.com/viettechno/vietgara-backend/internal/inventory/usecase"
	"github.com/viettechno/vietgara-backend/pkg/converter"
)

func itemToProto(item *domain.Item) *inventoryv1.Item {
	return &inventoryv1.Item{
		Id: item.ID, GarageId: item.GarageID, Sku: item.SKU, Name: item.Name, Unit: item.Unit,
		ReorderThreshold: toInt32(item.ReorderThreshold), QuantityOnHand: item.QuantityOnHand,
		CreatedAt: converter.TimeToProto(item.CreatedAt), UpdatedAt: converter.TimeToProto(item.UpdatedAt),
	}
}

func transactionToProto(t *domain.StockTransaction) *inventoryv1.StockTransaction {
	out := &inventoryv1.StockTransaction{
		Id: t.ID, ItemId: t.ItemID, ItemSku: t.ItemSKU, ItemName: t.ItemName, Type: transactionTypeToProto(t.Type),
		Quantity: t.Quantity, RefType: refTypeToProto(t.RefType), CreatedAt: converter.TimeToProto(t.CreatedAt),
	}

	if t.UnitCost != nil {
		out.UnitCost = t.UnitCost
	}

	if t.RefID != nil {
		out.RefId = *t.RefID
	}

	return out
}

func transactionTypeToProto(t domain.TransactionType) inventoryv1.StockTransactionType {
	if t == domain.TransactionOut {
		return inventoryv1.StockTransactionType_STOCK_TRANSACTION_TYPE_OUT
	}

	return inventoryv1.StockTransactionType_STOCK_TRANSACTION_TYPE_IN
}

// transactionTypeFromProto maps UNSPECIFIED (and unknown values) to "",
// which the use case treats as "every type" when listing.
func transactionTypeFromProto(t inventoryv1.StockTransactionType) domain.TransactionType {
	switch t {
	case inventoryv1.StockTransactionType_STOCK_TRANSACTION_TYPE_IN:
		return domain.TransactionIn
	case inventoryv1.StockTransactionType_STOCK_TRANSACTION_TYPE_OUT:
		return domain.TransactionOut
	case inventoryv1.StockTransactionType_STOCK_TRANSACTION_TYPE_UNSPECIFIED:
		return ""
	default:
		return ""
	}
}

func refTypeToProto(t domain.RefType) inventoryv1.StockTransactionRefType {
	if t == domain.RefRepairOrder {
		return inventoryv1.StockTransactionRefType_STOCK_TRANSACTION_REF_TYPE_REPAIR_ORDER
	}

	return inventoryv1.StockTransactionRefType_STOCK_TRANSACTION_REF_TYPE_PURCHASE
}

func itemFields(in *inventoryv1.Item, paths []string) usecase.ItemFields {
	var f usecase.ItemFields

	for _, path := range paths {
		switch path {
		case "sku":
			f.SKU = new(in.GetSku())
		case "name":
			f.Name = new(in.GetName())
		case "unit":
			f.Unit = new(in.GetUnit())
		case "reorder_threshold":
			threshold := int(in.GetReorderThreshold())
			f.ReorderThreshold = &threshold
		}
	}

	return f
}

func itemPaginationToProto(p *usecase.Page[*domain.Item]) *commonv1.Pagination {
	return &commonv1.Pagination{Total: toInt32(p.Total), Page: toInt32(p.Page), PageSize: toInt32(p.PageSize)}
}

func transactionPaginationToProto(p *usecase.Page[*domain.StockTransaction]) *commonv1.Pagination {
	return &commonv1.Pagination{Total: toInt32(p.Total), Page: toInt32(p.Page), PageSize: toInt32(p.PageSize)}
}

func pageRequest(page, pageSize int32) usecase.PageRequest {
	return usecase.PageRequest{Page: int(page), PageSize: int(pageSize)}
}

func toInt32(n int) int32 {
	if n > math.MaxInt32 {
		return math.MaxInt32
	}

	if n < math.MinInt32 {
		return math.MinInt32
	}

	return int32(n)
}
