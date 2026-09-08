import assert from 'node:assert/strict';
import {ForgeAnimState, POSE_CHANNELS, forgeContactDelay, sampleForgePose, triggerForgeAttack} from '../frontend/js/renderer/character-anims.js';
import {getCharacterProfile} from '../frontend/js/renderer/character-roster.js';

// Independent pre-polish landmarks: recovery may change, contact may not.
const landmarks = {
  sword: [0.42, 'weaponPitch', -2.28],
  bow: [0.58, 'elbowRPitch', -1.32],
  spear: [0.45, 'weaponZ', 3.4],
  daggers: [0.42, 'armRPitch', 1.5],
  staff: [0.55, 'weaponPitch', -2.12],
  shield: [0.41, 'weaponZ', 0.9],
  grapple: [0.36, 'weaponPitch', -1.55],
};
for (const [weapon, [contact, channel, expected]] of Object.entries(landmarks)) {
  for (const duration of [0.3, 0.7, 1.2]) {
    const state = new ForgeAnimState(weapon);
    triggerForgeAttack(state, weapon, duration);
    const profile = getCharacterProfile(weapon);
    assert.ok(Math.abs(forgeContactDelay(weapon, duration) - contact * duration) < 1e-9);
    state.attackTimer = contact * duration;
    const pose = sampleForgePose(profile, state, 0, false, 0, true, true, false);
    assert.ok(Math.abs(pose[POSE_CHANNELS.indexOf(channel)] - expected) < 1e-5, `${weapon}: contact remains authored`);
    // Reach is mostly withdrawn near the end; no accumulating residual pose.
    state.attackTimer = duration * 0.95;
    sampleForgePose(profile, state, 0, false, 0, true, true, false);
    assert.ok(Math.max(...pose.map(Math.abs)) < 0.015, `${weapon}: settle into guard before next action`);
    state.attackTimer = duration;
    sampleForgePose(profile, state, 0, false, 0, true, true, false);
    assert.ok(pose.every(value => value === 0), `${weapon}: exact rest at completion`);
  }
}

// Mode changes must not overwrite the saved legacy palette or allocate colors.
const color = (r, g, b) => ({r, g, b, copyFrom(c) { Object.assign(this, {r: c.r, g: c.g, b: c.b}); }});
const {applyForgeLightingMode} = await import('../frontend/js/renderer/forge-weapons.js');
const material = {
  diffuseColor: color(0, 0, 0), emissiveColor: color(0, 0, 0),
  _forgeLitDiffuse: color(0.13, 0.16, 0.20), _forgeUnlitDiffuse: color(0.28, 0.36, 0.50),
  _forgeLitEmissive: color(0.0156, 0.0192, 0.024), _forgeUnlitEmissive: color(0.18, 0.24, 0.36),
};
const diffuse = material.diffuseColor;
const emissive = material.emissiveColor;
for (let i = 0; i < 20; i++) {
  applyForgeLightingMode(material, true);
  assert.equal(material.disableLighting, false);
  assert.equal(material.diffuseColor.r, 0.13);
  assert.equal(material.emissiveColor.r, 0.0156);
  applyForgeLightingMode(material, false);
  assert.equal(material.disableLighting, true);
  assert.equal(material.diffuseColor.r, 0.28);
  assert.equal(material.emissiveColor.r, 0.18);
}
assert.equal(material.diffuseColor, diffuse);
assert.equal(material.emissiveColor, emissive);
assert.equal(material.emissiveTexture, null);
console.log('Forge material modes and contact/recovery polish: passed');
