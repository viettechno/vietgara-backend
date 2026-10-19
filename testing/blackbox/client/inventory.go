package client

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
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

// ---- Stock counts and adjustments (Release 7.1) -----------------------------

func stockCountPath(garageID, countID string) string {
	return garagePath(garageID) + "/stock-counts/" + url.PathEscape(countID)
}

// CreateStockCount saves an empty draft stock count (name and notes).
func (c *Client) CreateStockCount(ctx context.Context, garageID string, count *inventoryv1.StockCount) (*inventoryv1.StockCount, error) {
	return call[inventoryv1.StockCount](ctx, c, http.MethodPost, garagePath(garageID)+"/stock-counts", count)
}

// GetStockCount returns one stock count with its lines.
func (c *Client) GetStockCount(ctx context.Context, garageID, countID string) (*inventoryv1.StockCount, error) {
	return call[inventoryv1.StockCount](ctx, c, http.MethodGet, stockCountPath(garageID, countID), nil)
}

// ListStockCounts returns garageID's stock counts filtered by query
// parameters (query, status, page, pageSize).
func (c *Client) ListStockCounts(ctx context.Context, garageID string, query url.Values) ([]*inventoryv1.StockCount, error) {
	path := garagePath(garageID) + "/stock-counts"
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}

	resp, err := call[inventoryv1.ListStockCountsResponse](ctx, c, http.MethodGet, path, nil)

	return resp.GetData(), err
}

// UpdateStockCount patches the name and notes of a draft count.
func (c *Client) UpdateStockCount(
	ctx context.Context, garageID, countID string, count *inventoryv1.StockCount, paths ...string,
) (*inventoryv1.StockCount, error) {
	path := stockCountPath(garageID, countID)
	if len(paths) > 0 {
		path += "?updateMask=" + url.QueryEscape(strings.Join(paths, ","))
	}

	return call[inventoryv1.StockCount](ctx, c, http.MethodPatch, path, count)
}

// AddStockCountLines adds parts (by id, or by category) to a draft count.
func (c *Client) AddStockCountLines(ctx context.Context, garageID, countID string, partIDs []string, categoryID string) (*inventoryv1.StockCount, error) {
	return call[inventoryv1.StockCount](ctx, c, http.MethodPost, stockCountPath(garageID, countID)+"/lines",
		&inventoryv1.AddStockCountLinesRequest{PartIds: partIDs, CategoryId: categoryID})
}

// CountStockLine types the counted quantity of a line (unitID empty for the
// base unit, quantity empty to mark the line uncounted).
func (c *Client) CountStockLine(ctx context.Context, garageID, countID, lineID, unitID, quantity string) (*inventoryv1.StockCount, error) {
	line := &inventoryv1.StockCountLine{EnteredCountedQuantity: quantity}
	if unitID != "" {
		line.UnitId = &unitID
	}

	return call[inventoryv1.StockCount](ctx, c, http.MethodPatch, stockCountPath(garageID, countID)+"/lines/"+url.PathEscape(lineID), line)
}

// DeleteStockCountLine removes a line from a draft count.
func (c *Client) DeleteStockCountLine(ctx context.Context, garageID, countID, lineID string) (*inventoryv1.StockCount, error) {
	return call[inventoryv1.StockCount](ctx, c, http.MethodDelete, stockCountPath(garageID, countID)+"/lines/"+url.PathEscape(lineID), nil)
}

// MergeStockCounts folds draft counts into the first one.
func (c *Client) MergeStockCounts(ctx context.Context, garageID string, countIDs ...string) (*inventoryv1.StockCount, error) {
	return call[inventoryv1.StockCount](ctx, c, http.MethodPost, garagePath(garageID)+"/stock-counts:merge",
		&inventoryv1.MergeStockCountsRequest{StockCountIds: countIDs})
}

// CompleteStockCount posts the adjustments of a draft count.
func (c *Client) CompleteStockCount(ctx context.Context, garageID, countID string, skipUncounted bool) (*inventoryv1.StockCount, error) {
	return call[inventoryv1.StockCount](ctx, c, http.MethodPost, stockCountPath(garageID, countID)+":complete",
		&inventoryv1.CompleteStockCountRequest{SkipUncounted: skipUncounted})
}

// CancelStockCount closes a draft or reverses a completed count.
func (c *Client) CancelStockCount(ctx context.Context, garageID, countID string) (*inventoryv1.StockCount, error) {
	return call[inventoryv1.StockCount](ctx, c, http.MethodPost, stockCountPath(garageID, countID)+":cancel", &inventoryv1.CancelStockCountRequest{})
}

// DownloadStockCountExcel downloads a stock count as an Excel workbook.
func (c *Client) DownloadStockCountExcel(ctx context.Context, garageID, countID string) (string, []byte, error) {
	return c.DownloadRaw(ctx, http.MethodGet, stockCountPath(garageID, countID)+"/excel")
}

func writeOffPath(garageID, writeOffID string) string {
	return garagePath(garageID) + "/write-offs/" + url.PathEscape(writeOffID)
}

// CreateWriteOff posts a write-off.
func (c *Client) CreateWriteOff(ctx context.Context, garageID string, writeOff *inventoryv1.WriteOff) (*inventoryv1.WriteOff, error) {
	return call[inventoryv1.WriteOff](ctx, c, http.MethodPost, garagePath(garageID)+"/write-offs", writeOff)
}

// GetWriteOff returns one write-off with its lines.
func (c *Client) GetWriteOff(ctx context.Context, garageID, writeOffID string) (*inventoryv1.WriteOff, error) {
	return call[inventoryv1.WriteOff](ctx, c, http.MethodGet, writeOffPath(garageID, writeOffID), nil)
}

// ListWriteOffs returns garageID's write-offs filtered by query parameters
// (query, reason, status, page, pageSize).
func (c *Client) ListWriteOffs(ctx context.Context, garageID string, query url.Values) ([]*inventoryv1.WriteOff, error) {
	path := garagePath(garageID) + "/write-offs"
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}

	resp, err := call[inventoryv1.ListWriteOffsResponse](ctx, c, http.MethodGet, path, nil)

	return resp.GetData(), err
}

// CancelWriteOff puts the written-off parts back in stock.
func (c *Client) CancelWriteOff(ctx context.Context, garageID, writeOffID string) (*inventoryv1.WriteOff, error) {
	return call[inventoryv1.WriteOff](ctx, c, http.MethodPost, writeOffPath(garageID, writeOffID)+":cancel", &inventoryv1.CancelWriteOffRequest{})
}

func supplierReturnPath(garageID, returnID string) string {
	return garagePath(garageID) + "/supplier-returns/" + url.PathEscape(returnID)
}

// CreateSupplierReturn posts a return to supplier.
func (c *Client) CreateSupplierReturn(ctx context.Context, garageID string, ret *inventoryv1.SupplierReturn) (*inventoryv1.SupplierReturn, error) {
	return call[inventoryv1.SupplierReturn](ctx, c, http.MethodPost, garagePath(garageID)+"/supplier-returns", ret)
}

// GetSupplierReturn returns one return to supplier with its lines.
func (c *Client) GetSupplierReturn(ctx context.Context, garageID, returnID string) (*inventoryv1.SupplierReturn, error) {
	return call[inventoryv1.SupplierReturn](ctx, c, http.MethodGet, supplierReturnPath(garageID, returnID), nil)
}

// ListSupplierReturns returns garageID's returns to supplier filtered by
// query parameters (query, supplierId, status, page, pageSize).
func (c *Client) ListSupplierReturns(ctx context.Context, garageID string, query url.Values) ([]*inventoryv1.SupplierReturn, error) {
	path := garagePath(garageID) + "/supplier-returns"
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}

	resp, err := call[inventoryv1.ListSupplierReturnsResponse](ctx, c, http.MethodGet, path, nil)

	return resp.GetData(), err
}

// CancelSupplierReturn puts the returned parts back in stock.
func (c *Client) CancelSupplierReturn(ctx context.Context, garageID, returnID string) (*inventoryv1.SupplierReturn, error) {
	return call[inventoryv1.SupplierReturn](ctx, c, http.MethodPost, supplierReturnPath(garageID, returnID)+":cancel", &inventoryv1.CancelSupplierReturnRequest{})
}

// ReturnRepairOrderPart returns unused parts of a repair order to stock.
func (c *Client) ReturnRepairOrderPart(
	ctx context.Context, garageID, orderID string, ret *inventoryv1.RepairOrderPartReturn,
) (*inventoryv1.RepairOrderPartReturn, error) {
	return call[inventoryv1.RepairOrderPartReturn](ctx, c, http.MethodPost, repairOrderPath(garageID, orderID)+"/part-returns", ret)
}

// ListRepairOrderPartReturns returns the parts returned from a repair order.
func (c *Client) ListRepairOrderPartReturns(ctx context.Context, garageID, orderID string) ([]*inventoryv1.RepairOrderPartReturn, error) {
	resp, err := call[inventoryv1.ListRepairOrderPartReturnsResponse](ctx, c, http.MethodGet, repairOrderPath(garageID, orderID)+"/part-returns", nil)

	return resp.GetData(), err
}

// ListStockCard returns the movements of one part (its stock card), newest
// first, optionally only those caused by one kind of document (refType is the
// proto enum value, 0 for all).
func (c *Client) ListStockCard(ctx context.Context, garageID, partID string, refType inventoryv1.StockTransactionRefType) ([]*inventoryv1.StockTransaction, error) {
	query := url.Values{"partId": {partID}}
	if refType != inventoryv1.StockTransactionRefType_STOCK_TRANSACTION_REF_TYPE_UNSPECIFIED {
		query.Set("refType", strconv.Itoa(int(refType.Number())))
	}

	resp, err := call[inventoryv1.ListStockTransactionsResponse](ctx, c, http.MethodGet,
		garagePath(garageID)+"/reports/stock-movements?"+query.Encode(), nil)

	return resp.GetData(), err
}

// ---- Excel import (Release 7.2) -------------------------------------------

func importPath(garageID string, importType inventoryv1.ImportType, action string) string {
	return garagePath(garageID) + "/imports/" + importType.String() + "/" + action
}

// DownloadImportTemplate fetches the template workbook of an import type.
func (c *Client) DownloadImportTemplate(ctx context.Context, garageID string, importType inventoryv1.ImportType) (string, []byte, error) {
	return c.DownloadRaw(ctx, http.MethodGet, importPath(garageID, importType, "template"))
}

// PreviewImport checks a workbook and lists its problems without writing.
func (c *Client) PreviewImport(
	ctx context.Context, garageID string, importType inventoryv1.ImportType, fileName string, content []byte,
) (*inventoryv1.ImportPreview, error) {
	resp, err := call[inventoryv1.ImportPreview](ctx, c, http.MethodPost, importPath(garageID, importType, "preview"),
		&inventoryv1.PreviewImportRequest{GarageId: garageID, ImportType: importType, FileName: fileName, Content: content})

	return resp, err
}

// ConfirmImport imports a workbook as a whole or not at all.
func (c *Client) ConfirmImport(
	ctx context.Context, garageID string, importType inventoryv1.ImportType, fileName string, content []byte,
) (*inventoryv1.ImportResult, error) {
	return call[inventoryv1.ImportResult](ctx, c, http.MethodPost, importPath(garageID, importType, "confirm"),
		&inventoryv1.ConfirmImportRequest{GarageId: garageID, ImportType: importType, FileName: fileName, Content: content})
}
