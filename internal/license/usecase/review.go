package usecase

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/viettechno/vietgara-backend/internal/license/domain"
	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/platform/audit"
)

// ReviewDetail is a request of the review queue with its owner, the plan
// they are on and what they use.
type ReviewDetail struct {
	RequestView

	SubscriptionID string
	Email          string
	FullName       string
	CurrentPlan    *domain.Plan
	Usage          Usage
}

// ReviewList is one page of the review queue.
type ReviewList struct {
	Items []ReviewDetail
	Total int
}

// Review lists the payment requests for the platform admin, oldest
// submitted first (FR-LIC-08).
func (uc *PaymentUseCase) Review(ctx context.Context, f ReviewFilter) (*ReviewList, error) {
	if f.Status == "" {
		f.Status = domain.PaymentSubmitted
	}

	page, err := uc.payments.SearchRequests(ctx, f)
	if err != nil {
		return nil, toAppError("list requests", err)
	}

	out := &ReviewList{Total: page.Total}

	for _, item := range page.Items {
		detail, err := uc.detail(ctx, item.Request, item.Email, item.FullName)
		if err != nil {
			return nil, err
		}

		out.Items = append(out.Items, detail)
	}

	return out, nil
}

// ReviewOne returns one request with the owner's usage.
func (uc *PaymentUseCase) ReviewOne(ctx context.Context, requestID string) (ReviewDetail, error) {
	req, err := uc.payments.FindRequest(ctx, requestID)
	if err != nil {
		return ReviewDetail{}, toAppError("read request", err)
	}

	return uc.detail(ctx, req, "", "")
}

func (uc *PaymentUseCase) detail(ctx context.Context, req *domain.PlanPaymentRequest, email, name string) (ReviewDetail, error) {
	garages, staff, err := uc.usage.OwnerUsage(ctx, req.UserID)
	if err != nil {
		return ReviewDetail{}, toAppError("read usage", err)
	}

	sub, err := uc.subscriptions.LockByID(ctx, req.SubscriptionID)
	if err != nil {
		return ReviewDetail{}, toAppError("read subscription", err)
	}

	return ReviewDetail{
		RequestView: uc.view(ctx, req), SubscriptionID: req.SubscriptionID, Email: email, FullName: name,
		CurrentPlan: sub.Plan, Usage: Usage{Garages: garages, Staff: staff},
	}, nil
}

// Approve accepts the payment: the plan applies at once and a new period
// starts, the credit ledger and the entitlements follow, all in the
// caller's transaction under the subscription's lock (FR-LIC-08). The
// limits are validated again; a transfer that differs from the amount due
// needs a note.
func (uc *PaymentUseCase) Approve(ctx context.Context, adminID, requestID, note string) (*domain.Subscription, error) {
	req, err := uc.payments.LockRequest(ctx, requestID)
	if err != nil {
		return nil, toAppError("approve payment", err)
	}

	if req.Status != domain.PaymentSubmitted {
		return nil, toAppError("approve payment", domain.ErrRequestNotOpen)
	}

	note, err = approvalNote(req, note)
	if err != nil {
		return nil, toAppError("approve payment", err)
	}

	sub, err := uc.subscriptions.LockByID(ctx, req.SubscriptionID)
	if err != nil {
		return nil, toAppError("approve payment", err)
	}

	plan, err := uc.plans.FindByID(ctx, req.PlanID)
	if err != nil {
		return nil, toAppError("approve payment", err)
	}

	if err := uc.checkLimits(ctx, req.UserID, plan); err != nil {
		return nil, err
	}

	before, from, fromPlan := *sub, sub.Status, sub.PlanID
	now := uc.opts.now().UTC()

	if err := uc.applyCredit(ctx, sub, req, adminID); err != nil {
		return nil, err
	}

	sub.Apply(plan, req.Kind, req.QuotedAmount, now)

	if err := uc.subscriptions.Update(ctx, sub); err != nil {
		return nil, toAppError("save subscription", err)
	}

	reqBefore := requestAudit(req)
	req.Status, req.ReviewedBy, req.ReviewedAt, req.ReviewNote = domain.PaymentApproved, adminID, &now, note

	if err := uc.payments.SaveRequest(ctx, req); err != nil {
		return nil, toAppError("save request", err)
	}

	if err := uc.publishApproval(ctx, sub, req, from, fromPlan); err != nil {
		return nil, err
	}

	if err := uc.auditRequest(ctx, ActionPaymentApproved, req, reqBefore, map[string]any{
		keyStatus: req.Status, keyAmountDue: req.AmountDue, "transferred_amount": req.TransferredAmount,
		"amount_mismatch": req.AmountMismatch, "note": note, keyPlanID: plan.ID, "balance_before": before.CreditBalance,
		"balance_after": sub.CreditBalance,
	}); err != nil {
		return nil, err
	}

	return sub, nil
}

// approvalNote validates the reviewer's note: a transfer that differs from
// the amount due needs one.
func approvalNote(req *domain.PlanPaymentRequest, note string) (string, error) {
	note = strings.TrimSpace(note)

	switch {
	case req.AmountMismatch && note == "":
		return "", domain.ErrMismatchNoteRequired
	case utf8.RuneCountInString(note) > maxNoteLength:
		return "", domain.ErrInvalidDeclaration
	}

	return note, nil
}

// applyCredit records the credit a payment earned and used and sets the
// balance: the unused value of the period was earned at the quote, and the
// credit applied is what the payment used of it and of the balance.
func (uc *PaymentUseCase) applyCredit(ctx context.Context, sub *domain.Subscription, req *domain.PlanPaymentRequest, adminID string) error {
	balance := sub.CreditBalance

	if req.UnusedCredit > 0 {
		balance += req.UnusedCredit

		err := uc.payments.AddCreditEntry(ctx, &domain.CreditEntry{
			UserID: req.UserID, Kind: domain.CreditEarned, Amount: req.UnusedCredit, BalanceAfter: balance,
			PlanPaymentRequestID: req.ID, Note: "unused value of the previous period", CreatedBy: adminID,
		})
		if err != nil {
			return toAppError("record credit", err)
		}
	}

	if used := min(req.CreditApplied, balance); used > 0 {
		balance -= used

		err := uc.payments.AddCreditEntry(ctx, &domain.CreditEntry{
			UserID: req.UserID, Kind: domain.CreditApplied, Amount: -used, BalanceAfter: balance,
			PlanPaymentRequestID: req.ID, Note: "applied to " + req.Reference, CreatedBy: adminID,
		})
		if err != nil {
			return toAppError("record credit", err)
		}
	}

	sub.CreditBalance = balance

	return nil
}

// publishApproval syncs the entitlements and publishes the events of an
// approval.
func (uc *PaymentUseCase) publishApproval(
	ctx context.Context, sub *domain.Subscription, req *domain.PlanPaymentRequest, from domain.SubscriptionStatus, fromPlan string,
) error {
	if from != sub.Status {
		if err := uc.sync.statusChanged(ctx, sub, from, nil); err != nil {
			return toAppError("publish status change", err)
		}
	}

	if err := uc.sync.entitlementsChanged(ctx, sub); err != nil {
		return toAppError("sync entitlements", err)
	}

	err := uc.events.Publish(ctx, EventPaymentApproved, "", sub.UserID, map[string]any{
		keyRequestID: req.ID, keyUserID: sub.UserID, keyPlanCode: sub.Plan.Code, keyReference: req.Reference,
		"kind": req.Kind, keyAmountDue: req.AmountDue,
	})
	if err != nil {
		return toAppError("publish payment approved", err)
	}

	err = uc.events.Publish(ctx, EventPlanChanged, "", sub.UserID, map[string]any{
		"subscription_id": sub.ID, keyUserID: sub.UserID, "from_plan_id": fromPlan, keyPlanID: sub.PlanID,
		"period_start": sub.CurrentPeriodStart.Format(time.RFC3339), "period_end": sub.CurrentPeriodEnd.Format(time.RFC3339),
	})
	if err != nil {
		return toAppError("publish plan changed", err)
	}

	err = uc.events.Publish(ctx, EventCreditChanged, "", sub.UserID, map[string]any{
		keyUserID: sub.UserID, "balance": sub.CreditBalance, keyRequestID: req.ID,
	})
	if err != nil {
		return toAppError("publish credit changed", err)
	}

	return nil
}

// Reject turns a submitted request down with a reason; the owner is told
// and may submit a new request.
func (uc *PaymentUseCase) Reject(ctx context.Context, adminID, requestID, reason string) (RequestView, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" || utf8.RuneCountInString(reason) > maxNoteLength {
		return RequestView{}, toAppError("reject payment", domain.ErrInvalidDeclaration)
	}

	req, err := uc.payments.LockRequest(ctx, requestID)
	if err != nil {
		return RequestView{}, toAppError("reject payment", err)
	}

	if req.Status != domain.PaymentSubmitted {
		return RequestView{}, toAppError("reject payment", domain.ErrRequestNotOpen)
	}

	before, now := requestAudit(req), uc.opts.now().UTC()
	req.Status, req.RejectionReason, req.ReviewedBy, req.ReviewedAt = domain.PaymentRejected, reason, adminID, &now

	if err := uc.payments.SaveRequest(ctx, req); err != nil {
		return RequestView{}, toAppError("save request", err)
	}

	err = uc.events.Publish(ctx, EventPaymentRejected, "", req.UserID, map[string]any{
		keyRequestID: req.ID, keyUserID: req.UserID, keyReference: req.Reference, "reason": reason,
	})
	if err != nil {
		return RequestView{}, toAppError("publish payment rejected", err)
	}

	after := requestAudit(req)
	after["reason"] = reason

	if err := uc.auditRequest(ctx, ActionPaymentRejected, req, before, after); err != nil {
		return RequestView{}, err
	}

	return uc.view(ctx, req), nil
}

// AdjustCredit corrects an owner's account credit by a signed amount; the
// balance never goes below 0 (FR-LIC-09).
func (uc *PaymentUseCase) AdjustCredit(ctx context.Context, adminID, subscriptionID string, amount int64, note string) (*domain.CreditEntry, error) {
	note = strings.TrimSpace(note)
	if amount == 0 || note == "" || utf8.RuneCountInString(note) > maxNoteLength {
		return nil, toAppError("adjust credit", domain.ErrInvalidAdjustment)
	}

	sub, err := uc.subscriptions.LockByID(ctx, subscriptionID)
	if err != nil {
		return nil, toAppError("adjust credit", err)
	}

	balance := max(sub.CreditBalance+amount, 0)
	entry := &domain.CreditEntry{
		UserID: sub.UserID, Kind: domain.CreditAdjustment, Amount: balance - sub.CreditBalance, BalanceAfter: balance,
		Note: note, CreatedBy: adminID,
	}

	if entry.Amount == 0 {
		return nil, toAppError("adjust credit", domain.ErrInvalidAdjustment)
	}

	before := sub.CreditBalance
	sub.CreditBalance = balance

	if err := uc.subscriptions.Update(ctx, sub); err != nil {
		return nil, toAppError("save subscription", err)
	}

	if err := uc.payments.AddCreditEntry(ctx, entry); err != nil {
		return nil, toAppError("record credit", err)
	}

	err = uc.events.Publish(ctx, EventCreditChanged, "", sub.UserID, map[string]any{
		keyUserID: sub.UserID, "balance": balance, "adjustment": entry.Amount,
	})
	if err != nil {
		return nil, toAppError("publish credit changed", err)
	}

	err = uc.audit.Record(ctx, audit.Entry{
		Actor: middleware.UserID(ctx), Action: ActionCreditAdjusted, EntityType: "subscription", EntityID: sub.ID,
		Before: map[string]any{"credit_balance": before}, After: map[string]any{"credit_balance": balance, "note": note},
		IP: middleware.ClientIP(ctx),
	})
	if err != nil {
		return nil, toAppError("audit credit", err)
	}

	return entry, nil
}

// SettingsFields are the settable platform payment details; nil fields are
// left unchanged.
type SettingsFields struct {
	BankName             *string
	AccountNumber        *string
	AccountHolder        *string
	Branch               *string
	TransferInstructions *string
}

// SettingsView is the platform's payment details with the QR code's URL.
type SettingsView struct {
	Settings *domain.PaymentSettings
	QRURL    string
}

// Settings returns the platform's payment details.
func (uc *PaymentUseCase) Settings(ctx context.Context) (SettingsView, error) {
	s, err := uc.payments.Settings(ctx)
	if err != nil {
		return SettingsView{}, toAppError("read payment settings", err)
	}

	return uc.settingsView(ctx, s), nil
}

func (uc *PaymentUseCase) settingsView(ctx context.Context, s *domain.PaymentSettings) SettingsView {
	v := SettingsView{Settings: s}
	if s.QRFileID != "" {
		v.QRURL, _ = uc.files.SignedURL(ctx, s.QRFileID)
	}

	return v
}

// UpdateSettings changes the platform's payment details; every change is
// audited with the values before and after (FR-LIC-10).
func (uc *PaymentUseCase) UpdateSettings(ctx context.Context, adminID string, f SettingsFields) (SettingsView, error) {
	s, err := uc.payments.Settings(ctx)
	if err != nil {
		return SettingsView{}, toAppError("read payment settings", err)
	}

	before := *s

	for dst, src := range map[*string]*string{
		&s.BankName: f.BankName, &s.AccountNumber: f.AccountNumber, &s.AccountHolder: f.AccountHolder, &s.Branch: f.Branch,
		&s.TransferInstructions: f.TransferInstructions,
	} {
		if src == nil {
			continue
		}

		value := strings.TrimSpace(*src)
		if utf8.RuneCountInString(value) > maxNoteLength {
			return SettingsView{}, toAppError("update payment settings", domain.ErrInvalidDeclaration)
		}

		*dst = value
	}

	return uc.saveSettings(ctx, adminID, s, before)
}

// UploadQR replaces the platform's QR code.
func (uc *PaymentUseCase) UploadQR(ctx context.Context, adminID string, content []byte, contentType string) (SettingsView, error) {
	s, err := uc.payments.Settings(ctx)
	if err != nil {
		return SettingsView{}, toAppError("read payment settings", err)
	}

	before := *s

	id, err := uc.files.StoreQR(ctx, adminID, content, contentType)
	if err != nil {
		return SettingsView{}, toAppError("store qr", err)
	}

	s.QRFileID = id

	return uc.saveSettings(ctx, adminID, s, before)
}

func (uc *PaymentUseCase) saveSettings(ctx context.Context, adminID string, s *domain.PaymentSettings, before domain.PaymentSettings) (SettingsView, error) {
	if err := uc.payments.SaveSettings(ctx, s, adminID); err != nil {
		return SettingsView{}, toAppError("save payment settings", err)
	}

	err := uc.audit.Record(ctx, audit.Entry{
		Actor: adminID, Action: ActionSettingsChanged, EntityType: "platform_payment_settings", EntityID: "1",
		Before: settingsAudit(&before), After: settingsAudit(s), IP: middleware.ClientIP(ctx),
	})
	if err != nil {
		return SettingsView{}, toAppError("audit payment settings", err)
	}

	return uc.settingsView(ctx, s), nil
}

func settingsAudit(s *domain.PaymentSettings) map[string]any {
	return map[string]any{
		"bank_name": s.BankName, "account_number": s.AccountNumber, "account_holder": s.AccountHolder, "branch": s.Branch,
		"transfer_instructions": s.TransferInstructions, "qr_file_id": s.QRFileID,
	}
}

// WithOpenRequests sets the open request of each subscription of the admin
// list (FR-LIC-08), returning them by subscription id.
func (uc *PaymentUseCase) WithOpenRequests(ctx context.Context, subscriptionIDs []string) (map[string]RequestView, error) {
	open, err := uc.payments.OpenRequests(ctx, subscriptionIDs)
	if err != nil {
		return nil, toAppError("list open requests", err)
	}

	out := make(map[string]RequestView, len(open))
	for id, req := range open {
		out[id] = uc.view(ctx, req)
	}

	return out, nil
}
