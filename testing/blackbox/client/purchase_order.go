package client

import (
	"context"
	"net/http"
	"net/url"

	inventoryv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/inventory/v1"
)

// ---- Purchase orders (Release 11.0) ------------------------------------------

func purchaseOrderPath(garageID, orderID string) string {
	return garagePath(garageID) + "/purchase-orders/" + url.PathEscape(orderID)
}

// ListPurchaseOrders returns a page of the garage's orders, newest first;
// query may hold query, supplierId, status, page and pageSize.
func (c *Client) ListPurchaseOrders(ctx context.Context, garageID string, query url.Values) (*inventoryv1.ListPurchaseOrdersResponse, error) {
	return call[inventoryv1.ListPurchaseOrdersResponse](ctx, c, http.MethodGet, withQuery(garagePath(garageID)+"/purchase-orders", query), nil)
}

// GetPurchaseOrder returns one order with its lines.
func (c *Client) GetPurchaseOrder(ctx context.Context, garageID, orderID string) (*inventoryv1.PurchaseOrder, error) {
	return call[inventoryv1.PurchaseOrder](ctx, c, http.MethodGet, purchaseOrderPath(garageID, orderID), nil)
}

// CreatePurchaseOrder saves a draft order.
func (c *Client) CreatePurchaseOrder(ctx context.Context, garageID string, order *inventoryv1.PurchaseOrder) (*inventoryv1.PurchaseOrder, error) {
	return call[inventoryv1.PurchaseOrder](ctx, c, http.MethodPost, garagePath(garageID)+"/purchase-orders", order)
}

// UpdatePurchaseOrder replaces a draft order.
func (c *Client) UpdatePurchaseOrder(ctx context.Context, garageID, orderID string, order *inventoryv1.PurchaseOrder) (*inventoryv1.PurchaseOrder, error) {
	return call[inventoryv1.PurchaseOrder](ctx, c, http.MethodPut, purchaseOrderPath(garageID, orderID), order)
}

// CreatePurchaseOrdersFromLowStock makes one draft per default supplier for
// the low parts (partIDs: only these; empty: every low part).
func (c *Client) CreatePurchaseOrdersFromLowStock(ctx context.Context, garageID string, partIDs []string, expectedDate string) (*inventoryv1.CreatePurchaseOrdersFromLowStockResponse, error) {
	return call[inventoryv1.CreatePurchaseOrdersFromLowStockResponse](ctx, c, http.MethodPost, garagePath(garageID)+"/purchase-orders:from-low-stock",
		&inventoryv1.CreatePurchaseOrdersFromLowStockRequest{PartIds: partIDs, ExpectedDate: expectedDate})
}

// SendPurchaseOrder e-mails the order to the supplier's address on file.
func (c *Client) SendPurchaseOrder(ctx context.Context, garageID, orderID, message string) (*inventoryv1.PurchaseOrder, error) {
	return call[inventoryv1.PurchaseOrder](ctx, c, http.MethodPost, purchaseOrderPath(garageID, orderID)+":send",
		&inventoryv1.SendPurchaseOrderRequest{Message: message})
}

// CancelPurchaseOrder cancels an order.
func (c *Client) CancelPurchaseOrder(ctx context.Context, garageID, orderID string) (*inventoryv1.PurchaseOrder, error) {
	return call[inventoryv1.PurchaseOrder](ctx, c, http.MethodPost, purchaseOrderPath(garageID, orderID)+":cancel", &inventoryv1.CancelPurchaseOrderRequest{})
}

// ReceivePurchaseOrder creates the draft goods receipt for what is outstanding.
func (c *Client) ReceivePurchaseOrder(ctx context.Context, garageID, orderID string) (*inventoryv1.GoodsReceipt, error) {
	return call[inventoryv1.GoodsReceipt](ctx, c, http.MethodPost, purchaseOrderPath(garageID, orderID)+":receive", &inventoryv1.ReceivePurchaseOrderRequest{})
}

// DownloadPurchaseOrderPDF downloads an order's PDF.
func (c *Client) DownloadPurchaseOrderPDF(ctx context.Context, garageID, orderID string) (string, []byte, error) {
	return c.DownloadRaw(ctx, http.MethodGet, purchaseOrderPath(garageID, orderID)+"/pdf")
}

// PrintPurchaseOrderPDF prints an order: the PDF, and a line in the print log.
func (c *Client) PrintPurchaseOrderPDF(ctx context.Context, garageID, orderID string) (string, []byte, error) {
	return c.DownloadRaw(ctx, http.MethodPost, purchaseOrderPath(garageID, orderID)+"/pdf:print")
}
