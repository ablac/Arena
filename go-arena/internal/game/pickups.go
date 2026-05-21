package game

import (
	"math/rand"

	"arena-server/internal/config"

	"github.com/google/uuid"
)

// MaybeSpawnPickup spawns a random pickup inside the safe zone at regular
// intervals, up to the configured maximum number of active pickups.
func MaybeSpawnPickup(pickups *[]Pickup, arena *ArenaMap, tickCount int) {
	c := &config.C

	if c.PickupSpawnIntervalTicks <= 0 || tickCount%c.PickupSpawnIntervalTicks != 0 {
		return
	}
	if len(*pickups) >= c.PickupMaxActive {
		return
	}

	// Choose a random pickup type.
	types := []PickupType{
		PickupHealthPack,
		PickupHealthPack,
		PickupSpeedBoost,
		PickupDamageBoost,
		PickupDamageBoost,
		PickupShieldBubble,
		PickupGravityWell,
		PickupCooldownShard,
		PickupCooldownShard,
		PickupBountyToken,
	}
	pType := types[rand.Intn(len(types))]

	pos := arena.GetSpawnPoint()
	// Snap pickup position to grid cell centre; retry if it lands in a wall.
	if ActiveTerrain != nil {
		cell := ActiveTerrain.WorldToGrid(pos)
		for retries := 0; retries < 10 && ActiveTerrain.IsBlocked(cell[0], cell[1]); retries++ {
			pos = arena.GetSpawnPoint()
			cell = ActiveTerrain.WorldToGrid(pos)
		}
		if ActiveTerrain.IsBlocked(cell[0], cell[1]) {
			return // give up — no valid spawn found
		}
		pos = ActiveTerrain.GridToWorld(cell)
	}

	var value float64
	switch pType {
	case PickupHealthPack:
		value = c.PickupHealthAmount
	case PickupSpeedBoost:
		value = c.PickupSpeedBoostMult
	case PickupDamageBoost:
		value = c.PickupDamageBoostMult
	case PickupShieldBubble:
		value = c.PickupShieldBubbleHP
	case PickupGravityWell:
		value = 1 // 1 charge
	case PickupCooldownShard:
		value = c.PickupCooldownShardWeaponPct
	case PickupBountyToken:
		value = float64(c.PickupBountyTokenPoints)
	}

	*pickups = append(*pickups, Pickup{
		ID:       uuid.New().String(),
		Type:     pType,
		Position: pos,
		Value:    value,
	})
}

// CheckAutoCollect checks each alive bot against every active pickup and
// auto-collects any pickup within collection range.
func CheckAutoCollect(bots map[string]*BotState, pickups *[]Pickup) {
	for _, bot := range bots {
		if !bot.IsAlive {
			continue
		}

		// Iterate backwards so removals don't skip entries.
		for i := len(*pickups) - 1; i >= 0; i-- {
			p := (*pickups)[i]
			// Grid-based: collect if in same cell or adjacent.
			if IsInRange(bot.Position, p.Position, 0) {
				applyPickupEffect(bot, p)
				*pickups = append((*pickups)[:i], (*pickups)[i+1:]...)
			}
		}
	}
}

// CollectByAction attempts to collect a specific pickup by ID for a bot.
// Returns true if the pickup was found and collected.
func CollectByAction(bot *BotState, itemID string, pickups *[]Pickup) bool {
	for i, p := range *pickups {
		if p.ID != itemID {
			continue
		}
		// Grid-based: must be within 1 tile to collect.
		if !IsInRange(bot.Position, p.Position, 1) {
			return false
		}
		applyPickupEffect(bot, p)
		*pickups = append((*pickups)[:i], (*pickups)[i+1:]...)
		return true
	}
	return false
}

// applyPickupEffect applies a pickup's effect to the bot.
func applyPickupEffect(bot *BotState, pickup Pickup) {
	c := &config.C

	switch pickup.Type {
	case PickupHealthPack:
		bot.HP += pickup.Value
		if bot.HP > bot.MaxHP {
			bot.HP = bot.MaxHP
		}
	case PickupSpeedBoost:
		// Replace existing speed boost instead of stacking
		bot.ActiveEffects = removeEffectByName(bot.ActiveEffects, "speed_boost")
		bot.ActiveEffects = append(bot.ActiveEffects, Effect{
			Name:           "speed_boost",
			RemainingTicks: c.PickupSpeedBoostTicks,
			Value:          pickup.Value,
		})
	case PickupDamageBoost:
		// Replace existing damage boost instead of stacking
		bot.ActiveEffects = removeEffectByName(bot.ActiveEffects, "damage_boost")
		bot.ActiveEffects = append(bot.ActiveEffects, Effect{
			Name:           "damage_boost",
			RemainingTicks: c.PickupDamageBoostTicks,
			Value:          pickup.Value,
		})
	case PickupShieldBubble:
		bot.ShieldAbsorb += pickup.Value
	case PickupGravityWell:
		bot.GravityWellCharge++
	case PickupCooldownShard:
		applyCooldownShard(bot)
	case PickupBountyToken:
		bot.BountyTokenBonus = int(pickup.Value)
		bot.ActiveEffects = removeEffectByName(bot.ActiveEffects, "bounty_token")
		bot.ActiveEffects = append(bot.ActiveEffects, Effect{
			Name:           "bounty_token",
			RemainingTicks: c.PickupBountyTokenTicks,
			Value:          pickup.Value,
		})
	}

	bot.RoundPickups++
}

func applyCooldownShard(bot *BotState) {
	reducePct := clamp01(config.C.PickupCooldownShardWeaponPct)
	if bot.CooldownRemaining > 0 {
		bot.CooldownRemaining *= (1.0 - reducePct)
		if bot.CooldownRemaining < 0 {
			bot.CooldownRemaining = 0
		}
	}

	abilityPct := clamp01(config.C.PickupCooldownShardAbilityPct)
	if bot.ShoveCooldown > 0 {
		bot.ShoveCooldown *= (1.0 - abilityPct)
		if bot.ShoveCooldown < 0 {
			bot.ShoveCooldown = 0
		}
	}
	if bot.GrappleCooldown > 0 {
		bot.GrappleCooldown *= (1.0 - abilityPct)
		if bot.GrappleCooldown < 0 {
			bot.GrappleCooldown = 0
		}
	}
	if bot.DodgeCooldown > 0 {
		newTicks := int(float64(bot.DodgeCooldown) * (1.0 - abilityPct))
		if newTicks < 0 {
			newTicks = 0
		}
		bot.DodgeCooldown = newTicks
	}
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// removeEffectByName filters out all effects with the given name.
func removeEffectByName(effects []Effect, name string) []Effect {
	result := effects[:0]
	for _, e := range effects {
		if e.Name != name {
			result = append(result, e)
		}
	}
	return result
}
