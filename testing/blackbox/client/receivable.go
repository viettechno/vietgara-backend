package client

import (
	"context"
	"net/http"
	"net/url"

	receivablev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/receivable/v1"
)

// ---- Receivables (Release 13.0) ---------------------------------------------

// ReceivableFilter is the filter of ListReceivables; the zero value lists the
// open receivables.
type ReceivableFilter struct {
	CustomerID  string
	OverdueOnly bool
	IncludePaid bool
	Query       string
	Page        string
	PageSize    string
}

func (f ReceivableFilter) values() url.Values {
	q := url.Values{}

	if f.CustomerID != "" {
		q.Set("customerId", f.CustomerID)
	}

	if f.OverdueOnly {
		q.Set("overdueOnly", "true")
	}

	if f.IncludePaid {
		q.Set("includePaid", "true")
	}

	if f.Query != "" {
		q.Set("query", f.Query)
	}

	if f.Page != "" {
		q.Set("page", f.Page)
	}

	if f.PageSize != "" {
		q.Set("pageSize", f.PageSize)
	}

	return q
}

func withQuery(path string, q url.Values) string {
	if encoded := q.Encode(); encoded != "" {
		return path + "?" + encoded
	}

	return path
}

// ListReceivables returns a page of the garage's receivables with the summary.
func (c *Client) ListReceivables(ctx context.Context, garageID string, f ReceivableFilter) (*receivablev1.ListReceivablesResponse, error) {
	return call[receivablev1.ListReceivablesResponse](ctx, c, http.MethodGet, withQuery(garagePath(garageID)+"/receivables", f.values()), nil)
}

// GetReceivable returns one receivable with its ledger and reminders.
func (c *Client) GetReceivable(ctx context.Context, garageID, receivableID string) (*receivablev1.Receivable, error) {
	return call[receivablev1.Receivable](ctx, c, http.MethodGet, garagePath(garageID)+"/receivables/"+url.PathEscape(receivableID), nil)
}

// GetReceivableSettings returns the garage's payment terms and reminder schedule.
func (c *Client) GetReceivableSettings(ctx context.Context, garageID string) (*receivablev1.ReceivableSettings, error) {
	return call[receivablev1.ReceivableSettings](ctx, c, http.MethodGet, garagePath(garageID)+"/receivable-settings", nil)
}

// UpdateReceivableSettings replaces the settings.
func (c *Client) UpdateReceivableSettings(ctx context.Context, garageID string, s *receivablev1.ReceivableSettings) (*receivablev1.ReceivableSettings, error) {
	return call[receivablev1.ReceivableSettings](ctx, c, http.MethodPut, garagePath(garageID)+"/receivable-settings", s)
}

// GetReconciliation reconciles the period [from, to).
func (c *Client) GetReconciliation(ctx context.Context, garageID, from, to string) (*receivablev1.ReconciliationReport, error) {
	q := url.Values{fromParam: {from}, "to": {to}}

	return call[receivablev1.ReconciliationReport](ctx, c, http.MethodGet, withQuery(garagePath(garageID)+"/reports/receivable-reconciliation", q), nil)
}

// DownloadReceivablesExcel downloads the receivables list as a workbook.
func (c *Client) DownloadReceivablesExcel(ctx context.Context, garageID string, f ReceivableFilter) (string, []byte, error) {
	q := f.values()
	q.Del("page")
	q.Del("pageSize")

	return c.DownloadRaw(ctx, http.MethodGet, withQuery(garagePath(garageID)+"/reports/receivables/excel", q))
}

// DownloadReconciliationExcel downloads the reconciliation of a period as a workbook.
func (c *Client) DownloadReconciliationExcel(ctx context.Context, garageID, from, to string) (string, []byte, error) {
	q := url.Values{fromParam: {from}, "to": {to}}

	return c.DownloadRaw(ctx, http.MethodGet, withQuery(garagePath(garageID)+"/reports/receivable-reconciliation/excel", q))
}
