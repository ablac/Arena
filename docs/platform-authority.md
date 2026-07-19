# Platform Authority Migration

Arena is adopting the shared Angel-Serv identity and cosmetics platform through
an in-process, same-database strangler. The migration keeps the existing
PostgreSQL records authoritative while callers move behind one port. It does
not copy ownership state, dual-write records, or enable checkout.

## W1b.1 boundary

W1b.1 merged to Arena `main` in
[`ablac/Arena#213`](https://github.com/ablac/Arena/pull/213) at merge commit
`f9207348665f48adfc3e112a36515c4573c8c609`.

`go-arena/internal/platform` defines the first authority facets and a
`PostgresAuthority` adapter over Arena's existing transactions. Router wiring
passes the same adapter to:

- verified OIDC customer identity binding;
- public and administrative catalog operations;
- customer inventory;
- account-agent link and unlink;
- license assignment, grant, revocation, and administrator membership
  reconciliation.

Arena's existing `bot_id` is the stable platform `agent_id`; this checkpoint
does not mint replacement identities.

The following state remains private to Arena and is intentionally outside the
authority port:

- bot API credentials and account-key issuance;
- customer cookies and durable sessions;
- gameplay profile data and bot configuration;
- allowlisted local cosmetic asset keys;
- bot cosmetic inventory projections and `bot_cosmetic_loadout` equip state;
- live game-engine visual refreshes.

The adapter currently returns legacy Arena database projections so existing
HTTP responses remain byte-compatible. Later versioned platform handlers will
map those records to the operational cross-game contract rather than exposing
database types as a public service API.

## Preserved recovery paths

`cosmetic_entitlements`, unclaimed legacy licenses, and deterministic claim
logic remain in place. Link and assignment transactions still perform their
existing recovery and Arena loadout cleanup work atomically. Those crossings
are temporary compatibility behavior, not permission to create another copy of
the ownership ledger.

## Required follow-up before service extraction

W1b.1 establishes dependency direction only.

### W1b.2 operational metadata checkpoint

The next checkpoint remains in-process and on the same database. It must add:

1. durable agent and game-profile enrollment metadata while preserving every
   Arena bot ID;
2. platform-supplied `maximum_agents` and transactionally computed
   `current_agents` (never Arena's API-key limit);
3. resource revisions, idempotency records, durable link history, and an
   ordered, bounded change feed.

W1b.2 evidence must cover authentic legacy backfill, restart, rollback and
reconciliation, concurrent mutations, and large irrelevant histories with
index-compatible bounded reads. It must preserve `cosmetic_entitlements`,
deterministic legacy-license recovery, and the current single writable
authority. Migration and reconciliation must prove they never reactivate a
refunded, revoked, chargeback, or expired license. W1b.2 does not add a second
ownership store, dual-write records, expose platform HTTP handlers, or
physically extract a service.

### Later W1b checkpoints

Later checkpoints must add and prove the remaining operational contract before
service extraction:

1. authority-owned control-proof consumption for account-agent linking;
2. license lifecycle reconciliation that never reactivates a terminal
   refunded, revoked, chargeback, or expired license;
3. provider-neutral fulfillment and subscription paths through the same
   authority;
4. service-bearer authentication and the versioned platform HTTP adapter;
5. checkpoint-specific backfill, restart, rollback, concurrency, and
   large-history verification before any physical database or service split.

The native email verification transaction, Stripe order/subscription
orchestration, and demo-bot legacy entitlement grants are explicit remaining
legacy crossings. They stay on existing records until their authority methods
can preserve the current transactional safety and recovery behavior.
