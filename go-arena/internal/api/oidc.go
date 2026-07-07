package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"arena-server/internal/config"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// OIDCSession represents an authenticated admin session.
type OIDCSession struct {
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	Subject   string    `json:"subject"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// OIDCHandler manages OIDC authentication for the admin dashboard.
type OIDCHandler struct {
	provider     *oidc.Provider
	oauth2Config *oauth2.Config
	verifier     *oidc.IDTokenVerifier

	sessions   map[string]*OIDCSession
	states     map[string]time.Time // CSRF state tokens
	mu         sync.RWMutex
}

const (
	sessionCookieName = "arena_admin_session"
	stateTTL          = 10 * time.Minute
)

// NewOIDCHandler initialises the OIDC provider and returns a handler.
// Returns nil if OIDC is not enabled or misconfigured.
func NewOIDCHandler() *OIDCHandler {
	cfg := &config.C
	if !cfg.OIDCEnabled {
		return nil
	}
	if cfg.OIDCIssuer == "" || cfg.OIDCClientID == "" || cfg.OIDCClientSecret == "" || cfg.OIDCRedirectURI == "" {
		slog.Warn("OIDC enabled but missing required config (issuer/client_id/client_secret/redirect_uri)")
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	provider, err := oidc.NewProvider(ctx, cfg.OIDCIssuer)
	if err != nil {
		slog.Error("failed to initialise OIDC provider", "issuer", cfg.OIDCIssuer, "error", err)
		return nil
	}

	oauth2Cfg := &oauth2.Config{
		ClientID:     cfg.OIDCClientID,
		ClientSecret: cfg.OIDCClientSecret,
		RedirectURL:  cfg.OIDCRedirectURI,
		Endpoint:     provider.Endpoint(),
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
	}

	verifier := provider.Verifier(&oidc.Config{ClientID: cfg.OIDCClientID})

	h := &OIDCHandler{
		provider:     provider,
		oauth2Config: oauth2Cfg,
		verifier:     verifier,
		sessions:     make(map[string]*OIDCSession),
		states:       make(map[string]time.Time),
	}

	// Background goroutine to clean expired sessions and states.
	go h.cleanupLoop()

	slog.Info("OIDC admin auth initialised", "issuer", cfg.OIDCIssuer)
	return h
}

// generateToken creates a cryptographically random hex token.
func generateToken(bytes int) string {
	b := make([]byte, bytes)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// adminDashboardPath returns the path to redirect back to after an OIDC
// login/logout, honoring whichever prefix the request arrived on. The
// router mirrors /admin/* under /arena/admin/* for prefixed deployments, so a hardcoded
// "/admin/" redirect sends /arena/-mounted visitors to the wrong app.
func adminDashboardPath(r *http.Request) string {
	if strings.HasPrefix(r.URL.Path, "/arena/") {
		return "/arena/admin/"
	}
	return "/admin/"
}

// LoginHandler redirects the user to the Authentik authorization page.
func (h *OIDCHandler) LoginHandler(w http.ResponseWriter, r *http.Request) {
	state := generateToken(16)
	h.mu.Lock()
	h.states[state] = time.Now().Add(stateTTL)
	h.mu.Unlock()

	http.Redirect(w, r, h.oauth2Config.AuthCodeURL(state), http.StatusFound)
}

// CallbackHandler handles the OAuth2 callback from Authentik.
func (h *OIDCHandler) CallbackHandler(w http.ResponseWriter, r *http.Request) {
	// Verify state parameter (CSRF protection).
	state := r.URL.Query().Get("state")
	h.mu.Lock()
	expiry, ok := h.states[state]
	if ok {
		delete(h.states, state)
	}
	h.mu.Unlock()

	if !ok || time.Now().After(expiry) {
		http.Error(w, "invalid or expired state parameter", http.StatusBadRequest)
		return
	}

	// Check for error from provider.
	if errParam := r.URL.Query().Get("error"); errParam != "" {
		desc := r.URL.Query().Get("error_description")
		slog.Warn("OIDC callback error", "error", errParam, "description", desc)
		http.Error(w, "authentication failed: "+errParam, http.StatusForbidden)
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing authorization code", http.StatusBadRequest)
		return
	}

	// Exchange code for tokens.
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	oauth2Token, err := h.oauth2Config.Exchange(ctx, code)
	if err != nil {
		slog.Error("OIDC token exchange failed", "error", err)
		http.Error(w, "token exchange failed", http.StatusInternalServerError)
		return
	}

	// Extract and verify the ID token.
	rawIDToken, ok := oauth2Token.Extra("id_token").(string)
	if !ok {
		slog.Error("OIDC response missing id_token")
		http.Error(w, "missing id_token in response", http.StatusInternalServerError)
		return
	}

	idToken, err := h.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		slog.Error("OIDC id_token verification failed", "error", err)
		http.Error(w, "invalid id_token", http.StatusForbidden)
		return
	}

	// Extract claims.
	var claims struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
		PreferredUser string `json:"preferred_username"`
	}
	if err := idToken.Claims(&claims); err != nil {
		slog.Error("failed to parse OIDC claims", "error", err)
		http.Error(w, "failed to parse claims", http.StatusInternalServerError)
		return
	}

	// Create session.
	sessionID := generateToken(32)
	ttl := time.Duration(config.C.OIDCSessionTTL) * time.Hour
	session := &OIDCSession{
		Email:     claims.Email,
		Name:      claims.Name,
		Subject:   idToken.Subject,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(ttl),
	}
	if session.Name == "" {
		session.Name = claims.PreferredUser
	}

	h.mu.Lock()
	h.sessions[sessionID] = session
	h.mu.Unlock()

	slog.Info("OIDC admin login", "email", claims.Email, "name", session.Name, "subject", idToken.Subject)

	// Set session cookie.
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    sessionID,
		Path:     "/",
		MaxAge:   int(ttl.Seconds()),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})

	// Redirect to admin dashboard.
	http.Redirect(w, r, adminDashboardPath(r), http.StatusFound)
}

// LogoutHandler clears the session and optionally redirects to Authentik logout.
func (h *OIDCHandler) LogoutHandler(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(sessionCookieName)
	if err == nil && cookie.Value != "" {
		h.mu.Lock()
		delete(h.sessions, cookie.Value)
		h.mu.Unlock()
	}

	// Clear cookie.
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})

	// Redirect to admin login page.
	http.Redirect(w, r, adminDashboardPath(r), http.StatusFound)
}

// SessionInfoHandler returns the current session info as JSON (for the frontend).
func (h *OIDCHandler) SessionInfoHandler(w http.ResponseWriter, r *http.Request) {
	session := h.GetSession(r)
	if session == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"authenticated": false,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"authenticated": true,
		"email":         session.Email,
		"name":          session.Name,
		"expires_at":    session.ExpiresAt,
	})
}

// GetSession returns the OIDC session for the request, or nil if not authenticated.
func (h *OIDCHandler) GetSession(r *http.Request) *OIDCSession {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		return nil
	}

	h.mu.RLock()
	session, ok := h.sessions[cookie.Value]
	h.mu.RUnlock()

	if !ok || time.Now().After(session.ExpiresAt) {
		return nil
	}
	return session
}

// IsAuthenticated returns true if the request has a valid OIDC session.
func (h *OIDCHandler) IsAuthenticated(r *http.Request) bool {
	return h.GetSession(r) != nil
}

// cleanupLoop removes expired sessions and state tokens periodically.
func (h *OIDCHandler) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()
		h.mu.Lock()

		for id, s := range h.sessions {
			if now.After(s.ExpiresAt) {
				delete(h.sessions, id)
			}
		}
		for state, expiry := range h.states {
			if now.After(expiry) {
				delete(h.states, state)
			}
		}

		h.mu.Unlock()
	}
}

// OIDCInfoJSON returns the session info as a JSON-serialisable map.
func OIDCInfoJSON(session *OIDCSession) map[string]interface{} {
	if session == nil {
		return map[string]interface{}{"authenticated": false}
	}
	return map[string]interface{}{
		"authenticated": true,
		"email":         session.Email,
		"name":          session.Name,
		"expires_at":    session.ExpiresAt,
	}
}
