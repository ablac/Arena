package api

import "time"

// KeyGenerateResponse is returned when a new API key is created.
type KeyGenerateResponse struct {
	APIKey    string    `json:"api_key"`
	BotID     string    `json:"bot_id"`
	CreatedAt time.Time `json:"created_at"`
	Message   string    `json:"message"`
}

// KeyRevokeResponse is returned when an API key is deactivated.
type KeyRevokeResponse struct {
	Message string `json:"message"`
}

// BotConfigRequest is the body for updating bot configuration.
type BotConfigRequest struct {
	Name           *string         `json:"name,omitempty"`
	AvatarColor    *string         `json:"avatar_color,omitempty"`
	DefaultLoadout *LoadoutRequest `json:"default_loadout,omitempty"`
}

// LoadoutRequest describes a loadout within a BotConfigRequest.
type LoadoutRequest struct {
	Weapon   string         `json:"weapon"`
	Stats    map[string]int `json:"stats"`
	Fallback string         `json:"fallback_behavior"`
}

// BotConfigResponse is returned after a successful bot config update.
type BotConfigResponse struct {
	BotID       string         `json:"bot_id"`
	Name        string         `json:"name"`
	AvatarColor string         `json:"avatar_color"`
	Weapon      string         `json:"default_weapon"`
	Stats       map[string]int `json:"default_stats"`
	Fallback    string         `json:"default_fallback"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

// BotStatsResponse is returned for the bot stats endpoint.
type BotStatsResponse struct {
	BotID            string  `json:"bot_id"`
	Name             string  `json:"name"`
	Kills            int     `json:"kills"`
	Deaths           int     `json:"deaths"`
	KDRatio          float64 `json:"kd_ratio"`
	Assists          int     `json:"assists"`
	DamageDealt      int64   `json:"damage_dealt"`
	DamageTaken      int64   `json:"damage_taken"`
	CurrentStreak    int     `json:"current_streak"`
	BestStreak       int     `json:"best_streak"`
	Elo              int     `json:"elo"`
	Rank             int     `json:"rank"`
	RoundsPlayed     int     `json:"rounds_played"`
	RoundWins        int     `json:"round_wins"`
	PickupsCollected int     `json:"pickups_collected"`
	DistanceTraveled float64 `json:"distance_traveled"`
	TimeAliveSeconds int64   `json:"time_alive_seconds"`
	LongestLifeSecs  int     `json:"longest_life_secs"`
}

// ArenaStatusResponse is returned for the arena status endpoint.
type ArenaStatusResponse struct {
	Status             string  `json:"status"`
	BotsConnected      int     `json:"bots_connected"`
	BotsAlive          int     `json:"bots_alive"`
	RoundNumber        int     `json:"round_number"`
	RoundTimeRemaining float64 `json:"round_time_remaining"`
	SafeZoneRadius     float64 `json:"safe_zone_radius"`
	TopBot             string  `json:"top_bot"`
}

// LeaderboardEntry represents a single row in the leaderboard.
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

// LeaderboardResponse wraps a paginated list of leaderboard entries.
type LeaderboardResponse struct {
	Entries []LeaderboardEntry `json:"entries"`
	Total   int                `json:"total"`
	Limit   int                `json:"limit"`
	Offset  int                `json:"offset"`
}

// ErrorResponse is the standard error envelope.
type ErrorResponse struct {
	Error string `json:"error"`
}

// HealthResponse is returned by the health check endpoint.
type HealthResponse struct {
	Status     string `json:"status"`
	BotsOnline int    `json:"bots_online"`
}
