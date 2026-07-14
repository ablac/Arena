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

// BountyBoardEntry represents a single active bounty target row.
type BountyBoardEntry struct {
	Rank         int    `json:"rank"`
	BotID        string `json:"bot_id"`
	Name         string `json:"name"`
	AvatarColor  string `json:"avatar_color"`
	Weapon       string `json:"weapon"`
	BountyPoints int    `json:"bounty_points"`
	WinStreak    int    `json:"win_streak"`
	Claims       int    `json:"claims"`
	IsTarget     bool   `json:"is_target"`
}

// BountyBoardResponse wraps the current public bounty board.
type BountyBoardResponse struct {
	Entries []BountyBoardEntry `json:"entries"`
	Total   int                `json:"total"`
}

// WeaponStatsEntry represents one weapon's live balance and performance state.
type WeaponStatsEntry struct {
	Rank             int                         `json:"rank"`
	Weapon           string                      `json:"weapon"`
	Tier             string                      `json:"tier"`
	MetaScore        float64                     `json:"meta_score"`
	RecentForm       float64                     `json:"recent_form"`
	RecentRoundScore float64                     `json:"recent_round_score"`
	RecentDiffPct    float64                     `json:"recent_diff_pct"`
	RecentRounds     int                         `json:"recent_rounds"`
	RecentConfidence float64                     `json:"recent_confidence"`
	BalanceDirection string                      `json:"balance_direction"`
	Kills            int                         `json:"kills"`
	Kills24h         int                         `json:"kills_24h"`
	Kills1h          int                         `json:"kills_1h"`
	FinisherDamage   int64                       `json:"finisher_damage"`
	Damage           int                         `json:"damage"`
	DamageExact      float64                     `json:"damage_exact"`
	Cooldown         float64                     `json:"cooldown"`
	Range            float64                     `json:"range"`
	GridRange        int                         `json:"grid_range"`
	Special          string                      `json:"special"`
	BaseDamage       int                         `json:"base_damage"`
	BaseCooldown     float64                     `json:"base_cooldown"`
	DamageScale      float64                     `json:"damage_scale"`
	CooldownScale    float64                     `json:"cooldown_scale"`
	AdjustmentScale  float64                     `json:"adjustment_scale"`
	DamageTrend      string                      `json:"damage_trend"`
	CooldownTrend    string                      `json:"cooldown_trend"`
	LastDamageMove   string                      `json:"last_damage_move"`
	LastCooldownMove string                      `json:"last_cooldown_move"`
	DamageShiftPct   float64                     `json:"damage_shift_pct"`
	CooldownShiftPct float64                     `json:"cooldown_shift_pct"`
	ShotsFired       int                         `json:"shots_fired"`
	ShotsHit         int                         `json:"shots_hit"`
	HitRate          float64                     `json:"hit_rate"`
	DamagePerShot    float64                     `json:"damage_per_shot"`
	DamagePerHit     float64                     `json:"damage_per_hit"`
	ShotsPerLife     float64                     `json:"shots_per_life"`
	KillsPerHit      float64                     `json:"kills_per_hit"`
	RoundsTracked    int                         `json:"rounds_tracked"`
	LastBalanceAt    time.Time                   `json:"last_balance_at"`
	History          []WeaponBalanceHistoryPoint `json:"history,omitempty"`
}

// WeaponBalanceHistoryPoint represents one historical balance snapshot for charting.
type WeaponBalanceHistoryPoint struct {
	Round         int       `json:"round"`
	DamageScale   float64   `json:"damage_scale"`
	CooldownScale float64   `json:"cooldown_scale"`
	DamageExact   float64   `json:"damage_exact"`
	Cooldown      float64   `json:"cooldown"`
	DiffPct       float64   `json:"diff_pct"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// WeaponStatsResponse wraps the public live weapon stats board.
type WeaponStatsResponse struct {
	Entries   []WeaponStatsEntry `json:"entries"`
	UpdatedAt time.Time          `json:"updated_at"`
}

// ErrorResponse is the standard error envelope.
type ErrorResponse struct {
	Error string `json:"error"`
}

// HealthResponse is returned by the health check endpoint.
type HealthResponse struct {
	Status     string `json:"status"`
	BotsOnline int    `json:"bots_online"`
	Commit     string `json:"commit,omitempty"`
}

// VersionResponse identifies the running build for the About dialog and ops
// tooling.
type VersionResponse struct {
	Status      string `json:"status"`
	Commit      string `json:"commit"`
	CommitShort string `json:"commit_short"`
	BuildTime   string `json:"build_time"`
	GoVersion   string `json:"go_version"`
	Repo        string `json:"repo"`
}
