package client

import (
	"context"
	"net/http"
	"net/url"

	receivablev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/receivable/v1"
)

// ---- Supplier payables and payment-out vouchers (Release 11.0) --------------

// ListSupplierPayables returns a page of the garage's payables; includePaid
// also lists the paid ones.
func (c *Client) ListSupplierPayables(ctx context.Context, garageID, supplierID string, includePaid bool) (*receivablev1.ListSupplierPayablesResponse, error) {
	q := url.Values{}
	if supplierID != "" {
		q.Set("supplierId", supplierID)
	}

	if includePaid {
		q.Set("includePaid", "true")
	}

	return call[receivablev1.ListSupplierPayablesResponse](ctx, c, http.MethodGet, withQuery(garagePath(garageID)+"/supplier-payables", q), nil)
}

// GetSupplierPayablesAging returns the open payables by supplier and age.
func (c *Client) GetSupplierPayablesAging(ctx context.Context, garageID string) (*receivablev1.GetSupplierPayablesAgingResponse, error) {
	return call[receivablev1.GetSupplierPayablesAgingResponse](ctx, c, http.MethodGet, garagePath(garageID)+"/supplier-payables/aging", nil)
}

func voucherPath(garageID, voucherID string) string {
	return garagePath(garageID) + "/payment-out-vouchers/" + url.PathEscape(voucherID)
}

// ListPaymentOutVouchers returns the garage's vouchers, newest first.
func (c *Client) ListPaymentOutVouchers(ctx context.Context, garageID, supplierID string, includeVoided bool) (*receivablev1.ListPaymentOutVouchersResponse, error) {
	q := url.Values{}
	if supplierID != "" {
		q.Set("supplierId", supplierID)
	}

	if includeVoided {
		q.Set("includeVoided", "true")
	}

	return call[receivablev1.ListPaymentOutVouchersResponse](ctx, c, http.MethodGet, withQuery(garagePath(garageID)+"/payment-out-vouchers", q), nil)
}

// GetPaymentOutVoucher returns one voucher with its allocations.
func (c *Client) GetPaymentOutVoucher(ctx context.Context, garageID, voucherID string) (*receivablev1.PaymentOutVoucher, error) {
	return call[receivablev1.PaymentOutVoucher](ctx, c, http.MethodGet, voucherPath(garageID, voucherID), nil)
}

// CreatePaymentOutVoucher issues a voucher; send it with an Idempotency-Key.
func (c *Client) CreatePaymentOutVoucher(ctx context.Context, garageID string, v *receivablev1.PaymentOutVoucher) (*receivablev1.PaymentOutVoucher, error) {
	return call[receivablev1.PaymentOutVoucher](ctx, c, http.MethodPost, garagePath(garageID)+"/payment-out-vouchers", v)
}

// VoidPaymentOutVoucher voids a voucher.
func (c *Client) VoidPaymentOutVoucher(ctx context.Context, garageID, voucherID, reason string) (*receivablev1.PaymentOutVoucher, error) {
	return call[receivablev1.PaymentOutVoucher](ctx, c, http.MethodPost, voucherPath(garageID, voucherID)+":void",
		&receivablev1.VoidPaymentOutVoucherRequest{Reason: reason})
}

// DownloadPaymentOutVoucherPDF downloads a voucher's PDF.
func (c *Client) DownloadPaymentOutVoucherPDF(ctx context.Context, garageID, voucherID string) (string, []byte, error) {
	return c.DownloadRaw(ctx, http.MethodGet, voucherPath(garageID, voucherID)+"/pdf")
}

// PrintPaymentOutVoucherPDF prints a voucher: the PDF, and a line in the print log.
func (c *Client) PrintPaymentOutVoucherPDF(ctx context.Context, garageID, voucherID string) (string, []byte, error) {
	return c.DownloadRaw(ctx, http.MethodPost, voucherPath(garageID, voucherID)+"/pdf:print")
}

// DownloadSupplierPayablesExcel downloads the supplier payables as a workbook.
func (c *Client) DownloadSupplierPayablesExcel(ctx context.Context, garageID, supplierID string, includePaid bool) (string, []byte, error) {
	q := url.Values{}
	if supplierID != "" {
		q.Set("supplierId", supplierID)
	}

	if includePaid {
		q.Set("includePaid", "true")
	}

	return c.DownloadRaw(ctx, http.MethodGet, withQuery(garagePath(garageID)+"/reports/supplier-payables/excel", q))
}
