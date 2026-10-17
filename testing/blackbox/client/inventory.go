package client

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	inventoryv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/inventory/v1"
	supplierv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/supplier/v1"
)

// ---- Goods receipts, stock issues and cost (Release 7.0) -------------------

func goodsReceiptPath(garageID, receiptID string) string {
	return garagePath(garageID) + "/goods-receipts/" + url.PathEscape(receiptID)
}

// CreateGoodsReceipt saves a draft goods receipt.
func (c *Client) CreateGoodsReceipt(ctx context.Context, garageID string, receipt *inventoryv1.GoodsReceipt) (*inventoryv1.GoodsReceipt, error) {
	return call[inventoryv1.GoodsReceipt](ctx, c, http.MethodPost, garagePath(garageID)+"/goods-receipts", receipt)
}

// GetGoodsReceipt returns one goods receipt with its lines.
func (c *Client) GetGoodsReceipt(ctx context.Context, garageID, receiptID string) (*inventoryv1.GoodsReceipt, error) {
	return call[inventoryv1.GoodsReceipt](ctx, c, http.MethodGet, goodsReceiptPath(garageID, receiptID), nil)
}

// ListGoodsReceipts returns garageID's goods receipts filtered by query
// parameters (query, supplierId, status, page, pageSize).
func (c *Client) ListGoodsReceipts(ctx context.Context, garageID string, query url.Values) ([]*inventoryv1.GoodsReceipt, error) {
	path := garagePath(garageID) + "/goods-receipts"
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}

	resp, err := call[inventoryv1.ListGoodsReceiptsResponse](ctx, c, http.MethodGet, path, nil)

	return resp.GetData(), err
}

// UpdateGoodsReceipt patches a draft goods receipt (mask paths:
// default_supplier_id, receipt_date, delivery_note_number, notes, lines).
func (c *Client) UpdateGoodsReceipt(
	ctx context.Context, garageID, receiptID string, receipt *inventoryv1.GoodsReceipt, paths ...string,
) (*inventoryv1.GoodsReceipt, error) {
	path := goodsReceiptPath(garageID, receiptID)
	if len(paths) > 0 {
		path += "?updateMask=" + url.QueryEscape(strings.Join(paths, ","))
	}

	return call[inventoryv1.GoodsReceipt](ctx, c, http.MethodPatch, path, receipt)
}

// CompleteGoodsReceipt posts the receipt's stock movements.
func (c *Client) CompleteGoodsReceipt(ctx context.Context, garageID, receiptID string) (*inventoryv1.GoodsReceipt, error) {
	return call[inventoryv1.GoodsReceipt](ctx, c, http.MethodPost, goodsReceiptPath(garageID, receiptID)+":complete", &inventoryv1.CompleteGoodsReceiptRequest{})
}

// CancelGoodsReceipt closes a draft or reverses a completed receipt.
func (c *Client) CancelGoodsReceipt(ctx context.Context, garageID, receiptID string) (*inventoryv1.GoodsReceipt, error) {
	return call[inventoryv1.GoodsReceipt](ctx, c, http.MethodPost, goodsReceiptPath(garageID, receiptID)+":cancel", &inventoryv1.CancelGoodsReceiptRequest{})
}

// ReceiveStock records and completes a one-line goods receipt of partID: the
// quantity is typed in unitID (empty: the base unit) and unitCost is VND per
// that unit (0: not recorded). The supplier is a garage supplier created on
// first use.
func (c *Client) ReceiveStock(
	ctx context.Context, garageID, partID, quantity, unitID string, unitCost int64,
) (*inventoryv1.GoodsReceipt, error) {
	supplierID, err := c.receivingSupplier(ctx, garageID)
	if err != nil {
		return nil, err
	}

	line := &inventoryv1.GoodsReceiptLine{PartId: partID, EnteredQuantity: quantity, UnitCost: &unitCost}
	if unitID != "" {
		line.UnitId = &unitID
	}

	draft, err := c.CreateGoodsReceipt(ctx, garageID, &inventoryv1.GoodsReceipt{
		DefaultSupplierId: &supplierID, Lines: []*inventoryv1.GoodsReceiptLine{line},
	})
	if err != nil {
		return nil, err
	}

	return c.CompleteGoodsReceipt(ctx, garageID, draft.GetId())
}

// receivingSupplier returns the garage's first supplier, adding one when it has none.
func (c *Client) receivingSupplier(ctx context.Context, garageID string) (string, error) {
	suppliers, err := c.ListSuppliers(ctx, garageID)
	if err != nil {
		return "", err
	}

	if len(suppliers) > 0 {
		return suppliers[0].GetId(), nil
	}

	created, err := c.CreateSupplier(ctx, garageID, &supplierv1.Supplier{Name: "Blackbox supplier"})
	if err != nil {
		return "", err
	}

	return created.GetId(), nil
}

// GetPartSuppliers returns the usual suppliers of a part.
func (c *Client) GetPartSuppliers(ctx context.Context, garageID, partID string) ([]*inventoryv1.PartSupplier, error) {
	resp, err := call[inventoryv1.GetPartSuppliersResponse](ctx, c, http.MethodGet,
		garagePath(garageID)+"/parts/"+url.PathEscape(partID)+"/suppliers", nil)

	return resp.GetData(), err
}

// PutPartSuppliers replaces the usual suppliers of a part.
func (c *Client) PutPartSuppliers(ctx context.Context, garageID, partID string, list []*inventoryv1.PartSupplier) ([]*inventoryv1.PartSupplier, error) {
	resp, err := call[inventoryv1.PutPartSuppliersResponse](ctx, c, http.MethodPut,
		garagePath(garageID)+"/parts/"+url.PathEscape(partID)+"/suppliers", &inventoryv1.PutPartSuppliersRequest{Data: list})

	return resp.GetData(), err
}

// GetRepairOrderStockIssue returns the repair order's stock issue.
func (c *Client) GetRepairOrderStockIssue(ctx context.Context, garageID, orderID string) (*inventoryv1.StockIssue, error) {
	resp, err := call[inventoryv1.GetRepairOrderStockIssueResponse](ctx, c, http.MethodGet,
		repairOrderPath(garageID, orderID)+"/stock-issue", nil)

	return resp.GetStockIssue(), err
}

// PostStockIssue posts a draft stock issue now.
func (c *Client) PostStockIssue(ctx context.Context, garageID, issueID string) (*inventoryv1.StockIssue, error) {
	return call[inventoryv1.StockIssue](ctx, c, http.MethodPost,
		garagePath(garageID)+"/stock-issues/"+url.PathEscape(issueID)+":post", &inventoryv1.PostStockIssueRequest{})
}

// GetPurchasesReport returns the purchases-by-supplier report (from
// inclusive, to exclusive, "YYYY-MM-DD" or empty).
func (c *Client) GetPurchasesReport(ctx context.Context, garageID, supplierID, from, to string) (*inventoryv1.GetPurchasesReportResponse, error) {
	query := url.Values{}

	for key, value := range map[string]string{"supplierId": supplierID, "from": from, "to": to} {
		if value != "" {
			query.Set(key, value)
		}
	}

	path := garagePath(garageID) + "/reports/purchases"
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}

	return call[inventoryv1.GetPurchasesReportResponse](ctx, c, http.MethodGet, path, nil)
}

// GetStockValuationReport returns the stock valuation at a date ("YYYY-MM-DD",
// empty for today).
func (c *Client) GetStockValuationReport(ctx context.Context, garageID, at string) (*inventoryv1.GetStockValuationReportResponse, error) {
	path := garagePath(garageID) + "/reports/stock-valuation"
	if at != "" {
		path += "?at=" + url.QueryEscape(at)
	}

	return call[inventoryv1.GetStockValuationReportResponse](ctx, c, http.MethodGet, path, nil)
}
