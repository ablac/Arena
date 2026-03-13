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

	if tickCount%c.PickupSpawnIntervalTicks != 0 {
		return
	}
	if len(*pickups) >= c.PickupMaxActive {
		return
	}

	// Choose a random pickup type.
	types := []PickupType{PickupHealthPack, PickupSpeedBoost, PickupDamageBoost, PickupShieldBubble}
	pType := types[rand.Intn(len(types))]

	pos := arena.GetSpawnPoint()

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
	c := &config.C
	collectDist := c.PickupCollectRadius + c.BotRadius

	for _, bot := range bots {
		if !bot.IsAlive {
			continue
		}

		// Iterate backwards so removals don't skip entries.
		for i := len(*pickups) - 1; i >= 0; i-- {
			p := (*pickups)[i]
			if bot.Position.DistanceTo(p.Position) <= collectDist {
				applyPickupEffect(bot, p)
				*pickups = append((*pickups)[:i], (*pickups)[i+1:]...)
			}
		}
	}
}

// CollectByAction attempts to collect a specific pickup by ID for a bot.
// Returns true if the pickup was found and collected.
func CollectByAction(bot *BotState, itemID string, pickups *[]Pickup) bool {
	c := &config.C
	collectDist := c.PickupCollectRadius + c.BotRadius + 5.0

	for i, p := range *pickups {
		if p.ID != itemID {
			continue
		}
		if bot.Position.DistanceTo(p.Position) > collectDist {
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
