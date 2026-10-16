package client

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	billingv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/billing/v1"
	commonv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/common/v1"
	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"
	customerportalv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customerportal/v1"
	inventoryv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/inventory/v1"
	licensev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/license/v1"
	quotev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/quote/v1"
	repairorderv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/repairorder/v1"
	reportingv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/reporting/v1"
	supplierv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/supplier/v1"
	technicianv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/technician/v1"
	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"
)

// This file extends the typed client of api.go with the remaining Release
// 4.1 modules. The same conventions apply: the request body is the RPC's
// `body` field, the response its `response_body` field, and PATCH paths
// carry `?updateMask=` with proto field names.

// DownloadRaw fetches a raw (non-JSON) document such as a PDF or an Excel
// workbook. It returns the Content-Type and bytes of a 2xx response;
// otherwise it returns an *APIError decoded from the body when possible.
func (c *Client) DownloadRaw(ctx context.Context, method, path string) (string, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, http.NoBody)
	if err != nil {
		return "", nil, err
	}

	req.Header.Set("Accept", "*/*")

	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	for name, value := range c.headers {
		req.Header.Set(name, value)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, fmt.Errorf("%s %s: read body: %w", method, path, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", nil, decodeError(resp.StatusCode, raw)
	}

	return resp.Header.Get("Content-Type"), raw, nil
}

// ---- License ---------------------------------------------------------------

// GetGarageEntitlements returns what garageID may use; any active member
// may read it.
func (c *Client) GetGarageEntitlements(ctx context.Context, garageID string) (*licensev1.Entitlements, error) {
	resp, err := call[licensev1.Entitlements](ctx, c, http.MethodGet, garagePath(garageID)+"/entitlements", nil)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// ListAllPlans returns every plan, including hidden and inactive ones.
// Platform admin only.
func (c *Client) ListAllPlans(ctx context.Context) ([]*licensev1.Plan, error) {
	resp, err := call[licensev1.ListAllPlansResponse](ctx, c, http.MethodGet, "/api/v1/admin/plans", nil)

	return resp.GetData(), err
}

// CreatePlan creates a plan. Platform admin only.
func (c *Client) CreatePlan(ctx context.Context, plan *licensev1.Plan) (*licensev1.Plan, error) {
	return call[licensev1.Plan](ctx, c, http.MethodPost, "/api/v1/admin/plans", plan)
}

// UpdatePlan patches a plan (mask paths are proto field names).
// Platform admin only.
func (c *Client) UpdatePlan(ctx context.Context, planID string, plan *licensev1.Plan, paths ...string) (*licensev1.Plan, error) {
	path := "/api/v1/admin/plans/" + url.PathEscape(planID)
	if len(paths) > 0 {
		path += "?updateMask=" + url.QueryEscape(strings.Join(paths, ","))
	}

	return call[licensev1.Plan](ctx, c, http.MethodPatch, path, plan)
}

// ListSubscriptions returns owners' subscription summaries. Platform admin only.
// status 0 filters by nothing; query fuzzy-matches e-mail, name or garage.
func (c *Client) ListSubscriptions(ctx context.Context, query string,
	status licensev1.SubscriptionStatus,
) ([]*licensev1.SubscriptionSummary, error) {
	path := "/api/v1/admin/subscriptions"

	params := url.Values{}
	if query != "" {
		params.Set("query", query)
	}

	if status != 0 {
		params.Set("status", strconv.FormatInt(int64(status), 10))
	}

	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	resp, err := call[licensev1.ListSubscriptionsResponse](ctx, c, http.MethodGet, path, nil)

	return resp.GetData(), err
}

// ExtendSubscription extends a subscription by days. Platform admin only.
func (c *Client) ExtendSubscription(ctx context.Context, subscriptionID string, days int32) (*licensev1.Subscription, error) {
	return call[licensev1.Subscription](ctx, c, http.MethodPost,
		"/api/v1/admin/subscriptions/"+url.PathEscape(subscriptionID)+"/extensions",
		&licensev1.ExtendSubscriptionRequest{SubscriptionId: subscriptionID, Days: days})
}

// ApprovePlanPayment approves a submitted payment request; it returns the
// owner's subscription with the new plan and period. Platform admin only.
func (c *Client) ApprovePlanPayment(ctx context.Context, requestID, note string) (*licensev1.Subscription, error) {
	path := "/api/v1/admin/plan-payment-requests/" + url.PathEscape(requestID) + ":approve"

	return call[licensev1.Subscription](ctx, c, http.MethodPost, path,
		&licensev1.ApprovePlanPaymentRequest{RequestId: requestID, Note: note})
}

// RejectPlanPayment refuses a payment request. Platform admin only.
func (c *Client) RejectPlanPayment(ctx context.Context, requestID, reason string) (*licensev1.PlanPaymentRequest, error) {
	path := "/api/v1/admin/plan-payment-requests/" + url.PathEscape(requestID) + ":reject"

	return call[licensev1.PlanPaymentRequest](ctx, c, http.MethodPost, path,
		&licensev1.RejectPlanPaymentRequest{RequestId: requestID, Reason: reason})
}

// ---- Customers -------------------------------------------------------------

// SearchCustomers returns garageID's customers; query fuzzy-matches name,
// phone or e-mail.
func (c *Client) SearchCustomers(ctx context.Context, garageID, query string) ([]*customerv1.Customer, error) {
	path := garagePath(garageID) + "/customers"
	if query != "" {
		path += "?query=" + url.QueryEscape(query)
	}

	resp, err := call[customerv1.ListCustomersResponse](ctx, c, http.MethodGet, path, nil)

	return resp.GetData(), err
}

// UpdateCustomer patches a customer (mask paths are proto field names:
// "full_name", "phone", "email", "address", "personal_data_consent").
func (c *Client) UpdateCustomer(ctx context.Context, garageID, customerID string, customer *customerv1.Customer, paths ...string) (*customerv1.Customer, error) {
	path := garagePath(garageID) + "/customers/" + url.PathEscape(customerID)
	if len(paths) > 0 {
		path += "?updateMask=" + url.QueryEscape(strings.Join(paths, ","))
	}

	return call[customerv1.Customer](ctx, c, http.MethodPatch, path, customer)
}

// DeleteCustomer soft-deletes a customer (and deactivates instead when live
// documents refer to them).
func (c *Client) DeleteCustomer(ctx context.Context, garageID, customerID string) error {
	_, err := call[customerv1.DeleteCustomerResponse](ctx, c, http.MethodDelete,
		garagePath(garageID)+"/customers/"+url.PathEscape(customerID), nil)

	return err
}

// ActivateCustomer reactivates a deactivated customer.
func (c *Client) ActivateCustomer(ctx context.Context, garageID, customerID string) (*customerv1.Customer, error) {
	return call[customerv1.Customer](ctx, c, http.MethodPost,
		garagePath(garageID)+"/customers/"+url.PathEscape(customerID)+":activate",
		&customerv1.ActivateCustomerRequest{GarageId: garageID, CustomerId: customerID})
}

// LookupCustomers finds a garage's customers by exact phone and/or e-mail.
func (c *Client) LookupCustomers(ctx context.Context, garageID, phone, email string) ([]*customerv1.Customer, error) {
	query := url.Values{}
	if phone != "" {
		query.Set("phone", phone)
	}

	if email != "" {
		query.Set("email", email)
	}

	path := garagePath(garageID) + "/customers/lookup"
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}

	resp, err := call[customerv1.LookupCustomersResponse](ctx, c, http.MethodGet, path, nil)

	return resp.GetData(), err
}

// RequestCustomerUserLink asks to link a customer profile with the
// VietGara user matching its contact; fails when no user matches.
func (c *Client) RequestCustomerUserLink(ctx context.Context, garageID, customerID string) (*customerv1.CustomerUserLink, error) {
	resp, err := call[customerv1.CustomerUserLink](ctx, c, http.MethodPost,
		garagePath(garageID)+"/customers/"+url.PathEscape(customerID)+"/user-links",
		&customerv1.RequestCustomerUserLinkRequest{GarageId: garageID, CustomerId: customerID})
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// UnlinkCustomerUser ends the customer-user link from the garage's
// side; the profile stays.
func (c *Client) UnlinkCustomerUser(ctx context.Context, garageID, customerID string) (*customerv1.CustomerUserLink, error) {
	resp, err := call[customerv1.CustomerUserLink](ctx, c, http.MethodDelete,
		garagePath(garageID)+"/customers/"+url.PathEscape(customerID)+"/user-links/current", nil)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// GetVehicleHistory returns a vehicle's quotes and repair orders, newest
// first.
func (c *Client) GetVehicleHistory(ctx context.Context, garageID, vehicleID string) ([]*customerv1.VehicleHistoryEntry, error) {
	resp, err := call[customerv1.GetVehicleHistoryResponse](ctx, c, http.MethodGet,
		garagePath(garageID)+"/vehicles/"+url.PathEscape(vehicleID)+"/history", nil)

	return resp.GetData(), err
}

// ---- Vehicles --------------------------------------------------------------

// ListVehicles returns garageID's vehicles.
func (c *Client) ListVehicles(ctx context.Context, garageID string) ([]*customerv1.Vehicle, error) {
	resp, err := call[customerv1.ListVehiclesResponse](ctx, c, http.MethodGet, garagePath(garageID)+"/vehicles", nil)

	return resp.GetData(), err
}

// SearchVehicles returns garageID's vehicles matching query (plate,
// make or model).
func (c *Client) SearchVehicles(ctx context.Context, garageID, query string) ([]*customerv1.Vehicle, error) {
	path := garagePath(garageID) + "/vehicles"
	if query != "" {
		path += "?query=" + url.QueryEscape(query)
	}

	resp, err := call[customerv1.ListVehiclesResponse](ctx, c, http.MethodGet, path, nil)

	return resp.GetData(), err
}

// GetVehicle returns one vehicle of garageID.
func (c *Client) GetVehicle(ctx context.Context, garageID, vehicleID string) (*customerv1.Vehicle, error) {
	return call[customerv1.Vehicle](ctx, c, http.MethodGet,
		garagePath(garageID)+"/vehicles/"+url.PathEscape(vehicleID), nil)
}

// UpdateVehicle patches a vehicle's non-identity fields. Changing the
// owner here is refused; identity fields locked by a live document are
// refused with VEHICLE_FIELD_LOCKED (use CorrectVehicle instead).
func (c *Client) UpdateVehicle(ctx context.Context, garageID, vehicleID string, vehicle *customerv1.Vehicle, paths ...string) (*customerv1.Vehicle, error) {
	path := garagePath(garageID) + "/vehicles/" + url.PathEscape(vehicleID)
	if len(paths) > 0 {
		path += "?updateMask=" + url.QueryEscape(strings.Join(paths, ","))
	}

	return call[customerv1.Vehicle](ctx, c, http.MethodPatch, path, vehicle)
}

// DeleteVehicle soft-deletes a vehicle (deactivates instead when live
// documents refer to it).
func (c *Client) DeleteVehicle(ctx context.Context, garageID, vehicleID string) error {
	_, err := call[customerv1.DeleteVehicleResponse](ctx, c, http.MethodDelete,
		garagePath(garageID)+"/vehicles/"+url.PathEscape(vehicleID), nil)

	return err
}

// ActivateVehicle reactivates a deactivated vehicle.
func (c *Client) ActivateVehicle(ctx context.Context, garageID, vehicleID string) (*customerv1.Vehicle, error) {
	return call[customerv1.Vehicle](ctx, c, http.MethodPost,
		garagePath(garageID)+"/vehicles/"+url.PathEscape(vehicleID)+":activate",
		&customerv1.ActivateVehicleRequest{GarageId: garageID, VehicleId: vehicleID})
}

// CorrectVehicle fixes one identity field of a vehicle locked by a live
// document; reason is kept in the audit log.
func (c *Client) CorrectVehicle(ctx context.Context, garageID, vehicleID string, field customerv1.VehicleIdentityField, value, reason string) (*customerv1.Vehicle, error) {
	return call[customerv1.Vehicle](ctx, c, http.MethodPost,
		garagePath(garageID)+"/vehicles/"+url.PathEscape(vehicleID)+":correct",
		&customerv1.CorrectVehicleRequest{GarageId: garageID, VehicleId: vehicleID, Field: field, Value: value, Reason: reason})
}

// ChangeVehicleOwner moves a vehicle to another customer of the same
// garage; reason is kept in the audit log.
func (c *Client) ChangeVehicleOwner(ctx context.Context, garageID, vehicleID, customerID, reason string) (*customerv1.Vehicle, error) {
	return call[customerv1.Vehicle](ctx, c, http.MethodPost,
		garagePath(garageID)+"/vehicles/"+url.PathEscape(vehicleID)+":change-owner",
		&customerv1.ChangeVehicleOwnerRequest{GarageId: garageID, VehicleId: vehicleID, CustomerId: customerID, Reason: reason})
}

// ---- Suppliers -------------------------------------------------------------

// ListSuppliers returns garageID's suppliers.
func (c *Client) ListSuppliers(ctx context.Context, garageID string) ([]*supplierv1.Supplier, error) {
	resp, err := call[supplierv1.ListSuppliersResponse](ctx, c, http.MethodGet, garagePath(garageID)+"/suppliers", nil)

	return resp.GetData(), err
}

// SearchSuppliers returns garageID's suppliers matching query (name,
// tax code or contact).
func (c *Client) SearchSuppliers(ctx context.Context, garageID, query string) ([]*supplierv1.Supplier, error) {
	path := garagePath(garageID) + "/suppliers"
	if query != "" {
		path += "?query=" + url.QueryEscape(query)
	}

	resp, err := call[supplierv1.ListSuppliersResponse](ctx, c, http.MethodGet, path, nil)

	return resp.GetData(), err
}

// CreateSupplier adds a supplier to garageID.
func (c *Client) CreateSupplier(ctx context.Context, garageID string, supplier *supplierv1.Supplier) (*supplierv1.Supplier, error) {
	return call[supplierv1.Supplier](ctx, c, http.MethodPost, garagePath(garageID)+"/suppliers", supplier)
}

// GetSupplier returns one supplier of garageID.
func (c *Client) GetSupplier(ctx context.Context, garageID, supplierID string) (*supplierv1.Supplier, error) {
	return call[supplierv1.Supplier](ctx, c, http.MethodGet,
		garagePath(garageID)+"/suppliers/"+url.PathEscape(supplierID), nil)
}

// UpdateSupplier patches a supplier (mask paths are proto field names;
// "contact" replaces the whole contact block).
func (c *Client) UpdateSupplier(ctx context.Context, garageID, supplierID string, supplier *supplierv1.Supplier, paths ...string) (*supplierv1.Supplier, error) {
	path := garagePath(garageID) + "/suppliers/" + url.PathEscape(supplierID)
	if len(paths) > 0 {
		path += "?updateMask=" + url.QueryEscape(strings.Join(paths, ","))
	}

	return call[supplierv1.Supplier](ctx, c, http.MethodPatch, path, supplier)
}

// DeleteSupplier deletes (or deactivates, when referenced) a supplier.
func (c *Client) DeleteSupplier(ctx context.Context, garageID, supplierID string) error {
	_, err := call[supplierv1.DeleteSupplierResponse](ctx, c, http.MethodDelete,
		garagePath(garageID)+"/suppliers/"+url.PathEscape(supplierID), nil)

	return err
}

// ActivateSupplier reactivates a deactivated supplier.
func (c *Client) ActivateSupplier(ctx context.Context, garageID, supplierID string) (*supplierv1.Supplier, error) {
	return call[supplierv1.Supplier](ctx, c, http.MethodPost,
		garagePath(garageID)+"/suppliers/"+url.PathEscape(supplierID)+":activate",
		&supplierv1.ActivateSupplierRequest{GarageId: garageID, SupplierId: supplierID})
}

// ---- Quotes ----------------------------------------------------------------

// ListQuotes returns garageID's quotes, newest first.
func (c *Client) ListQuotes(ctx context.Context, garageID string) ([]*quotev1.Quote, error) {
	resp, err := call[quotev1.ListQuotesResponse](ctx, c, http.MethodGet, garagePath(garageID)+"/quotes", nil)

	return resp.GetData(), err
}

// ListQuotesByStatus returns garageID's quotes of one status, newest
// first.
func (c *Client) ListQuotesByStatus(ctx context.Context, garageID string, status quotev1.QuoteStatus) ([]*quotev1.Quote, error) {
	path := garagePath(garageID) + "/quotes"
	if status != quotev1.QuoteStatus_QUOTE_STATUS_UNSPECIFIED {
		path += "?status=" + url.QueryEscape(strconv.Itoa(int(status.Number())))
	}

	resp, err := call[quotev1.ListQuotesResponse](ctx, c, http.MethodGet, path, nil)

	return resp.GetData(), err
}

// CreateQuote creates a draft quote in garageID; counts against the plan's
// daily transaction limit.
func (c *Client) CreateQuote(ctx context.Context, garageID string, quote *quotev1.Quote) (*quotev1.Quote, error) {
	return call[quotev1.Quote](ctx, c, http.MethodPost, garagePath(garageID)+"/quotes", quote)
}

// GetQuote returns one quote of garageID.
func (c *Client) GetQuote(ctx context.Context, garageID, quoteID string) (*quotev1.Quote, error) {
	return call[quotev1.Quote](ctx, c, http.MethodGet,
		garagePath(garageID)+"/quotes/"+url.PathEscape(quoteID), nil)
}

// UpdateQuote replaces a draft quote's lines ("lines" mask); it fails
// once the quote is no longer a draft.
func (c *Client) UpdateQuote(ctx context.Context, garageID, quoteID string, quote *quotev1.Quote, paths ...string) (*quotev1.Quote, error) {
	path := garagePath(garageID) + "/quotes/" + url.PathEscape(quoteID)
	if len(paths) > 0 {
		path += "?updateMask=" + url.QueryEscape(strings.Join(paths, ","))
	}

	return call[quotev1.Quote](ctx, c, http.MethodPatch, path, quote)
}

// DeleteQuote deletes a draft quote.
func (c *Client) DeleteQuote(ctx context.Context, garageID, quoteID string) error {
	_, err := call[quotev1.DeleteQuoteResponse](ctx, c, http.MethodDelete,
		garagePath(garageID)+"/quotes/"+url.PathEscape(quoteID), nil)

	return err
}

// SendQuote sends a draft quote for approval: a LINK-mode quote gets a
// public URL (e-mailed when the customer has an e-mail on file); a
// MANUAL-mode quote is marked ready for RecordQuoteDecision.
func (c *Client) SendQuote(ctx context.Context, garageID, quoteID string) (*quotev1.SendQuoteResponse, error) {
	return call[quotev1.SendQuoteResponse](ctx, c, http.MethodPost,
		garagePath(garageID)+"/quotes/"+url.PathEscape(quoteID)+"/sending",
		&quotev1.SendQuoteRequest{GarageId: garageID, QuoteId: quoteID})
}

// RecordQuoteDecision records the customer's decision line by line (e.g.
// decided by phone); MANUAL-mode quotes are decided this way.
func (c *Client) RecordQuoteDecision(ctx context.Context, garageID, quoteID string, decisions ...*quotev1.QuoteLineDecisionInput) (*quotev1.Quote, error) {
	return call[quotev1.Quote](ctx, c, http.MethodPost,
		garagePath(garageID)+"/quotes/"+url.PathEscape(quoteID)+"/decision",
		&quotev1.RecordQuoteDecisionRequest{GarageId: garageID, QuoteId: quoteID, Decisions: decisions})
}

// ConvertQuote converts an approved quote into a repair order with only
// the approved lines; it returns the updated quote and the new order's id.
func (c *Client) ConvertQuote(ctx context.Context, garageID, quoteID string) (*quotev1.ConvertQuoteResponse, error) {
	return call[quotev1.ConvertQuoteResponse](ctx, c, http.MethodPost,
		garagePath(garageID)+"/quotes/"+url.PathEscape(quoteID)+"/conversion",
		&quotev1.ConvertQuoteRequest{GarageId: garageID, QuoteId: quoteID})
}

// GetPublicQuote opens a quote's public approval page without signing in.
// Accepts either a full public URL or just the token.
func (c *Client) GetPublicQuote(ctx context.Context, tokenOrURL string) (*quotev1.PublicQuote, error) {
	token := tokenOrURL
	if strings.HasPrefix(tokenOrURL, "http") {
		// Extract token from full URL: .../public/quotes/{token}
		// URL format: https://host/public/quotes/{token} or https://host/public/quotes/{token}/
		const prefix = "/public/quotes/"

		_, after, found := strings.Cut(tokenOrURL, prefix)
		if found {
			token = after
			// Remove trailing slash and query parameters
			token = strings.TrimSuffix(token, "/")
			if qIdx := strings.Index(token, "?"); qIdx >= 0 {
				token = token[:qIdx]
			}
		}
	}

	resp, err := call[quotev1.PublicQuote](ctx, c, http.MethodGet,
		"/api/v1/public/quotes/"+url.PathEscape(token), nil)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// DecidePublicQuote submits the customer's line decisions through the
// public link, without signing in. Accepts either a full public URL or just the token.
func (c *Client) DecidePublicQuote(ctx context.Context, tokenOrURL string, decisions ...*quotev1.QuoteLineDecisionInput) (*quotev1.PublicQuote, error) {
	token := tokenOrURL
	if strings.HasPrefix(tokenOrURL, "http") {
		const prefix = "/public/quotes/"

		_, after, found := strings.Cut(tokenOrURL, prefix)
		if found {
			token = after

			token = strings.TrimSuffix(token, "/")
			if qIdx := strings.Index(token, "?"); qIdx >= 0 {
				token = token[:qIdx]
			}
		}
	}

	resp, err := call[quotev1.PublicQuote](ctx, c, http.MethodPost,
		"/api/v1/public/quotes/"+url.PathEscape(token)+"/decision",
		&quotev1.DecidePublicQuoteRequest{Token: token, Decisions: decisions})
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// ---- Repair orders ---------------------------------------------------------

// ListRepairOrders returns garageID's repair orders.
func (c *Client) ListRepairOrders(ctx context.Context, garageID string) ([]*repairorderv1.RepairOrder, error) {
	resp, err := call[repairorderv1.ListRepairOrdersResponse](ctx, c, http.MethodGet, garagePath(garageID)+"/repair-orders", nil)

	return resp.GetData(), err
}

// ListRepairOrdersByTechnician returns garageID's repair orders with a
// line assigned to technicianID.
func (c *Client) ListRepairOrdersByTechnician(ctx context.Context, garageID, technicianID string) ([]*repairorderv1.RepairOrder, error) {
	path := garagePath(garageID) + "/repair-orders"
	if technicianID != "" {
		path += "?technicianId=" + url.QueryEscape(technicianID)
	}

	resp, err := call[repairorderv1.ListRepairOrdersResponse](ctx, c, http.MethodGet, path, nil)

	return resp.GetData(), err
}

// GetRepairOrder returns one repair order of garageID.
func (c *Client) GetRepairOrder(ctx context.Context, garageID, orderID string) (*repairorderv1.RepairOrder, error) {
	return call[repairorderv1.RepairOrder](ctx, c, http.MethodGet, repairOrderPath(garageID, orderID), nil)
}

// UpdateRepairOrderLineMasked changes only the masked fields of a line
// ("status", "assigned_technician_ids"), leaving the other field
// untouched; it returns the order.
func (c *Client) UpdateRepairOrderLineMasked(
	ctx context.Context, garageID, orderID, lineID string, line *repairorderv1.RepairOrderLine, paths ...string,
) (*repairorderv1.RepairOrder, error) {
	path := repairOrderPath(garageID, orderID) + "/lines/" + url.PathEscape(lineID)
	if len(paths) > 0 {
		path += "?updateMask=" + url.QueryEscape(strings.Join(paths, ","))
	}

	return call[repairorderv1.RepairOrder](ctx, c, http.MethodPatch, path, line)
}

// ---- Inventory -------------------------------------------------------------

// ListStockLevels returns garageID's parts with their stock on hand.
func (c *Client) ListStockLevels(ctx context.Context, garageID string) ([]*inventoryv1.StockLevel, error) {
	return c.SearchStockLevels(ctx, garageID, url.Values{})
}

// SearchStockLevels returns garageID's stock levels filtered by query
// parameters (query, categoryId, brand, lowStock, includeInactive, page,
// pageSize).
func (c *Client) SearchStockLevels(ctx context.Context, garageID string, query url.Values) ([]*inventoryv1.StockLevel, error) {
	path := garagePath(garageID) + "/inventory-items"
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}

	resp, err := call[inventoryv1.ListStockLevelsResponse](ctx, c, http.MethodGet, path, nil)

	return resp.GetData(), err
}

// GetStockLevel returns one part's stock on hand.
func (c *Client) GetStockLevel(ctx context.Context, garageID, partID string) (*inventoryv1.StockLevel, error) {
	return call[inventoryv1.StockLevel](ctx, c, http.MethodGet,
		garagePath(garageID)+"/inventory-items/"+url.PathEscape(partID), nil)
}

// CreateStockReceipt records a goods receipt against a part, increasing
// stock on hand. quantity is typed in unitID (empty: the base unit) and
// unitCost is per that unit; unitCost 0 is "not recorded".
func (c *Client) CreateStockReceipt(
	ctx context.Context, garageID, partID, quantity, unitID string, unitCost int64,
) (*inventoryv1.CreateStockReceiptResponse, error) {
	req := &inventoryv1.CreateStockReceiptRequest{GarageId: garageID, ItemId: partID, Quantity: quantity, UnitCost: unitCost}
	if unitID != "" {
		req.UnitId = &unitID
	}

	return call[inventoryv1.CreateStockReceiptResponse](ctx, c, http.MethodPost, garagePath(garageID)+"/stock-receipts", req)
}

// ListStockTransactions returns garageID's stock movements, newest
// first.
func (c *Client) ListStockTransactions(ctx context.Context, garageID string) ([]*inventoryv1.StockTransaction, error) {
	resp, err := call[inventoryv1.ListStockTransactionsResponse](ctx, c, http.MethodGet, garagePath(garageID)+"/reports/stock-movements", nil)

	return resp.GetData(), err
}

// ListStockTransactionsByType returns garageID's stock movements of one
// direction, newest first.
func (c *Client) ListStockTransactionsByType(ctx context.Context, garageID string, transactionType inventoryv1.StockTransactionType) ([]*inventoryv1.StockTransaction, error) {
	path := garagePath(garageID) + "/reports/stock-movements"
	if transactionType != inventoryv1.StockTransactionType_STOCK_TRANSACTION_TYPE_UNSPECIFIED {
		path += "?type=" + url.QueryEscape(strconv.Itoa(int(transactionType.Number())))
	}

	resp, err := call[inventoryv1.ListStockTransactionsResponse](ctx, c, http.MethodGet, path, nil)

	return resp.GetData(), err
}

// ---- Settlements -----------------------------------------------------------

// ListSettlements returns garageID's settlements.
func (c *Client) ListSettlements(ctx context.Context, garageID string) ([]*billingv1.Settlement, error) {
	resp, err := call[billingv1.ListSettlementsResponse](ctx, c, http.MethodGet, garagePath(garageID)+"/settlements", nil)

	return resp.GetData(), err
}

// ListOutstandingSettlements returns garageID's receivables view:
// settlements not yet fully paid.
func (c *Client) ListOutstandingSettlements(ctx context.Context, garageID string) ([]*billingv1.Settlement, error) {
	path := garagePath(garageID) + "/settlements?outstandingOnly=true"

	resp, err := call[billingv1.ListSettlementsResponse](ctx, c, http.MethodGet, path, nil)

	return resp.GetData(), err
}

// GetSettlement returns one settlement with its payments.
func (c *Client) GetSettlement(ctx context.Context, garageID, settlementID string) (*billingv1.Settlement, error) {
	resp, err := call[billingv1.Settlement](ctx, c, http.MethodGet,
		garagePath(garageID)+"/settlements/"+url.PathEscape(settlementID), nil)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// UpdateSettlement changes who a settlement is billed to and whether a
// VAT invoice is requested (mask paths "billing_party",
// "invoice_requested").
func (c *Client) UpdateSettlement(ctx context.Context, garageID, settlementID string, settlement *billingv1.Settlement, paths ...string) (*billingv1.Settlement, error) {
	path := garagePath(garageID) + "/settlements/" + url.PathEscape(settlementID)
	if len(paths) > 0 {
		path += "?updateMask=" + url.QueryEscape(strings.Join(paths, ","))
	}

	return call[billingv1.Settlement](ctx, c, http.MethodPatch, path, settlement)
}

// VoidSettlementPayment voids a payment with a reason; the voucher stays
// on file marked void and the outstanding amount is restored.
func (c *Client) VoidSettlementPayment(ctx context.Context, garageID, settlementID, paymentID, reason string) (*billingv1.Settlement, error) {
	return call[billingv1.Settlement](ctx, c, http.MethodPost,
		garagePath(garageID)+"/settlements/"+url.PathEscape(settlementID)+"/payments/"+url.PathEscape(paymentID)+":void",
		&billingv1.VoidSettlementPaymentRequest{GarageId: garageID, SettlementId: settlementID, PaymentId: paymentID, Reason: reason})
}

// ---- Reporting & documents -------------------------------------------------

// GetRevenueReport returns garageID's revenue buckets for the inclusive
// from date ("YYYY-MM-DD") to the exclusive to date.
func (c *Client) GetRevenueReport(ctx context.Context, garageID string, period reportingv1.RevenuePeriod, from, to string) (*reportingv1.GetRevenueReportResponse, error) {
	query := url.Values{}
	if period != reportingv1.RevenuePeriod_REVENUE_PERIOD_UNSPECIFIED {
		query.Set("period", strconv.Itoa(int(period.Number())))
	}

	if from != "" {
		query.Set("from", from)
	}

	if to != "" {
		query.Set("to", to)
	}

	path := garagePath(garageID) + "/reports/revenue"
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}

	return call[reportingv1.GetRevenueReportResponse](ctx, c, http.MethodGet, path, nil)
}

// DownloadQuotePDF downloads a quote's PDF.
func (c *Client) DownloadQuotePDF(ctx context.Context, garageID, quoteID string) (string, []byte, error) {
	return c.DownloadRaw(ctx, http.MethodGet, garagePath(garageID)+"/quotes/"+url.PathEscape(quoteID)+"/pdf")
}

// DownloadRepairOrderPDF downloads a repair order's PDF.
func (c *Client) DownloadRepairOrderPDF(ctx context.Context, garageID, orderID string) (string, []byte, error) {
	return c.DownloadRaw(ctx, http.MethodGet, repairOrderPath(garageID, orderID)+"/pdf")
}

// DownloadReceiptPDF downloads a settlement's receipt PDF.
func (c *Client) DownloadReceiptPDF(ctx context.Context, garageID, settlementID string) (string, []byte, error) {
	return c.DownloadRaw(ctx, http.MethodGet, garagePath(garageID)+"/settlements/"+url.PathEscape(settlementID)+"/receipt-pdf")
}

// DownloadVoucherPDF downloads one payment voucher's PDF.
func (c *Client) DownloadVoucherPDF(ctx context.Context, garageID, settlementID, paymentID string) (string, []byte, error) {
	return c.DownloadRaw(ctx, http.MethodGet,
		garagePath(garageID)+"/settlements/"+url.PathEscape(settlementID)+"/payments/"+url.PathEscape(paymentID)+"/voucher-pdf")
}

// DownloadStockMovementsExcel downloads the stock-movement report as an
// Excel workbook.
func (c *Client) DownloadStockMovementsExcel(ctx context.Context, garageID string) (string, []byte, error) {
	return c.DownloadRaw(ctx, http.MethodGet, garagePath(garageID)+"/reports/stock-movements/excel")
}

// ---- Technician & customer BFFs --------------------------------------------

// ListMyWork returns the signed-in technician's open work orders across
// their garages.
func (c *Client) ListMyWork(ctx context.Context) ([]*technicianv1.WorkOrder, error) {
	resp, err := call[technicianv1.ListMyWorkResponse](ctx, c, http.MethodGet, "/api/v1/technician/work", nil)

	return resp.GetData(), err
}

// ListMyVehicles returns the signed-in person's vehicles, grouped by
// garage (customer portal).
func (c *Client) ListMyVehicles(ctx context.Context) ([]*customerportalv1.MyGarage, error) {
	resp, err := call[customerportalv1.ListMyVehiclesResponse](ctx, c, http.MethodGet, "/api/v1/customer/vehicles", nil)

	return resp.GetData(), err
}

// ListMyGarageLinks returns the signed-in person's garage link requests
// and linked garages (customer portal).
func (c *Client) ListMyGarageLinks(ctx context.Context) ([]*customerportalv1.GarageLink, error) {
	resp, err := call[customerportalv1.ListMyGarageLinksResponse](ctx, c, http.MethodGet, "/api/v1/me/garage-links", nil)

	return resp.GetData(), err
}

// AcceptGarageLink accepts a garage's link request (customer portal).
func (c *Client) AcceptGarageLink(ctx context.Context, linkID string) (*customerportalv1.GarageLink, error) {
	resp, err := call[customerportalv1.GarageLink](ctx, c, http.MethodPost,
		"/api/v1/me/garage-links/"+url.PathEscape(linkID)+":accept",
		&customerportalv1.AcceptGarageLinkRequest{LinkId: linkID})
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// DeclineGarageLink declines a garage's link request (customer portal).
func (c *Client) DeclineGarageLink(ctx context.Context, linkID string) (*customerportalv1.GarageLink, error) {
	resp, err := call[customerportalv1.GarageLink](ctx, c, http.MethodPost,
		"/api/v1/me/garage-links/"+url.PathEscape(linkID)+":decline",
		&customerportalv1.DeclineGarageLinkRequest{LinkId: linkID})
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// UnlinkGarageLink ends a garage link from the user's side (customer
// portal).
func (c *Client) UnlinkGarageLink(ctx context.Context, linkID string) (*customerportalv1.GarageLink, error) {
	resp, err := call[customerportalv1.GarageLink](ctx, c, http.MethodPost,
		"/api/v1/me/garage-links/"+url.PathEscape(linkID)+":unlink",
		&customerportalv1.UnlinkGarageLinkRequest{LinkId: linkID})
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// GetMyVehicleHistory returns the signed-in person's history of one
// vehicle at one garage (customer portal).
func (c *Client) GetMyVehicleHistory(ctx context.Context, garageID, vehicleID string) (*customerportalv1.GetMyVehicleHistoryResponse, error) {
	return call[customerportalv1.GetMyVehicleHistoryResponse](ctx, c, http.MethodGet,
		"/api/v1/customer/garages/"+url.PathEscape(garageID)+"/vehicles/"+url.PathEscape(vehicleID)+"/history", nil)
}

// ---- Tenant extras ---------------------------------------------------------

// GetMyInvitation returns one of the signed-in user's invitations.
func (c *Client) GetMyInvitation(ctx context.Context, invitationID string) (*tenantv1.Invitation, error) {
	resp, err := call[tenantv1.Invitation](ctx, c, http.MethodGet,
		"/api/v1/me/invitations/"+url.PathEscape(invitationID), nil)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// ListPermissions returns the permission catalogue.
func (c *Client) ListPermissions(ctx context.Context) ([]*tenantv1.PermissionInfo, error) {
	resp, err := call[tenantv1.ListPermissionsResponse](ctx, c, http.MethodGet, "/api/v1/permissions", nil)

	return resp.GetData(), err
}

// GetRole returns one role.
func (c *Client) GetRole(ctx context.Context, roleID string) (*tenantv1.Role, error) {
	resp, err := call[tenantv1.Role](ctx, c, http.MethodGet, "/api/v1/roles/"+url.PathEscape(roleID), nil)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

var _ = commonv1.QuoteApprovalMode_QUOTE_APPROVAL_MODE_UNSPECIFIED

// DownloadRevenueExcel downloads the revenue report as an Excel workbook.
func (c *Client) DownloadRevenueExcel(ctx context.Context, garageID string, period reportingv1.RevenuePeriod, from, to string) (string, []byte, error) {
	query := url.Values{}
	query.Set("period", strconv.Itoa(int(period.Number())))
	query.Set("from", from)
	query.Set("to", to)

	path := garagePath(garageID) + "/reports/revenue/excel"
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}

	return c.DownloadRaw(ctx, http.MethodGet, path)
}
