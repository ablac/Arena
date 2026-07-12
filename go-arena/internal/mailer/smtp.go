package mailer

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"html"
	"io"
	"mime/multipart"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"net/url"
	"strings"
	"time"
)

const smtpOperationTimeout = 15 * time.Second

type SMTPConfig struct {
	Host          string
	Port          int
	TLSMode       string
	TLSServerName string
	Username      string
	Password      string
	From          string
}

type SMTPMagicLinkSender struct {
	config SMTPConfig
	from   *mail.Address
	now    func() time.Time
	rand   io.Reader
}

func NewSMTPMagicLinkSender(config SMTPConfig) (*SMTPMagicLinkSender, error) {
	config.Host = strings.TrimSpace(config.Host)
	config.TLSMode = strings.ToLower(strings.TrimSpace(config.TLSMode))
	config.TLSServerName = strings.TrimSpace(config.TLSServerName)
	config.Username = strings.TrimSpace(config.Username)
	config.From = strings.TrimSpace(config.From)
	if config.Host == "" || config.Port <= 0 || config.Port > 65535 {
		return nil, fmt.Errorf("SMTP host and port are required")
	}
	if config.TLSMode != "implicit" && config.TLSMode != "starttls" {
		return nil, fmt.Errorf("SMTP transport must use implicit TLS or STARTTLS")
	}
	if config.TLSServerName == "" {
		return nil, fmt.Errorf("SMTP TLS server name is required")
	}
	username, err := mail.ParseAddress(config.Username)
	if err != nil || username.Address != config.Username {
		return nil, fmt.Errorf("parse SMTP username as mailbox address")
	}
	if config.Password == "" {
		return nil, fmt.Errorf("SMTP app password is required")
	}
	from, err := mail.ParseAddress(strings.TrimSpace(config.From))
	if err != nil || from.Address == "" {
		return nil, fmt.Errorf("parse SMTP From address: %w", err)
	}
	if !strings.EqualFold(from.Address, username.Address) {
		return nil, fmt.Errorf("SMTP From address must equal the authenticated mailbox")
	}
	return &SMTPMagicLinkSender{config: config, from: from, now: time.Now, rand: rand.Reader}, nil
}

func (s *SMTPMagicLinkSender) SendMagicLink(ctx context.Context, to, displayName, magicLink string, expiresIn time.Duration) error {
	recipient, err := mail.ParseAddress(strings.TrimSpace(to))
	if err != nil || recipient.Address != strings.TrimSpace(to) {
		return fmt.Errorf("parse recipient address: %w", err)
	}
	if !validMagicLinkURL(magicLink) {
		return fmt.Errorf("magic link must be absolute HTTPS (loopback HTTP is allowed for development)")
	}
	messageID, err := s.messageID()
	if err != nil {
		return fmt.Errorf("generate message ID: %w", err)
	}
	message, err := buildMagicLinkMessage(s.from, recipient, displayName, magicLink, expiresIn, s.now().UTC(), messageID)
	if err != nil {
		return err
	}
	return s.send(ctx, recipient.Address, message)
}

func validMagicLinkURL(raw string) bool {
	link, err := url.Parse(raw)
	if err != nil || link.Host == "" || link.User != nil {
		return false
	}
	if strings.EqualFold(link.Scheme, "https") {
		return true
	}
	if !strings.EqualFold(link.Scheme, "http") {
		return false
	}
	host := strings.ToLower(strings.TrimSpace(link.Hostname()))
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (s *SMTPMagicLinkSender) messageID() (string, error) {
	random := make([]byte, 16)
	if _, err := io.ReadFull(s.rand, random); err != nil {
		return "", err
	}
	parts := strings.Split(s.from.Address, "@")
	domain := "arena.local"
	if len(parts) == 2 && parts[1] != "" {
		domain = parts[1]
	}
	return "<" + hex.EncodeToString(random) + "@" + domain + ">", nil
}

func buildMagicLinkMessage(from, to *mail.Address, displayName, magicLink string, expiresIn time.Duration, sentAt time.Time, messageID string) ([]byte, error) {
	minutes := int(expiresIn.Round(time.Minute) / time.Minute)
	if minutes < 1 {
		minutes = 1
	}
	greeting := "Hello,"
	if name := strings.TrimSpace(displayName); name != "" {
		greeting = "Hello " + name + ","
	}
	plain := fmt.Sprintf("%s\n\nUse this one-time link to sign in or create your Arena account and manage cosmetics:\n\n%s\n\nThis link expires in %d minutes and can be used once. If you did not request it, you can ignore this email.\n", greeting, magicLink, minutes)
	htmlBody := fmt.Sprintf(`<!doctype html><html><body><p>%s</p><p><a href="%s">Sign in to Arena</a></p><p>This one-time link expires in %d minutes. If you did not request it, you can ignore this email.</p></body></html>`, html.EscapeString(greeting), html.EscapeString(magicLink), minutes)

	var message bytes.Buffer
	writer := multipart.NewWriter(&message)
	boundary := writer.Boundary()
	fmt.Fprintf(&message, "From: %s\r\n", from.String())
	fmt.Fprintf(&message, "To: %s\r\n", to.String())
	fmt.Fprint(&message, "Subject: Sign in to Arena\r\n")
	fmt.Fprintf(&message, "Date: %s\r\n", sentAt.Format(time.RFC1123Z))
	fmt.Fprintf(&message, "Message-ID: %s\r\n", messageID)
	fmt.Fprint(&message, "MIME-Version: 1.0\r\n")
	fmt.Fprintf(&message, "Content-Type: multipart/alternative; boundary=%q\r\n\r\n", boundary)
	plainHeader := make(textproto.MIMEHeader)
	plainHeader.Set("Content-Type", "text/plain; charset=utf-8")
	plainHeader.Set("Content-Transfer-Encoding", "8bit")
	plainPart, err := writer.CreatePart(plainHeader)
	if err != nil {
		return nil, fmt.Errorf("create plain email part: %w", err)
	}
	if _, err := io.WriteString(plainPart, plain); err != nil {
		return nil, fmt.Errorf("write plain email part: %w", err)
	}
	htmlHeader := make(textproto.MIMEHeader)
	htmlHeader.Set("Content-Type", "text/html; charset=utf-8")
	htmlHeader.Set("Content-Transfer-Encoding", "8bit")
	htmlPart, err := writer.CreatePart(htmlHeader)
	if err != nil {
		return nil, fmt.Errorf("create HTML email part: %w", err)
	}
	if _, err := io.WriteString(htmlPart, htmlBody); err != nil {
		return nil, fmt.Errorf("write HTML email part: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("close email MIME body: %w", err)
	}
	return message.Bytes(), nil
}

func (s *SMTPMagicLinkSender) send(ctx context.Context, recipient string, message []byte) error {
	address := net.JoinHostPort(strings.TrimSpace(s.config.Host), fmt.Sprintf("%d", s.config.Port))
	deadline := time.Now().Add(smtpOperationTimeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	dialer := &net.Dialer{Timeout: time.Until(deadline)}
	connection, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return fmt.Errorf("connect to SMTP submission service: %w", err)
	}
	if err := connection.SetDeadline(deadline); err != nil {
		connection.Close()
		return fmt.Errorf("set SMTP deadline: %w", err)
	}
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
		ServerName: strings.TrimSpace(s.config.TLSServerName),
	}

	mode := strings.ToLower(strings.TrimSpace(s.config.TLSMode))
	if mode == "implicit" {
		tlsConnection := tls.Client(connection, tlsConfig)
		if err := tlsConnection.HandshakeContext(ctx); err != nil {
			connection.Close()
			return fmt.Errorf("negotiate implicit SMTP TLS: %w", err)
		}
		connection = tlsConnection
	}
	client, err := smtp.NewClient(connection, strings.TrimSpace(s.config.TLSServerName))
	if err != nil {
		connection.Close()
		return fmt.Errorf("create SMTP client: %w", err)
	}
	defer client.Close()
	if mode == "starttls" {
		if supported, _ := client.Extension("STARTTLS"); !supported {
			return fmt.Errorf("SMTP service does not advertise required STARTTLS")
		}
		if err := client.StartTLS(tlsConfig); err != nil {
			return fmt.Errorf("negotiate SMTP STARTTLS: %w", err)
		}
	}
	auth := smtp.PlainAuth("", strings.TrimSpace(s.config.Username), s.config.Password, strings.TrimSpace(s.config.TLSServerName))
	if err := client.Auth(auth); err != nil {
		return fmt.Errorf("authenticate to SMTP submission service: %w", err)
	}
	if err := client.Mail(s.from.Address); err != nil {
		return fmt.Errorf("set SMTP envelope sender: %w", err)
	}
	if err := client.Rcpt(recipient); err != nil {
		return fmt.Errorf("set SMTP recipient: %w", err)
	}
	data, err := client.Data()
	if err != nil {
		return fmt.Errorf("start SMTP message body: %w", err)
	}
	if _, err := data.Write(message); err != nil {
		data.Close()
		return fmt.Errorf("write SMTP message body: %w", err)
	}
	if err := data.Close(); err != nil {
		return fmt.Errorf("submit SMTP message: %w", err)
	}
	// Once DATA is accepted, a QUIT transport failure must not revoke the valid
	// one-time token or make the caller send a duplicate message.
	_ = client.Quit()
	return nil
}
