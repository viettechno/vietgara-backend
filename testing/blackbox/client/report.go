package client

import (
	"context"
	"net/http"
	"net/url"

	reportingv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/reporting/v1"
)

// fromParam is the query parameter that starts a period.
const fromParam = "from"

// ---- Technician and profit reports (Release 11.1) ----------------------------

// GetTechnicianReport returns the technicians' completed lines in [from, to)
// ("YYYY-MM-DD" in the garage's zone).
func (c *Client) GetTechnicianReport(ctx context.Context, garageID, from, to string) (*reportingv1.GetTechnicianReportResponse, error) {
	q := url.Values{fromParam: {from}, "to": {to}}

	return call[reportingv1.GetTechnicianReportResponse](ctx, c, http.MethodGet, withQuery(garagePath(garageID)+"/reports/technicians", q), nil)
}

// GetProfitReport returns the profit of the repair orders closed in [from, to)
// grouped by groupBy (REPAIR_ORDER, JOB or TECHNICIAN; empty: not sent).
func (c *Client) GetProfitReport(ctx context.Context, garageID, groupBy, from, to string) (*reportingv1.GetProfitReportResponse, error) {
	q := url.Values{fromParam: {from}, "to": {to}}
	if groupBy != "" {
		q.Set("groupBy", "PROFIT_GROUP_BY_"+groupBy)
	}

	return call[reportingv1.GetProfitReportResponse](ctx, c, http.MethodGet, withQuery(garagePath(garageID)+"/reports/profit", q), nil)
}

// GetRepairOrderProfit returns the profit of one repair order.
func (c *Client) GetRepairOrderProfit(ctx context.Context, garageID, orderID string) (*reportingv1.RepairOrderProfit, error) {
	return call[reportingv1.RepairOrderProfit](ctx, c, http.MethodGet, repairOrderPath(garageID, orderID)+"/profit", nil)
}
