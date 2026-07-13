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
	plain := fmt.Sprintf(`ARENA ACCOUNT ACCESS

%s

Sign in to your Arena Dashboard with this one-time link:

%s

This link expires in %d minutes and can be used once. Never share this link. Arena staff will never ask you for it.

If you did not request this email, you can safely ignore it. No changes will be made to your account.
`, greeting, magicLink, minutes)
	escapedGreeting := html.EscapeString(greeting)
	escapedLink := html.EscapeString(magicLink)
	htmlBody := fmt.Sprintf(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <title>Your Arena sign-in link</title>
</head>
<body style="margin:0;background:#050912;color:#dcecff;font-family:Arial,Helvetica,sans-serif;">
  <div style="display:none;max-height:0;overflow:hidden;opacity:0;color:transparent;">Your one-time Arena Dashboard sign-in link expires in %d minutes.</div>
  <table role="presentation" width="100%%" cellspacing="0" cellpadding="0" border="0" style="background:#050912;">
    <tr><td align="center" style="padding:32px 16px;">
      <table role="presentation" width="100%%" cellspacing="0" cellpadding="0" border="0" style="max-width:600px;background:#0b1424;border:1px solid #203a5c;border-radius:16px;overflow:hidden;">
        <tr><td style="padding:18px 28px;background:#10243b;border-bottom:1px solid #203a5c;color:#62d9ff;font-size:12px;font-weight:700;letter-spacing:2px;">ARENA ACCOUNT ACCESS</td></tr>
        <tr><td style="padding:32px 28px;">
          <p style="margin:0 0 18px;color:#dcecff;font-size:16px;line-height:1.6;">%s</p>
          <h1 style="margin:0 0 14px;color:#ffffff;font-size:26px;line-height:1.25;">Sign in to your Arena Dashboard</h1>
          <p style="margin:0 0 24px;color:#a9bed5;font-size:15px;line-height:1.6;">Use this secure, one-time link to manage your bots and account-owned cosmetics.</p>
          <p style="margin:0 0 26px;"><a href="%s" style="display:inline-block;padding:14px 22px;border-radius:9px;background:#36c9f4;color:#041019;text-decoration:none;font-size:16px;font-weight:700;">Sign in to your Arena Dashboard</a></p>
          <p style="margin:0 0 8px;color:#a9bed5;font-size:13px;line-height:1.5;">If the button does not work: Copy and paste this address into your browser.</p>
          <div style="margin:0 0 24px;padding:12px;border-radius:8px;background:#07101d;border:1px solid #203a5c;color:#b8eaff;font-family:Consolas,Monaco,monospace;font-size:12px;line-height:1.5;word-break:break-all;">%s</div>
          <div style="padding:14px 16px;border-radius:9px;background:#171a24;border-left:4px solid #f3bd4f;color:#d5dbe5;font-size:13px;line-height:1.55;"><strong style="color:#ffffff;">Security note:</strong> This link expires in %d minutes and can be used once. Never share this link. Arena staff will never ask you for it.</div>
          <p style="margin:22px 0 0;color:#8399b2;font-size:13px;line-height:1.55;">If you did not request this email, you can safely ignore it. No changes will be made to your account.</p>
        </td></tr>
        <tr><td style="padding:18px 28px;background:#08111e;border-top:1px solid #203a5c;color:#71879f;font-size:12px;line-height:1.5;">Arena by Angel-Serv &middot; Transactional account email</td></tr>
      </table>
    </td></tr>
  </table>
</body>
</html>`, minutes, escapedGreeting, escapedLink, escapedLink, minutes)

	var message bytes.Buffer
	writer := multipart.NewWriter(&message)
	boundary := writer.Boundary()
	fmt.Fprintf(&message, "From: %s\r\n", from.String())
	fmt.Fprintf(&message, "Reply-To: %s\r\n", from.String())
	fmt.Fprintf(&message, "To: %s\r\n", to.String())
	fmt.Fprint(&message, "Subject: Your Arena sign-in link\r\n")
	fmt.Fprintf(&message, "Date: %s\r\n", sentAt.Format(time.RFC1123Z))
	fmt.Fprintf(&message, "Message-ID: %s\r\n", messageID)
	fmt.Fprint(&message, "Auto-Submitted: auto-generated\r\n")
	fmt.Fprint(&message, "X-Auto-Response-Suppress: All\r\n")
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
