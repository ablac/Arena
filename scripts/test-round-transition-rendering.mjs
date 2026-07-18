import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const { GameplayRenderer } = await import('../frontend/js/renderer/gameplay.js');
const engineSource = readFileSync(new URL('../frontend/js/renderer/engine.js', import.meta.url), 'utf8');

const gameplay = Object.create(GameplayRenderer.prototype);
gameplay._roundTransitionActive = false;
gameplay.bountyTargetId = 'winner-id';
gameplay.bountyBots = [{ bot_id: 'winner-id', is_alive: true, position: [10, 20] }];
gameplay.bountyGroup = {
  ring: { visibility: 1 },
  sparkle: { emitRate: 18 },
};

gameplay.beginRoundTransition();
assert.equal(gameplay._roundTransitionActive, true, 'round_end must suspend bounty rendering');
assert.equal(gameplay.bountyTargetId, null, 'round_end must release the latched bounty target');
assert.deepEqual(gameplay.bountyBots, [], 'round_end must discard stale fallback bot positions');
assert.equal(gameplay.bountyGroup.ring.visibility, 0, 'round_end must hide the crown immediately');
assert.equal(gameplay.bountyGroup.sparkle.emitRate, 0, 'round_end must stop crown particles immediately');

gameplay.endRoundTransition();
assert.equal(gameplay._roundTransitionActive, false, 'the next round must restore bounty rendering');

assert.match(
  engineSource,
  /if \(state\.type === 'round_end'\)[\s\S]*_beginRoundTransition\(state\)/,
  'the typed round_end path must enter renderer transition ownership',
);
assert.match(
  engineSource,
  /if \(self\.botRenderer && !self\._roundTransitionActive\)[\s\S]*self\.botRenderer\.interpolate\(\)/,
  'normal bot interpolation must stop while the round transition owns the winner pose',
);
assert.match(
  engineSource,
  /_maybeEndRoundTransition\(state\)[\s\S]*this\.gameplayRenderer\.update\(state\)/,
  'only a newer authoritative arena state may restore normal gameplay rendering',
);
assert.match(
  engineSource,
  /this\.gameplayRenderer = new GameplayRenderer\(scene\);[\s\S]{0,500}if \(this\._roundTransitionActive\) this\.gameplayRenderer\.beginRoundTransition\(\)/,
  'a scene rebuild during intermission must keep stale bounty visuals suspended',
);

console.log('round transition freezes bots and clears stale bounty visuals');
