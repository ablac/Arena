"""FastAPI application entry point for the AI Battle Arena."""

import asyncio
from contextlib import asynccontextmanager
from collections.abc import AsyncGenerator
from typing import Any

from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware
from fastapi.staticfiles import StaticFiles

from server.config import settings
from server.db.connection import init_db
from server.api.keys import router as keys_router
from server.api.bot import router as bot_router
from server.api.leaderboard import router as leaderboard_router
from server.api.arena import router as arena_router, set_engine as set_arena_engine
from server.game.engine import GameEngine
from server.ws.bot_handler import router as ws_bot_router, set_engine as set_bot_engine
from server.ws.spectator import router as ws_spectator_router, set_engine as set_spec_engine

# Global game engine instance
engine = GameEngine()


@asynccontextmanager
async def lifespan(app: FastAPI) -> AsyncGenerator[None, None]:
    """Initialize database and start game engine on startup."""
    await init_db()
    set_bot_engine(engine)
    set_spec_engine(engine)
    set_arena_engine(engine)
    engine_task = asyncio.create_task(engine.run())
    yield
    engine.running = False
    engine_task.cancel()


app = FastAPI(
    title="AI Battle Arena",
    version="0.1.0",
    lifespan=lifespan,
)

# CORS middleware
origins = [o.strip() for o in settings.app.cors_origins.split(",")]
app.add_middleware(
    CORSMiddleware,
    allow_origins=origins,
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)

# API routers
app.include_router(keys_router)
app.include_router(bot_router)
app.include_router(leaderboard_router)
app.include_router(arena_router)

# WebSocket routers
app.include_router(ws_bot_router)
app.include_router(ws_spectator_router)


@app.get("/api/v1/health")
async def health_check() -> dict[str, Any]:
    """Health check endpoint."""
    return {"status": "ok", "bots_online": len(engine.bots)}


# Mount frontend — must be last (catch-all for non-API routes)
app.mount("/", StaticFiles(directory="frontend", html=True), name="frontend")
