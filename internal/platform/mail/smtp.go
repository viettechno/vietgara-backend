package mail

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/smtp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// SMTPSender delivers through an SMTP server. Locally and in integration
// tests that is Mailpit (no authentication, no TLS); authentication is used
// when a username is set.
type SMTPSender struct {
	addr     string
	host     string
	username string
	password string
	from     From
	send     func(addr string, a smtp.Auth, from string, to []string, msg []byte) error
}

// NewSMTPSender returns an SMTPSender for host:port.
func NewSMTPSender(host string, port int, username, password string, from From) *SMTPSender {
	return &SMTPSender{
		addr: net.JoinHostPort(host, strconv.Itoa(port)), host: host,
		username: username, password: password, from: from, send: smtp.SendMail,
	}
}

// Send delivers msg as a multipart/alternative (text + HTML) message.
func (s *SMTPSender) Send(ctx context.Context, msg Message) error {
	if err := validate(msg); err != nil {
		return err
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	var auth smtp.Auth
	if s.username != "" {
		auth = smtp.PlainAuth("", s.username, s.password, s.host)
	}

	if err := s.send(s.addr, auth, s.from.Email, []string{msg.ToEmail}, buildMIME(s.from, msg)); err != nil {
		return fmt.Errorf("smtp send: %w", err)
	}

	return nil
}

func buildMIME(from From, msg Message) []byte {
	boundaryBytes := make([]byte, 12)
	_, _ = rand.Read(boundaryBytes)
	boundary := "vietgara-" + hex.EncodeToString(boundaryBytes)

	var b bytes.Buffer

	header := func(k, v string) { fmt.Fprintf(&b, "%s: %s\r\n", k, v) }

	fromName := from.Name
	if msg.FromName != "" {
		fromName = msg.FromName
	}

	header("From", address(fromName, from.Email))
	header("To", address(msg.ToName, msg.ToEmail))

	if msg.ReplyTo != "" {
		header("Reply-To", "<"+msg.ReplyTo+">")
	}

	for _, name := range sortedKeys(msg.Headers) {
		header(name, msg.Headers[name])
	}

	header("Subject", mime.QEncoding.Encode("utf-8", msg.Subject))
	header("Date", time.Now().UTC().Format(time.RFC1123Z))
	header("MIME-Version", "1.0")

	if msg.Tag != "" {
		header("X-Vietgara-Tag", msg.Tag)
	}

	alternative := boundary
	if len(msg.Attachments) > 0 {
		alternative = boundary + "-alt"

		header("Content-Type", `multipart/mixed; boundary="`+boundary+`"`)
		b.WriteString("\r\n")
		fmt.Fprintf(&b, "--%s\r\nContent-Type: multipart/alternative; boundary=\"%s\"\r\n\r\n", boundary, alternative)
	} else {
		header("Content-Type", `multipart/alternative; boundary="`+boundary+`"`)
		b.WriteString("\r\n")
	}

	part := func(contentType, body string) {
		if body == "" {
			return
		}

		fmt.Fprintf(&b, "--%s\r\nContent-Type: %s; charset=UTF-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n", alternative, contentType)

		qp := quotedprintable.NewWriter(&b)
		_, _ = qp.Write([]byte(body))
		_ = qp.Close()

		b.WriteString("\r\n")
	}
	part("text/plain", msg.Text)
	part("text/html", msg.HTML)
	fmt.Fprintf(&b, "--%s--\r\n", alternative)

	for _, a := range msg.Attachments {
		writeAttachment(&b, boundary, a)
	}

	if len(msg.Attachments) > 0 {
		fmt.Fprintf(&b, "--%s--\r\n", boundary)
	}

	return b.Bytes()
}

func writeAttachment(b *bytes.Buffer, boundary string, a Attachment) {
	contentType := a.ContentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	name := mime.QEncoding.Encode("utf-8", a.Name)
	fmt.Fprintf(b, "--%s\r\nContent-Type: %s; name=%q\r\nContent-Transfer-Encoding: base64\r\n", boundary, contentType, name)
	fmt.Fprintf(b, "Content-Disposition: attachment; filename=%q\r\n\r\n", name)

	encoded := base64.StdEncoding.EncodeToString(a.Data)
	for len(encoded) > 76 {
		b.WriteString(encoded[:76] + "\r\n")

		encoded = encoded[76:]
	}

	b.WriteString(encoded + "\r\n")
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	return keys
}

func address(name, email string) string {
	if strings.TrimSpace(name) == "" {
		return "<" + email + ">"
	}

	return mime.QEncoding.Encode("utf-8", name) + " <" + email + ">"
}
