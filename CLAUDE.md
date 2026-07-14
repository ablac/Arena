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
    security/                   # Auth, rate limiting, input validation
    ws/                         # WebSocket handlers (bot + spectator)
frontend/
  js/renderer/                  # Babylon.js modules (engine, bots, weapons, etc.)
  js/                           # App boot, spectator WS, leaderboard
  m/                            # Mobile spectator site (served at /m/, reuses js/renderer + spectator-ws)
  dashboard/                    # Customer bot/account dashboard (Admin lives in admin/)
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
# Pass build identity so /api/v1/version and the About drawer report the live commit:
GIT_COMMIT=$(git rev-parse HEAD) BUILD_TIME=$(date -u +%Y-%m-%dT%H:%M:%SZ) \
  docker compose build arena-server

# Arena CLI (set ARENA_DIR to override the default install path)
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
| Graphics/animation toggle (settings panel) | `docs/settings-system.md` - read before adding any new visual effect |
| Build, deploy, or anything touching security headers/static caching | `docs/build-and-deploy.md` - read before touching `security_headers.go` or `noCacheStaticHandler`; has a list of regressions that already shipped once |

## Game Mechanics Quick Ref

- **7 weapons:** Sword, Bow, Daggers, Shield, Spear, Staff, Grapple (each with a signature move: cleave, charged shot, backstab, bash, brace, burn field, slam)
- **Stats:** 20-point budget across HP/Speed/Attack/Defense (1-10 each)
- **Pickups:** Health, Damage Boost, Speed Boost, Shield Bubble, Gravity Well, Cooldown Shard, Bounty Token, Hazard Key, Overdrive Core, Grapple Charge, Relay Battery
- **Game modes:** `ARENA_GAME_MODE` = `ffa` (default) | `team_battle` | `ctf`; `ARENA_TEAM_COUNT` (2), `ARENA_FRIENDLY_FIRE` (false), CTF first to `ARENA_CTF_CAPTURES_TO_WIN` (3) captures
- **Map shapes:** `ARENA_MAP_SHAPE` = `random` (default) | `square` | `circle` | `hexagon` | `diamond` | `cross` | `caves` | `donut` | `islands` | `rooms` | `spiral` — carved into the terrain grid as blocked cells; random obstacles are rejection-sampled against the carved mask (never inside walls) and the combined grid is connectivity-checked
- **Safe zone:** Starts covering the whole map (`ARENA_ZONE_COVER_MAP`, default true; `ARENA_ZONE_INITIAL_RADIUS` only applies when false), shrinks 15% every 20s after a 60s delay, 3 dmg/tick outside
- **Tick rate:** 10 ticks/sec (configurable)
- **Round:** 300s default, min 2 bots to start
- **Extras:** teleport pads, capture pads, hazard zones, landmines, gravity wells, bounty system, universal grapple ability, sudden death, ~30% chance of a round modifier
- **Sudden death:** activates when the zone reaches min radius OR the round clock expires with the fight unresolved; the round then plays overtime (up to `ARENA_SUDDEN_DEATH_MAX_OVERTIME`, 90s) instead of ending on the timer. All damage is multiplied by `ARENA_SUDDEN_DEATH_DAMAGE_MULT` (2x), void tiles spawn, and if no bot deals damage for `ARENA_SUDDEN_DEATH_STALL_SECONDS` (20s) every living bot takes ramping stall damage until combat resumes

## Protocols

- **Bot WebSocket:** `ws://host:8700/ws/bot?key=<api_key>` — ticks carry `your_state.team`, `game_mode`, and in team modes `team_scores` + `flags`; `void_tiles` + `sudden_death_stall` during sudden death
- **Spectator WebSocket:** `ws://host:8700/ws/spectator` — per-bot `team`; top-level `game_mode`, `map_shape`, `team_scores`, `flags`; obstacles only on keyframe broadcasts (`ARENA_SPECTATOR_KEYFRAME_INTERVAL`, default 10, plus on join) — clients keep the last copy between keyframes
- **REST API:** public server-issued bot registration at `POST /api/v1/keys/generate` (empty body; plaintext returned once), optional verified-customer key management at `/api/v1/account/keys`, later bot claim at `POST /api/v1/account/bots`, plus `/api/v1/arena/status`, `/api/v1/arena/map`, `/api/v1/leaderboard`, `/api/v1/bot-setup`, `/api/v1/version` (build identity: git commit + build time; shown in the site's About drawer), `/api/v1/admin/*`
- **Admin runtime tuning:** `PUT /api/v1/admin/game/config` accepts `game_mode`, `team_count`, `friendly_fire`, `map_shape` (plus round/zone/stat keys) — mode/shape changes take effect next round
