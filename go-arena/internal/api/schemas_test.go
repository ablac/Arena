package api

import (
	"encoding/json"
	"testing"
	"time"
)

// --- KeyGenerateResponse ---

func TestKeyGenerateResponseJSON(t *testing.T) {
	r := KeyGenerateResponse{
		APIKey:    "arena_abc123",
		BotID:     "bot-uuid",
		CreatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		Message:   "Key created",
	}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var out KeyGenerateResponse
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out.APIKey != r.APIKey {
		t.Errorf("APIKey mismatch: %v", out.APIKey)
	}
	if out.BotID != r.BotID {
		t.Errorf("BotID mismatch: %v", out.BotID)
	}
}

// --- BotConfigRequest ---

func TestBotConfigRequestJSONOmitempty(t *testing.T) {
	req := BotConfigRequest{} // all nil
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	// omitempty: should be empty object
	if string(data) != "{}" {
		t.Errorf("expected {}, got %s", data)
	}
}

func TestBotConfigRequestWithName(t *testing.T) {
	name := "AlphaBot"
	req := BotConfigRequest{Name: &name}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var out BotConfigRequest
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out.Name == nil || *out.Name != name {
		t.Errorf("Name mismatch: %v", out.Name)
	}
}

func TestBotConfigRequestWithLoadout(t *testing.T) {
	weapon := "sword"
	color := "#ff0000"
	req := BotConfigRequest{
		Name:        &weapon, // reuse string ptr
		AvatarColor: &color,
		DefaultLoadout: &LoadoutRequest{
			Weapon:   "sword",
			Stats:    map[string]int{"hp": 5, "speed": 5, "attack": 5, "defense": 5},
			Fallback: "aggressive",
		},
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var out BotConfigRequest
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out.DefaultLoadout == nil {
		t.Fatal("DefaultLoadout should not be nil")
	}
	if out.DefaultLoadout.Weapon != "sword" {
		t.Errorf("Weapon=%v", out.DefaultLoadout.Weapon)
	}
	if out.DefaultLoadout.Stats["hp"] != 5 {
		t.Errorf("hp stat=%v", out.DefaultLoadout.Stats["hp"])
	}
}

// --- LoadoutRequest ---

func TestLoadoutRequestRoundTrip(t *testing.T) {
	req := LoadoutRequest{
		Weapon:   "bow",
		Stats:    map[string]int{"hp": 4, "speed": 6, "attack": 5, "defense": 5},
		Fallback: "hunter",
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var out LoadoutRequest
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out.Weapon != req.Weapon {
		t.Errorf("weapon=%v", out.Weapon)
	}
	if out.Fallback != req.Fallback {
		t.Errorf("fallback=%v", out.Fallback)
	}
}

// --- BotStatsResponse ---

func TestBotStatsResponseFields(t *testing.T) {
	r := BotStatsResponse{
		BotID:       "bot-1",
		Name:        "Alpha",
		Kills:       10,
		Deaths:      3,
		KDRatio:     3.33,
		DamageDealt: 5000,
		Elo:         1200,
	}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var out BotStatsResponse
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out.KDRatio != r.KDRatio {
		t.Errorf("KDRatio=%v", out.KDRatio)
	}
	if out.Elo != 1200 {
		t.Errorf("Elo=%v", out.Elo)
	}
}

// --- ArenaStatusResponse ---

func TestArenaStatusResponseJSON(t *testing.T) {
	r := ArenaStatusResponse{
		Status:             "fighting",
		BotsConnected:      50,
		BotsAlive:          30,
		RoundNumber:        5,
		RoundTimeRemaining: 120.5,
		SafeZoneRadius:     500.0,
		TopBot:             "AlphaBot",
	}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var out ArenaStatusResponse
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out.Status != "fighting" {
		t.Errorf("Status=%v", out.Status)
	}
	if out.RoundTimeRemaining != 120.5 {
		t.Errorf("RoundTimeRemaining=%v", out.RoundTimeRemaining)
	}
}

// --- LeaderboardResponse ---

func TestLeaderboardResponsePagination(t *testing.T) {
	r := LeaderboardResponse{
		Entries: []LeaderboardEntry{
			{Rank: 1, BotID: "b1", Name: "Alpha", Kills: 100, Elo: 2000},
			{Rank: 2, BotID: "b2", Name: "Beta", Kills: 80, Elo: 1800},
		},
		Total:  2,
		Limit:  10,
		Offset: 0,
	}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var out LeaderboardResponse
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Entries) != 2 {
		t.Errorf("entries len=%d", len(out.Entries))
	}
	if out.Total != 2 {
		t.Errorf("total=%d", out.Total)
	}
}

func TestLeaderboardResponseEmptyEntries(t *testing.T) {
	r := LeaderboardResponse{
		Entries: []LeaderboardEntry{},
		Total:   0,
		Limit:   10,
		Offset:  0,
	}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var out LeaderboardResponse
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out.Total != 0 {
		t.Errorf("total=%d", out.Total)
	}
}

// --- ErrorResponse ---

func TestErrorResponseJSON(t *testing.T) {
	r := ErrorResponse{Error: "unauthorized"}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var out ErrorResponse
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out.Error != "unauthorized" {
		t.Errorf("Error=%v", out.Error)
	}
}

// --- HealthResponse ---

func TestHealthResponseJSON(t *testing.T) {
	r := HealthResponse{Status: "ok", BotsOnline: 42}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var out HealthResponse
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out.Status != "ok" {
		t.Errorf("Status=%v", out.Status)
	}
	if out.BotsOnline != 42 {
		t.Errorf("BotsOnline=%v", out.BotsOnline)
	}
}

// --- KeyRevokeResponse ---

func TestKeyRevokeResponseJSON(t *testing.T) {
	r := KeyRevokeResponse{Message: "key revoked"}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var out KeyRevokeResponse
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out.Message != "key revoked" {
		t.Errorf("Message=%v", out.Message)
	}
}

// --- BotConfigResponse ---

func TestBotConfigResponseJSON(t *testing.T) {
	r := BotConfigResponse{
		BotID:       "bot-xyz",
		Name:        "TestBot",
		AvatarColor: "#ff0000",
		Weapon:      "sword",
		Stats:       map[string]int{"hp": 5, "speed": 5, "attack": 5, "defense": 5},
		Fallback:    "aggressive",
		UpdatedAt:   time.Now(),
	}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var out BotConfigResponse
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out.BotID != "bot-xyz" {
		t.Errorf("BotID=%v", out.BotID)
	}
	if out.Stats["hp"] != 5 {
		t.Errorf("hp stat=%v", out.Stats["hp"])
	}
}

// --- LeaderboardEntry ---

func TestLeaderboardEntryRoundTrip(t *testing.T) {
	e := LeaderboardEntry{
		Rank:         1,
		BotID:        "bot-1",
		Name:         "Alpha",
		AvatarColor:  "#ff0000",
		Kills:        50,
		Deaths:       10,
		Elo:          1500,
		BestStreak:   7,
		DamageDealt:  10000,
		RoundsPlayed: 20,
		RoundWins:    15,
	}
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	var out LeaderboardEntry
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out.BestStreak != e.BestStreak {
		t.Errorf("BestStreak=%v", out.BestStreak)
	}
	if out.DamageDealt != e.DamageDealt {
		t.Errorf("DamageDealt=%v", out.DamageDealt)
	}
}

// Table-driven test for JSON field names
func TestSchemaJSONFieldNames(t *testing.T) {
	tests := []struct {
		name   string
		input  any
		field  string
		expect string
	}{
		{"error_field", ErrorResponse{Error: "fail"}, "error", "fail"},
		{"health_status", HealthResponse{Status: "ok", BotsOnline: 1}, "status", "ok"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(tc.input)
			if err != nil {
				t.Fatal(err)
			}
			var m map[string]any
			if err := json.Unmarshal(data, &m); err != nil {
				t.Fatal(err)
			}
			val, ok := m[tc.field]
			if !ok {
				t.Errorf("field %q missing from JSON: %s", tc.field, data)
			}
			if val != tc.expect {
				t.Errorf("field %q = %v, want %v", tc.field, val, tc.expect)
			}
		})
	}
}
