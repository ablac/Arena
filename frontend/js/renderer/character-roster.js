'use strict';

/**
 * Declarative Forge-class character roster.
 *
 * Every weapon receives its own silhouette and kinetic signature while sharing
 * one rig and one restrained graphite/accent material language. The values are
 * dimensionless multipliers consumed by character-rig.js and
 * character-anims.js; server-provided strings never become geometry or code.
 * @module renderer/character-roster
 */

export const FORGE_WEAPONS = Object.freeze([
  'sword', 'bow', 'spear', 'daggers', 'staff', 'shield', 'grapple',
]);

export const REQUIRED_CHARACTER_MOUNTS = Object.freeze([
  'head', 'chest', 'back', 'shoulderL', 'shoulderR',
  'handL', 'handR', 'weapon', 'core', 'cosmeticRoot',
]);

const common = Object.freeze({
  meshBudget: 22,
  mounts: REQUIRED_CHARACTER_MOUNTS,
});

function profile(spec) {
  return Object.freeze({
    ...common,
    ...spec,
    proportions: Object.freeze(spec.proportions),
    motion: Object.freeze(spec.motion),
    weaponPose: Object.freeze(spec.weaponPose),
    stance: Object.freeze(spec.stance || {}),
  });
}

// `stance` is the class's static ready pose, in character semantics
// (character-rig converts it to rig space once at build time):
//   crouch: world units the body drops; knee: extra shin tuck to sell it
//   armL/armR: forward arm raise; elbowL/elbowR: elbow bend
//   bodyYaw: static torso twist; headPitch: positive looks down
//   armLRoll/armRRoll: raw rig-space arm flare
export const CHARACTER_ROSTER = Object.freeze({
  sword: profile({
    weapon: 'sword', callsign: 'Vanguard', role: 'balanced duelist', armor: 'asymmetric pauldron',
    proportions: {shoulders: 1.00, torso: 1.00, hips: 0.72, leg: 1.00, posture: 0.02, head: 0.94},
    motion: {signature: 'measured guard footwork', strideHz: 1.75, bob: 0.34, sway: 0.08, weight: 0.75},
    weaponPose: {hand: 'right', restX: 0.12, restY: 0.10, restZ: -0.42, reach: 1.0},
    stance: {bodyYaw: 0.10, armR: 0.25, elbowR: 0.35, armL: 0.08},
  }),
  bow: profile({
    weapon: 'bow', callsign: 'Ranger', role: 'precision hunter', armor: 'tapered scout plates',
    proportions: {shoulders: 0.86, torso: 0.82, hips: 0.68, leg: 1.12, posture: -0.08, head: 0.90},
    motion: {signature: 'quiet stalking coil', strideHz: 1.45, bob: 0.20, sway: 0.05, weight: 0.42},
    weaponPose: {hand: 'left', restX: -0.18, restY: 0.02, restZ: 0.08, reach: 1.1},
    stance: {bodyYaw: -0.30, armL: 0.20, elbowR: 0.25},
  }),
  spear: profile({
    weapon: 'spear', callsign: 'Lancer', role: 'reach controller', armor: 'forward lance cradle',
    proportions: {shoulders: 0.92, torso: 0.90, hips: 0.76, leg: 1.08, posture: 0.18, head: 0.92},
    motion: {signature: 'long planted stride', strideHz: 1.35, bob: 0.26, sway: 0.06, weight: 0.68},
    weaponPose: {hand: 'right', restX: 0.30, restY: -0.08, restZ: -0.24, reach: 1.35},
    stance: {bodyYaw: 0.24, armR: 0.45, elbowR: 0.20, armL: -0.06, crouch: 0.3, knee: 0.08},
  }),
  daggers: profile({
    weapon: 'daggers', callsign: 'Skirmisher', role: 'close-range disruptor', armor: 'split flank blades',
    proportions: {shoulders: 0.82, torso: 0.78, hips: 0.78, leg: 0.86, posture: 0.20, head: 0.86},
    motion: {signature: 'rapid lateral feint', strideHz: 2.55, bob: 0.28, sway: 0.16, weight: 0.30},
    weaponPose: {hand: 'both', restX: 0.24, restY: -0.06, restZ: 0.10, reach: 0.72},
    stance: {crouch: 0.9, knee: 0.22, armL: 0.35, armR: 0.35, elbowL: 0.85, elbowR: 0.85, bodyYaw: 0.06},
  }),
  staff: profile({
    weapon: 'staff', callsign: 'Arcanist', role: 'area caster', armor: 'vertical focus mantle',
    proportions: {shoulders: 0.76, torso: 0.86, hips: 0.62, leg: 1.12, posture: -0.04, head: 1.00},
    motion: {signature: 'staff-planted pulse', strideHz: 1.20, bob: 0.16, sway: 0.10, weight: 0.52},
    weaponPose: {hand: 'right', restX: 0.10, restY: -0.10, restZ: 0.04, reach: 1.28},
    stance: {armR: 0.15, elbowR: 0.30, headPitch: -0.06, bodyYaw: -0.08},
  }),
  shield: profile({
    weapon: 'shield', callsign: 'Bulwark', role: 'front-line anchor', armor: 'broad wedge cuirass',
    proportions: {shoulders: 1.28, torso: 1.12, hips: 1.05, leg: 0.93, posture: 0.12, head: 1.02},
    motion: {signature: 'heavy shield-leading stomp', strideHz: 1.05, bob: 0.40, sway: 0.04, weight: 1.00},
    weaponPose: {hand: 'left', restX: -0.32, restY: 0.02, restZ: 0.02, reach: 0.82},
    stance: {crouch: 0.55, knee: 0.14, armL: 0.30, elbowL: 0.25, bodyYaw: 0.18, armR: 0.10},
  }),
  grapple: profile({
    weapon: 'grapple', callsign: 'Rigger', role: 'mobility engineer', armor: 'asymmetric winch frame',
    proportions: {shoulders: 1.04, torso: 0.94, hips: 0.82, leg: 0.98, posture: 0.20, head: 0.88},
    motion: {signature: 'spring-loaded cable crouch', strideHz: 1.90, bob: 0.30, sway: 0.12, weight: 0.58},
    weaponPose: {hand: 'right', restX: 0.26, restY: -0.02, restZ: -0.12, reach: 0.94},
    stance: {armR: 0.35, elbowR: 0.40, bodyYaw: -0.14, crouch: 0.25, knee: 0.08},
  }),
});

export function getCharacterProfile(weapon) {
  return CHARACTER_ROSTER[typeof weapon === 'string' ? weapon.toLowerCase() : '']
    || CHARACTER_ROSTER.sword;
}
