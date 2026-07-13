import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';

const rigSource = readFileSync(new URL('../frontend/js/renderer/character-rig.js', import.meta.url), 'utf8');
const rosterSource = readFileSync(new URL('../frontend/js/renderer/character-roster.js', import.meta.url), 'utf8');
const botSource = readFileSync(new URL('../frontend/js/renderer/bots.js', import.meta.url), 'utf8');
const {
  FORGE_FAR_LOD_PROXY_PARTS,
  FORGE_FAR_LOD_ENTER_DISTANCE,
  FORGE_FAR_LOD_EXIT_DISTANCE,
  setForgeCharacterLOD,
  updateForgeCharacterLOD,
} = await import(new URL('../frontend/js/renderer/character-rig.js?lod-runtime-test', import.meta.url));

assert.deepEqual(
  new Set(FORGE_FAR_LOD_PROXY_PARTS.map(part => part.role)),
  new Set(['torso', 'head', 'arm-left', 'arm-right', 'leg-left', 'leg-right']),
  'the far proxy must preserve a complete humanoid silhouette instead of collapsing into a tapered shard',
);
assert.ok(FORGE_FAR_LOD_PROXY_PARTS.every(part => Array.isArray(part.position)
  && Array.isArray(part.scaling)),
  'every far-proxy body part needs a deterministic transform for the shared merged template');

assert.ok(FORGE_FAR_LOD_ENTER_DISTANCE > FORGE_FAR_LOD_EXIT_DISTANCE,
  'far LOD needs hysteresis so camera movement cannot flicker detail levels');
assert.equal(typeof updateForgeCharacterLOD, 'function');

const toggle = (enabled = true) => ({
  enabled,
  writes: 0,
  setEnabled(value) { this.enabled = value; this.writes += 1; },
  isEnabled() { return this.enabled; },
});
const highMeshes = [toggle(), toggle(), toggle()];
const selector = toggle();
const lowDetail = toggle(false);
const cosmeticGroup = toggle();
const entry = {
  root: {position: {x: 0, y: 0, z: 0}},
  isForgeCharacter: true,
  presentationOnly: false,
  _forgeMeshes: highMeshes,
  selector,
  lowDetail,
  _cosmeticState: {groups: [cosmeticGroup]},
  _forgeFarLOD: false,
};
const toggles = [...highMeshes, selector, lowDetail, cosmeticGroup];
const resetWrites = () => { for (const node of toggles) node.writes = 0; };
const writeCount = () => toggles.reduce((total, node) => total + node.writes, 0);

assert.equal(updateForgeCharacterLOD(entry, {position: {x: 0, y: 30, z: 0}}), false);
assert.equal(writeCount(), 0,
  'an unchanged near bot must not rewrite every high-detail node each frame');

assert.equal(updateForgeCharacterLOD(entry, {
  position: {x: 0, y: FORGE_FAR_LOD_ENTER_DISTANCE + 20, z: 0},
}), true, 'a distant live bot must switch to its shared proxy');
assert.ok(highMeshes.every(mesh => !mesh.enabled));
assert.equal(selector.enabled, false, 'the high-detail selector must leave the draw list at far LOD');
assert.equal(lowDetail.enabled, true, 'the single instanced low-detail proxy must become visible');
assert.equal(cosmeticGroup.enabled, false, 'far bots must not draw cosmetic geometry');

resetWrites();
assert.equal(updateForgeCharacterLOD(entry, {
  position: {x: 0, y: FORGE_FAR_LOD_EXIT_DISTANCE + 10, z: 0},
}), true, 'hysteresis must keep a far bot stable until the camera crosses the exit boundary');
assert.equal(writeCount(), 0,
  'an unchanged far bot must not rewrite every disabled node each frame');
setForgeCharacterLOD(entry, true);
assert.ok(writeCount() > 0,
  'the explicit setter must remain forceful so refreshed cosmetic groups inherit current LOD');
assert.equal(updateForgeCharacterLOD(entry, {
  position: {x: 0, y: FORGE_FAR_LOD_EXIT_DISTANCE - 20, z: 0},
}), false, 'a nearby bot must restore its articulated model');
assert.ok(highMeshes.every(mesh => mesh.enabled));
assert.equal(selector.enabled, true);
assert.equal(lowDetail.enabled, false);
assert.equal(cosmeticGroup.enabled, true);

const presentationEntry = {
  ...entry,
  presentationOnly: true,
  _forgeFarLOD: false,
  lowDetail: null,
};
assert.equal(updateForgeCharacterLOD(presentationEntry, {
  position: {x: 0, y: FORGE_FAR_LOD_ENTER_DISTANCE * 2, z: 0},
}), false, 'the Shop must remain high-detail regardless of preview-camera distance');

assert.match(rigSource, /lowDetail[^\n]*resources\.[A-Za-z]+\.createInstance|resources\.[A-Za-z]+\.createInstance\([^)]*forge-low/,
  'low-detail Forge bodies must instance one scene-owned proxy template');
assert.match(rigSource, /Mesh\.MergeMeshes\(/,
  'the complete far silhouette must be merged once so each distant bot still uses one shared proxy instance');
assert.doesNotMatch(rigSource,
  /CreateCylinder\('forge-low-template'[\s\S]{0,180}diameterTop:\s*0\.62[\s\S]{0,120}tessellation:\s*6/,
  'the old tapered six-sided cylinder reads as a blue triangle and must not return');
assert.match(rigSource, /setLOD\(far\s*=\s*this\._forgeFarLOD\)/,
  'entries must let refreshed cosmetic groups reapply the current LOD without arguments');
assert.doesNotMatch(rosterSource, /drawCallBudget|\blod:\s*Object\.freeze/,
  'the roster must not advertise decorative budgets that runtime code does not enforce');
assert.match(botSource, /updateForgeCharacterLOD\(entry, this\.scene\.activeCamera\)/,
  'live interpolation must choose Forge detail from the active camera');
assert.match(botSource, /updateForgeCharacter\(entry, dt, this\._motionQuery\?\.matches === true, !farLOD\)/,
  'far bots must advance state without rewriting every articulated joint');

console.log('Forge far LOD uses one shared proxy, hysteresis, and cosmetic/high-detail gating');
