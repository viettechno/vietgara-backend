package mail

import (
	"bytes"
	"embed"
	"fmt"
	htmltemplate "html/template"
	"strings"
	texttemplate "text/template"
)

// Template names.
const (
	TemplateEmailVerification        = "email_verification"
	TemplatePasswordReset            = "password_reset"
	TemplateCustomerLink             = "customer_link"
	TemplateStaffInvitation          = "staff_invitation"
	TemplateQuoteReadyForApproval    = "quote_ready_for_approval"
	TemplateRegistrationConfirmation = "registration_confirmation"
	TemplateSubscriptionExpiringSoon = "subscription_expiring_soon"
)

// Supported locales; Vietnamese is the default.
const (
	LocaleVI = "vi"
	LocaleEN = "en"
)

//go:embed templates
var templateFS embed.FS

// Render builds a message from the template name in locale (falling back to
// Vietnamese) with data, addressed to toEmail/toName.
func Render(name, locale, toEmail, toName string, data any) (Message, error) {
	if locale != LocaleEN {
		locale = LocaleVI
	}

	raw, err := templateFS.ReadFile("templates/" + locale + "/" + name + ".tmpl")
	if err != nil {
		return Message{}, fmt.Errorf("mail: unknown template %s/%s: %w", locale, name, err)
	}

	text, err := texttemplate.New(name).Option("missingkey=error").Parse(string(raw))
	if err != nil {
		return Message{}, fmt.Errorf("mail: parse %s: %w", name, err)
	}

	html, err := htmltemplate.New(name).Option("missingkey=error").Parse(string(raw))
	if err != nil {
		return Message{}, fmt.Errorf("mail: parse %s: %w", name, err)
	}

	msg := Message{ToEmail: toEmail, ToName: toName, Tag: name}

	for _, part := range []struct {
		block string
		dst   *string
		exec  func(*bytes.Buffer) error
	}{
		{"subject", &msg.Subject, func(b *bytes.Buffer) error { return text.ExecuteTemplate(b, "subject", data) }},
		{"text", &msg.Text, func(b *bytes.Buffer) error { return text.ExecuteTemplate(b, "text", data) }},
		{"html", &msg.HTML, func(b *bytes.Buffer) error { return html.ExecuteTemplate(b, "html", data) }},
	} {
		var b bytes.Buffer
		if err := part.exec(&b); err != nil {
			return Message{}, fmt.Errorf("mail: render %s %s: %w", name, part.block, err)
		}

		*part.dst = strings.TrimSpace(b.String())
	}

	return msg, nil
}
