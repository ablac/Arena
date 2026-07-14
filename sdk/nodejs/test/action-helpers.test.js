import assert from 'node:assert/strict';
import test from 'node:test';

import ArenaBot from '../src/ArenaBot.js';

test('staff helpers use the documented target_position field', () => {
  const bot = new ArenaBot('test-key');

  // The server rejects an attack action carrying both target and
  // target_position (exactly one aim mode is allowed), so attack() must
  // drop targetId in favor of targetPosition when both are given.
  assert.deepEqual(bot.attack('enemy', [12, 7]), {
    action: 'attack',
    target_position: [12, 7],
  });
  assert.deepEqual(bot.attack('enemy'), {
    action: 'attack',
    target: 'enemy',
  });
  assert.deepEqual(bot.staffAttack([4, 9]), {
    action: 'attack',
    target_position: [4, 9],
  });
});
