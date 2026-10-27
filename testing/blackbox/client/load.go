package client

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"
)

// ListCustomersPage returns one page of garageID's customers (page size up to
// 100) and the total number of customers.
func (c *Client) ListCustomersPage(ctx context.Context, garageID string, page, pageSize int) ([]*customerv1.Customer, int, error) {
	resp, err := call[customerv1.ListCustomersResponse](ctx, c, http.MethodGet,
		garagePath(garageID)+"/customers?"+pageQuery(page, pageSize), nil)

	return resp.GetData(), int(resp.GetPagination().GetTotal()), err
}

// ListVehiclesPage returns one page of garageID's vehicles (page size up to
// 100) and the total number of vehicles.
func (c *Client) ListVehiclesPage(ctx context.Context, garageID string, page, pageSize int) ([]*customerv1.Vehicle, int, error) {
	resp, err := call[customerv1.ListVehiclesResponse](ctx, c, http.MethodGet,
		garagePath(garageID)+"/vehicles?"+pageQuery(page, pageSize), nil)

	return resp.GetData(), int(resp.GetPagination().GetTotal()), err
}

func pageQuery(page, pageSize int) string {
	return url.Values{"page": {strconv.Itoa(page)}, "pageSize": {strconv.Itoa(pageSize)}}.Encode()
}
