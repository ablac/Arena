# Cosmetics And Fair Monetization

Arena ships a launch catalog for bot skins, weapon finishes, and attachments,
plus a provider-neutral ownership ledger and Stripe-hosted Checkout adapter.
Checkout is intentionally disabled by default: the public catalog only exposes
working purchase buttons after verified customer auth, PostgreSQL, Stripe credentials,
signed webhooks, and return URLs pass startup validation. Catalog browsing,
Admin editing, verified-email ownership, per-copy licenses, bot assignment,
equip, refunds, disputes, protocol boundaries, and rendering remain useful and
testable independently.

## Ownership and assignment rules

Purchases belong to a verified customer email account, never to an API key.
An API key is only a one-time proof that the signed-in customer controls a bot.

- Every purchased copy is a separate cosmetic license with a stable license ID.
- A license may be assigned to zero or one linked bot at a time.
- Assignment and equip are separate. Moving a license does not silently replace
  the destination bot's current cosmetic in that slot.
- Only an active license assigned to that bot can be equipped or rendered.
- Unassigning, unlinking a bot, fully refunding, charging back, or revoking a
  license removes that exact paid loadout. A partial refund flags the order for
  review without guessing which piece to remove. Other orders are untouched.
- Losing, revoking, or replacing an API key does not delete account ownership.
- Linking a bot already owned by another account is rejected; possession of a
  leaked key cannot silently steal the existing account link.

The Bot Dashboard is the owner-facing control surface. A signed-in customer
can link a bot by submitting its key once, assign/move/remove each license, and
explicitly equip an assigned license. The link form clears the plaintext key
after submission and does not save it as ownership data.

Legacy bot-scoped entitlements are preserved as unclaimed licenses during the
schema upgrade. A verified account can claim them by proving the original bot
key once. After that claim, the account remains the owner even if the key is
later lost or revoked. If both the key and provider purchase reference are
already unavailable, recovery requires an administrator; Arena does not invent
an email owner.

## Customer registration and authentication

Arena supports two independent ways to establish the same verified customer
session: native passwordless email links or a dedicated customer OIDC client.
Neither can authorize Admin routes. Both produce the same distinct HttpOnly,
Secure, SameSite=Lax customer cookie and the same CSRF token for mutations.

### Native passwordless email registration

The Dashboard can create or sign into an account by emailing a one-time link.
Arena generates 32 random bytes, stores only the SHA-256 digest in PostgreSQL,
expires it after 15 minutes, replaces an older unused token only after a
per-email cooldown, and atomically consumes it once. The bearer token is in the
URL fragment, which is not sent in the HTTP request; the Dashboard removes it
from browser history and waits for the customer to click **Continue to Arena**
before a same-origin POST verifies it. A mail scanner or preview that merely
fetches or renders the link therefore cannot consume it. Request responses are
generic and do not disclose whether an account already exists.

Angel-serv runs Stalwart for transactional mail. Provision a dedicated regular
user `noreply@angel-serv.com`, then give Arena a high-entropy credential used
only by this transactional sender; do not reuse a recovery, admin, or personal
mailbox credential. Configure the production container with:

```dotenv
ARENA_CUSTOMER_EMAIL_AUTH_ENABLED=true
ARENA_CUSTOMER_EMAIL_SIGN_IN_URL=https://arena.angel-serv.com/dashboard/
ARENA_CUSTOMER_EMAIL_TOKEN_TTL_MINUTES=15
ARENA_CUSTOMER_EMAIL_SEND_COOLDOWN_SECONDS=60
ARENA_CUSTOMER_EMAIL_SEND_RPM=5
ARENA_CUSTOMER_OIDC_SESSION_TTL_HOURS=24
ARENA_SMTP_HOST=100.71.171.28
ARENA_SMTP_PORT=465
ARENA_SMTP_TLS_MODE=implicit
ARENA_SMTP_TLS_SERVER_NAME=mail.angel-serv.com
ARENA_SMTP_USERNAME=noreply@angel-serv.com
ARENA_SMTP_PASSWORD=replace-with-send-only-app-password
ARENA_SMTP_FROM=Arena <noreply@angel-serv.com>
```

The private submission address is deliberately separate from the TLS server
name. Arena verifies the `mail.angel-serv.com` certificate with TLS 1.2 or
newer and never uses `InsecureSkipVerify` or public port 25. The production
`.env` is rendered by `nimbus-infra/roles/arena/templates/.env.j2`; add these
nonsecret settings there and keep the app password in the role's encrypted
vault variable so an Ansible run cannot erase the integration.

### Dedicated customer OIDC

OIDC remains supported for deployments that already have a public customer
identity provider. It requires a non-empty `email_verified=true` claim,
browser-bound state, nonce, and PKCE. Never reuse the Admin client or open the
shared staff Authentik instance to public enrollment.

Configure a dedicated customer OIDC application:

```dotenv
ARENA_CUSTOMER_OIDC_ENABLED=true
ARENA_CUSTOMER_OIDC_ISSUER=https://YOUR_IDP/application/o/arena-customer/
ARENA_CUSTOMER_OIDC_CLIENT_ID=arena-customer
ARENA_CUSTOMER_OIDC_CLIENT_SECRET=replace-me
ARENA_CUSTOMER_OIDC_REDIRECT_URI=https://YOUR_ARENA_HOST/account/callback
ARENA_CUSTOMER_OIDC_SESSION_TTL_HOURS=24
ARENA_CUSTOMER_BOT_LINK_RPM=10
```

For an `/arena`-prefixed deployment, register
`https://YOUR_ARENA_HOST/arena/account/callback` instead. The provider must put
the verified email claim in the ID token. A typed email address is never treated
as proof of ownership.

Customer sessions are currently process-local, so a server restart asks the
customer to sign in again. Accounts, bot links, licenses, assignments, and
purchase history remain in PostgreSQL.

## Fair-play boundary

Cosmetics must never change a bot's HP, movement, attack, defense, weapon,
cooldown, hitbox, collision, visibility to other bots, or protocol data used by
AI decisions.

The implementation enforces this in several places:

- Catalog, account, license, and assignment tables contain presentation and
  ownership data only.
- The game engine receives only three allowlisted local asset keys. It never
  receives price, rarity, email, payment, or account data.
- The spectator renderer maps those keys to fixed local visuals; catalog rows
  cannot load arbitrary URLs, scripts, models, or materials.
- Paid equip checks the exact active license and its current bot assignment on
  the server.
- Unknown, inactive, or retired assets fall back to standard visuals.
- Spectators can disable chassis skins, weapon finishes, or attachments in the
  existing graphics settings.

## Launch catalog

The built-in catalog contains exactly 303 items: three permanent defaults and
300 custom cosmetics arranged as 100 coordinated sets. Each set contains one
chassis, one weapon finish, and one attachment. The original Neon Signal and
Void Orbit packs remain sets 001 and 002; sets 003-100 add 294 custom pieces
across Elemental, Cosmic, Cyber, Wild, Arcane, Industrial, Royal, Abyssal, and
Apex collections.

Every coordinated set costs **$1.99 USD** regardless of rarity. Rarity remains
presentation and catalog metadata; it does not change the one-time set price.

All Access costs **$19.99 USD per month** and grants every current cosmetic set,
every set published later while access remains active, and up to five active
account-owned API keys. Cancellation keeps access through the paid period. When
service ends, subscription-provided licenses are removed from the account and
from bots using them. Separately purchased $1.99 sets remain owned.

Only whole sets are purchasable at launch. Individual pieces keep reference
price and rarity metadata for Admin/search displays but cannot create an
accidental single-item checkout. Quantity is bounded to 1-10; buying two packs
creates six independent licenses. Every price, currency, pack membership, and
quantity is snapshotted on the server before Stripe is called.

Set keys use the fixed `arena_set_NNN_slug` contract. A local deterministic
theme mapper gives every set a bounded palette, chassis pattern, weapon finish,
and attachment recipe. No catalog row can load a remote image, model, script,
or gameplay behavior. Public and account storefronts render 12 packs initially
and use search/filter/show-more controls instead of inserting all 300 pieces
into the DOM at once.

## Asset source and intake policy

The starter cosmetics are fixed, local procedural visuals already rendered by
Arena. This catalog/admin work does not import a third-party archive or load
remote art at runtime.

Good CC0 candidates for later reviewed drops include Kenney's
[Rune Pack](https://kenney.nl/assets/rune-pack) for decals,
[Particle Pack](https://kenney.nl/assets/particle-pack) for aura/trail source
art, and [Space Shooter Extension](https://kenney.nl/assets/space-shooter-extension)
for attachment silhouettes. Kenney's [support page](https://kenney.nl/support)
states that game assets on its asset pages are CC0 and may be used commercially
without required attribution. Preserve the exact source page and license record
with every imported batch even when attribution is optional.

Before an external asset becomes a catalog item:

1. Download it from the recorded upstream page and retain the license snapshot.
2. Reject executables, scripts, remote URLs, and files outside the reviewed
   image/model formats.
3. Normalize dimensions, transparency, naming, and texture size offline.
4. Map it to a fixed local `asset_key`; catalog rows must not select arbitrary
   files or URLs.
5. Test GPU memory, draw calls, mobile fidelity, color contrast, and cleanup on
   round/map changes before enabling it.
6. Sell the Arena license to use the curated cosmetic, not the upstream source
   archive, attribution, authorship, or exclusivity.

## API

Public catalog:

```bash
curl https://YOUR_ARENA_HOST/api/v1/cosmetics/catalog
```

Customer account routes use the verified customer session cookie. Every
authenticated POST, PUT, and
DELETE also requires the `X-CSRF-Token` returned by the session endpoint.

```text
GET    /api/v1/dashboard/login
POST   /api/v1/dashboard/logout
GET    /api/v1/account/session
POST   /api/v1/account/email/start
POST   /api/v1/account/email/verify
GET    /api/v1/account/cosmetics
GET    /api/v1/account/cosmetics/orders
POST   /api/v1/account/cosmetics/checkout
POST   /api/v1/account/cosmetics/subscription/checkout
POST   /api/v1/account/cosmetics/subscription/portal
GET    /api/v1/account/keys
POST   /api/v1/account/keys
DELETE /api/v1/account/keys/{key_id}
POST   /api/v1/account/bots
DELETE /api/v1/account/bots/{bot_id}
PUT    /api/v1/account/cosmetic-licenses/{license_id}/assignment
DELETE /api/v1/account/cosmetic-licenses/{license_id}/assignment
PUT    /api/v1/account/bots/{bot_id}/cosmetics
```

Linking submits `{"api_key":"arena_..."}`. Assignment submits
`{"bot_id":"BOT_UUID"}`. Explicit account equip submits
`{"license_id":"LICENSE_UUID"}`. Checkout submits a catalog identity, never a
price: `{"pack_id":"arena-set-003-ember-vanguard-pack","quantity":1}`. Arena
creates a pending order from the current server-side pack snapshot, then returns
the HTTPS `checkout_url` for Stripe-hosted Checkout.

Account key creation submits `{"bot_name":"MyBot"}` and returns the full
`api_key` once alongside safe key metadata, `active_count`, and the server limit.
The Dashboard never stores the plaintext key after the user clears it. Arena
keeps every issued-key record linked to the verified account, including revoked
keys: five may be active, creation is limited to 10 per account per hour,
revocation to 20 per account per hour, and the lifetime history is capped at
100 records pending a support review. Both account and per-IP mutation limits
are enforced before expensive key verification or bcrypt work. All Access
checkout and customer-portal requests return HTTPS `checkout_url` and
`portal_url` redirects respectively.

Stripe posts signed events to the public provider endpoint; this route does not
use a customer cookie or CSRF token because it authenticates the exact raw body
with the configured Stripe signing secret:

```text
POST /api/v1/cosmetics/webhooks/stripe
```

The two email endpoints are pre-authentication routes. They require a strict
same-origin browser request and have IP rate limits; `start` also has the
PostgreSQL per-email cooldown. `verify` receives the fragment token only after
the Dashboard removes it from the address bar. OIDC-only deployments may leave
native email auth disabled. Redis is an availability dependency for `start`:
Arena returns `503` without sending mail whenever the per-client quota cannot
be enforced.

The `/arena/api/v1/...` mirror is also registered for prefixed deployments.
Configure exactly one of these URLs in Stripe, matching the deployment's public
path.

The existing bot-key route remains useful for bot code and free cosmetics. A
paid item succeeds only when an active exact license is assigned to that bot:

```bash
curl -X PUT \
  -H "X-Arena-Key: YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"slot":"attachment","cosmetic_id":"attachment-signal-antenna"}' \
  https://YOUR_ARENA_HOST/api/v1/bot/cosmetics
```

Admin fulfillment grants ownership by purchaser email and returns the stable
license ID. `external_reference` must be a stable order-line/copy or support
ticket reference; a transient webhook delivery ID is a separate concern.

```bash
curl -X POST \
  -H "X-Admin-Token: YOUR_ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"email":"buyer@example.com","cosmetic_id":"skin-neon-grid","external_reference":"ticket-123-copy-1"}' \
  https://YOUR_ARENA_HOST/api/v1/admin/cosmetics/grants
```

Catalog administration uses the same protected admin authentication as the
rest of the control center. The admin dashboard gives it a dedicated Cosmetics
Shop section rather than placing it in Game Config.

```text
GET    /api/v1/admin/cosmetics/catalog
PUT    /api/v1/admin/cosmetics/categories/{category_id}
DELETE /api/v1/admin/cosmetics/categories/{category_id}
PUT    /api/v1/admin/cosmetics/items/{item_id}
DELETE /api/v1/admin/cosmetics/items/{item_id}
PUT    /api/v1/admin/cosmetics/packs/{pack_id}
DELETE /api/v1/admin/cosmetics/packs/{pack_id}
GET    /api/v1/admin/cosmetics/audit?limit=50
GET    /api/v1/admin/cosmetics/orders?status=paid&query=buyer@example.com
GET    /api/v1/admin/cosmetics/access?email=buyer@example.com
POST   /api/v1/admin/cosmetics/memberships
DELETE /api/v1/admin/cosmetics/memberships/{membership_id}
```

Complimentary admin memberships are cosmetics-only access grants. A request
provides an email and exactly one of `duration_days` or an RFC3339
`expires_at`, plus an optional internal note. They materialize one independently
assignable license for every item in current purchasable sets and sync future
sets while active. They do not create Stripe billing state or change API-key
limits. Revocation or expiry removes only membership-issued assignments and
loadouts; purchased and separately granted licenses remain intact.

Admin catalog reads include inactive records. Item `slot` and `asset_key` are
immutable after creation so a price/name edit cannot silently turn an owned
license into a different visual. Category, item, and pack mutations are
transactional, validate bounded IDs/prices/currencies, and record before/after
audit snapshots.

Revocation targets one exact copy and soft-revokes it:

```bash
curl -X DELETE \
  -H "X-Admin-Token: YOUR_ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"license_id":"LICENSE_UUID"}' \
  https://YOUR_ARENA_HOST/api/v1/admin/cosmetics/grants
```

## Data model

```text
customer_accounts
  normalized verified email identity and durable account ID

customer_email_verifications
  one short-lived SHA-256 token digest per normalized email; atomically deleted
  when consumed and never stores the bearer token

account_bot_links
  bots proven by API-key possession; each bot links to at most one account

account_api_keys
  durable account ownership for bcrypt-hashed API keys; unlinking a bot does not
  erase ownership or bypass the five-active-key cap

cosmetic_items
  catalog identity, slot, allowlisted asset key, price metadata, sale flags

cosmetic_categories
  ordered admin-managed group metadata; inactive categories stay out of public reads

cosmetic_packs
  ordered bundle metadata with a fixed 199 USD-minor-unit sale price

cosmetic_pack_items
  exact ordered membership of allowlisted catalog items in each pack

cosmetic_catalog_audit
  actor, action, entity identity, and before/after catalog snapshots

cosmetic_licenses
  one row per purchased copy, account owner, source/reference, lifecycle status

cosmetic_license_assignments
  exact license -> exact linked bot; primary key enforces one bot per license

bot_cosmetic_loadout
  one equipped cosmetic per bot/slot and the exact paid license when applicable

cosmetic_entitlements
  legacy bot-scoped claims retained only for safe upgrade/recovery

cosmetic_orders
  account, immutable pack/price snapshot, quantity, expected subtotal,
  received/refunded amounts, provider IDs, and lifecycle timestamps

cosmetic_order_items
  ordered item/name/slot/asset snapshots protected by item foreign keys

cosmetic_order_licenses
  exact order + copy + item position mapped to the exact fulfilled license

cosmetic_payment_events
  provider event ID, type, payload hash, processing status, and idempotency state

cosmetic_order_refunds
  per-refund amount and lifecycle used to compute cumulative refund state

cosmetic_subscriptions
  fixed 1999 USD/month account snapshot, provider IDs, status, period end, and
  stale-event ordering watermark

cosmetic_subscription_licenses
  exact subscription + item mapping to the ordinary per-item license it created

cosmetic_subscription_events
  signed provider event ID/hash and idempotent processing state
```

PostgreSQL composite foreign keys enforce that an assignment and paid loadout
refer to a bot linked to the same account as the license. Application checks
provide useful errors, but the database remains the final exclusivity boundary.

## Stripe fulfillment and reversal rules

Arena uses [Stripe-hosted Checkout](https://docs.stripe.com/api/checkout/sessions/create),
so payment details never enter Arena's frontend or backend. A signed paid event
must match the pending order's account, Checkout Session, currency, and minimum
server-side subtotal before fulfillment. Automatic tax may raise the final
amount. Unpaid `checkout.session.completed` events remain processing until an
asynchronous success or failure arrives.

Webhook event IDs and raw-body SHA-256 hashes are recorded independently from
the deterministic order/copy/item license references. Fulfillment and license
creation commit in one PostgreSQL transaction, making retries and concurrent
delivery safe. Arena follows Stripe's guidance to verify the raw body and
signature before acting on an event; see [Stripe webhooks](https://docs.stripe.com/webhooks?lang=node).

- A successful partial refund moves the order to `refund_review`; licenses stay
  active because Arena cannot safely infer which bundled piece was refunded.
- Successful refunds totaling the amount actually received revoke only that
  order's mapped licenses and remove their assignments/loadouts.
- A created dispute immediately revokes only that order's mapped licenses and
  becomes terminal. Late paid events never restore refunded/disputed copies.
- Failed or canceled refund updates recompute from successful refund records;
  `charge.refunded` cumulative totals do not double-count individual events.
- Subscription Checkout uses a server-owned recurring `1999 USD/month` amount.
  `active` and `trialing` events materialize one license for every active set
  item, including future items on the next inventory sync. A scheduled
  `cancel_at_period_end` keeps access while Stripe still reports `active`.
  `past_due`, `unpaid`, `paused`, canceled, and deleted states remove only the
  mapped subscription licenses and their loadouts; purchased/manual copies are
  untouched. Stripe's `incomplete_expired` state is projected as terminal
  `expired`, so it can never leave inaccessible billing in a resumable state.
- For nonterminal `customer.subscription.created` and `.updated` webhooks,
  Arena retrieves the current Subscription before changing access. Exactly one
  nondeleted item with quantity 1, `1999 USD`, and a one-month recurring interval
  is required. Any plan drift becomes recoverable `billing_mismatch`: access is
  removed while the customer can still open Billing Portal, and a later valid
  provider state restores access. Provider-observation timestamps order these
  reconciliations even when Stripe events share a one-second `created` value.
- Signed terminal cancellation/deletion payloads remain directly actionable
  during a transient Stripe API outage. Terminal-state dominance prevents any
  later nonterminal delivery from restoring ended access.

Review Stripe's [refund behavior](https://docs.stripe.com/refunds?locale=en-GB)
and set a customer-facing refund/support policy before enabling live mode.

## Launch configuration and runbook

Use `stripe-go` v86 and configure the webhook endpoint for Stripe API version
`2026-06-24.dahlia`. Subscribe to:

```text
checkout.session.completed
checkout.session.async_payment_succeeded
checkout.session.async_payment_failed
checkout.session.expired
refund.created
refund.updated
refund.failed
charge.refunded
charge.dispute.created
customer.subscription.created
customer.subscription.updated
customer.subscription.deleted
```

Configure either native verified-email auth or the dedicated customer OIDC
application first, ensure Redis is available for the fail-closed checkout
quota, then add:

```dotenv
ARENA_COSMETICS_CHECKOUT_ENABLED=true
ARENA_COSMETICS_CHECKOUT_RPM=10
ARENA_STRIPE_SECRET_KEY=sk_live_replace_me
ARENA_STRIPE_WEBHOOK_SECRETS=whsec_current,whsec_previous
ARENA_STRIPE_SUCCESS_URL=https://YOUR_ARENA_HOST/dashboard/?tab=cosmetics&checkout=success&session_id={CHECKOUT_SESSION_ID}
ARENA_STRIPE_CANCEL_URL=https://YOUR_ARENA_HOST/dashboard/?tab=cosmetics&checkout=cancel
ARENA_STRIPE_PORTAL_RETURN_URL=https://YOUR_ARENA_HOST/dashboard/?tab=cosmetics
ARENA_STRIPE_AUTOMATIC_TAX=false
```

The comma-separated webhook list supports zero-downtime secret rotation. Keep
the old secret until Stripe no longer delivers events signed with it. During a
sales pause, retain the webhook secrets, Stripe API key, and Billing Portal
return URL while any subscriptions exist. Arena retrieves Stripe's authoritative
state for nonterminal subscription events before changing access and keeps
cancellation self-service available while new checkout is disabled; startup
fails closed when that retained configuration is incomplete. This lets existing
orders finish and cancellations, delinquency, plan corrections, and renewals
reconcile safely. For an `/arena` deployment, use the prefixed dashboard and
webhook paths.

Before switching from `sk_test_...` to live credentials:

1. Provision the dedicated Stalwart mailbox/app password, persist it through
   the `nimbus-infra` Arena role, and complete one real registration email and
   one expired/replayed-link exercise. Do not use a recovery-admin credential.
2. Apply the idempotent schema migration with the release migrator and confirm
   the managed-schema preflight passes.
3. Complete a test purchase, duplicate webhook delivery, quantity-two purchase,
   asynchronous payment, partial refund, full refund, and dispute exercise.
4. Confirm the account order history and protected Admin order search show the
   same amount, currency, provider IDs, status, and fulfillment count.
5. Confirm full reversals remove the three exact licenses while another order's
   licenses remain equipped and usable.
6. Configure Stripe receipts, tax registrations/Stripe Tax policy, statement
   descriptor, customer support/refund terms, and production alerting.
7. Enable checkout, fetch `/api/v1/cosmetics/catalog`, and verify
   `checkout_enabled:true` only after the provider configuration is active.

Customer sessions remain process-local, so a restart asks the browser to sign
in again; accounts, orders, licenses, assignments, and reversals remain durable
in PostgreSQL. A multi-replica deployment should add a shared session store or
sticky sessions before horizontal scaling. Creator-submitted art still requires
a moderated asset pipeline; never render unreviewed remote content.
