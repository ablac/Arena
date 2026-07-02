package game

import "arena-server/internal/config"

// AntiTeamTracker detects bots that stay near each other without fighting
// and applies a damage penalty to discourage teaming.
type AntiTeamTracker struct {
	// proximity tracks how many ticks each pair of bots has been near each
	// other without either attacking the other. Key is "idA:idB" (sorted).
	proximity map[string]int
}

// NewAntiTeamTracker creates a fresh tracker.
func NewAntiTeamTracker() *AntiTeamTracker {
	return &AntiTeamTracker{
		proximity: make(map[string]int),
	}
}

// Clear resets all tracking (e.g. at round start).
func (at *AntiTeamTracker) Clear() {
	at.proximity = make(map[string]int)
}

// pairKey returns a deterministic key for a bot pair.
func pairKey(a, b string) string {
	if a < b {
		return a + ":" + b
	}
	return b + ":" + a
}

// RecordAttack resets the proximity counter between attacker and target,
// since they are actively fighting.
func (at *AntiTeamTracker) RecordAttack(attackerID, targetID string) {
	key := pairKey(attackerID, targetID)
	delete(at.proximity, key)
}

// Update checks all alive bot pairs. For each pair within the teaming
// detection radius, the counter increments. If either bot attacked the
// other this tick the counter was already reset by RecordAttack.
// Returns a list of bot IDs that should take anti-team damage this tick.
func (at *AntiTeamTracker) Update(bots map[string]*BotState, grid *SpatialGrid) []string {
	c := &config.C
	radius := c.AntiTeamRadius
	threshold := c.AntiTeamThresholdTicks
	if radius <= 0 || threshold <= 0 {
		return nil
	}

	// Track which pairs are currently near each other.
	activePairs := make(map[string]bool)

	var nearbyIDs []string
	for _, bot := range bots {
		if !bot.IsAlive {
			continue
		}

		nearbyIDs = grid.QueryRadiusInto(bot.Position, radius, nearbyIDs[:0])
		for _, otherID := range nearbyIDs {
			if otherID == bot.BotID {
				continue
			}
			other, ok := bots[otherID]
			if !ok || !other.IsAlive {
				continue
			}

			// Team modes: teammates are supposed to stick together.
			if SameTeam(bot, other) {
				continue
			}

			key := pairKey(bot.BotID, otherID)
			if activePairs[key] {
				continue // already counted this pair
			}
			activePairs[key] = true
			at.proximity[key]++
		}
	}

	// Decay pairs that are no longer near each other.
	for key := range at.proximity {
		if !activePairs[key] {
			at.proximity[key] -= 2 // decay faster than accumulation
			if at.proximity[key] <= 0 {
				delete(at.proximity, key)
			}
		}
	}

	// Find bots that should be penalised.
	penalised := make(map[string]bool)
	for key, ticks := range at.proximity {
		if ticks >= threshold {
			// Extract both bot IDs from the key.
			for i := 0; i < len(key); i++ {
				if key[i] == ':' {
					penalised[key[:i]] = true
					penalised[key[i+1:]] = true
					break
				}
			}
		}
	}

	result := make([]string, 0, len(penalised))
	for id := range penalised {
		result = append(result, id)
	}
	return result
}
