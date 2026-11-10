package client

import (
	"context"
	"net/http"
	"net/url"

	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"
)

// ---- Personal-data requests (Release 11.1) ------------------------------------

// CreateDataRequest records a request a customer made in person.
func (c *Client) CreateDataRequest(
	ctx context.Context, garageID, customerID string, typ customerv1.DataRequestType, details string,
) (*customerv1.DataRequest, error) {
	return call[customerv1.DataRequest](ctx, c, http.MethodPost,
		garagePath(garageID)+"/customers/"+url.PathEscape(customerID)+"/data-requests",
		&customerv1.CreateDataRequestRequest{Type: typ, Details: details})
}

// ListDataRequests returns a page of the garage's data requests; query may hold
// customerId, type, status, page and pageSize.
func (c *Client) ListDataRequests(ctx context.Context, garageID string, query url.Values) (*customerv1.ListDataRequestsResponse, error) {
	return call[customerv1.ListDataRequestsResponse](ctx, c, http.MethodGet, withQuery(garagePath(garageID)+"/data-requests", query), nil)
}

// GetDataRequest returns one data request.
func (c *Client) GetDataRequest(ctx context.Context, garageID, requestID string) (*customerv1.DataRequest, error) {
	return call[customerv1.DataRequest](ctx, c, http.MethodGet, garagePath(garageID)+"/data-requests/"+url.PathEscape(requestID), nil)
}

// CompleteDataRequest completes a waiting correction with outcome, or declines it.
func (c *Client) CompleteDataRequest(ctx context.Context, garageID, requestID, outcome string, declined bool) (*customerv1.DataRequest, error) {
	return call[customerv1.DataRequest](ctx, c, http.MethodPost, garagePath(garageID)+"/data-requests/"+url.PathEscape(requestID)+":complete",
		&customerv1.CompleteDataRequestRequest{Outcome: outcome, Declined: declined})
}

// SubmitPublicDataRequest asks, without signing in, to see, correct or delete the
// data held under an e-mail address.
func (c *Client) SubmitPublicDataRequest(
	ctx context.Context, email string, typ customerv1.DataRequestType, details, locale string,
) (*customerv1.SubmitPublicDataRequestResponse, error) {
	return call[customerv1.SubmitPublicDataRequestResponse](ctx, c, http.MethodPost, "/api/v1/public/data-requests",
		&customerv1.SubmitPublicDataRequestRequest{Email: email, Type: typ, Details: details, Locale: locale})
}

// ConfirmPublicDataRequest confirms a public request with the token of its e-mail.
func (c *Client) ConfirmPublicDataRequest(ctx context.Context, token string) (*customerv1.ConfirmPublicDataRequestResponse, error) {
	return call[customerv1.ConfirmPublicDataRequestResponse](ctx, c, http.MethodPost, "/api/v1/public/data-requests:confirm",
		&customerv1.ConfirmPublicDataRequestRequest{Token: token})
}
