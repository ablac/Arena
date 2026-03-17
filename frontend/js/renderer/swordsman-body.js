'use strict';

/**
 * Swordsman body — articulated 14-joint skeletal character from Babylon.js primitives.
 * Replaces the rigid cylinder body for sword-wielding bots with a fully
 * animatable humanoid that supports HEMA stance and attack animations.
 *
 * Hierarchy mirrors the Three.js animation editor's CharacterBuilder:
 *   root (TransformNode)
 *     body (TransformNode) — torso group, breathing/bob target
 *       torso (Box)
 *       head (Sphere)
 *       leftArm (TransformNode) — shoulder pivot
 *         leftUpperArm (Box)
 *         leftLowerArm (TransformNode) — elbow pivot
 *           leftForearm (Box)
 *           leftHand (Box)
 *       rightArm (TransformNode) — shoulder pivot
 *         rightUpperArm (Box)
 *         rightLowerArm (TransformNode) — elbow pivot
 *           rightForearm (Box)
 *           rightHand (Box)
 *             sword (TransformNode) — longsword group
 *       leftLeg (TransformNode) — hip pivot
 *         leftUpperLeg (Box)
 *         leftLowerLeg (TransformNode) — knee pivot
 *           leftShin (Box)
 *       rightLeg (TransformNode) — hip pivot
 *         rightUpperLeg (Box)
 *         rightLowerLeg (TransformNode) — knee pivot
 *           rightShin (Box)
 *
 * @module renderer/swordsman-body
 */

import { parseColor, makeMat } from './utils.js';
import { getGuiTexture } from './bot-body.js';
import { SwordsmanAnimState } from './swordsman-anims.js';

// ─── Scale ───────────────────────────────────────────────────────────────────
// Editor character is ~1.85 units tall. Arena bots are ~24 units tall.
// Scale factor ≈ 13. All dimensions below are editor values * S.
const S = 13;

// ─── Body dimensions (editor units * S) ─────────────────────────────────────
const TORSO_W  = 0.50 * S;   // 6.5
const TORSO_H  = 0.70 * S;   // 9.1
const TORSO_D  = 0.30 * S;   // 3.9
const HEAD_R   = 0.20 * S;   // 2.6

const UPPER_ARM_W = 0.15 * S;
const UPPER_ARM_H = 0.35 * S;
const UPPER_ARM_D = 0.15 * S;

const FOREARM_W = 0.12 * S;
const FOREARM_H = 0.30 * S;
const FOREARM_D = 0.12 * S;

const HAND_W = 0.10 * S;
const HAND_H = 0.08 * S;
const HAND_D = 0.10 * S;

const UPPER_LEG_W = 0.18 * S;
const UPPER_LEG_H = 0.40 * S;
const UPPER_LEG_D = 0.18 * S;

const SHIN_W = 0.15 * S;
const SHIN_H = 0.35 * S;
const SHIN_D = 0.15 * S;

// ─── Pivot offsets (editor coords * S) ──────────────────────────────────────
const BODY_Y        = 0.75 * S;   // body group origin above ground
const SHOULDER_X    = 0.325 * S;  // shoulder offset from torso center
const SHOULDER_Y    = 0.65 * S;   // shoulder height relative to body origin
const HIP_X         = 0.14 * S;   // hip offset from body center

// ─── Sword dimensions (scaled) ──────────────────────────────────────────────
const BLADE_W  = 0.04 * S;
const BLADE_H  = 0.80 * S;
const BLADE_D  = 0.015 * S;
const GUARD_W  = 0.20 * S;
const GUARD_H  = 0.03 * S;
const GUARD_D  = 0.03 * S;
const GRIP_W   = 0.03 * S;
const GRIP_H   = 0.15 * S;
const GRIP_D   = 0.03 * S;
const POMMEL_R = 0.025 * S;

// ─── Shared materials ───────────────────────────────────────────────────────
let _shdMat = null;
let _swordBladeMat = null;
let _swordGuardMat = null;
let _swordGripMat = null;
let _swordPommelMat = null;

function _getShadowMat(scene) {
  if (!_shdMat || _shdMat.isDisposed) {
    const B = window.BABYLON;
    _shdMat = new B.StandardMaterial('smat-sw-shared', scene);
    _shdMat.diffuseColor = new B.Color3(0, 0, 0);
    _shdMat.specularColor = B.Color3.Black();
    _shdMat.emissiveColor = B.Color3.Black();
    _shdMat.disableLighting = true;
    _shdMat.alpha = 0.3;
    _shdMat.backFaceCulling = false;
    _shdMat.freeze();
  }
  return _shdMat;
}

function _getSwordMats(scene) {
  const B = window.BABYLON;
  if (!_swordBladeMat || _swordBladeMat.isDisposed) {
    _swordBladeMat = makeMat('sw-blade', scene, new B.Color3(0.85, 0.85, 0.95), {
      emissiveFactor: 0.5, specular: new B.Color3(0.6, 0.6, 0.6)
    });
    _swordBladeMat.freeze();
  }
  if (!_swordGuardMat || _swordGuardMat.isDisposed) {
    _swordGuardMat = makeMat('sw-guard', scene, new B.Color3(0.55, 0.45, 0.25), {
      emissiveFactor: 0.3
    });
    _swordGuardMat.freeze();
  }
  if (!_swordGripMat || _swordGripMat.isDisposed) {
    _swordGripMat = makeMat('sw-grip', scene, new B.Color3(0.3, 0.2, 0.1), {
      emissiveFactor: 0.2
    });
    _swordGripMat.freeze();
  }
  if (!_swordPommelMat || _swordPommelMat.isDisposed) {
    _swordPommelMat = makeMat('sw-pommel', scene, new B.Color3(0.6, 0.5, 0.3), {
      emissiveFactor: 0.3
    });
    _swordPommelMat.freeze();
  }
  return { blade: _swordBladeMat, guard: _swordGuardMat, grip: _swordGripMat, pommel: _swordPommelMat };
}

// ─── Helper: create a box mesh parented to a node ───────────────────────────
function _box(name, w, h, d, scene, parent, mat) {
  const B = window.BABYLON;
  const m = B.MeshBuilder.CreateBox(name, { width: w, height: h, depth: d }, scene);
  m.parent = parent;
  m.material = mat;
  m.isPickable = false;
  m.alwaysSelectAsActiveMesh = true;
  return m;
}

// ─── Build the longsword ────────────────────────────────────────────────────
function _buildSword(id, scene, parent) {
  const B = window.BABYLON;
  const mats = _getSwordMats(scene);

  const swordRoot = new B.TransformNode(`sw-sword-${id}`, scene);
  swordRoot.parent = parent;

  // Grip (centered in hand)
  const grip = _box(`sw-grip-${id}`, GRIP_W, GRIP_H, GRIP_D, scene, swordRoot, mats.grip);
  grip.position.y = -GRIP_H / 2;

  // Crossguard
  const guard = _box(`sw-guard-${id}`, GUARD_W, GUARD_H, GUARD_D, scene, swordRoot, mats.guard);
  guard.position.y = 0;

  // Blade (extends upward from guard)
  const blade = _box(`sw-blade-${id}`, BLADE_W, BLADE_H, BLADE_D, scene, swordRoot, mats.blade);
  blade.position.y = BLADE_H / 2 + GUARD_H / 2;

  // Pommel (bottom of grip)
  const pommel = B.MeshBuilder.CreateSphere(`sw-pommel-${id}`, {
    diameter: POMMEL_R * 2, segments: 4
  }, scene);
  pommel.parent = swordRoot;
  pommel.position.y = -GRIP_H;
  pommel.material = mats.pommel;
  pommel.isPickable = false;
  pommel.alwaysSelectAsActiveMesh = true;

  // Orient sword so it points up from the hand by default
  // (initial idle rotation will be set by animation system)

  return swordRoot;
}

// ─── Main entry point ───────────────────────────────────────────────────────

/**
 * Build an articulated swordsman mesh hierarchy.
 * Returns an entry object compatible with BotRenderer's expectations
 * (root, weapon, bodyMat, headMat, hpBar, etc.) plus joint references
 * for the animation system.
 *
 * @param {Object} bot - Bot data from server (bot_id, avatar_color, name, weapon)
 * @param {BABYLON.Scene} scene
 * @returns {Object} Entry object for BotRenderer
 */
export function createSwordsmanEntry(bot, scene) {
  const B = window.BABYLON;
  const id = bot.bot_id;
  const color = parseColor(bot.avatar_color);

  // ── Root node (world position) ──
  const root = new B.TransformNode(`swRoot-${id}`, scene);

  // ── Body group (raised above ground) ──
  const body = new B.TransformNode(`swBody-${id}`, scene);
  body.parent = root;
  body.position.y = BODY_Y;

  // ── Torso ──
  const bodyMat = makeMat(`sw-bmat-${id}`, scene, color, { emissiveFactor: 0.35 });
  bodyMat.emissiveFresnelParameters = new B.FresnelParameters({
    bias: 0.6, power: 2,
    leftColor: new B.Color3(color.r * 0.8, color.g * 0.8, color.b * 0.8),
    rightColor: B.Color3.Black()
  });
  const torso = _box(`swTorso-${id}`, TORSO_W, TORSO_H, TORSO_D, scene, body, bodyMat);

  // ── Head ──
  const headColor = new B.Color3(
    Math.min(color.r * 1.2, 1), Math.min(color.g * 1.2, 1), Math.min(color.b * 1.2, 1)
  );
  const headMat = makeMat(`sw-hmat-${id}`, scene, headColor, { emissiveFactor: 0.4 });
  headMat.emissiveFresnelParameters = new B.FresnelParameters({
    bias: 0.5, power: 2,
    leftColor: new B.Color3(color.r * 0.8, color.g * 0.8, color.b * 0.8),
    rightColor: B.Color3.Black()
  });
  const head = B.MeshBuilder.CreateSphere(`swHead-${id}`, {
    diameter: HEAD_R * 2, segments: 6
  }, scene);
  head.parent = body;
  head.position.y = TORSO_H / 2 + HEAD_R * 0.8;
  head.material = headMat;
  head.isPickable = false;
  head.alwaysSelectAsActiveMesh = true;

  // ── Arm material (shared per bot, slightly darker) ──
  const armMat = makeMat(`sw-amat-${id}`, scene, color.scale(0.8), { emissiveFactor: 0.3 });
  const handMat = makeMat(`sw-handmat-${id}`, scene, headColor.scale(0.9), { emissiveFactor: 0.3 });

  // ── Left Arm ──
  const leftArm = new B.TransformNode(`swLArm-${id}`, scene);
  leftArm.parent = body;
  leftArm.position.set(-SHOULDER_X, SHOULDER_Y, 0);

  const leftUpperArm = _box(`swLUA-${id}`, UPPER_ARM_W, UPPER_ARM_H, UPPER_ARM_D, scene, leftArm, armMat);
  leftUpperArm.position.y = -UPPER_ARM_H / 2;

  const leftLowerArm = new B.TransformNode(`swLLA-${id}`, scene);
  leftLowerArm.parent = leftArm;
  leftLowerArm.position.y = -UPPER_ARM_H;

  const leftForearm = _box(`swLFA-${id}`, FOREARM_W, FOREARM_H, FOREARM_D, scene, leftLowerArm, armMat);
  leftForearm.position.y = -FOREARM_H / 2;

  const leftHand = _box(`swLH-${id}`, HAND_W, HAND_H, HAND_D, scene, leftLowerArm, handMat);
  leftHand.position.y = -FOREARM_H - HAND_H / 2;

  // ── Right Arm ──
  const rightArm = new B.TransformNode(`swRArm-${id}`, scene);
  rightArm.parent = body;
  rightArm.position.set(SHOULDER_X, SHOULDER_Y, 0);

  const rightUpperArm = _box(`swRUA-${id}`, UPPER_ARM_W, UPPER_ARM_H, UPPER_ARM_D, scene, rightArm, armMat);
  rightUpperArm.position.y = -UPPER_ARM_H / 2;

  const rightLowerArm = new B.TransformNode(`swRLA-${id}`, scene);
  rightLowerArm.parent = rightArm;
  rightLowerArm.position.y = -UPPER_ARM_H;

  const rightForearm = _box(`swRFA-${id}`, FOREARM_W, FOREARM_H, FOREARM_D, scene, rightLowerArm, armMat);
  rightForearm.position.y = -FOREARM_H / 2;

  const rightHand = _box(`swRH-${id}`, HAND_W, HAND_H, HAND_D, scene, rightLowerArm, handMat);
  rightHand.position.y = -FOREARM_H - HAND_H / 2;

  // ── Sword (attached to right hand) ──
  const sword = _buildSword(id, scene, rightHand);
  sword.position.y = -HAND_H;

  // ── Leg material ──
  const legMat = makeMat(`sw-lmat-${id}`, scene, color.scale(0.7), { emissiveFactor: 0.25 });

  // ── Left Leg ──
  const leftLeg = new B.TransformNode(`swLL-${id}`, scene);
  leftLeg.parent = body;
  leftLeg.position.set(-HIP_X, 0, 0);

  const leftUpperLeg = _box(`swLUL-${id}`, UPPER_LEG_W, UPPER_LEG_H, UPPER_LEG_D, scene, leftLeg, legMat);
  leftUpperLeg.position.y = -UPPER_LEG_H / 2;

  const leftLowerLeg = new B.TransformNode(`swLLL-${id}`, scene);
  leftLowerLeg.parent = leftLeg;
  leftLowerLeg.position.y = -UPPER_LEG_H;

  const leftShin = _box(`swLS-${id}`, SHIN_W, SHIN_H, SHIN_D, scene, leftLowerLeg, legMat);
  leftShin.position.y = -SHIN_H / 2;

  // ── Right Leg ──
  const rightLeg = new B.TransformNode(`swRL-${id}`, scene);
  rightLeg.parent = body;
  rightLeg.position.set(HIP_X, 0, 0);

  const rightUpperLeg = _box(`swRUL-${id}`, UPPER_LEG_W, UPPER_LEG_H, UPPER_LEG_D, scene, rightLeg, legMat);
  rightUpperLeg.position.y = -UPPER_LEG_H / 2;

  const rightLowerLeg = new B.TransformNode(`swRLL-${id}`, scene);
  rightLowerLeg.parent = rightLeg;
  rightLowerLeg.position.y = -UPPER_LEG_H;

  const rightShin = _box(`swRS-${id}`, SHIN_W, SHIN_H, SHIN_D, scene, rightLowerLeg, legMat);
  rightShin.position.y = -SHIN_H / 2;

  // ── Shadow disc ──
  const shadow = B.MeshBuilder.CreateDisc(`swShd-${id}`, {
    radius: TORSO_W * 0.9, tessellation: 6
  }, scene);
  shadow.rotation.x = Math.PI / 2;
  shadow.position.y = 0.1;
  shadow.parent = root;
  shadow.material = _getShadowMat(scene);
  shadow.isPickable = false;
  shadow.alwaysSelectAsActiveMesh = true;

  // ── GUI-based name label & HP bar ──
  const GUI = window.BABYLON.GUI;
  const adt = getGuiTexture();

  // Name label
  const nameLabel = new GUI.TextBlock(`sw-lbl-${id}`);
  const displayName = (bot.name || '???');
  nameLabel.text = displayName.length > 12 ? displayName.slice(0, 11) + '\u2026' : displayName;
  nameLabel.color = 'white';
  nameLabel.fontSize = 14;
  nameLabel.fontFamily = 'monospace';
  nameLabel.fontWeight = 'bold';
  nameLabel.resizeToFit = true;
  adt.addControl(nameLabel);
  nameLabel.linkWithMesh(root);
  nameLabel.linkOffsetY = -50;

  // HP bar background container
  const hpContainer = new GUI.Rectangle(`sw-hpbg-${id}`);
  hpContainer.width = '60px';
  hpContainer.height = '8px';
  hpContainer.background = '#1a1a1a';
  hpContainer.thickness = 0;
  hpContainer.alpha = 0.85;
  adt.addControl(hpContainer);
  hpContainer.linkWithMesh(root);
  hpContainer.linkOffsetY = -38;

  // HP bar fill
  const hpFill = new GUI.Rectangle(`sw-hp-${id}`);
  hpFill.width = 1;
  hpFill.height = 1;
  hpFill.background = '#00ff00';
  hpFill.thickness = 0;
  hpFill.horizontalAlignment = GUI.Control.HORIZONTAL_ALIGNMENT_LEFT;
  hpContainer.addControl(hpFill);

  // ── Joint references for animation system ──
  const joints = {
    body,
    torso,
    head,
    leftArm,
    leftLowerArm,
    rightArm,
    rightLowerArm,
    leftLeg,
    leftLowerLeg,
    rightLeg,
    rightLowerLeg,
    sword,
  };

  return {
    // Standard BotRenderer fields
    root,
    body: root,          // updateBotAnim passes this as 'body' — root handles world pos/facing
    bodyMat,
    head,
    headMat,
    lArm: leftArm,
    rArm: rightArm,
    armMat,
    shadow,
    weapon: sword,       // kept for disposeWeapon compat
    hpContainer,
    hpFill,
    nameLabel,
    anim: new SwordsmanAnimState(),
    isAlive: true,
    _wasAlive: true,
    _lastHp: -1,

    // Swordsman-specific
    isSwordsman: true,
    joints,

    // Per-bot mats for disposal
    _swMats: [bodyMat, headMat, armMat, handMat, legMat],
  };
}

/**
 * Dispose a swordsman entry and all its per-bot materials.
 * Shared weapon materials are NOT disposed (they persist across bots).
 */
export function disposeSwordsmanEntry(entry) {
  // Remove GUI controls from the fullscreen texture
  if (entry.nameLabel) entry.nameLabel.dispose();
  if (entry.hpContainer) entry.hpContainer.dispose();
  for (const mat of entry._swMats) {
    if (mat && !mat.isDisposed) mat.dispose();
  }
  entry.root.dispose();
}
