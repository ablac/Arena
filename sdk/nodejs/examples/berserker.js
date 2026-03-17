/**
 * Berserker Bot — charges the closest enemy and attacks relentlessly.
 *
 * Usage:
 *   node berserker.js <API_KEY> [SERVER_URL]
 *
 * Loadout: sword, HP 3 / Speed 4 / Attack 10 / Defense 3
 */

import ArenaBot from '../src/ArenaBot.js';
import { distance } from '../src/helpers.js';

class BerserkerBot extends ArenaBot {
  /**
   * Every tick: find the closest enemy, charge at it, and attack.
   * Positions are [col, row] integers; distances are Chebyshev (tile count).
   * @param {object} state - Our bot's current state.
   * @param {object[]} nearby - Nearby entities.
   * @param {object} safeZone - Safe zone boundaries.
   * @returns {Promise<object>} Action to perform.
   */
  async onTick(state, nearby, safeZone) {
    const enemy = this.closestEnemy(nearby);
    if (!enemy) return this.idle();

    const dist = distance(state.position, enemy.position);

    // Adjacent tile — attack
    if (dist <= 1) {
      return this.attack(enemy.id);
    }

    // Otherwise charge toward the enemy
    return this.moveToward(state.position, enemy.position);
  }

  /** @param {object} deathInfo */
  async onDeath(deathInfo) {
    console.log(
      `[Berserker] Killed by ${deathInfo.killed_by} ` +
      `with ${deathInfo.weapon_used}. Kills this life: ${deathInfo.your_kills_this_life}`
    );
  }

  /** @param {object} roundInfo */
  async onRoundEnd(roundInfo) {
    const stats = roundInfo.your_stats || {};
    console.log(
      `[Berserker] Round ${roundInfo.round_number} — ` +
      `Kills: ${stats.kills ?? 0}, Deaths: ${stats.deaths ?? 0}`
    );
  }
}

// ─── Entry point ──────────────────────────────────────────────────────

const apiKey = process.argv[2];
const serverUrl = process.argv[3];

if (!apiKey) {
  console.error('Usage: node berserker.js <API_KEY> [SERVER_URL]');
  process.exit(1);
}

const bot = new BerserkerBot(apiKey, serverUrl || undefined);
bot.setLoadout('sword', { hp: 3, speed: 4, attack: 10, defense: 3 });
bot.run();
