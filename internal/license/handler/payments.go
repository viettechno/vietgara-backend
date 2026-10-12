package handler

import (
	"context"

	licensev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/license/v1"

	"github.com/viettechno/vietgara-backend/internal/license/domain"
	"github.com/viettechno/vietgara-backend/internal/license/usecase"
	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/platform/httpapi"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// CreatePlanPaymentRequest quotes a plan change or a renewal (FR-LIC-07);
// answers 201.
func (s *Server) CreatePlanPaymentRequest(
	ctx context.Context, req *licensev1.CreatePlanPaymentRequestRequest,
) (*licensev1.CreatePlanPaymentRequestResponse, error) {
	kind := domain.PaymentChange
	if req.GetKind() == licensev1.PlanPaymentKind_PLAN_PAYMENT_KIND_RENEW {
		kind = domain.PaymentRenew
	}

	if req.GetKind() == licensev1.PlanPaymentKind_PLAN_PAYMENT_KIND_UNSPECIFIED {
		kind = ""
	}

	view, err := s.payments.Start(ctx, middleware.UserID(ctx), req.GetPlanId(), kind)
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	httpapi.Created(ctx)

	return &licensev1.CreatePlanPaymentRequestResponse{Request: paymentRequestToProto(view)}, nil
}

// ListMyPlanPaymentRequests lists the caller's requests.
func (s *Server) ListMyPlanPaymentRequests(
	ctx context.Context, req *licensev1.ListMyPlanPaymentRequestsRequest,
) (*licensev1.ListMyPlanPaymentRequestsResponse, error) {
	views, err := s.payments.ListMine(ctx, middleware.UserID(ctx), paymentStatusFromProto(req.GetStatus()))
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	resp := &licensev1.ListMyPlanPaymentRequestsResponse{Data: make([]*licensev1.PlanPaymentRequest, 0, len(views))}
	for _, v := range views {
		resp.Data = append(resp.Data, paymentRequestToProto(v))
	}

	return resp, nil
}

// GetMyPlanPaymentRequest returns one of the caller's requests.
func (s *Server) GetMyPlanPaymentRequest(
	ctx context.Context, req *licensev1.GetMyPlanPaymentRequestRequest,
) (*licensev1.GetMyPlanPaymentRequestResponse, error) {
	view, err := s.payments.GetMine(ctx, middleware.UserID(ctx), req.GetRequestId())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &licensev1.GetMyPlanPaymentRequestResponse{Request: paymentRequestToProto(view)}, nil
}

// SubmitPlanPayment declares the transfer with its evidence.
func (s *Server) SubmitPlanPayment(
	ctx context.Context, req *licensev1.SubmitPlanPaymentRequest,
) (*licensev1.SubmitPlanPaymentResponse, error) {
	d := usecase.Declaration{
		PayerName: req.GetPayerName(), PayerBank: req.GetPayerBank(), PayerAccount: req.GetPayerAccount(),
		TransferredAmount: req.GetTransferredAmount(), BankTransactionRef: req.GetBankTransactionRef(),
		Evidence: req.GetEvidence().GetContent(), EvidenceContentType: req.GetEvidence().GetContentType(),
	}

	if at := req.GetPaidAt(); at != nil {
		t := at.AsTime()
		d.PaidAt = &t
	}

	view, err := s.payments.Submit(ctx, middleware.UserID(ctx), req.GetRequestId(), d)
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &licensev1.SubmitPlanPaymentResponse{Request: paymentRequestToProto(view)}, nil
}

// CancelPlanPayment withdraws a request before review.
func (s *Server) CancelPlanPayment(
	ctx context.Context, req *licensev1.CancelPlanPaymentRequest,
) (*licensev1.CancelPlanPaymentResponse, error) {
	view, err := s.payments.Cancel(ctx, middleware.UserID(ctx), req.GetRequestId())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &licensev1.CancelPlanPaymentResponse{Request: paymentRequestToProto(view)}, nil
}

// ListMyCreditLedger lists the caller's account credit entries.
func (s *Server) ListMyCreditLedger(
	ctx context.Context, _ *licensev1.ListMyCreditLedgerRequest,
) (*licensev1.ListMyCreditLedgerResponse, error) {
	entries, err := s.payments.ListCredit(ctx, middleware.UserID(ctx))
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	resp := &licensev1.ListMyCreditLedgerResponse{Data: make([]*licensev1.CreditLedgerEntry, 0, len(entries))}
	for _, e := range entries {
		resp.Data = append(resp.Data, creditEntryToProto(e))
	}

	return resp, nil
}

// GetPaymentInstructions returns the platform's payment details.
func (s *Server) GetPaymentInstructions(
	ctx context.Context, _ *licensev1.GetPaymentInstructionsRequest,
) (*licensev1.GetPaymentInstructionsResponse, error) {
	instructions, qr, err := s.payments.Instructions(ctx)
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &licensev1.GetPaymentInstructionsResponse{Instructions: &licensev1.PaymentInstructions{
		BankName: instructions.BankName, AccountNumber: instructions.AccountNumber, AccountHolder: instructions.AccountHolder,
		Branch: instructions.Branch, TransferInstructions: instructions.TransferInstructions, QrUrl: qr,
	}}, nil
}

// ListPlanPaymentRequests is the platform admin's review queue.
func (s *Server) ListPlanPaymentRequests(
	ctx context.Context, req *licensev1.ListPlanPaymentRequestsRequest,
) (*licensev1.ListPlanPaymentRequestsResponse, error) {
	page, pageSize := pageOf(req.GetPage(), req.GetPageSize())

	result, err := s.payments.Review(ctx, usecase.ReviewFilter{
		Status: paymentStatusFromProto(req.GetStatus()), Query: req.GetQuery(), Page: page, PageSize: pageSize,
	})
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	resp := &licensev1.ListPlanPaymentRequestsResponse{
		Data: make([]*licensev1.AdminPlanPaymentRequest, 0, len(result.Items)), Pagination: paginationOf(result.Total, page, pageSize),
	}
	for _, item := range result.Items {
		resp.Data = append(resp.Data, reviewDetailToProto(item))
	}

	return resp, nil
}

// GetPlanPaymentRequest returns one request for review.
func (s *Server) GetPlanPaymentRequest(
	ctx context.Context, req *licensev1.GetPlanPaymentRequestRequest,
) (*licensev1.GetPlanPaymentRequestResponse, error) {
	detail, err := s.payments.ReviewOne(ctx, req.GetRequestId())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &licensev1.GetPlanPaymentRequestResponse{Request: reviewDetailToProto(detail)}, nil
}

// ApprovePlanPayment accepts the payment; the plan applies at once.
func (s *Server) ApprovePlanPayment(
	ctx context.Context, req *licensev1.ApprovePlanPaymentRequest,
) (*licensev1.ApprovePlanPaymentResponse, error) {
	sub, err := s.payments.Approve(ctx, middleware.UserID(ctx), req.GetRequestId(), req.GetNote())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &licensev1.ApprovePlanPaymentResponse{Subscription: subscriptionToProto(sub, nil)}, nil
}

// RejectPlanPayment turns a request down with a reason.
func (s *Server) RejectPlanPayment(
	ctx context.Context, req *licensev1.RejectPlanPaymentRequest,
) (*licensev1.RejectPlanPaymentResponse, error) {
	view, err := s.payments.Reject(ctx, middleware.UserID(ctx), req.GetRequestId(), req.GetReason())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &licensev1.RejectPlanPaymentResponse{Request: paymentRequestToProto(view)}, nil
}

// AdjustCredit corrects an owner's account credit.
func (s *Server) AdjustCredit(
	ctx context.Context, req *licensev1.AdjustCreditRequest,
) (*licensev1.AdjustCreditResponse, error) {
	entry, err := s.payments.AdjustCredit(ctx, middleware.UserID(ctx), req.GetSubscriptionId(), req.GetAmount(), req.GetNote())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &licensev1.AdjustCreditResponse{Entry: creditEntryToProto(entry)}, nil
}

// GetPlatformPaymentSettings returns the platform's payment details.
func (s *Server) GetPlatformPaymentSettings(
	ctx context.Context, _ *licensev1.GetPlatformPaymentSettingsRequest,
) (*licensev1.GetPlatformPaymentSettingsResponse, error) {
	view, err := s.payments.Settings(ctx)
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &licensev1.GetPlatformPaymentSettingsResponse{Settings: settingsToProto(view)}, nil
}

// settingsPaths are the settable paths of the platform payment details.
var settingsPaths = []string{"bank_name", "account_number", "account_holder", "branch", "transfer_instructions"}

// UpdatePlatformPaymentSettings changes the fields named by the update mask.
func (s *Server) UpdatePlatformPaymentSettings(
	ctx context.Context, req *licensev1.UpdatePlatformPaymentSettingsRequest,
) (*licensev1.UpdatePlatformPaymentSettingsResponse, error) {
	paths := req.GetUpdateMask().GetPaths()
	if len(paths) == 0 {
		paths = settingsPaths
	}

	in := req.GetSettings()
	fields := usecase.SettingsFields{}

	for _, path := range paths {
		switch path {
		case "bank_name":
			fields.BankName = new(in.GetBankName())
		case "account_number":
			fields.AccountNumber = new(in.GetAccountNumber())
		case "account_holder":
			fields.AccountHolder = new(in.GetAccountHolder())
		case "branch":
			fields.Branch = new(in.GetBranch())
		case "transfer_instructions":
			fields.TransferInstructions = new(in.GetTransferInstructions())
		default:
			return nil, apperror.ToGRPCError(apperror.Validation("updateMask", apperror.IssueInvalid, "field "+path+" cannot be updated"))
		}
	}

	view, err := s.payments.UpdateSettings(ctx, middleware.UserID(ctx), fields)
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &licensev1.UpdatePlatformPaymentSettingsResponse{Settings: settingsToProto(view)}, nil
}

// UploadPaymentQr replaces the platform's QR code.
func (s *Server) UploadPaymentQr(
	ctx context.Context, req *licensev1.UploadPaymentQrRequest,
) (*licensev1.UploadPaymentQrResponse, error) {
	view, err := s.payments.UploadQR(ctx, middleware.UserID(ctx), req.GetContent(), req.GetContentType())
	if err != nil {
		return nil, apperror.ToGRPCError(err)
	}

	return &licensev1.UploadPaymentQrResponse{Settings: settingsToProto(view)}, nil
}
