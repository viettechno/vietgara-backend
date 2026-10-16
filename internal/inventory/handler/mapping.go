package handler

import (
	"math"
	"math/big"

	commonv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/common/v1"
	inventoryv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/inventory/v1"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
	"github.com/viettechno/vietgara-backend/internal/inventory/usecase"
	"github.com/viettechno/vietgara-backend/pkg/converter"
)

func stockLevelToProto(l *domain.StockLevel) *inventoryv1.StockLevel {
	return &inventoryv1.StockLevel{
		PartId: l.PartID, Sku: l.SKU, Name: l.Name, BaseUnitName: l.BaseUnitName, BaseUnitAbbreviation: l.BaseUnitAbbreviation,
		CategoryName: l.CategoryName, Brand: l.Brand, IsActive: l.IsActive, ThumbnailUrl: l.ThumbnailURL,
		QuantityOnHand: l.QuantityOnHand, ReorderThreshold: l.ReorderThreshold, LowStock: l.LowStock,
	}
}

func transactionToProto(t *domain.StockTransaction) *inventoryv1.StockTransaction {
	out := &inventoryv1.StockTransaction{
		Id: t.ID, ItemId: t.PartID, ItemSku: t.PartSKU, ItemName: t.PartName, Type: transactionTypeToProto(t.Type),
		Quantity: t.Quantity, RefType: refTypeToProto(t.RefType), CreatedAt: converter.TimeToProto(t.CreatedAt),
		UnitId: t.UnitID, UnitAbbreviation: t.UnitAbbreviation, BaseUnitAbbreviation: t.BaseUnitAbbreviation,
	}

	if t.EnteredQuantity != nil {
		out.EnteredQuantity = *t.EnteredQuantity
	}

	if t.UnitCost != nil {
		out.UnitCost = wholeDong(*t.UnitCost)
	}

	if t.RefID != nil {
		out.RefId = *t.RefID
	}

	return out
}

// wholeDong rounds a cost with up to four decimals to whole dong; nil when
// it cannot be parsed.
func wholeDong(cost string) *int64 {
	r, ok := new(big.Rat).SetString(cost)
	if !ok {
		return nil
	}

	r.Add(r, big.NewRat(1, 2))
	n := new(big.Int).Quo(r.Num(), r.Denom()).Int64()

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
	if t == domain.RefRepairOrder {
		return inventoryv1.StockTransactionRefType_STOCK_TRANSACTION_REF_TYPE_REPAIR_ORDER
	}

	return inventoryv1.StockTransactionRefType_STOCK_TRANSACTION_REF_TYPE_PURCHASE
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
