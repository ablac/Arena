"""API key generation and revocation endpoints."""

from fastapi import APIRouter, Depends, Request
from sqlalchemy.ext.asyncio import AsyncSession

from server.config import settings
from server.db.connection import get_db
from server.db.models import ApiKey, Bot, BotStats
from server.security.auth import api_key_dependency, generate_api_key
from server.security.rate_limiter import rate_limit_api_calls, rate_limit_key_generation

from .schemas import KeyGenerateResponse, KeyRevokeResponse

router = APIRouter(prefix="/api/v1/keys", tags=["keys"])


@router.post("/generate", response_model=KeyGenerateResponse)
async def generate_key(
    request: Request,
    db: AsyncSession = Depends(get_db),
    _rl: None = Depends(rate_limit_key_generation),
) -> KeyGenerateResponse:
    """Generate a new API key, bot, and stats record."""
    client_ip = request.client.host if request.client else None

    raw_key, key_hash, key_prefix = generate_api_key()

    api_key = ApiKey(
        key_hash=key_hash,
        key_prefix=key_prefix,
        ip_created=client_ip,
    )
    db.add(api_key)
    await db.flush()

    bot = Bot(api_key_id=api_key.id)
    db.add(bot)
    await db.flush()

    bot_stats = BotStats(
        bot_id=bot.id,
        elo=settings.elo.starting_elo,
    )
    db.add(bot_stats)

    await db.commit()
    await db.refresh(api_key)
    await db.refresh(bot)
    await db.refresh(bot_stats)

    return KeyGenerateResponse(
        api_key=raw_key,
        bot_id=str(bot.id),
        created_at=api_key.created_at,
        message="Save this key! It cannot be recovered.",
    )


@router.delete("/revoke", response_model=KeyRevokeResponse)
async def revoke_key(
    bot: Bot = Depends(api_key_dependency),
    db: AsyncSession = Depends(get_db),
    _rl: None = Depends(rate_limit_api_calls),
) -> KeyRevokeResponse:
    """Revoke the API key associated with the authenticated bot."""
    api_key = await db.get(ApiKey, bot.api_key_id)
    if api_key:
        api_key.is_active = False
        await db.commit()

    return KeyRevokeResponse(message="API key revoked successfully.")
