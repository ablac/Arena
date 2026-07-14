package ws

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	"arena-server/internal/config"
	"arena-server/internal/db"
	"arena-server/internal/game"
	"arena-server/internal/security"

	"github.com/gorilla/websocket"
)

// Developer lobby chat: a WebSocket hub for the humans building the bots.
// Deliberately separate from /ws/bot (gameplay) and /ws/spectator (delayed
// broadcast). Anyone may connect and read; posting requires a signed-in
// customer session, and - because a real-time channel readable by a bot
// process would bypass the spectator stream's anti-radar delay - posting is
// locked while any bot linked to the poster's account is alive in an active
// round (config.ChatAliveLock).

const (
	chatPingInterval      = 30 * time.Second
	chatPongTimeout       = 60 * time.Second
	chatHeartbeatInterval = 10 * time.Second
	chatWriteTimeout      = 10 * time.Second
	chatSendBuffer        = 32
	chatReadLimit         = 4096
)

// Chat wire messages. Structs keep the "type" field first, matching the
// package convention that outbound messages are classifiable by prefix.
type chatWireMessage struct {
	ID        int64  `json:"id"`
	Handle    string `json:"handle"`
	Body      string `json:"body"`
	Timestamp int64  `json:"ts"`
}

type chatStatusMessage struct {
	Type    string `json:"type"`
	Handle  string `json:"handle,omitempty"`
	CanPost bool   `json:"can_post"`
	Reason  string `json:"reason,omitempty"`
}

type chatHistoryMessage struct {
	Type     string            `json:"type"`
	Messages []chatWireMessage `json:"messages"`
}

type chatBroadcastMessage struct {
	Type    string          `json:"type"`
	Message chatWireMessage `json:"message"`
}

type chatHiddenMessage struct {
	Type string `json:"type"`
	ID   int64  `json:"id"`
}

type chatErrorMessage struct {
	Type    string `json:"type"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type chatHeartbeat struct {
	Type       string `json:"type"`
	ServerTime int64  `json:"server_time"`
}

// ChatPostMessage is the single client-to-server message type.
type ChatPostMessage struct {
	Type string `json:"type"`
	Body string `json:"body"`
}

// ChatIdentity is the resolved posting identity for one connection. A nil
// identity is a read-only (anonymous) connection.
type ChatIdentity struct {
	AccountID string
	Name      string
}

// ChatStore is the persistence surface the hub needs. The default
// implementation is backed by the db package; tests substitute a fake.
type ChatStore interface {
	RecentMessages(ctx context.Context, limit int) ([]db.ChatMessage, error)
	Insert(ctx context.Context, m *db.ChatMessage) error
	ChatBanUntil(ctx context.Context, accountID string) (*time.Time, error)
	LinkedBotIDs(ctx context.Context, accountID string) ([]string, error)
}

type dbChatStore struct{}

func (dbChatStore) RecentMessages(ctx context.Context, limit int) ([]db.ChatMessage, error) {
	return db.ListRecentChatMessages(ctx, limit)
}
func (dbChatStore) Insert(ctx context.Context, m *db.ChatMessage) error {
	return db.InsertChatMessage(ctx, m)
}
func (dbChatStore) ChatBanUntil(ctx context.Context, accountID string) (*time.Time, error) {
	return db.GetCustomerChatBan(ctx, accountID)
}
func (dbChatStore) LinkedBotIDs(ctx context.Context, accountID string) ([]string, error) {
	return db.ListLinkedBotIDs(ctx, accountID)
}

type chatClient struct {
	conn     *websocket.Conn
	send     chan []byte
	identity *ChatIdentity
	handle   string
	ip       string
}

// ChatHub owns every chat connection and the recent-message ring. It is the
// SOLE closer of a client's send channel (closed only after the client is
// removed from the map under the hub lock), which is what lets broadcasts
// skip the recover() dance the spectator path needs.
type ChatHub struct {
	mu      sync.Mutex
	clients map[*chatClient]struct{}
	ring    []db.ChatMessage

	store       ChatStore
	isBotAlive  func(botID string) bool
	roundActive func() bool

	// postWindows is the per-account sliding window used to throttle posts
	// when Redis is unavailable (security.CheckRateLimit fails open). Keyed
	// by account so opening extra sockets does not multiply a poster's
	// budget. Guarded by mu; empty entries are pruned so it stays bounded to
	// accounts active in the last minute.
	postWindows map[string][]time.Time

	// memID hands out ids when the database is absent (dev mode) so hide
	// and dedup still work within the process lifetime.
	memID atomic.Int64
}

// NewChatHub builds the production hub wired to the game engine and the
// database-backed store.
func NewChatHub(engine *game.GameEngine) *ChatHub {
	h := newChatHub(dbChatStore{},
		func(botID string) bool {
			detail, ok := engine.GetBotDetail(botID)
			if !ok {
				return false
			}
			alive, _ := detail["is_alive"].(bool)
			return alive
		},
		func() bool {
			return engine.GetArenaSnapshot().Phase == game.PhaseActive
		},
	)
	return h
}

func newChatHub(store ChatStore, isBotAlive func(string) bool, roundActive func() bool) *ChatHub {
	h := &ChatHub{
		clients:     make(map[*chatClient]struct{}),
		postWindows: make(map[string][]time.Time),
		store:       store,
		isBotAlive:  isBotAlive,
		roundActive: roundActive,
	}
	h.memID.Store(time.Now().UnixMilli())
	return h
}

// reservePostWindow reserves a post slot in the per-account fallback window
// (used when Redis is down). Prune, check, and append happen under ONE lock
// so concurrent sockets for the same account cannot all pass the check
// before any of them records a slot. The slot is consumed even when the
// post later fails a downstream check (ban, alive-lock, insert); a rejected
// poster burning budget is acceptable and also throttles error spam.
func (h *ChatHub) reservePostWindow(accountID string, limit int, now time.Time) bool {
	cutoff := now.Add(-time.Minute)
	h.mu.Lock()
	defer h.mu.Unlock()
	times := h.postWindows[accountID]
	kept := times[:0]
	for _, t := range times {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= limit {
		h.postWindows[accountID] = kept
		return false
	}
	h.postWindows[accountID] = append(kept, now)
	return true
}

// WarmHistory seeds the in-memory ring from the database at startup. A
// missing database is fine (dev mode); the ring just starts empty.
func (h *ChatHub) WarmHistory(ctx context.Context) {
	msgs, err := h.store.RecentMessages(ctx, config.C.ChatHistorySize)
	if err != nil {
		if !errors.Is(err, db.ErrNoDatabase) {
			slog.Error("chat history warm failed", "error", err)
		}
		return
	}
	h.mu.Lock()
	h.ring = msgs
	h.mu.Unlock()
}

// register admits a client unless the hub is at capacity. Admission and the
// capacity check are one atomic operation, mirroring TryAddSpectator.
func (h *ChatHub) register(c *chatClient, maxClients int) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if maxClients <= 0 || len(h.clients) >= maxClients {
		return false
	}
	h.clients[c] = struct{}{}
	return true
}

// remove deletes the client and closes its send channel. Because both happen
// under the hub lock, no broadcast can write to a closed channel.
func (h *ChatHub) remove(c *chatClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.clients[c]; !ok {
		return
	}
	delete(h.clients, c)
	close(c.send)
}

// broadcastLocked sends to every client without blocking the hub: a client
// with a full buffer drops the message, same as the spectator path.
func (h *ChatHub) broadcastLocked(payload []byte) {
	for c := range h.clients {
		select {
		case c.send <- payload:
		default:
		}
	}
}

// ClientCount reports the number of connected chat clients.
func (h *ChatHub) ClientCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}

// HideMessage removes a message from the live ring and tells connected
// clients to drop it. The database soft-delete is the caller's job (the
// admin handler), so the hub stays storage-agnostic.
func (h *ChatHub) HideMessage(id int64) {
	payload, _ := json.Marshal(chatHiddenMessage{Type: "chat_message_hidden", ID: id})
	h.mu.Lock()
	defer h.mu.Unlock()
	for i, m := range h.ring {
		if m.ID == id {
			h.ring = append(h.ring[:i], h.ring[i+1:]...)
			break
		}
	}
	h.broadcastLocked(payload)
}

type chatPostError struct {
	code    string
	message string
}

// post runs the full validation pipeline for one incoming message and, on
// success, persists, records in the ring, and broadcasts it.
func (h *ChatHub) post(ctx context.Context, c *chatClient, rawBody string) *chatPostError {
	if c.identity == nil {
		return &chatPostError{"AUTH_REQUIRED", "sign in to post in chat"}
	}

	body, ok := sanitizeChatBody(rawBody, config.C.ChatMaxBodyLen)
	if !ok {
		return &chatPostError{"INVALID_BODY", fmt.Sprintf("message must be 1-%d characters", config.C.ChatMaxBodyLen)}
	}

	now := time.Now()

	// Per-account fallback window: holds across a poster's sockets even when
	// Redis is down (security.CheckRateLimit fails open).
	if !h.reservePostWindow(c.identity.AccountID, config.C.ChatPostsPerMin, now) {
		return &chatPostError{"RATE_LIMITED", "slow down: too many messages"}
	}

	// Cross-instance limit (Redis), keyed by the same account.
	if allowed, _, _, err := security.CheckRateLimit(ctx, "chat:post:"+c.identity.AccountID, config.C.ChatPostsPerMin, 60); err == nil && !allowed {
		return &chatPostError{"RATE_LIMITED", "slow down: too many messages"}
	}

	// Fails CLOSED on a real DB error, matching the alive-lock check below:
	// a banned poster must not slip through on a transient outage.
	// db.ErrNoDatabase (dev mode, no ban data) is the one intentional
	// pass-through.
	switch until, err := h.store.ChatBanUntil(ctx, c.identity.AccountID); {
	case errors.Is(err, db.ErrNoDatabase):
		// Dev mode: no ban table, check cannot apply.
	case err != nil:
		slog.Error("chat ban lookup failed", "error", err)
		return &chatPostError{"POST_FAILED", "message could not be verified, try again"}
	case until != nil && until.After(now):
		return &chatPostError{"CHAT_BANNED", "you are muted in chat until " + until.UTC().Format(time.RFC3339)}
	}

	// The integrity lock. During an active round a poster must have at least
	// one linked bot AND none of them may be alive. Requiring linkage is what
	// makes the lock meaningful: an unlinked account could otherwise dodge it
	// forever by simply never linking its bot, so "do not link" cannot be a
	// cheat. The lookup fails CLOSED on a real DB error (a live bot must not
	// slip through on an outage); ErrNoDatabase (dev mode, no linkage data)
	// is the one intentional pass-through.
	if config.C.ChatAliveLock && h.roundActive != nil && h.roundActive() {
		botIDs, err := h.store.LinkedBotIDs(ctx, c.identity.AccountID)
		switch {
		case errors.Is(err, db.ErrNoDatabase):
			// Dev mode: no linkage table, lock cannot apply.
		case err != nil:
			slog.Error("chat alive-lock lookup failed", "error", err)
			return &chatPostError{"POST_FAILED", "message could not be verified, try again"}
		case len(botIDs) == 0:
			return &chatPostError{"LINK_REQUIRED", "link a bot to your account to chat during a live round"}
		default:
			for _, id := range botIDs {
				if h.isBotAlive != nil && h.isBotAlive(id) {
					return &chatPostError{"BOT_ALIVE_LOCK", "chat unlocks when your bot is out of the round"}
				}
			}
		}
	}

	msg := &db.ChatMessage{
		AccountID: &c.identity.AccountID,
		Handle:    c.handle,
		Body:      body,
		IP:        c.ip,
		CreatedAt: now,
	}
	if err := h.store.Insert(ctx, msg); err != nil {
		if errors.Is(err, db.ErrNoDatabase) {
			msg.ID = h.memID.Add(1)
		} else {
			slog.Error("chat message insert failed", "error", err)
			return &chatPostError{"POST_FAILED", "message could not be saved, try again"}
		}
	}

	payload, _ := json.Marshal(chatBroadcastMessage{
		Type:    "chat_message",
		Message: toWireMessage(*msg),
	})

	h.mu.Lock()
	defer h.mu.Unlock()
	h.ring = append(h.ring, *msg)
	if over := len(h.ring) - config.C.ChatHistorySize; over > 0 {
		h.ring = h.ring[over:]
	}
	h.broadcastLocked(payload)
	return nil
}

func (h *ChatHub) historyPayload() []byte {
	h.mu.Lock()
	msgs := make([]chatWireMessage, 0, len(h.ring))
	for _, m := range h.ring {
		msgs = append(msgs, toWireMessage(m))
	}
	h.mu.Unlock()
	payload, _ := json.Marshal(chatHistoryMessage{Type: "chat_history", Messages: msgs})
	return payload
}

func toWireMessage(m db.ChatMessage) chatWireMessage {
	return chatWireMessage{
		ID:        m.ID,
		Handle:    m.Handle,
		Body:      m.Body,
		Timestamp: m.CreatedAt.UnixMilli(),
	}
}

// sanitizeChatBody normalizes a raw chat body: newlines and tabs collapse to
// single spaces, other control characters are stripped, whitespace is
// trimmed, and the result must be valid UTF-8 of 1..maxRunes runes.
func sanitizeChatBody(raw string, maxRunes int) (string, bool) {
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
			// Drop control (Cc) and format (Cf) runes. Cf covers zero-width
			// characters (U+200B..D, U+FEFF), the soft hyphen, and the bidi
			// overrides used to spoof handles and hide message content.
		default:
			b.WriteRune(r)
		}
	}
	body := strings.TrimSpace(b.String())
	if body == "" {
		return "", false
	}
	if maxRunes > 0 && utf8.RuneCountInString(body) > maxRunes {
		return "", false
	}
	return body, true
}

// chatHandle derives the spoof-resistant display handle: the sanitized
// account display name plus a stable discriminator hashed from the account
// id. The name is user-settable at the IdP, so the discriminator is what
// distinguishes two people who both call themselves "ADMIN"; hashing the
// full id (rather than truncating it) avoids leaking the raw id prefix, and
// 8 hex chars (32 bits) makes a targeted same-name collision impractical.
func chatHandle(identity *ChatIdentity) string {
	name, _ := sanitizeChatBody(identity.Name, 24)
	if name == "" {
		name = "dev"
	}
	sum := sha256.Sum256([]byte(identity.AccountID))
	disc := hex.EncodeToString(sum[:])[:8]
	return name + "#" + disc
}

// chatSameOrigin reports whether a browser request's Origin header matches
// the request host. Anti cross-site-WebSocket-hijacking: a session cookie is
// only honored on same-origin upgrades, so a malicious page cannot post as a
// logged-in visitor. Requests with no Origin header (non-browser clients,
// which do not hold ambient cookies) pass.
func chatSameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	// Scheme must match too: an http page and an https page on the same host
	// are different origins, and honoring a cookie across that boundary would
	// weaken the CSWSH protection.
	if !strings.EqualFold(u.Scheme, requestScheme(r)) {
		return false
	}
	return strings.EqualFold(stripDefaultPort(u.Host, u.Scheme), stripDefaultPort(r.Host, requestScheme(r)))
}

func requestScheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		return proto
	}
	return "http"
}

func stripDefaultPort(host, scheme string) string {
	host = strings.ToLower(host)
	switch scheme {
	case "https", "wss":
		return strings.TrimSuffix(host, ":443")
	case "http", "ws":
		return strings.TrimSuffix(host, ":80")
	}
	return host
}

var chatWriteBufferPool sync.Pool

var chatUpgrader = websocket.Upgrader{
	HandshakeTimeout:  5 * time.Second,
	ReadBufferSize:    1024,
	WriteBufferSize:   4096,
	WriteBufferPool:   &chatWriteBufferPool,
	EnableCompression: true,
	CheckOrigin: func(r *http.Request) bool {
		// Reads are public (mirrors the spectator stream); identity is
		// origin-gated separately in ChatHandler.
		return true
	},
}

// ChatHandler serves /ws/chat. resolveSession maps a request to a posting
// identity (nil for anonymous); the router builds it from the customer OIDC
// handler so this package stays decoupled from the api package.
func ChatHandler(engine *game.GameEngine, hub *ChatHub, resolveSession func(*http.Request) *ChatIdentity) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		admission := beginWebSocketAdmission(websocketEndpointChat)
		defer admission.finish()

		cfg := &config.C
		if !cfg.ChatEnabled {
			http.Error(w, "chat is disabled", http.StatusNotFound)
			return
		}

		clientIP := security.ExtractClientIP(r)
		if engine != nil && engine.IsIPBanned(clientIP) {
			admission.fail(websocketFailureAuth)
			http.Error(w, "IP banned", http.StatusForbidden)
			return
		}

		// Per-IP connect budget, same shape and gating as the bot WS
		// endpoint: a limit of 0 disables it, and loopback (local dev, tests)
		// is exempt.
		if cfg.WSConnectRatePerMin > 0 && !isLoopbackIP(clientIP) {
			if allowed, _, _, err := security.CheckRateLimit(r.Context(), "ws:chat:ip:"+clientIP, cfg.WSConnectRatePerMin, 60); err == nil && !allowed {
				admission.fail(websocketFailureRateLimit)
				http.Error(w, "too many connections, try again shortly", http.StatusTooManyRequests)
				return
			}
		}

		// Resolve identity BEFORE upgrading, and only on same-origin
		// requests: a cross-site page may read the public stream but can
		// never wield the visitor's session cookie to post.
		var identity *ChatIdentity
		if resolveSession != nil && chatSameOrigin(r) {
			identity = resolveSession(r)
		}

		conn, err := chatUpgrader.Upgrade(w, r, nil)
		if err != nil {
			admission.fail(websocketFailureUpgrade)
			slog.Error("chat websocket upgrade failed", "error", err, "remote", r.RemoteAddr)
			return
		}
		admission.upgraded()
		conn.SetReadLimit(chatReadLimit)

		defer func() {
			if p := recover(); p != nil {
				slog.Error("panic in chat handler", "recover", p)
			}
			conn.Close()
		}()

		client := &chatClient{
			conn:     conn,
			send:     make(chan []byte, chatSendBuffer),
			identity: identity,
			ip:       clientIP,
		}
		if identity != nil {
			client.handle = chatHandle(identity)
		}

		// Enqueue the initial status + history BEFORE registering. While the
		// client is not yet in the hub map the handler is the channel's only
		// producer, so these two sends into the 32-slot buffer provably
		// cannot block. Registering first would let a concurrent broadcast
		// fill the buffer and wedge these blocking sends on a channel whose
		// writer has not started.
		status := chatStatusMessage{Type: "chat_status", CanPost: identity != nil}
		if identity != nil {
			status.Handle = client.handle
		} else {
			status.Reason = "sign_in_required"
		}
		statusPayload, _ := json.Marshal(status)
		client.send <- statusPayload
		client.send <- hub.historyPayload()

		if !hub.register(client, cfg.ChatMaxClients) {
			admission.fail(websocketFailureCapacity)
			_ = conn.WriteControl(
				websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseTryAgainLater, "chat is full"),
				time.Now().Add(chatWriteTimeout),
			)
			return
		}
		admission.admitted()
		// Cleanup on the deferred path so a panic in the reader pipeline still
		// unregisters the client and closes its send channel. remove is
		// idempotent; defers run LIFO so cancel fires before remove, matching
		// the intended shutdown order.
		ctx, cancel := context.WithCancel(context.Background())
		defer func() {
			cancel()
			hub.remove(client)
		}()

		go chatWriter(ctx, client)

		chatReader(r.Context(), hub, client)
	}
}

// isLoopbackIP reports whether ip is a loopback address (127.0.0.1, ::1).
func isLoopbackIP(ip string) bool {
	parsed := net.ParseIP(ip)
	return parsed != nil && parsed.IsLoopback()
}

// chatReader parses incoming frames on the handler goroutine. Its return is
// the single "connection over" signal.
func chatReader(ctx context.Context, hub *ChatHub, c *chatClient) {
	c.conn.SetReadDeadline(time.Now().Add(chatPongTimeout))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(chatPongTimeout))
		return nil
	})

	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				slog.Debug("chat read error", "error", err)
			}
			return
		}
		c.conn.SetReadDeadline(time.Now().Add(chatPongTimeout))

		// The frontend socket class sends a bare "ping" text keepalive.
		if string(data) == "ping" {
			continue
		}

		msgType, msg, err := parseChatMessage(data)
		if err != nil {
			sendChatError(c, "BAD_MESSAGE", "unrecognized message")
			continue
		}
		switch msgType {
		case "chat_post":
			post := msg.(*ChatPostMessage)
			if perr := hub.post(ctx, c, post.Body); perr != nil {
				sendChatError(c, perr.code, perr.message)
			}
		}
	}
}

// parseChatMessage mirrors ParseBotMessage: sniff the type, then strict
// decode into the dedicated struct.
func parseChatMessage(data []byte) (string, interface{}, error) {
	if err := rejectDuplicateJSONFields(data); err != nil {
		return "", nil, err
	}
	var raw RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return "", nil, fmt.Errorf("invalid JSON: %w", err)
	}
	switch raw.Type {
	case "chat_post":
		var msg ChatPostMessage
		if err := unmarshalStrict(data, &msg); err != nil {
			return "", nil, fmt.Errorf("invalid chat_post: %w", err)
		}
		return raw.Type, &msg, nil
	default:
		return "", nil, fmt.Errorf("unknown message type: %s", raw.Type)
	}
}

// sendChatError enqueues an error for one client, dropping it if the client
// is too far behind (the connection is already in trouble then).
func sendChatError(c *chatClient, code, message string) {
	payload, _ := json.Marshal(chatErrorMessage{Type: "chat_error", Code: code, Message: message})
	select {
	case c.send <- payload:
	default:
	}
}

// chatWriter drains the send channel, keeps the connection alive with ping
// frames, and emits app-level heartbeats (browser JS cannot see ping frames,
// and the frontend's stale-stream timer needs periodic application data).
func chatWriter(ctx context.Context, c *chatClient) {
	pingTicker := time.NewTicker(chatPingInterval)
	heartbeatTicker := time.NewTicker(chatHeartbeatInterval)
	defer pingTicker.Stop()
	defer heartbeatTicker.Stop()
	defer c.conn.Close()

	for {
		select {
		case <-ctx.Done():
			_ = writeChatMessage(c.conn, websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			return

		case <-pingTicker.C:
			if err := writeChatMessage(c.conn, websocket.PingMessage, nil); err != nil {
				return
			}

		case now := <-heartbeatTicker.C:
			payload, _ := json.Marshal(chatHeartbeat{
				Type:       "heartbeat",
				ServerTime: now.UnixMilli(),
			})
			if err := writeChatMessage(c.conn, websocket.TextMessage, payload); err != nil {
				return
			}

		case msg, ok := <-c.send:
			if !ok {
				_ = writeChatMessage(c.conn, websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
				return
			}
			if err := writeChatMessage(c.conn, websocket.TextMessage, msg); err != nil {
				return
			}
		}
	}
}

func writeChatMessage(conn *websocket.Conn, messageType int, payload []byte) error {
	if err := conn.SetWriteDeadline(time.Now().Add(chatWriteTimeout)); err != nil {
		return err
	}
	return conn.WriteMessage(messageType, payload)
}
