# AI Battle Arena

Multiplayer AI battle simulator where user-created bots compete in a real-time arena.

## Architecture

- **Backend:** Go 1.25 with chi/v5 router (`go-arena/`)
- **Frontend:** Vanilla JS + Babylon.js 3D renderer (`frontend/`)
- **Database:** PostgreSQL 16 (`arena-db:5432` in Docker, `localhost:5433` on host)
- **Cache/Rate Limiting:** Redis 7
- **SDKs:** Python (asyncio/websockets) and Node.js (ws) in `sdk/`
- **Infrastructure:** Docker Compose (3 services: db, redis, server)

## Project Layout

```
go-arena/
  cmd/arena-server/main.go     # Entry point
  internal/
    api/                        # REST endpoints, admin API, router
    config/                     # Env-based config (100+ params)
    db/                         # PostgreSQL queries & models
    game/                       # Core game engine, combat, pathfinding, weapons
    demobots/                   # Built-in demo bot AI (behavior trees)
    security/                   # Auth, rate limiting, input validation
    ws/                         # WebSocket handlers (bot + spectator)
frontend/
  js/renderer/                  # Babylon.js modules (engine, bots, weapons, etc.)
  js/                           # App boot, spectator WS, leaderboard
  dashboard/                    # Admin dashboard
sdk/
  python/arena_sdk/             # Python SDK (ArenaBot base class)
  nodejs/src/                   # Node.js SDK
bots/                           # User-created bots
arena                           # CLI management script (bash)
```

## Common Commands

```bash
# Docker (production)
docker compose up -d
docker compose down
docker compose build arena-server

# Arena CLI (/opt/ai-battle-arena/arena)
arena start                     # Start all containers
arena stop                      # Stop everything
arena restart                   # Full rebuild + restart
arena status                    # Health & running bots
arena docker-restart            # Rebuild server only
arena logs [n]                  # Tail server logs
arena bots                      # List running bots
arena bot-start <name>          # Start a bot
arena bot-stop <name>           # Stop a bot
arena admin <endpoint> [args]   # Admin API calls

# Go server (standalone dev)
cd go-arena && go build -o arena-server ./cmd/arena-server

# SDK install
cd sdk/python && pip install -e .
cd sdk/nodejs && npm install
```

## Git Workflow

- **Branches:** `develop` (pre-release), `main` (production)
- **Naming:** `feature/*`, `fix/*`, `docs/*`, `refactor/*`
- **Commits:** Conventional (`feat:`, `fix:`, `docs:`, `refactor:`)
- **Process:** Feature branch -> PR to `develop` -> squash-merge -> `develop` merges to `main` for release

## Key Modification Points

| Task | Where |
|------|-------|
| New API endpoint | `go-arena/internal/api/router.go` + new handler |
| Game mechanics | `go-arena/internal/game/` (combat.go, weapons.go, movement.go) |
| Balance tuning | `go-arena/internal/config/config.go` or admin API at runtime |
| Bot SDK features | `sdk/python/arena_sdk/bot.py` / `sdk/nodejs/src/ArenaBot.js` |
| 3D rendering | `frontend/js/renderer/*.js` |
| Admin features | `go-arena/internal/api/admin.go` |
| Demo bot AI | `go-arena/internal/demobots/ai.go` |

## Game Mechanics Quick Ref

- **6 weapons:** Sword, Bow, Daggers, Shield, Spear, Staff
- **Stats:** 20-point budget across HP/Speed/Attack/Defense (1-10 each)
- **Pickups:** Health, Damage Boost, Speed Boost, Shield Bubble
- **Safe zone:** Shrinks 15% every 20s, 3 dmg/tick outside
- **Tick rate:** 10 ticks/sec (configurable)
- **Round:** 240s default, min 2 bots to start

## Protocols

- **Bot WebSocket:** `ws://host:8700/ws/bot?key=<api_key>`
- **Spectator WebSocket:** `ws://host:8700/ws/spectator`
- **REST API:** `/api/v1/keys/generate`, `/api/v1/arena/state`, `/api/v1/leaderboard`, `/api/v1/admin/*`
