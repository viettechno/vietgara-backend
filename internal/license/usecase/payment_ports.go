package usecase

import (
	"context"
	"time"

	"github.com/viettechno/vietgara-backend/internal/license/domain"
)

// ReviewFilter narrows the platform admin's review queue.
type ReviewFilter struct {
	Status domain.PaymentStatus
	// Query matches the owner's e-mail or name, or the transfer reference.
	Query    string
	Page     int
	PageSize int
}

// ReviewItem is one request of the review queue with its owner.
type ReviewItem struct {
	Request  *domain.PlanPaymentRequest
	Email    string
	FullName string
}

// ReviewPage is one page of the review queue.
type ReviewPage struct {
	Items []ReviewItem
	Total int
}

// PaymentRepository persists payment requests, the account credit ledger and
// the platform's payment settings (FR-LIC-07..10). Reads load the request's
// plan; finders return domain.ErrPaymentRequestNotFound.
type PaymentRepository interface {
	// CreateRequest inserts r, filling its id. An owner with an open request
	// returns domain.ErrRequestAlreadyOpen; a taken reference
	// domain.ErrReferenceTaken.
	CreateRequest(ctx context.Context, r *domain.PlanPaymentRequest) error
	FindRequest(ctx context.Context, id string) (*domain.PlanPaymentRequest, error)
	// LockRequest is FindRequest holding a row lock until the transaction
	// ends.
	LockRequest(ctx context.Context, id string) (*domain.PlanPaymentRequest, error)
	// FindOpenRequest returns the owner's awaiting_payment or submitted
	// request.
	FindOpenRequest(ctx context.Context, userID string) (*domain.PlanPaymentRequest, error)
	// SaveRequest persists the status, declaration and review fields.
	SaveRequest(ctx context.Context, r *domain.PlanPaymentRequest) error
	// ListRequests returns the owner's requests, newest first.
	ListRequests(ctx context.Context, userID string, status domain.PaymentStatus) ([]*domain.PlanPaymentRequest, error)
	// OpenRequests returns the open request of each of the subscriptions, by
	// subscription id.
	OpenRequests(ctx context.Context, subscriptionIDs []string) (map[string]*domain.PlanPaymentRequest, error)
	// SearchRequests is the review queue: oldest submitted first.
	SearchRequests(ctx context.Context, f ReviewFilter) (*ReviewPage, error)
	// WaitingForReview counts the submitted requests and the oldest
	// submission (zero when none).
	WaitingForReview(ctx context.Context) (int, time.Time, error)
	// AddCreditEntry appends to the ledger, filling its id and time.
	AddCreditEntry(ctx context.Context, e *domain.CreditEntry) error
	// ListCredit returns the owner's ledger, newest first.
	ListCredit(ctx context.Context, userID string) ([]*domain.CreditEntry, error)
	// ClaimReviewReminder reports whether the admins are due a reminder and,
	// if so, records it: at most once per minInterval across instances.
	ClaimReviewReminder(ctx context.Context, now time.Time, minInterval time.Duration) (bool, error)
	// Settings returns the platform's payment details.
	Settings(ctx context.Context) (*domain.PaymentSettings, error)
	// SaveSettings persists the details and the QR file.
	SaveSettings(ctx context.Context, s *domain.PaymentSettings, updatedBy string) error
}

// FileStore keeps the private files of payments: the evidence of a transfer
// (owned by the owner) and the platform's QR code (owned by the admin who
// uploaded it). It is implemented in cmd/server over the File module.
type FileStore interface {
	// StoreEvidence registers content as the owner's payment evidence and
	// returns its file id. domain.ErrInvalidEvidence when not an accepted
	// file.
	StoreEvidence(ctx context.Context, userID string, content []byte, contentType string) (string, error)
	// StoreQR registers content as the platform's QR code.
	StoreQR(ctx context.Context, adminID string, content []byte, contentType string) (string, error)
	// SignedURL returns a short-lived URL of a stored file.
	SignedURL(ctx context.Context, fileID string) (string, error)
}
