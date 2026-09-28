package mail

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/smtp"
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
	header("From", address(from.Name, from.Email))
	header("To", address(msg.ToName, msg.ToEmail))
	header("Subject", mime.QEncoding.Encode("utf-8", msg.Subject))
	header("Date", time.Now().UTC().Format(time.RFC1123Z))
	header("MIME-Version", "1.0")

	if msg.Tag != "" {
		header("X-Vietgara-Tag", msg.Tag)
	}

	header("Content-Type", `multipart/alternative; boundary="`+boundary+`"`)
	b.WriteString("\r\n")

	part := func(contentType, body string) {
		if body == "" {
			return
		}

		fmt.Fprintf(&b, "--%s\r\nContent-Type: %s; charset=UTF-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n", boundary, contentType)

		qp := quotedprintable.NewWriter(&b)
		_, _ = qp.Write([]byte(body))
		_ = qp.Close()

		b.WriteString("\r\n")
	}
	part("text/plain", msg.Text)
	part("text/html", msg.HTML)
	fmt.Fprintf(&b, "--%s--\r\n", boundary)

	return b.Bytes()
}

func address(name, email string) string {
	if strings.TrimSpace(name) == "" {
		return "<" + email + ">"
	}

	return mime.QEncoding.Encode("utf-8", name) + " <" + email + ">"
}
