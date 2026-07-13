import assert from 'node:assert/strict';

const {
  CHARACTER_ROSTER,
  FORGE_WEAPONS,
  REQUIRED_CHARACTER_MOUNTS,
  getCharacterProfile,
} = await import(new URL('../frontend/js/renderer/character-roster.js?roster-test', import.meta.url));

const expected = ['sword', 'bow', 'spear', 'daggers', 'staff', 'shield', 'grapple'];
assert.deepEqual([...FORGE_WEAPONS], expected, 'the Arena has exactly seven supported combat chassis');
assert.deepEqual(Object.keys(CHARACTER_ROSTER), expected, 'every weapon owns one explicit chassis profile');

const signatures = new Set();
const callsigns = new Set();
for (const weapon of expected) {
  const profile = getCharacterProfile(weapon);
  assert.equal(profile.weapon, weapon);
  assert.ok(profile.callsign && !callsigns.has(profile.callsign), `${weapon} needs a unique showroom callsign`);
  callsigns.add(profile.callsign);
  assert.ok(profile.role && profile.motion?.signature, `${weapon} needs a readable role and kinetic signature`);
  assert.ok(profile.meshBudget <= 22, `${weapon} exceeds the live LOD0 mesh budget`);
  assert.equal('drawCallBudget' in profile, false,
    `${weapon} must not advertise an unmeasured per-character draw-call budget`);
  assert.equal('lod' in profile, false,
    `${weapon} must use the renderer's measured camera-distance LOD instead of decorative metadata`);
  for (const mount of REQUIRED_CHARACTER_MOUNTS) {
    assert.ok(profile.mounts.includes(mount), `${weapon} is missing semantic mount ${mount}`);
  }
  const silhouette = JSON.stringify([
    profile.proportions.shoulders,
    profile.proportions.torso,
    profile.proportions.hips,
    profile.proportions.leg,
    profile.proportions.posture,
    profile.armor,
  ]);
  assert.ok(!signatures.has(silhouette), `${weapon} repeats another chassis silhouette`);
  signatures.add(silhouette);
}

assert.equal(getCharacterProfile('not-a-weapon'), CHARACTER_ROSTER.sword,
  'unknown server values fail safely to the Vanguard chassis');

console.log('seven Forge-class chassis have unique silhouettes, semantic mounts, and bounded render budgets');
