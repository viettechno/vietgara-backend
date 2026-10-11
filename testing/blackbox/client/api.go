package client

import (
	"context"
	"net/http"
	"net/url"

	billingv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/billing/v1"
	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"
	identityv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/identity/v1"
	licensev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/license/v1"
	repairorderv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/repairorder/v1"
	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"
	"google.golang.org/protobuf/proto"
)

// The typed calls below follow the google.api.http mapping of each RPC in
// vietgara-proto: the request body is the RPC's `body` field and the
// response is its `response_body` field when one is set.

// ---- Identity ------------------------------------------------------------

// Register creates an e-mail/password user and e-mails the first
// verification OTP.
func (c *Client) Register(ctx context.Context, req *identityv1.RegisterRequest) (*identityv1.Session, error) {
	return call[identityv1.Session](ctx, c, http.MethodPost, "/api/v1/auth/register", req)
}

// Login signs in with e-mail and password.
func (c *Client) Login(ctx context.Context, email, password string) (*identityv1.Session, error) {
	return call[identityv1.Session](ctx, c, http.MethodPost, "/api/v1/auth/login",
		&identityv1.LoginRequest{Email: email, Password: password})
}

// VerifyEmail confirms the e-mail OTP and rotates refreshToken into a
// session that carries the verified claim.
func (c *Client) VerifyEmail(ctx context.Context, otp, refreshToken string) (*identityv1.Session, error) {
	return call[identityv1.Session](ctx, c, http.MethodPost, "/api/v1/auth/email-verification",
		&identityv1.VerifyEmailRequest{OtpCode: otp, RefreshToken: refreshToken})
}

// Refresh rotates refreshToken.
func (c *Client) Refresh(ctx context.Context, refreshToken string) (*identityv1.Session, error) {
	return call[identityv1.Session](ctx, c, http.MethodPost, "/api/v1/auth/refresh",
		&identityv1.RefreshTokenRequest{RefreshToken: refreshToken})
}

// SignInWithGoogleIDToken signs in (or up) with an ID token from native
// Google Sign-In, as the mobile apps do.
func (c *Client) SignInWithGoogleIDToken(ctx context.Context, idToken string) (*identityv1.Session, error) {
	return call[identityv1.Session](ctx, c, http.MethodPost, "/api/v1/auth/google/id-token",
		&identityv1.SignInWithGoogleIdTokenRequest{IdToken: idToken})
}

// Me returns the signed-in user.
func (c *Client) Me(ctx context.Context) (*identityv1.User, error) {
	return call[identityv1.User](ctx, c, http.MethodGet, "/api/v1/me", nil)
}

// ---- Tenant ----------------------------------------------------------------

// CreateGarage creates a garage owned by the signed-in user.
func (c *Client) CreateGarage(ctx context.Context, garage *tenantv1.Garage) (*tenantv1.Garage, error) {
	return call[tenantv1.Garage](ctx, c, http.MethodPost, "/api/v1/garages", garage)
}

// GetGarage returns one garage.
func (c *Client) GetGarage(ctx context.Context, garageID string) (*tenantv1.Garage, error) {
	return call[tenantv1.Garage](ctx, c, http.MethodGet, "/api/v1/garages/"+url.PathEscape(garageID), nil)
}

// ListGarages returns the garages the user owns or works at.
func (c *Client) ListGarages(ctx context.Context) ([]*tenantv1.Garage, error) {
	resp, err := call[tenantv1.ListGaragesResponse](ctx, c, http.MethodGet, "/api/v1/garages", nil)

	return resp.GetData(), err
}

// ---- License ---------------------------------------------------------------

// MySubscription returns the signed-in owner's subscription and usage.
func (c *Client) MySubscription(ctx context.Context) (*licensev1.Subscription, error) {
	return call[licensev1.Subscription](ctx, c, http.MethodGet, "/api/v1/me/subscription", nil)
}

// ListPlans returns the public plans available to the signed-in owner.
func (c *Client) ListPlans(ctx context.Context) ([]*licensev1.Plan, error) {
	resp, err := call[licensev1.ListPlansResponse](ctx, c, http.MethodGet, "/api/v1/plans", nil)

	return resp.GetData(), err
}

// ChangeMyPlan changes the signed-in owner's subscription to planID.
func (c *Client) ChangeMyPlan(ctx context.Context, planID string) (*licensev1.Subscription, error) {
	return call[licensev1.Subscription](ctx, c, http.MethodPost, "/api/v1/me/subscription/plan-changes",
		&licensev1.ChangeMyPlanRequest{PlanId: planID})
}

// ---- Customer --------------------------------------------------------------

// CreateCustomer adds a customer to garageID.
func (c *Client) CreateCustomer(ctx context.Context, garageID string, customer *customerv1.Customer) (*customerv1.Customer, error) {
	return call[customerv1.Customer](ctx, c, http.MethodPost, garagePath(garageID)+"/customers", customer)
}

// GetCustomer returns one customer of garageID.
func (c *Client) GetCustomer(ctx context.Context, garageID, customerID string) (*customerv1.Customer, error) {
	return call[customerv1.Customer](ctx, c, http.MethodGet, garagePath(garageID)+"/customers/"+url.PathEscape(customerID), nil)
}

// ListCustomers returns the first page of garageID's customers.
func (c *Client) ListCustomers(ctx context.Context, garageID string) ([]*customerv1.Customer, error) {
	resp, err := call[customerv1.ListCustomersResponse](ctx, c, http.MethodGet, garagePath(garageID)+"/customers", nil)

	return resp.GetData(), err
}

// CreateVehicle registers a vehicle in garageID.
func (c *Client) CreateVehicle(ctx context.Context, garageID string, vehicle *customerv1.Vehicle) (*customerv1.Vehicle, error) {
	return call[customerv1.Vehicle](ctx, c, http.MethodPost, garagePath(garageID)+"/vehicles", vehicle)
}

// ---- Repair Order ----------------------------------------------------------

// CreateRepairOrder creates an ad-hoc repair order in garageID.
func (c *Client) CreateRepairOrder(ctx context.Context, garageID string, order *repairorderv1.RepairOrder) (*repairorderv1.RepairOrder, error) {
	return call[repairorderv1.RepairOrder](ctx, c, http.MethodPost, garagePath(garageID)+"/repair-orders", order)
}

// UpdateRepairOrderLine changes the fields set on line; returns the order.
func (c *Client) UpdateRepairOrderLine(
	ctx context.Context, garageID, orderID, lineID string, line *repairorderv1.RepairOrderLine,
) (*repairorderv1.RepairOrder, error) {
	return call[repairorderv1.RepairOrder](ctx, c, http.MethodPatch,
		repairOrderPath(garageID, orderID)+"/lines/"+url.PathEscape(lineID), line)
}

// CloseRepairOrder closes a repair order whose lines are all completed.
func (c *Client) CloseRepairOrder(ctx context.Context, garageID, orderID string) (*repairorderv1.RepairOrder, error) {
	return call[repairorderv1.RepairOrder](ctx, c, http.MethodPost, repairOrderPath(garageID, orderID)+"/closing",
		&repairorderv1.CloseRepairOrderRequest{})
}

func repairOrderPath(garageID, orderID string) string {
	return garagePath(garageID) + "/repair-orders/" + url.PathEscape(orderID)
}

// ---- Billing & Settlement --------------------------------------------------

// CreateSettlement settles a closed repair order. The API requires an
// Idempotency-Key (WithIdempotencyKey).
func (c *Client) CreateSettlement(ctx context.Context, garageID, repairOrderID string) (*billingv1.Settlement, error) {
	return call[billingv1.Settlement](ctx, c, http.MethodPost, garagePath(garageID)+"/settlements",
		&billingv1.CreateSettlementRequest{RepairOrderId: repairOrderID})
}

// RecordPayment records a payment against a settlement. The API requires
// an Idempotency-Key (WithIdempotencyKey).
func (c *Client) RecordPayment(
	ctx context.Context, garageID, settlementID string, payment *billingv1.RecordSettlementPaymentRequest,
) (*billingv1.Settlement, error) {
	return call[billingv1.Settlement](ctx, c, http.MethodPost,
		garagePath(garageID)+"/settlements/"+url.PathEscape(settlementID)+"/payments", payment)
}

func garagePath(garageID string) string { return "/api/v1/garages/" + url.PathEscape(garageID) }

// call decodes the response into a new T.
func call[T any, PT interface {
	*T
	proto.Message
}](ctx context.Context, c *Client, method, path string, in proto.Message,
) (PT, error) {
	out := PT(new(T))
	if err := c.Do(ctx, method, path, in, out); err != nil {
		return nil, err
	}

	return out, nil
}
