package handler

import (
	"context"

	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"

	"github.com/viettechno/vietgara-backend/internal/customer/domain"
	"github.com/viettechno/vietgara-backend/internal/customer/usecase"
	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/platform/access"
	"github.com/viettechno/vietgara-backend/pkg/converter"
)

// LinkServer implements customerv1.CustomerLinkServiceServer (FR-CUS-06).
type LinkServer struct {
	customerv1.UnimplementedCustomerLinkServiceServer

	uc *usecase.LinkUseCase
}

// NewLinkServer wires the handler to its use case.
func NewLinkServer(uc *usecase.LinkUseCase) *LinkServer {
	return &LinkServer{uc: uc}
}

// LinkPolicies declares the permission every link RPC requires in its
// garage.
func LinkPolicies() middleware.Policies {
	write := middleware.GarageMethod(access.Requires(access.CustomerWrite))

	return middleware.Policies{
		customerv1.CustomerLinkService_RequestCustomerUserLink_FullMethodName: write,
		customerv1.CustomerLinkService_ConfirmCustomerUserLink_FullMethodName: write,
		customerv1.CustomerLinkService_UnlinkCustomerUser_FullMethodName:      write,
	}
}

// RequestCustomerUserLink asks the user that owns the customer's verified
// e-mail to link.
func (s *LinkServer) RequestCustomerUserLink(
	ctx context.Context, req *customerv1.RequestCustomerUserLinkRequest,
) (*customerv1.RequestCustomerUserLinkResponse, error) {
	return run(ctx, func(actorID string) (*customerv1.RequestCustomerUserLinkResponse, error) {
		link, err := s.uc.Request(ctx, req.GetGarageId(), req.GetCustomerId(), actorID)
		if err != nil {
			return nil, err
		}

		return &customerv1.RequestCustomerUserLinkResponse{Link: LinkToProto(link)}, nil
	})
}

// ConfirmCustomerUserLink confirms the waiting request with the user's code.
func (s *LinkServer) ConfirmCustomerUserLink(
	ctx context.Context, req *customerv1.ConfirmCustomerUserLinkRequest,
) (*customerv1.ConfirmCustomerUserLinkResponse, error) {
	return run(ctx, func(actorID string) (*customerv1.ConfirmCustomerUserLinkResponse, error) {
		link, err := s.uc.Confirm(ctx, req.GetGarageId(), req.GetCustomerId(), actorID, req.GetOtp())
		if err != nil {
			return nil, err
		}

		return &customerv1.ConfirmCustomerUserLinkResponse{Link: LinkToProto(link)}, nil
	})
}

// UnlinkCustomerUser ends the link from the garage's side.
func (s *LinkServer) UnlinkCustomerUser(
	ctx context.Context, req *customerv1.UnlinkCustomerUserRequest,
) (*customerv1.UnlinkCustomerUserResponse, error) {
	return run(ctx, func(actorID string) (*customerv1.UnlinkCustomerUserResponse, error) {
		link, err := s.uc.Unlink(ctx, req.GetGarageId(), req.GetCustomerId(), actorID)
		if err != nil {
			return nil, err
		}

		return &customerv1.UnlinkCustomerUserResponse{Link: LinkToProto(link)}, nil
	})
}

// LinkToProto maps a link for the garage's side and the Customer BFF; it
// never carries the user.
func LinkToProto(l *domain.UserLink) *customerv1.CustomerUserLink {
	out := &customerv1.CustomerUserLink{
		Id: l.ID, GarageId: l.GarageID, CustomerId: l.CustomerID, Status: LinkStatusToProto(l.Status),
		Channel: linkChannelToProto(l.Channel), RequestedAt: converter.TimeToProto(l.RequestedAt),
	}

	if l.LinkedAt != nil {
		out.LinkedAt = converter.TimeToProto(*l.LinkedAt)
	}

	return out
}

// LinkStatusToProto maps a link status; "" (never linked) is UNSPECIFIED.
func LinkStatusToProto(s domain.LinkStatus) customerv1.CustomerUserLinkStatus {
	switch s {
	case domain.LinkRequested:
		return customerv1.CustomerUserLinkStatus_CUSTOMER_USER_LINK_STATUS_REQUESTED
	case domain.LinkLinked:
		return customerv1.CustomerUserLinkStatus_CUSTOMER_USER_LINK_STATUS_LINKED
	case domain.LinkDeclined:
		return customerv1.CustomerUserLinkStatus_CUSTOMER_USER_LINK_STATUS_DECLINED
	case domain.LinkUnlinked:
		return customerv1.CustomerUserLinkStatus_CUSTOMER_USER_LINK_STATUS_UNLINKED
	default:
		return customerv1.CustomerUserLinkStatus_CUSTOMER_USER_LINK_STATUS_UNSPECIFIED
	}
}

func linkChannelToProto(c domain.LinkChannel) customerv1.CustomerUserLinkChannel {
	switch c {
	case domain.ChannelOTP:
		return customerv1.CustomerUserLinkChannel_CUSTOMER_USER_LINK_CHANNEL_OTP
	case domain.ChannelApp:
		return customerv1.CustomerUserLinkChannel_CUSTOMER_USER_LINK_CHANNEL_APP
	case domain.ChannelAutoEmail:
		return customerv1.CustomerUserLinkChannel_CUSTOMER_USER_LINK_CHANNEL_AUTO_EMAIL
	default:
		return customerv1.CustomerUserLinkChannel_CUSTOMER_USER_LINK_CHANNEL_UNSPECIFIED
	}
}
