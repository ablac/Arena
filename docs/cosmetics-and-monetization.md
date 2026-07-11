# Cosmetics And Fair Monetization

Arena has a provider-neutral foundation for bot skins, weapon finishes, and
attachments. Checkout is intentionally disabled until a payment provider,
tax/receipt policy, and signed webhook flow are selected. The catalog,
verified-email account, per-copy license, bot assignment, equip, protocol, and
renderer boundaries are in place first.

## Ownership and assignment rules

Purchases belong to a verified customer email account, never to an API key.
An API key is only a one-time proof that the signed-in customer controls a bot.

- Every purchased copy is a separate cosmetic license with a stable license ID.
- A license may be assigned to zero or one linked bot at a time.
- Assignment and equip are separate. Moving a license does not silently replace
  the destination bot's current cosmetic in that slot.
- Only an active license assigned to that bot can be equipped or rendered.
- Unassigning, unlinking a bot, refunding, charging back, or revoking a license
  removes that exact paid loadout. The account's other licenses are untouched.
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

## Customer authentication

Customer login is a separate OIDC client from admin login. Customer sessions
cannot authorize admin routes. The customer flow requires a non-empty
`email_verified=true` claim, browser-bound state, nonce, PKCE, a distinct
HttpOnly cookie, and a CSRF token for mutations.

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

## Current catalog

| Slot | Free starter options | Paid preview options |
| --- | --- | --- |
| `bot_skin` | Standard Chassis | Neon Grid, Carbon Armor |
| `weapon_skin` | Standard Weapon Finish | Solar Flare, Void Edge |
| `attachment` | None, Signal Antenna | Orbital Halo |

The launch catalog is organized into `chassis`, `weapon-finishes`,
`attachments`, and `starter-packs`. Two curated packs expose the intended
launch price without enabling payment:

| Pack | Contents | Planned price |
| --- | --- | --- |
| Neon Signal Pack | Neon Grid, Solar Flare, Signal Antenna | $0.99 USD |
| Void Orbit Pack | Carbon Armor, Void Edge, Orbital Halo | $0.99 USD |

Individual preview items retain reference price metadata, but are not directly
purchasable. Pack rows can be marked sale-ready by an administrator while the
public `checkout_enabled` value remains false. The public page therefore labels
every paid offer Coming soon and never invents a checkout or payment endpoint.

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

Customer account routes use the OIDC session cookie. Every POST, PUT, and
DELETE also requires the `X-CSRF-Token` returned by the session endpoint.

```text
GET    /api/v1/dashboard/login
POST   /api/v1/dashboard/logout
GET    /api/v1/account/session
GET    /api/v1/account/cosmetics
POST   /api/v1/account/bots
DELETE /api/v1/account/bots/{bot_id}
PUT    /api/v1/account/cosmetic-licenses/{license_id}/assignment
DELETE /api/v1/account/cosmetic-licenses/{license_id}/assignment
PUT    /api/v1/account/bots/{bot_id}/cosmetics
```

Linking submits `{"api_key":"arena_..."}`. Assignment submits
`{"bot_id":"BOT_UUID"}`. Explicit account equip submits
`{"license_id":"LICENSE_UUID"}`.

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
  -d '{"email":"buyer@example.com","cosmetic_id":"skin-neon-grid","source":"manual","external_reference":"ticket-123-copy-1"}' \
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
```

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

account_bot_links
  bots proven by API-key possession; each bot links to at most one account

cosmetic_items
  catalog identity, slot, allowlisted asset key, price metadata, sale flags

cosmetic_categories
  ordered admin-managed group metadata; inactive categories stay out of public reads

cosmetic_packs
  ordered bundle metadata and planned minor-unit price

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
```

PostgreSQL composite foreign keys enforce that an assignment and paid loadout
refer to a bot linked to the same account as the license. Application checks
provide useful errors, but the database remains the final exclusivity boundary.

## Payment launch work still required

Before charging real money:

1. Choose a payment provider and tax/receipt policy.
2. Create checkout only from an authenticated customer session. Resolve
   product IDs and amounts server-side and attach the stable account ID/email.
3. Verify signed webhooks against the bounded raw request body. Track webhook
   event idempotency separately from the order-line/copy reference used for a
   license.
4. Support quantity correctly: quantity two creates two license rows. Make
   failed webhook attempts retryable.
5. Map refunds and chargebacks to the exact license copy and keep terminal
   states from being resurrected by late paid events.
6. Add purchase history, receipts, email-change/recovery support, durable
   customer sessions, and account-driven API-key rotation.
7. Add a moderated asset pipeline before accepting creator-submitted designs.
   Never render unreviewed remote assets or executable content.

This sequence keeps monetization auditable and recoverable while protecting
Arena's no-pay-to-win promise.
