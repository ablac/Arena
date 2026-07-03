# Security

This document summarizes the security controls in the AI Battle Arena backend
and how to configure them safely. It is intended as both operator guidance
and a reference for compliance reviews (e.g. SOC 2).

## Reporting a vulnerability

Open a private security advisory on the repository, or contact the
maintainers directly. Do not open a public issue for undisclosed
vulnerabilities.

## Authentication & access control

- **Bot API keys** are 32-byte random values, stored only as bcrypt hashes
  (cost factor `ARENA_BCRYPT_ROUNDS`, default 12) plus a 12-character prefix
  for lookup. The plaintext key is shown once at generation time and cannot
  be recovered.
- **Admin API** requires `X-Admin-Token` (compared in constant time),
  a database-issued token, or an OIDC/SSO session cookie
  (`HttpOnly`, `Secure`, `SameSite=Lax`). `ARENA_ADMIN_LOCALHOST_BYPASS`
  (default `true`) allows unauthenticated admin access from loopback
  addresses only — disable it (`ARENA_ADMIN_LOCALHOST_BYPASS=false`) if your
  reverse proxy or container network could ever make requests appear to
  originate from `127.0.0.1`/`::1`.
- Set `ARENA_ADMIN_TOKEN` to a strong random value in every deployment. The
  server logs a warning at startup if it is unset.

## Secrets

- Never commit API keys, admin tokens, or database passwords to source
  control. Bot scripts must read credentials from the environment (see
  `bots/*/README.md` / `ARENA_API_KEY`).
- `.env` is git-ignored; only `.env.example` (placeholder values) is
  committed.
- The server warns at startup if `ARENA_DB_PASSWORD` is left at its insecure
  default value.

## Network-facing hardening

- **Rate limiting** (Redis-backed, fails open if Redis is unavailable so a
  cache outage never blocks legitimate bots/players from connecting):
  per-IP key registration, per-IP and per-API-key WebSocket connection rate
  limits, per-endpoint HTTP rate limits, admin API rate limits.
- **Input validation**: bot names are HTML-tag-stripped and character
  allow-listed server-side; stats, weapons, fallback behaviors, and colors
  are validated against fixed schemas before being applied.
- **WebSocket hardening**: message size cap (`ARENA_WS_MESSAGE_MAX_BYTES`),
  per-connection message rate limiting, heartbeat-based dead-connection
  reaping, IP and API-key ban lists (with optional Cloudflare push).
- **HTTP security headers** (`ARENA_SECURITY_HEADERS_ENABLED`, default
  `true`): `Content-Security-Policy`, `Strict-Transport-Security`,
  `X-Content-Type-Options`, `X-Frame-Options`, `Referrer-Policy`,
  `Permissions-Policy`.
- **Request body size limits** (1 MiB) prevent oversized-payload memory
  exhaustion on JSON API endpoints.
- **Slowloris mitigation** via `ReadHeaderTimeout`/`IdleTimeout` on the HTTP
  server; WebSocket connections manage their own read/write deadlines after
  the upgrade handshake so long-lived bot/spectator connections are
  unaffected.
- CORS origins are configurable via `ARENA_CORS_ORIGINS` (comma-separated;
  defaults to `*` for public read endpoints — tighten this for
  production deployments that don't need cross-origin bot tooling).

## Data protection

- Passwords/keys are never logged. Admin endpoints that display API keys
  mask everything except a short hint, except for the deliberately-visible
  built-in demo bot keys (non-sensitive, local-only fixtures).
- Database credentials, admin tokens, and third-party API tokens (e.g.
  Cloudflare) are loaded exclusively from environment variables
  (`go-arena/internal/config/config.go`), never hardcoded.

## Infrastructure

- The production container runs as a non-root user (`distroless/static:nonroot`)
  on a minimal base image with no shell or package manager.
- Postgres and Redis are bound to `127.0.0.1` only in `docker-compose.yml`
  and are not reachable from outside the host.

## Anti-cheat

- The admin `anticheat` endpoint flags stat-budget violations, physically
  impossible speed/damage/accuracy, and multi-account IP correlation, purely
  as an operator-facing detection tool (not an automated ban).

## Availability posture

Every control above is designed to degrade gracefully rather than block
legitimate traffic: rate limiting fails open if Redis is down, security
headers and body-size limits are configurable escape hatches, and
`ARENA_DB_OPTIONAL` allows the server to run in a degraded mode without
persistence rather than refuse to start.
