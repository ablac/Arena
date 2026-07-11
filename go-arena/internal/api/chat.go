package api

import (
	"net/http"

	"arena-server/internal/config"
)

// ChatConfigHandler reports the public chat configuration so the frontend
// can decide whether to show the chat panel at all (and how to validate
// input client-side before the server enforces the same limits).
func ChatConfigHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"enabled":       config.C.ChatEnabled,
		"max_body_len":  config.C.ChatMaxBodyLen,
		"posts_per_min": config.C.ChatPostsPerMin,
		"history_size":  config.C.ChatHistorySize,
	})
}
