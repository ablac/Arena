# Building, Running, and Deploying the Arena

This covers local dev, production build/deploy, and - most importantly - a
short list of specific things that have broken the live site before. Read
the "Things that have broken this site" section before touching security
headers, static file serving, or anything CSP-related. It is not generic
advice; every item is a real regression that shipped once already.

For architecture, project layout, and day-to-day commands (`arena` CLI,
`docker compose`, game mechanics reference), see the top-level `CLAUDE.md` -
this file doesn't repeat that, it covers build/deploy specifics and
regression traps that belong closer to the code that can reintroduce them.

## Local dev

```powershell
docker compose up -d arena-db arena-redis   # host ports 5439 / 6390 per .env - NOT the compose defaults (5432/6379)
powershell -File scripts/dev-server.ps1     # builds + runs go-arena against those containers, demo bots on, short caves-only rounds
```

`.claude/launch.json` server name is `arena-native` if you're driving this
through Claude Code's preview tooling. The frontend is served directly from
`frontend/` by the Go binary's `http.FileServer` - there is no bundler and no
separate frontend build step. Editing any `.js`/`.css`/`.html` under
`frontend/` takes effect on the next request; only Go changes need a rebuild
(`go build` / `scripts/dev-server.ps1` re-runs that for you).

Postgres/Redis containers may already exist but be stopped
(`docker start arena-db arena-redis` rather than `docker compose up -d` if
so - compose will otherwise try to recreate them).

## Production build

```bash
GIT_COMMIT=$(git rev-parse HEAD) BUILD_TIME=$(date -u +%Y-%m-%dT%H:%M:%SZ) \
  docker compose build arena-server
```

The `GIT_COMMIT`/`BUILD_TIME` build args are not optional decoration - the
site's `/api/v1/version`, `/api/v1/health`, and the About drawer all report
the running commit, and that's the primary way to verify a deploy actually
took effect versus silently serving a stale image. See
`go-arena/internal/version/` and the About overlay markup in
`frontend/index.html`.

## Deploying to angelserv (production)

The `/srv/docker/arena` checkout on the `angelserv` host is root-owned with
no GitHub credentials, so `git pull` fails there. Ship a bundle instead:

```bash
git bundle create arena.bundle <server-head-sha>..main
cat arena.bundle | ssh -p 64217 keith@angelserv 'cat > /tmp/arena.bundle'   # scp fails - hardened sshd has no SFTP subsystem
```

Then on the server:

```bash
sudo git -C /srv/docker/arena fetch /tmp/arena.bundle main
sudo git -C /srv/docker/arena reset --hard FETCH_HEAD
sudo GIT_COMMIT=$(sudo git rev-parse HEAD) BUILD_TIME=$(date -u +%Y-%m-%dT%H:%M:%SZ) \
  docker compose -f docker-compose.yml -f docker-compose.override.yml build arena-server
sudo docker compose -f docker-compose.yml -f docker-compose.override.yml up -d --no-deps --force-recreate arena-server
```

Verify: `curl https://arena.angel-serv.com/api/v1/version` matches the
merged commit; logs show `all demo bots launched count=14` and
`round started`; the About drawer's version matches.

Untracked server-side files (`.env`, `docker-compose.override.yml`,
`.anismin-backup-*`) survive a bundle reset - it only moves the git history
forward, it doesn't touch files git doesn't track.

**Repo etiquette:** other agents land PRs on `main` continuously. Always
`git fetch` and rebase before merging - a branch cut even an hour earlier can
conflict with several new `main` commits, most often on the `?v=` cache-bust
tags described below.

## Things that have broken this site (don't reintroduce these)

### 1. Blanket security headers break the site's own same-origin iframe

`frontend/index.html` embeds `/dashboard/?view=public` and
`/dashboard/?view=private` in same-origin `<iframe>`s - that's how the
Toolkit and Dashboard nav overlays work. **`X-Frame-Options: DENY` and CSP
`frame-ancestors 'none'` block same-origin framing too, not just
third-party/clickjacking framing.** A 2026-07 SOC 2 hardening pass added
exactly that as a blanket `SecurityHeadersMiddleware` applied to every route
with `r.Use(...)` and it silently broke both overlays - the iframe's request
still returned `200 OK`, so it looked fine in a network log; the browser just
refused to render it (`net::ERR_BLOCKED_BY_RESPONSE`).

The fix in place now (`go-arena/internal/api/security_headers.go`) uses
`X-Frame-Options: SAMEORIGIN` and `frame-ancestors 'self'` - this still
blocks every third-party clickjacking attempt (the actual SOC 2 concern),
it just also permits the app's own same-origin embed. **If you're adding or
changing security headers, grep `frontend/` for `<iframe` first** and make
sure whatever you ship still permits same-origin framing if any exist.
`go-arena/internal/api/security_headers_test.go` has a regression test for
this - don't remove or weaken it without understanding why it's there.

### 2. Only `.js`/`.css` had no-cache headers - HTML didn't

`noCacheStaticHandler` (`go-arena/internal/api/router.go`) forces
`Cache-Control: no-cache, no-store, must-revalidate` so browsers always
fetch the latest JS/CSS. It used to only match `.js`/`.css` file extensions -
but `/`, `/dashboard/`, `/admin/`, `/m/` are extensionless directory routes
that `http.FileServer` resolves to an implicit `index.html`, so a browser
that cached one of *those* before a fix shipped kept serving the cached,
broken response after the fix was live - the exact same "looks fine" trap as
above, but from the client cache side. The handler now also matches
`.html` and any path whose last segment has no `.` in it. If you add a new
top-level directory-style route, this already covers it (it matches on
"does the URL look like a page vs. a fingerprinted asset", not a hardcoded
list of paths) - but if you add a genuinely long-lived binary asset type
(fonts, models) under a path that doesn't look like a directory, double
check it isn't accidentally caught by this and losing its caching.

### 3. Cache-bust `?v=` tags: bump on real changes, don't chase the whole graph

Frontend JS/CSS references carry a `?v=YYYYMMDDx` query string
(`app.js?v=20260706e`). Since (2) above already forces `no-store` on every
`.js`/`.css` response regardless of query string, these tags are no longer
load-bearing for correctness - but they're still useful for human-readable
provenance (you can tell from a Network tab which version shipped) and as a
second line of defense against a misbehaving intermediate cache/CDN that
doesn't fully respect `no-store`. Bump the tag on files whose behavior
actually changed, using a value strictly newer than the current highest tag
in the touched files (`grep -oE '\?v=[0-9]{8}[a-z]' frontend/index.html
frontend/js/app.js | sort -u` to find it). Don't feel obligated to cascade
the bump through every file that merely *imports* something you changed -
that was worth doing before (2) existed; now it's optional polish, not a
correctness requirement.

## Testing

- **Backend**: `cd go-arena && go test ./...`. Add a test alongside any bug
  fix in `internal/api/` or elsewhere - `security_headers_test.go` and
  `router_test.go` are recent examples of regression tests for exactly the
  bugs described above.
- **Frontend**: no bundler, no JS test framework. `node --check <file>.js`
  catches syntax errors fast. Actual behavior verification is manual/browser
  based - there is no headless test runner wired up for the frontend.
