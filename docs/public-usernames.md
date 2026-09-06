# Public usernames and account privacy

Angel Accounts owns the globally unique public username shared by Angel apps.
Arena reads the optional `preferred_username` claim from its verified OIDC ID
token (`profile` scope), normalizes ASCII letters to lowercase, and caches it
in nullable `customer_accounts.public_username`. Accepted values have 3–24
ASCII letters, digits or underscores. Missing, empty or invalid strings clear
the cache. Arena never derives a public alias from a private name, email or
account ID, and never offers a local username editor.

The verified `(oidc_issuer, oidc_subject)` pair and Arena account ID retain
ownership of bots, keys, subscriptions and history. An alias change or reuse
must not rebind those records. Global uniqueness, reserved aliases and change
limits belong to Accounts; Arena's cache has no unique username constraint.
The existing private `display_name` remains private and is not backfilled into
the public column or serialized in customer inventory.

## Choosing and refreshing a username

The Dashboard Profile tab displays the public username or **Username
unavailable**. Its setup link opens
[Angel Accounts profile details](https://accounts.angel-serv.com/portal/account/details).
After saving there, press **Refresh username** in Arena. That runs the existing
Accounts sign-in popup again; a live Accounts session normally completes it
without more input. Visiting Accounts alone does not update Arena. No provider
access or refresh token is retained, and no new registration is required.

Missing usernames leave account controls, profile cosmetics and public chat
reading available, but prevent public chat posting. The chat panel offers the
same setup link and refresh action. Bio, avatar color and public bot visibility
remain editable locally. Bio and message bodies are user-authored content;
they are not rewritten by an identity refresh.

## Public API and WebSocket contract

- `GET /api/v1/account/session`: `account.public_username` is a string or null;
  `account.username_setup_url` links to Accounts. Compatibility `name` and
  `display_name` keys contain the public label or **Username unavailable**,
  never the private account name.
- `GET /api/v1/profile/{account_id}` and successful
  `PATCH /api/v1/account/profile` return `public_username` (string or null),
  `chat_handle` (the same public label or **Username unavailable**),
  `username_setup_url`, bio, avatar color, bot visibility and public bots.
  Public profile responses no longer contain `display_name`.
- Profile PATCH accepts `bio`, `avatar_color`, `show_bots_public`. Any supplied
  `display_name`, `name`, `username` or `public_username` key, including null,
  is rejected with HTTP 400 and the Accounts setup URL before applying edits.
- `/ws/chat` uses stable `account_id` for author identity and the current
  username for `handle`. A signed-in reader without one receives
  `chat_status` with `can_post:false`, `reason:"username_required"` and
  `username_setup_url`. Posting returns `USERNAME_REQUIRED` when missing.
  The server checks the current alias on every post and again under the
  account lock when inserting, preserving existing bans and posting limits.
- `chat_identity` carries `account_id` and nullable `public_username` after a
  local sign-in refresh. Open clients update earlier message labels by account
  ID. Other replicas refresh public labels on the 10-second heartbeat.
  Warm history, database history and reconnects project current aliases;
  removed/deleted identities show **Username unavailable**. Legacy stored
  handle snapshots are never a public fallback.

All HTTP and WebSocket paths retain the existing `/arena` mount support.
Current username reads return session snapshots without evicting cached
sessions: old cookies retain their CSRF tokens and their original, expiring,
memory-only administrator grants. Durable session restoration never invents
an administrator grant.

## Migration and verification

Run the existing serialized schema migrator before starting a managed-schema
runtime. The idempotent migration adds the nullable column with no backfill;
runtime preflight requires it. Existing accounts consequently need to choose
a central username and sign in again before posting publicly. Deploying an old
server or old frontend would restore its former name projections, so rollback
requires evaluating that privacy behavior; keep the nullable column in place.

Focused checks include signed-token callback tests, PostgreSQL migration,
identity/session/profile/history tests, open-socket rename/removal tests,
`node scripts/test-public-usernames.mjs`, and dashboard browser tests at root
and `/arena` paths. The full Go gate remains `go test ./...`; set
`ARENA_TEST_DATABASE_URL` and `ARENA_TEST_REDIS_ADDR` to isolated local services
to include integration coverage.
