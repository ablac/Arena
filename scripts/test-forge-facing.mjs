import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';

const rig = readFileSync(new URL('../frontend/js/renderer/character-rig.js', import.meta.url), 'utf8');
const preview = readFileSync(new URL('../frontend/js/shop-preview.js', import.meta.url), 'utf8');
const bots = readFileSync(new URL('../frontend/js/renderer/bots.js', import.meta.url), 'utf8');

assert.match(
  rig,
  /const modelRoot = new B\.TransformNode\(`forge-model-root-\$\{id\}`[\s\S]{0,180}modelRoot\.rotation\.y = Math\.PI;/,
  'the authored -Z Forge model must be turned once into Babylon +Z facing space',
);
assert.match(
  rig,
  /bodyJoint\.parent = modelRoot;/,
  'all articulated body, weapon, and cosmetic mounts must inherit the forward correction',
);
assert.match(
  rig,
  /modelRoot,/,
  'the model-space correction must stay discoverable on the character entry',
);

assert.match(preview, /Math\.sin\(this\._runElapsed\)[\s\S]{0,120}Math\.cos\(this\._runElapsed\)/,
  'the Shop should retain its smooth authored run loop');
assert.doesNotMatch(preview, /PREVIEW_RUN_RADIANS_PER_SECOND\s*=\s*-/,
  'the Shop must not hide a global facing defect by reversing only its orbit');
assert.match(bots, /entry\.anim\.targetRotY = Math\.atan2\(vx, vz\)/,
  'live movement should keep the canonical +Z yaw convention after model-space correction');

console.log('Forge authored -Z geometry is corrected once and faces live/Shop travel direction');
