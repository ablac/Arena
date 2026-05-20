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
