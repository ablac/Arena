/**
 * Simple Bot — minimal example that attacks the nearest enemy.
 *
 * This is the simplest possible bot, ideal as a starting template.
 *
 * Usage:
 *   node simple-bot.js <API_KEY> [SERVER_URL]
 *
 * Loadout: sword, balanced stats (5/5/5/5)
 */

import ArenaBot from '../src/ArenaBot.js';

class SimpleBot extends ArenaBot {
  /**
   * Attack the nearest enemy, or idle if none are visible.
   * @param {object} state - Our bot's current state.
   * @param {object[]} nearby - Nearby entities.
   * @param {object} safeZone - Safe zone boundaries.
   * @returns {Promise<object>} Action to perform.
   */
  async onTick(state, nearby, safeZone) {
    const enemy = this.closestEnemy(nearby);
    if (enemy) return this.attack(enemy.id);
    return this.idle();
  }
}

// ─── Entry point ──────────────────────────────────────────────────────

const apiKey = process.argv[2];
const serverUrl = process.argv[3];

if (!apiKey) {
  console.error('Usage: node simple-bot.js <API_KEY> [SERVER_URL]');
  process.exit(1);
}

const bot = new SimpleBot(apiKey, serverUrl || undefined);
bot.setLoadout('sword', { hp: 5, speed: 5, attack: 5, defense: 5 });
bot.run();
