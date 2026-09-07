package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"arena-server/internal/db"

	"github.com/go-chi/chi/v5"
)

const (
	profileBioMaxRunes       = 280
	profileAvatarColorMaxLen = 32
)

// profileAvatarColorPattern is a CSS hex colour (#rgb, #rgba, #rrggbb,
// #rrggbbaa) or empty, which clears the colour.
var profileAvatarColorPattern = regexp.MustCompile(`^(#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{4}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8}))?$`)

// sanitizeProfileText strips control/format characters and collapses
// whitespace, matching the chat body sanitizer's rules so a profile field
// cannot be used to smuggle the same spoofing tricks (zero-width characters,
// bidi overrides) chat already guards against.
func sanitizeProfileText(raw string, maxRunes int) (string, bool) {
	if !utf8.ValidString(raw) {
		return "", false
	}
	var b strings.Builder
	b.Grow(len(raw))
	for _, r := range raw {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			b.WriteRune(' ')
		case unicode.IsControl(r) || unicode.Is(unicode.Cf, r):
		default:
			b.WriteRune(r)
		}
	}
	text := strings.TrimSpace(b.String())
	if maxRunes > 0 && utf8.RuneCountInString(text) > maxRunes {
		return "", false
	}
	return text, true
}

func profileJSON(p *db.PublicProfile) map[string]interface{} {
	return map[string]interface{}{
		"account_id":         p.AccountID,
		"public_username":    p.PublicUsername,
		"username_setup_url": db.PublicUsernameSetupURL,
		"chat_handle":        db.PublicUsernameLabel(p.PublicUsername),
		"bio":                p.Bio,
		"avatar_color":       p.AvatarColor,
		"joined_at":          p.JoinedAt,
		"shows_bots":         p.ShowsBots,
		"bots":               p.Bots,
	}
}

// PublicProfileHandler serves GET /api/v1/profile/{account_id}. Public and
// unauthenticated, same trust level as the chat feed itself: account ids
// only become discoverable by reading messages in the (already public) chat
// stream, and this endpoint never returns an email address.
func PublicProfileHandler(w http.ResponseWriter, r *http.Request) {
	accountID := strings.TrimSpace(chi.URLParam(r, "account_id"))
	if accountID == "" || len(accountID) > 128 {
		writeError(w, http.StatusBadRequest, "invalid account id")
		return
	}
	profile, err := db.GetPublicProfile(r.Context(), accountID)
	if err != nil {
		if errors.Is(err, db.ErrNoDatabase) {
			writeError(w, http.StatusServiceUnavailable, "profiles require the database")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load profile")
		return
	}
	if profile == nil {
		writeError(w, http.StatusNotFound, "profile not found")
		return
	}
	writeJSON(w, http.StatusOK, profileJSON(profile))
}

// UpdateAccountProfileHandler serves PATCH /api/v1/account/profile, mounted
// behind MakeCustomerAuthMiddleware (session + same-origin + CSRF) alongside
// the other /account/* routes.
func UpdateAccountProfileHandler(w http.ResponseWriter, r *http.Request) {
	session := CustomerSessionFromContext(r.Context())
	if session == nil {
		writeError(w, http.StatusUnauthorized, "customer authentication required")
		return
	}
	var req struct {
		DisplayName    json.RawMessage `json:"display_name"`
		Name           json.RawMessage `json:"name"`
		Username       json.RawMessage `json:"username"`
		PublicUsername json.RawMessage `json:"public_username"`
		Bio            *string         `json:"bio"`
		AvatarColor    *string         `json:"avatar_color"`
		ShowBotsPublic *bool           `json:"show_bots_public"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	update := db.CustomerProfileUpdate{ShowBotsPublic: req.ShowBotsPublic}
	if len(req.DisplayName)+len(req.Name)+len(req.Username)+len(req.PublicUsername) > 0 {
		writeError(w, http.StatusBadRequest, "Public usernames are managed at "+db.PublicUsernameSetupURL+"; sign in again to refresh Arena.")
		return
	}
	if req.Bio != nil {
		clean, ok := sanitizeProfileText(*req.Bio, profileBioMaxRunes)
		if !ok {
			writeError(w, http.StatusBadRequest, "bio must be at most 280 characters")
			return
		}
		update.Bio = &clean
	}
	if req.AvatarColor != nil {
		color := strings.TrimSpace(*req.AvatarColor)
		// A hex colour or nothing. Every consumer puts this inside a CSS
		// `background:` declaration, and the length check alone let a
		// declaration terminator through; the profile popup already insists
		// on hex, so the server now states the same contract.
		if len(color) > profileAvatarColorMaxLen || !profileAvatarColorPattern.MatchString(color) {
			writeError(w, http.StatusBadRequest, "avatar color must be a hex colour like #22ccff")
			return
		}
		update.AvatarColor = &color
	}

	profile, err := db.UpdateCustomerProfile(r.Context(), session.AccountID, update)
	if err != nil {
		if errors.Is(err, db.ErrNoDatabase) {
			writeError(w, http.StatusServiceUnavailable, "profiles require the database")
			return
		}
		if errors.Is(err, db.ErrCustomerAccountNotFound) {
			writeError(w, http.StatusNotFound, "account not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to update profile")
		return
	}
	writeJSON(w, http.StatusOK, profileJSON(profile))
}
