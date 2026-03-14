#!/usr/bin/env python3
"""
Arena Admin API — Python Example
Usage: python3 admin_example.py
"""
import json
import os
import requests

# ── Config ──────────────────────────────────────────────────
# Reads token from env var, or falls back to .env file
ADMIN_TOKEN = os.environ.get("ARENA_ADMIN_TOKEN")
if not ADMIN_TOKEN:
    env_path = os.path.join(os.path.dirname(__file__), "..", ".env")
    if os.path.exists(env_path):
        for line in open(env_path):
            if line.startswith("ARENA_ADMIN_TOKEN="):
                ADMIN_TOKEN = line.strip().split("=", 1)[1]

BASE_URL = os.environ.get("ARENA_URL", "http://localhost:8700")
ADMIN_URL = f"{BASE_URL}/api/v1/admin"

HEADERS = {
    "X-Admin-Token": ADMIN_TOKEN,
    "Content-Type": "application/json",
}


# ── Helper ──────────────────────────────────────────────────
def admin_get(endpoint):
    """GET an admin endpoint and return parsed JSON."""
    r = requests.get(f"{ADMIN_URL}/{endpoint}", headers=HEADERS)
    r.raise_for_status()
    return r.json()


def admin_post(endpoint, data=None):
    """POST to an admin endpoint and return parsed JSON."""
    r = requests.post(f"{ADMIN_URL}/{endpoint}", headers=HEADERS, json=data or {})
    r.raise_for_status()
    return r.json()


def pp(data):
    """Pretty print JSON."""
    print(json.dumps(data, indent=2))


# ── Examples ────────────────────────────────────────────────
if __name__ == "__main__":
    print("🏟️  Arena Admin API Examples\n")

    # 1. Deep health check
    print("━━━ Deep Health ━━━")
    pp(admin_get("health/deep"))

    # 2. Server metrics
    print("\n━━━ Server Metrics ━━━")
    pp(admin_get("debug/metrics"))

    # 3. List demo bots
    print("\n━━━ Demo Bots ━━━")
    pp(admin_get("demobots"))

    # 4. List all connected bots
    print("\n━━━ All Bots ━━━")
    pp(admin_get("bots"))

    # 5. Game state
    print("\n━━━ Game State ━━━")
    pp(admin_get("debug/game-state"))

    # ── Uncomment to try write operations ───────────────────
    #
    # # Stop all demo bots
    # print("\n━━━ Stopping Demo Bots ━━━")
    # pp(admin_post("demobots/stop"))
    #
    # # Start 5 demo bots
    # print("\n━━━ Starting 5 Demo Bots ━━━")
    # pp(admin_post("demobots/start", {"count": 5}))
    #
    # # Pause the game
    # pp(admin_post("game/pause"))
    #
    # # Resume the game
    # pp(admin_post("game/resume"))
    #
    # # Force new round
    # pp(admin_post("game/restart-round"))
    #
    # # Kick a bot by ID
    # pp(admin_post("bots/SOME-BOT-UUID/kick"))
    #
    # # Kill a bot in-game (set HP to 0)
    # pp(admin_post("bots/SOME-BOT-UUID/kill"))
    #
    # # Teleport a bot
    # pp(admin_post("bots/SOME-BOT-UUID/teleport", {"x": 500, "y": 500}))
    #
    # # Reset leaderboard
    # pp(admin_post("db/reset-leaderboard"))

    print("\n✅ Done!")
