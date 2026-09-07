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

// Rig-space sign conventions: the model faces local -Z, torso content sits
// above its joint, and limbs hang below theirs. These directions regressed
// once (high-posture classes leaned backward and swings wound up behind the
// body), so pin them.
const spearProfile = getCharacterProfile('spear');
const spearWeapon = joint();
const spearEntry = {
  root: joint(),
  profile: spearProfile,
  anim: new ForgeAnimState('spear'),
  isAlive: true,
  joints: {
    body: joint(), head: joint(), leftArm: joint(), leftElbow: joint(),
    rightArm: joint(), rightElbow: joint(), leftLeg: joint(), leftKnee: joint(),
    rightLeg: joint(), rightKnee: joint(), core: joint(),
  },
  basePose: {
    bodyY: 10, armLRoll: 0, armRRoll: 0, elbowLPitch: 0, elbowRPitch: 0, kneePitch: 0.09,
  },
  weaponPoseNodes: [spearWeapon],
  weaponBases: [{x: 0, y: 0, z: 0, rx: 0, ry: 0, rz: 0, sign: 1}],
};
updateForgeCharacter(spearEntry, 0.016, false, true);
assert.ok(spearEntry.joints.body.rotation.x < 0,
  'a positive roster posture must render as a FORWARD lean (negative rig pitch)');
assert.ok(spearEntry.joints.leftKnee.rotation.x < 0,
  'the knee pre-bend must tuck the shin backward (negative rig pitch)');
// Drive the spear to its thrust contact frame and confirm the weapon and
// striking arm both travel forward, not into the character's own back.
for (let step = 0; step < 6; step += 1) updateForgeCharacter(spearEntry, 0.058 * 0.62 / 6, false, true);
assert.equal(spearEntry.anim.attackTimer, -1, 'spear entry starts at rest');
triggerForgeAttack(spearEntry.anim, 'spear');
let spearFrames = Math.round((spearEntry.anim.attackDuration * 0.62) / 0.008);
for (let step = 0; step < spearFrames; step += 1) updateForgeCharacter(spearEntry, 0.008, false, true);
assert.ok(spearWeapon.position.z < -1,
  'the spear thrust must translate the weapon toward -Z (the authored facing)');
assert.ok(spearEntry.joints.rightArm.rotation.x > 0,
  'the thrusting arm must swing forward (positive rig pitch for a hanging limb)');
assert.ok(spearEntry.joints.body.rotation.x < -spearProfile.proportions.posture,
  'the thrust must deepen the forward lean beyond the resting posture');

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
// Forge parts carry their dimensions in `scaling`, so the impact squash must
// key relative to each part's authored scale. The legacy absolute 1,1,1 end
// key collapsed torsos/heads to unit specks on the first ranged hit and left
// every veteran bot looking half-missing (live regression, 2026-07-13).
assert.match(botSource, /_impactScaleBase/,
  'the impact squash must capture and restore each part\'s authored scale');
assert.doesNotMatch(botSource, /value:\s*new B\.Vector3\(1,\s*1,\s*1\)/,
  'the impact squash must never end on an absolute unit scale');

console.log('all Forge-class motion states are allocation-stable, finite, reduced-motion aware, and return to rest');


// Locomotion is driven by actual travel, so a 120 Hz display cannot make a
// bot take more steps than a 30 Hz display following the same trajectory.
function walkingEntry() {
  return {
    ...spearEntry, root: joint(), anim: new ForgeAnimState('spear'),
    joints: Object.fromEntries(Object.keys(spearEntry.joints).map(key => [key, joint()])),
    weaponPoseNodes: [], weaponBases: [],
  };
}
function travelAt(fps) {
  const entry = walkingEntry();
  updateForgeCharacter(entry, 0, false, true);
  for (let frame = 0; frame < fps * 2; frame += 1) {
    entry.root.position.z += 50 / fps;
    updateForgeCharacter(entry, 1 / fps, false, true);
  }
  return entry;
}
const walk30 = travelAt(30);
const walk120 = travelAt(120);
assert.ok(Math.abs(walk30.anim.gaitPhase - walk120.anim.gaitPhase) < 0.00001,
  'equal travel must produce the same stride phase across refresh rates');
assert.ok(Math.abs(walk30.anim.locomotionWeight - walk120.anim.locomotionWeight) < 0.00001,
  'locomotion blending must be frame-rate independent');
const beforeStop = walk120.anim.pose[POSE_CHANNELS.indexOf('legLPitch')];
updateForgeCharacter(walk120, 1 / 120, false, true);
assert.ok(Math.abs(walk120.anim.pose[POSE_CHANNELS.indexOf('legLPitch')] - beforeStop) < 0.08,
  'stopping must settle the legs rather than snap them to rest');
for (let frame = 0; frame < 240; frame += 1) updateForgeCharacter(walk120, 1 / 120, false, true);
assert.ok(Math.abs(walk120.anim.pose[POSE_CHANNELS.indexOf('legLPitch')]) < 0.001,
  'stationary legs must settle to rest');
const preTeleportPhase = walk120.anim.gaitPhase;
walk120.root.position.x += 4000;
updateForgeCharacter(walk120, 1 / 60, false, true);
assert.equal(walk120.anim.gaitPhase, preTeleportPhase,
  'teleport discontinuities must not count as thousands of running steps');
for (const invalidDt of [NaN, Infinity, -1, 0]) {
  updateForgeCharacter(walk120, invalidDt, false, true);
  for (const node of [walk120.root, ...Object.values(walk120.joints)]) {
    for (const axis of ['x', 'y', 'z']) assert.ok(Number.isFinite(node.rotation[axis]));
  }
}
const farWalk = walkingEntry();
updateForgeCharacter(farWalk, 0, false, false);
farWalk.root.position.z = 1;
updateForgeCharacter(farWalk, 1 / 60, false, false);
assert.ok(farWalk.anim.gaitPhase > 0, 'far LOD must keep stride state current');
assert.equal(farWalk.joints.body.position.y, 0, 'far LOD must not rewrite disabled joints');

const slowWalker = walkingEntry();
const fastWalker = walkingEntry();
updateForgeCharacter(slowWalker, 0);
updateForgeCharacter(fastWalker, 0);
for (let frame = 0; frame < 120; frame += 1) {
  slowWalker.root.position.z += 0.25;
  fastWalker.root.position.z += 0.5;
  updateForgeCharacter(slowWalker, 1 / 60);
  updateForgeCharacter(fastWalker, 1 / 60);
}
assert.ok(Math.abs(fastWalker.anim.gaitPhase - slowWalker.anim.gaitPhase * 2) < 0.00001,
  'twice the travel must take twice the steps below the sprint cadence cap');
const banked = walkingEntry();
const unbanked = walkingEntry();
const restrainedWalker = walkingEntry();
for (const entry of [banked, unbanked, restrainedWalker]) updateForgeCharacter(entry, 0);
for (let frame = 0; frame < 15; frame += 1) {
  for (const entry of [banked, unbanked, restrainedWalker]) entry.root.position.x += 1;
  updateForgeCharacter(banked, 1 / 60, false, true, true);
  updateForgeCharacter(unbanked, 1 / 60, false, true, false);
  updateForgeCharacter(restrainedWalker, 1 / 60, true, true, true);
}
assert.ok(Math.abs(banked.joints.body.rotation.x - unbanked.joints.body.rotation.x) > 0.025,
  'secondary motion must visibly shift body weight during acceleration');
assert.equal(restrainedWalker.joints.body.rotation.z, 0,
  'reduced motion must suppress turn banking and lateral weight shift');
assert.equal(unbanked.anim.gaitPhase, banked.anim.gaitPhase,
  'disabling secondary motion must preserve locomotion state');
assert.equal(unbanked.root.rotation.y, banked.root.rotation.y,
  'disabling secondary motion must preserve gameplay facing');
for (let frame = 0; frame < 180; frame += 1) {
  updateForgeCharacter(banked, 1 / 60, false, true, false);
}
assert.ok(Math.abs(banked.anim.accelerationLean) < 0.0001 && Math.abs(banked.anim.turnLean) < 0.0001,
  'hidden secondary state must keep settling instead of freezing until re-enabled');

console.log('Forge locomotion preserves cadence, blends stops, rejects teleport spikes, and respects motion controls');
