package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"arena-server/internal/config"
	"arena-server/internal/game"
	"arena-server/internal/security"
	"arena-server/internal/ws"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
)

// NewRouter builds the HTTP router with all API routes, WebSocket endpoints,
// middleware, and static file serving.
func NewRouter(engine *game.GameEngine) *chi.Mux {
	r := chi.NewRouter()

	// --- Middleware ---

	// CORS: use origins from config.
	corsOrigins := []string{"*"}
	if config.C.CORSOrigins != "*" {
		corsOrigins = strings.Split(config.C.CORSOrigins, ",")
		for i := range corsOrigins {
			corsOrigins[i] = strings.TrimSpace(corsOrigins[i])
		}
	}
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   corsOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Arena-Key"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	// Request logging via slog.
	r.Use(requestLogger)

	// Panic recovery.
	r.Use(middleware.Recoverer)

	// --- API v1 routes ---

	r.Route("/api/v1", func(api chi.Router) {
		// Health check (public).
		api.Get("/health", healthHandler(engine))

		// Key generation (public, rate-limited per IP for registration).
		api.With(
			security.RateLimitMiddleware(config.C.RateLimitRegisterPerHour),
		).Post("/keys/generate", GenerateKey)

		// Authenticated routes.
		api.Group(func(auth chi.Router) {
			auth.Use(security.AuthMiddleware)

			auth.Delete("/keys/revoke", RevokeKey)
			auth.With(
				security.RateLimitMiddleware(config.C.RateLimitBotConfigPerMin),
			).Put("/bot/config", UpdateBotConfig)
			auth.Get("/bot/stats", GetBotStats(engine))
		})

		// Leaderboard (public).
		api.Get("/leaderboard", GetLeaderboard)

		// Arena status (public).
		api.Get("/arena/status", GetArenaStatus(engine))
	})

	// --- WebSocket endpoints ---
	r.Get("/ws/bot", ws.BotHandler(engine))
	r.Get("/ws/spectator", ws.SpectatorHandler(engine))

	// Caddy rewrites arena.angel-serv.com/* → /arena/*
	// Mount the same routes under /arena prefix for compatibility.
	r.Route("/arena", func(ar chi.Router) {
		ar.Get("/ws/bot", ws.BotHandler(engine))
		ar.Get("/ws/spectator", ws.SpectatorHandler(engine))

		ar.Route("/api/v1", func(api chi.Router) {
			api.Get("/health", healthHandler(engine))
			api.With(
				security.RateLimitMiddleware(config.C.RateLimitRegisterPerHour),
			).Post("/keys/generate", GenerateKey)
			api.Group(func(auth chi.Router) {
				auth.Use(security.AuthMiddleware)
				auth.Delete("/keys/revoke", RevokeKey)
				auth.With(
					security.RateLimitMiddleware(config.C.RateLimitBotConfigPerMin),
				).Put("/bot/config", UpdateBotConfig)
				auth.Get("/bot/stats", GetBotStats(engine))
			})
			api.Get("/leaderboard", GetLeaderboard)
			api.Get("/arena/status", GetArenaStatus(engine))
		})

		// Static files under /arena/
		frontendDirArena := resolveFrontendDir()
		fileServerArena := http.StripPrefix("/arena", http.FileServer(http.Dir(frontendDirArena)))
		ar.Handle("/*", fileServerArena)
	})

	// --- Static file serving ---
	// Serve the frontend directory at the root path.
	frontendDir := resolveFrontendDir()
	fileServer := http.FileServer(http.Dir(frontendDir))
	r.Handle("/*", fileServer)

	return r
}

// healthHandler returns a handler for GET /api/v1/health.
func healthHandler(engine *game.GameEngine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, HealthResponse{
			Status:     "ok",
			BotsOnline: engine.ConnectedBotCount(),
		})
	}
}

// writeJSON serialises data as JSON and writes it to the response with the
// given HTTP status code.
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		slog.Error("failed to write JSON response", "error", err)
	}
}

// writeError writes a standard ErrorResponse.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, ErrorResponse{Error: message})
}

// requestLogger is a lightweight slog-based request logging middleware.
func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)

		slog.Info("http",
			"method", r.Method,
			"path", r.URL.Path,
			"status", ww.Status(),
			"bytes", ww.BytesWritten(),
			"duration", time.Since(start).String(),
			"remote", r.RemoteAddr,
		)
	})
}

// resolveFrontendDir determines the path to the frontend directory.
// It first checks for a "../frontend" directory relative to the working
// directory, then falls back to a "/frontend" absolute path (for Docker).
func resolveFrontendDir() string {
	// Try relative to working directory.
	if wd, err := os.Getwd(); err == nil {
		candidate := filepath.Join(wd, "..", "frontend")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
	}

	// Docker / absolute fallback.
	if info, err := os.Stat("/frontend"); err == nil && info.IsDir() {
		return "/frontend"
	}

	// Last resort: relative path.
	return "../frontend"
}
