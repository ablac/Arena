# Cosmetics And Fair Monetization

Arena now has a provider-neutral cosmetics foundation for bot skins, weapon
finishes, and attachments. The first implementation deliberately stops short
of taking payments: it establishes catalog, entitlement, equip, protocol, and
renderer contracts that a payment provider can fulfill later without touching
combat code.

## Fair-play boundary

Cosmetics must never change a bot's HP, movement, attack, defense, weapon,
cooldown, hitbox, collision, visibility to other bots, or protocol data used by
AI decisions.

The implementation enforces that boundary in several places:

- Cosmetic catalog and entitlement tables contain presentation fields only.
- The game engine receives only three allowlisted asset keys. It never receives
  price, rarity, ownership, or payment data.
- The spectator renderer maps those keys to fixed local procedural visuals.
  It does not load arbitrary URLs, scripts, models, or materials from catalog
  data.
- Equip requests are authenticated and paid items require a server-side
  entitlement.
- Unknown or retired asset keys fall back to standard visuals.
- Spectators can disable chassis skins, weapon finishes, or attachments in the
  existing graphics settings panel.

## Current slots and starter catalog

| Slot | Free starter options | Paid preview options |
| --- | --- | --- |
| `bot_skin` | Standard Chassis | Neon Grid, Carbon Armor |
| `weapon_skin` | Standard Weapon Finish | Solar Flare, Void Edge |
| `attachment` | None, Signal Antenna | Orbital Halo |

Paid preview entries carry planned prices, but `is_purchasable` remains false
until checkout and webhook fulfillment are configured. The public catalog's
`checkout_enabled` field therefore stays false instead of advertising a button
that cannot complete a purchase.

The public **Get Started -> Make the bot yours** panel renders this catalog,
accepts a bot API key without saving it, shows owned/locked/equipped state, and
can equip free or entitled items. The admin Operations Console includes a
provider-neutral entitlement grant/revoke panel for support and integration
testing.

## API

List the public catalog:

```bash
curl https://YOUR_ARENA_HOST/api/v1/cosmetics/catalog
```

List the authenticated bot's owned and equipped items:

```bash
curl -H "X-Arena-Key: YOUR_API_KEY" \
  https://YOUR_ARENA_HOST/api/v1/bot/cosmetics
```

Equip a free or entitled item:

```bash
curl -X PUT \
  -H "X-Arena-Key: YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"slot":"attachment","cosmetic_id":"attachment-signal-antenna"}' \
  https://YOUR_ARENA_HOST/api/v1/bot/cosmetics
```

Admin fulfillment is idempotent when the payment provider's event or order ID
is supplied as `external_reference`:

```bash
curl -X POST \
  -H "X-Admin-Token: YOUR_ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"bot_id":"BOT_UUID","cosmetic_id":"skin-neon-grid","source":"stripe","external_reference":"evt_123"}' \
  https://YOUR_ARENA_HOST/api/v1/admin/cosmetics/grants
```

Revocation uses `DELETE` on the same route. Revoking an equipped item also
removes it from the bot's equipped loadout.

## Data model

```text
cosmetic_items
  catalog identity, slot, local asset key, price metadata, sale flags

cosmetic_entitlements
  bot + cosmetic ownership, grant source, idempotency reference

bot_cosmetic_loadout
  one equipped cosmetic per bot and slot
```

Free catalog entries are treated as owned without creating entitlement rows.
This keeps starter customization usable while preserving the same equip path
paid items use.

## Payment launch work still required

Before charging real money:

1. Choose a payment provider and tax/receipt policy.
2. Add checkout creation that resolves a stable product/price ID server-side.
   Never accept a client-supplied amount.
3. Verify signed provider webhooks and grant only after a completed payment.
4. Decide purchase recovery. Today's ownership is bot-scoped, so losing a bot
   API key also loses access. A small account/login layer or signed recovery
   flow is needed before broad sales.
5. Extend the starter catalog/equip UI with provider checkout, purchase
   history, receipts, and account-level recovery.
6. Handle refunds, chargebacks, revocations, duplicate webhook delivery, and
   support grants.
7. Add a moderated asset pipeline before accepting creator-supplied designs.
   Do not let catalog records point directly at unreviewed remote assets.

This sequence keeps the first revenue feature reversible and auditable while
protecting the arena's no-pay-to-win promise.
