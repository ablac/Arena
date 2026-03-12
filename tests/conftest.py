"""Shared fixtures for API tests.

Environment variables MUST be set before any server imports so that
pydantic-settings picks up the test-appropriate values (Docker ports
exposed on localhost).
"""

import os

# Set env vars BEFORE importing anything from server.*
os.environ["ARENA_DB_HOST"] = "localhost"
os.environ["ARENA_DB_PORT"] = "5433"
os.environ["ARENA_DB_PASSWORD"] = "changeme_arena_2026"
os.environ["ARENA_REDIS_HOST"] = "localhost"
os.environ["ARENA_REDIS_PORT"] = "6380"

from collections.abc import AsyncGenerator  # noqa: E402

import pytest_asyncio  # noqa: E402
from httpx import ASGITransport, AsyncClient  # noqa: E402
from sqlalchemy.ext.asyncio import AsyncSession, async_sessionmaker, create_async_engine  # noqa: E402
from sqlalchemy.pool import NullPool  # noqa: E402

from server.config import settings  # noqa: E402
from server.db.connection import get_db  # noqa: E402
from server.main import app  # noqa: E402
from server.security.rate_limiter import (  # noqa: E402
    rate_limit_api_calls,
    rate_limit_bot_config,
    rate_limit_key_generation,
)

# NullPool avoids event-loop / connection reuse issues in tests.
engine = create_async_engine(settings.db.url, poolclass=NullPool)
test_session_factory = async_sessionmaker(
    engine, class_=AsyncSession, expire_on_commit=False
)


async def _override_get_db() -> AsyncGenerator[AsyncSession, None]:
    async with test_session_factory() as session:
        yield session


async def _noop_rate_limit() -> None:
    """No-op replacement for rate limit dependencies in tests."""
    pass


app.dependency_overrides[get_db] = _override_get_db
app.dependency_overrides[rate_limit_key_generation] = _noop_rate_limit
app.dependency_overrides[rate_limit_api_calls] = _noop_rate_limit
app.dependency_overrides[rate_limit_bot_config] = _noop_rate_limit


@pytest_asyncio.fixture
async def client() -> AsyncGenerator[AsyncClient, None]:
    """Async HTTP client wired to the FastAPI app."""
    transport = ASGITransport(app=app)
    async with AsyncClient(transport=transport, base_url="http://test") as ac:
        yield ac


@pytest_asyncio.fixture
async def api_key(client: AsyncClient) -> str:
    """Generate and return a fresh API key for authenticated tests."""
    resp = await client.post("/api/v1/keys/generate")
    assert resp.status_code == 200
    return resp.json()["api_key"]
