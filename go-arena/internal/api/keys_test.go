package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"arena-server/internal/config"
	"arena-server/internal/db"
	"arena-server/internal/game"
	"arena-server/internal/security"

	"github.com/go-chi/chi/v5"
)

type fakeAccountAPIKeyStore struct {
	keys            []db.AccountAPIKey
	activeCount     int
	createErr       error
	deactivateErr   error
	capacityErr     error
	quotaErr        error
	capacityActive  int
	capacityTotal   int
	enforceQuota    bool
	quotaCounts     map[string]int
	lastAccountID   string
	lastKeyID       string
	lastHash        string
	lastPrefix      string
	lastBot         *db.Bot
	deactivatedKey  *db.AccountAPIKey
	deactivateCalls int
}

type fakePublicAPIKeyStore struct {
	allowed     bool
	quotaErr    error
	createErr   error
	quotaCalls  int
	createCalls int
	lastIP      string
	lastKeyID   string
	lastHash    string
	lastPrefix  string
	lastBot     *db.Bot
}

func (f *fakePublicAPIKeyStore) CheckRegistrationQuota(_ context.Context, ip string, _ int) (bool, int, error) {
	f.quotaCalls++
	f.lastIP = ip
	return f.allowed, 0, f.quotaErr
}

func (f *fakePublicAPIKeyStore) Create(_ context.Context, keyID, keyHash, keyPrefix, ip string, bot *db.Bot) error {
	f.createCalls++
	f.lastKeyID, f.lastHash, f.lastPrefix, f.lastIP, f.lastBot = keyID, keyHash, keyPrefix, ip, bot
	return f.createErr
}

func (f *fakeAccountAPIKeyStore) Capacity(_ context.Context, accountID string) (int, int, error) {
	f.lastAccountID = accountID
	return f.capacityActive, f.capacityTotal, f.capacityErr
}

func (f *fakeAccountAPIKeyStore) ConsumeQuota(_ context.Context, accountID string, action db.AccountAPIKeyQuotaAction, limit int) (bool, int, error) {
	f.lastAccountID = accountID
	if f.quotaErr != nil {
		return false, 0, f.quotaErr
	}
	if !f.enforceQuota {
		return true, limit, nil
	}
	if f.quotaCounts == nil {
		f.quotaCounts = make(map[string]int)
	}
	key := accountID + ":" + string(action)
	if f.quotaCounts[key] >= limit {
		return false, 0, nil
	}
	f.quotaCounts[key]++
	return true, limit - f.quotaCounts[key], nil
}

func (f *fakeAccountAPIKeyStore) List(_ context.Context, accountID string) ([]db.AccountAPIKey, int, error) {
	f.lastAccountID = accountID
	return f.keys, f.activeCount, nil
}

func (f *fakeAccountAPIKeyStore) Create(_ context.Context, accountID, keyID, keyHash, keyPrefix, _ string, bot *db.Bot) (*db.AccountAPIKey, int, error) {
	f.lastAccountID, f.lastKeyID, f.lastHash, f.lastPrefix, f.lastBot = accountID, keyID, keyHash, keyPrefix, bot
	if f.createErr != nil {
		return nil, f.activeCount, f.createErr
	}
	return &db.AccountAPIKey{
		ID: keyID, KeyPrefix: keyPrefix, BotID: bot.ID, BotName: bot.Name, CreatedAt: bot.CreatedAt, IsActive: true,
	}, f.activeCount, nil
}

func (f *fakeAccountAPIKeyStore) Deactivate(_ context.Context, accountID, keyID string) (*db.AccountAPIKey, int, error) {
	f.lastAccountID, f.lastKeyID = accountID, keyID
	f.deactivateCalls++
	if f.deactivateErr != nil {
		return nil, f.activeCount, f.deactivateErr
	}
	return f.deactivatedKey, f.activeCount, nil
}

func verifiedAccountKeyRequest(method, target string, body []byte) *http.Request {
	verifiedAt := time.Now()
	req := httptest.NewRequest(method, target, bytes.NewReader(body))
	return req.WithContext(withCustomerSession(req.Context(), &CustomerSession{
		AccountID:       "account-1",
		Email:           "owner@example.com",
		EmailVerifiedAt: &verifiedAt,
	}))
}

func TestAccountKeysListResponseNeverContainsSecretMaterial(t *testing.T) {
	store := &fakeAccountAPIKeyStore{
		keys: []db.AccountAPIKey{{
			ID: "key-1", KeyPrefix: "arena_abc123", BotID: "bot-1", BotName: "First Bot", IsActive: true,
		}},
		activeCount: 1,
	}
	handler := newAccountKeysHandler(store, nil, security.GenerateAPIKey)
	rec := httptest.NewRecorder()

	handler.List(rec, verifiedAccountKeyRequest(http.MethodGet, "/api/v1/account/keys", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if store.lastAccountID != "account-1" {
		t.Fatalf("listed account = %q", store.lastAccountID)
	}
	if body := rec.Body.String(); strings.Contains(body, "key_hash") || strings.Contains(body, "api_key\"") {
		t.Fatalf("list response contains secret material: %s", body)
	}
	var payload struct {
		Keys        []db.AccountAPIKey `json:"keys"`
		ActiveCount int                `json:"active_count"`
		Limit       int                `json:"limit"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Keys) != 1 || payload.ActiveCount != 1 || payload.Limit != db.MaxActiveAccountAPIKeys {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestAccountKeysCreateReturnsPlaintextOnceAndOwnedMetadata(t *testing.T) {
	store := &fakeAccountAPIKeyStore{activeCount: 1}
	generator := func() (string, string, string, error) {
		return "arena_plaintext_once", "bcrypt-only", "arena_plaint", nil
	}
	handler := newAccountKeysHandler(store, nil, generator)
	rec := httptest.NewRecorder()

	handler.Create(rec, verifiedAccountKeyRequest(http.MethodPost, "/api/v1/account/keys", []byte(`{"bot_name":"<b>Key Bot</b>"}`)))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if store.lastAccountID != "account-1" || store.lastHash != "bcrypt-only" || store.lastPrefix != "arena_plaint" {
		t.Fatalf("create inputs: account=%q hash=%q prefix=%q", store.lastAccountID, store.lastHash, store.lastPrefix)
	}
	if store.lastBot == nil || store.lastBot.Name != "Key Bot" || store.lastBot.APIKeyID != store.lastKeyID {
		t.Fatalf("created bot = %+v, key id = %q", store.lastBot, store.lastKeyID)
	}
	var payload struct {
		APIKey      string           `json:"api_key"`
		Key         db.AccountAPIKey `json:"key"`
		ActiveCount int              `json:"active_count"`
		Limit       int              `json:"limit"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.APIKey != "arena_plaintext_once" || payload.Key.BotName != "Key Bot" || payload.ActiveCount != 1 || payload.Limit != db.MaxActiveAccountAPIKeys {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestAccountKeysCreateMapsFiveKeyLimitToConflict(t *testing.T) {
	store := &fakeAccountAPIKeyStore{activeCount: db.MaxActiveAccountAPIKeys, createErr: db.ErrCustomerAPIKeyLimit}
	handler := newAccountKeysHandler(store, nil, security.GenerateAPIKey)
	rec := httptest.NewRecorder()

	handler.Create(rec, verifiedAccountKeyRequest(http.MethodPost, "/api/v1/account/keys", nil))

	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "API_KEY_LIMIT") {
		t.Fatalf("limit response = %d %s", rec.Code, rec.Body.String())
	}
}

func TestAccountKeysCreateMapsPlatformAgentLimitToConflict(t *testing.T) {
	store := &fakeAccountAPIKeyStore{createErr: &db.PlatformAgentLimitError{
		CurrentAgents: 10,
		MaximumAgents: 10,
	}}
	handler := newAccountKeysHandler(store, nil, security.GenerateAPIKey)
	rec := httptest.NewRecorder()

	handler.Create(rec, verifiedAccountKeyRequest(http.MethodPost, "/api/v1/account/keys", nil))

	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "AGENT_LIMIT") ||
		!strings.Contains(rec.Body.String(), `"current_agents":10`) ||
		!strings.Contains(rec.Body.String(), `"maximum_agents":10`) {
		t.Fatalf("agent-limit response = %d %s", rec.Code, rec.Body.String())
	}
}

func TestAccountKeysCreatePreflightsCapacityBeforeGeneratingSecret(t *testing.T) {
	const historyLimit = 100
	for _, tc := range []struct {
		name        string
		activeCount int
		totalCount  int
		wantCode    string
	}{
		{name: "active cap", activeCount: db.MaxActiveAccountAPIKeys, totalCount: db.MaxActiveAccountAPIKeys, wantCode: "API_KEY_LIMIT"},
		{name: "lifetime history cap", totalCount: historyLimit, wantCode: "API_KEY_HISTORY_LIMIT"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeAccountAPIKeyStore{capacityActive: tc.activeCount, capacityTotal: tc.totalCount}
			generated := 0
			handler := newAccountKeysHandler(store, nil, func() (string, string, string, error) {
				generated++
				return "arena_should_not_exist", "unused", "arena_unused", nil
			})
			recorder := httptest.NewRecorder()

			handler.Create(recorder, verifiedAccountKeyRequest(http.MethodPost, "/api/v1/account/keys", nil))

			if recorder.Code != http.StatusConflict || !strings.Contains(recorder.Body.String(), tc.wantCode) {
				t.Fatalf("capacity response = %d %s", recorder.Code, recorder.Body.String())
			}
			if generated != 0 || store.lastBot != nil {
				t.Fatalf("capacity rejection generated=%d bot=%+v", generated, store.lastBot)
			}
		})
	}
}

func TestAccountKeyCreateQuotaIsPerAccountAcrossSourceIPs(t *testing.T) {
	previous := config.C.CustomerAPIKeyCreatePerHour
	config.C.CustomerAPIKeyCreatePerHour = 1
	t.Cleanup(func() { config.C.CustomerAPIKeyCreatePerHour = previous })

	store := &fakeAccountAPIKeyStore{enforceQuota: true}
	generated := 0
	handler := newAccountKeysHandler(store, nil, func() (string, string, string, error) {
		generated++
		return fmt.Sprintf("arena_plaintext_%d", generated), fmt.Sprintf("hash-%d", generated), fmt.Sprintf("prefix-%d", generated), nil
	})

	for index, remote := range []string{"198.51.100.10:1000", "203.0.113.20:2000"} {
		req := verifiedAccountKeyRequest(http.MethodPost, "/api/v1/account/keys", nil)
		req.RemoteAddr = remote
		recorder := httptest.NewRecorder()
		handler.Create(recorder, req)
		if index == 0 && recorder.Code != http.StatusCreated {
			t.Fatalf("first create = %d %s", recorder.Code, recorder.Body.String())
		}
		if index == 1 && (recorder.Code != http.StatusTooManyRequests || !strings.Contains(recorder.Body.String(), "ACCOUNT_API_KEY_CREATE_RATE_LIMIT")) {
			t.Fatalf("second create = %d %s", recorder.Code, recorder.Body.String())
		}
	}
	if generated != 1 {
		t.Fatalf("generated secrets = %d, want 1", generated)
	}
}

type recordingSessionRevoker struct {
	botID  string
	reason string
	calls  int
}

func (r *recordingSessionRevoker) KickBot(botID, reason string) bool {
	r.botID, r.reason = botID, reason
	r.calls++
	return true
}

func TestAccountKeysDeleteRevokesOwnedKeyAndRequestsSessionKick(t *testing.T) {
	sessions := &recordingSessionRevoker{}
	store := &fakeAccountAPIKeyStore{
		activeCount:    0,
		deactivatedKey: &db.AccountAPIKey{ID: "key-1", BotID: "bot-1", IsActive: false},
	}
	handler := newAccountKeysHandler(store, sessions, security.GenerateAPIKey)
	req := verifiedAccountKeyRequest(http.MethodDelete, "/api/v1/account/keys/key-1", nil)
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("key_id", "key-1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))
	rec := httptest.NewRecorder()

	handler.Deactivate(rec, req)

	if rec.Code != http.StatusOK || store.lastAccountID != "account-1" || store.lastKeyID != "key-1" {
		t.Fatalf("delete response = %d %s, account=%q key=%q", rec.Code, rec.Body.String(), store.lastAccountID, store.lastKeyID)
	}
	if sessions.calls != 1 || sessions.botID != "bot-1" || sessions.reason != "API key revoked" {
		t.Fatalf("session revocation = calls %d, bot %q, reason %q", sessions.calls, sessions.botID, sessions.reason)
	}
	var payload struct {
		Revoked     bool `json:"revoked"`
		ActiveCount int  `json:"active_count"`
		Limit       int  `json:"limit"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil || !payload.Revoked || payload.ActiveCount != 0 || payload.Limit != db.MaxActiveAccountAPIKeys {
		t.Fatalf("payload = %+v, error %v", payload, err)
	}
}

func TestAccountKeyRevokeQuotaStopsCreateRevokeCyclingAcrossSourceIPs(t *testing.T) {
	previous := config.C.CustomerAPIKeyRevokePerHour
	config.C.CustomerAPIKeyRevokePerHour = 2
	t.Cleanup(func() { config.C.CustomerAPIKeyRevokePerHour = previous })

	sessions := &recordingSessionRevoker{}
	store := &fakeAccountAPIKeyStore{
		enforceQuota:   true,
		deactivatedKey: &db.AccountAPIKey{ID: "key", BotID: "bot", IsActive: false},
	}
	handler := newAccountKeysHandler(store, sessions, security.GenerateAPIKey)

	for index := 0; index < 3; index++ {
		keyID := fmt.Sprintf("key-%d", index)
		req := verifiedAccountKeyRequest(http.MethodDelete, "/api/v1/account/keys/"+keyID, nil)
		req.RemoteAddr = fmt.Sprintf("198.51.100.%d:1234", index+1)
		routeCtx := chi.NewRouteContext()
		routeCtx.URLParams.Add("key_id", keyID)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))
		recorder := httptest.NewRecorder()
		handler.Deactivate(recorder, req)
		if index < 2 && recorder.Code != http.StatusOK {
			t.Fatalf("revoke %d = %d %s", index, recorder.Code, recorder.Body.String())
		}
		if index == 2 && (recorder.Code != http.StatusTooManyRequests || !strings.Contains(recorder.Body.String(), "ACCOUNT_API_KEY_REVOKE_RATE_LIMIT")) {
			t.Fatalf("third revoke = %d %s", recorder.Code, recorder.Body.String())
		}
	}
	if store.deactivateCalls != 2 || sessions.calls != 2 {
		t.Fatalf("revoke side effects: database=%d sessions=%d", store.deactivateCalls, sessions.calls)
	}
}

func TestPublicKeyGenerationIsMountedAtEveryPrefix(t *testing.T) {
	originalPool := db.Pool
	db.Pool = nil
	t.Cleanup(func() { db.Pool = originalPool })

	router := NewRouter(game.NewGameEngine())
	for _, path := range []string{"/api/v1/keys/generate", "/arena/api/v1/keys/generate"} {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, nil))

			if rec.Code != http.StatusServiceUnavailable || strings.Contains(rec.Body.String(), "ACCOUNT_REQUIRED") {
				t.Fatalf("status = %d, want mounted public registration with a fail-closed dependency; body: %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestGenerateKeyDoesNotRequireCustomerSession(t *testing.T) {
	originalPool := db.Pool
	db.Pool = nil
	t.Cleanup(func() { db.Pool = originalPool })

	req := httptest.NewRequest(http.MethodPost, "/api/v1/keys/generate", nil)
	rec := httptest.NewRecorder()
	GenerateKey(rec, req)
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "database") {
		t.Fatalf("status = %d, want anonymous request to reach database requirement; body: %s", rec.Code, rec.Body.String())
	}
}

func TestPublicKeyGenerationReturnsServerSecretAndPersistsHashOnly(t *testing.T) {
	store := &fakePublicAPIKeyStore{allowed: true}
	generated := 0
	handler := newPublicKeysHandler(store, func() (string, string, string, error) {
		generated++
		return "arena_plaintext_once", "bcrypt-hash-only", "arena_plaint", nil
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/keys/generate", nil)
	req.RemoteAddr = "198.51.100.42:5000"
	rec := httptest.NewRecorder()

	handler.Create(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if generated != 1 || store.quotaCalls != 1 || store.createCalls != 1 {
		t.Fatalf("calls: generated=%d quota=%d create=%d", generated, store.quotaCalls, store.createCalls)
	}
	if store.lastHash != "bcrypt-hash-only" || store.lastPrefix != "arena_plaint" || store.lastIP != "198.51.100.42" {
		t.Fatalf("stored secret metadata: hash=%q prefix=%q ip=%q", store.lastHash, store.lastPrefix, store.lastIP)
	}
	if store.lastBot == nil || store.lastBot.APIKeyID != store.lastKeyID || store.lastBot.Name != "Unnamed Bot" {
		t.Fatalf("stored bot=%+v key=%q", store.lastBot, store.lastKeyID)
	}
	if strings.Contains(store.lastHash, "plaintext_once") {
		t.Fatalf("plaintext leaked into persisted hash field: %q", store.lastHash)
	}
	var payload KeyGenerateResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.APIKey != "arena_plaintext_once" || payload.BotID != store.lastBot.ID {
		t.Fatalf("payload=%+v", payload)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
}

func TestPublicKeyGenerationRequiresSameOriginForBrowserRequests(t *testing.T) {
	for _, tc := range []struct {
		name   string
		origin string
	}{
		{name: "cross origin", origin: "https://attacker.example"},
		{name: "opaque null origin", origin: "null"},
		{name: "empty origin header", origin: ""},
		{name: "comma separated origins", origin: "https://arena.example, https://attacker.example"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakePublicAPIKeyStore{allowed: true}
			generated := 0
			handler := newPublicKeysHandler(store, func() (string, string, string, error) {
				generated++
				return "arena_must_not_exist", "hash", "arena_must", nil
			})
			req := httptest.NewRequest(http.MethodPost, "https://arena.example/api/v1/keys/generate", nil)
			req.Header.Set("Origin", tc.origin)
			recorder := httptest.NewRecorder()

			handler.Create(recorder, req)

			if recorder.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want %d; body: %s", recorder.Code, http.StatusForbidden, recorder.Body.String())
			}
			if generated != 0 || store.quotaCalls != 0 || store.createCalls != 0 {
				t.Fatalf("cross-origin rejection had side effects: generated=%d quota=%d create=%d", generated, store.quotaCalls, store.createCalls)
			}
		})
	}
}

func TestPublicKeyGenerationAcceptsSameOriginBrowserRequest(t *testing.T) {
	store := &fakePublicAPIKeyStore{allowed: true}
	handler := newPublicKeysHandler(store, func() (string, string, string, error) {
		return "arena_same_origin", "bcrypt-hash", "arena_same", nil
	})
	req := httptest.NewRequest(http.MethodPost, "https://arena.example/api/v1/keys/generate", nil)
	req.Header.Set("Origin", "https://arena.example")
	recorder := httptest.NewRecorder()

	handler.Create(recorder, req)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", recorder.Code, http.StatusCreated, recorder.Body.String())
	}
	if store.quotaCalls != 1 || store.createCalls != 1 {
		t.Fatalf("same-origin request calls: quota=%d create=%d", store.quotaCalls, store.createCalls)
	}
}

func TestPublicKeyGenerationRejectsCallerSelectedCredential(t *testing.T) {
	store := &fakePublicAPIKeyStore{allowed: true}
	generated := 0
	handler := newPublicKeysHandler(store, func() (string, string, string, error) {
		generated++
		return "arena_server_value", "hash", "arena_server", nil
	})
	rec := httptest.NewRecorder()
	handler.Create(rec, httptest.NewRequest(http.MethodPost, "/api/v1/keys/generate",
		strings.NewReader(`{"api_key":"arena_caller_chosen"}`)))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if generated != 0 || store.quotaCalls != 0 || store.createCalls != 0 || strings.Contains(rec.Body.String(), "caller_chosen") {
		t.Fatalf("caller credential reached generation or persistence: generated=%d quota=%d create=%d body=%s",
			generated, store.quotaCalls, store.createCalls, rec.Body.String())
	}
}

func TestPublicKeyGenerationFailsClosedBeforeCreatingSecretWhenQuotaStoreFails(t *testing.T) {
	store := &fakePublicAPIKeyStore{allowed: true, quotaErr: errors.New("quota database offline")}
	generated := 0
	handler := newPublicKeysHandler(store, func() (string, string, string, error) {
		generated++
		return "arena_must_not_exist", "hash", "arena_must", nil
	})
	recorder := httptest.NewRecorder()
	handler.Create(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/keys/generate", strings.NewReader(`{}`)))

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d; body: %s", recorder.Code, http.StatusServiceUnavailable, recorder.Body.String())
	}
	if generated != 0 || store.createCalls != 0 || strings.Contains(recorder.Body.String(), "must_not_exist") {
		t.Fatalf("quota failure leaked or persisted a secret: generated=%d create=%d body=%s",
			generated, store.createCalls, recorder.Body.String())
	}
}

func TestPublicKeyGenerationNeverReturnsSecretBeforeAtomicPersistenceSucceeds(t *testing.T) {
	store := &fakePublicAPIKeyStore{allowed: true, createErr: errors.New("bot insert rejected")}
	handler := newPublicKeysHandler(store, func() (string, string, string, error) {
		return "arena_not_committed", "bcrypt-hash", "arena_not_co", nil
	})
	recorder := httptest.NewRecorder()
	handler.Create(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/keys/generate", nil))

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body: %s", recorder.Code, http.StatusInternalServerError, recorder.Body.String())
	}
	if store.createCalls != 1 || strings.Contains(recorder.Body.String(), "not_committed") {
		t.Fatalf("failed persistence returned the one-time secret: create=%d body=%s", store.createCalls, recorder.Body.String())
	}
}

func TestGenerateKeyRequiresDatabase(t *testing.T) {
	originalPool := db.Pool
	db.Pool = nil
	t.Cleanup(func() { db.Pool = originalPool })

	req := httptest.NewRequest(http.MethodPost, "/api/v1/keys/generate", nil)
	rec := httptest.NewRecorder()
	GenerateKey(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusServiceUnavailable, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, "key registration requires the database") {
		t.Fatalf("body = %q, want database requirement", body)
	}
}

type orderedSessionRevoker struct {
	t         *testing.T
	engine    *game.GameEngine
	keyActive *bool
}

func (r orderedSessionRevoker) KickBot(botID, reason string) bool {
	r.t.Helper()
	if *r.keyActive {
		r.t.Fatal("session removal ran before database deactivation")
	}
	return r.engine.KickBot(botID, reason)
}

func TestRevokeKeyDeactivatesBeforeRemovingActiveOrWaitingSession(t *testing.T) {
	for _, location := range []string{"active", "waiting"} {
		t.Run(location, func(t *testing.T) {
			engine := game.NewGameEngine()
			state := &game.BotState{
				BotID:    "bot-1",
				APIKeyID: "key-1",
				Name:     "Revoke Me",
				SendChan: make(chan []byte, 1),
				IsAlive:  location == "active",
			}
			if location == "active" {
				engine.Bots[state.BotID] = state
			} else {
				engine.WaitingBots[state.BotID] = state
			}

			keyActive := true
			deactivateCalls := 0
			sessions := orderedSessionRevoker{t: t, engine: engine, keyActive: &keyActive}
			handler := revokeKeyHandler(sessions, func(_ context.Context, keyID string) error {
				deactivateCalls++
				if keyID != state.APIKeyID {
					t.Fatalf("deactivated key = %q, want %q", keyID, state.APIKeyID)
				}
				keyActive = false
				return nil
			})

			req := httptest.NewRequest(http.MethodDelete, "/api/v1/keys/revoke", nil)
			req = req.WithContext(security.WithBotContext(req.Context(), &db.Bot{
				ID:       state.BotID,
				APIKeyID: state.APIKeyID,
			}))
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
			}
			if deactivateCalls != 1 {
				t.Fatalf("deactivate calls = %d, want 1", deactivateCalls)
			}
			if keyActive {
				t.Fatal("key remained active after successful revoke")
			}
			if got := engine.ConnectedBotCount(); got != 0 {
				t.Fatalf("connected bot count = %d, want 0", got)
			}

			select {
			case payload := <-state.SendChan:
				var message map[string]string
				if err := json.Unmarshal(payload, &message); err != nil {
					t.Fatalf("decode kick message: %v", err)
				}
				if message["type"] != "kick" || message["reason"] != "API key revoked" {
					t.Fatalf("kick message = %#v", message)
				}
			default:
				t.Fatal("session was removed without receiving a revoke kick")
			}
		})
	}
}

func TestRevokeKeyKeepsSessionWhenDatabaseDeactivationFails(t *testing.T) {
	engine := game.NewGameEngine()
	state := &game.BotState{BotID: "bot-1", APIKeyID: "key-1", SendChan: make(chan []byte, 1)}
	engine.Bots[state.BotID] = state

	wantErr := errors.New("database unavailable")
	handler := revokeKeyHandler(engine, func(context.Context, string) error { return wantErr })
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/keys/revoke", nil)
	req = req.WithContext(security.WithBotContext(req.Context(), &db.Bot{
		ID:       state.BotID,
		APIKeyID: state.APIKeyID,
	}))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
	if got := engine.ConnectedBotCount(); got != 1 {
		t.Fatalf("connected bot count = %d, want session preserved on DB failure", got)
	}
	select {
	case payload := <-state.SendChan:
		t.Fatalf("unexpected kick after failed deactivation: %s", payload)
	default:
	}
}

func TestRevokeKeyRequiresAuthenticatedBot(t *testing.T) {
	called := false
	handler := revokeKeyHandler(nil, func(context.Context, string) error {
		called = true
		return nil
	})
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/keys/revoke", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if called {
		t.Fatal("database deactivation was called without authentication")
	}
}
