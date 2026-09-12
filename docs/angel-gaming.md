# Angel Gaming integration and domain migration

Arena's public address is `https://arena.angel-gaming.com`; the hub is
`https://angel-gaming.com`. Arena remains a standalone Go/PostgreSQL/Redis
application. Accounts at `https://accounts.angel-serv.com` continues to own
identity, central public aliases and administrator grants. Gaming usernames
are separate Gaming profile labels and do not overwrite Arena's Accounts alias.

## Service configuration

Integration is disabled when both settings are absent. Set
`ANGEL_GAMING_ORIGIN=https://angel-gaming.com` and
`ANGEL_GAMING_SERVICE_KEY` to the same game-specific, 64-character hexadecimal
credential configured for Arena at the hub. Provision it through the Shared
Vault; never put it in browser code, an SDK, source, a URL or a log. The origin
must be HTTPS without a path, credentials, query or fragment. Invalid partial
configuration fails startup. A service restart is required after configuration.

Run the existing owner migration before deploying this server. The additive
`gaming_event_outbox` and `round_persistence_receipts` schemas are mandatory even when delivery is disabled; the
managed runtime preflight checks it. Retain this table on rollback. An older
server can continue writing Arena results, but cannot emit new Gaming events.

## Owner bot projection

`POST /api/v1/gaming/profile` requires `Authorization: Bearer <service key>`.
The JSON request contains only `issuer` and `subject`. The issuer must be
`https://accounts.angel-serv.com`; the hub must derive the subject from its
verified signed-in owner, never from a public caller's requested identity.
The endpoint allows 60 authenticated requests per minute per process, limits
the body to 1024 bytes and bounds database lookup time to five seconds.

The response is `{bots:[{id,name,elo,kills,deaths,wins,public}],public_username}`.
Unknown identities return an empty bot list and a null public username. All
linked bots are available to the authenticated owner; `public` on each bot
comes from Arena's `show_bots_public` setting. The hub must filter that flag
and its own profile visibility for public views. Responses are `no-store`
and never contain account IDs, private names, email, API keys or tokens.

## Durable authoritative round events

The production per-round batch insert writes a round receipt, Arena stats and
its Gaming outbox in one PostgreSQL statement. A receipt keyed by the durable
round ID prevents duplicate statistics after an uncertain commit acknowledgment.
Each call captures the complete result for one round. It snapshots each bot's current verified owner
through `account_bot_links`; unowned bots and non-Accounts identities produce
no event. The engine's existing leaderboard reset epoch and straddling-round
exclusions remain unchanged. No browser or bot-supplied award IDs are accepted.

A failed producer write retains its immutable captured round result in memory.
The next regular persistence flush or an independent five-second retry loop
retries it, even while the arena is idle. Each database attempt has a five-second
timeout. A successful leaderboard reset clears pending pre-reset captures;
a failed reset retains them. The owner is the verified owner at the first
successful database persistence, not an earlier owner at the instant of combat.

This memory retry queue does not survive a process crash before its first
successful database write. A crash combined with a database outage can lose
that uncommitted round, as can the existing earlier round-creation failure
path. After the receipt/stats/outbox transaction commits, queued delivery
survives restart and repeats safely through hub idempotency. No filesystem spool
or historical ownership reconstruction is introduced by this integration.

The sender posts to `/api/integrations/arena/events` on the configured hub with
the game-specific bearer. Its body contains:

```json
{
  "eventId": "<round-id>:<bot-id>",
  "issuer": "https://accounts.angel-serv.com",
  "subject": "<verified-owner-subject>",
  "occurredAt": "2026-09-12T21:00:00.000Z",
  "type": "round.completed",
  "facts": {"won": true, "kills": 3, "deaths": 1, "botId": "<bot-id>"}
}
```

Ownership and payload are immutable once queued. Later unlink/relink operations
do not transfer historical events. Event IDs distinguish every participating
bot, including several bots owned by one account in the same round. Hub
idempotency makes a retried acknowledgment safe. The event time is the durable
result write time in UTC, with milliseconds.

The background sender leases at most ten due events for one minute, uses a
five-second HTTP timeout and polls every five seconds. Successful 2xx responses
(including duplicate acknowledgments) mark the row delivered. Failures retain
the payload and retry with exponential delays capped at one hour. Redirects
are never followed; 4xx responses including conflicting event IDs stay queued
for operator diagnosis. Process crashes release work after the durable lease
expires. Logs contain only general status, never payloads or credentials.
Delivered rows remain for idempotency and audit; leaderboard resets do not erase
Gaming awards or pending deliveries. Disabling integration stops delivery and
new event production without changing existing queued rows.

## Staged deployment and compatibility

This source change does not configure DNS, reverse proxies or OIDC callbacks.
Add the new hostname at the existing Arena reverse proxy while the old hostname
continues to serve. Preserve TLS and WebSocket upgrade headers. Add a scoped
Cloudflare RUM-disable rule for `arena.angel-gaming.com`, matching the existing
old-host rule; do not widen Arena's CSP to permit injected analytics.

Allow both old and new callback URLs at Accounts first. Keep
`ARENA_CUSTOMER_OIDC_REDIRECT_URI=https://arena.angel-serv.com/account/callback`
and add
`ARENA_CUSTOMER_OIDC_ADDITIONAL_REDIRECT_URIS=https://arena.angel-gaming.com/account/callback`.
The explicit allowlist selects a literal callback by the actual request Host
and trusted transport scheme. Forwarded Host is never trusted. Each new sign-in
persists its selected callback with the single-use, browser-bound transaction;
the callback must arrive on that exact host/path and the token exchange uses
the persisted URI. Old transactions lacking the additive `redirect_uri` field
use the primary old callback, so in-flight sign-ins survive the cutover.
Run the owner migration before deploying; managed startup checks this column.

Cookies remain host-only. Visitors establish an Arena session on each origin
through Accounts; no cookies or account ownership are copied across domains.
Do not remove an allowed callback or change the primary legacy fallback during
the ten-minute sign-in transaction window. Optional prefixed callbacks must be
explicitly listed, and every accepted callback must be HTTPS with exactly
`/account/callback` or `/arena/account/callback` and no query/fragment/userinfo.

Once new-origin sign-in and gameplay pass live checks, redirect only ordinary
GET/HEAD browser pages at `arena.angel-serv.com` to the same path and query at
`arena.angel-gaming.com`. Keep `/api/`, `/ws/`, `/arena/api/`, `/arena/ws/` and
pending callback routes operational on the old hostname. Do not redirect bot or
spectator WebSockets. Keep legacy machine endpoints until all consumers are
verified migrated. Do not change Accounts, Support or other angel-serv hosts.

Verify new and old health/version, spectator and bot WebSockets, profile lookup,
dashboard OIDC, CSRF-protected mutations, public alias behavior, administrator
grant preservation, session restoration and sign-out. Check root/mobile
canonical tags and legal links to Accounts. No live migration success should
be inferred from local tests.

## Local validation

Use isolated PostgreSQL and Redis instances with `ARENA_TEST_DATABASE_URL` and
`ARENA_TEST_REDIS_ADDR`, then run `cd go-arena && go test ./...` and
`git diff --check`. Gaming tests cover service authorization/body/quota limits,
safe owner projections, atomic failure rollback, unowned bots, immutable owner
snapshots, durable retry acknowledgments and no-follow HTTP delivery.

## Included access and optional cosmetics

Accounts may return an Arena entitlement with `source: "included"` and
`active: true` for automatic base access. This never unlocks paid cosmetics.
Arena reads the nested `upgrade` only when its source is `subscription`, it
names the Arena product, has a nonempty plan slug, and Accounts says it is
active. No upgrade, expired upgrade, malformed upgrade, or unknown source grants
paid cosmetics. The existing subscription row shape (source absent or
`subscription`) continues to use Accounts' active flag. No plan price, trial,
seat or expiration rule is recomputed in Arena. Accounts still owns the optional
paid all-cosmetics plan; the Gaming profile and achievements do not grant it.
