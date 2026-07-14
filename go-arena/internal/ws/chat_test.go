package ws

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"arena-server/internal/config"
	"arena-server/internal/db"
	"arena-server/internal/game"

	"github.com/gorilla/websocket"
)

// fakeChatStore is an in-memory ChatStore. Chat tests must not require a
// live database.
type fakeChatStore struct {
	mu          sync.Mutex
	messages    []db.ChatMessage
	nextID      int64
	banUntil    map[string]*time.Time
	linkedIDs   map[string][]string
	insertErr   error
	insertPanic bool
	linkedErr   error
	banErr      error
}

func newFakeChatStore() *fakeChatStore {
	return &fakeChatStore{
		banUntil:  make(map[string]*time.Time),
		linkedIDs: make(map[string][]string),
	}
}

func (s *fakeChatStore) RecentMessages(ctx context.Context, limit int) ([]db.ChatMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.messages) > limit {
		return append([]db.ChatMessage{}, s.messages[len(s.messages)-limit:]...), nil
	}
	return append([]db.ChatMessage{}, s.messages...), nil
}

func (s *fakeChatStore) Insert(ctx context.Context, m *db.ChatMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.insertPanic {
		panic("fake store insert panic")
	}
	if s.insertErr != nil {
		return s.insertErr
	}
	s.nextID++
	m.ID = s.nextID
	s.messages = append(s.messages, *m)
	return nil
}

func (s *fakeChatStore) ChatBanUntil(ctx context.Context, accountID string) (*time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.banErr != nil {
		return nil, s.banErr
	}
	return s.banUntil[accountID], nil
}

func (s *fakeChatStore) LinkedBotIDs(ctx context.Context, accountID string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.linkedErr != nil {
		return nil, s.linkedErr
	}
	return s.linkedIDs[accountID], nil
}

// withChatConfig snapshots config.C, applies chat test settings, and
// restores on cleanup. Chat tests mutate the global config so they must not
// run in parallel (package convention).
func withChatConfig(t *testing.T) {
	t.Helper()
	saved := config.C
	t.Cleanup(func() { config.C = saved })
	config.C.ChatEnabled = true
	config.C.ChatMaxClients = 16
	config.C.ChatHistorySize = 10
	config.C.ChatMaxBodyLen = 60
	config.C.ChatPostsPerMin = 5
	config.C.ChatAliveLock = true
	config.C.WSConnectRatePerMin = 1000
}

type chatTestEnv struct {
	hub    *ChatHub
	store  *fakeChatStore
	server *httptest.Server

	aliveMu  sync.Mutex
	aliveIDs map[string]bool
	active   bool
}

func (e *chatTestEnv) setAlive(botID string, alive bool) {
	e.aliveMu.Lock()
	defer e.aliveMu.Unlock()
	e.aliveIDs[botID] = alive
}

func (e *chatTestEnv) setRoundActive(active bool) {
	e.aliveMu.Lock()
	defer e.aliveMu.Unlock()
	e.active = active
}

// newChatTestEnv wires a hub with fake store/engine hooks behind a real
// ChatHandler. Identity is injected via the X-Test-Account / X-Test-Name
// request headers so tests exercise the same resolver seam the router uses.
func newChatTestEnv(t *testing.T) *chatTestEnv {
	t.Helper()
	env := &chatTestEnv{
		store:    newFakeChatStore(),
		aliveIDs: make(map[string]bool),
	}
	env.hub = newChatHub(env.store,
		func(botID string) bool {
			env.aliveMu.Lock()
			defer env.aliveMu.Unlock()
			return env.aliveIDs[botID]
		},
		func() bool {
			env.aliveMu.Lock()
			defer env.aliveMu.Unlock()
			return env.active
		},
	)

	resolver := func(r *http.Request) *ChatIdentity {
		account := r.Header.Get("X-Test-Account")
		if account == "" {
			return nil
		}
		return &ChatIdentity{AccountID: account, Name: r.Header.Get("X-Test-Name")}
	}

	env.server = httptest.NewServer(ChatHandler(game.NewGameEngine(), env.hub, resolver))
	t.Cleanup(env.server.Close)
	return env
}

func (e *chatTestEnv) dial(t *testing.T, headers http.Header) *websocket.Conn {
	t.Helper()
	url := "ws" + strings.TrimPrefix(e.server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(url, headers)
	if err != nil {
		t.Fatalf("chat dial failed: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func identityHeaders(account, name string) http.Header {
	h := http.Header{}
	h.Set("X-Test-Account", account)
	h.Set("X-Test-Name", name)
	return h
}

// readChatMessage reads frames until one of the wanted type arrives,
// skipping heartbeats and unrelated frames.
func readChatMessage(t *testing.T, conn *websocket.Conn, wantType string) map[string]interface{} {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("waiting for %q: read failed: %v", wantType, err)
		}
		var msg map[string]interface{}
		if err := json.Unmarshal(data, &msg); err != nil {
			t.Fatalf("waiting for %q: invalid frame %q: %v", wantType, data, err)
		}
		if msg["type"] == wantType {
			return msg
		}
	}
}

func postChat(t *testing.T, conn *websocket.Conn, body string) {
	t.Helper()
	payload, _ := json.Marshal(ChatPostMessage{Type: "chat_post", Body: body})
	if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
		t.Fatalf("chat post write failed: %v", err)
	}
}

func TestChatHandlerDisabled(t *testing.T) {
	withChatConfig(t)
	config.C.ChatEnabled = false
	env := newChatTestEnv(t)

	resp, err := http.Get(env.server.URL)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("disabled chat: got status %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

func TestChatAnonymousIsReadOnly(t *testing.T) {
	withChatConfig(t)
	env := newChatTestEnv(t)

	conn := env.dial(t, nil)

	status := readChatMessage(t, conn, "chat_status")
	if status["can_post"] != false {
		t.Fatalf("anonymous chat_status: can_post = %v, want false", status["can_post"])
	}
	if status["reason"] != "sign_in_required" {
		t.Fatalf("anonymous chat_status: reason = %v, want sign_in_required", status["reason"])
	}
	readChatMessage(t, conn, "chat_history")

	postChat(t, conn, "hello")
	errMsg := readChatMessage(t, conn, "chat_error")
	if errMsg["code"] != "AUTH_REQUIRED" {
		t.Fatalf("anonymous post: code = %v, want AUTH_REQUIRED", errMsg["code"])
	}
}

func TestChatPostBroadcastsAndBackfills(t *testing.T) {
	withChatConfig(t)
	env := newChatTestEnv(t)

	poster := env.dial(t, identityHeaders("acct-1234abcd", "Lucas"))
	status := readChatMessage(t, poster, "chat_status")
	if status["can_post"] != true {
		t.Fatalf("signed-in chat_status: can_post = %v, want true", status["can_post"])
	}
	handle, _ := status["handle"].(string)
	if !strings.HasPrefix(handle, "Lucas#") || len(handle) != len("Lucas#")+8 {
		t.Fatalf("handle = %q, want Lucas# + 8-hex discriminator", handle)
	}
	readChatMessage(t, poster, "chat_history")

	watcher := env.dial(t, nil)
	readChatMessage(t, watcher, "chat_status")
	readChatMessage(t, watcher, "chat_history")

	postChat(t, poster, "gg everyone")

	for _, conn := range []*websocket.Conn{poster, watcher} {
		broadcast := readChatMessage(t, conn, "chat_message")
		msg := broadcast["message"].(map[string]interface{})
		if msg["body"] != "gg everyone" {
			t.Fatalf("broadcast body = %v, want %q", msg["body"], "gg everyone")
		}
		if msg["handle"] != handle {
			t.Fatalf("broadcast handle = %v, want %q", msg["handle"], handle)
		}
	}

	// A client connecting after the post receives it in history.
	late := env.dial(t, nil)
	readChatMessage(t, late, "chat_status")
	history := readChatMessage(t, late, "chat_history")
	msgs := history["messages"].([]interface{})
	if len(msgs) != 1 {
		t.Fatalf("late-join history has %d messages, want 1", len(msgs))
	}
	if msgs[0].(map[string]interface{})["body"] != "gg everyone" {
		t.Fatalf("late-join history body = %v, want %q", msgs[0].(map[string]interface{})["body"], "gg everyone")
	}
}

func TestChatBodyValidation(t *testing.T) {
	withChatConfig(t)
	env := newChatTestEnv(t)

	conn := env.dial(t, identityHeaders("acct-1", "Dev"))
	readChatMessage(t, conn, "chat_status")
	readChatMessage(t, conn, "chat_history")

	// Over the rune limit.
	postChat(t, conn, strings.Repeat("x", config.C.ChatMaxBodyLen+1))
	errMsg := readChatMessage(t, conn, "chat_error")
	if errMsg["code"] != "INVALID_BODY" {
		t.Fatalf("oversized body: code = %v, want INVALID_BODY", errMsg["code"])
	}

	// Whitespace-only collapses to empty.
	postChat(t, conn, " \t\n ")
	errMsg = readChatMessage(t, conn, "chat_error")
	if errMsg["code"] != "INVALID_BODY" {
		t.Fatalf("blank body: code = %v, want INVALID_BODY", errMsg["code"])
	}

	// Control characters are stripped, newlines become spaces.
	postChat(t, conn, "one\ntwo\x00three")
	broadcast := readChatMessage(t, conn, "chat_message")
	body := broadcast["message"].(map[string]interface{})["body"]
	if body != "one twothree" {
		t.Fatalf("sanitized body = %q, want %q", body, "one twothree")
	}
}

func TestChatInMemoryRateLimit(t *testing.T) {
	withChatConfig(t)
	config.C.ChatPostsPerMin = 2
	env := newChatTestEnv(t)

	conn := env.dial(t, identityHeaders("acct-1", "Dev"))
	readChatMessage(t, conn, "chat_status")
	readChatMessage(t, conn, "chat_history")

	postChat(t, conn, "one")
	readChatMessage(t, conn, "chat_message")
	postChat(t, conn, "two")
	readChatMessage(t, conn, "chat_message")
	postChat(t, conn, "three")
	errMsg := readChatMessage(t, conn, "chat_error")
	if errMsg["code"] != "RATE_LIMITED" {
		t.Fatalf("third post in window: code = %v, want RATE_LIMITED", errMsg["code"])
	}
}

func TestChatAliveLockBlocksPosting(t *testing.T) {
	withChatConfig(t)
	env := newChatTestEnv(t)
	env.store.linkedIDs["acct-1"] = []string{"bot-a"}
	env.setAlive("bot-a", true)
	env.setRoundActive(true)

	conn := env.dial(t, identityHeaders("acct-1", "Dev"))
	readChatMessage(t, conn, "chat_status")
	readChatMessage(t, conn, "chat_history")

	postChat(t, conn, "focus the red bot")
	errMsg := readChatMessage(t, conn, "chat_error")
	if errMsg["code"] != "BOT_ALIVE_LOCK" {
		t.Fatalf("alive bot in active round: code = %v, want BOT_ALIVE_LOCK", errMsg["code"])
	}

	// Bot dies: posting unlocks even mid-round.
	env.setAlive("bot-a", false)
	postChat(t, conn, "gg, my bot is out")
	readChatMessage(t, conn, "chat_message")

	// Round over: alive flag no longer matters.
	env.setAlive("bot-a", true)
	env.setRoundActive(false)
	postChat(t, conn, "rematch?")
	readChatMessage(t, conn, "chat_message")
}

func TestChatAliveLockRequiresLinkedBot(t *testing.T) {
	withChatConfig(t)
	env := newChatTestEnv(t)
	env.setRoundActive(true) // acct-1 has NO linked bots

	conn := env.dial(t, identityHeaders("acct-1", "Dev"))
	readChatMessage(t, conn, "chat_status")
	readChatMessage(t, conn, "chat_history")

	// An unlinked account must not be able to dodge the alive-lock by never
	// linking; during a live round it cannot post at all.
	postChat(t, conn, "sneaky coordination")
	errMsg := readChatMessage(t, conn, "chat_error")
	if errMsg["code"] != "LINK_REQUIRED" {
		t.Fatalf("unlinked account mid-round: code = %v, want LINK_REQUIRED", errMsg["code"])
	}

	// Between rounds, an unlinked account can still chat.
	env.setRoundActive(false)
	postChat(t, conn, "gg all")
	readChatMessage(t, conn, "chat_message")
}

func TestChatAliveLockFailsClosedOnDBError(t *testing.T) {
	withChatConfig(t)
	env := newChatTestEnv(t)
	env.setRoundActive(true)
	env.store.linkedErr = errors.New("connection reset")

	conn := env.dial(t, identityHeaders("acct-1", "Dev"))
	readChatMessage(t, conn, "chat_status")
	readChatMessage(t, conn, "chat_history")

	// A lookup failure must not let a possibly-live bot's owner post; fail
	// closed rather than open.
	postChat(t, conn, "post during outage")
	errMsg := readChatMessage(t, conn, "chat_error")
	if errMsg["code"] != "POST_FAILED" {
		t.Fatalf("alive-lock lookup error: code = %v, want POST_FAILED", errMsg["code"])
	}
}

func TestChatBanCheckFailsClosedOnDBError(t *testing.T) {
	withChatConfig(t)
	env := newChatTestEnv(t)
	env.store.banErr = errors.New("connection reset")

	conn := env.dial(t, identityHeaders("acct-1", "Dev"))
	readChatMessage(t, conn, "chat_status")
	readChatMessage(t, conn, "chat_history")

	// A ban-lookup failure must not let a possibly-banned poster through;
	// fail closed rather than open (mirrors the alive-lock's own DB-error
	// handling immediately below it in ChatHub.post).
	postChat(t, conn, "post during outage")
	errMsg := readChatMessage(t, conn, "chat_error")
	if errMsg["code"] != "POST_FAILED" {
		t.Fatalf("ban lookup error: code = %v, want POST_FAILED", errMsg["code"])
	}
}

func TestChatRateLimitIsPerAccountAcrossSockets(t *testing.T) {
	withChatConfig(t)
	config.C.ChatPostsPerMin = 2
	env := newChatTestEnv(t)

	// Two sockets, same account: the budget must be shared (the Redis limiter
	// is absent in tests, so this exercises the in-memory per-account window).
	a := env.dial(t, identityHeaders("acct-1", "Dev"))
	readChatMessage(t, a, "chat_status")
	readChatMessage(t, a, "chat_history")
	b := env.dial(t, identityHeaders("acct-1", "Dev"))
	readChatMessage(t, b, "chat_status")
	readChatMessage(t, b, "chat_history")

	postChat(t, a, "one")
	readChatMessage(t, a, "chat_message")
	postChat(t, b, "two")
	readChatMessage(t, b, "chat_message")
	// Third post on either socket exceeds the shared per-account window.
	postChat(t, b, "three")
	errMsg := readChatMessage(t, b, "chat_error")
	if errMsg["code"] != "RATE_LIMITED" {
		t.Fatalf("third post across sockets: code = %v, want RATE_LIMITED", errMsg["code"])
	}
}

func TestChatReservePostWindowAtomic(t *testing.T) {
	withChatConfig(t)
	env := newChatTestEnv(t)

	// The check and the slot append are one critical section: N concurrent
	// reservations for the same account must admit exactly `limit`, never
	// more (the TOCTOU burst that separate check/record allowed).
	const limit = 5
	const attempts = 40
	now := time.Now()
	var wg sync.WaitGroup
	granted := make(chan struct{}, attempts)
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if env.hub.reservePostWindow("acct-race", limit, now) {
				granted <- struct{}{}
			}
		}()
	}
	wg.Wait()
	close(granted)
	if got := len(granted); got != limit {
		t.Fatalf("concurrent reservations granted = %d, want exactly %d", got, limit)
	}
}

func TestChatPanicInPostDoesNotLeakSlot(t *testing.T) {
	withChatConfig(t)
	env := newChatTestEnv(t)
	env.store.insertPanic = true

	conn := env.dial(t, identityHeaders("acct-1", "Dev"))
	readChatMessage(t, conn, "chat_status")
	readChatMessage(t, conn, "chat_history")
	if got := env.hub.ClientCount(); got != 1 {
		t.Fatalf("client count before panic = %d, want 1", got)
	}

	// The post panics inside store.Insert; the recover closes the connection.
	postChat(t, conn, "boom")
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}

	// The slot must be reclaimed even though the handler panicked.
	deadline := time.Now().Add(2 * time.Second)
	for env.hub.ClientCount() != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := env.hub.ClientCount(); got != 0 {
		t.Fatalf("client count after panic = %d, want 0 (slot leaked)", got)
	}
}

func TestChatBanBlocksPosting(t *testing.T) {
	withChatConfig(t)
	env := newChatTestEnv(t)
	until := time.Now().Add(time.Hour)
	env.store.banUntil["acct-1"] = &until

	conn := env.dial(t, identityHeaders("acct-1", "Dev"))
	readChatMessage(t, conn, "chat_status")
	readChatMessage(t, conn, "chat_history")

	postChat(t, conn, "hello")
	errMsg := readChatMessage(t, conn, "chat_error")
	if errMsg["code"] != "CHAT_BANNED" {
		t.Fatalf("banned account: code = %v, want CHAT_BANNED", errMsg["code"])
	}
}

func TestChatHideMessage(t *testing.T) {
	withChatConfig(t)
	env := newChatTestEnv(t)

	conn := env.dial(t, identityHeaders("acct-1", "Dev"))
	readChatMessage(t, conn, "chat_status")
	readChatMessage(t, conn, "chat_history")

	postChat(t, conn, "delete me")
	broadcast := readChatMessage(t, conn, "chat_message")
	id := int64(broadcast["message"].(map[string]interface{})["id"].(float64))

	env.hub.HideMessage(id)

	hidden := readChatMessage(t, conn, "chat_message_hidden")
	if int64(hidden["id"].(float64)) != id {
		t.Fatalf("chat_message_hidden id = %v, want %d", hidden["id"], id)
	}

	late := env.dial(t, nil)
	readChatMessage(t, late, "chat_status")
	history := readChatMessage(t, late, "chat_history")
	if msgs := history["messages"].([]interface{}); len(msgs) != 0 {
		t.Fatalf("history after hide has %d messages, want 0", len(msgs))
	}
}

func TestChatCapacityLimit(t *testing.T) {
	withChatConfig(t)
	config.C.ChatMaxClients = 1
	env := newChatTestEnv(t)

	first := env.dial(t, nil)
	readChatMessage(t, first, "chat_status")

	second := env.dial(t, nil)
	second.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, _, err := second.ReadMessage()
	closeErr, ok := err.(*websocket.CloseError)
	if !ok || closeErr.Code != websocket.CloseTryAgainLater {
		t.Fatalf("over-capacity connect: err = %v, want close %d", err, websocket.CloseTryAgainLater)
	}
}

func TestChatCrossOriginSessionIgnored(t *testing.T) {
	withChatConfig(t)
	env := newChatTestEnv(t)

	// A cross-site page can open the socket (public read), but the session
	// cookie must not grant it a posting identity.
	headers := identityHeaders("acct-1", "Dev")
	headers.Set("Origin", "https://evil.example")
	conn := env.dial(t, headers)

	status := readChatMessage(t, conn, "chat_status")
	if status["can_post"] != false {
		t.Fatalf("cross-origin connect: can_post = %v, want false", status["can_post"])
	}
}

func TestChatRingBounded(t *testing.T) {
	withChatConfig(t)
	config.C.ChatHistorySize = 3
	config.C.ChatPostsPerMin = 100
	env := newChatTestEnv(t)

	conn := env.dial(t, identityHeaders("acct-1", "Dev"))
	readChatMessage(t, conn, "chat_status")
	readChatMessage(t, conn, "chat_history")

	for _, body := range []string{"m1", "m2", "m3", "m4", "m5"} {
		postChat(t, conn, body)
		readChatMessage(t, conn, "chat_message")
	}

	late := env.dial(t, nil)
	readChatMessage(t, late, "chat_status")
	history := readChatMessage(t, late, "chat_history")
	msgs := history["messages"].([]interface{})
	if len(msgs) != 3 {
		t.Fatalf("bounded ring history has %d messages, want 3", len(msgs))
	}
	if msgs[0].(map[string]interface{})["body"] != "m3" {
		t.Fatalf("oldest retained = %v, want m3", msgs[0].(map[string]interface{})["body"])
	}
}

func TestSanitizeChatBody(t *testing.T) {
	cases := []struct {
		name string
		in   string
		max  int
		out  string
		ok   bool
	}{
		{"plain", "hello world", 60, "hello world", true},
		{"trimmed", "  hi  ", 60, "hi", true},
		{"newlines to spaces", "a\r\nb", 60, "a  b", true},
		{"controls stripped", "a\x01\x02b", 60, "ab", true},
		{"empty", "", 60, "", false},
		{"whitespace only", " \t ", 60, "", false},
		{"over limit", "abcdef", 5, "", false},
		{"at limit", "abcde", 5, "abcde", true},
		{"unicode counted in runes", "héllo wörld", 11, "héllo wörld", true},
		{"invalid utf8", string([]byte{0xff, 0xfe}), 60, "", false},
		// Format (Cf) characters are constructed from code points so the
		// source file stays free of invisible characters.
		{"zero-width space stripped", "a" + string(rune(0x200B)) + "b", 60, "ab", true},
		{"bidi override stripped", "a" + string(rune(0x202E)) + "b", 60, "ab", true},
		{"zero-width only is empty", string(rune(0x200B)) + string(rune(0xFEFF)), 60, "", false},
		{"soft hyphen stripped", "co" + string(rune(0x00AD)) + "op", 60, "coop", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, ok := sanitizeChatBody(tc.in, tc.max)
			if ok != tc.ok || out != tc.out {
				t.Fatalf("sanitizeChatBody(%q, %d) = (%q, %v), want (%q, %v)", tc.in, tc.max, out, ok, tc.out, tc.ok)
			}
		})
	}
}

func TestChatSameOrigin(t *testing.T) {
	mk := func(host, origin string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "http://"+host+"/ws/chat", nil)
		r.Host = host
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		return r
	}
	cases := []struct {
		name string
		r    *http.Request
		want bool
	}{
		{"no origin (non-browser)", mk("arena.example.com", ""), true},
		{"same origin", mk("arena.example.com", "http://arena.example.com"), true},
		{"same origin explicit port", mk("arena.example.com:80", "http://arena.example.com"), true},
		{"cross origin", mk("arena.example.com", "https://evil.example"), false},
		{"subdomain is cross origin", mk("arena.example.com", "http://sub.arena.example.com"), false},
		{"cross scheme same host", mk("arena.example.com", "https://arena.example.com"), false},
		{"garbage origin", mk("arena.example.com", "::not a url::"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := chatSameOrigin(tc.r); got != tc.want {
				t.Fatalf("chatSameOrigin(%q vs host %q) = %v, want %v",
					tc.r.Header.Get("Origin"), tc.r.Host, got, tc.want)
			}
		})
	}
}

func TestChatBlockedKeywordRejected(t *testing.T) {
	withChatConfig(t)
	env := newChatTestEnv(t)
	env.hub.SetBlockedKeywords([]string{"badword"})

	poster := env.dial(t, identityHeaders("acct-kw", "Kappa"))
	readChatMessage(t, poster, "chat_status")
	readChatMessage(t, poster, "chat_history")

	postChat(t, poster, "this has a BadWord in it")
	errMsg := readChatMessage(t, poster, "chat_error")
	if errMsg["code"] != "BLOCKED_KEYWORD" {
		t.Fatalf("blocked-keyword post: code = %v, want BLOCKED_KEYWORD", errMsg["code"])
	}

	postChat(t, poster, "this one is clean")
	msg := readChatMessage(t, poster, "chat_message")
	if msg["message"] == nil {
		t.Fatalf("clean post after keyword rejection: expected a broadcast message")
	}
}

func TestChatRuntimeDisableRejectsPostsAndNotifiesClients(t *testing.T) {
	withChatConfig(t)
	env := newChatTestEnv(t)

	poster := env.dial(t, identityHeaders("acct-toggle", "Toggle"))
	status := readChatMessage(t, poster, "chat_status")
	if status["can_post"] != true {
		t.Fatalf("initial chat_status: can_post = %v, want true", status["can_post"])
	}
	readChatMessage(t, poster, "chat_history")

	env.hub.SetEnabled(false)
	settings := readChatMessage(t, poster, "chat_settings")
	if settings["enabled"] != false {
		t.Fatalf("chat_settings broadcast: enabled = %v, want false", settings["enabled"])
	}

	postChat(t, poster, "still trying to post")
	errMsg := readChatMessage(t, poster, "chat_error")
	if errMsg["code"] != "CHAT_DISABLED" {
		t.Fatalf("post while runtime-disabled: code = %v, want CHAT_DISABLED", errMsg["code"])
	}

	env.hub.SetEnabled(true)
	settings = readChatMessage(t, poster, "chat_settings")
	if settings["enabled"] != true {
		t.Fatalf("chat_settings broadcast: enabled = %v, want true", settings["enabled"])
	}
	postChat(t, poster, "now it should work")
	msg := readChatMessage(t, poster, "chat_message")
	if msg["message"] == nil {
		t.Fatalf("post after re-enable: expected a broadcast message")
	}
}

func TestChatHandlerNewConnectionReflectsRuntimeDisabled(t *testing.T) {
	withChatConfig(t)
	env := newChatTestEnv(t)
	env.hub.SetEnabled(false)

	conn := env.dial(t, identityHeaders("acct-newconn", "Newcomer"))
	status := readChatMessage(t, conn, "chat_status")
	if status["can_post"] != false {
		t.Fatalf("new connection while disabled: can_post = %v, want false", status["can_post"])
	}
	if status["reason"] != "chat_disabled" {
		t.Fatalf("new connection while disabled: reason = %v, want chat_disabled", status["reason"])
	}
}
