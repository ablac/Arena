import assert from 'node:assert/strict';

const {
  BODY_FORM_NEAR_MESH_BUDGET,
  BODY_FORM_SPECS,
  bodyFormForAsset,
  bodyFormKeyForBot,
} = await import(new URL('../frontend/js/renderer/body-form-roster.js?body-form-roster-test', import.meta.url));

const expected = [
  'giant_chicken', 'highland_cow', 'corgi', 'tabby_cat', 'red_fox', 'battle_rabbit',
  'emperor_penguin', 'bullfrog', 'land_shark', 'tyrant_rex', 'human_adventurer',
  'astronaut', 'knight', 'wizard', 'skeleton', 'stone_golem', 'slime_monarch', 'spider_drone',
];

assert.equal(BODY_FORM_NEAR_MESH_BUDGET, 22);
assert.deepEqual(BODY_FORM_SPECS.map(form => form.key), expected,
  'the paid body-form roster must be explicit, ordered, and reviewable');
assert.equal(new Set(BODY_FORM_SPECS.map(form => form.assetKey)).size, expected.length,
  'every body form needs one stable render asset key');

for (const form of BODY_FORM_SPECS) {
  assert.equal(bodyFormForAsset(form.assetKey), form);
  assert.match(form.label, /\S/);
  assert.match(form.family, /^(avian|mammal|amphibian|marine|reptile|human|undead|construct|slime|drone)$/);
  assert.match(form.primary, /^#[0-9a-f]{6}$/i);
  assert.match(form.secondary, /^#[0-9a-f]{6}$/i);
  assert.match(form.accent, /^#[0-9a-f]{6}$/i);
  assert.ok(form.nearMeshBudget <= BODY_FORM_NEAR_MESH_BUDGET,
    `${form.key} exceeds the fixed near-mesh budget`);
}

assert.equal(bodyFormForAsset('https://example.com/chicken.glb'), null,
  'body forms must never resolve remote models');
assert.equal(bodyFormForAsset('../chicken'), null);
assert.equal(bodyFormKeyForBot({cosmetics: {bot_skin: 'body_giant_chicken'}}), 'giant_chicken');
assert.equal(bodyFormKeyForBot({cosmetics: {bot_skin: 'standard'}}), 'standard');

console.log('18 original body forms are explicit, bounded, and reject arbitrary assets');
