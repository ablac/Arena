package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"arena-server/internal/config"
	"arena-server/internal/db"
	"arena-server/internal/game"
	"arena-server/internal/security"
)

func testConfigBot() *db.Bot {
	return &db.Bot{
		ID:              "bot-1",
		APIKeyID:        "key-1",
		Name:            "Original",
		AvatarColor:     "#112233",
		DefaultWeapon:   "sword",
		DefaultStats:    db.JSONBStats{"hp": 5, "speed": 5, "attack": 5, "defense": 5},
		DefaultFallback: "aggressive",
		CreatedAt:       time.Unix(100, 0),
		UpdatedAt:       time.Unix(200, 0),
	}
}

func cloneConfigBot(bot *db.Bot) db.Bot {
	clone := *bot
	clone.DefaultStats = make(db.JSONBStats, len(bot.DefaultStats))
	for name, value := range bot.DefaultStats {
		clone.DefaultStats[name] = value
	}
	return clone
}

func requestBotConfig(t *testing.T, bot *db.Bot, body string) *httptest.ResponseRecorder {
	t.Helper()

	originalPool := db.Pool
	originalBudget, originalMin, originalMax := config.C.StatBudget, config.C.StatMin, config.C.StatMax
	db.Pool = nil
	config.C.StatBudget, config.C.StatMin, config.C.StatMax = 20, 1, 10
	t.Cleanup(func() {
		db.Pool = originalPool
		config.C.StatBudget, config.C.StatMin, config.C.StatMax = originalBudget, originalMin, originalMax
	})

	req := httptest.NewRequest(http.MethodPut, "/api/v1/bot/config", strings.NewReader(body))
	req = req.WithContext(security.WithBotContext(req.Context(), bot))
	rec := httptest.NewRecorder()
	UpdateBotConfig(rec, req)
	return rec
}

func TestUpdateBotConfigRejectsInvalidRequestAtomically(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "invalid color after name",
			body: `{"name":"Changed","avatar_color":"red"}`,
		},
		{
			name: "over-budget stats after weapon",
			body: `{"name":"Changed","default_loadout":{"weapon":"staff","stats":{"hp":10,"speed":10,"attack":10,"defense":10},"fallback_behavior":"aggressive"}}`,
		},
		{
			name: "invalid fallback after valid stats",
			body: `{"name":"Changed","default_loadout":{"weapon":"staff","stats":{"hp":5,"speed":5,"attack":5,"defense":5},"fallback_behavior":"never_fight"}}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bot := testConfigBot()
			before := cloneConfigBot(bot)

			rec := requestBotConfig(t, bot, tc.body)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
			}
			if !reflect.DeepEqual(*bot, before) {
				t.Fatalf("rejected request partially mutated bot:\n got: %#v\nwant: %#v", *bot, before)
			}
		})
	}
}

func TestUpdateBotConfigRejectsAmbiguousJSON(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "unknown field",
			body: `{"name":"Changed","stat_points":999}`,
		},
		{
			name: "trailing JSON value",
			body: `{"name":"Changed"}{"name":"Smuggled"}`,
		},
		{
			name: "duplicate top-level field",
			body: `{"name":"First","name":"Second"}`,
		},
		{
			name: "duplicate nested stat",
			body: `{"default_loadout":{"weapon":"staff","stats":{"hp":9,"hp":5,"speed":5,"attack":5,"defense":5},"fallback_behavior":"aggressive"}}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bot := testConfigBot()
			before := cloneConfigBot(bot)

			rec := requestBotConfig(t, bot, tc.body)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
			}
			if !reflect.DeepEqual(*bot, before) {
				t.Fatalf("ambiguous request mutated bot:\n got: %#v\nwant: %#v", *bot, before)
			}
		})
	}
}

func TestUpdateBotConfigAcceptsDocumentedPayload(t *testing.T) {
	bot := testConfigBot()
	rec := requestBotConfig(t, bot, `{
		"name":"Arena Hero",
		"avatar_color":"#aabbcc",
		"default_loadout":{
			"weapon":"staff",
			"stats":{"hp":7,"speed":5,"attack":5,"defense":3},
			"fallback_behavior":"defensive"
		}
	}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if bot.Name != "Arena Hero" || bot.AvatarColor != "#aabbcc" || bot.DefaultWeapon != "staff" || bot.DefaultFallback != "defensive" {
		t.Fatalf("documented fields were not applied: %#v", *bot)
	}
	if !reflect.DeepEqual(map[string]int(bot.DefaultStats), map[string]int{"hp": 7, "speed": 5, "attack": 5, "defense": 3}) {
		t.Fatalf("stats = %#v, want documented 20-point allocation", bot.DefaultStats)
	}
}

func TestUpdateBotConfigCannotBypassExactStatContract(t *testing.T) {
	tests := []struct {
		name  string
		stats string
	}{
		{name: "over budget", stats: `{"hp":6,"speed":5,"attack":5,"defense":5}`},
		{name: "extra stat with total twenty", stats: `{"hp":4,"speed":5,"attack":5,"defense":5,"luck":1}`},
		{name: "substituted stat with total twenty", stats: `{"hp":5,"speed":5,"attack":5,"agility":5}`},
		{name: "below per-stat minimum", stats: `{"hp":0,"speed":10,"attack":9,"defense":1}`},
		{name: "above per-stat maximum", stats: `{"hp":11,"speed":3,"attack":3,"defense":3}`},
		{name: "non-integer stat", stats: `{"hp":"5","speed":5,"attack":5,"defense":5}`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bot := testConfigBot()
			before := cloneConfigBot(bot)
			body := `{"default_loadout":{"weapon":"staff","stats":` + tc.stats + `,"fallback_behavior":"aggressive"}}`

			rec := requestBotConfig(t, bot, body)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
			}
			if !reflect.DeepEqual(*bot, before) {
				t.Fatalf("invalid stat allocation mutated bot:\n got: %#v\nwant: %#v", *bot, before)
			}
		})
	}
}

func TestGetBotLiveCannotSelectAnotherBot(t *testing.T) {
	engine := game.NewGameEngine()
	engine.Bots["bot-1"] = &game.BotState{
		BotID:    "bot-1",
		Name:     "Own Bot",
		HP:       42,
		MaxHP:    100,
		Position: game.NewVec2(3, 4),
		IsAlive:  true,
	}
	engine.Bots["bot-2"] = &game.BotState{
		BotID:    "bot-2",
		Name:     "Other Bot",
		HP:       99,
		MaxHP:    100,
		Position: game.NewVec2(80, 90),
		IsAlive:  true,
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/bot/live?bot_id=bot-2", nil)
	req = req.WithContext(security.WithBotContext(req.Context(), &db.Bot{ID: "bot-1", Name: "Own Bot"}))
	rec := httptest.NewRecorder()
	GetBotLive(engine).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["bot_id"] != "bot-1" || body["hp"] != float64(42) {
		t.Fatalf("live endpoint returned another bot: %#v", body)
	}
	if got := body["position"]; !reflect.DeepEqual(got, []interface{}{float64(3), float64(4)}) {
		t.Fatalf("position = %#v, want own bot position [3,4]", got)
	}
}
