"""SQLAlchemy 2.0 ORM models for the AI Battle Arena."""

import uuid
from datetime import datetime

from sqlalchemy import (
    BigInteger,
    Boolean,
    DateTime,
    Float,
    ForeignKey,
    Integer,
    String,
    func,
)
from sqlalchemy.dialects.postgresql import JSON, UUID
from sqlalchemy.orm import DeclarativeBase, Mapped, mapped_column, relationship


class Base(DeclarativeBase):
    """Base class for all ORM models."""

    pass


class ApiKey(Base):
    """Registered API keys for bot authentication."""

    __tablename__ = "api_keys"

    id: Mapped[uuid.UUID] = mapped_column(
        UUID(as_uuid=True), primary_key=True, default=uuid.uuid4
    )
    key_hash: Mapped[str] = mapped_column(String(255), unique=True, nullable=False)
    key_prefix: Mapped[str] = mapped_column(String(12), nullable=False)
    created_at: Mapped[datetime] = mapped_column(
        DateTime(timezone=True), server_default=func.now()
    )
    last_seen: Mapped[datetime | None] = mapped_column(
        DateTime(timezone=True), nullable=True
    )
    is_active: Mapped[bool] = mapped_column(Boolean, default=True)
    ip_created: Mapped[str | None] = mapped_column(String(45), nullable=True)

    bots: Mapped[list["Bot"]] = relationship(
        back_populates="api_key", cascade="all, delete-orphan"
    )


class Bot(Base):
    """A registered bot in the arena."""

    __tablename__ = "bots"

    id: Mapped[uuid.UUID] = mapped_column(
        UUID(as_uuid=True), primary_key=True, default=uuid.uuid4
    )
    api_key_id: Mapped[uuid.UUID] = mapped_column(
        UUID(as_uuid=True),
        ForeignKey("api_keys.id", ondelete="CASCADE"),
        nullable=False,
    )
    name: Mapped[str] = mapped_column(String(20), default="Unnamed Bot")
    avatar_color: Mapped[str] = mapped_column(String(7), default="#FFFFFF")
    default_weapon: Mapped[str] = mapped_column(String(20), default="sword")
    default_stats: Mapped[dict] = mapped_column(
        JSON,
        default=lambda: {"hp": 5, "speed": 5, "attack": 5, "defense": 5},
    )
    default_fallback: Mapped[str] = mapped_column(String(20), default="aggressive")
    created_at: Mapped[datetime] = mapped_column(
        DateTime(timezone=True), server_default=func.now()
    )
    updated_at: Mapped[datetime | None] = mapped_column(
        DateTime(timezone=True), onupdate=func.now(), nullable=True
    )

    api_key: Mapped["ApiKey"] = relationship(back_populates="bots")
    stats: Mapped["BotStats"] = relationship(
        back_populates="bot", uselist=False, cascade="all, delete-orphan"
    )
    kills_dealt: Mapped[list["KillLog"]] = relationship(
        back_populates="killer", foreign_keys="KillLog.killer_id"
    )
    deaths_received: Mapped[list["KillLog"]] = relationship(
        back_populates="victim", foreign_keys="KillLog.victim_id"
    )


class BotStats(Base):
    """Lifetime statistics for a bot."""

    __tablename__ = "bot_stats"

    bot_id: Mapped[uuid.UUID] = mapped_column(
        UUID(as_uuid=True),
        ForeignKey("bots.id", ondelete="CASCADE"),
        primary_key=True,
    )
    kills: Mapped[int] = mapped_column(Integer, default=0)
    deaths: Mapped[int] = mapped_column(Integer, default=0)
    assists: Mapped[int] = mapped_column(Integer, default=0)
    damage_dealt: Mapped[int] = mapped_column(BigInteger, default=0)
    damage_taken: Mapped[int] = mapped_column(BigInteger, default=0)
    current_streak: Mapped[int] = mapped_column(Integer, default=0)
    best_streak: Mapped[int] = mapped_column(Integer, default=0)
    elo: Mapped[int] = mapped_column(Integer, default=1000)
    time_alive_seconds: Mapped[int] = mapped_column(BigInteger, default=0)
    longest_life_secs: Mapped[int] = mapped_column(Integer, default=0)
    rounds_played: Mapped[int] = mapped_column(Integer, default=0)
    round_wins: Mapped[int] = mapped_column(Integer, default=0)
    pickups_collected: Mapped[int] = mapped_column(Integer, default=0)
    distance_traveled: Mapped[float] = mapped_column(Float, default=0.0)
    updated_at: Mapped[datetime | None] = mapped_column(
        DateTime(timezone=True), onupdate=func.now(), nullable=True
    )

    bot: Mapped["Bot"] = relationship(back_populates="stats")


class KillLog(Base):
    """Record of each kill event."""

    __tablename__ = "kill_log"

    id: Mapped[uuid.UUID] = mapped_column(
        UUID(as_uuid=True), primary_key=True, default=uuid.uuid4
    )
    round_id: Mapped[uuid.UUID | None] = mapped_column(
        UUID(as_uuid=True), ForeignKey("rounds.id"), nullable=True
    )
    killer_id: Mapped[uuid.UUID] = mapped_column(
        UUID(as_uuid=True), ForeignKey("bots.id"), nullable=False
    )
    victim_id: Mapped[uuid.UUID] = mapped_column(
        UUID(as_uuid=True), ForeignKey("bots.id"), nullable=False
    )
    weapon: Mapped[str] = mapped_column(String(20), nullable=False)
    damage: Mapped[int] = mapped_column(Integer, nullable=False)
    killer_hp: Mapped[int] = mapped_column(Integer, nullable=False)
    tick: Mapped[int] = mapped_column(Integer, nullable=False)
    created_at: Mapped[datetime] = mapped_column(
        DateTime(timezone=True), server_default=func.now()
    )

    round: Mapped["Round | None"] = relationship(back_populates="kills")
    killer: Mapped["Bot"] = relationship(
        back_populates="kills_dealt", foreign_keys=[killer_id]
    )
    victim: Mapped["Bot"] = relationship(
        back_populates="deaths_received", foreign_keys=[victim_id]
    )


class Round(Base):
    """A single game round."""

    __tablename__ = "rounds"

    id: Mapped[uuid.UUID] = mapped_column(
        UUID(as_uuid=True), primary_key=True, default=uuid.uuid4
    )
    round_number: Mapped[int] = mapped_column(Integer, nullable=False)
    started_at: Mapped[datetime] = mapped_column(
        DateTime(timezone=True), server_default=func.now()
    )
    ended_at: Mapped[datetime | None] = mapped_column(
        DateTime(timezone=True), nullable=True
    )
    bots_participated: Mapped[int] = mapped_column(Integer, default=0)
    mvp_bot_id: Mapped[uuid.UUID | None] = mapped_column(
        UUID(as_uuid=True), ForeignKey("bots.id"), nullable=True
    )
    status: Mapped[str] = mapped_column(String(20), default="active")

    kills: Mapped[list["KillLog"]] = relationship(back_populates="round")


class RateLimit(Base):
    """Track API key registration rate limiting per IP."""

    __tablename__ = "rate_limits"

    ip_address: Mapped[str] = mapped_column(String(45), primary_key=True)
    keys_generated: Mapped[int] = mapped_column(Integer, default=0)
    window_start: Mapped[datetime] = mapped_column(
        DateTime(timezone=True), server_default=func.now()
    )
