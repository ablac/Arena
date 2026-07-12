package api

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"arena-server/internal/config"
	"arena-server/internal/db"
	"arena-server/internal/mailer"
)

const customerEmailBodyLimit = 16 << 10

type customerEmailStore interface {
	CreateVerification(context.Context, string, string, string, []byte, time.Time, time.Duration, time.Duration) error
	ConsumeVerification(context.Context, []byte, time.Time) (*db.CustomerAccount, string, error)
	DeleteVerification(context.Context, []byte) error
}

type customerEmailSender interface {
	SendMagicLink(context.Context, string, string, string, time.Duration) error
}

type databaseCustomerEmailStore struct{}

func (databaseCustomerEmailStore) CreateVerification(ctx context.Context, email, displayName, returnTo string, tokenHash []byte, createdAt time.Time, ttl, cooldown time.Duration) error {
	return db.CreateCustomerEmailVerification(ctx, email, displayName, returnTo, tokenHash, createdAt, ttl, cooldown)
}

func (databaseCustomerEmailStore) ConsumeVerification(ctx context.Context, tokenHash []byte, now time.Time) (*db.CustomerAccount, string, error) {
	return db.ConsumeCustomerEmailVerification(ctx, tokenHash, now)
}

func (databaseCustomerEmailStore) DeleteVerification(ctx context.Context, tokenHash []byte) error {
	return db.DeleteCustomerEmailVerification(ctx, tokenHash)
}

func configureCustomerEmailAuth(handler *CustomerOIDCHandler, cfg config.Config) error {
	sender, err := mailer.NewSMTPMagicLinkSender(mailer.SMTPConfig{
		Host:          cfg.SMTPHost,
		Port:          cfg.SMTPPort,
		TLSMode:       cfg.SMTPTLSMode,
		TLSServerName: cfg.SMTPTLSServerName,
		Username:      cfg.SMTPUsername,
		Password:      cfg.SMTPPassword,
		From:          cfg.SMTPFrom,
	})
	if err != nil {
		return err
	}
	handler.emailStore = databaseCustomerEmailStore{}
	handler.emailSender = sender
	handler.emailSignInURL = strings.TrimSpace(cfg.CustomerEmailSignInURL)
	handler.emailTokenTTL = time.Duration(cfg.CustomerEmailTokenTTLMinutes) * time.Minute
	handler.emailSendCooldown = time.Duration(cfg.CustomerEmailSendCooldownSeconds) * time.Second
	slog.Info("native customer email auth initialised", "smtp_host", cfg.SMTPHost, "smtp_port", cfg.SMTPPort)
	return nil
}

func safeCustomerReturnToValue(raw, fallback string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 2048 || strings.Contains(raw, "\\") || strings.HasPrefix(raw, "//") {
		return fallback
	}
	parsed, err := url.ParseRequestURI(raw)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || parsed.Fragment != "" {
		return fallback
	}
	if parsed.Path != "/dashboard" && !strings.HasPrefix(parsed.Path, "/dashboard/") &&
		parsed.Path != "/arena/dashboard" && !strings.HasPrefix(parsed.Path, "/arena/dashboard/") {
		return fallback
	}
	return raw
}

func decodeCustomerEmailJSON(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, customerEmailBodyLimit)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return fmt.Errorf("request body too large: %w", err)
		}
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain one JSON object")
	}
	return nil
}

func customerEmailJSONError(w http.ResponseWriter, err error) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) || strings.Contains(err.Error(), "request body too large") {
		writeError(w, http.StatusRequestEntityTooLarge, "request body is too large")
		return
	}
	writeError(w, http.StatusBadRequest, "invalid request body")
}

func (h *CustomerOIDCHandler) EmailStartHandler(w http.ResponseWriter, r *http.Request) {
	setCustomerNoStore(w)
	if h == nil || h.emailStore == nil || h.emailSender == nil {
		writeError(w, http.StatusServiceUnavailable, "customer email sign-in is not configured")
		return
	}
	if !customerMutationHasSameOrigin(r) {
		writeError(w, http.StatusForbidden, "cross-origin customer email request rejected")
		return
	}
	var request struct {
		Email       string `json:"email"`
		DisplayName string `json:"display_name"`
		ReturnTo    string `json:"return_to"`
	}
	if err := decodeCustomerEmailJSON(w, r, &request); err != nil {
		customerEmailJSONError(w, err)
		return
	}
	email, err := db.NormalizeCustomerEmail(request.Email)
	if err != nil {
		writeError(w, http.StatusBadRequest, "enter a valid email address")
		return
	}
	displayName := strings.TrimSpace(request.DisplayName)
	if len(displayName) > 200 || strings.ContainsAny(displayName, "\r\n\x00") {
		writeError(w, http.StatusBadRequest, "display name is invalid")
		return
	}
	returnTo := safeCustomerReturnToValue(request.ReturnTo, customerDashboardPath(r))
	rawToken := generateToken(32)
	digest := sha256.Sum256([]byte(rawToken))
	now := time.Now().UTC()
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	err = h.emailStore.CreateVerification(ctx, email, displayName, returnTo, digest[:], now, h.emailTokenTTL, h.emailSendCooldown)
	if errors.Is(err, db.ErrCustomerEmailVerificationRateLimited) {
		writeJSON(w, http.StatusAccepted, map[string]any{
			"accepted": true,
			"message":  "If that address can receive Arena mail, a sign-in link will arrive shortly.",
		})
		return
	}
	if errors.Is(err, db.ErrCustomerEmailInvalid) || errors.Is(err, db.ErrCustomerEmailVerificationInvalid) {
		writeError(w, http.StatusBadRequest, "invalid email sign-in request")
		return
	}
	if err != nil {
		slog.Warn("customer email verification claim failed", "error", err)
		writeError(w, http.StatusServiceUnavailable, "email sign-in is temporarily unavailable")
		return
	}
	magicURL, err := url.Parse(h.emailSignInURL)
	if err != nil {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = h.emailStore.DeleteVerification(cleanupCtx, digest[:])
		cleanupCancel()
		slog.Error("configured customer email sign-in URL became invalid", "error", err)
		writeError(w, http.StatusServiceUnavailable, "email sign-in is temporarily unavailable")
		return
	}
	magicURL.Fragment = url.Values{"email_token": {rawToken}}.Encode()
	if err := h.emailSender.SendMagicLink(ctx, email, displayName, magicURL.String(), h.emailTokenTTL); err != nil {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 3*time.Second)
		deleteErr := h.emailStore.DeleteVerification(cleanupCtx, digest[:])
		cleanupCancel()
		if deleteErr != nil {
			slog.Error("failed to revoke undelivered customer email token", "error", deleteErr)
		}
		// SMTP rejections may echo the recipient address or provider internals.
		// Keep operational logging free of customer PII and bearer-adjacent data.
		slog.Warn("customer email delivery failed")
		writeError(w, http.StatusBadGateway, "the sign-in email could not be sent")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"accepted": true,
		"message":  "If that address can receive Arena mail, a sign-in link will arrive shortly.",
	})
}

func (h *CustomerOIDCHandler) EmailVerifyHandler(w http.ResponseWriter, r *http.Request) {
	setCustomerNoStore(w)
	if h == nil || h.emailStore == nil || h.emailSender == nil {
		writeError(w, http.StatusServiceUnavailable, "customer email sign-in is not configured")
		return
	}
	if !customerMutationHasSameOrigin(r) {
		writeError(w, http.StatusForbidden, "cross-origin customer email verification rejected")
		return
	}
	var request struct {
		Token string `json:"token"`
	}
	if err := decodeCustomerEmailJSON(w, r, &request); err != nil {
		customerEmailJSONError(w, err)
		return
	}
	rawToken := strings.TrimSpace(request.Token)
	if len(rawToken) < 32 || len(rawToken) > 256 {
		writeError(w, http.StatusBadRequest, "sign-in link is invalid or expired")
		return
	}
	digest := sha256.Sum256([]byte(rawToken))
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	account, returnTo, err := h.emailStore.ConsumeVerification(ctx, digest[:], time.Now().UTC())
	if errors.Is(err, db.ErrCustomerEmailVerificationInvalid) {
		writeError(w, http.StatusBadRequest, "sign-in link is invalid or expired")
		return
	}
	if err != nil {
		slog.Warn("customer email verification failed", "error", err)
		writeError(w, http.StatusServiceUnavailable, "email sign-in is temporarily unavailable")
		return
	}
	if account == nil || account.EmailVerifiedAt == nil {
		slog.Error("customer email verification returned an unverified account")
		writeError(w, http.StatusInternalServerError, "email sign-in could not be completed")
		return
	}
	h.establishCustomerSession(w, r, account, "email:"+account.ID)
	slog.Info("customer email login", "account_id", account.ID)
	writeJSON(w, http.StatusOK, map[string]any{
		"verified":    true,
		"redirect_to": safeCustomerReturnToValue(returnTo, customerDashboardPath(r)),
	})
}
