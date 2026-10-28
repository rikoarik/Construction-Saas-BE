// Package email delivers authentication messages through an explicitly
// configured SMTP relay. It never logs recipients or message bodies.
package email

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"
)

// Message is a transactional email with a plain-text fallback.
type Message struct {
	To        string
	Subject   string
	Body      string
	HTMLBody  string
	MessageID string
}

// Sender performs one delivery attempt. Implementations must honor the context.
type Sender interface {
	Send(context.Context, Message) error
}

// SMTPConfig describes an implicit-TLS or STARTTLS relay. Plaintext submission is refused.
type SMTPConfig struct {
	Addr     string
	Host     string
	Username string
	Password string
	From     string
	TLS      *tls.Config
	Timeout  time.Duration
	// ImplicitTLS connects with TLS immediately (SMTPS, typically port 465).
	// When false the sender requires a successful STARTTLS upgrade.
	ImplicitTLS bool
}

type SMTPSender struct {
	config SMTPConfig
}

func NewSMTPSender(config SMTPConfig) (*SMTPSender, error) {
	if config.Addr == "" || config.From == "" {
		return nil, errors.New("SMTP address and sender are required")
	}
	from, err := mail.ParseAddress(config.From)
	if err != nil || from.Address != config.From || strings.ContainsAny(config.From, "\r\n") {
		return nil, errors.New("SMTP sender must be a single email address")
	}
	if config.Host == "" {
		return nil, errors.New("SMTP host is required for certificate validation")
	}
	if config.Timeout <= 0 {
		config.Timeout = 10 * time.Second
	}
	if config.TLS == nil {
		config.TLS = &tls.Config{MinVersion: tls.VersionTLS12, ServerName: config.Host}
	}
	if config.TLS.ServerName == "" {
		config.TLS.ServerName = config.Host
	}
	if _, _, err := net.SplitHostPort(config.Addr); err != nil {
		return nil, fmt.Errorf("invalid SMTP address: %w", err)
	}
	return &SMTPSender{config: config}, nil
}

func (s *SMTPSender) Send(ctx context.Context, message Message) error {
	if err := validateMessage(message); err != nil {
		return err
	}
	client, err := s.dial(ctx)
	if err != nil {
		return err
	}
	defer client.Close()
	if !s.config.ImplicitTLS {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return errors.New("SMTP server does not offer STARTTLS")
		}
		if err := client.StartTLS(s.config.TLS); err != nil {
			return fmt.Errorf("start SMTP TLS: %w", err)
		}
	}
	if s.config.Username != "" {
		auth := smtp.PlainAuth("", s.config.Username, s.config.Password, s.config.Host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("SMTP authentication failed: %w", err)
		}
	}
	if err := client.Mail(s.config.From); err != nil {
		return fmt.Errorf("SMTP envelope sender rejected: %w", err)
	}
	if err := client.Rcpt(message.To); err != nil {
		return fmt.Errorf("SMTP recipient rejected: %w", err)
	}
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("SMTP data rejected: %w", err)
	}
	if _, err := writer.Write(render(s.config.From, message)); err != nil {
		_ = writer.Close()
		return fmt.Errorf("write SMTP message: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("finish SMTP message: %w", err)
	}
	_ = client.Quit()
	return nil
}

func (s *SMTPSender) dial(ctx context.Context) (*smtp.Client, error) {
	dialer := &net.Dialer{Timeout: s.config.Timeout}
	deadline := time.Now().Add(s.config.Timeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	var conn net.Conn
	var err error
	if s.config.ImplicitTLS {
		tlsDialer := &tls.Dialer{NetDialer: dialer, Config: s.config.TLS}
		conn, err = tlsDialer.DialContext(ctx, "tcp", s.config.Addr)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", s.config.Addr)
	}
	if err != nil {
		return nil, fmt.Errorf("dial SMTP: %w", err)
	}
	_ = conn.SetDeadline(deadline)
	client, err := smtp.NewClient(conn, s.config.Host)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("start SMTP session: %w", err)
	}
	return client, nil
}

// Ping verifies connectivity, TLS, and authentication without sending mail.
func (s *SMTPSender) Ping(ctx context.Context) error {
	client, err := s.dial(ctx)
	if err != nil {
		return err
	}
	defer client.Close()
	if !s.config.ImplicitTLS {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return errors.New("SMTP server does not offer STARTTLS")
		}
		if err := client.StartTLS(s.config.TLS); err != nil {
			return fmt.Errorf("start SMTP TLS: %w", err)
		}
	}
	if s.config.Username != "" {
		auth := smtp.PlainAuth("", s.config.Username, s.config.Password, s.config.Host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("SMTP authentication failed: %w", err)
		}
	}
	if err := client.Noop(); err != nil {
		return fmt.Errorf("SMTP noop failed: %w", err)
	}
	_ = client.Quit()
	return nil
}

func validateMessage(message Message) error {
	for name, value := range map[string]string{
		"recipient": message.To,
		"subject":   message.Subject,
		"sender":    message.MessageID,
	} {
		if strings.ContainsAny(value, "\r\n") {
			return fmt.Errorf("%s contains a newline", name)
		}
	}
	if !strings.Contains(message.To, "@") || message.Subject == "" {
		return errors.New("recipient and subject are required")
	}
	return nil
}

func render(from string, message Message) []byte {
	messageID := message.MessageID
	if messageID == "" {
		var raw [16]byte
		_, _ = rand.Read(raw[:])
		messageID = "<" + hex.EncodeToString(raw[:]) + "@construction-saas.invalid>"
	}
	headers := []string{
		"From: " + from,
		"To: " + message.To,
		"Subject: " + mime.QEncoding.Encode("utf-8", message.Subject),
		"Message-ID: " + messageID,
		"MIME-Version: 1.0",
		"Date: " + time.Now().UTC().Format(time.RFC1123Z),
	}
	if message.HTMLBody == "" {
		headers = append(headers, "Content-Type: text/plain; charset=utf-8", "Content-Transfer-Encoding: 8bit")
		return []byte(strings.Join(headers, "\r\n") + "\r\n\r\n" + message.Body + "\r\n")
	}
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	boundary := writer.Boundary()
	for _, part := range []struct{ kind, body string }{{"plain", message.Body}, {"html", message.HTMLBody}} {
		h, err := writer.CreatePart(map[string][]string{
			"Content-Type":              {"text/" + part.kind + "; charset=utf-8"},
			"Content-Transfer-Encoding": {"quoted-printable"},
		})
		if err != nil {
			return nil
		}
		encoded := quotedprintable.NewWriter(h)
		_, _ = encoded.Write([]byte(part.body))
		_ = encoded.Close()
	}
	_ = writer.Close()
	headers = append(headers, "Content-Type: multipart/alternative; boundary="+boundary)
	return append([]byte(strings.Join(headers, "\r\n")+"\r\n\r\n"), buf.Bytes()...)
}
