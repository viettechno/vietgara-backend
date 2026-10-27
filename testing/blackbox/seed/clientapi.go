package seed

import (
	"context"

	identityv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/identity/v1"
	licensev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/license/v1"
	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/client"
)

// NewClientAPI adapts the blackbox client to the API the seeder needs.
func NewClientAPI(c *client.Client) API { return clientAPI{client: c} }

type clientAPI struct {
	client *client.Client
}

func (a clientAPI) Login(ctx context.Context, email, password string) (*identityv1.Session, error) {
	return a.client.Login(ctx, email, password)
}

func (a clientAPI) Register(ctx context.Context, request *identityv1.RegisterRequest) (*identityv1.Session, error) {
	return a.client.Register(ctx, request)
}

func (a clientAPI) VerifyEmail(ctx context.Context, otp, refreshToken string) (*identityv1.Session, error) {
	return a.client.VerifyEmail(ctx, otp, refreshToken)
}

func (a clientAPI) MySubscription(ctx context.Context) (*licensev1.Subscription, error) {
	return a.client.MySubscription(ctx)
}

func (a clientAPI) ListPlans(ctx context.Context) ([]*licensev1.Plan, error) {
	return a.client.ListPlans(ctx)
}

func (a clientAPI) CreatePlanPaymentRequest(ctx context.Context, planID string,
	kind licensev1.PlanPaymentKind,
) (*licensev1.PlanPaymentRequest, error) {
	return a.client.CreatePlanPaymentRequest(ctx, planID, kind)
}

func (a clientAPI) SubmitPlanPayment(ctx context.Context,
	req *licensev1.SubmitPlanPaymentRequest,
) (*licensev1.PlanPaymentRequest, error) {
	return a.client.SubmitPlanPayment(ctx, req)
}

func (a clientAPI) ApprovePlanPayment(ctx context.Context, requestID, note string) (*licensev1.Subscription, error) {
	return a.client.ApprovePlanPayment(ctx, requestID, note)
}

func (a clientAPI) ListGarages(ctx context.Context) ([]*tenantv1.Garage, error) {
	return a.client.ListGarages(ctx)
}

func (a clientAPI) CreateGarage(ctx context.Context, garage *tenantv1.Garage) (*tenantv1.Garage, error) {
	return a.client.CreateGarage(ctx, garage)
}

func (a clientAPI) WithToken(token string) API {
	return clientAPI{client: a.client.WithToken(token)}
}
