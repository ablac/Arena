package game

import (
	"math"
	"math/rand"

	"arena-server/internal/config"
)

func rollRoundModifier() RoundModifier {
	c := &config.C
	if c.RoundModifierChance <= 0 || rand.Float64() > c.RoundModifierChance {
		return RoundModifierNone
	}

	options := []RoundModifier{
		RoundModifierFastZone,
		RoundModifierPickupSurge,
		RoundModifierDoubleBounty,
		RoundModifierTeleportSurge,
		RoundModifierHazardStorm,
	}
	return options[rand.Intn(len(options))]
}

func effectiveZoneProfile(mod RoundModifier) (delayTicks, intervalTicks int, shrinkPercent float64, roundTotalTicks int) {
	c := &config.C
	delayTicks = int(c.ZoneShrinkDelay * float64(c.TickRate))
	intervalTicks = int(c.ZoneShrinkInterval * float64(c.TickRate))
	shrinkPercent = c.ZoneShrinkPercent
	roundTotalTicks = int(c.RoundDuration * float64(c.TickRate))

	if mod == RoundModifierFastZone {
		delayTicks = int(float64(delayTicks) * c.RoundModifierFastZoneDelayMult)
		intervalTicks = int(float64(intervalTicks) * c.RoundModifierFastZoneIntervalMult)
	}
	if delayTicks < 0 {
		delayTicks = 0
	}
	if intervalTicks < 1 {
		intervalTicks = 1
	}
	if shrinkPercent < 0 {
		shrinkPercent = 0
	}
	if shrinkPercent > 0.95 {
		shrinkPercent = 0.95
	}
	return delayTicks, intervalTicks, shrinkPercent, roundTotalTicks
}

func effectivePickupSpawnInterval(mod RoundModifier) int {
	c := &config.C
	interval := c.PickupSpawnIntervalTicks
	if mod == RoundModifierPickupSurge {
		interval = int(math.Round(float64(interval) * c.RoundModifierPickupSurgeIntervalMult))
	}
	if interval < 1 {
		interval = 1
	}
	return interval
}

func effectiveBountyRewardMultiplier(mod RoundModifier) float64 {
	if mod == RoundModifierDoubleBounty {
		return config.C.RoundModifierDoubleBountyMult
	}
	return 1
}

func effectiveTeleportProfile(mod RoundModifier) (cooldownTicks, lockTicks int) {
	c := &config.C
	cooldownTicks = c.TeleportCooldownTicks
	lockTicks = c.TeleportPadLockTicks
	if mod == RoundModifierTeleportSurge {
		cooldownTicks = int(math.Round(float64(cooldownTicks) * c.RoundModifierTeleportCooldownMult))
		lockTicks = int(math.Round(float64(lockTicks) * c.RoundModifierTeleportLockMult))
	}
	if cooldownTicks < 1 {
		cooldownTicks = 1
	}
	if lockTicks < 1 {
		lockTicks = 1
	}
	return cooldownTicks, lockTicks
}

func effectiveHazardProfile(mod RoundModifier, zone HazardZone) (onTicks, offTicks int, damagePerTick float64) {
	c := &config.C
	onTicks = zone.PulseOnTicks
	offTicks = zone.PulseOffTicks
	damagePerTick = zone.DamagePerTick

	if mod == RoundModifierHazardStorm {
		onTicks = int(math.Round(float64(onTicks) * c.RoundModifierHazardStormOnMult))
		offTicks = int(math.Round(float64(offTicks) * c.RoundModifierHazardStormOffMult))
		damagePerTick *= c.RoundModifierHazardStormDamageMult
	}
	if onTicks < 1 {
		onTicks = 1
	}
	if offTicks < 1 {
		offTicks = 1
	}
	if damagePerTick < 0 {
		damagePerTick = 0
	}
	return onTicks, offTicks, damagePerTick
}
