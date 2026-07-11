import assert from 'node:assert/strict';
import test from 'node:test';

import ArenaBot from '../src/ArenaBot.js';

test('staff helpers use the documented target_position field', () => {
  const bot = new ArenaBot('test-key');

  assert.deepEqual(bot.attack('enemy', [12, 7]), {
    action: 'attack',
    target: 'enemy',
    target_position: [12, 7],
  });
  assert.deepEqual(bot.staffAttack([4, 9]), {
    action: 'attack',
    target_position: [4, 9],
  });
});
