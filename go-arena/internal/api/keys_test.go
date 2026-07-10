package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"arena-server/internal/db"
	"arena-server/internal/game"
	"arena-server/internal/security"
)

func TestGenerateKeyRequiresDatabase(t *testing.T) {
	originalPool := db.Pool
	db.Pool = nil
	t.Cleanup(func() { db.Pool = originalPool })

	rec := httptest.NewRecorder()
	GenerateKey(rec, httptest.NewRequest(http.MethodPost, "/api/v1/keys/generate", nil))

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
