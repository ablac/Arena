package game

import "sort"

// UpdateBotCosmetics replaces the presentation-only cosmetic asset keys for
// a connected or waiting bot. The map is copied so request handlers cannot
// mutate live game state after releasing the engine lock.
func (e *GameEngine) UpdateBotCosmetics(botID string, cosmetics map[string]string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()

	copyLoadout := func() map[string]string {
		result := make(map[string]string, len(cosmetics))
		for slot, assetKey := range cosmetics {
			result[slot] = assetKey
		}
		return result
	}

	if bot, ok := e.Bots[botID]; ok {
		bot.Cosmetics = copyLoadout()
		return true
	}
	if bot, ok := e.WaitingBots[botID]; ok {
		bot.Cosmetics = copyLoadout()
		return true
	}
	return false
}

// ConnectedBotIDs returns one stable snapshot of active and waiting bot IDs.
// Catalog administration and terminal payment reversals use it to invalidate
// presentation-only loadouts immediately. The copy keeps DB work outside the
// engine lock and deduplicates the short reconnect overlap where a bot identity
// may appear in both maps.
func (e *GameEngine) ConnectedBotIDs() []string {
	e.mu.RLock()
	defer e.mu.RUnlock()

	unique := make(map[string]struct{}, len(e.Bots)+len(e.WaitingBots))
	for botID := range e.Bots {
		unique[botID] = struct{}{}
	}
	for botID := range e.WaitingBots {
		unique[botID] = struct{}{}
	}
	result := make([]string, 0, len(unique))
	for botID := range unique {
		result = append(result, botID)
	}
	sort.Strings(result)
	return result
}
