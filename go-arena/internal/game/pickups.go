package game

import (
	"math/rand"

	"arena-server/internal/config"

	"github.com/google/uuid"
)

// MaybeSpawnPickup spawns a random pickup inside the safe zone at regular
// intervals, up to the configured maximum number of active pickups.
func MaybeSpawnPickup(pickups *[]Pickup, arena *ArenaMap, tickCount int) {
	MaybeSpawnPickupAtInterval(pickups, arena, tickCount, config.C.PickupSpawnIntervalTicks)
}

// MaybeSpawnPickupAtInterval spawns a random pickup using the provided spawn
// cadence instead of the global default.
func MaybeSpawnPickupAtInterval(pickups *[]Pickup, arena *ArenaMap, tickCount int, intervalTicks int) {
	c := &config.C

	if intervalTicks <= 0 || tickCount%intervalTicks != 0 {
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
		PickupHazardKey,
		PickupOverdriveCore,
		PickupGrappleCharge,
		PickupRelayBattery,
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
		value = c.PickupCooldownShardMult
	case PickupBountyToken:
		value = float64(c.PickupBountyTokenPoints)
	case PickupHazardKey:
		value = 1
	case PickupOverdriveCore:
		value = c.PickupOverdriveDamageMult
	case PickupGrappleCharge:
		value = float64(c.PickupGrappleChargeAmount)
	case PickupRelayBattery:
		value = float64(c.PickupRelayBatteryBonusProgress)
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
		bot.ActiveEffects = removeEffectByName(bot.ActiveEffects, "cooldown_shard")
		bot.ActiveEffects = append(bot.ActiveEffects, Effect{
			Name:           "cooldown_shard",
			RemainingTicks: c.PickupCooldownShardTicks,
			Value:          pickup.Value,
		})
	case PickupBountyToken:
		bot.BountyTokenBonus = int(pickup.Value)
		bot.ActiveEffects = removeEffectByName(bot.ActiveEffects, "bounty_token")
		bot.ActiveEffects = append(bot.ActiveEffects, Effect{
			Name:           "bounty_token",
			RemainingTicks: c.PickupBountyTokenTicks,
			Value:          pickup.Value,
		})
	case PickupHazardKey:
		bot.ActiveEffects = removeEffectByName(bot.ActiveEffects, "hazard_key")
		bot.ActiveEffects = append(bot.ActiveEffects, Effect{
			Name:           "hazard_key",
			RemainingTicks: c.PickupHazardKeyTicks,
			Value:          1,
		})
	case PickupOverdriveCore:
		bot.ActiveEffects = removeEffectByName(bot.ActiveEffects, "overdrive_core")
		bot.ActiveEffects = append(bot.ActiveEffects, Effect{
			Name:           "overdrive_core",
			RemainingTicks: c.PickupOverdriveTicks,
			Value:          c.PickupOverdriveDamageMult,
			AuxValue:       c.PickupOverdriveCooldownMult,
		})
	case PickupGrappleCharge:
		bot.GrappleCharges += int(pickup.Value)
		bot.GrappleCooldown = 0
	case PickupRelayBattery:
		bot.ActiveEffects = removeEffectByName(bot.ActiveEffects, "relay_battery")
		bot.ActiveEffects = append(bot.ActiveEffects, Effect{
			Name:           "relay_battery",
			RemainingTicks: c.PickupRelayBatteryTicks,
			Value:          pickup.Value,
		})
	}

	bot.RoundPickups++
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

func hasEffectByName(effects []Effect, name string) bool {
	for _, e := range effects {
		if e.Name == name && e.RemainingTicks > 0 {
			return true
		}
	}
	return false
}

func effectRemainingTicks(effects []Effect, name string) int {
	for _, e := range effects {
		if e.Name == name && e.RemainingTicks > 0 {
			return e.RemainingTicks
		}
	}
	return 0
}

func effectValueByName(effects []Effect, name string) float64 {
	for _, e := range effects {
		if e.Name == name && e.RemainingTicks > 0 {
			return e.Value
		}
	}
	return 0
}
