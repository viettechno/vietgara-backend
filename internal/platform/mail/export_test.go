package mail

import "net/smtp"

// SetSMTPSendFunc replaces smtp.SendMail in tests.
func (s *SMTPSender) SetSMTPSendFunc(fn func(addr string, a smtp.Auth, from string, to []string, msg []byte) error) {
	s.send = fn
}
