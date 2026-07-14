package api

import (
	"net/http"

	"arena-server/internal/config"
	"arena-server/internal/ws"
)

// ChatConfigHandler reports the public chat configuration so the frontend
// can decide whether to show the chat panel at all (and how to validate
// input client-side before the server enforces the same limits). enabled
// reflects both the startup master switch (ARENA_CHAT_ENABLED) and the
// admin-toggled runtime kill switch, so the frontend needs only one field.
func ChatConfigHandler(hub *ws.ChatHub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		enabled := config.C.ChatEnabled && hub != nil && hub.Enabled()
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"enabled":       enabled,
			"max_body_len":  config.C.ChatMaxBodyLen,
			"posts_per_min": config.C.ChatPostsPerMin,
			"history_size":  config.C.ChatHistorySize,
		})
	}
}
