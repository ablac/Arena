"""Pydantic request/response schemas for the AI Battle Arena API."""

from datetime import datetime
from enum import Enum

from pydantic import BaseModel, Field


class WeaponType(str, Enum):
    """Available weapon types for bot loadouts."""

    sword = "sword"
    bow = "bow"
    daggers = "daggers"
    shield = "shield"
    spear = "spear"
    staff = "staff"


class FallbackBehavior(str, Enum):
    """AI fallback behaviors when no explicit command is given."""

    aggressive = "aggressive"
    defensive = "defensive"
    opportunistic = "opportunistic"
    territorial = "territorial"
    hunter = "hunter"


class StatsAllocation(BaseModel):
    """Stat point allocation for a bot (must total 20)."""

    hp: int = Field(ge=1, le=10)
    speed: int = Field(ge=1, le=10)
    attack: int = Field(ge=1, le=10)
    defense: int = Field(ge=1, le=10)


class LoadoutConfig(BaseModel):
    """Full loadout configuration: weapon, stats, and fallback behavior."""

    weapon: WeaponType
    stats: StatsAllocation
    fallback_behavior: FallbackBehavior


class BotConfigRequest(BaseModel):
    """Request body for updating bot configuration."""

    name: str | None = None
    avatar_color: str | None = None
    default_loadout: LoadoutConfig | None = None


class BotConfigResponse(BaseModel):
    """Response after reading or updating bot configuration."""

    name: str
    avatar_color: str
    default_weapon: str
    default_stats: dict
    default_fallback: str
    updated_at: datetime | None


class KeyGenerateResponse(BaseModel):
    """Response after generating a new API key."""

    api_key: str
    bot_id: str
    created_at: datetime
    message: str


class KeyRevokeResponse(BaseModel):
    """Response after revoking an API key."""

    message: str


class BotStatsResponse(BaseModel):
    """Lifetime statistics for a single bot."""

    kills: int
    deaths: int
    kd_ratio: float
    assists: int
    damage_dealt: int
    damage_taken: int
    current_streak: int
    best_streak: int
    elo: int
    rank: int
    time_alive_seconds: int
    longest_life_secs: int
    rounds_played: int
    round_wins: int
    pickups_collected: int
    distance_traveled: float


class LeaderboardEntry(BaseModel):
    """A single row on the leaderboard."""

    rank: int
    name: str
    kills: int
    deaths: int
    kd_ratio: float
    elo: int
    best_streak: int


class LeaderboardResponse(BaseModel):
    """Paginated leaderboard with metadata."""

    entries: list[LeaderboardEntry]
    total_bots: int
    arena_status: str


class ArenaStatusResponse(BaseModel):
    """Current state of the arena."""

    status: str
    bots_connected: int
    bots_alive: int
    round_number: int
    round_time_remaining: int
    safe_zone_radius: float
    top_bot: str | None
