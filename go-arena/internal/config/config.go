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
	ArenaWidth     float64 `envconfig:"ARENA_WIDTH" default:"2000"`
	ArenaHeight    float64 `envconfig:"ARENA_HEIGHT" default:"2000"`
	BotRadius      float64 `envconfig:"ARENA_BOT_RADIUS" default:"5.0"`
	SpatialCellSize float64 `envconfig:"ARENA_SPATIAL_CELL_SIZE" default:"100"`
	PathfindingCellSize float64 `envconfig:"ARENA_PATHFINDING_CELL_SIZE" default:"20"`
	FogRadius           int     `envconfig:"ARENA_FOG_RADIUS" default:"7"`

	// Game modes (groundwork). "ffa" (default), "team_battle", "ctf".
	GameModeName     string  `envconfig:"ARENA_GAME_MODE" default:"ffa"`
	TeamCount        int     `envconfig:"ARENA_TEAM_COUNT" default:"2"`
	FriendlyFire     bool    `envconfig:"ARENA_FRIENDLY_FIRE" default:"false"`
	CTFCapturesToWin    int     `envconfig:"ARENA_CTF_CAPTURES_TO_WIN" default:"3"`
	CTFFlagPickupRadius float64 `envconfig:"ARENA_CTF_FLAG_PICKUP_RADIUS" default:"25"`
	CTFFlagReturnSecs   float64 `envconfig:"ARENA_CTF_FLAG_RETURN_SECS" default:"20"`

	// Map generation. "square" (classic), "circle", "hexagon", "diamond",
	// "cross", "caves", or "random" to roll a shape each round.
	MapShape string `envconfig:"ARENA_MAP_SHAPE" default:"random"`

	// Dynamic arena size: the map grows with the number of bots joining the
	// round, from the base ARENA_WIDTH/HEIGHT at <= ARENA_SIZE_BASE_BOTS up
	// to ARENA_SIZE_MAX_SCALE times the base dimensions at
	// >= ARENA_SIZE_MAX_BOTS. Applied when each round's terrain is generated.
	ArenaSizeDynamic  bool    `envconfig:"ARENA_SIZE_DYNAMIC" default:"true"`
	ArenaSizeBaseBots int     `envconfig:"ARENA_SIZE_BASE_BOTS" default:"12"`
	ArenaSizeMaxBots  int     `envconfig:"ARENA_SIZE_MAX_BOTS" default:"48"`
	ArenaSizeMaxScale float64 `envconfig:"ARENA_SIZE_MAX_SCALE" default:"2.0"`
	// Below ARENA_SIZE_BASE_BOTS the map shrinks linearly down to
	// ARENA_SIZE_MIN_SCALE times the base dimensions at 2 bots (a duel plays
	// on a small arena instead of the full map; GitHub issue #12). Set to 1.0
	// to disable shrinking and keep the pre-delta scale-up-only behavior.
	ArenaSizeMinScale float64 `envconfig:"ARENA_SIZE_MIN_SCALE" default:"0.6"`

	// Spectator keyframes: static round data (obstacles, map shape) is only
	// included every Nth broadcast plus immediately after a spectator joins.
	// 1 = include everything every tick (legacy behaviour).
	SpectatorKeyframeInterval int `envconfig:"ARENA_SPECTATOR_KEYFRAME_INTERVAL" default:"10"`

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
	StaffBurnFieldTicks int `envconfig:"ARENA_STAFF_BURN_FIELD_TICKS" default:"12"`
	StaffBurnFieldRadius int `envconfig:"ARENA_STAFF_BURN_FIELD_RADIUS" default:"1"`
	StaffBurnFieldTickInterval int `envconfig:"ARENA_STAFF_BURN_FIELD_TICK_INTERVAL" default:"2"`
	StaffBurnFieldDamage float64 `envconfig:"ARENA_STAFF_BURN_FIELD_DAMAGE" default:"3"`
	StunDurationTicks  int `envconfig:"ARENA_STUN_DURATION_TICKS" default:"1"`

	// Weapon signatures
	ShieldBashBonusMultiplier float64 `envconfig:"ARENA_SHIELD_BASH_BONUS_MULT" default:"1.35"`
	ShieldDisruptWindowTicks  int     `envconfig:"ARENA_SHIELD_DISRUPT_WINDOW_TICKS" default:"10"`
	DaggerBackstabBonusMultiplier float64 `envconfig:"ARENA_DAGGER_BACKSTAB_BONUS_MULT" default:"1.45"`
	DaggerBackstabDotThreshold float64 `envconfig:"ARENA_DAGGER_BACKSTAB_DOT_THRESHOLD" default:"-0.35"`
	SpearBraceStillTicks int     `envconfig:"ARENA_SPEAR_BRACE_STILL_TICKS" default:"2"`
	SpearBraceBonusMultiplier float64 `envconfig:"ARENA_SPEAR_BRACE_BONUS_MULT" default:"1.35"`
	SpearBraceBonusKnockback int `envconfig:"ARENA_SPEAR_BRACE_BONUS_KNOCKBACK" default:"1"`
	BowChargeMaxTicks int `envconfig:"ARENA_BOW_CHARGE_MAX_TICKS" default:"6"`
	BowChargeReadyTicks int `envconfig:"ARENA_BOW_CHARGE_READY_TICKS" default:"2"`
	BowChargeDamagePerTick float64 `envconfig:"ARENA_BOW_CHARGE_DAMAGE_PER_TICK" default:"0.12"`
	BowChargeSpeedPerTick float64 `envconfig:"ARENA_BOW_CHARGE_SPEED_PER_TICK" default:"0.08"`
	BowChargeCooldownPerTick float64 `envconfig:"ARENA_BOW_CHARGE_COOLDOWN_PER_TICK" default:"0.06"`
	GrappleSlamMinRange int `envconfig:"ARENA_GRAPPLE_SLAM_MIN_RANGE" default:"3"`
	GrappleSlamBonusMultiplier float64 `envconfig:"ARENA_GRAPPLE_SLAM_BONUS_MULT" default:"1.4"`
	GrappleSlamStunTicks int `envconfig:"ARENA_GRAPPLE_SLAM_STUN_TICKS" default:"2"`

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
	ZoneCoverMap        bool    `envconfig:"ARENA_ZONE_COVER_MAP" default:"true"`
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
	PickupCooldownShardTicks   int     `envconfig:"ARENA_PICKUP_COOLDOWN_SHARD_TICKS" default:"100"`
	PickupCooldownShardMult    float64 `envconfig:"ARENA_PICKUP_COOLDOWN_SHARD_MULT" default:"0.6"`
	PickupBountyTokenPoints  int     `envconfig:"ARENA_PICKUP_BOUNTY_TOKEN_POINTS" default:"18"`
	PickupBountyTokenTicks   int     `envconfig:"ARENA_PICKUP_BOUNTY_TOKEN_TICKS" default:"90"`
	PickupHazardKeyTicks     int     `envconfig:"ARENA_PICKUP_HAZARD_KEY_TICKS" default:"80"`
	PickupOverdriveTicks     int     `envconfig:"ARENA_PICKUP_OVERDRIVE_TICKS" default:"60"`
	PickupOverdriveDamageMult float64 `envconfig:"ARENA_PICKUP_OVERDRIVE_DAMAGE_MULT" default:"1.25"`
	PickupOverdriveCooldownMult float64 `envconfig:"ARENA_PICKUP_OVERDRIVE_COOLDOWN_MULT" default:"0.75"`
	PickupGrappleChargeAmount int     `envconfig:"ARENA_PICKUP_GRAPPLE_CHARGE_AMOUNT" default:"1"`
	PickupRelayBatteryTicks  int     `envconfig:"ARENA_PICKUP_RELAY_BATTERY_TICKS" default:"90"`
	PickupRelayBatteryBonusProgress int `envconfig:"ARENA_PICKUP_RELAY_BATTERY_BONUS_PROGRESS" default:"1"`
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

	// Self-update (optional). The Admin Panel "Update" action is only enabled
	// when both UpdaterURL and UpdaterSharedSecret are set and point at a running
	// arena-updater sidecar (see docs/build-and-deploy.md).
	UpdaterURL          string `envconfig:"ARENA_UPDATER_URL" default:""`
	UpdaterSharedSecret string `envconfig:"ARENA_UPDATER_SHARED_SECRET" default:""`
	// Optional GitHub token to raise the tarball-fetch / compare rate limit.
	// Arena is a public repo, so this is not required.
	UpdateGitHubToken string `envconfig:"ARENA_UPDATE_GITHUB_TOKEN" default:""`
	// owner/repo and branch the "update to latest" check compares the running
	// build against (production release branch by default).
	UpdateRepo   string `envconfig:"ARENA_UPDATE_REPO" default:"ablac/Arena"`
	UpdateBranch string `envconfig:"ARENA_UPDATE_BRANCH" default:"main"`

	// CORS
	CORSOrigins string `envconfig:"ARENA_CORS_ORIGINS" default:"*"`

	// Security headers (HSTS, CSP, X-Frame-Options, etc). Enabled by default;
	// provided as an escape hatch in case a header ever conflicts with a
	// deployment's reverse-proxy setup.
	SecurityHeadersEnabled bool `envconfig:"ARENA_SECURITY_HEADERS_ENABLED" default:"true"`

	// DB Pool
	DBPoolSize    int `envconfig:"ARENA_DB_POOL_SIZE" default:"20"`
	DBMaxOverflow int `envconfig:"ARENA_DB_MAX_OVERFLOW" default:"10"`

	// DB connection resilience
	DBConnectAttempts     int  `envconfig:"ARENA_DB_CONNECT_ATTEMPTS" default:"10"`
	DBConnectRetrySeconds int  `envconfig:"ARENA_DB_CONNECT_RETRY_SECONDS" default:"3"`
	DBOptional            bool `envconfig:"ARENA_DB_OPTIONAL" default:"false"`

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

	// Anti-teaming
	AntiTeamRadius         float64 `envconfig:"ARENA_ANTI_TEAM_RADIUS" default:"30.0"`
	AntiTeamThresholdTicks int     `envconfig:"ARENA_ANTI_TEAM_THRESHOLD_TICKS" default:"50"`
	AntiTeamDamagePerTick  float64 `envconfig:"ARENA_ANTI_TEAM_DAMAGE_PER_TICK" default:"2.0"`

	// Teleport Pads
	TeleportPadPairs       int `envconfig:"ARENA_TELEPORT_PAD_PAIRS" default:"3"`
	TeleportCooldownTicks  int `envconfig:"ARENA_TELEPORT_COOLDOWN_TICKS" default:"50"`
	TeleportCollectRadius  int `envconfig:"ARENA_TELEPORT_COLLECT_RADIUS" default:"1"`
	TeleportPadLockTicks   int `envconfig:"ARENA_TELEPORT_PAD_LOCK_TICKS" default:"30"`
	TeleportHazardGraceTicks int `envconfig:"ARENA_TELEPORT_HAZARD_GRACE_TICKS" default:"2"`

	// Capture Pad objective
	CapturePadCount            int     `envconfig:"ARENA_CAPTURE_PAD_COUNT" default:"1"`
	CapturePadRadius           int     `envconfig:"ARENA_CAPTURE_PAD_RADIUS" default:"2"`
	CapturePadCaptureTicks     int     `envconfig:"ARENA_CAPTURE_PAD_CAPTURE_TICKS" default:"20"`
	CapturePadCooldownTicks    int     `envconfig:"ARENA_CAPTURE_PAD_COOLDOWN_TICKS" default:"120"`
	CapturePadScoreBonus       int     `envconfig:"ARENA_CAPTURE_PAD_SCORE_BONUS" default:"12"`
	CapturePadShieldBonus      float64 `envconfig:"ARENA_CAPTURE_PAD_SHIELD_BONUS" default:"20"`
	CapturePadDamageBoostMult  float64 `envconfig:"ARENA_CAPTURE_PAD_DAMAGE_BOOST_MULT" default:"1.2"`
	CapturePadEffectTicks      int     `envconfig:"ARENA_CAPTURE_PAD_EFFECT_TICKS" default:"80"`
	CapturePadControlPulseTicks int    `envconfig:"ARENA_CAPTURE_PAD_CONTROL_PULSE_TICKS" default:"15"`
	CapturePadControlPulseScore int    `envconfig:"ARENA_CAPTURE_PAD_CONTROL_PULSE_SCORE" default:"2"`
	CapturePadControlPulseShield float64 `envconfig:"ARENA_CAPTURE_PAD_CONTROL_PULSE_SHIELD" default:"4"`

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

	// Occasional special round modifiers
	RoundModifierChance                 float64 `envconfig:"ARENA_ROUND_MODIFIER_CHANCE" default:"0.30"`
	RoundModifierFastZoneDelayMult      float64 `envconfig:"ARENA_ROUND_MOD_FAST_ZONE_DELAY_MULT" default:"0.55"`
	RoundModifierFastZoneIntervalMult   float64 `envconfig:"ARENA_ROUND_MOD_FAST_ZONE_INTERVAL_MULT" default:"0.65"`
	RoundModifierPickupSurgeIntervalMult float64 `envconfig:"ARENA_ROUND_MOD_PICKUP_SURGE_INTERVAL_MULT" default:"0.50"`
	RoundModifierDoubleBountyMult       float64 `envconfig:"ARENA_ROUND_MOD_DOUBLE_BOUNTY_MULT" default:"2.0"`
	RoundModifierTeleportCooldownMult   float64 `envconfig:"ARENA_ROUND_MOD_TELEPORT_COOLDOWN_MULT" default:"0.45"`
	RoundModifierTeleportLockMult       float64 `envconfig:"ARENA_ROUND_MOD_TELEPORT_LOCK_MULT" default:"0.55"`
	RoundModifierHazardStormOnMult      float64 `envconfig:"ARENA_ROUND_MOD_HAZARD_STORM_ON_MULT" default:"1.20"`
	RoundModifierHazardStormOffMult     float64 `envconfig:"ARENA_ROUND_MOD_HAZARD_STORM_OFF_MULT" default:"0.45"`
	RoundModifierHazardStormDamageMult  float64 `envconfig:"ARENA_ROUND_MOD_HAZARD_STORM_DAMAGE_MULT" default:"1.35"`

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
	warnInsecureDefaults()
}

// warnInsecureDefaults logs loud (but non-fatal) warnings when the server is
// running with default/weak credentials. It never blocks startup — operators
// should be nudged to fix these, not locked out by a config validation bug.
func warnInsecureDefaults() {
	if C.DBPassword == "arena" {
		slog.Warn("SECURITY: ARENA_DB_PASSWORD is set to the insecure default " +
			"value — set a strong, unique password before exposing this server")
	}
	if C.AdminToken == "" {
		if C.AdminLocalhostBypass {
			slog.Warn("SECURITY: ARENA_ADMIN_TOKEN is not set; admin API is only " +
				"reachable via the ARENA_ADMIN_LOCALHOST_BYPASS loopback path (no " +
				"remote admin auth configured)")
		} else {
			slog.Warn("SECURITY: ARENA_ADMIN_TOKEN is not set and " +
				"ARENA_ADMIN_LOCALHOST_BYPASS is disabled — the admin API cannot " +
				"be authenticated at all unless OIDC or a DB-issued token is configured")
		}
	}
}
