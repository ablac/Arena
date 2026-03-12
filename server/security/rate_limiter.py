"""Redis-backed rate limiting with FastAPI dependencies."""

import time

import redis.asyncio as aioredis
from fastapi import HTTPException, Request

from server.config import settings

# Lazily initialised Redis connection pool.
_redis_pool: aioredis.Redis | None = None


async def get_redis() -> aioredis.Redis:
    """Return the shared Redis connection, creating it on first call.

    Returns:
        An async Redis client connected to the configured URL.
    """
    global _redis_pool
    if _redis_pool is None:
        _redis_pool = aioredis.from_url(
            settings.redis.url, decode_responses=True
        )
    return _redis_pool


async def check_rate_limit(
    key: str, limit: int, window_seconds: int
) -> tuple[bool, int, int]:
    """Check and increment a rate limit counter in Redis.

    Uses the INCR + EXPIRE pattern: on the first request in a window the
    counter is created and given a TTL equal to *window_seconds*.

    Args:
        key: Logical identifier (e.g. ``"keygen:127.0.0.1"``).
        limit: Maximum number of requests allowed within the window.
        window_seconds: Length of the sliding window in seconds.

    Returns:
        A tuple of (allowed, remaining, reset_at) where:
        - allowed: True if the request is within the limit.
        - remaining: How many requests are left in this window.
        - reset_at: Unix timestamp when the window resets.
    """
    redis = await get_redis()
    redis_key: str = f"ratelimit:{key}"

    count: int = await redis.incr(redis_key)

    if count == 1:
        await redis.expire(redis_key, window_seconds)

    ttl: int = await redis.ttl(redis_key)
    if ttl < 0:
        ttl = window_seconds

    reset_at: int = int(time.time()) + ttl
    remaining: int = max(0, limit - count)
    allowed: bool = count <= limit

    return allowed, remaining, reset_at


async def rate_limit_key_generation(request: Request) -> None:
    """FastAPI dependency — rate-limit API key registration.

    Allows ``settings.security.rate_limit_register_per_hour`` requests per
    hour per client IP.

    Raises:
        HTTPException: 429 if the limit is exceeded.
    """
    client_ip: str = request.client.host if request.client else "unknown"
    allowed, remaining, reset_at = await check_rate_limit(
        key=f"keygen:{client_ip}",
        limit=settings.security.rate_limit_register_per_hour,
        window_seconds=3600,
    )
    if not allowed:
        raise HTTPException(
            status_code=429,
            detail=(
                f"Registration rate limit exceeded. "
                f"Remaining: {remaining}. Resets at: {reset_at}."
            ),
        )


async def rate_limit_api_calls(request: Request) -> None:
    """FastAPI dependency — rate-limit general API calls.

    Allows ``settings.security.rate_limit_rpm`` requests per minute per
    client IP.

    Raises:
        HTTPException: 429 if the limit is exceeded.
    """
    client_ip: str = request.client.host if request.client else "unknown"
    allowed, remaining, reset_at = await check_rate_limit(
        key=f"api:{client_ip}",
        limit=settings.security.rate_limit_rpm,
        window_seconds=60,
    )
    if not allowed:
        raise HTTPException(
            status_code=429,
            detail=(
                f"API rate limit exceeded. "
                f"Remaining: {remaining}. Resets at: {reset_at}."
            ),
        )


async def rate_limit_bot_config(request: Request) -> None:
    """FastAPI dependency — rate-limit bot configuration updates.

    Allows 10 configuration changes per minute per client IP.

    Raises:
        HTTPException: 429 if the limit is exceeded.
    """
    client_ip: str = request.client.host if request.client else "unknown"
    allowed, remaining, reset_at = await check_rate_limit(
        key=f"botcfg:{client_ip}",
        limit=10,
        window_seconds=60,
    )
    if not allowed:
        raise HTTPException(
            status_code=429,
            detail=(
                f"Bot config rate limit exceeded. "
                f"Remaining: {remaining}. Resets at: {reset_at}."
            ),
        )
