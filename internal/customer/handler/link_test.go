package handler_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/viettechno/vietgara-backend/internal/customer/domain"
	"github.com/viettechno/vietgara-backend/internal/customer/handler"
	"github.com/viettechno/vietgara-backend/internal/customer/usecase"
	"github.com/viettechno/vietgara-backend/internal/mocks"
	"github.com/viettechno/vietgara-backend/internal/platform/audit"
)

const (
	linkCustomerID = "33333333-3333-4333-8333-333333333333"
	linkUserID     = "44444444-4444-4444-8444-444444444444"
)

// oneLink is a LinkRepository holding at most one link.
type oneLink struct{ link *domain.UserLink }

func (o *oneLink) Create(_ context.Context, l *domain.UserLink) error { o.link = l; return nil }

func (o *oneLink) FindCurrent(context.Context, string, string) (*domain.UserLink, error) {
	if o.link == nil || !o.link.Status.Open() {
		return nil, domain.ErrLinkNotFound
	}

	return o.link, nil
}

func (o *oneLink) FindForUser(context.Context, string, string) (*domain.UserLink, error) {
	return o.link, nil
}

func (o *oneLink) ListOpenForUser(context.Context, string) ([]*domain.UserLink, error) {
	return nil, nil
}

func (o *oneLink) HasAny(context.Context, string, string) (bool, error) { return false, nil }

func (o *oneLink) Update(context.Context, *domain.UserLink) error { return nil }

type linkDirectory struct{}

func (linkDirectory) FindVerifiedByEmail(context.Context, string) (string, bool, error) {
	return linkUserID, true, nil
}

type linkCodes struct{}

func (linkCodes) Send(context.Context, string, string, string) error { return nil }

func (linkCodes) Check(context.Context, string, string) error { return nil }

type linkGarage struct{}

func (linkGarage) Name(context.Context, string) (string, error) { return "Gara", nil }

type linkAudit struct{}

func (linkAudit) Record(context.Context, audit.Entry) error { return nil }

func TestLinkPolicies(t *testing.T) {
	t.Parallel()

	p := handler.LinkPolicies()
	assert.Len(t, p, 3)
	assert.NotNil(t, p[customerv1.CustomerLinkService_RequestCustomerUserLink_FullMethodName].Garage)
}

func TestLinkRPCs(t *testing.T) {
	t.Parallel()

	email := "binh@example.com"
	customers := mocks.NewMockCustomerRepository(t)
	customers.EXPECT().FindByID(anyArg, garageID, linkCustomerID).
		Return(&domain.Customer{ID: linkCustomerID, GarageID: garageID, FullName: "Binh", Email: &email}, nil).Maybe()

	srv := handler.NewLinkServer(usecase.NewLinkUseCase(usecase.LinkDeps{
		Links: &oneLink{}, Customers: customers, Users: linkDirectory{}, Codes: linkCodes{}, Garages: linkGarage{},
		Audit: linkAudit{},
	}, usecase.WithClock(func() time.Time { return time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC) })))

	requested, err := srv.RequestCustomerUserLink(signedIn(t), &customerv1.RequestCustomerUserLinkRequest{
		GarageId: garageID, CustomerId: linkCustomerID,
	})
	require.NoError(t, err)
	assert.Equal(t, customerv1.CustomerUserLinkStatus_CUSTOMER_USER_LINK_STATUS_REQUESTED, requested.GetLink().GetStatus())
	assert.Equal(t, linkCustomerID, requested.GetLink().GetCustomerId())

	confirmed, err := srv.ConfirmCustomerUserLink(signedIn(t), &customerv1.ConfirmCustomerUserLinkRequest{
		GarageId: garageID, CustomerId: linkCustomerID, Otp: "123456",
	})
	require.NoError(t, err)
	assert.Equal(t, customerv1.CustomerUserLinkStatus_CUSTOMER_USER_LINK_STATUS_LINKED, confirmed.GetLink().GetStatus())
	assert.Equal(t, customerv1.CustomerUserLinkChannel_CUSTOMER_USER_LINK_CHANNEL_OTP, confirmed.GetLink().GetChannel())
	assert.NotNil(t, confirmed.GetLink().GetLinkedAt())

	unlinked, err := srv.UnlinkCustomerUser(signedIn(t), &customerv1.UnlinkCustomerUserRequest{
		GarageId: garageID, CustomerId: linkCustomerID,
	})
	require.NoError(t, err)
	assert.Equal(t, customerv1.CustomerUserLinkStatus_CUSTOMER_USER_LINK_STATUS_UNLINKED, unlinked.GetLink().GetStatus())

	_, err = srv.UnlinkCustomerUser(signedIn(t), &customerv1.UnlinkCustomerUserRequest{
		GarageId: garageID, CustomerId: linkCustomerID,
	})
	assert.Equal(t, codes.NotFound, status.Code(err))

	_, err = srv.ConfirmCustomerUserLink(signedIn(t), &customerv1.ConfirmCustomerUserLinkRequest{
		GarageId: garageID, CustomerId: linkCustomerID, Otp: "123456",
	})
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
}

func TestLinkStatusMapping(t *testing.T) {
	t.Parallel()

	for status, want := range map[domain.LinkStatus]customerv1.CustomerUserLinkStatus{
		"":                   customerv1.CustomerUserLinkStatus_CUSTOMER_USER_LINK_STATUS_UNSPECIFIED,
		domain.LinkDeclined:  customerv1.CustomerUserLinkStatus_CUSTOMER_USER_LINK_STATUS_DECLINED,
		domain.LinkUnlinked:  customerv1.CustomerUserLinkStatus_CUSTOMER_USER_LINK_STATUS_UNLINKED,
		domain.LinkRequested: customerv1.CustomerUserLinkStatus_CUSTOMER_USER_LINK_STATUS_REQUESTED,
	} {
		assert.Equal(t, want, handler.LinkStatusToProto(status))
	}

	link := handler.LinkToProto(&domain.UserLink{Status: domain.LinkLinked, Channel: domain.ChannelAutoEmail})
	assert.Equal(t, customerv1.CustomerUserLinkChannel_CUSTOMER_USER_LINK_CHANNEL_AUTO_EMAIL, link.GetChannel())

	link = handler.LinkToProto(&domain.UserLink{Status: domain.LinkLinked, Channel: domain.ChannelApp})
	assert.Equal(t, customerv1.CustomerUserLinkChannel_CUSTOMER_USER_LINK_CHANNEL_APP, link.GetChannel())
}
