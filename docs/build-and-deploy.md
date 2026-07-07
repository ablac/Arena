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

## Self-Update (Admin Panel "Update to latest")

The Admin Panel's Controls tab has a **Version & Update** card that shows the
running commit vs. the latest commit on the release branch and, when enabled,
a one-click **Update to latest** button. It rebuilds and recreates only the
`arena-server` container at the chosen commit. `arena-db` and `arena-redis` are
never touched, so game data and stats are preserved.

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
DEPLOY_OWNER=1000:1001        # the deploy dir's owner uid:gid (id -u:id -g)
EOF

# 2. Build + bring up the server (so it picks up the new env) and the sidecar:
GIT_COMMIT=$(git rev-parse HEAD) BUILD_TIME=$(date -u +%Y-%m-%dT%H:%M:%SZ) \
  docker compose --profile updater build arena-server arena-updater
docker compose --profile updater up -d arena-server arena-updater
```

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
