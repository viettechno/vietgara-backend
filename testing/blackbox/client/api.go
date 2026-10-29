package client

import (
	"context"
	"net/http"
	"net/url"
	"strings"

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

// LinkGoogle links the Google account of an ID token to the signed-in user,
// who confirms with the password (FR-IAM-06, as the mobile apps do).
func (c *Client) LinkGoogle(ctx context.Context, idToken, password string) (*identityv1.User, error) {
	return call[identityv1.User](ctx, c, http.MethodPost, "/api/v1/me/google-link",
		&identityv1.LinkGoogleRequest{IdToken: idToken, Password: password})
}

// UnlinkGoogle removes the linked Google account and answers a new session.
func (c *Client) UnlinkGoogle(ctx context.Context, password string) (*identityv1.Session, error) {
	return call[identityv1.Session](ctx, c, http.MethodPost, "/api/v1/me/google-link:unlink",
		&identityv1.UnlinkGoogleRequest{Password: password})
}

// CompleteGoogleLink links the Google account of a popup proof, as the web
// consoles do.
func (c *Client) CompleteGoogleLink(ctx context.Context, linkProof, password string) (*identityv1.User, error) {
	return call[identityv1.User](ctx, c, http.MethodPost, "/api/v1/me/google-link:complete",
		&identityv1.CompleteGoogleLinkRequest{LinkProof: linkProof, Password: password})
}

// Me returns the signed-in user.
func (c *Client) Me(ctx context.Context) (*identityv1.User, error) {
	return call[identityv1.User](ctx, c, http.MethodGet, "/api/v1/me", nil)
}

// UpdateMe changes the signed-in user's full name, phone and/or locale.
// Paths are proto field names ("full_name", "phone", "locale"); unknown
// paths are refused. E-mail is not editable through this call.
func (c *Client) UpdateMe(ctx context.Context, user *identityv1.User, paths ...string) (*identityv1.User, error) {
	path := "/api/v1/me"
	if len(paths) > 0 {
		path += "?updateMask=" + url.QueryEscape(strings.Join(paths, ","))
	}

	return call[identityv1.User](ctx, c, http.MethodPatch, path, user)
}

// Logout revokes a refresh token. Revoking twice succeeds: revocation is
// idempotent.
func (c *Client) Logout(ctx context.Context, refreshToken string) error {
	_, err := call[identityv1.LogoutResponse](ctx, c, http.MethodPost, "/api/v1/auth/logout",
		&identityv1.LogoutRequest{RefreshToken: refreshToken})

	return err
}

// RequestPasswordReset starts a password reset: it always succeeds (so
// user existence is not revealed) and e-mails an OTP when the user exists.
func (c *Client) RequestPasswordReset(ctx context.Context, email string) (*identityv1.OtpChallenge, error) {
	return call[identityv1.OtpChallenge](ctx, c, http.MethodPost, "/api/v1/auth/password-reset/otp",
		&identityv1.RequestPasswordResetRequest{Email: email})
}

// VerifyPasswordResetOtp checks the reset OTP and returns a single-use
// reset token.
func (c *Client) VerifyPasswordResetOtp(ctx context.Context, email, otp string) (*identityv1.VerifyPasswordResetOtpResponse, error) {
	return call[identityv1.VerifyPasswordResetOtpResponse](ctx, c, http.MethodPost, "/api/v1/auth/password-reset/otp/verification",
		&identityv1.VerifyPasswordResetOtpRequest{Email: email, OtpCode: otp})
}

// ResetPassword sets a new password with the reset token and revokes
// every session of the user.
func (c *Client) ResetPassword(ctx context.Context, resetToken, newPassword string) error {
	_, err := call[identityv1.ResetPasswordResponse](ctx, c, http.MethodPost, "/api/v1/auth/password-reset",
		&identityv1.ResetPasswordRequest{ResetToken: resetToken, NewPassword: newPassword})

	return err
}

// SendVerificationOtp e-mails a fresh verification OTP to the signed-in
// (unverified) user. Within the 30-second cooldown it fails with
// OTP_RESEND_TOO_SOON.
func (c *Client) SendVerificationOtp(ctx context.Context) (*identityv1.OtpChallenge, error) {
	return call[identityv1.OtpChallenge](ctx, c, http.MethodPost, "/api/v1/auth/email-verification/otp",
		&identityv1.SendEmailVerificationOtpRequest{})
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

// UpdateGarage patches the garage's masked fields (mask paths are proto
// field names: "phone", "tax_code", "status", "operating_hours",
// "numbering", ...).
func (c *Client) UpdateGarage(ctx context.Context, garageID string, garage *tenantv1.Garage, paths ...string) (*tenantv1.Garage, error) {
	path := garagePath(garageID)
	if len(paths) > 0 {
		path += "?updateMask=" + url.QueryEscape(strings.Join(paths, ","))
	}

	return call[tenantv1.Garage](ctx, c, http.MethodPatch, path, garage)
}

// UploadGarageLogo replaces the garage logo; content is the raw image
// bytes and must be a PNG, JPEG or WebP image of at most 1 MB.
func (c *Client) UploadGarageLogo(ctx context.Context, garageID string, content []byte, contentType string) (*tenantv1.Garage, error) {
	return call[tenantv1.Garage](ctx, c, http.MethodPut, garagePath(garageID)+"/logo",
		&tenantv1.UploadGarageLogoRequest{GarageId: garageID, Content: content, ContentType: contentType})
}

// DeleteGarageLogo removes the garage logo.
func (c *Client) DeleteGarageLogo(ctx context.Context, garageID string) (*tenantv1.Garage, error) {
	return call[tenantv1.Garage](ctx, c, http.MethodDelete, garagePath(garageID)+"/logo", nil)
}

// ---- Invitations ------------------------------------------------------------

// LookupStaffCandidate finds an existing user by exact e-mail or phone to
// invite. Unknown users fail with USER_NOT_FOUND; the candidate carries
// its current membership (none, member, invited).
func (c *Client) LookupStaffCandidate(ctx context.Context, garageID, query string) (*tenantv1.StaffCandidate, error) {
	return call[tenantv1.StaffCandidate](ctx, c, http.MethodGet,
		garagePath(garageID)+"/staff-candidates?query="+url.QueryEscape(query), nil)
}

// CreateInvitation invites userID to garageID with the given groups. The
// invitation starts pending and expires in 7 days.
func (c *Client) CreateInvitation(ctx context.Context, garageID, userID string, groupIDs []string) (*tenantv1.Invitation, error) {
	return call[tenantv1.Invitation](ctx, c, http.MethodPost, garagePath(garageID)+"/invitations",
		&tenantv1.CreateInvitationRequest{GarageId: garageID, UserId: userID, GroupIds: groupIDs})
}

// ListInvitations returns the garage's invitations.
func (c *Client) ListInvitations(ctx context.Context, garageID string) ([]*tenantv1.Invitation, error) {
	resp, err := call[tenantv1.ListInvitationsResponse](ctx, c, http.MethodGet, garagePath(garageID)+"/invitations", nil)

	return resp.GetData(), err
}

// RevokeInvitation revokes a pending invitation; accepting it afterwards
// fails with INVITATION_NOT_PENDING.
func (c *Client) RevokeInvitation(ctx context.Context, garageID, invitationID string) (*tenantv1.Invitation, error) {
	return call[tenantv1.Invitation](ctx, c, http.MethodPost,
		garagePath(garageID)+"/invitations/"+url.PathEscape(invitationID)+"/revoke",
		&tenantv1.RevokeInvitationRequest{GarageId: garageID, InvitationId: invitationID})
}

// ListMyInvitations returns the signed-in user's pending invitations.
func (c *Client) ListMyInvitations(ctx context.Context) ([]*tenantv1.Invitation, error) {
	resp, err := call[tenantv1.ListMyInvitationsResponse](ctx, c, http.MethodGet, "/api/v1/me/invitations", nil)

	return resp.GetData(), err
}

// AcceptInvitation accepts the signed-in user's invitation and joins the
// garage with its groups.
func (c *Client) AcceptInvitation(ctx context.Context, invitationID string) (*tenantv1.Invitation, error) {
	return call[tenantv1.Invitation](ctx, c, http.MethodPost,
		"/api/v1/me/invitations/"+url.PathEscape(invitationID)+"/accept",
		&tenantv1.AcceptInvitationRequest{InvitationId: invitationID})
}

// DeclineInvitation declines the signed-in user's invitation.
func (c *Client) DeclineInvitation(ctx context.Context, invitationID string) (*tenantv1.Invitation, error) {
	return call[tenantv1.Invitation](ctx, c, http.MethodPost,
		"/api/v1/me/invitations/"+url.PathEscape(invitationID)+"/decline",
		&tenantv1.DeclineInvitationRequest{InvitationId: invitationID})
}

// ---- Staff ------------------------------------------------------------------

// ListStaff returns the garage's members, owners first.
func (c *Client) ListStaff(ctx context.Context, garageID string) ([]*tenantv1.Staff, error) {
	resp, err := call[tenantv1.ListStaffResponse](ctx, c, http.MethodGet, garagePath(garageID)+"/staff", nil)

	return resp.GetData(), err
}

// UpdateStaff changes a member's status or groups (mask paths "status",
// "group_ids"). The owner's own row is protected.
func (c *Client) UpdateStaff(ctx context.Context, garageID, staffID string, staff *tenantv1.Staff, paths ...string) (*tenantv1.Staff, error) {
	path := garagePath(garageID) + "/staff/" + url.PathEscape(staffID)
	if len(paths) > 0 {
		path += "?updateMask=" + url.QueryEscape(strings.Join(paths, ","))
	}

	return call[tenantv1.Staff](ctx, c, http.MethodPatch, path, staff)
}

// DeleteStaff removes a member from the garage at once.
func (c *Client) DeleteStaff(ctx context.Context, garageID, staffID string) error {
	_, err := call[tenantv1.DeleteStaffResponse](ctx, c, http.MethodDelete,
		garagePath(garageID)+"/staff/"+url.PathEscape(staffID), nil)

	return err
}

// ---- Roles and groups -------------------------------------------------------

// ListRoles returns the owner's roles, defaults first.
func (c *Client) ListRoles(ctx context.Context) ([]*tenantv1.Role, error) {
	resp, err := call[tenantv1.ListRolesResponse](ctx, c, http.MethodGet, "/api/v1/roles", nil)

	return resp.GetData(), err
}

// CreateRole creates a custom role; write permissions imply their reads.
func (c *Client) CreateRole(ctx context.Context, role *tenantv1.Role) (*tenantv1.Role, error) {
	return call[tenantv1.Role](ctx, c, http.MethodPost, "/api/v1/roles", role)
}

// UpdateRole saves a role's name, description and permissions (paths name
// the fields to change). Default roles keep their key and may be edited
// and reset to their default permissions; write permissions imply reads.
func (c *Client) UpdateRole(ctx context.Context, roleID string, role *tenantv1.Role, paths ...string) (*tenantv1.Role, error) {
	path := "/api/v1/roles/" + url.PathEscape(roleID)
	if len(paths) > 0 {
		path += "?updateMask=" + url.QueryEscape(strings.Join(paths, ","))
	}

	return call[tenantv1.Role](ctx, c, http.MethodPatch, path, role)
}

// DeleteRole deletes a custom role no group uses; in-use roles fail with
// ROLE_IN_USE.
func (c *Client) DeleteRole(ctx context.Context, roleID string) error {
	_, err := call[tenantv1.DeleteRoleResponse](ctx, c, http.MethodDelete,
		"/api/v1/roles/"+url.PathEscape(roleID), nil)

	return err
}

// ListStaffGroups returns the garage's groups.
func (c *Client) ListStaffGroups(ctx context.Context, garageID string) ([]*tenantv1.StaffGroup, error) {
	resp, err := call[tenantv1.ListStaffGroupsResponse](ctx, c, http.MethodGet, garagePath(garageID)+"/staff-groups", nil)

	return resp.GetData(), err
}

// CreateStaffGroup creates a group in the garage with an existing role.
func (c *Client) CreateStaffGroup(ctx context.Context, garageID string, group *tenantv1.StaffGroup) (*tenantv1.StaffGroup, error) {
	return call[tenantv1.StaffGroup](ctx, c, http.MethodPost, garagePath(garageID)+"/staff-groups", group)
}

// UpdateStaffGroup changes a group's name, description or role (mask
// paths "name", "description", "role_id").
func (c *Client) UpdateStaffGroup(ctx context.Context, garageID, groupID string, group *tenantv1.StaffGroup, paths ...string) (*tenantv1.StaffGroup, error) {
	path := garagePath(garageID) + "/staff-groups/" + url.PathEscape(groupID)
	if len(paths) > 0 {
		path += "?updateMask=" + url.QueryEscape(strings.Join(paths, ","))
	}

	return call[tenantv1.StaffGroup](ctx, c, http.MethodPatch, path, group)
}

// DeleteStaffGroup deletes a group; members keep their other groups.
func (c *Client) DeleteStaffGroup(ctx context.Context, garageID, groupID string) error {
	_, err := call[tenantv1.DeleteStaffGroupResponse](ctx, c, http.MethodDelete,
		garagePath(garageID)+"/staff-groups/"+url.PathEscape(groupID), nil)

	return err
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

// CreatePlanPaymentRequest opens a request to pay for a plan change or
// renewal (Release 5.2 bank-transfer flow).
func (c *Client) CreatePlanPaymentRequest(ctx context.Context, planID string,
	kind licensev1.PlanPaymentKind,
) (*licensev1.PlanPaymentRequest, error) {
	return call[licensev1.PlanPaymentRequest](ctx, c, http.MethodPost, "/api/v1/me/subscription/payment-requests",
		&licensev1.CreatePlanPaymentRequestRequest{PlanId: planID, Kind: kind})
}

// SubmitPlanPayment declares the bank transfer for requestID. Evidence is
// required unless the amount due is 0.
func (c *Client) SubmitPlanPayment(ctx context.Context,
	req *licensev1.SubmitPlanPaymentRequest,
) (*licensev1.PlanPaymentRequest, error) {
	path := "/api/v1/me/subscription/payment-requests/" + url.PathEscape(req.GetRequestId()) + ":submit"

	return call[licensev1.PlanPaymentRequest](ctx, c, http.MethodPost, path, req)
}

// CancelPlanPayment drops requestID (the signed-in owner's own request).
func (c *Client) CancelPlanPayment(ctx context.Context, requestID string) (*licensev1.PlanPaymentRequest, error) {
	path := "/api/v1/me/subscription/payment-requests/" + url.PathEscape(requestID) + ":cancel"

	return call[licensev1.PlanPaymentRequest](ctx, c, http.MethodPost, path,
		&licensev1.CancelPlanPaymentRequest{RequestId: requestID})
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
