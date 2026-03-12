"""API key generation, verification, and FastAPI authentication dependency."""

import secrets
from datetime import datetime, timezone

import bcrypt
from fastapi import Depends, Header, HTTPException
from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession
from sqlalchemy.orm import selectinload

from server.config import settings
from server.db.connection import get_db
from server.db.models import ApiKey, Bot

# Characters used for base62 encoding of random bytes.
_BASE62_CHARS: str = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"


def _bytes_to_base62(data: bytes) -> str:
    """Encode raw bytes as a base62 string."""
    num = int.from_bytes(data, byteorder="big")
    if num == 0:
        return _BASE62_CHARS[0]
    chars: list[str] = []
    while num > 0:
        num, remainder = divmod(num, 62)
        chars.append(_BASE62_CHARS[remainder])
    return "".join(reversed(chars))


def generate_api_key() -> tuple[str, str, str]:
    """Generate a new API key.

    Returns:
        A tuple of (raw_key, key_hash, key_prefix).
        - raw_key: the full key shown to the user once.
        - key_hash: bcrypt hash stored in the database.
        - key_prefix: first 12 characters, used for fast DB lookup.
    """
    random_part: str = _bytes_to_base62(secrets.token_bytes(32))
    raw_key: str = settings.security.api_key_prefix + random_part

    hashed: bytes = bcrypt.hashpw(
        raw_key.encode(), bcrypt.gensalt(rounds=settings.security.bcrypt_rounds)
    )
    key_hash: str = hashed.decode()
    key_prefix: str = raw_key[:12]

    return raw_key, key_hash, key_prefix


def verify_api_key(raw_key: str, key_hash: str) -> bool:
    """Verify a raw API key against a stored bcrypt hash.

    Args:
        raw_key: The plaintext API key provided by the client.
        key_hash: The bcrypt hash stored in the database.

    Returns:
        True if the key matches, False otherwise.
    """
    try:
        return bcrypt.checkpw(raw_key.encode(), key_hash.encode())
    except Exception:
        return False


async def get_bot_by_key(db: AsyncSession, raw_key: str) -> Bot | None:
    """Look up a bot by its raw API key.

    Performs a prefix-based lookup, verifies the hash, updates last_seen,
    and returns the first bot associated with the key.

    Args:
        db: An async database session.
        raw_key: The plaintext API key from the request header.

    Returns:
        The Bot instance, or None if authentication fails.
    """
    if len(raw_key) < 12:
        return None

    prefix: str = raw_key[:12]

    stmt = (
        select(ApiKey)
        .options(selectinload(ApiKey.bots))
        .where(ApiKey.key_prefix == prefix, ApiKey.is_active.is_(True))
    )
    result = await db.execute(stmt)
    api_key: ApiKey | None = result.scalar_one_or_none()

    if api_key is None:
        return None

    if not verify_api_key(raw_key, api_key.key_hash):
        return None

    # Update last_seen within the current transaction.
    api_key.last_seen = datetime.now(timezone.utc)
    await db.flush()

    # Return the first associated bot, if any.
    if not api_key.bots:
        return None
    return api_key.bots[0]


async def api_key_dependency(
    x_arena_key: str = Header(..., alias="X-Arena-Key"),
    db: AsyncSession = Depends(get_db),
) -> Bot:
    """FastAPI dependency that authenticates requests via the X-Arena-Key header.

    Raises:
        HTTPException: 401 if the key is missing, invalid, or has no bot.

    Returns:
        The authenticated Bot instance.
    """
    bot: Bot | None = await get_bot_by_key(db, x_arena_key)
    if bot is None:
        raise HTTPException(status_code=401, detail="Invalid or inactive API key.")
    return bot
