"""Demo bots that keep the arena populated for spectators."""

import asyncio
import json
import logging
import os

import aiohttp
import websockets

from server.demo_bot_ai import DEMO_CONFIGS, pick_action

logger = logging.getLogger(__name__)


def should_run_demo_bots() -> bool:
    """Check if demo bots should be started (from env var)."""
    return os.getenv("ARENA_DEMO_BOTS", "false").lower() in ("true", "1", "yes")


async def _register_bot(session: aiohttp.ClientSession, base_url: str) -> str | None:
    """Register a demo bot via REST API and return its API key."""
    try:
        async with session.post(f"{base_url}/api/v1/keys/generate") as resp:
            if resp.status == 200:
                data = await resp.json()
                api_key = data.get("api_key")
                logger.info("Registered demo bot (key prefix: %s...)", api_key[:12])
                return api_key
            logger.error("Failed to register demo bot: HTTP %d", resp.status)
    except Exception as exc:
        logger.error("Registration error: %s", exc)
    return None


async def _run_demo_bot(config: dict, api_key: str, ws_url: str) -> None:
    """Run a single demo bot with simple AI logic."""
    name = config["name"]
    strategy = config["strategy"]
    weapon = config["weapon"]
    stats = config["stats"]
    fallback = strategy if strategy in ("aggressive", "defensive", "territorial") else "aggressive"
    backoff = 1.0

    while True:
        try:
            url = f"{ws_url}?key={api_key}"
            async with websockets.connect(url) as ws:
                raw = await ws.recv()
                msg = json.loads(raw)
                if msg.get("type") != "connected":
                    logger.warning("[%s] Expected 'connected', got '%s'", name, msg.get("type"))
                    return

                await ws.send(json.dumps({
                    "type": "select_loadout",
                    "weapon": weapon,
                    "stats": stats,
                    "fallback_behavior": fallback,
                }))

                raw = await ws.recv()
                msg = json.loads(raw)
                if msg.get("type") != "loadout_confirmed":
                    logger.warning("[%s] Expected 'loadout_confirmed', got '%s'", name, msg.get("type"))

                logger.info("[%s] Entered arena with %s/%s", name, weapon, strategy)
                backoff = 1.0

                while True:
                    raw = await ws.recv()
                    msg = json.loads(raw)
                    msg_type = msg.get("type")

                    if msg_type == "tick":
                        action = pick_action(
                            strategy, msg.get("your_state", {}),
                            msg.get("nearby_entities", []),
                            msg.get("safe_zone", {}), weapon,
                        )
                        payload = {"type": "action", "tick": msg.get("tick_number", 0)}
                        payload.update(action)
                        await ws.send(json.dumps(payload))
                    elif msg_type == "kick":
                        logger.warning("[%s] Kicked: %s", name, msg.get("reason"))
                        return

        except websockets.ConnectionClosed:
            logger.info("[%s] Disconnected, reconnecting in %.0fs", name, backoff)
        except Exception as exc:
            logger.warning("[%s] Error: %s, reconnecting in %.0fs", name, exc, backoff)

        await asyncio.sleep(backoff)
        backoff = min(backoff * 2, 30.0)


async def start_demo_bots(
    base_url: str = "http://localhost:8000",
    ws_url: str = "ws://localhost:8000/ws/bot",
) -> None:
    """Register and start all demo bots. Runs until cancelled."""
    logger.info("Starting %d demo bots...", len(DEMO_CONFIGS))
    await asyncio.sleep(2)

    tasks: list = []
    async with aiohttp.ClientSession() as session:
        for config in DEMO_CONFIGS:
            api_key = await _register_bot(session, base_url)
            if api_key:
                tasks.append(_run_demo_bot(config, api_key, ws_url))
            else:
                logger.error("Skipping %s — registration failed", config["name"])

    if not tasks:
        logger.error("No demo bots registered, aborting")
        return

    logger.info("All %d demo bots registered, entering arena", len(tasks))
    await asyncio.gather(*tasks)
