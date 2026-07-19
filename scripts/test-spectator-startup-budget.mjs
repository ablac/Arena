import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const read = (path) => readFileSync(new URL(path, import.meta.url), 'utf8');
const desktop = read('../frontend/index.html');
const mobile = read('../frontend/m/index.html');
const bots = read('../frontend/js/renderer/bots.js');
const botBody = read('../frontend/js/renderer/bot-body.js');
const worldHud = read('../frontend/js/renderer/world-hud.js');

for (const [route, html] of [['desktop', desktop], ['mobile', mobile]]) {
  assert.doesNotMatch(html, /babylonjs-gui|babylon\.gui/i,
    `${route} spectator must not eagerly load Babylon GUI`);
  const eagerBabylonScripts = [...html.matchAll(/<script[^>]+src="[^"]*babylon[^\"]*"/gi)];
  assert.equal(eagerBabylonScripts.length, 1,
    `${route} spectator is budgeted for one eager Babylon runtime script`);
}

assert.doesNotMatch(bots, /BABYLON\.GUI|getGuiTexture|summaryPanel|summaryText/,
  'selected-bot details must not recreate a fullscreen Babylon GUI texture');
assert.doesNotMatch(botBody, /AdvancedDynamicTexture|BABYLON\.GUI|getGuiTexture/,
  'bot construction must remain independent of Babylon GUI');
assert.match(worldHud, /showWorldSelection/);
assert.match(worldHud, /hideWorldSelection/);

console.log('spectator startup budget keeps Babylon GUI out of eager routes');
