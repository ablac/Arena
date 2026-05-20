package db

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

// JSONBStats is a map[string]int that implements sql Scanner and Valuer for JSONB columns.
type JSONBStats map[string]int

// Scan implements the sql.Scanner interface for reading JSONB from the database.
func (j *JSONBStats) Scan(src interface{}) error {
	if src == nil {
		*j = nil
		return nil
	}

	var data []byte
	switch v := src.(type) {
	case []byte:
		data = v
	case string:
		data = []byte(v)
	default:
		return fmt.Errorf("cannot scan %T into JSONBStats", src)
	}

	return json.Unmarshal(data, j)
}

// Value implements the driver.Valuer interface for writing JSONB to the database.
func (j JSONBStats) Value() (driver.Value, error) {
	if j == nil {
		return nil, nil
	}
	return json.Marshal(j)
}

// ApiKey represents a row in the api_keys table.
type ApiKey struct {
	ID        string     `json:"id"`
	KeyHash   string     `json:"key_hash"`
	KeyPrefix string     `json:"key_prefix"`
	CreatedAt time.Time  `json:"created_at"`
	LastSeen  *time.Time `json:"last_seen"`
	IsActive  bool       `json:"is_active"`
	IPCreated *string    `json:"ip_created"`
}

// Bot represents a row in the bots table.
type Bot struct {
	ID              string     `json:"id"`
	APIKeyID        string     `json:"api_key_id"`
	Name            string     `json:"name"`
	AvatarColor     string     `json:"avatar_color"`
	DefaultWeapon   string     `json:"default_weapon"`
	DefaultStats    JSONBStats `json:"default_stats"`
	DefaultFallback string     `json:"default_fallback"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// BotStats represents a row in the bot_stats table.
type BotStats struct {
	BotID            string    `json:"bot_id"`
	Kills            int       `json:"kills"`
	Deaths           int       `json:"deaths"`
	Assists          int       `json:"assists"`
	DamageDealt      int64     `json:"damage_dealt"`
	DamageTaken      int64     `json:"damage_taken"`
	CurrentStreak    int       `json:"current_streak"`
	BestStreak       int       `json:"best_streak"`
	Elo              int       `json:"elo"`
	TimeAliveSecs    int64     `json:"time_alive_seconds"`
	LongestLifeSecs  int       `json:"longest_life_secs"`
	RoundsPlayed     int       `json:"rounds_played"`
	RoundWins        int       `json:"round_wins"`
	PickupsCollected int       `json:"pickups_collected"`
	DistanceTraveled float64   `json:"distance_traveled"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// KillLog represents a row in the kill_log table.
type KillLog struct {
	ID        string    `json:"id"`
	RoundID   *string   `json:"round_id"`
	KillerID  string    `json:"killer_id"`
	VictimID  string    `json:"victim_id"`
	Weapon    string    `json:"weapon"`
	Damage    int       `json:"damage"`
	KillerHP  int       `json:"killer_hp"`
	Tick      int       `json:"tick"`
	CreatedAt time.Time `json:"created_at"`
}

// Round represents a row in the rounds table.
type Round struct {
	ID               string     `json:"id"`
	RoundNumber      int        `json:"round_number"`
	StartedAt        time.Time  `json:"started_at"`
	EndedAt          *time.Time `json:"ended_at"`
	BotsParticipated int        `json:"bots_participated"`
	MVPBotID         *string    `json:"mvp_bot_id"`
	Status           string     `json:"status"`
}

// WeaponBalance represents the adaptive balancing state for a weapon.
type WeaponBalance struct {
	Weapon          string    `json:"weapon"`
	DamageScale     float64   `json:"damage_scale"`
	CooldownScale   float64   `json:"cooldown_scale"`
	AdjustmentScale float64   `json:"adjustment_scale"`
	RoundsTracked   int       `json:"rounds_tracked"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// WeaponKillStats represents aggregated kill activity for a weapon.
type WeaponKillStats struct {
	Weapon         string `json:"weapon"`
	Kills          int    `json:"kills"`
	Kills24h       int    `json:"kills_24h"`
	Kills1h        int    `json:"kills_1h"`
	FinisherDamage int64  `json:"finisher_damage"`
}

// WeaponRecentPerformance represents aggregated per-weapon performance across recent rounds.
type WeaponRecentPerformance struct {
	Weapon    string  `json:"weapon"`
	Bots      int     `json:"bots"`
	Wins      int     `json:"wins"`
	Rounds    int     `json:"rounds"`
	AvgScore  float64 `json:"avg_score"`
}

// WeaponBalanceHistory captures one adaptive balancing decision snapshot.
type WeaponBalanceHistory struct {
	Weapon          string    `json:"weapon"`
	RoundsTracked   int       `json:"rounds_tracked"`
	DamageScale     float64   `json:"damage_scale"`
	CooldownScale   float64   `json:"cooldown_scale"`
	AdjustmentScale float64   `json:"adjustment_scale"`
	AvgScore        float64   `json:"avg_score"`
	MeanScore       float64   `json:"mean_score"`
	DiffPct         float64   `json:"diff_pct"`
	DamageDelta     float64   `json:"damage_delta"`
	CooldownDelta   float64   `json:"cooldown_delta"`
	CreatedAt       time.Time `json:"created_at"`
}

// BountyBoardEntry represents a persisted bounty board row.
type BountyBoardEntry struct {
	BotID        string    `json:"bot_id"`
	Name         string    `json:"name"`
	AvatarColor  string    `json:"avatar_color"`
	Weapon       string    `json:"weapon"`
	WinStreak    int       `json:"win_streak"`
	BountyPoints int       `json:"bounty_points"`
	Claims       int       `json:"claims"`
	IsTarget     bool      `json:"is_target"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// RateLimit represents a row in the rate_limits table.
type RateLimit struct {
	IPAddress     string    `json:"ip_address"`
	KeysGenerated int       `json:"keys_generated"`
	WindowStart   time.Time `json:"window_start"`
}

// LeaderboardEntry is the struct returned by leaderboard queries.
type LeaderboardEntry struct {
	Rank         int    `json:"rank"`
	BotID        string `json:"bot_id"`
	Name         string `json:"name"`
	AvatarColor  string `json:"avatar_color"`
	Kills        int    `json:"kills"`
	Deaths       int    `json:"deaths"`
	Elo          int    `json:"elo"`
	BestStreak   int    `json:"best_streak"`
	DamageDealt  int64  `json:"damage_dealt"`
	RoundsPlayed int    `json:"rounds_played"`
	RoundWins    int    `json:"round_wins"`
}
