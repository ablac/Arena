package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"arena-server/internal/db"

	"github.com/go-chi/chi/v5"
)

// adminChatMessage is the admin-facing view of a chat message: unlike the
// public wire format it includes the poster IP and hidden flag.
type adminChatMessage struct {
	ID        int64     `json:"id"`
	AccountID string    `json:"account_id,omitempty"`
	Handle    string    `json:"handle"`
	Body      string    `json:"body"`
	IP        string    `json:"ip,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	Hidden    bool      `json:"hidden"`
}

func toAdminChatMessage(m db.ChatMessage) adminChatMessage {
	out := adminChatMessage{
		ID:        m.ID,
		Handle:    m.Handle,
		Body:      m.Body,
		IP:        m.IP,
		CreatedAt: m.CreatedAt,
		Hidden:    m.Hidden,
	}
	if m.AccountID != nil {
		out.AccountID = *m.AccountID
	}
	return out
}

// Chat moderation endpoints. Registered inside AdminHandler.Routes so both
// the root admin block and the /arena mirror pick them up.

// hideChatMessage soft-deletes one lobby chat message and drops it from the
// live ring so connected clients remove it immediately.
func (h *AdminHandler) hideChatMessage(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid chat message id")
		return
	}

	persisted := false
	if db.Pool != nil {
		found, err := db.HideChatMessage(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to hide chat message")
			return
		}
		if !found {
			// The id may still exist in-memory only (database-optional
			// mode assigns local ids), so fall through to the hub purge.

		} else {
			persisted = true
		}
	}

	if h.ChatHub != nil {
		h.ChatHub.HideMessage(id)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"hidden":    id,
		"persisted": persisted,
	})
}

// setChatBan sets or clears the chat-scoped mute on a customer account.
// minutes <= 0 clears the ban. This does not touch game access; the IP ban
// remains the blunt instrument for that.
func (h *AdminHandler) setChatBan(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AccountID string `json:"account_id"`
		Minutes   int    `json:"minutes"`
		Reason    string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.AccountID == "" {
		writeError(w, http.StatusBadRequest, "account_id is required")
		return
	}
	reason := strings.TrimSpace(req.Reason)
	if len(reason) > 500 {
		reason = reason[:500]
	}

	var until *time.Time
	if req.Minutes > 0 {
		t := time.Now().Add(time.Duration(req.Minutes) * time.Minute)
		until = &t
	}

	found, err := db.SetCustomerChatBan(r.Context(), req.AccountID, until)
	if err != nil {
		if errors.Is(err, db.ErrNoDatabase) {
			writeError(w, http.StatusServiceUnavailable, "chat bans require the database")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to update chat ban")
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "customer account not found")
		return
	}
	// Best effort: the ban itself already applied, do not fail the request
	// over an audit-log write.
	_ = db.InsertChatBanLogEntry(r.Context(), req.AccountID, req.Minutes, until, reason)

	resp := map[string]interface{}{
		"account_id":   req.AccountID,
		"banned_until": nil,
	}
	if until != nil {
		resp["banned_until"] = until.UTC().Format(time.RFC3339)
	}
	writeJSON(w, http.StatusOK, resp)
}

// unhideChatMessage clears the soft-delete flag set by hideChatMessage.
func (h *AdminHandler) unhideChatMessage(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid chat message id")
		return
	}
	found, err := db.UnhideChatMessage(r.Context(), id)
	if err != nil {
		if errors.Is(err, db.ErrNoDatabase) {
			writeError(w, http.StatusServiceUnavailable, "chat moderation requires the database")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to unhide chat message")
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "chat message not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"unhidden": id})
}

// listChatMessages returns recent chat messages for the admin moderation
// view, including hidden ones and poster IPs.
func (h *AdminHandler) listChatMessages(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	messages, err := db.ListChatMessagesForAdmin(r.Context(), limit)
	if err != nil {
		if errors.Is(err, db.ErrNoDatabase) {
			writeJSON(w, http.StatusOK, map[string]interface{}{"messages": []adminChatMessage{}})
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load chat messages")
		return
	}
	out := make([]adminChatMessage, len(messages))
	for i, m := range messages {
		out[i] = toAdminChatMessage(m)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"messages": out})
}

// listChatBans returns every currently-active chat ban.
func (h *AdminHandler) listChatBans(w http.ResponseWriter, r *http.Request) {
	bans, err := db.ListActiveChatBans(r.Context())
	if err != nil {
		if errors.Is(err, db.ErrNoDatabase) {
			writeJSON(w, http.StatusOK, map[string]interface{}{"bans": []db.ActiveChatBan{}})
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load chat bans")
		return
	}
	if bans == nil {
		bans = []db.ActiveChatBan{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"bans": bans})
}

// chatBanLog returns the recent ban/unban audit trail.
func (h *AdminHandler) chatBanLog(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	entries, err := db.ListChatBanLog(r.Context(), limit)
	if err != nil {
		if errors.Is(err, db.ErrNoDatabase) {
			writeJSON(w, http.StatusOK, map[string]interface{}{"log": []db.ChatBanLogEntry{}})
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load chat ban log")
		return
	}
	if entries == nil {
		entries = []db.ChatBanLogEntry{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"log": entries})
}

// listChatKeywords returns the blocked-keyword list.
func (h *AdminHandler) listChatKeywords(w http.ResponseWriter, r *http.Request) {
	keywords, err := db.ListChatBlockedKeywords(r.Context())
	if err != nil {
		if errors.Is(err, db.ErrNoDatabase) {
			writeJSON(w, http.StatusOK, map[string]interface{}{"keywords": []db.ChatBlockedKeyword{}})
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load blocked keywords")
		return
	}
	if keywords == nil {
		keywords = []db.ChatBlockedKeyword{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"keywords": keywords})
}

// addChatKeyword adds one keyword to the blocklist and pushes the updated
// list into the live hub so enforcement is immediate.
func (h *AdminHandler) addChatKeyword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Keyword string `json:"keyword"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	keyword := strings.ToLower(strings.TrimSpace(req.Keyword))
	if keyword == "" || len(keyword) > 100 {
		writeError(w, http.StatusBadRequest, "keyword must be 1-100 characters")
		return
	}
	added, err := db.InsertChatBlockedKeyword(r.Context(), keyword)
	if err != nil {
		if errors.Is(err, db.ErrChatKeywordExists) {
			writeError(w, http.StatusConflict, "keyword is already blocked")
			return
		}
		if errors.Is(err, db.ErrNoDatabase) {
			writeError(w, http.StatusServiceUnavailable, "keyword blocklist requires the database")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to add keyword")
		return
	}
	h.refreshChatKeywordsLocked(r)
	writeJSON(w, http.StatusOK, added)
}

// deleteChatKeyword removes one keyword from the blocklist.
func (h *AdminHandler) deleteChatKeyword(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid keyword id")
		return
	}
	found, err := db.DeleteChatBlockedKeyword(r.Context(), id)
	if err != nil {
		if errors.Is(err, db.ErrNoDatabase) {
			writeError(w, http.StatusServiceUnavailable, "keyword blocklist requires the database")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to remove keyword")
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "keyword not found")
		return
	}
	h.refreshChatKeywordsLocked(r)
	writeJSON(w, http.StatusOK, map[string]interface{}{"removed": id})
}

// refreshChatKeywordsLocked reloads the keyword list from the database into
// the live hub. Best effort: a failure here just means the previous list
// stays active until the next successful reload.
func (h *AdminHandler) refreshChatKeywordsLocked(r *http.Request) {
	if h.ChatHub == nil {
		return
	}
	keywords, err := db.ListChatBlockedKeywords(r.Context())
	if err != nil {
		return
	}
	list := make([]string, len(keywords))
	for i, k := range keywords {
		list[i] = k.Keyword
	}
	h.ChatHub.SetBlockedKeywords(list)
}

// setChatEnabled toggles the live admin kill switch, independent of the
// ARENA_CHAT_ENABLED startup flag.
func (h *AdminHandler) setChatEnabled(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := db.SetChatRuntimeEnabled(r.Context(), req.Enabled); err != nil {
		if !errors.Is(err, db.ErrNoDatabase) {
			writeError(w, http.StatusInternalServerError, "failed to save chat enabled state")
			return
		}
		// Dev mode without a database: apply live without persisting.
	}
	if h.ChatHub != nil {
		h.ChatHub.SetEnabled(req.Enabled)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"enabled": req.Enabled})
}

// chatOverview reports a small set of at-a-glance moderation stats.
func (h *AdminHandler) chatOverview(w http.ResponseWriter, r *http.Request) {
	resp := map[string]interface{}{
		"enabled":           h.ChatHub != nil && h.ChatHub.Enabled(),
		"connected_clients": 0,
		"keyword_count":     0,
		"active_ban_count":  0,
	}
	if h.ChatHub != nil {
		resp["connected_clients"] = h.ChatHub.ClientCount()
	}
	if keywords, err := db.ListChatBlockedKeywords(r.Context()); err == nil {
		resp["keyword_count"] = len(keywords)
	}
	if bans, err := db.ListActiveChatBans(r.Context()); err == nil {
		resp["active_ban_count"] = len(bans)
	}
	writeJSON(w, http.StatusOK, resp)
}
