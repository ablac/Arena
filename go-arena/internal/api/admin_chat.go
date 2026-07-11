package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"arena-server/internal/db"

	"github.com/go-chi/chi/v5"
)

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
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.AccountID == "" {
		writeError(w, http.StatusBadRequest, "account_id is required")
		return
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

	resp := map[string]interface{}{
		"account_id":   req.AccountID,
		"banned_until": nil,
	}
	if until != nil {
		resp["banned_until"] = until.UTC().Format(time.RFC3339)
	}
	writeJSON(w, http.StatusOK, resp)
}
