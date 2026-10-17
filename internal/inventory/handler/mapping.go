package handler

import (
	"context"
	"math"

	commonv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/common/v1"
	inventoryv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/inventory/v1"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
	"github.com/viettechno/vietgara-backend/internal/inventory/usecase"
	"github.com/viettechno/vietgara-backend/internal/platform/access"
	"github.com/viettechno/vietgara-backend/pkg/converter"
)

func stockLevelToProto(ctx context.Context, l *domain.StockLevel) *inventoryv1.StockLevel {
	out := &inventoryv1.StockLevel{
		PartId: l.PartID, Sku: l.SKU, Name: l.Name, BaseUnitName: l.BaseUnitName, BaseUnitAbbreviation: l.BaseUnitAbbreviation,
		CategoryName: l.CategoryName, Brand: l.Brand, IsActive: l.IsActive, ThumbnailUrl: l.ThumbnailURL,
		QuantityOnHand: l.QuantityOnHand, ReorderThreshold: l.ReorderThreshold, LowStock: l.LowStock,
		QuantityReserved: l.QuantityReserved, QuantityAvailable: l.QuantityAvailable,
	}

	if canSeeCost(ctx) {
		out.AverageCost, out.StockValue = wholeDongRef(l.AverageCost), wholeDongRef(l.StockValue)
	}

	return out
}

// canSeeCost reports whether the caller holds inventory.cost.read (FR-INV-08);
// responses omit every cost field without it.
func canSeeCost(ctx context.Context) bool {
	a, ok := access.FromContext(ctx)

	return ok && a.Has(access.InventoryCostRead)
}

func transactionToProto(ctx context.Context, t *domain.StockTransaction) *inventoryv1.StockTransaction {
	out := &inventoryv1.StockTransaction{
		Id: t.ID, PartId: t.PartID, PartSku: t.PartSKU, PartName: t.PartName, Type: transactionTypeToProto(t.Type),
		Quantity: t.Quantity, RefType: refTypeToProto(t.RefType), CreatedAt: converter.TimeToProto(t.CreatedAt),
		UnitId: t.UnitID, UnitAbbreviation: t.UnitAbbreviation, BaseUnitAbbreviation: t.BaseUnitAbbreviation,
	}

	if t.EnteredQuantity != nil {
		out.EnteredQuantity = *t.EnteredQuantity
	}

	if canSeeCost(ctx) {
		out.UnitCost = wholeDongRef(t.UnitCost)
	}

	if t.RefID != nil {
		out.RefId = *t.RefID
	}

	return out
}

// wholeDongRef rounds a cost with up to four decimals to whole dong; nil when
// it is absent or cannot be parsed.
func wholeDongRef(cost *string) *int64 {
	if cost == nil {
		return nil
	}

	n, ok := domain.WholeDong(*cost)
	if !ok {
		return nil
	}

	return &n
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
	switch t {
	case domain.RefRepairOrder:
		return inventoryv1.StockTransactionRefType_STOCK_TRANSACTION_REF_TYPE_REPAIR_ORDER
	case domain.RefGoodsReceipt:
		return inventoryv1.StockTransactionRefType_STOCK_TRANSACTION_REF_TYPE_GOODS_RECEIPT
	case domain.RefStockIssue:
		return inventoryv1.StockTransactionRefType_STOCK_TRANSACTION_REF_TYPE_STOCK_ISSUE
	case domain.RefPurchase:
		return inventoryv1.StockTransactionRefType_STOCK_TRANSACTION_REF_TYPE_PURCHASE
	default:
		return inventoryv1.StockTransactionRefType_STOCK_TRANSACTION_REF_TYPE_UNSPECIFIED
	}
}

func paginationToProto[T any](p *usecase.Page[T]) *commonv1.Pagination {
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
