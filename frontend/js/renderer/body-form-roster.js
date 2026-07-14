'use strict';

/**
 * Original, commercially safe full-body forms authored for Arena.
 *
 * This is an exact allowlist: server values select a bounded procedural recipe
 * and can never become a URL, model path, script, or unbounded geometry input.
 * @module renderer/body-form-roster
 */

export const BODY_FORM_NEAR_MESH_BUDGET = 22;

// The trailing `motion` entry is each form's movement personality, consumed
// by character-anims (flavor behavior + gait multipliers, all bounded):
//   flavor: hop | waddle | skitter | squash | flutter | lumber | prowl | glide | rattle
//   stride/bob/sway: gait multipliers; legScale: leg-swing scale;
//   posture: extra forward lean in radians (character semantics).
const rawSpecs = [
  ['giant_chicken', 'Giant Chicken', 'avian', '#f4eee2', '#e34f3f', '#f4bd35', 22, ['beak', 'comb', 'wings', 'tailFan'],
    {flavor: 'flutter', stride: 1.25, bob: 1.1}],
  ['highland_cow', 'Highland Cow', 'mammal', '#7c4327', '#d99a57', '#f2d5a4', 19, ['muzzle', 'horns', 'ears', 'tail'],
    {flavor: 'lumber', stride: 0.85, bob: 1.3}],
  ['corgi', 'Arena Corgi', 'mammal', '#d9853f', '#fff1d2', '#61d7ff', 18, ['muzzle', 'longEars', 'tail'],
    {flavor: 'prowl', stride: 1.5, bob: 0.8, posture: 0.06}],
  ['tabby_cat', 'Tabby Cat', 'mammal', '#8d7c6d', '#d4c1a6', '#72e0ff', 21, ['muzzle', 'catEars', 'tail', 'stripes'],
    {flavor: 'prowl', stride: 1.2, bob: 0.55, sway: 1.4, posture: 0.10}],
  ['red_fox', 'Red Fox', 'mammal', '#d95c2f', '#f7e5c8', '#4ad9ff', 19, ['muzzle', 'foxEars', 'brushTail'],
    {flavor: 'prowl', stride: 1.3, bob: 0.6, sway: 1.3, posture: 0.08}],
  ['battle_rabbit', 'Battle Rabbit', 'mammal', '#c9c7d2', '#f2edf7', '#ff6ea8', 18, ['muzzle', 'rabbitEars', 'tailPom'],
    {flavor: 'hop', stride: 0.9}],
  ['emperor_penguin', 'Emperor Penguin', 'avian', '#222b3a', '#f4f4ed', '#f2bd35', 17, ['beak', 'wings', 'belly'],
    {flavor: 'waddle', stride: 1.35, legScale: 0.55}],
  ['bullfrog', 'Bullfrog', 'amphibian', '#4f9b4f', '#a9cf5f', '#f2d85c', 17, ['frogEyes', 'wideMouth', 'webFeet'],
    {flavor: 'hop', stride: 0.7, posture: 0.12}],
  ['land_shark', 'Land Shark', 'marine', '#477b98', '#c6e2e8', '#ff6a64', 20, ['sharkSnout', 'dorsal', 'tailFin', 'teeth'],
    {flavor: 'glide', sway: 1.8, bob: 0.5, legScale: 0.65}],
  ['tyrant_rex', 'Tyrant Rex', 'reptile', '#5f8f48', '#b7c86d', '#ff8b45', 20, ['dinoSnout', 'brow', 'dinoTail', 'claws'],
    {flavor: 'lumber', stride: 0.8, bob: 1.35, posture: 0.22}],
  ['human_adventurer', 'Human Adventurer', 'human', '#c58d69', '#325e91', '#e7ad4f', 18, ['hair', 'belt', 'boots'],
    null],
  ['astronaut', 'Deep-Space Astronaut', 'human', '#e8eef5', '#697b91', '#48d8ff', 19, ['visor', 'backpack', 'boots'],
    {flavor: 'hop', stride: 0.55, bob: 0.9}],
  ['knight', 'Arena Knight', 'human', '#9aa8b8', '#334158', '#f0c54b', 20, ['helmet', 'plume', 'pauldrons'],
    {flavor: 'lumber', stride: 0.9, bob: 1.15}],
  ['wizard', 'Circuit Wizard', 'human', '#4e3a82', '#8e6bd1', '#65e5ff', 20, ['hat', 'robe', 'rune'],
    {flavor: 'glide', bob: 0.4, sway: 1.2, legScale: 0.7}],
  ['skeleton', 'Neon Skeleton', 'undead', '#dedbd0', '#333b4c', '#60ebff', 21, ['skull', 'ribs', 'boneLimbs'],
    {flavor: 'rattle', stride: 1.1}],
  ['stone_golem', 'Stone Golem', 'construct', '#69737b', '#3e474d', '#ffb34e', 20, ['boulders', 'runes', 'pauldrons'],
    {flavor: 'lumber', stride: 0.7, bob: 1.6}],
  ['slime_monarch', 'Slime Monarch', 'slime', '#4bcf8d', '#1f8f74', '#f3d45b', 16, ['blob', 'crown', 'glowCore'],
    {flavor: 'squash', legScale: 0}],
  ['spider_drone', 'Spider Drone', 'drone', '#30384a', '#657189', '#f15bff', 22, ['droneBody', 'spiderLegs', 'optic'],
    {flavor: 'skitter', stride: 1.9, legScale: 0.5}],
];

export const BODY_FORM_SPECS = Object.freeze(rawSpecs.map(([
  key, label, family, primary, secondary, accent, nearMeshBudget, features, motion,
]) => Object.freeze({
  key,
  assetKey: `body_${key}`,
  label,
  family,
  primary,
  secondary,
  accent,
  nearMeshBudget,
  features: Object.freeze([...features]),
  motion: motion ? Object.freeze({...motion}) : null,
})));

const BODY_FORM_BY_ASSET = new Map(BODY_FORM_SPECS.map(spec => [spec.assetKey, spec]));

/** Resolve one exact body-form asset; arbitrary strings always return null. */
export function bodyFormForAsset(value) {
  return typeof value === 'string' ? (BODY_FORM_BY_ASSET.get(value) || null) : null;
}

/** Stable presentation key used to decide whether a live rig must be rebuilt. */
export function bodyFormKeyForBot(bot) {
  const asset = bot?.cosmetics && typeof bot.cosmetics === 'object'
    ? bot.cosmetics.bot_skin
    : null;
  return bodyFormForAsset(asset)?.key || 'standard';
}
