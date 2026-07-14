package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"arena-server/internal/db"
	"arena-server/internal/security"
)

const consentVersionMaxLen = 40

// RecordConsentAcceptance serves POST /api/v1/consent/accept. It is a
// best-effort audit beacon for the client-side TOS/Privacy gate (see
// consent-gate.js): the gate already decided to let the visitor proceed
// before calling this, so a database outage here must never block sign-in or
// key generation — it only means the acceptance is not durably logged.
func RecordConsentAcceptance(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	version := strings.TrimSpace(req.Version)
	if version == "" {
		version = "unknown"
	}
	if len(version) > consentVersionMaxLen {
		version = version[:consentVersionMaxLen]
	}
	ip := security.ExtractClientIP(r)
	// Logged only, never surfaced: this is an audit trail, not a gate. The
	// error is intentionally discarded (including db.ErrNoDatabase in dev
	// mode); a missing audit row must never block sign-in or key generation.
	_ = db.InsertConsentAcceptance(r.Context(), "", version, ip)
	writeJSON(w, http.StatusOK, map[string]bool{"recorded": true})
}
