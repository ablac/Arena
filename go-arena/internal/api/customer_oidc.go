package api

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"arena-server/internal/config"
	"arena-server/internal/db"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

const (
	customerSessionCookieName = "arena_customer_session"
	customerStateCookieName   = "arena_customer_oauth_state"
	customerStateTTL          = 10 * time.Minute
)

// CustomerSession is intentionally separate from OIDCSession. In particular,
// this cookie and context value are never accepted by admin middleware.
type CustomerSession struct {
	AccountID       string
	Email           string
	Name            string
	Subject         string
	EmailVerifiedAt *time.Time
	CSRFToken       string
	CreatedAt       time.Time
	ExpiresAt       time.Time
}

type customerOIDCTransaction struct {
	ExpiresAt            time.Time
	BrowserBindingDigest [sha256.Size]byte
	Nonce                string
	PKCEVerifier         string
	ReturnTo             string
}

type CustomerOIDCHandler struct {
	oauth2Config *oauth2.Config
	verifier     *oidc.IDTokenVerifier
	issuer       string

	sessions map[string]*CustomerSession
	states   map[string]customerOIDCTransaction
	mu       sync.RWMutex
}

type customerSessionContextKey struct{}

func NewCustomerOIDCHandler() *CustomerOIDCHandler {
	cfg := &config.C
	if !cfg.CustomerOIDCEnabled {
		return nil
	}
	if cfg.CustomerOIDCIssuer == "" || cfg.CustomerOIDCClientID == "" ||
		cfg.CustomerOIDCClientSecret == "" || cfg.CustomerOIDCRedirectURI == "" {
		slog.Warn("customer OIDC enabled but missing required config")
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	provider, err := oidc.NewProvider(ctx, cfg.CustomerOIDCIssuer)
	if err != nil {
		slog.Error("failed to initialise customer OIDC provider", "issuer", cfg.CustomerOIDCIssuer, "error", err)
		return nil
	}
	h := &CustomerOIDCHandler{
		oauth2Config: &oauth2.Config{
			ClientID:     cfg.CustomerOIDCClientID,
			ClientSecret: cfg.CustomerOIDCClientSecret,
			RedirectURL:  cfg.CustomerOIDCRedirectURI,
			Endpoint:     provider.Endpoint(),
			Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
		},
		verifier: provider.Verifier(&oidc.Config{ClientID: cfg.CustomerOIDCClientID}),
		issuer:   cfg.CustomerOIDCIssuer,
		sessions: make(map[string]*CustomerSession),
		states:   make(map[string]customerOIDCTransaction),
	}
	go h.cleanupLoop()
	slog.Info("customer OIDC auth initialised", "issuer", cfg.CustomerOIDCIssuer)
	return h
}

func customerDashboardPath(r *http.Request) string {
	if strings.HasPrefix(r.URL.Path, "/arena/") {
		return "/arena/dashboard/"
	}
	return "/dashboard/"
}

func customerAPIDashboardPath(r *http.Request, suffix string) string {
	if strings.HasPrefix(r.URL.Path, "/arena/") {
		return "/arena/api/v1/dashboard" + suffix
	}
	return "/api/v1/dashboard" + suffix
}

func secureCookie(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0]), "https")
}

func setCustomerNoStore(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
}

func safeCustomerReturnTo(r *http.Request) string {
	raw := strings.TrimSpace(r.URL.Query().Get("return_to"))
	if raw == "" || len(raw) > 2048 || strings.Contains(raw, "\\") || strings.HasPrefix(raw, "//") {
		return customerDashboardPath(r)
	}
	parsed, err := url.ParseRequestURI(raw)
	if err != nil || parsed.IsAbs() || parsed.Host != "" {
		return customerDashboardPath(r)
	}
	if parsed.Path != "/dashboard" && !strings.HasPrefix(parsed.Path, "/dashboard/") &&
		parsed.Path != "/arena/dashboard" && !strings.HasPrefix(parsed.Path, "/arena/dashboard/") {
		return customerDashboardPath(r)
	}
	return raw
}

func (h *CustomerOIDCHandler) LoginHandler(w http.ResponseWriter, r *http.Request) {
	setCustomerNoStore(w)
	state := generateToken(32)
	browserBinding := generateToken(32)
	pkceVerifier := generateToken(32)
	nonce := generateToken(32)
	txn := customerOIDCTransaction{
		ExpiresAt:            time.Now().Add(customerStateTTL),
		BrowserBindingDigest: sha256.Sum256([]byte(browserBinding)),
		Nonce:                nonce,
		PKCEVerifier:         pkceVerifier,
		ReturnTo:             safeCustomerReturnTo(r),
	}
	h.mu.Lock()
	h.states[state] = txn
	h.mu.Unlock()
	http.SetCookie(w, &http.Cookie{
		Name:     customerStateCookieName,
		Value:    browserBinding,
		Path:     "/",
		MaxAge:   int(customerStateTTL.Seconds()),
		HttpOnly: true,
		Secure:   secureCookie(r),
		SameSite: http.SameSiteLaxMode,
	})
	authURL := h.oauth2Config.AuthCodeURL(state,
		oauth2.S256ChallengeOption(pkceVerifier),
		oauth2.SetAuthURLParam("nonce", nonce),
	)
	http.Redirect(w, r, authURL, http.StatusFound)
}

func (h *CustomerOIDCHandler) CallbackHandler(w http.ResponseWriter, r *http.Request) {
	setCustomerNoStore(w)
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	stateCookie, cookieErr := r.Cookie(customerStateCookieName)
	var txn customerOIDCTransaction
	validState := false
	if cookieErr == nil && state != "" && stateCookie.Value != "" {
		bindingDigest := sha256.Sum256([]byte(stateCookie.Value))
		h.mu.Lock()
		if candidate, exists := h.states[state]; exists && time.Now().Before(candidate.ExpiresAt) &&
			subtle.ConstantTimeCompare(bindingDigest[:], candidate.BrowserBindingDigest[:]) == 1 {
			txn = candidate
			validState = true
			delete(h.states, state)
		}
		h.mu.Unlock()
	}
	clearCustomerCookie(w, r, customerStateCookieName)
	if !validState {
		http.Error(w, "invalid or expired state parameter", http.StatusBadRequest)
		return
	}
	if errParam := r.URL.Query().Get("error"); errParam != "" {
		http.Error(w, "authentication failed", http.StatusForbidden)
		return
	}
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if code == "" {
		http.Error(w, "missing authorization code", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	token, err := h.oauth2Config.Exchange(ctx, code, oauth2.VerifierOption(txn.PKCEVerifier))
	if err != nil {
		slog.Warn("customer OIDC token exchange failed", "error", err)
		http.Error(w, "token exchange failed", http.StatusBadGateway)
		return
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		http.Error(w, "identity provider omitted id_token", http.StatusBadGateway)
		return
	}
	idToken, err := h.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		slog.Warn("customer OIDC id_token verification failed", "error", err)
		http.Error(w, "invalid identity token", http.StatusForbidden)
		return
	}
	if idToken.Nonce == "" || subtle.ConstantTimeCompare([]byte(idToken.Nonce), []byte(txn.Nonce)) != 1 {
		http.Error(w, "invalid identity token nonce", http.StatusForbidden)
		return
	}
	var claims struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
		PreferredUser string `json:"preferred_username"`
	}
	if err := idToken.Claims(&claims); err != nil {
		http.Error(w, "invalid identity claims", http.StatusForbidden)
		return
	}
	if !claims.EmailVerified || strings.TrimSpace(claims.Email) == "" {
		http.Error(w, "a verified email address is required", http.StatusForbidden)
		return
	}
	if claims.Name == "" {
		claims.Name = claims.PreferredUser
	}
	verifiedIssuer := strings.TrimSpace(idToken.Issuer)
	if verifiedIssuer == "" {
		verifiedIssuer = h.issuer
	}
	account, err := db.UpsertVerifiedCustomerAccount(ctx, claims.Email, verifiedIssuer, idToken.Subject, claims.Name)
	if err != nil {
		slog.Warn("customer account binding failed", "error", err, "subject", idToken.Subject)
		http.Error(w, "unable to bind customer account", http.StatusConflict)
		return
	}
	if account.EmailVerifiedAt == nil {
		slog.Error("verified customer account is missing verification timestamp", "account_id", account.ID)
		http.Error(w, "unable to verify customer account", http.StatusInternalServerError)
		return
	}
	ttl := time.Duration(config.C.CustomerOIDCSessionTTL) * time.Hour
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	now := time.Now()
	sessionID := generateToken(32)
	session := &CustomerSession{
		AccountID:       account.ID,
		Email:           account.Email,
		Name:            account.DisplayName,
		Subject:         idToken.Subject,
		EmailVerifiedAt: account.EmailVerifiedAt,
		CSRFToken:       generateToken(32),
		CreatedAt:       now,
		ExpiresAt:       now.Add(ttl),
	}
	h.mu.Lock()
	h.sessions[sessionID] = session
	h.mu.Unlock()
	http.SetCookie(w, &http.Cookie{
		Name:     customerSessionCookieName,
		Value:    sessionID,
		Path:     "/",
		MaxAge:   int(ttl.Seconds()),
		HttpOnly: true,
		Secure:   secureCookie(r),
		SameSite: http.SameSiteLaxMode,
	})
	slog.Info("customer OIDC login", "account_id", account.ID, "email", account.Email)
	http.Redirect(w, r, txn.ReturnTo, http.StatusFound)
}

func clearCustomerCookie(w http.ResponseWriter, r *http.Request, name string) {
	http.SetCookie(w, &http.Cookie{
		Name: name, Value: "", Path: "/", MaxAge: -1, HttpOnly: true,
		Secure: secureCookie(r), SameSite: http.SameSiteLaxMode,
	})
}

// LogoutHandler is a mutation and is only mounted behind customer auth,
// same-origin Origin validation, and CSRF validation.
func (h *CustomerOIDCHandler) LogoutHandler(w http.ResponseWriter, r *http.Request) {
	setCustomerNoStore(w)
	if cookie, err := r.Cookie(customerSessionCookieName); err == nil {
		h.mu.Lock()
		delete(h.sessions, cookie.Value)
		h.mu.Unlock()
	}
	clearCustomerCookie(w, r, customerSessionCookieName)
	writeJSON(w, http.StatusOK, map[string]any{
		"logged_out":  true,
		"redirect_to": safeCustomerReturnTo(r),
	})
}

func (h *CustomerOIDCHandler) GetSession(r *http.Request) *CustomerSession {
	cookie, err := r.Cookie(customerSessionCookieName)
	if err != nil || cookie.Value == "" {
		return nil
	}
	h.mu.RLock()
	session := h.sessions[cookie.Value]
	h.mu.RUnlock()
	if session == nil || time.Now().After(session.ExpiresAt) {
		return nil
	}
	return session
}

func (h *CustomerOIDCHandler) SessionInfoHandler(w http.ResponseWriter, r *http.Request) {
	setCustomerNoStore(w)
	session := h.GetSession(r)
	if session == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"authenticated": false,
			"login_enabled": true,
			"login_url":     customerAPIDashboardPath(r, "/login"),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated": true,
		"login_enabled": true,
		"account": map[string]any{
			"id":                session.AccountID,
			"email":             session.Email,
			"display_name":      session.Name,
			"name":              session.Name,
			"email_verified":    session.EmailVerifiedAt != nil,
			"email_verified_at": session.EmailVerifiedAt,
		},
		"csrf_token": session.CSRFToken,
		"created_at": session.CreatedAt,
		"expires_at": session.ExpiresAt,
		"login_url":  customerAPIDashboardPath(r, "/login"),
		"logout_url": customerAPIDashboardPath(r, "/logout"),
	})
}

func CustomerSessionUnavailableHandler(w http.ResponseWriter, r *http.Request) {
	setCustomerNoStore(w)
	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated": false,
		"login_enabled": false,
	})
}

func CustomerLoginUnavailableHandler(w http.ResponseWriter, _ *http.Request) {
	setCustomerNoStore(w)
	writeError(w, http.StatusServiceUnavailable, "customer login is not configured")
}

func CustomerSessionFromContext(ctx context.Context) *CustomerSession {
	session, _ := ctx.Value(customerSessionContextKey{}).(*CustomerSession)
	return session
}

func withCustomerSession(ctx context.Context, session *CustomerSession) context.Context {
	return context.WithValue(ctx, customerSessionContextKey{}, session)
}

func normalizedOriginPort(scheme, port string) string {
	if port != "" {
		return port
	}
	if strings.EqualFold(scheme, "https") {
		return "443"
	}
	return "80"
}

func customerMutationHasSameOrigin(r *http.Request) bool {
	rawOrigin := strings.TrimSpace(r.Header.Get("Origin"))
	if rawOrigin == "" || strings.Contains(rawOrigin, ",") || strings.EqualFold(rawOrigin, "null") {
		return false
	}
	origin, err := url.Parse(rawOrigin)
	if err != nil || origin.User != nil || origin.Hostname() == "" || origin.Path != "" || origin.RawQuery != "" || origin.Fragment != "" {
		return false
	}
	scheme := "http"
	if secureCookie(r) {
		scheme = "https"
	}
	expected, err := url.Parse(scheme + "://" + r.Host)
	if err != nil || expected.Hostname() == "" {
		return false
	}
	return strings.EqualFold(origin.Scheme, scheme) &&
		strings.EqualFold(origin.Hostname(), expected.Hostname()) &&
		normalizedOriginPort(origin.Scheme, origin.Port()) == normalizedOriginPort(scheme, expected.Port())
}

func MakeCustomerAuthMiddleware(handler *CustomerOIDCHandler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			setCustomerNoStore(w)
			if handler == nil {
				writeError(w, http.StatusServiceUnavailable, "customer login is not configured")
				return
			}
			session := handler.GetSession(r)
			if session == nil {
				writeError(w, http.StatusUnauthorized, "customer authentication required")
				return
			}
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
			default:
				if !customerMutationHasSameOrigin(r) {
					writeError(w, http.StatusForbidden, "cross-origin customer mutation rejected")
					return
				}
				provided := r.Header.Get("X-CSRF-Token")
				if provided == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(session.CSRFToken)) != 1 {
					writeError(w, http.StatusForbidden, "invalid CSRF token")
					return
				}
			}
			next.ServeHTTP(w, r.WithContext(withCustomerSession(r.Context(), session)))
		})
	}
}

func (h *CustomerOIDCHandler) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		now := time.Now()
		h.mu.Lock()
		for id, session := range h.sessions {
			if now.After(session.ExpiresAt) {
				delete(h.sessions, id)
			}
		}
		for state, txn := range h.states {
			if now.After(txn.ExpiresAt) {
				delete(h.states, state)
			}
		}
		h.mu.Unlock()
	}
}
