// Package usecase stores the contact requests of the public website and tells
// support about them (FR-WEB-02, FR-WEB-04).
package usecase

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/viettechno/vietgara-backend/internal/contactrequest/domain"
	"github.com/viettechno/vietgara-backend/internal/platform/mail"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

const (
	// MaxPerHour is how many requests one source may send in an hour. The gateway
	// limits the rate; this bounds what is stored and e-mailed.
	MaxPerHour = 10

	maxUserAgent = 300
	tag          = "contact_request"
)

// Repository stores the requests.
type Repository interface {
	Create(ctx context.Context, r *domain.Request) error
	// CountSince counts the requests of a source created at or after since.
	CountSince(ctx context.Context, sourceIPHash string, since time.Time) (int, error)
}

// Mailer delivers an e-mail.
type Mailer interface {
	Send(ctx context.Context, msg mail.Message) error
}

// UseCase creates contact requests.
type UseCase struct {
	repo    Repository
	mailer  Mailer
	support string
	ipKey   []byte
	now     func() time.Time
}

// New returns a use case that e-mails support at supportEmail and hashes the
// source addresses with ipKey.
func New(repo Repository, mailer Mailer, supportEmail string, ipKey []byte) *UseCase {
	return &UseCase{repo: repo, mailer: mailer, support: supportEmail, ipKey: ipKey, now: time.Now}
}

// Source is who sent the request, as the gateway saw it.
type Source struct {
	IP        string
	UserAgent string
}

// Create validates and stores a request and e-mails support, and returns its id.
// A source over its hourly limit is refused. A failed e-mail does not fail the
// request: it is stored, and support finds it there.
func (uc *UseCase) Create(ctx context.Context, in domain.Input, src Source) (string, error) {
	in, err := in.Normalize()
	if err != nil {
		return "", err
	}

	now := uc.now().UTC()
	hash := uc.hashIP(src.IP)

	sent, err := uc.repo.CountSince(ctx, hash, now.Add(-time.Hour))
	if err != nil {
		return "", apperror.Internal("count contact requests", err)
	}

	if sent >= MaxPerHour {
		return "", apperror.New(apperror.CodeResourceExhausted, "too many requests, try again later")
	}

	req := &domain.Request{
		ID: uuid.NewString(), Name: in.Name, GarageName: in.GarageName, Phone: in.Phone, Email: in.Email,
		Message: in.Message, Locale: in.Locale, ConsentAt: now, SourceIPHash: hash,
		UserAgent: truncate(src.UserAgent, maxUserAgent), Status: domain.StatusNew,
	}

	if err := uc.repo.Create(ctx, req); err != nil {
		return "", apperror.Internal("create contact request", err)
	}

	if err := uc.mailer.Send(ctx, uc.supportMail(req)); err != nil {
		slog.ErrorContext(ctx, "contactrequest: e-mail to support failed", "request_id", req.ID, "error", err)
	}

	return req.ID, nil
}

func (uc *UseCase) hashIP(ip string) string {
	mac := hmac.New(sha256.New, uc.ipKey)
	mac.Write([]byte("contact-request-source\n" + ip))

	return hex.EncodeToString(mac.Sum(nil))
}

func (uc *UseCase) supportMail(r *domain.Request) mail.Message {
	subject := "[VietGara] Yêu cầu liên hệ: " + r.Name
	if r.GarageName != "" {
		subject += " (" + r.GarageName + ")"
	}

	var b strings.Builder

	fmt.Fprintf(&b, "Tên: %s\nGara: %s\nĐiện thoại: %s\nE-mail: %s\nNgôn ngữ: %s\nMã yêu cầu: %s\n\n%s\n",
		r.Name, orDash(r.GarageName), r.Phone, orDash(r.Email), r.Locale, r.ID, orDash(r.Message))

	return mail.Message{ToEmail: uc.support, Subject: subject, Text: b.String(), Tag: tag, ReplyTo: r.Email}
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}

	return s
}

func truncate(s string, maxRunes int) string {
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}

	return string([]rune(s)[:maxRunes])
}
