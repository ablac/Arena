import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';

const {FORGE_WEAPONS, getCharacterProfile} = await import(
  new URL('../frontend/js/renderer/character-roster.js?animation-roster-test', import.meta.url)
);
const {
  ForgeAnimState,
  POSE_CHANNELS,
  forgeContactDelay,
  sampleForgePose,
  triggerForgeAttack,
  triggerForgeDodge,
  triggerForgeShove,
  updateForgeCharacter,
} = await import(new URL('../frontend/js/renderer/character-anims.js?animation-test', import.meta.url));

assert.equal(typeof forgeContactDelay, 'function',
  'Forge owns its effect-contact timing without loading the retired generic animator');
for (const weapon of FORGE_WEAPONS) {
  const delay = forgeContactDelay(weapon);
  assert.ok(Number.isFinite(delay) && delay >= 0.12 && delay < 1,
    `${weapon} needs a bounded visual contact delay`);
}

for (const weapon of FORGE_WEAPONS) {
  const profile = getCharacterProfile(weapon);
  const state = new ForgeAnimState(weapon);
  assert.ok(state.pose instanceof Float32Array);
  assert.equal(state.pose.length, POSE_CHANNELS.length);

  triggerForgeAttack(state, weapon, 0.35);
  triggerForgeDodge(state, 0.4);
  triggerForgeShove(state);
  for (let frame = 0; frame < 600; frame += 1) {
    const pose = sampleForgePose(
      profile, state, 1 / 120,
      frame < 180, frame < 180 ? 0.8 : 0, true, false,
    );
    assert.equal(pose, state.pose, `${weapon} must reuse one pose buffer`);
    for (const value of pose) assert.ok(Number.isFinite(value), `${weapon} emitted a non-finite pose`);
  }
  assert.equal(state.attackTimer, -1, `${weapon} attack must return to rest`);
  assert.equal(state.dodgeTimer, -1, `${weapon} dodge must return to rest`);
  assert.equal(state.shoveTimer, -1, `${weapon} shove must return to rest`);

  const restrained = sampleForgePose(profile, state, 1 / 60, false, 0, true, true);
  for (const value of restrained) assert.ok(Number.isFinite(value));
  assert.ok(Math.abs(restrained[POSE_CHANNELS.indexOf('bodyY')]) < 0.001,
    `${weapon} reduced-motion idle must not bob`);
}

const vector = (x = 0, y = 0, z = 0) => ({
  x, y, z,
  setAll(value) { this.x = value; this.y = value; this.z = value; },
});
const joint = () => ({position: vector(), rotation: vector(), scaling: vector(1, 1, 1)});
const daggerProfile = getCharacterProfile('daggers');
const daggerAnim = new ForgeAnimState('daggers');
const leftDagger = joint();
const rightDagger = joint();
const daggerEntry = {
  root: joint(),
  profile: daggerProfile,
  anim: daggerAnim,
  isAlive: true,
  joints: {
    body: joint(), head: joint(), leftArm: joint(), leftElbow: joint(),
    rightArm: joint(), rightElbow: joint(), leftLeg: joint(), leftKnee: joint(),
    rightLeg: joint(), rightKnee: joint(), core: joint(),
  },
  basePose: {
    bodyY: 10,
    armLRoll: 0,
    armRRoll: 0,
    elbowLPitch: 0,
    elbowRPitch: 0,
    kneePitch: 0,
  },
  weaponPoseNodes: [leftDagger, rightDagger],
  weaponBases: [
    {x: 0, y: 0, z: 0, rx: 0, ry: 0, rz: 0, sign: -1},
    {x: 0, y: 0, z: 0, rx: 0, ry: 0, rz: 0, sign: 1},
  ],
};
triggerForgeAttack(daggerAnim, 'daggers', 0.3);
updateForgeCharacter(daggerEntry, 0.05, false, true);
assert.ok(leftDagger.rotation.z < 0 && rightDagger.rotation.z > 0,
  'the two hand-mounted daggers must receive mirrored attack rotations');

const animSource = readFileSync(
  new URL('../frontend/js/renderer/character-anims.js', import.meta.url),
  'utf8',
);
const botSource = readFileSync(new URL('../frontend/js/renderer/bots.js', import.meta.url), 'utf8');
assert.doesNotMatch(animSource, /sampleForgePose\([^)]*motion\s*=\s*\{\}/,
  'the per-bot pose sampler must not allocate a default motion object');
assert.doesNotMatch(animSource, /updateForgeCharacter\([^)]*options\s*=\s*\{\}/,
  'the per-bot animator must not allocate a default options object');
assert.doesNotMatch(animSource, /sampleForgePose\([\s\S]{0,180}\{\s*moving[,}]/,
  'the animator must pass scalar state instead of allocating one motion object per bot/frame');
assert.match(botSource, /this\._motionQuery\s*=\s*typeof window\.matchMedia[\s\S]{0,180}prefers-reduced-motion/,
  'the live renderer must retain one media-query object instead of querying per bot');
assert.match(botSource, /updateForgeCharacter\(entry, dt, this\._motionQuery\?\.matches === true/,
  'the live renderer must pass the current reduced-motion preference into Forge animation');

console.log('all Forge-class motion states are allocation-stable, finite, reduced-motion aware, and return to rest');
