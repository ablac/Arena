package game

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
