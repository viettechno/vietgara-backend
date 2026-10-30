package handler

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"html"
	"io"
	"log/slog"
	"net/http"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"

	"github.com/viettechno/vietgara-backend/internal/notification/usecase"
	"github.com/viettechno/vietgara-backend/internal/platform/httpapi"
)

// Paths of the plain HTTP routes, which need no sign-in.
const (
	// OptOutPath is the page of the link in a customer e-mail.
	OptOutPath = usecase.OptOutPath + "{token}"
	// MailjetWebhookPath receives the provider's delivery events; the secret
	// in it is the credential.
	MailjetWebhookPath = "/api/v1/webhooks/mailjet/{secret}"

	maxWebhookBytes = 1 << 20
	errorKey        = "error"
)

// RegisterHTTPRoutes adds the opt-out page and, when webhookSecret is set, the
// provider's webhook to mux.
func (s *Server) RegisterHTTPRoutes(mux *runtime.ServeMux, webhookSecret string) error {
	if err := mux.HandlePath(http.MethodGet, OptOutPath, func(w http.ResponseWriter, _ *http.Request, params map[string]string) {
		s.optOutPage(w, params["token"])
	}); err != nil {
		return err
	}

	if err := mux.HandlePath(http.MethodPost, OptOutPath, func(w http.ResponseWriter, r *http.Request, params map[string]string) {
		s.optOut(w, r, params["token"])
	}); err != nil {
		return err
	}

	if webhookSecret == "" {
		return nil
	}

	return mux.HandlePath(http.MethodPost, MailjetWebhookPath, func(w http.ResponseWriter, r *http.Request, params map[string]string) {
		s.mailjetWebhook(w, r, params["secret"], webhookSecret)
	})
}

// optOutPage shows the one button that confirms the opt-out. It changes
// nothing: a mail scanner that opens the link must not opt anyone out.
func (s *Server) optOutPage(w http.ResponseWriter, token string) {
	if _, _, ok := s.uc.VerifyOptOutToken(token); !ok {
		notFound(w)

		return
	}

	writePage(w, "Hủy nhận e-mail / Unsubscribe", `<p>Bạn không muốn nhận loại e-mail này từ gara nữa?</p>
<p>Do you no longer want these e-mails from the garage?</p>
<form method="post" action="`+html.EscapeString(optOutAction(token))+`"><button type="submit">Hủy nhận / Unsubscribe</button></form>`)
}

func optOutAction(token string) string { return usecase.OptOutPath + token }

// optOut opts the customer out (the page's button, and the mail client's
// one-click request).
func (s *Server) optOut(w http.ResponseWriter, r *http.Request, token string) {
	if err := s.uc.OptOut(r.Context(), token); err != nil {
		if errors.Is(err, usecase.ErrInvalidOptOutToken) {
			notFound(w)

			return
		}

		slog.ErrorContext(r.Context(), "notification: opting out failed", "error", err)
		httpapi.WriteJSON(w, http.StatusInternalServerError, map[string]string{errorKey: "internal"})

		return
	}

	writePage(w, "Đã hủy nhận / Unsubscribed", `<p>Bạn sẽ không nhận các e-mail này từ gara nữa. E-mail báo giá và biên nhận vẫn được gửi.</p>
<p>You will no longer get these e-mails from the garage. Quote and receipt e-mails are still sent.</p>`)
}

func writePage(w http.ResponseWriter, title, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.WriteString(w, `<!doctype html><html lang="vi"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>`+
		html.EscapeString(title)+`</title><body><h1>`+html.EscapeString(title)+`</h1>`+body+`</body></html>`)
}

func notFound(w http.ResponseWriter) {
	httpapi.WriteJSON(w, http.StatusNotFound, map[string]any{"error": map[string]string{"code": "NOT_FOUND", "message": "not found"}})
}

// mailjetEvent is one entry of the provider's event webhook.
type mailjetEvent struct {
	Event      string `json:"event"`
	Email      string `json:"email"`
	HardBounce bool   `json:"hard_bounce"`
}

// mailjetWebhook suppresses the addresses of hard bounces and spam complaints;
// other events are ignored: a block is often the sender's, or temporary, and
// must not silence a customer for good. A wrong secret answers 404.
func (s *Server) mailjetWebhook(w http.ResponseWriter, r *http.Request, given, want string) {
	if subtle.ConstantTimeCompare([]byte(given), []byte(want)) != 1 {
		notFound(w)

		return
	}

	raw, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBytes))
	if err != nil {
		httpapi.WriteJSON(w, http.StatusBadRequest, map[string]string{errorKey: "unreadable body"})

		return
	}

	for _, e := range parseMailjetEvents(raw) {
		reason := suppressionReason(e)
		if reason == "" {
			continue
		}

		if err := s.uc.Suppress(r.Context(), e.Email, reason); err != nil {
			slog.ErrorContext(r.Context(), "notification: suppressing an address failed", "error", err)
			httpapi.WriteJSON(w, http.StatusInternalServerError, map[string]string{errorKey: "internal"})

			return
		}
	}

	httpapi.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// parseMailjetEvents reads the array Mailjet posts, or a single event.
func parseMailjetEvents(raw []byte) []mailjetEvent {
	var many []mailjetEvent
	if err := json.Unmarshal(raw, &many); err == nil {
		return many
	}

	var one mailjetEvent
	if err := json.Unmarshal(raw, &one); err == nil && one.Event != "" {
		return []mailjetEvent{one}
	}

	return nil
}

func suppressionReason(e mailjetEvent) string {
	switch {
	case e.Event == "bounce" && e.HardBounce:
		return "bounce"
	case e.Event == "spam":
		return "spam"
	default:
		return ""
	}
}
