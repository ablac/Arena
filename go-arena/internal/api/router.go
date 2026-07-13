package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"arena-server/internal/config"
	"arena-server/internal/demobots"
	"arena-server/internal/game"
	"arena-server/internal/security"
	"arena-server/internal/version"
	"arena-server/internal/ws"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
)

// GlobalEventBus is the shared event bus for dashboard logging.
// It is initialised by NewRouter and accessible to other packages via this variable.
var GlobalEventBus *EventBus

// NewRouter builds the HTTP router with all API routes, WebSocket endpoints,
// middleware, and static file serving.
func NewRouter(engine *game.GameEngine, opts ...RouterOption) *chi.Mux {
	var ro routerOptions
	for _, opt := range opts {
		opt(&ro)
	}
	r := chi.NewRouter()

	// --- Event Bus for dashboard ---
	bus := NewEventBus()
	GlobalEventBus = bus

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
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Arena-Key", "X-Admin-Token", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	// Security headers (CSP, HSTS, X-Frame-Options, etc).
	r.Use(SecurityHeadersMiddleware)

	// Bound request body size to prevent oversized-body memory exhaustion.
	r.Use(BodySizeLimitMiddleware)

	// Dashboard HTTP logging middleware (before request logger so it captures all).
	r.Use(DashboardLogMiddleware(bus))

	// Request logging via slog.
	r.Use(requestLogger)

	// Panic recovery.
	r.Use(middleware.Recoverer)

	// --- API v1 routes ---

	// Create admin handler if demo manager provided.
	var adminHandler *AdminHandler
	if ro.demoManager != nil {
		adminHandler = NewAdminHandler(engine, ro.demoManager)
	} else {
		adminHandler = NewAdminHandler(engine, nil)
	}
	serviceStatus := NewServiceStatusService(engine, bus)
	adminHandler.ServiceStatus = serviceStatus
	adminHandler.Shutdown = ro.shutdown
	if err := serviceStatus.Load(context.Background()); err != nil {
		slog.Warn("failed to restore service status", "error", err)
	}

	// Create dashboard handler.
	dashboardHandler := NewDashboardHandler(bus, adminHandler)
	cosmeticsHandler := NewCosmeticsHandler(engine)
	commerceHandler := NewCosmeticCommerceHandler(engine)
	accountKeysHandler := NewAccountKeysHandler(engine)

	// Initialise OIDC handler (nil if disabled/misconfigured).
	oidcHandler := NewOIDCHandler()
	customerOIDCHandler := NewCustomerOIDCHandler()
	checkoutReady := commerceHandler.Enabled() && customerAccountAuthEnabled(customerOIDCHandler) && security.RedisClient != nil
	commerceHandler.checkoutEnabled = checkoutReady
	cosmeticsHandler.checkoutEnabled = checkoutReady

	// Developer lobby chat hub. The session resolver maps a request cookie
	// to a chat identity; it stays nil-safe when customer OIDC is disabled
	// (chat is then read-only for everyone).
	chatHub := ws.NewChatHub(engine)
	if config.C.ChatEnabled {
		warmCtx, cancelWarm := context.WithTimeout(context.Background(), 10*time.Second)
		chatHub.WarmHistory(warmCtx)
		cancelWarm()
	}
	chatSessionResolver := func(r *http.Request) *ws.ChatIdentity {
		if customerOIDCHandler == nil {
			return nil
		}
		session := customerOIDCHandler.GetSession(r)
		if session == nil {
			return nil
		}
		return &ws.ChatIdentity{AccountID: session.AccountID, Name: session.Name}
	}
	adminHandler.ChatHub = chatHub

	// --- OIDC routes (mounted OUTSIDE admin auth — these handle pre-auth flow) ---
	if oidcHandler != nil {
		// Rate-limited per IP so an attacker can't grow the in-memory CSRF
		// state/session maps unbounded by hammering /admin/login before the
		// 5-minute cleanup loop runs.
		oidcEntry := security.RateLimitMiddleware(config.C.AdminRateLimitRPM)
		r.With(oidcEntry).Get("/admin/login", oidcHandler.LoginHandler)
		r.With(oidcEntry).Get("/admin/callback", oidcHandler.CallbackHandler)
		r.Get("/admin/logout", oidcHandler.LogoutHandler)
		r.Get("/api/v1/admin/session", oidcHandler.SessionInfoHandler)
		// Mirror under /arena prefix
		r.With(oidcEntry).Get("/arena/admin/login", oidcHandler.LoginHandler)
		r.With(oidcEntry).Get("/arena/admin/callback", oidcHandler.CallbackHandler)
		r.Get("/arena/admin/logout", oidcHandler.LogoutHandler)
		r.Get("/arena/api/v1/admin/session", oidcHandler.SessionInfoHandler)
	} else {
		r.Get("/api/v1/admin/session", AdminSessionUnavailableHandler)
		r.Get("/arena/api/v1/admin/session", AdminSessionUnavailableHandler)
	}
	if customerOIDCHandler != nil {
		if customerOIDCHandler.oauth2Config != nil {
			customerOIDCEntry := security.RateLimitMiddleware(config.C.AdminRateLimitRPM)
			r.With(customerOIDCEntry).Get("/account/login", customerOIDCHandler.LoginHandler)
			r.With(customerOIDCEntry).Get("/account/callback", customerOIDCHandler.CallbackHandler)
			r.With(customerOIDCEntry).Get("/arena/account/login", customerOIDCHandler.LoginHandler)
			r.With(customerOIDCEntry).Get("/arena/account/callback", customerOIDCHandler.CallbackHandler)
		}
		r.With(MakeCustomerAuthMiddleware(customerOIDCHandler)).Post("/account/logout", customerOIDCHandler.LogoutHandler)
		r.With(MakeCustomerAuthMiddleware(customerOIDCHandler)).Post("/arena/account/logout", customerOIDCHandler.LogoutHandler)
	}

	r.Route("/api/v1", func(api chi.Router) {
		// Health check (public).
		api.Get("/health", healthHandler(engine))

		// Build identity (public) — commit hash of the running server.
		api.Get("/version", versionHandler())

		// Bot setup reference (public — no auth).
		api.Get("/bot-setup", BotSetup())
		api.Get("/content", PublicContentBlocks)
		api.Get("/service-status", serviceStatus.publicStatus)
		api.Get("/cosmetics/catalog", cosmeticsHandler.Catalog)
		api.Post("/cosmetics/webhooks/stripe", commerceHandler.StripeWebhook)
		if customerOIDCHandler != nil {
			if customerOIDCHandler.oauth2Config != nil {
				customerOIDCEntry := security.RateLimitMiddleware(config.C.AdminRateLimitRPM)
				api.With(customerOIDCEntry).Get("/dashboard/login", customerOIDCHandler.LoginHandler)
			} else {
				api.Get("/dashboard/login", CustomerLoginUnavailableHandler)
			}
			api.With(MakeCustomerAuthMiddleware(customerOIDCHandler)).Post("/dashboard/logout", customerOIDCHandler.LogoutHandler)
			api.Get("/account/session", customerOIDCHandler.SessionInfoHandler)
			registerCustomerEmailAuthRoutes(api, customerOIDCHandler)
		} else {
			api.Get("/dashboard/login", CustomerLoginUnavailableHandler)
			api.Post("/dashboard/logout", CustomerLoginUnavailableHandler)
			api.Get("/account/session", CustomerSessionUnavailableHandler)
		}
		api.Route("/account", func(account chi.Router) {
			account.Use(MakeCustomerAuthMiddleware(customerOIDCHandler))
			account.With(
				security.RateLimitMiddleware(config.C.CosmeticsAccountReadRPM),
			).Get("/cosmetics", cosmeticsHandler.AccountInventory)
			registerCustomerCosmeticCommerceRoutes(account, commerceHandler)
			account.Get("/keys", accountKeysHandler.List)
			account.With(
				security.FailClosedRateLimitMiddleware(config.C.CustomerAPIKeyMutationRPM),
			).Post("/keys", accountKeysHandler.Create)
			account.With(
				security.FailClosedRateLimitMiddleware(config.C.CustomerAPIKeyMutationRPM),
			).Delete("/keys/{key_id}", accountKeysHandler.Deactivate)
			account.With(
				security.FailClosedRateLimitMiddleware(config.C.CustomerBotLinkRPM),
			).Post("/bots", cosmeticsHandler.LinkAccountBot)
			account.Delete("/bots/{bot_id}", cosmeticsHandler.UnlinkAccountBot)
			account.Put("/cosmetic-licenses/{license_id}/assignment", cosmeticsHandler.AssignAccountLicense)
			account.Delete("/cosmetic-licenses/{license_id}/assignment", cosmeticsHandler.AssignAccountLicense)
			account.Put("/bots/{bot_id}/cosmetics", cosmeticsHandler.EquipAccountLicense)
		})

		// Public registration issues a server-generated, database-backed key and
		// bot. Verified accounts can claim it later through /account/bots.
		api.With(
			security.FailClosedRateLimitMiddleware(config.C.RateLimitRegisterRPM),
		).Post("/keys/generate", GenerateKey)

		// Authenticated routes.
		api.Group(func(auth chi.Router) {
			auth.Use(security.AuthMiddleware)

			auth.Delete("/keys/revoke", RevokeKey(engine))
			auth.With(
				security.RateLimitMiddleware(config.C.RateLimitBotConfigPerMin),
			).Put("/bot/config", UpdateBotConfig)
			auth.Get("/bot/stats", GetBotStats(engine))
			auth.Get("/bot/live", GetBotLive(engine))
			auth.Get("/bot/cosmetics", cosmeticsHandler.BotInventory)
			auth.Put("/bot/cosmetics", cosmeticsHandler.Equip)
		})

		// Leaderboard (public).
		api.Get("/leaderboard", GetLeaderboard)
		api.Get("/bounties", GetBountyBoard(engine))
		api.Get("/weapon-stats", GetWeaponStats)

		// Arena status (public).
		api.Get("/arena/status", GetArenaStatus(engine))

		// Arena map (public) — returns current terrain grid.
		api.Get("/arena/map", GetArenaMap(engine))

		// Chat configuration (public).
		api.Get("/chat/config", ChatConfigHandler)

		// Admin routes (token-authenticated or OIDC session, rate-limited).
		api.Route("/admin", func(admin chi.Router) {
			admin.Use(MakeAdminAuthMiddlewareWithOIDC(adminHandler, oidcHandler))
			admin.Use(security.RateLimitMiddleware(config.C.AdminRateLimitRPM))
			adminHandler.Routes(admin)
			registerCosmeticsAdminRoutes(admin, cosmeticsHandler)
			admin.Get("/cosmetics/orders", commerceHandler.AdminOrders)

			// Dashboard API endpoints.
			admin.Route("/dashboard", func(dash chi.Router) {
				dashboardHandler.DashboardRoutes(dash)
			})
		})
	})

	// --- WebSocket endpoints ---
	r.Get("/ws/bot", ws.BotHandler(engine))
	r.Get("/ws/spectator", ws.SpectatorHandler(engine))
	r.Get("/ws/chat", ws.ChatHandler(engine, chatHub, chatSessionResolver))
	r.Post("/internal/updater/status", serviceStatus.updaterStatusCallback)

	// The public reverse proxy can mount the app behind an /arena prefix.
	// Mount the same routes under /arena prefix for compatibility.
	r.Route("/arena", func(ar chi.Router) {
		ar.Get("/ws/bot", ws.BotHandler(engine))
		ar.Get("/ws/spectator", ws.SpectatorHandler(engine))
		ar.Get("/ws/chat", ws.ChatHandler(engine, chatHub, chatSessionResolver))

		ar.Route("/api/v1", func(api chi.Router) {
			api.Get("/health", healthHandler(engine))
			api.Get("/version", versionHandler())
			api.Get("/bot-setup", BotSetup())
			api.Get("/content", PublicContentBlocks)
			api.Get("/service-status", serviceStatus.publicStatus)
			api.Get("/cosmetics/catalog", cosmeticsHandler.Catalog)
			api.Post("/cosmetics/webhooks/stripe", commerceHandler.StripeWebhook)
			if customerOIDCHandler != nil {
				if customerOIDCHandler.oauth2Config != nil {
					customerOIDCEntry := security.RateLimitMiddleware(config.C.AdminRateLimitRPM)
					api.With(customerOIDCEntry).Get("/dashboard/login", customerOIDCHandler.LoginHandler)
				} else {
					api.Get("/dashboard/login", CustomerLoginUnavailableHandler)
				}
				api.With(MakeCustomerAuthMiddleware(customerOIDCHandler)).Post("/dashboard/logout", customerOIDCHandler.LogoutHandler)
				api.Get("/account/session", customerOIDCHandler.SessionInfoHandler)
				registerCustomerEmailAuthRoutes(api, customerOIDCHandler)
			} else {
				api.Get("/dashboard/login", CustomerLoginUnavailableHandler)
				api.Post("/dashboard/logout", CustomerLoginUnavailableHandler)
				api.Get("/account/session", CustomerSessionUnavailableHandler)
			}
			api.Route("/account", func(account chi.Router) {
				account.Use(MakeCustomerAuthMiddleware(customerOIDCHandler))
				account.With(
					security.RateLimitMiddleware(config.C.CosmeticsAccountReadRPM),
				).Get("/cosmetics", cosmeticsHandler.AccountInventory)
				registerCustomerCosmeticCommerceRoutes(account, commerceHandler)
				account.Get("/keys", accountKeysHandler.List)
				account.With(
					security.FailClosedRateLimitMiddleware(config.C.CustomerAPIKeyMutationRPM),
				).Post("/keys", accountKeysHandler.Create)
				account.With(
					security.FailClosedRateLimitMiddleware(config.C.CustomerAPIKeyMutationRPM),
				).Delete("/keys/{key_id}", accountKeysHandler.Deactivate)
				account.With(
					security.FailClosedRateLimitMiddleware(config.C.CustomerBotLinkRPM),
				).Post("/bots", cosmeticsHandler.LinkAccountBot)
				account.Delete("/bots/{bot_id}", cosmeticsHandler.UnlinkAccountBot)
				account.Put("/cosmetic-licenses/{license_id}/assignment", cosmeticsHandler.AssignAccountLicense)
				account.Delete("/cosmetic-licenses/{license_id}/assignment", cosmeticsHandler.AssignAccountLicense)
				account.Put("/bots/{bot_id}/cosmetics", cosmeticsHandler.EquipAccountLicense)
			})
			api.With(
				security.FailClosedRateLimitMiddleware(config.C.RateLimitRegisterRPM),
			).Post("/keys/generate", GenerateKey)
			api.Group(func(auth chi.Router) {
				auth.Use(security.AuthMiddleware)
				auth.Delete("/keys/revoke", RevokeKey(engine))
				auth.With(
					security.RateLimitMiddleware(config.C.RateLimitBotConfigPerMin),
				).Put("/bot/config", UpdateBotConfig)
				auth.Get("/bot/stats", GetBotStats(engine))
				auth.Get("/bot/live", GetBotLive(engine))
				auth.Get("/bot/cosmetics", cosmeticsHandler.BotInventory)
				auth.Put("/bot/cosmetics", cosmeticsHandler.Equip)
			})
			api.Get("/leaderboard", GetLeaderboard)
			api.Get("/bounties", GetBountyBoard(engine))
			api.Get("/weapon-stats", GetWeaponStats)
			api.Get("/arena/status", GetArenaStatus(engine))
			api.Get("/arena/map", GetArenaMap(engine))
			api.Get("/chat/config", ChatConfigHandler)

			// Admin routes (mirrored under /arena prefix).
			api.Route("/admin", func(admin chi.Router) {
				admin.Use(MakeAdminAuthMiddlewareWithOIDC(adminHandler, oidcHandler))
				admin.Use(security.RateLimitMiddleware(config.C.AdminRateLimitRPM))
				adminHandler.Routes(admin)
				registerCosmeticsAdminRoutes(admin, cosmeticsHandler)
				admin.Get("/cosmetics/orders", commerceHandler.AdminOrders)

				admin.Route("/dashboard", func(dash chi.Router) {
					dashboardHandler.DashboardRoutes(dash)
				})
			})
		})

		// Static files under /arena/
		frontendDirArena := resolveFrontendDir()
		fileServerArena := http.StripPrefix("/arena", http.FileServer(http.Dir(frontendDirArena)))
		ar.Handle("/*", noCacheStaticHandler(fileServerArena))
	})

	// --- Static file serving ---
	// Serve the frontend directory at the root path with no-cache for JS/CSS.
	frontendDir := resolveFrontendDir()
	fileServer := http.FileServer(http.Dir(frontendDir))
	r.Handle("/*", noCacheStaticHandler(fileServer))

	return r
}

func registerCustomerEmailAuthRoutes(api chi.Router, handler *CustomerOIDCHandler) {
	if handler == nil || handler.emailSender == nil || handler.emailStore == nil {
		return
	}
	api.With(security.FailClosedRateLimitMiddleware(config.C.CustomerEmailSendRPM)).Post("/account/email/start", handler.EmailStartHandler)
	api.With(security.RateLimitMiddleware(config.C.AdminRateLimitRPM)).Post("/account/email/verify", handler.EmailVerifyHandler)
}

func registerCustomerCosmeticCommerceRoutes(account chi.Router, handler *CosmeticCommerceHandler) {
	account.Get("/cosmetics/orders", handler.CustomerOrders)
	checkoutQuota := security.FailClosedRateLimitMiddleware(config.C.CosmeticsCheckoutRPM)
	account.With(checkoutQuota).Post("/cosmetics/checkout", handler.Checkout)
	account.With(checkoutQuota).Post("/cosmetics/subscription/checkout", handler.SubscriptionCheckout)
	account.With(checkoutQuota).Post("/cosmetics/subscription/portal", handler.SubscriptionPortal)
}

// healthHandler returns a handler for GET /api/v1/health.
func healthHandler(engine *game.GameEngine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, HealthResponse{
			Status:     "ok",
			BotsOnline: engine.ConnectedBotCount(),
			Commit:     version.ShortCommit(),
		})
	}
}

// versionHandler returns a handler for GET /api/v1/version — the build
// identity of the running server (git commit, build time), used by the
// frontend About dialog.
func versionHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, VersionResponse{
			Status:      "ok",
			Commit:      version.ResolvedCommit(),
			CommitShort: version.ShortCommit(),
			BuildTime:   version.BuildTime,
			GoVersion:   runtime.Version(),
			Repo:        "https://github.com/ablac/Arena",
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

// noCacheStaticHandler wraps a file server to set no-cache headers on JS/CSS
// files and on HTML documents (including extensionless directory routes
// like "/", "/dashboard/", "/admin/", "/m/", "/shop/" that http.FileServer resolves to
// an index.html) so browsers always fetch the latest version. Without this,
// a browser that cached a bad response for one of these routes keeps
// serving it from cache after the server is fixed, since nothing tells it
// to revalidate. Other static assets (textures, fonts, favicon) keep normal
// caching.
func noCacheStaticHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		lastSegment := path[strings.LastIndex(path, "/")+1:]
		isVersionedAsset := strings.HasSuffix(path, ".js") || strings.HasSuffix(path, ".css")
		isHTMLDocument := strings.HasSuffix(path, ".html") || !strings.Contains(lastSegment, ".")
		if isVersionedAsset || isHTMLDocument {
			w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
			w.Header().Set("Pragma", "no-cache")
			w.Header().Set("Expires", "0")
		}
		next.ServeHTTP(w, r)
	})
}

// routerOptions holds optional configuration for the router.
type routerOptions struct {
	demoManager *demobots.Manager
	shutdown    func()
}

// RouterOption is a functional option for NewRouter.
type RouterOption func(*routerOptions)

// WithDemoManager provides the demo bot manager to the router for admin endpoints.
func WithDemoManager(m *demobots.Manager) RouterOption {
	return func(o *routerOptions) {
		o.demoManager = m
	}
}

// WithShutdown provides the graceful process-cancellation callback used by the
// authenticated admin restart endpoint.
func WithShutdown(shutdown func()) RouterOption {
	return func(o *routerOptions) {
		o.shutdown = shutdown
	}
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
