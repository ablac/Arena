import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';

const source = readFileSync(new URL('../frontend/js/renderer/bots.js', import.meta.url), 'utf8');
const {BotRenderer, actionTickStarted, cooldownActionStarted} = await import(
  new URL('../frontend/js/renderer/bots.js?forge-action-edge-test', import.meta.url)
);

assert.equal(typeof cooldownActionStarted, 'function',
  'the renderer must expose its cooldown-edge predicate for behavior testing');
assert.equal(cooldownActionStarted('grapple', '', 4, 0, 'grapple'), true,
  'a newly accepted grapple must create one animation/effect edge');
assert.equal(cooldownActionStarted('grapple', 'grapple', 3.9, 4, 'grapple'), false,
  'repeated snapshots of the same grapple must not replay its pose or effect');
assert.equal(cooldownActionStarted('grapple', 'grapple', 4, 0, 'grapple'), true,
  'a later grapple must be detected by its cooldown rising again');
assert.equal(actionTickStarted('shove', 'shove', 91, 90, false), true,
  'a new authoritative action tick must emit one shove edge');
assert.equal(actionTickStarted('shove', 'shove', 91, 91, true), false,
  'the same shove tick must not replay even if a fallback signal remains true');
assert.equal(actionTickStarted('dodge', 'dodge', 120, 119, false), true,
  'a new authoritative action tick must emit one dodge edge');
assert.equal(actionTickStarted('grapple', 'grapple', 140, 140, true), false,
  'the same grapple tick must not replay its pose or effect');
assert.equal(actionTickStarted('dodge', 'dodge', Number.NaN, Number.NaN, true), true,
  'older spectator payloads must retain a conservative rising-edge fallback');
assert.match(source,
  /grappleJustStarted[\s\S]{0,350}cooldownActionStarted\([\s\S]{0,220}'grapple'[\s\S]{0,700}target_position[\s\S]{0,700}triggerForgeAttack[\s\S]{0,700}onGrapple/,
  'the grapple edge must animate once and support both bot and anchor-position targets');
assert.match(source, /const shoveJustStarted[\s\S]{0,360}actionTickStarted\([\s\S]{0,180}'shove'/,
  'shove pose and effects must be gated by the authoritative action edge');
assert.match(source, /const dodgeJustStarted[\s\S]{0,360}actionTickStarted\([\s\S]{0,180}'dodge'/,
  'dodge pose and effects must be gated by the authoritative action edge');

const visibilityMeshes = [{visibility: 1}, {visibility: 1}, {visibility: 1}];
const lowDetail = {visibility: 1};
const color = () => ({
  diffuseColor: {r: 0.4, g: 0.5, b: 0.6},
  emissiveColor: {r: 0.2, g: 0.25, b: 0.3, set(r, g, b) { Object.assign(this, {r, g, b}); }},
  alpha: 1,
});
const bodyStatus = color();
const headStatus = color();
const formAccentStatus = color();
const statusEntry = {
  isForgeCharacter: true,
  bodyMat: bodyStatus,
  headMat: headStatus,
  _forgeStatusMaterials: [bodyStatus, headStatus, formAccentStatus],
  _forgeMeshes: visibilityMeshes,
  lowDetail,
};
BotRenderer.prototype._updateStatusEffects.call({}, statusEntry, {
  is_alive: true,
  is_dodging: true,
  is_stunned: false,
  hp: 100,
  max_hp: 100,
}, 0);
assert.ok(visibilityMeshes.every(mesh => mesh.visibility === 1),
  'dodge feedback must not write unsupported visibility values to Babylon instances');
assert.equal(lowDetail.visibility, 1,
  'the readable far proxy must remain opaque during dodge');
assert.equal(statusEntry.bodyMat.alpha, 0.5,
  'dodge feedback must dim the bot-owned accent material');
assert.equal(statusEntry.headMat.alpha, 0.5,
  'dodge feedback must dim the bot-owned core material');
assert.equal(formAccentStatus.alpha, 0.5,
  'dodge feedback must dim every visible full-body accent material');
BotRenderer.prototype._updateStatusEffects.call({}, statusEntry, {
  is_alive: true,
  is_dodging: false,
  is_stunned: false,
  hp: 100,
  max_hp: 100,
}, 0);
assert.ok(visibilityMeshes.every(mesh => mesh.visibility === 1));
assert.equal(lowDetail.visibility, 1);
assert.equal(statusEntry.bodyMat.alpha, 1);
assert.equal(statusEntry.headMat.alpha, 1);
assert.equal(formAccentStatus.alpha, 1,
  'full-body accent opacity must restore after dodge');

console.log('Forge actions emit one edge and dodge feedback avoids unsupported instance visibility');
