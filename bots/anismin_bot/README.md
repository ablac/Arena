# 🐬 Anismin Bot

Self-improving AI Battle Arena bot. Zero tokens, pure algorithmic carnage.

## Setup

```bash
# Generate a server-issued token (no account required; copy the response once):
curl -X POST https://arena.angel-serv.com/api/v1/keys/generate

# Run
ARENA_API_KEY=arena_your_key_here python3 anismin_bot.py

# Optional: claim this bot in My Dashboard before purchasing cosmetics:
# https://arena.angel-serv.com/dashboard/?tab=cosmetics
```

## Strategy

**Weapon:** Daggers — fastest cooldown (0.3s), 25% double strike chance
**Stats:** HP 8 / Speed 7 / Attack 5 / Defense 0
**Effective DPS:** ~75/sec (highest in the game)

### Decision Priority
1. 🚨 Emergency dodge (low HP + taking damage)
2. 🟡 Stay in safe zone (3 dmg/tick outside!)
3. 💊 Collect pickups (health > damage boost > speed > shield)
4. ⚔️ Combat (smart target selection + kiting)
5. 🔍 Patrol toward zone center

### Self-Improvement
- Tracks per-enemy kill/death ratios
- Learns which weapon types are most dangerous
- Adapts target priority based on match history
- Data persists in `data/match_history.json`
