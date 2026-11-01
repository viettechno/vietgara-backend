package client

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	commonv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/common/v1"
	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"
	licensev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/license/v1"
	supplierv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/supplier/v1"
)

// The methods below serve Releases 5.0 to 5.2: tax profiles, the link
// confirmation, delete-or-deactivate with its outcome and preview, and the
// account credit of a plan change.

func taxProfilesPath(garageID, customerID string) string {
	return garagePath(garageID) + "/customers/" + url.PathEscape(customerID) + "/tax-profiles"
}

// CreateTaxProfile adds a tax profile to a customer.
func (c *Client) CreateTaxProfile(ctx context.Context, garageID, customerID string, profile *customerv1.CustomerTaxProfile) (*customerv1.CustomerTaxProfile, error) {
	return call[customerv1.CustomerTaxProfile](ctx, c, http.MethodPost, taxProfilesPath(garageID, customerID), profile)
}

// ListTaxProfiles returns a customer's tax profiles, the default first.
func (c *Client) ListTaxProfiles(ctx context.Context, garageID, customerID string, includeInactive bool) ([]*customerv1.CustomerTaxProfile, error) {
	path := taxProfilesPath(garageID, customerID)
	if includeInactive {
		path += "?includeInactive=true"
	}

	resp, err := call[customerv1.ListCustomerTaxProfilesResponse](ctx, c, http.MethodGet, path, nil)

	return resp.GetData(), err
}

// UpdateTaxProfile changes the fields named by paths.
func (c *Client) UpdateTaxProfile(ctx context.Context, garageID, customerID, profileID string, profile *customerv1.CustomerTaxProfile, paths ...string) (*customerv1.CustomerTaxProfile, error) {
	path := taxProfilesPath(garageID, customerID) + "/" + url.PathEscape(profileID)
	if len(paths) > 0 {
		path += "?updateMask=" + url.QueryEscape(strings.Join(paths, ","))
	}

	return call[customerv1.CustomerTaxProfile](ctx, c, http.MethodPatch, path, profile)
}

// DeleteTaxProfile deletes or deactivates a tax profile; dryRun previews.
func (c *Client) DeleteTaxProfile(ctx context.Context, garageID, customerID, profileID string, dryRun bool) (commonv1.DeleteOutcome, error) {
	resp, err := call[customerv1.DeleteCustomerTaxProfileResponse](ctx, c, http.MethodDelete,
		taxProfilesPath(garageID, customerID)+"/"+url.PathEscape(profileID)+dryRunSuffix(dryRun), nil)

	return resp.GetOutcome(), err
}

// ActivateTaxProfile reactivates a deactivated tax profile.
func (c *Client) ActivateTaxProfile(ctx context.Context, garageID, customerID, profileID string) (*customerv1.CustomerTaxProfile, error) {
	return call[customerv1.CustomerTaxProfile](ctx, c, http.MethodPost,
		taxProfilesPath(garageID, customerID)+"/"+url.PathEscape(profileID)+":activate",
		&customerv1.ActivateCustomerTaxProfileRequest{GarageId: garageID, CustomerId: customerID, TaxProfileId: profileID})
}

// dryRunSuffix is the query that makes a delete only report its outcome.
func dryRunSuffix(dryRun bool) string {
	if dryRun {
		return dryRunQuery
	}

	return ""
}

// DeleteCustomerOutcome is DeleteCustomer with the answer and the preview.
func (c *Client) DeleteCustomerOutcome(ctx context.Context, garageID, customerID string, dryRun bool) (commonv1.DeleteOutcome, error) {
	resp, err := call[customerv1.DeleteCustomerResponse](ctx, c, http.MethodDelete,
		garagePath(garageID)+"/customers/"+url.PathEscape(customerID)+dryRunSuffix(dryRun), nil)

	return resp.GetOutcome(), err
}

// DeleteVehicleOutcome is DeleteVehicle with the answer and the preview.
func (c *Client) DeleteVehicleOutcome(ctx context.Context, garageID, vehicleID string, dryRun bool) (commonv1.DeleteOutcome, error) {
	resp, err := call[customerv1.DeleteVehicleResponse](ctx, c, http.MethodDelete,
		garagePath(garageID)+"/vehicles/"+url.PathEscape(vehicleID)+dryRunSuffix(dryRun), nil)

	return resp.GetOutcome(), err
}

// DeleteSupplierOutcome is DeleteSupplier with the answer and the preview.
func (c *Client) DeleteSupplierOutcome(ctx context.Context, garageID, supplierID string, dryRun bool) (commonv1.DeleteOutcome, error) {
	resp, err := call[supplierv1.DeleteSupplierResponse](ctx, c, http.MethodDelete,
		garagePath(garageID)+"/suppliers/"+url.PathEscape(supplierID)+dryRunSuffix(dryRun), nil)

	return resp.GetOutcome(), err
}

// ConfirmCustomerUserLink confirms the waiting link request with the code
// the user read out.
func (c *Client) ConfirmCustomerUserLink(ctx context.Context, garageID, customerID, otp string) (*customerv1.CustomerUserLink, error) {
	return call[customerv1.CustomerUserLink](ctx, c, http.MethodPost,
		garagePath(garageID)+"/customers/"+url.PathEscape(customerID)+"/user-links/current:confirm",
		&customerv1.ConfirmCustomerUserLinkRequest{GarageId: garageID, CustomerId: customerID, Otp: otp})
}

// CreditLedger returns the signed-in owner's account credit entries.
func (c *Client) CreditLedger(ctx context.Context) ([]*licensev1.CreditLedgerEntry, error) {
	resp, err := call[licensev1.ListMyCreditLedgerResponse](ctx, c, http.MethodGet, "/api/v1/me/subscription/credit-ledger", nil)

	return resp.GetData(), err
}

// MyPaymentRequests lists the signed-in owner's payment requests.
func (c *Client) MyPaymentRequests(ctx context.Context) ([]*licensev1.PlanPaymentRequest, error) {
	resp, err := call[licensev1.ListMyPlanPaymentRequestsResponse](ctx, c, http.MethodGet, "/api/v1/me/subscription/payment-requests", nil)

	return resp.GetData(), err
}

// MyPaymentRequest reads one of the signed-in owner's payment requests.
func (c *Client) MyPaymentRequest(ctx context.Context, requestID string) (*licensev1.PlanPaymentRequest, error) {
	return call[licensev1.PlanPaymentRequest](ctx, c, http.MethodGet,
		"/api/v1/me/subscription/payment-requests/"+url.PathEscape(requestID), nil)
}

// AdjustCredit corrects a subscription's account credit. Platform admin only.
func (c *Client) AdjustCredit(ctx context.Context, subscriptionID string, amount int64, note string) (*licensev1.CreditLedgerEntry, error) {
	return call[licensev1.CreditLedgerEntry](ctx, c, http.MethodPost,
		"/api/v1/admin/subscriptions/"+url.PathEscape(subscriptionID)+"/credit-adjustments",
		&licensev1.AdjustCreditRequest{SubscriptionId: subscriptionID, Amount: amount, Note: note})
}

// PlatformPaymentSettings reads the platform's payment details. Platform
// admin only.
func (c *Client) PlatformPaymentSettings(ctx context.Context) (*licensev1.PlatformPaymentSettings, error) {
	return call[licensev1.PlatformPaymentSettings](ctx, c, http.MethodGet, "/api/v1/admin/payment-settings", nil)
}

// UpdatePlatformPaymentSettings changes the fields named by paths. Platform
// admin only.
func (c *Client) UpdatePlatformPaymentSettings(ctx context.Context, settings *licensev1.PlatformPaymentSettings, paths ...string) (*licensev1.PlatformPaymentSettings, error) {
	path := "/api/v1/admin/payment-settings"
	if len(paths) > 0 {
		path += "?updateMask=" + url.QueryEscape(strings.Join(paths, ","))
	}

	return call[licensev1.PlatformPaymentSettings](ctx, c, http.MethodPatch, path, settings)
}
