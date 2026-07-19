# Build And Deploy

This guide covers local development, Docker builds, production deployment shape, and regression checks. Keep environment-specific hostnames, SSH ports, private paths, and credentials in a private ops runbook.

## Local Development

Start the full stack:

```bash
cp .env.example .env
docker compose up -d --build
```

The local server listens at:

```text
http://localhost:8700
```

Useful checks:

```bash
curl http://localhost:8700/api/v1/health
curl http://localhost:8700/api/v1/version
```

Run the Go server tests:

```bash
cd go-arena
go test ./...
```

For a native Go server against local Postgres/Redis containers:

```powershell
docker compose up -d arena-db arena-redis
powershell -File scripts/dev-server.ps1
```

## Production Build

Build identity is part of the public version endpoint and About panel. Pass it when building the server image:

```bash
GIT_COMMIT=$(git rev-parse HEAD) BUILD_TIME=$(date -u +%Y-%m-%dT%H:%M:%SZ) \
  docker compose build arena-server
```

Apply the schema with the database owner before starting or recreating a
managed production server. The migration command applies the full idempotent
schema, grants the separately named runtime role DML/table and sequence access,
sets matching default privileges for future objects, and fails non-zero if any
step or the post-migration preflight fails:

```bash
(
  set -a
  . ./.env
  set +a
  runtime_db_user="$ARENA_DB_USER"
  export ARENA_RUNTIME_DB_USER="$runtime_db_user"
  export ARENA_DB_USER="$ARENA_DB_MIGRATOR_USER"
  if [ -n "${ARENA_DB_MIGRATOR_PASSWORD:-}" ]; then
    export ARENA_DB_PASSWORD="$ARENA_DB_MIGRATOR_PASSWORD"
  fi
  docker compose run --rm --no-deps \
    -e ARENA_DB_USER -e ARENA_DB_PASSWORD -e ARENA_RUNTIME_DB_USER \
    arena-server /arena-server migrate
)
```

Then start or recreate the server:

```bash
docker compose up -d arena-server
```

Verify the running version:

```bash
curl https://YOUR_ARENA_HOST/api/v1/version
```

The returned commit should match the commit you intended to deploy.

## Deployment Shape

A typical production deployment uses:

```text
Internet
  -> TLS reverse proxy
  -> arena-server container on localhost/private network
  -> Postgres and Redis bound to localhost/private network
```

Recommended operator rules:

- Keep `.env` private and outside source control.
- Set `ARENA_ADMIN_TOKEN` to a strong random value.
- Change `ARENA_DB_PASSWORD` from the placeholder value.
- Set `ARENA_ADMIN_LOCALHOST_BYPASS=false` when a proxy or container network could make untrusted traffic look like loopback.
- Restrict `ARENA_CORS_ORIGINS` if public cross-origin tooling is not needed.
- Keep Postgres and Redis off the public internet.

## Admin configuration persistence

The Admin Panel stores accepted game configuration, Map Workshop settings, and weapon tuning values in PostgreSQL. The server loads environment/default configuration first, applies saved game and map overrides, initializes weapon ranges, then applies saved base weapon values before loading adaptive balance scales. This order prevents a saved weapon value from being multiplied into the base again.

Game and map saves are deliberately restart-staged. The simulation has many direct reads of its immutable startup configuration, so changing that global object during a live match would create data races and half-applied rounds. Config responses show the saved desired values and include `_persistence.restart_required`, `_persistence.pending_values`, and `_persistence.active_values`. Weapon tuning remains live because its dedicated registry is lock-protected. Every save fails closed when PostgreSQL is unavailable or the transaction fails.

## Self-Update (Admin Panel "Update to latest")

The Admin Panel's Controls tab has a **Version & Update** card that shows the
running commit vs. the latest commit on the release branch and, when enabled,
a one-click **Update to latest** button. It builds the new `arena-server` image,
asks the old app to drain, stops that old writer without removing its container,
runs the new image's migration-only command as the database owner, and only then
recreates the app. `arena-db` and `arena-redis` are never touched, so game data
and stats are preserved. A migration failure fails the update closed and starts
the retained old app container again before clearing the maintenance notice.

It works through a separate `arena-updater` sidecar that holds Docker-socket
(host-root-equivalent) access so `arena-server` itself never needs it. The
sidecar publishes no host port (only reachable from `arena-server` over the
compose network), is gated behind the `updater` Compose profile, requires a
shared-secret bearer token, and fetches the exact 40-char commit SHA from the
**public** Arena repo (no GitHub credentials needed).

Why the sidecar reads the compose project off the live container: `docker
compose` derives its project name from the working directory, and the project
name scopes which network and volumes a service attaches to. If the sidecar
recreated `arena-server` under a different project, it would land on a fresh
network and could not reach `arena-db`/`arena-redis`. The sidecar reads the
project (and the exact compose files, including a VPS-only
`docker-compose.override.yml`) off the running `arena-server` container's own
labels and reuses them; the top-level `name: arena` in `docker-compose.yml`
pins it belt-and-suspenders. This is the same pattern documented in
`hermes-pr-review-agents` (`docs/ops/self-update-for-other-projects.md`).

### Enabling it (one-time, on the server)

```bash
cd /srv/docker/arena          # the deploy dir; compose is always run from here

# 1. Generate a shared secret and wire the sidecar into .env:
openssl rand -hex 32          # -> paste as ARENA_UPDATER_SHARED_SECRET
cat >> .env <<'EOF'
ARENA_UPDATER_URL=http://arena-updater:8090/update
ARENA_UPDATER_SHARED_SECRET=<the-hex-secret>
ARENA_DB_MIGRATIONS_MANAGED=true
ARENA_DB_MIGRATOR_USER=arena_user  # existing PostgreSQL owner, not arena_app
# ARENA_DB_MIGRATOR_PASSWORD=      # only when different from ARENA_DB_PASSWORD
DEPLOY_OWNER=1000:1001        # the deploy dir's owner uid:gid (id -u:id -g)
EOF

# 2. Build + replace only the updater sidecar:
docker compose --profile updater build arena-updater
docker compose --profile updater up -d --no-deps --force-recreate arena-updater
```

Do not combine the initial updater recreation with `arena-server` or its
dependencies. The server should already have received the owner-run migration
above, while the database and Redis containers must remain untouched. In a
managed deployment the migrator variables also pin PostgreSQL's bootstrap owner
and healthcheck identity; `ARENA_DB_USER` remains the separate runtime role.

`ARENA_DB_USER` remains the least-privilege runtime role (for example,
`arena_app`). Compose passes that identity to the one-shot migration as
`ARENA_RUNTIME_DB_USER` before switching the connection to
`ARENA_DB_MIGRATOR_USER`. Normal startup with
`ARENA_DB_MIGRATIONS_MANAGED=true` performs no DDL, but it does run a read-only
required-table/column preflight and exits instead of serving against stale or
inaccessible schema.

Existing updater installations must be rebuilt and recreated once to load this
migration pipeline; an updater cannot replace its own running JavaScript during
an in-flight server update:

```bash
docker compose --profile updater build arena-updater
docker compose --profile updater up -d --no-deps arena-updater
```

After that one-time bootstrap, future Admin Panel updates run migrations
automatically. A missing migrator/runtime role setting is treated as an update
failure before source sync or app shutdown.

Leaving `ARENA_UPDATER_URL` / `ARENA_UPDATER_SHARED_SECRET` unset (the default)
keeps the Version card at display-only, with no update button, and the sidecar
is never created. Treat the shared secret as root-equivalent and rotate it like
one.

## Static File Notes

The frontend is served directly from `frontend/`; there is no build step. JavaScript, CSS, and HTML changes take effect on the next request after the container has the new files.

`go-arena/internal/api/router.go` applies no-cache headers to JavaScript, CSS, HTML, and extensionless page routes so browsers fetch the newest page shell and code.

## Regression Notes

These are real classes of regressions that have broken the site before.

### Same-Origin Iframes

The desktop page embeds dashboard/toolkit views as same-origin iframes. Security headers must allow same-origin framing:

- `X-Frame-Options: SAMEORIGIN`
- CSP `frame-ancestors 'self'`

Do not replace these with `DENY` or `frame-ancestors 'none'` without removing the iframe-based UI first.

Regression test:

```bash
cd go-arena
go test ./internal/api -run TestSecurityHeaders
```

### HTML Cache

Page routes such as `/`, `/dashboard/`, `/admin/`, and `/m/` are extensionless routes that resolve to `index.html`. They need no-cache behavior just like `.js` and `.css`, otherwise a browser can keep serving a stale page shell after a fix ships.

Regression test:

```bash
cd go-arena
go test ./internal/api -run TestNoCacheStaticHandler
```

### Cache-Bust Query Tags

Frontend script and stylesheet tags use `?v=YYYYMMDDx` query strings for provenance and extra cache safety. When behavior changes in a referenced frontend file, bump the tag on the page or importer that references it.

### Cloudflare Browser Insights

Arena intentionally does not allow `static.cloudflareinsights.com` in its Content Security Policy. Cloudflare Web Analytics must not inject its Browser Insights beacon on the Arena hostname.

The `angel-serv.com` zone owns a hostname-scoped Configuration Rule:

- name: `Disable RUM on Arena`
- expression: `http.host eq "arena.angel-serv.com"`
- setting: `Disable Real User Monitoring` enabled (`disable_rum: true`)

Keep this rule scoped to the Arena hostname so analytics on other hosts are unaffected. If a browser reports a CSP violation for `beacon.min.js`, restore the Cloudflare rule; do not widen Arena's CSP to trust another executable-script origin.

After changing the rule, check `/`, `/m/`, `/shop/`, `/dashboard/`, and `/admin/`. None of their HTML responses should contain `beacon.min.js` or `data-cf-beacon`, and the `Content-Security-Policy` header should remain unchanged.

Regression test:

```bash
cd go-arena
go test ./internal/api -run TestSecurityHeaders
```

## Release Checklist

Before pushing a deployment:

```bash
cd go-arena
go test ./...
```

Check touched JavaScript files:

```bash
node --check frontend/js/app.js
```

Check public metadata:

```bash
git diff --check
rg -n --hidden -g '!**/.git/**' 'ARENA_ADMIN_TOKEN=|ARENA_DB_PASSWORD=|private key|BEGIN .*PRIVATE'
```

After deployment:

```bash
curl https://YOUR_ARENA_HOST/api/v1/health
curl https://YOUR_ARENA_HOST/api/v1/version
```
