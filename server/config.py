"""Master configuration — all configurable values live here.

Uses pydantic-settings to load from environment variables with ARENA_ prefix.
"""

from pydantic import Field
from pydantic_settings import BaseSettings, SettingsConfigDict


class DatabaseSettings(BaseSettings):
    """PostgreSQL connection settings."""
    model_config = SettingsConfigDict(env_prefix="ARENA_DB_")
    host: str = Field(default="localhost", description="Database hostname")
    port: int = Field(default=5432, description="Database port")
    name: str = Field(default="arena", description="Database name")
    user: str = Field(default="arena_user", description="Database user")
    password: str = Field(default="changeme", description="Database password")

    @property
    def url(self) -> str:
        """Async SQLAlchemy connection URL."""
        return f"postgresql+asyncpg://{self.user}:{self.password}@{self.host}:{self.port}/{self.name}"


class RedisSettings(BaseSettings):
    """Redis connection settings."""
    model_config = SettingsConfigDict(env_prefix="ARENA_REDIS_")
    host: str = Field(default="localhost", description="Redis hostname")
    port: int = Field(default=6379, description="Redis port")

    @property
    def url(self) -> str:
        """Redis connection URL."""
        return f"redis://{self.host}:{self.port}"


class GameSettings(BaseSettings):
    """Core game loop and arena settings."""
    model_config = SettingsConfigDict(env_prefix="ARENA_GAME_")
    tick_rate: int = Field(default=10, description="Game ticks per second")
    max_bots: int = Field(default=500, description="Max bots allowed")
    max_spectators: int = Field(default=500, description="Max spectators")
    view_radius: int = Field(default=100, description="Bot view radius in arena units")
    arena_width: int = Field(default=2000, description="Arena width")
    arena_height: int = Field(default=2000, description="Arena height")
    spatial_cell_size: int = Field(default=100, description="Spatial grid cell size")
    pathfinding_cell_size: int = Field(default=20, description="A* pathfinding grid cell size")


class CombatSettings(BaseSettings):
    """Combat balance and stat allocation settings."""
    model_config = SettingsConfigDict(env_prefix="ARENA_COMBAT_")
    stat_budget: int = Field(default=20, description="Total stat points per bot")
    stat_min: int = Field(default=1, description="Minimum stat value")
    stat_max: int = Field(default=10, description="Maximum stat value")
    respawn_time: int = Field(default=5, description="Respawn delay in seconds")
    round_duration: int = Field(default=600, description="Round duration in seconds")
    intermission_time: int = Field(default=10, description="Intermission between rounds")
    lobby_countdown: int = Field(default=10, description="Lobby countdown before round starts")
    min_bots_to_start: int = Field(default=2, description="Minimum bots needed to start a round")
    dodge_speed_mult: float = Field(default=2.0, description="Dodge speed multiplier")
    dodge_invuln_ticks: int = Field(default=3, description="Invulnerability ticks on dodge")
    dodge_cooldown_ticks: int = Field(default=30, description="Dodge cooldown in ticks")
    knockback_wall_damage: int = Field(default=5, description="Bonus damage on wall knockback")
    projectile_speed: float = Field(default=30.0, description="Arrow travel speed units/sec")
    projectile_hit_radius: float = Field(default=1.0, description="Projectile hit radius")
    projectile_max_age_secs: float = Field(default=1.0, description="Max projectile lifetime")
    staff_delay_ticks: int = Field(default=2, description="Staff area attack delay ticks")
    stun_duration_ticks: int = Field(default=1, description="Shield bash stun duration")


class EloSettings(BaseSettings):
    """ELO rating system settings."""
    model_config = SettingsConfigDict(env_prefix="ARENA_ELO_")
    k_factor: int = Field(default=32, description="ELO K-factor")
    starting_elo: int = Field(default=1000, description="Starting ELO for new bots")
    min_elo: int = Field(default=100, description="Minimum ELO floor")


class WeaponConfig(BaseSettings):
    """Stats for a single weapon type."""
    damage: int = Field(default=0, description="Base damage")
    range: float = Field(default=0.0, description="Attack range in arena units")
    cooldown: float = Field(default=0.0, description="Cooldown between attacks in seconds")
    special: str = Field(default="", description="Special ability name")
    special_param: float = Field(default=0.0, description="Special ability parameter")


class WeaponSettings(BaseSettings):
    """All weapon type configurations."""
    model_config = SettingsConfigDict(env_prefix="ARENA_WEAPON_")
    sword: WeaponConfig = Field(default_factory=lambda: WeaponConfig(
        damage=25, range=2.0, cooldown=0.5, special="cleave"))
    bow: WeaponConfig = Field(default_factory=lambda: WeaponConfig(
        damage=15, range=20.0, cooldown=1.0, special="projectile"))
    daggers: WeaponConfig = Field(default_factory=lambda: WeaponConfig(
        damage=12, range=1.5, cooldown=0.3, special="double_strike", special_param=0.2))
    shield: WeaponConfig = Field(default_factory=lambda: WeaponConfig(
        damage=8, range=1.5, cooldown=0.8, special="block", special_param=0.5))
    spear: WeaponConfig = Field(default_factory=lambda: WeaponConfig(
        damage=20, range=3.0, cooldown=0.7, special="knockback", special_param=2.0))
    staff: WeaponConfig = Field(default_factory=lambda: WeaponConfig(
        damage=18, range=15.0, cooldown=1.2, special="area", special_param=3.0))


class ArenaZoneSettings(BaseSettings):
    """Shrinking safe zone settings."""

    model_config = SettingsConfigDict(env_prefix="ARENA_ZONE_")
    initial_radius: float = Field(default=1000.0, description="Starting safe zone radius")
    center_x: float = Field(default=1000.0, description="Safe zone center X")
    center_y: float = Field(default=1000.0, description="Safe zone center Y")
    shrink_percent: float = Field(default=0.10, description="Shrink fraction each interval")
    shrink_interval_secs: int = Field(default=60, description="Seconds between shrinks")
    damage_per_tick: int = Field(default=2, description="HP damage per tick outside zone")
    min_radius: float = Field(default=200.0, description="Minimum safe zone radius")
    obstacle_count_min: int = Field(default=20, description="Min obstacles per round")
    obstacle_count_max: int = Field(default=30, description="Max obstacles per round")


class PickupSettings(BaseSettings):
    """Item pickup configuration."""

    model_config = SettingsConfigDict(env_prefix="ARENA_PICKUP_")
    spawn_interval_ticks: int = Field(default=50, description="Ticks between pickup spawns")
    max_active: int = Field(default=20, description="Max pickups on map")
    health_amount: int = Field(default=30, description="HP restored by health pickup")
    speed_boost_mult: float = Field(default=2.0, description="Speed boost multiplier")
    speed_boost_ticks: int = Field(default=50, description="Speed boost duration ticks")
    damage_boost_mult: float = Field(default=1.5, description="Damage boost multiplier")
    damage_boost_ticks: int = Field(default=50, description="Damage boost duration ticks")
    shield_bubble_hp: int = Field(default=50, description="Shield bubble absorb HP")
    collect_radius: float = Field(default=2.0, description="Auto-collect radius")


class NetworkSettings(BaseSettings):
    """WebSocket and connection settings."""

    model_config = SettingsConfigDict(env_prefix="ARENA_NET_")
    persist_interval_secs: int = Field(default=30, description="Stat persist to DB interval")
    kill_feed_size: int = Field(default=20, description="Kill feed max entries")
    ws_message_max_bytes: int = Field(default=1024, description="Max WS message size")
    ws_max_messages_per_sec: int = Field(default=12, description="Max WS msgs/sec")
    connection_timeout: int = Field(default=10, description="Connection timeout secs")
    heartbeat_interval: int = Field(default=30, description="Heartbeat interval secs")
    ws_connect_rate_per_min: int = Field(default=3, description="Max WS connects per key/min")
    loadout_timeout_secs: int = Field(default=10, description="Loadout selection timeout")
    spectator_broadcast_interval: int = Field(default=2, description="Spectator update ticks")
    afk_timeout_ticks: int = Field(default=30, description="Ticks before AFK kick")


class SecuritySettings(BaseSettings):
    """Authentication and rate-limiting settings."""

    model_config = SettingsConfigDict(env_prefix="ARENA_SEC_")
    api_key_prefix: str = Field(default="arena_", description="API key prefix")
    bcrypt_rounds: int = Field(default=12, description="bcrypt cost factor")
    rate_limit_rpm: int = Field(default=60, description="Requests per minute per IP")
    rate_limit_register_per_hour: int = Field(default=5, description="Registrations/hour/IP")


class FrontendSettings(BaseSettings):
    """Frontend display settings."""

    model_config = SettingsConfigDict(env_prefix="ARENA_UI_")
    bg_color: str = Field(default="#0a0e17", description="Background color")
    bg_secondary: str = Field(default="#111827", description="Secondary background")
    accent_blue: str = Field(default="#00d4ff", description="Electric blue accent")
    accent_red: str = Field(default="#ff3333", description="Blood red accent")
    accent_gold: str = Field(default="#ffd700", description="Gold accent")
    text_color: str = Field(default="#e2e8f0", description="Primary text color")
    grid_color: str = Field(default="#1a1a2e", description="Grid line color")
    font_family: str = Field(default="monospace", description="UI font family")
    minimap_size: int = Field(default=150, description="Minimap size in pixels")


class AppSettings(BaseSettings):
    """Top-level application settings."""

    model_config = SettingsConfigDict(env_prefix="ARENA_")
    cors_origins: str = Field(default="*,https://angel-serv.com", description="CORS origins")
    admin_key: str = Field(default="changeme_admin_key", description="Admin API key")
    secret_key: str = Field(default="changeme_secret_key", description="App secret key")


class Settings:
    """Aggregated settings — single point of access for all config."""

    def __init__(self) -> None:
        self.db = DatabaseSettings()
        self.redis = RedisSettings()
        self.game = GameSettings()
        self.combat = CombatSettings()
        self.elo = EloSettings()
        self.weapons = WeaponSettings()
        self.arena_zone = ArenaZoneSettings()
        self.pickups = PickupSettings()
        self.network = NetworkSettings()
        self.security = SecuritySettings()
        self.frontend = FrontendSettings()
        self.app = AppSettings()


# Singleton instance — import this everywhere
settings = Settings()
