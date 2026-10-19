package client

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	cataloguev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/catalogue/v1"
	quotev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/quote/v1"
	repairorderv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/repairorder/v1"
)

// This file covers Release 7.3: the job catalogue, adding a job to a quote,
// and the repair order line and cancellation endpoints.

func jobPath(garageID, jobID string) string {
	return garagePath(garageID) + "/jobs/" + url.PathEscape(jobID)
}

// ListJobsPage returns one page of the job catalogue with its pagination;
// query takes query, category, includeInactive, page and pageSize.
func (c *Client) ListJobsPage(ctx context.Context, garageID string, query url.Values) (*cataloguev1.ListJobsResponse, error) {
	path := garagePath(garageID) + "/jobs"
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}

	return call[cataloguev1.ListJobsResponse](ctx, c, http.MethodGet, path, nil)
}

// ListJobs returns the jobs matching query.
func (c *Client) ListJobs(ctx context.Context, garageID string, query url.Values) ([]*cataloguev1.Job, error) {
	resp, err := c.ListJobsPage(ctx, garageID, query)

	return resp.GetData(), err
}

// CreateJob adds a job to the catalogue.
func (c *Client) CreateJob(ctx context.Context, garageID string, job *cataloguev1.Job) (*cataloguev1.Job, error) {
	return call[cataloguev1.Job](ctx, c, http.MethodPost, garagePath(garageID)+"/jobs", job)
}

// GetJob returns one job.
func (c *Client) GetJob(ctx context.Context, garageID, jobID string) (*cataloguev1.Job, error) {
	return call[cataloguev1.Job](ctx, c, http.MethodGet, jobPath(garageID, jobID), nil)
}

// UpdateJob patches a job; paths name the fields to change (proto field
// names, "parts" replaces the whole list).
func (c *Client) UpdateJob(ctx context.Context, garageID, jobID string, job *cataloguev1.Job, paths ...string) (*cataloguev1.Job, error) {
	path := jobPath(garageID, jobID)
	if len(paths) > 0 {
		path += "?updateMask=" + url.QueryEscape(strings.Join(paths, ","))
	}

	return call[cataloguev1.Job](ctx, c, http.MethodPatch, path, job)
}

// DeleteJob deletes a job, or deactivates it when a quote or an order line
// came from it; dryRun only reports which.
func (c *Client) DeleteJob(ctx context.Context, garageID, jobID string, dryRun bool) (*cataloguev1.DeleteJobResponse, error) {
	path := jobPath(garageID, jobID)
	if dryRun {
		path += dryRunQuery
	}

	return call[cataloguev1.DeleteJobResponse](ctx, c, http.MethodDelete, path, nil)
}

// ActivateJob reactivates a deactivated job.
func (c *Client) ActivateJob(ctx context.Context, garageID, jobID string) (*cataloguev1.Job, error) {
	return call[cataloguev1.Job](ctx, c, http.MethodPost, jobPath(garageID, jobID)+":activate", &cataloguev1.ActivateJobRequest{})
}

// AddQuoteJob appends a job's labor and part lines to a draft quote.
func (c *Client) AddQuoteJob(ctx context.Context, garageID, quoteID, jobID string) (*quotev1.Quote, error) {
	return call[quotev1.Quote](ctx, c, http.MethodPost, garagePath(garageID)+"/quotes/"+url.PathEscape(quoteID)+"/lines:fromJob",
		&quotev1.AddQuoteJobRequest{JobId: jobID})
}

// AddRepairOrderLine adds a line to a repair order that is not closed or
// cancelled.
func (c *Client) AddRepairOrderLine(
	ctx context.Context, garageID, orderID string, line *repairorderv1.RepairOrderLine,
) (*repairorderv1.RepairOrder, error) {
	return call[repairorderv1.RepairOrder](ctx, c, http.MethodPost, repairOrderPath(garageID, orderID)+"/lines", line)
}

// DeleteRepairOrderLine removes a line from a repair order that is not closed
// or cancelled.
func (c *Client) DeleteRepairOrderLine(ctx context.Context, garageID, orderID, lineID string) (*repairorderv1.RepairOrder, error) {
	return call[repairorderv1.RepairOrder](ctx, c, http.MethodDelete,
		repairOrderPath(garageID, orderID)+"/lines/"+url.PathEscape(lineID), nil)
}

// CancelRepairOrder cancels a repair order whose work has not started.
func (c *Client) CancelRepairOrder(ctx context.Context, garageID, orderID string) (*repairorderv1.RepairOrder, error) {
	return call[repairorderv1.RepairOrder](ctx, c, http.MethodPost, repairOrderPath(garageID, orderID)+"/cancellation",
		&repairorderv1.CancelRepairOrderRequest{})
}
