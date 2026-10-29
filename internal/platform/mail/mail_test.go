package mail_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/smtp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/platform/mail"
)

var from = mail.From{Email: "no-reply@vietgara.viettechno.com", Name: "VietGara"}

func validMessage() mail.Message {
	return mail.Message{ToEmail: "owner@example.com", ToName: "Nguyễn Minh", Subject: "Mã xác minh", Text: "Mã: 123456", HTML: "<p>Mã: 123456</p>", Tag: "email_verification"}
}

func TestNew(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     mail.Config
		wantErr string
	}{
		{name: "smtp", cfg: mail.Config{Provider: "smtp", From: from, SMTPHost: "localhost", SMTPPort: 1025}},
		{name: "mailjet", cfg: mail.Config{Provider: "mailjet", From: from, MailjetAPIKey: "k", MailjetSecretKey: "s"}},
		{name: "missing from", cfg: mail.Config{Provider: "smtp"}, wantErr: "MAIL_FROM_ADDRESS"},
		{name: "smtp without host", cfg: mail.Config{Provider: "smtp", From: from}, wantErr: "SMTP_HOST"},
		{name: "mailjet without keys", cfg: mail.Config{Provider: "mailjet", From: from}, wantErr: "MAILJET_API_KEY"},
		{name: "unknown provider", cfg: mail.Config{Provider: "carrier-pigeon", From: from}, wantErr: "unknown MAIL_PROVIDER"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sender, err := mail.New(tt.cfg)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)
			assert.NotNil(t, sender)
		})
	}
}

func TestSMTPSender(t *testing.T) {
	t.Parallel()

	t.Run("sends a multipart message without auth", func(t *testing.T) {
		t.Parallel()

		sender := mail.NewSMTPSender("mailpit", 1025, "", "", from)

		var (
			gotAddr, gotFrom string
			gotTo            []string
			gotMsg           []byte
			gotAuth          smtp.Auth
		)

		sender.SetSMTPSendFunc(func(addr string, a smtp.Auth, f string, to []string, msg []byte) error {
			gotAddr, gotAuth, gotFrom, gotTo, gotMsg = addr, a, f, to, msg

			return nil
		})

		require.NoError(t, sender.Send(t.Context(), validMessage()))
		assert.Equal(t, "mailpit:1025", gotAddr)
		assert.Nil(t, gotAuth)
		assert.Equal(t, from.Email, gotFrom)
		assert.Equal(t, []string{"owner@example.com"}, gotTo)

		raw := string(gotMsg)
		assert.Contains(t, raw, "multipart/alternative")
		assert.Contains(t, raw, "Content-Type: text/plain; charset=UTF-8")
		assert.Contains(t, raw, "Content-Type: text/html; charset=UTF-8")
		assert.Contains(t, raw, "X-Vietgara-Tag: email_verification")
		assert.Contains(t, raw, "=?utf-8?q?")
	})

	t.Run("sends a PDF attachment, a sender name, a reply address and extra headers", func(t *testing.T) {
		t.Parallel()

		sender := mail.NewSMTPSender("mailpit", 1025, "", "", from)

		var gotMsg []byte

		sender.SetSMTPSendFunc(func(_ string, _ smtp.Auth, _ string, _ []string, msg []byte) error {
			gotMsg = msg

			return nil
		})

		msg := validMessage()
		msg.FromName = "Gara Anh Minh"
		msg.ReplyTo = "gara@example.com"
		msg.Headers = map[string]string{"List-Unsubscribe": "<https://x/opt-out>", "List-Unsubscribe-Post": "List-Unsubscribe=One-Click"}
		msg.Attachments = []mail.Attachment{{Name: "bien-nhan.pdf", ContentType: "application/pdf", Data: []byte("%PDF-1.4 test")}}
		require.NoError(t, sender.Send(t.Context(), msg))

		raw := string(gotMsg)
		assert.Contains(t, raw, "From: Gara Anh Minh <no-reply@vietgara.viettechno.com>")
		assert.Contains(t, raw, "Reply-To: <gara@example.com>")
		assert.Contains(t, raw, "List-Unsubscribe: <https://x/opt-out>")
		assert.Contains(t, raw, "List-Unsubscribe-Post: List-Unsubscribe=One-Click")
		assert.Contains(t, raw, "multipart/mixed")
		assert.Contains(t, raw, "multipart/alternative")
		assert.Contains(t, raw, `Content-Disposition: attachment; filename="bien-nhan.pdf"`)
		assert.Contains(t, raw, "JVBERi0xLjQgdGVzdA==", "the attachment is base64")
		assert.Equal(t, 1, strings.Count(raw, "Content-Type: application/pdf"))
	})

	t.Run("uses plain auth and reports failures", func(t *testing.T) {
		t.Parallel()

		sender := mail.NewSMTPSender("smtp.example.com", 587, "user", "pass", mail.From{Email: from.Email})
		sender.SetSMTPSendFunc(func(_ string, a smtp.Auth, _ string, _ []string, _ []byte) error {
			assert.NotNil(t, a)

			return errMailTestTemporary
		})

		msg := validMessage()
		msg.ToName = ""
		require.ErrorContains(t, sender.Send(t.Context(), msg), "421 try later")
	})

	t.Run("rejects invalid messages and cancelled contexts", func(t *testing.T) {
		t.Parallel()

		sender := mail.NewSMTPSender("mailpit", 1025, "", "", from)
		require.ErrorIs(t, sender.Send(t.Context(), mail.Message{}), mail.ErrInvalidMessage)

		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		require.ErrorIs(t, sender.Send(ctx, validMessage()), context.Canceled)
	})
}

func TestMailjetSender(t *testing.T) {
	t.Parallel()

	t.Run("posts the Send API v3.1 payload", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, pass, ok := r.BasicAuth()
			assert.True(t, ok)
			assert.Equal(t, "api-key", user)
			assert.Equal(t, "secret", pass)

			var body map[string][]map[string]any
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			msg := body["Messages"][0]
			assert.Equal(t, "Mã xác minh", msg["Subject"])
			assert.Equal(t, "email_verification", msg["CustomID"])
			to, _ := msg["To"].([]any)
			recipient, _ := to[0].(map[string]any)
			assert.Equal(t, "owner@example.com", recipient["Email"])

			_, _ = w.Write([]byte(`{"Messages":[{"Status":"success"}]}`))
		}))
		t.Cleanup(srv.Close)

		sender := mail.NewMailjetSender("api-key", "secret", from, srv.URL)
		require.NoError(t, sender.Send(t.Context(), validMessage()))
	})

	t.Run("carries the attachment, headers, reply address and sender name", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body map[string][]map[string]any
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			msg := body["Messages"][0]

			sender, _ := msg["From"].(map[string]any)
			assert.Equal(t, "Gara Anh Minh", sender["Name"])
			assert.Equal(t, from.Email, sender["Email"])

			replyTo, _ := msg["ReplyTo"].(map[string]any)
			assert.Equal(t, "gara@example.com", replyTo["Email"])

			headers, _ := msg["Headers"].(map[string]any)
			assert.Equal(t, "<https://x/opt-out>", headers["List-Unsubscribe"])

			attachments, _ := msg["Attachments"].([]any)
			if assert.Len(t, attachments, 1) {
				attachment, _ := attachments[0].(map[string]any)
				assert.Equal(t, "bien-nhan.pdf", attachment["Filename"])
				assert.Equal(t, "application/pdf", attachment["ContentType"])
				assert.Equal(t, "JVBERi0xLjQgdGVzdA==", attachment["Base64Content"])
			}

			_, _ = w.Write([]byte(`{"Messages":[{"Status":"success"}]}`))
		}))
		t.Cleanup(srv.Close)

		msg := validMessage()
		msg.FromName = "Gara Anh Minh"
		msg.ReplyTo = "gara@example.com"
		msg.Headers = map[string]string{"List-Unsubscribe": "<https://x/opt-out>"}
		msg.Attachments = []mail.Attachment{{Name: "bien-nhan.pdf", Data: []byte("%PDF-1.4 test")}}
		msg.Attachments[0].ContentType = "application/pdf"

		require.NoError(t, mail.NewMailjetSender("api-key", "secret", from, srv.URL).Send(t.Context(), msg))
	})

	tests := []struct {
		name    string
		status  int
		body    string
		wantErr string
	}{
		{name: "http error", status: http.StatusUnauthorized, body: `{"ErrorMessage":"bad key"}`, wantErr: "status 401"},
		{name: "rejected message", status: http.StatusOK, body: `{"Messages":[{"Status":"error","Errors":[{"ErrorMessage":"invalid recipient"}]}]}`, wantErr: "invalid recipient"},
		{name: "rejected without detail", status: http.StatusOK, body: `{"Messages":[{"Status":"error"}]}`, wantErr: "rejected: error"},
		{name: "garbage", status: http.StatusOK, body: `nope`, wantErr: "decode response"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			t.Cleanup(srv.Close)

			err := mail.NewMailjetSender("k", "s", from, srv.URL).Send(t.Context(), validMessage())
			require.ErrorContains(t, err, tt.wantErr)
		})
	}

	t.Run("invalid message, bad URL and unreachable server", func(t *testing.T) {
		t.Parallel()

		require.ErrorIs(t, mail.NewMailjetSender("k", "s", from, "").Send(t.Context(), mail.Message{}), mail.ErrInvalidMessage)
		require.ErrorContains(t, mail.NewMailjetSender("k", "s", from, "://bad").Send(t.Context(), validMessage()), "request")
		require.ErrorContains(t, mail.NewMailjetSender("k", "s", from, "http://127.0.0.1:1").Send(t.Context(), validMessage()), "send")
	})
}

func TestRender(t *testing.T) {
	t.Parallel()

	otp := map[string]any{"Name": "Minh", "Code": "123456", "Minutes": 2}
	platformInvite := map[string]any{"Name": "Lan", "GroupName": "Billing <support>", "InviterName": "Minh", "AcceptURL": "https://admin/invitations/1", "ExpiresAt": "02/10/2026"}
	support := map[string]any{"Name": "Lan", "GarageName": "Gara <A>", "StaffName": "Minh", "StaffEmail": "m@x.test", "Reason": "ticket 7", "Hours": 24, "ReviewURL": "https://app/support-access?garageId=1", "ExpiresAt": "02/10/2026"}
	answered := map[string]any{"Name": "Lan", "GarageName": "Gara <A>", "Approved": true, "ExpiresAt": "02/10/2026"}
	customerMail := map[string]any{
		"CustomerName": "An", "VehiclePlate": "51A-111.11", "GarageName": "Gara <A>", "GaragePhone": "0901234567", "GarageAddress": "1 Le Loi",
		"Number": "ST-001", "Amount": "500.000", "Balance": "300.000", "Total": "800.000", "Attached": true, "OptOutURL": "https://x/opt-out",
	}
	staffMail := map[string]any{
		"Name": "Lan", "CustomerName": "An", "Number": "BG-001", "VehiclePlate": "51A-111.11", "Approved": true, "ConsoleURL": "https://app",
		"Total": "800.000", "PartName": "Lọc dầu <X>", "SKU": "LD-01", "GarageName": "Gara <A>", "Quantity": "3", "Threshold": "4",
	}
	invite := map[string]any{"Name": "Lan", "GarageName": "Gara <A>", "InviterName": "Minh", "AcceptURL": "https://app/invitations/1", "ExpiresAt": "02/10/2026"}

	tests := []struct {
		name, template, locale string
		data                   any
		wantSubject            string
		wantText               string
	}{
		{"vi verification", mail.TemplateEmailVerification, "vi", otp, "Mã xác minh email VietGara: 123456", "hiệu lực trong 2 phút"},
		{"en verification", mail.TemplateEmailVerification, "en", otp, "Your VietGara verification code: 123456", "valid for 2 minutes"},
		{"unknown locale falls back to vi", mail.TemplatePasswordReset, "fr", otp, "Mã đặt lại mật khẩu VietGara: 123456", "đặt lại mật khẩu"},
		{"en reset", mail.TemplatePasswordReset, "en", otp, "Your VietGara password reset code: 123456", "reset code"},
		{"vi invitation", mail.TemplateStaffInvitation, "vi", invite, "Minh mời bạn tham gia Gara <A> trên VietGara", "https://app/invitations/1"},
		{"en invitation", mail.TemplateStaffInvitation, "en", invite, "Minh invited you to join Gara <A> on VietGara", "02/10/2026"},
		{"vi platform invitation", mail.TemplatePlatformStaffInvitation, "vi", platformInvite, "Minh mời bạn vào đội ngũ nền tảng VietGara", "https://admin/invitations/1"},
		{"en platform invitation", mail.TemplatePlatformStaffInvitation, "en", platformInvite, "Minh invited you to the VietGara platform team", "Billing <support>"},
		{"vi support request", mail.TemplateSupportGrantRequested, "vi", support, "Bộ phận hỗ trợ VietGara xin quyền xem Gara <A>", "https://app/support-access"},
		{"en support request", mail.TemplateSupportGrantRequested, "en", support, "VietGara support asks to read Gara <A>", "ticket 7"},
		{"vi support opened", mail.TemplateSupportGrantOpened, "vi", support, "VietGara đã mở Gara <A> để hỗ trợ", "02/10/2026"},
		{"en support opened", mail.TemplateSupportGrantOpened, "en", support, "VietGara opened Gara <A> for support", "02/10/2026"},
		{"vi support approved", mail.TemplateSupportGrantAnswered, "vi", answered, "Quyền hỗ trợ Gara <A> của bạn đã được đồng ý", "02/10/2026"},
		{"vi vehicle ready", mail.TemplateVehicleReady, "vi", customerMail, "Xe 51A-111.11 của bạn đã sẵn sàng", "https://x/opt-out"},
		{"en vehicle ready", mail.TemplateVehicleReady, "en", customerMail, "Your vehicle 51A-111.11 is ready", "Address: 1 Le Loi"},
		{"vi payment received", mail.TemplatePaymentReceived, "vi", customerMail, "Gara <A> đã nhận 500.000 đ cho xe 51A-111.11", "Số tiền còn lại: 300.000 đ"},
		{"en payment received", mail.TemplatePaymentReceived, "en", customerMail, "Gara <A> received 500.000 VND for 51A-111.11", "Balance left: 300.000 VND"},
		{"vi receipt attached", mail.TemplateSettlementReceipt, "vi", customerMail, "Biên nhận ST-001 cho xe 51A-111.11", "đính kèm dưới dạng PDF"},
		{"en receipt not attached", mail.TemplateSettlementReceipt, "en", map[string]any{"CustomerName": "An", "VehiclePlate": "51A-111.11", "GarageName": "Gara <A>", "Number": "ST-001", "Total": "800.000", "Attached": false}, "Receipt ST-001 for 51A-111.11", "could not be attached"},
		{"vi quote approved", mail.TemplateQuoteDecided, "vi", staffMail, "Báo giá BG-001 đã được duyệt", "đã duyệt báo giá BG-001"},
		{"en quote rejected", mail.TemplateQuoteDecided, "en", map[string]any{"Name": "Lan", "CustomerName": "An", "Number": "BG-001", "VehiclePlate": "51A-111.11", "Approved": false, "ConsoleURL": "https://app"}, "Quote BG-001 was rejected", "rejected quote BG-001"},
		{"vi settlement paid", mail.TemplateSettlementPaid, "vi", staffMail, "Quyết toán BG-001 đã thanh toán đủ", "800.000 đ"},
		{"en settlement paid", mail.TemplateSettlementPaid, "en", staffMail, "Settlement BG-001 is paid in full", "800.000 VND"},
		{"vi low stock", mail.TemplateLowStock, "vi", staffMail, "Sắp hết hàng: Lọc dầu <X>", "ngưỡng đặt hàng 4"},
		{"en low stock", mail.TemplateLowStock, "en", staffMail, "Low stock: Lọc dầu <X>", "reorder threshold of 4"},
		{"en support declined", mail.TemplateSupportGrantAnswered, "en", map[string]any{"Name": "Lan", "GarageName": "Gara <A>", "Approved": false, "ExpiresAt": ""}, "Your support access to Gara <A> was declined", "declined your request"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			msg, err := mail.Render(tt.template, tt.locale, "to@example.com", "To", tt.data)
			require.NoError(t, err)
			assert.Equal(t, tt.wantSubject, msg.Subject)
			assert.Contains(t, msg.Text, tt.wantText)
			assert.NotEmpty(t, msg.HTML)
			assert.Equal(t, tt.template, msg.Tag)
			assert.NotContains(t, msg.HTML, "<A>", "html is escaped")
			assert.NotContains(t, msg.HTML, "<support>", "html is escaped")
		})
	}

	_, err := mail.Render("nope", "vi", "a@b.c", "", otp)
	require.ErrorContains(t, err, "unknown template")

	_, err = mail.Render(mail.TemplateEmailVerification, "vi", "a@b.c", "", map[string]any{})
	require.ErrorContains(t, err, "render")
}
