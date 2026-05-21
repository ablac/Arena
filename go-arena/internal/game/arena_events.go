package game

import "fmt"

func (e *GameEngine) appendArenaEvents(events ...ArenaEvent) {
	for _, ev := range events {
		if ev.ID == "" {
			continue
		}
		e.RecentEvents = append(e.RecentEvents, ev)
	}
}

func buildTeleportEvent(bot *BotState, pad TeleportPad, from, to Vec2, tick int) ArenaEvent {
	fromCopy := from
	toCopy := to
	return ArenaEvent{
		ID:           fmt.Sprintf("teleport:%s:%d:%s", bot.BotID, tick, pad.ID),
		Type:         "teleport",
		Tick:         tick,
		Position:     to,
		FromPosition: &fromCopy,
		ToPosition:   &toCopy,
		OwnerID:      bot.BotID,
		Color:        pad.Color,
	}
}

func buildMineDetonationEvent(mine Landmine, tick int) ArenaEvent {
	radius := float64(mine.BlastRadius)
	if radius <= 0 {
		radius = 1
	}
	return ArenaEvent{
		ID:       fmt.Sprintf("mine:%s:%d", mine.ID, tick),
		Type:     "mine_detonated",
		Tick:     tick,
		Position: mine.Position,
		OwnerID:  mine.OwnerID,
		Radius:   radius,
	}
}

func buildStaffDetonationEvent(impact StaffImpact, tick int) ArenaEvent {
	radius := impact.Radius
	if radius <= 0 {
		radius = 1
	}
	return ArenaEvent{
		ID:       fmt.Sprintf("staff:%s:%d:%0.0f:%0.0f", impact.OwnerID, tick, impact.Position.X(), impact.Position.Y()),
		Type:     "staff_detonated",
		Tick:     tick,
		Position: impact.Position,
		OwnerID:  impact.OwnerID,
		Color:    "#8d4dff",
		Radius:   radius,
	}
}

func buildBurnFieldSpawnEvent(field BurnField, tick int) ArenaEvent {
	return ArenaEvent{
		ID:       fmt.Sprintf("burn:%s:%d", field.ID, tick),
		Type:     "burn_field_spawned",
		Tick:     tick,
		Position: field.Position,
		OwnerID:  field.OwnerID,
		Color:    "#ff7d36",
		Radius:   field.Radius,
	}
}

func buildBowShotEvent(ownerID, color string, from, to Vec2, tick int, intensity float64) ArenaEvent {
	fromCopy := from
	toCopy := to
	return ArenaEvent{
		ID:           fmt.Sprintf("bow-shot:%s:%d:%0.0f:%0.0f", ownerID, tick, to.X(), to.Y()),
		Type:         "bow_fired",
		Tick:         tick,
		Position:     to,
		FromPosition: &fromCopy,
		ToPosition:   &toCopy,
		OwnerID:      ownerID,
		Color:        color,
		Intensity:    intensity,
	}
}

func buildBowImpactEvent(projectileID, ownerID, color string, pos Vec2, tick int, targetID string, intensity float64) ArenaEvent {
	return ArenaEvent{
		ID:       fmt.Sprintf("bow-impact:%s:%d", projectileID, tick),
		Type:     "bow_impact",
		Tick:     tick,
		Position: pos,
		OwnerID:  ownerID,
		TargetID: targetID,
		Color:    color,
		Intensity: intensity,
	}
}

func buildGrappleEvent(ownerID, targetID string, from, anchor, to Vec2, selfPull bool, tick int) ArenaEvent {
	fromCopy := from
	toCopy := to
	eventType := "grapple_pull"
	if selfPull {
		eventType = "grapple_anchor"
	}
	return ArenaEvent{
		ID:           fmt.Sprintf("grapple:%s:%d:%s:%t", ownerID, tick, targetID, selfPull),
		Type:         eventType,
		Tick:         tick,
		Position:     anchor,
		FromPosition: &fromCopy,
		ToPosition:   &toCopy,
		OwnerID:      ownerID,
		TargetID:     targetID,
		Color:        "#59f1ff",
		Radius:       0,
	}
}

func buildShieldBashEvent(attacker, target *BotState, tick int) ArenaEvent {
	fromCopy := attacker.Position
	toCopy := target.Position
	return ArenaEvent{
		ID:           fmt.Sprintf("shield-bash:%s:%s:%d", attacker.BotID, target.BotID, tick),
		Type:         "shield_bash",
		Tick:         tick,
		Position:     target.Position,
		FromPosition: &fromCopy,
		ToPosition:   &toCopy,
		OwnerID:      attacker.BotID,
		TargetID:     target.BotID,
		Color:        attacker.AvatarColor,
	}
}

func buildBackstabEvent(attacker, target *BotState, tick int) ArenaEvent {
	fromCopy := attacker.Position
	toCopy := target.Position
	return ArenaEvent{
		ID:           fmt.Sprintf("backstab:%s:%s:%d", attacker.BotID, target.BotID, tick),
		Type:         "backstab",
		Tick:         tick,
		Position:     target.Position,
		FromPosition: &fromCopy,
		ToPosition:   &toCopy,
		OwnerID:      attacker.BotID,
		TargetID:     target.BotID,
		Color:        attacker.AvatarColor,
	}
}

func buildSpearBraceEvent(attacker, target *BotState, tick int) ArenaEvent {
	fromCopy := attacker.Position
	toCopy := target.Position
	return ArenaEvent{
		ID:           fmt.Sprintf("spear-brace:%s:%s:%d", attacker.BotID, target.BotID, tick),
		Type:         "spear_brace",
		Tick:         tick,
		Position:     target.Position,
		FromPosition: &fromCopy,
		ToPosition:   &toCopy,
		OwnerID:      attacker.BotID,
		TargetID:     target.BotID,
		Color:        attacker.AvatarColor,
	}
}

func buildGrappleSlamEvent(attacker, target *BotState, tick int) ArenaEvent {
	fromCopy := attacker.Position
	toCopy := target.Position
	return ArenaEvent{
		ID:           fmt.Sprintf("grapple-slam:%s:%s:%d", attacker.BotID, target.BotID, tick),
		Type:         "grapple_slam",
		Tick:         tick,
		Position:     target.Position,
		FromPosition: &fromCopy,
		ToPosition:   &toCopy,
		OwnerID:      attacker.BotID,
		TargetID:     target.BotID,
		Color:        attacker.AvatarColor,
		Intensity:    1.25,
	}
}
