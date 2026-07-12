# Architecture

AI Battle Arena has four main surfaces: the Go game server, the browser spectator UI, bot SDKs, and optional local admin tooling.

## High-Level Flow

```text
Bot SDK / custom bot
  -> REST: create key, fetch map, configure bot
  -> WebSocket: select loadout, receive ticks, send actions

Go server
  -> validates credentials, loadouts, and actions
  -> advances the game loop
  -> persists bot and leaderboard data
  -> broadcasts spectator state

Browser frontend
  -> REST: public status, leaderboard, weapon stats, docs
  -> WebSocket: spectator stream
  -> renders the arena with Babylon.js
```

## Backend

The backend lives in `go-arena/`.

| Package | Responsibility |
| --- | --- |
| `cmd/arena-server` | process entrypoint and service wiring |
| `internal/api` | REST routes, admin routes, security headers, public bot setup metadata |
| `internal/config` | environment-driven configuration |
| `internal/db` | PostgreSQL connection, queries, and models |
| `internal/game` | game state, combat, movement, pickups, map shape, scoring, rounds |
| `internal/demobots` | built-in demo bot behavior |
| `internal/security` | API key generation/verification, validation, rate limiting |
| `internal/ws` | bot and spectator WebSocket handlers |

## Frontend

The frontend lives in `frontend/` and is served as static files by the Go server. There is no bundler.

- `frontend/index.html` is the desktop spectator and onboarding page.
- `frontend/m/` is the mobile spectator page.
- `frontend/dashboard/` is the public/private bot toolkit surface.
- `frontend/js/renderer/` contains Babylon.js scene modules.
- `frontend/js/settings.js` is the graphics settings source of truth.

Because static files are served directly, HTML, CSS, and JavaScript changes should be syntax-checked and tested in a browser.

## Persistence And Cache

The default Docker Compose stack runs:

- PostgreSQL for bot keys, bot metadata, leaderboard, round history, and related state
- Redis for rate limiting and cache-backed controls
- The Go server bound to localhost by default

For local experiments, `ARENA_DB_OPTIONAL=true` can let the server run in a degraded mode without persistence.

## Security Model

- Bot keys are generated only for verified customer accounts, durably linked to
  that account, and stored as bcrypt hashes with lookup prefixes.
- Admin APIs require an admin token, a database-issued admin token, or configured OIDC/SSO.
- Bot input is schema validated before it affects game state.
- WebSocket and HTTP paths have size and rate controls.
- Production deployments should terminate TLS at a reverse proxy and pass only the needed routes to the server.

## Bot Protocol

Bots interact with the arena through a small loop:

1. Generate an account-owned API key in the verified-email Dashboard, or load an existing key.
2. Connect to `/ws/bot?key=...`.
3. Receive `connected`.
4. Send `select_loadout`.
5. Receive `tick` messages.
6. Send an `action` for the current tick.

The complete public reference is in [BOT-GUIDE.md](../BOT-GUIDE.md) and the machine-readable endpoint `GET /api/v1/bot-setup`.
