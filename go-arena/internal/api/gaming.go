package api

import (
	"arena-server/internal/db"
	"context"
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode"
)

type gamingProfileLookup func(context.Context, string, string) (*db.GamingProfile, error)

// One fixed-size service quota, independent of Redis and untrusted IP headers.
// Only an authenticated service can consume it or reach the identity lookup.
func newGamingProfileHandler(key string, lookup gamingProfileLookup) http.Handler {
	var mu sync.Mutex
	var window time.Time
	var used int
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		auth := r.Header.Get("Authorization")
		if len(key) != 64 || !strings.HasPrefix(auth, "Bearer ") || subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(auth, "Bearer ")), []byte(key)) != 1 {
			writeError(w, http.StatusUnauthorized, "service authentication required")
			return
		}
		mu.Lock()
		if time.Since(window) >= time.Minute {
			window = time.Now()
			used = 0
		}
		allowed := used < 60
		if allowed {
			used++
		}
		mu.Unlock()
		if !allowed {
			w.Header().Set("Retry-After", "60")
			writeError(w, http.StatusTooManyRequests, "service quota exceeded")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1024)
		var request struct {
			Issuer  string `json:"issuer"`
			Subject string `json:"subject"`
		}
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil {
			writeError(w, 400, "invalid profile request")
			return
		}
		if err := decoder.Decode(&struct{}{}); err != io.EOF {
			writeError(w, 400, "invalid profile request")
			return
		}
		invalidSubject := strings.IndexFunc(request.Subject, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) || unicode.Is(unicode.Cf, r) }) >= 0
		if request.Issuer != "https://accounts.angel-serv.com" || len(request.Subject) == 0 || len(request.Subject) > 255 || invalidSubject {
			writeError(w, 400, "invalid verified identity")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		profile, err := lookup(ctx, request.Issuer, request.Subject)
		if err != nil {
			writeError(w, 503, "gaming profile temporarily unavailable")
			return
		}
		writeJSON(w, 200, profile)
	})
}
