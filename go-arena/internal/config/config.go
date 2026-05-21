package config

import (
	"log/slog"

	"github.com/kelseyhightower/envconfig"
)

type Config struct {
	// Database
	DBHost     string `envconfig:"ARENA_DB_HOST" default:"localhost"`
	DBPort     int    `envconfig:"ARENA_DB_PORT" default:"5432"`
	DBName     string `envconfig:"ARENA_DB_NAME" default:"arena"`
	DBUser     string `envconfig:"ARENA_DB_USER" default:"arena"`
	DBPassword string `envconfig:"ARENA_DB_PASSWORD" default:"arena"`

	// Redis
	RedisHost string `envconfig:"ARENA_REDIS_HOST" default:"localhost"`
	RedisPort int    `envconfig:"ARENA_REDIS_PORT" default:"6379"`

	// Server
	ServerHost string `envconfig:"ARENA_HOST" default:"0.0.0.0"`
	ServerPort int    `envconfig:"ARENA_PORT" default:"8000"`

	// Game
	TickRate       int     `envconfig:"ARENA_TICK_RATE" default:"10"`
	MaxBots        int     `envconfig:"ARENA_MAX_BOTS" default:"500"`
	MaxSpectators  int     `envconfig:"ARENA_MAX_SPECTATORS" default:"500"`
	ViewRadius     float64 `envconfig:"ARENA_VIEW_RADIUS" default:"100"`
	ArenaWidth     float64 `envconfig:"ARENA_WIDTH" default:"2000"`
	ArenaHeight    float64 `envconfig:"ARENA_HEIGHT" default:"2000"`
	BotRadius      float64 `envconfig:"ARENA_BOT_RADIUS" default:"5.0"`
	SpatialCellSize float64 `envconfig:"ARENA_SPATIAL_CELL_SIZE" default:"100"`
	PathfindingCellSize float64 `envconfig:"ARENA_PATHFINDING_CELL_SIZE" default:"20"`
	FogRadius           int     `envconfig:"ARENA_FOG_RADIUS" default:"7"`

	// Combat
	StatBudget        int     `envconfig:"ARENA_STAT_BUDGET" default:"20"`
	StatMin           int     `envconfig:"ARENA_STAT_MIN" default:"1"`
	StatMax           int     `envconfig:"ARENA_STAT_MAX" default:"10"`
	RoundDuration     float64 `envconfig:"ARENA_ROUND_DURATION" default:"300"`
	IntermissionTime  float64 `envconfig:"ARENA_INTERMISSION_TIME" default:"10"`
	LobbyCountdown    float64 `envconfig:"ARENA_LOBBY_COUNTDOWN" default:"10"`
	MinBotsToStart    int     `envconfig:"ARENA_MIN_BOTS_TO_START" default:"2"`

	// Stat multipliers (for live balance tuning)
	StatHPBase       float64 `envconfig:"ARENA_STAT_HP_BASE" default:"100"`
	StatHPPerPoint   float64 `envconfig:"ARENA_STAT_HP_PER_POINT" default:"10"`
	StatSpeedBase    float64 `envconfig:"ARENA_STAT_SPEED_BASE" default:"3.0"`
	StatSpeedPerPoint float64 `envconfig:"ARENA_STAT_SPEED_PER_POINT" default:"0.5"`
	StatAttackBase   float64 `envconfig:"ARENA_STAT_ATTACK_BASE" default:"1.0"`
	StatAttackPerPoint float64 `envconfig:"ARENA_STAT_ATTACK_PER_POINT" default:"0.1"`
	StatDefensePerPoint float64 `envconfig:"ARENA_STAT_DEFENSE_PER_POINT" default:"0.03"`

	// Dodge
	DodgeSpeedMult   float64 `envconfig:"ARENA_DODGE_SPEED_MULT" default:"2.0"`
	DodgeInvulnTicks int     `envconfig:"ARENA_DODGE_INVULN_TICKS" default:"3"`
	DodgeCooldownTicks int   `envconfig:"ARENA_DODGE_COOLDOWN_TICKS" default:"30"`

	// Knockback
	KnockbackWallDamage float64 `envconfig:"ARENA_KNOCKBACK_WALL_DAMAGE" default:"5"`

	// Projectiles
	ProjectileSpeed      float64 `envconfig:"ARENA_PROJECTILE_SPEED" default:"240.0"`
	ProjectileHitRadius  float64 `envconfig:"ARENA_PROJECTILE_HIT_RADIUS" default:"1.0"`
	ProjectileMaxAgeSecs float64 `envconfig:"ARENA_PROJECTILE_MAX_AGE_SECS" default:"1.0"`

	// Staff
	StaffDelayTicks    int `envconfig:"ARENA_STAFF_DELAY_TICKS" default:"3"`
	StunDurationTicks  int `envconfig:"ARENA_STUN_DURATION_TICKS" default:"1"`

	// Universal Grapple Ability
	GrappleChargesPerRound     int     `envconfig:"ARENA_GRAPPLE_CHARGES_PER_ROUND" default:"2"`
	GrappleAbilityRangeTiles   int     `envconfig:"ARENA_GRAPPLE_RANGE_TILES" default:"12"`
	GrappleAbilityDamage       float64 `envconfig:"ARENA_GRAPPLE_DAMAGE" default:"15"`
	GrappleAbilityCooldownSecs float64 `envconfig:"ARENA_GRAPPLE_COOLDOWN_SECS" default:"4.0"`
	GrappleAbilityStunTicks    int     `envconfig:"ARENA_GRAPPLE_STUN_TICKS" default:"3"`

	// Shove
	ShoveRange        float64 `envconfig:"ARENA_SHOVE_RANGE" default:"2.0"`
	ShoveKnockback    float64 `envconfig:"ARENA_SHOVE_KNOCKBACK" default:"15.0"`
	ShoveStunTicks    int     `envconfig:"ARENA_SHOVE_STUN_TICKS" default:"2"`
	ShoveCooldown     float64 `envconfig:"ARENA_SHOVE_COOLDOWN" default:"1.5"`

	// Zone
	ZoneInitialRadius   float64 `envconfig:"ARENA_ZONE_INITIAL_RADIUS" default:"1000.0"`
	ZoneCenterX         float64 `envconfig:"ARENA_ZONE_CENTER_X" default:"1000.0"`
	ZoneCenterY         float64 `envconfig:"ARENA_ZONE_CENTER_Y" default:"1000.0"`
	ZoneShrinkPercent   float64 `envconfig:"ARENA_ZONE_SHRINK_PERCENT" default:"0.15"`
	ZoneShrinkInterval  float64 `envconfig:"ARENA_ZONE_SHRINK_INTERVAL_SECS" default:"20"`
	ZoneDamagePerTick   float64 `envconfig:"ARENA_ZONE_DAMAGE_PER_TICK" default:"3"`
	ZoneMinRadius       float64 `envconfig:"ARENA_ZONE_MIN_RADIUS" default:"175.0"`
	ZoneShrinkDelay     float64 `envconfig:"ARENA_ZONE_SHRINK_DELAY_SECS" default:"60"`

	// Obstacles
	ObstacleCountMin int `envconfig:"ARENA_OBSTACLE_COUNT_MIN" default:"20"`
	ObstacleCountMax int `envconfig:"ARENA_OBSTACLE_COUNT_MAX" default:"30"`

	// Pickups
	PickupSpawnIntervalTicks int     `envconfig:"ARENA_PICKUP_SPAWN_INTERVAL_TICKS" default:"50"`
	PickupMaxActive          int     `envconfig:"ARENA_PICKUP_MAX_ACTIVE" default:"20"`
	PickupHealthAmount       float64 `envconfig:"ARENA_PICKUP_HEALTH_AMOUNT" default:"30"`
	PickupSpeedBoostMult     float64 `envconfig:"ARENA_PICKUP_SPEED_BOOST_MULT" default:"2.0"`
	PickupSpeedBoostTicks    int     `envconfig:"ARENA_PICKUP_SPEED_BOOST_TICKS" default:"50"`
	PickupDamageBoostMult    float64 `envconfig:"ARENA_PICKUP_DAMAGE_BOOST_MULT" default:"1.5"`
	PickupDamageBoostTicks   int     `envconfig:"ARENA_PICKUP_DAMAGE_BOOST_TICKS" default:"50"`
	PickupShieldBubbleHP     float64 `envconfig:"ARENA_PICKUP_SHIELD_BUBBLE_HP" default:"50"`
	PickupCollectRadius      float64 `envconfig:"ARENA_PICKUP_COLLECT_RADIUS" default:"2.0"`

	// Network / persistence
	PersistIntervalSecs          float64 `envconfig:"ARENA_PERSIST_INTERVAL_SECS" default:"30"`
	KillFeedSize                 int     `envconfig:"ARENA_KILL_FEED_SIZE" default:"20"`
	WSMessageMaxBytes            int     `envconfig:"ARENA_WS_MESSAGE_MAX_BYTES" default:"1024"`
	WSMaxMessagesPerSec          int     `envconfig:"ARENA_WS_MAX_MESSAGES_PER_SEC" default:"25"`
	ConnectionTimeout            float64 `envconfig:"ARENA_CONNECTION_TIMEOUT" default:"10"`
	HeartbeatInterval            float64 `envconfig:"ARENA_HEARTBEAT_INTERVAL" default:"30"`
	WSConnectRatePerMin          int     `envconfig:"ARENA_WS_CONNECT_RATE_PER_MIN" default:"3"`
	LoadoutTimeoutSecs           float64 `envconfig:"ARENA_LOADOUT_TIMEOUT_SECS" default:"10"`
	SpectatorBroadcastInterval   int     `envconfig:"ARENA_SPECTATOR_BROADCAST_INTERVAL" default:"1"`
	AFKTimeoutTicks              int     `envconfig:"ARENA_AFK_TIMEOUT_TICKS" default:"30"`

	// Admin
	AdminKey            string `envconfig:"ARENA_ADMIN_KEY" default:"changeme_admin_key"`
	AdminToken          string `envconfig:"ARENA_ADMIN_TOKEN" default:""`
	AdminLocalhostBypass bool   `envconfig:"ARENA_ADMIN_LOCALHOST_BYPASS" default:"true"`
	AdminRateLimitRPM   int    `envconfig:"ARENA_ADMIN_RATE_LIMIT_RPM" default:"120"`

	// Cloudflare (optional — for IP ban push)
	CloudflareAPIToken string `envconfig:"ARENA_CF_API_TOKEN" default:""`
	CloudflareZoneID   string `envconfig:"ARENA_CF_ZONE_ID" default:""`

	// CORS
	CORSOrigins string `envconfig:"ARENA_CORS_ORIGINS" default:"*"`

	// DB Pool
	DBPoolSize    int `envconfig:"ARENA_DB_POOL_SIZE" default:"20"`
	DBMaxOverflow int `envconfig:"ARENA_DB_MAX_OVERFLOW" default:"10"`

	// Frontend UI
	UIBgColor      string `envconfig:"ARENA_UI_BG_COLOR" default:"#1a1a2e"`
	UIBgSecondary  string `envconfig:"ARENA_UI_BG_SECONDARY" default:"#16213e"`
	UIAccentBlue   string `envconfig:"ARENA_UI_ACCENT_BLUE" default:"#0f3460"`
	UIAccentRed    string `envconfig:"ARENA_UI_ACCENT_RED" default:"#e94560"`
	UIAccentGold   string `envconfig:"ARENA_UI_ACCENT_GOLD" default:"#ffd700"`
	UITextColor    string `envconfig:"ARENA_UI_TEXT_COLOR" default:"#eee"`
	UIGridColor    string `envconfig:"ARENA_UI_GRID_COLOR" default:"#333"`
	UIFontFamily   string `envconfig:"ARENA_UI_FONT_FAMILY" default:"'Courier New', monospace"`
	UIMinimapSize  int    `envconfig:"ARENA_UI_MINIMAP_SIZE" default:"200"`

	// Rate limiting per endpoint
	RateLimitBotConfigPerMin int `envconfig:"ARENA_RATE_LIMIT_BOT_CONFIG_PER_MIN" default:"120"`

	// Security
	APIKeyPrefix           string `envconfig:"ARENA_API_KEY_PREFIX" default:"arena_"`
	BcryptRounds           int    `envconfig:"ARENA_BCRYPT_ROUNDS" default:"12"`
	RateLimitRPM           int    `envconfig:"ARENA_RATE_LIMIT_RPM" default:"1200"`
	RateLimitRegisterPerHour int  `envconfig:"ARENA_RATE_LIMIT_REGISTER_PER_HOUR" default:"500"`

	// ELO
	EloKFactor    float64 `envconfig:"ARENA_ELO_K_FACTOR" default:"32"`
	EloStarting   int     `envconfig:"ARENA_ELO_STARTING" default:"1000"`
	EloMin        int     `envconfig:"ARENA_ELO_MIN" default:"100"`

	// Bot separation
	BotSeparationDist float64 `envconfig:"ARENA_BOT_SEPARATION_DIST" default:"20.0"`
	BotSeparationFactor float64 `envconfig:"ARENA_BOT_SEPARATION_FACTOR" default:"1.5"`

	// Anti-teaming
	AntiTeamRadius         float64 `envconfig:"ARENA_ANTI_TEAM_RADIUS" default:"30.0"`
	AntiTeamThresholdTicks int     `envconfig:"ARENA_ANTI_TEAM_THRESHOLD_TICKS" default:"50"`
	AntiTeamDamagePerTick  float64 `envconfig:"ARENA_ANTI_TEAM_DAMAGE_PER_TICK" default:"2.0"`

	// Teleport Pads
	TeleportPadPairs       int `envconfig:"ARENA_TELEPORT_PAD_PAIRS" default:"3"`
	TeleportCooldownTicks  int `envconfig:"ARENA_TELEPORT_COOLDOWN_TICKS" default:"50"`
	TeleportCollectRadius  int `envconfig:"ARENA_TELEPORT_COLLECT_RADIUS" default:"1"`
	TeleportPadLockTicks   int `envconfig:"ARENA_TELEPORT_PAD_LOCK_TICKS" default:"30"`

	// Environmental Hazards
	HazardZoneCount        int     `envconfig:"ARENA_HAZARD_ZONE_COUNT" default:"6"`
	HazardMinWidth         int     `envconfig:"ARENA_HAZARD_MIN_WIDTH" default:"2"`
	HazardMaxWidth         int     `envconfig:"ARENA_HAZARD_MAX_WIDTH" default:"4"`
	HazardDamagePerTick    float64 `envconfig:"ARENA_HAZARD_DAMAGE_PER_TICK" default:"3"`
	HazardPulseOnTicks     int     `envconfig:"ARENA_HAZARD_PULSE_ON_TICKS" default:"30"`
	HazardPulseOffTicks    int     `envconfig:"ARENA_HAZARD_PULSE_OFF_TICKS" default:"20"`

	// Sudden Death
	SuddenDeathTilesPerTick int     `envconfig:"ARENA_SUDDEN_DEATH_TILES_PER_TICK" default:"2"`
	SuddenDeathDamage       float64 `envconfig:"ARENA_SUDDEN_DEATH_DAMAGE" default:"999"`

	// Bounty System
	BountyKillStreakThreshold int     `envconfig:"ARENA_BOUNTY_KILL_STREAK" default:"3"`
	BountyBonusPoints        float64 `envconfig:"ARENA_BOUNTY_BONUS_POINTS" default:"50"`
	BountyWinStreakThreshold int     `envconfig:"ARENA_BOUNTY_WIN_STREAK" default:"1"`
	BountyBoardBasePoints    int     `envconfig:"ARENA_BOUNTY_BOARD_BASE_POINTS" default:"6"`
	BountyBoardStepPoints    int     `envconfig:"ARENA_BOUNTY_BOARD_STEP_POINTS" default:"4"`
	BountyBoardMaxPoints     int     `envconfig:"ARENA_BOUNTY_BOARD_MAX_POINTS" default:"18"`

	// Landmines
	MineMaxPerBot     int     `envconfig:"ARENA_MINE_MAX_PER_BOT" default:"3"`
	MineDamage        float64 `envconfig:"ARENA_MINE_DAMAGE" default:"40"`
	MineBlastRadius   int     `envconfig:"ARENA_MINE_BLAST_RADIUS" default:"1"`
	MineArmDelayTicks int     `envconfig:"ARENA_MINE_ARM_DELAY_TICKS" default:"10"`

	// Gravity Well
	GravityWellDurationTicks int     `envconfig:"ARENA_GRAVITY_WELL_DURATION_TICKS" default:"30"`
	GravityWellPullRadius    int     `envconfig:"ARENA_GRAVITY_WELL_PULL_RADIUS" default:"3"`
	GravityWellPullForce     float64 `envconfig:"ARENA_GRAVITY_WELL_PULL_FORCE" default:"0.5"`

	// Automatic weapon balancing
	WeaponAutoBalanceEnabled          bool    `envconfig:"ARENA_WEAPON_AUTO_BALANCE_ENABLED" default:"true"`
	WeaponAutoBalanceStartStep        float64 `envconfig:"ARENA_WEAPON_AUTO_BALANCE_START_STEP" default:"0.05"`
	WeaponAutoBalanceMinStep          float64 `envconfig:"ARENA_WEAPON_AUTO_BALANCE_MIN_STEP" default:"0.005"`
	WeaponAutoBalanceDecay            float64 `envconfig:"ARENA_WEAPON_AUTO_BALANCE_DECAY" default:"0.94"`
	WeaponAutoBalanceDeadzoneStart    float64 `envconfig:"ARENA_WEAPON_AUTO_BALANCE_DEADZONE_START" default:"0.02"`
	WeaponAutoBalanceDeadzoneMin      float64 `envconfig:"ARENA_WEAPON_AUTO_BALANCE_DEADZONE_MIN" default:"0.003"`
	WeaponAutoBalanceMinDamageScale   float64 `envconfig:"ARENA_WEAPON_AUTO_BALANCE_MIN_DAMAGE_SCALE" default:"0.80"`
	WeaponAutoBalanceMaxDamageScale   float64 `envconfig:"ARENA_WEAPON_AUTO_BALANCE_MAX_DAMAGE_SCALE" default:"1.30"`
	WeaponAutoBalanceMinCooldownScale float64 `envconfig:"ARENA_WEAPON_AUTO_BALANCE_MIN_COOLDOWN_SCALE" default:"0.85"`
	WeaponAutoBalanceMaxCooldownScale float64 `envconfig:"ARENA_WEAPON_AUTO_BALANCE_MAX_COOLDOWN_SCALE" default:"1.20"`
	WeaponAutoBalanceDamageWeight     float64 `envconfig:"ARENA_WEAPON_AUTO_BALANCE_DAMAGE_WEIGHT" default:"0.65"`
	WeaponAutoBalanceCooldownWeight   float64 `envconfig:"ARENA_WEAPON_AUTO_BALANCE_COOLDOWN_WEIGHT" default:"0.45"`

	// OIDC / SSO (opt-in)
	OIDCEnabled      bool   `envconfig:"ARENA_OIDC_ENABLED" default:"false"`
	OIDCIssuer       string `envconfig:"ARENA_OIDC_ISSUER" default:""`
	OIDCClientID     string `envconfig:"ARENA_OIDC_CLIENT_ID" default:""`
	OIDCClientSecret string `envconfig:"ARENA_OIDC_CLIENT_SECRET" default:""`
	OIDCRedirectURI  string `envconfig:"ARENA_OIDC_REDIRECT_URI" default:""`
	OIDCSessionTTL   int    `envconfig:"ARENA_OIDC_SESSION_TTL_HOURS" default:"8"`
}

var C Config

func Load() {
	if err := envconfig.Process("", &C); err != nil {
		slog.Error("failed to load config", "error", err)
		panic(err)
	}
	slog.Info("config loaded",
		"host", C.ServerHost,
		"port", C.ServerPort,
		"tick_rate", C.TickRate,
		"arena", [2]float64{C.ArenaWidth, C.ArenaHeight},
	)
}
