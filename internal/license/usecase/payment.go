package usecase

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/viettechno/vietgara-backend/internal/license/domain"
	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/platform/audit"
)

// Events and audit actions of payment requests (Event Design Section 4,
// Security Architecture Section 7).
const (
	EventPaymentSubmitted      = "subscription.payment_submitted"
	EventPaymentApproved       = "subscription.payment_approved"
	EventPaymentRejected       = "subscription.payment_rejected"
	EventPlanChanged           = "subscription.plan_changed"
	EventCreditChanged         = "subscription.credit_changed"
	EventPaymentReviewReminder = "subscription.payment_review_reminder"

	ActionPaymentSubmitted = "subscription.plan_payment_submitted"
	ActionPaymentCancelled = "subscription.plan_payment_cancelled"
	ActionPaymentApproved  = "subscription.plan_payment_approved"
	ActionPaymentRejected  = "subscription.plan_payment_rejected"
	ActionCreditAdjusted   = "subscription.credit_adjusted"
	ActionSettingsChanged  = "platform_payment_settings.updated"

	entityPaymentRequest = "plan_payment_request"

	maxReferenceAttempts = 5
	maxPayerNameLength   = 255
	maxNoteLength        = 2000
)

// PaymentDeps are the dependencies of PaymentUseCase.
type PaymentDeps struct {
	Plans         PlanRepository
	Subscriptions SubscriptionRepository
	Entitlements  EntitlementRepository
	Payments      PaymentRepository
	Files         FileStore
	Usage         UsageReader
	Events        EventPublisher
	Cache         EntitlementCache
	Audit         AuditLogger
}

// PaymentUseCase (FR-LIC-07..10) is the manual payment of plan changes and
// renewals: the owner quotes and declares a transfer, a platform admin
// reviews it and the plan applies at once; account credit carries unused
// value to the next payment.
type PaymentUseCase struct {
	plans         PlanRepository
	subscriptions SubscriptionRepository
	payments      PaymentRepository
	files         FileStore
	usage         UsageReader
	events        EventPublisher
	audit         AuditLogger
	sync          syncer
	opts          options
}

// NewPaymentUseCase wires the payment use case.
func NewPaymentUseCase(deps PaymentDeps, opts ...Option) *PaymentUseCase {
	return &PaymentUseCase{
		plans: deps.Plans, subscriptions: deps.Subscriptions, payments: deps.Payments, files: deps.Files,
		usage: deps.Usage, events: deps.Events, audit: deps.Audit,
		sync: syncer{entitlements: deps.Entitlements, events: deps.Events, cache: deps.Cache},
		opts: newOptions(opts),
	}
}

// RequestView is a request with the signed URLs it is shown with.
type RequestView struct {
	Request *domain.PlanPaymentRequest
	// QRURL is the QR code of the platform's payment details.
	QRURL string
	// EvidenceURL is the screenshot of the transfer, once submitted.
	EvidenceURL string
}

// view signs the URLs a request is shown with. A file that cannot be signed
// is left out: the request is still readable.
func (uc *PaymentUseCase) view(ctx context.Context, r *domain.PlanPaymentRequest) RequestView {
	v := RequestView{Request: r}

	if id := r.Instructions.QRFileID; id != "" {
		v.QRURL, _ = uc.files.SignedURL(ctx, id)
	}

	if r.EvidenceFileID != "" {
		v.EvidenceURL, _ = uc.files.SignedURL(ctx, r.EvidenceFileID)
	}

	return v
}

// views signs a list of requests.
func (uc *PaymentUseCase) views(ctx context.Context, requests []*domain.PlanPaymentRequest) []RequestView {
	out := make([]RequestView, 0, len(requests))
	for _, r := range requests {
		out = append(out, uc.view(ctx, r))
	}

	return out
}

// Start quotes a plan change or a renewal and returns the request with the
// transfer reference and the platform's payment details (FR-LIC-07).
func (uc *PaymentUseCase) Start(ctx context.Context, userID, planID string, kind domain.PaymentKind) (RequestView, error) {
	if !kind.IsValid() {
		return RequestView{}, toAppError("start payment", domain.ErrPlanNotAvailable)
	}

	// Ensure the user has a subscription (trial) before locking it.
	if _, err := uc.ensureSubscription(ctx, userID); err != nil {
		return RequestView{}, toAppError("start payment", err)
	}

	// Now lock the subscription for the payment flow.
	sub, err := uc.subscriptions.LockByUserID(ctx, userID)
	if err != nil {
		return RequestView{}, toAppError("start payment", err)
	}

	if sub.Status == domain.StatusCancelled {
		return RequestView{}, toAppError("start payment", domain.ErrSubscriptionCancelled)
	}

	plan, err := uc.choosePlan(ctx, sub, planID, kind)
	if err != nil {
		return RequestView{}, err
	}

	if err := uc.checkLimits(ctx, userID, plan); err != nil {
		return RequestView{}, err
	}

	if err := uc.replaceAwaiting(ctx, userID); err != nil {
		return RequestView{}, err
	}

	settings, err := uc.payments.Settings(ctx)
	if err != nil {
		return RequestView{}, toAppError("read payment settings", err)
	}

	quote := domain.QuotePayment(sub, plan, kind, uc.opts.now().UTC())
	req := &domain.PlanPaymentRequest{
		UserID: userID, SubscriptionID: sub.ID, Kind: kind, PlanID: plan.ID, Plan: plan, Status: domain.PaymentAwaitingPayment,
		QuotedAmount: quote.QuotedAmount, CreditApplied: quote.CreditApplied, AmountDue: quote.AmountDue,
		CreditSurplus: quote.CreditSurplus, UnusedCredit: quote.UnusedCredit, PeriodDays: quote.PeriodDays,
		UnusedDays: quote.UnusedDays, Instructions: settings.Instructions(),
	}

	if err := uc.create(ctx, req); err != nil {
		return RequestView{}, err
	}

	return uc.view(ctx, req), nil
}

// ensureSubscription ensures the user has a subscription (creating trial if needed)
// and returns the subscription.
func (uc *PaymentUseCase) ensureSubscription(ctx context.Context, userID string) (*domain.Subscription, error) {
	sub, err := uc.subscriptions.FindByUserID(ctx, userID)
	if err == nil {
		return sub, nil
	}

	if !errors.Is(err, domain.ErrSubscriptionNotFound) {
		return nil, err
	}

	trialPlan, err := uc.plans.FindByCode(ctx, domain.TrialPlanCode)
	if err != nil {
		return nil, err
	}

	trialSub := domain.NewTrial(userID, trialPlan, uc.opts.now().UTC())

	inserted, err := uc.subscriptions.CreateIfAbsent(ctx, trialSub)
	if err != nil {
		return nil, err
	}

	if inserted {
		if err := uc.sync.entitlementsChanged(ctx, trialSub); err != nil {
			return nil, err
		}
	}

	return uc.subscriptions.FindByUserID(ctx, userID)
}

// choosePlan resolves the plan a request pays for: the chosen one for a
// change, the current one for a renewal.
func (uc *PaymentUseCase) choosePlan(ctx context.Context, sub *domain.Subscription, planID string, kind domain.PaymentKind) (*domain.Plan, error) {
	plan, err := uc.plans.FindByID(ctx, planID)
	if err != nil {
		return nil, toAppError("start payment", err)
	}

	switch {
	case plan.Code == domain.TrialPlanCode:
		return nil, toAppError("start payment", domain.ErrTrialNotAvailable)
	case !plan.Selectable() && (kind != domain.PaymentRenew || plan.ID != sub.PlanID || !plan.Active):
		return nil, toAppError("start payment", domain.ErrPlanNotAvailable)
	case kind == domain.PaymentChange && plan.ID == sub.PlanID:
		return nil, toAppError("start payment", domain.ErrPlanNotAvailable)
	case kind == domain.PaymentRenew && plan.ID != sub.PlanID:
		return nil, toAppError("start payment", domain.ErrPlanNotAvailable)
	}

	return plan, nil
}

// checkLimits refuses a plan whose limits the owner's usage already exceeds.
func (uc *PaymentUseCase) checkLimits(ctx context.Context, userID string, plan *domain.Plan) error {
	garages, staff, err := uc.usage.OwnerUsage(ctx, userID)
	if err != nil {
		return toAppError("read usage", err)
	}

	switch {
	case garages > plan.MaxGarages:
		return limitReached(domain.LimitGarages, plan.MaxGarages)
	case staff > plan.MaxStaff:
		return limitReached(domain.LimitStaff, plan.MaxStaff)
	default:
		return nil
	}
}

// replaceAwaiting cancels the owner's request that was never declared; one
// that waits for review blocks a new one.
func (uc *PaymentUseCase) replaceAwaiting(ctx context.Context, userID string) error {
	open, err := uc.payments.FindOpenRequest(ctx, userID)
	if errors.Is(err, domain.ErrPaymentRequestNotFound) {
		return nil
	}

	if err != nil {
		return toAppError("read open request", err)
	}

	if open.Status == domain.PaymentSubmitted {
		return toAppError("start payment", domain.ErrRequestAlreadyOpen)
	}

	open.Status = domain.PaymentCancelled
	if err := uc.payments.SaveRequest(ctx, open); err != nil {
		return toAppError("replace request", err)
	}

	return nil
}

// create inserts the request under a fresh reference.
func (uc *PaymentUseCase) create(ctx context.Context, req *domain.PlanPaymentRequest) error {
	for range maxReferenceAttempts {
		req.Reference = domain.NewReference()

		err := uc.payments.CreateRequest(ctx, req)
		if !errors.Is(err, domain.ErrReferenceTaken) {
			if err != nil {
				return toAppError("create request", err)
			}

			return nil
		}
	}

	return toAppError("create request", domain.ErrReferenceTaken)
}

// ListMine returns the owner's requests, newest first.
func (uc *PaymentUseCase) ListMine(ctx context.Context, userID string, status domain.PaymentStatus) ([]RequestView, error) {
	requests, err := uc.payments.ListRequests(ctx, userID, status)
	if err != nil {
		return nil, toAppError("list requests", err)
	}

	return uc.views(ctx, requests), nil
}

// GetMine returns one of the owner's requests.
func (uc *PaymentUseCase) GetMine(ctx context.Context, userID, requestID string) (RequestView, error) {
	req, err := uc.mine(ctx, userID, requestID, false)
	if err != nil {
		return RequestView{}, err
	}

	return uc.view(ctx, req), nil
}

// OpenRequest returns the owner's open request, nil when none.
func (uc *PaymentUseCase) OpenRequest(ctx context.Context, userID string) (*RequestView, error) {
	req, err := uc.payments.FindOpenRequest(ctx, userID)
	if errors.Is(err, domain.ErrPaymentRequestNotFound) {
		return nil, nil //nolint:nilnil // no open request is an answer, not an error
	}

	if err != nil {
		return nil, toAppError("read open request", err)
	}

	v := uc.view(ctx, req)

	return &v, nil
}

// mine reads a request that must belong to the owner (a stranger's request
// is not found); lock holds its row lock.
func (uc *PaymentUseCase) mine(ctx context.Context, userID, requestID string, lock bool) (*domain.PlanPaymentRequest, error) {
	find := uc.payments.FindRequest
	if lock {
		find = uc.payments.LockRequest
	}

	req, err := find(ctx, requestID)
	if err != nil {
		return nil, toAppError("read request", err)
	}

	if req.UserID != userID {
		return nil, toAppError("read request", domain.ErrPaymentRequestNotFound)
	}

	return req, nil
}

// Declaration is what the owner declares about the transfer.
type Declaration struct {
	PayerName          string
	PayerBank          string
	PayerAccount       string
	TransferredAmount  int64
	BankTransactionRef string
	PaidAt             *time.Time
	// Evidence is the screenshot; required unless the amount due is 0.
	Evidence            []byte
	EvidenceContentType string
}

// Submit declares the transfer of a quoted request and sends it for review.
// The platform admins are notified (FR-LIC-07).
func (uc *PaymentUseCase) Submit(ctx context.Context, userID, requestID string, d Declaration) (RequestView, error) {
	req, err := uc.mine(ctx, userID, requestID, true)
	if err != nil {
		return RequestView{}, err
	}

	if req.Status != domain.PaymentAwaitingPayment {
		return RequestView{}, toAppError("submit payment", domain.ErrRequestNotOpen)
	}

	payer := strings.TrimSpace(d.PayerName)
	if payer == "" || utf8.RuneCountInString(payer) > maxPayerNameLength || d.TransferredAmount < 0 {
		return RequestView{}, toAppError("submit payment", domain.ErrInvalidDeclaration)
	}

	if req.AmountDue > 0 && len(d.Evidence) == 0 {
		return RequestView{}, toAppError("submit payment", domain.ErrEvidenceRequired)
	}

	if len(d.Evidence) > 0 {
		id, err := uc.files.StoreEvidence(ctx, userID, d.Evidence, d.EvidenceContentType)
		if err != nil {
			return RequestView{}, toAppError("store evidence", err)
		}

		req.EvidenceFileID = id
	}

	now := uc.opts.now().UTC()
	amount := d.TransferredAmount
	req.PayerName, req.PayerBank, req.PayerAccount = payer, strings.TrimSpace(d.PayerBank), strings.TrimSpace(d.PayerAccount)
	req.TransferredAmount, req.BankTransactionRef, req.PaidAt = &amount, strings.TrimSpace(d.BankTransactionRef), d.PaidAt
	req.AmountMismatch = amount != req.AmountDue
	req.Status, req.SubmittedAt = domain.PaymentSubmitted, &now

	if err := uc.payments.SaveRequest(ctx, req); err != nil {
		return RequestView{}, toAppError("save request", err)
	}

	err = uc.events.Publish(ctx, EventPaymentSubmitted, "", userID, map[string]any{
		keyRequestID: req.ID, keyUserID: userID, keyPlanCode: req.Plan.Code, keyAmountDue: req.AmountDue,
		"transferred_amount": amount, keyReference: req.Reference, "amount_mismatch": req.AmountMismatch,
	})
	if err != nil {
		return RequestView{}, toAppError("publish payment submitted", err)
	}

	if err := uc.auditRequest(ctx, ActionPaymentSubmitted, req, map[string]any{keyStatus: domain.PaymentAwaitingPayment},
		requestAudit(req)); err != nil {
		return RequestView{}, err
	}

	return uc.view(ctx, req), nil
}

// Cancel withdraws a request before it is reviewed.
func (uc *PaymentUseCase) Cancel(ctx context.Context, userID, requestID string) (RequestView, error) {
	req, err := uc.mine(ctx, userID, requestID, true)
	if err != nil {
		return RequestView{}, err
	}

	if !req.Status.Open() {
		return RequestView{}, toAppError("cancel payment", domain.ErrRequestNotOpen)
	}

	before := requestAudit(req)
	req.Status = domain.PaymentCancelled

	if err := uc.payments.SaveRequest(ctx, req); err != nil {
		return RequestView{}, toAppError("save request", err)
	}

	if err := uc.auditRequest(ctx, ActionPaymentCancelled, req, before, requestAudit(req)); err != nil {
		return RequestView{}, err
	}

	return uc.view(ctx, req), nil
}

// ListCredit returns the owner's credit ledger, newest first.
func (uc *PaymentUseCase) ListCredit(ctx context.Context, userID string) ([]*domain.CreditEntry, error) {
	entries, err := uc.payments.ListCredit(ctx, userID)
	if err != nil {
		return nil, toAppError("list credit", err)
	}

	return entries, nil
}

// Instructions returns the platform's payment details with the QR code's
// signed URL.
func (uc *PaymentUseCase) Instructions(ctx context.Context) (domain.PaymentInstructions, string, error) {
	settings, err := uc.payments.Settings(ctx)
	if err != nil {
		return domain.PaymentInstructions{}, "", toAppError("read payment settings", err)
	}

	var qr string
	if settings.QRFileID != "" {
		qr, _ = uc.files.SignedURL(ctx, settings.QRFileID)
	}

	return settings.Instructions(), qr, nil
}

func requestAudit(r *domain.PlanPaymentRequest) map[string]any {
	return map[string]any{
		keyStatus: r.Status, "kind": r.Kind, keyPlanID: r.PlanID, keyReference: r.Reference, keyAmountDue: r.AmountDue,
		"transferred_amount": r.TransferredAmount, "amount_mismatch": r.AmountMismatch,
	}
}

func (uc *PaymentUseCase) auditRequest(ctx context.Context, action string, r *domain.PlanPaymentRequest, before, after any) error {
	err := uc.audit.Record(ctx, audit.Entry{
		Actor: middleware.UserID(ctx), Action: action, EntityType: entityPaymentRequest, EntityID: r.ID,
		Before: before, After: after, IP: middleware.ClientIP(ctx),
	})
	if err != nil {
		return toAppError("audit "+action, err)
	}

	return nil
}
