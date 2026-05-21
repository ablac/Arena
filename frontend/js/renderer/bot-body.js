'use strict';

/**
 * Bot body mesh construction — humanoid figure from MeshBuilder primitives.
 * Shared materials for shadow, HP bar BG. Per-bot materials only where colour differs.
 * @module renderer/bot-body
 */

import { parseColor, makeMat } from './utils.js';
import { createWeaponMesh, disposeWeapon } from './weapons.js?v=20260521f';
import { BotAnimState } from './animations.js?v=20260521f';
import { createSwordsmanEntry, disposeSwordsmanEntry } from './swordsman-body.js';

const BODY_H = 12;
const BODY_R = 5;
const HEAD_R = 4;
const ARM_H = 10;
const ARM_R = 1.5;

/** Shared materials (created once, reused across all bots). */
let _shdMat = null;

/** Template meshes for instancing (lazy-initialized singletons). */
let _tplArm = null;
let _tplArmMat = null;
let _tplShadow = null;
let _pickMat = null;

/** Singleton fullscreen GUI texture for all bot HUD elements. */
let _guiTexture = null;

/**
 * Get or create the singleton AdvancedDynamicTexture for bot GUI overlays.
 * @returns {BABYLON.GUI.AdvancedDynamicTexture}
 */
export function getGuiTexture() {
  if (!_guiTexture || _guiTexture.isDisposed) {
    const GUI = window.BABYLON.GUI;
    _guiTexture = GUI.AdvancedDynamicTexture.CreateFullscreenUI('botUI');
  }
  return _guiTexture;
}

function _getShadowMat(scene) {
  if (!_shdMat || _shdMat.isDisposed) {
    const B = window.BABYLON;
    _shdMat = new B.StandardMaterial('smat-shared', scene);
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

/**
 * Get or create the singleton arm template mesh for instancing.
 * Hidden source mesh; each bot arm is an InstancedMesh created from this.
 */
function _getTplArm(scene) {
  if (!_tplArm || _tplArm.isDisposed()) {
    const B = window.BABYLON;
    _tplArm = B.MeshBuilder.CreateCylinder('tpl-arm', {
      height: ARM_H, diameter: ARM_R * 2, tessellation: 4
    }, scene);
    _tplArm.setEnabled(false);
    _tplArm.isPickable = false;
    _tplArm.registerInstancedBuffer('color', 4);
    _tplArm.instancedBuffers.color = new B.Color4(0.5, 0.5, 0.5, 1);

    _tplArmMat = new B.StandardMaterial('tpl-arm-mat', scene);
    _tplArmMat.specularColor = B.Color3.Black();
    _tplArmMat.emissiveColor = new B.Color3(0.15, 0.15, 0.15);
    _tplArmMat.backFaceCulling = false;
    _tplArm.material = _tplArmMat;
  }
  return _tplArm;
}

/**
 * Get or create the singleton shadow disc template mesh for instancing.
 * All shadows share the same black semi-transparent material.
 */
export function _getTplShadow(scene) {
  if (!_tplShadow || _tplShadow.isDisposed()) {
    const B = window.BABYLON;
    _tplShadow = B.MeshBuilder.CreateDisc('tpl-shadow', {
      radius: BODY_R * 1.3, tessellation: 6
    }, scene);
    _tplShadow.rotation.x = Math.PI / 2;
    _tplShadow.setEnabled(false);
    _tplShadow.isPickable = false;
    _tplShadow.material = _getShadowMat(scene);
  }
  return _tplShadow;
}

function _getPickMat(scene) {
  if (!_pickMat || _pickMat.isDisposed) {
    const B = window.BABYLON;
    _pickMat = new B.StandardMaterial('bot-pick-mat', scene);
    _pickMat.diffuseColor = B.Color3.Black();
    _pickMat.emissiveColor = B.Color3.Black();
    _pickMat.alpha = 0.001;
    _pickMat.disableLighting = true;
    _pickMat.backFaceCulling = false;
  }
  return _pickMat;
}

export function createBotEntry(bot, scene) {
  // Sword bots get the articulated swordsman character
  if ((bot.weapon || 'sword') === 'sword') {
    return createSwordsmanEntry(bot, scene);
  }

  const B = window.BABYLON;
  const id = bot.bot_id;
  const color = parseColor(bot.avatar_color);

  const root = new B.TransformNode(`botRoot-${id}`, scene);

  // Body cylinder
  const body = B.MeshBuilder.CreateCylinder(`body-${id}`, {
    height: BODY_H, diameter: BODY_R * 2, tessellation: 6
  }, scene);
  body.position.y = BODY_H / 2;
  body.parent = root;
  body.isPickable = true;
  body.metadata = { botId: id };
  body.alwaysSelectAsActiveMesh = true;
  const bodyMat = makeMat(`bmat-${id}`, scene, color, { emissiveFactor: 0.35 });
  bodyMat.emissiveFresnelParameters = new B.FresnelParameters({
    bias: 0.6,
    power: 2,
    leftColor: new B.Color3(color.r * 0.8, color.g * 0.8, color.b * 0.8),
    rightColor: B.Color3.Black()
  });
  body.material = bodyMat;

  // Head sphere
  const head = B.MeshBuilder.CreateSphere(`head-${id}`, {
    diameter: HEAD_R * 2, segments: 4
  }, scene);
  head.isPickable = true;
  head.metadata = { botId: id };
  head.alwaysSelectAsActiveMesh = true;
  head.position.y = BODY_H + HEAD_R * 0.7;
  head.parent = root;
  const headColor = new B.Color3(
    Math.min(color.r * 1.2, 1), Math.min(color.g * 1.2, 1), Math.min(color.b * 1.2, 1)
  );
  const headMat = makeMat(`hmat-${id}`, scene, headColor, { emissiveFactor: 0.4 });
  headMat.emissiveFresnelParameters = new B.FresnelParameters({
    bias: 0.5,
    power: 2,
    leftColor: new B.Color3(color.r * 0.8, color.g * 0.8, color.b * 0.8),
    rightColor: B.Color3.Black()
  });
  head.material = headMat;

  // Arms — instanced from shared template (per-instance color)
  const armColor = new B.Color4(color.r * 0.8, color.g * 0.8, color.b * 0.8, 1);
  const lArm = _getTplArm(scene).createInstance(`larm-${id}`);
  lArm.instancedBuffers.color = armColor;
  lArm.position.set(-BODY_R - ARM_R, BODY_H * 0.6, 0);
  lArm.parent = root;
  lArm.isPickable = false;
  lArm.alwaysSelectAsActiveMesh = true;

  const rArm = _getTplArm(scene).createInstance(`rarm-${id}`);
  rArm.instancedBuffers.color = armColor;
  rArm.position.set(BODY_R + ARM_R, BODY_H * 0.6, 0);
  rArm.parent = root;
  rArm.isPickable = false;
  rArm.alwaysSelectAsActiveMesh = true;

  // Shadow disc — instanced from shared template (all shadows identical)
  const shadow = _getTplShadow(scene).createInstance(`shd-${id}`);
  shadow.position.y = 0.1;
  shadow.parent = root;
  shadow.isPickable = false;
  shadow.alwaysSelectAsActiveMesh = true;

  const selector = B.MeshBuilder.CreateCylinder(`pick-${id}`, {
    height: 34, diameter: 28, tessellation: 12,
  }, scene);
  selector.parent = root;
  selector.position.y = 16;
  selector.material = _getPickMat(scene);
  selector.isPickable = true;
  selector.metadata = { botId: id };
  selector.visibility = 0.01;
  selector.alwaysSelectAsActiveMesh = true;

  // Weapon
  const weapon = createWeaponMesh(bot.weapon || 'sword', id, scene, root);

  // ── GUI-based name label & HP bar ──
  const GUI = window.BABYLON.GUI;
  const adt = getGuiTexture();

  // Name label (TextBlock linked to root mesh)
  const nameLabel = new GUI.TextBlock(`lbl-${id}`);
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
  const hpContainer = new GUI.Rectangle(`hpbg-${id}`);
  hpContainer.width = '60px';
  hpContainer.height = '8px';
  hpContainer.background = '#1a1a1a';
  hpContainer.thickness = 0;
  hpContainer.alpha = 0.85;
  adt.addControl(hpContainer);
  hpContainer.linkWithMesh(root);
  hpContainer.linkOffsetY = -38;

  // HP bar fill
  const hpFill = new GUI.Rectangle(`hp-${id}`);
  hpFill.width = 1;
  hpFill.height = 1;
  hpFill.background = '#00ff00';
  hpFill.thickness = 0;
  hpFill.horizontalAlignment = GUI.Control.HORIZONTAL_ALIGNMENT_LEFT;
  hpContainer.addControl(hpFill);

  return {
    root, body, bodyMat, head, headMat, lArm, rArm,
    shadow, weapon, hpContainer, hpFill, nameLabel,
    selector, pickMeshes: [selector, body, head],
    anim: new BotAnimState(),
    isAlive: true, _wasAlive: true, _lastHp: -1,
  };
}

export function disposeBotEntry(entry) {
  if (entry.isSwordsman) {
    disposeSwordsmanEntry(entry);
    return;
  }
  // Remove GUI controls from the fullscreen texture
  if (entry.hpFill) entry.hpFill.dispose();
  if (entry.hpContainer) entry.hpContainer.dispose();
  if (entry.nameLabel) entry.nameLabel.dispose();
  // Only dispose per-bot materials (not shared/instanced ones)
  for (const k of ['bodyMat', 'headMat']) {
    if (entry[k] && !entry[k].isDisposed) entry[k].dispose();
  }
  // Arms and shadow are InstancedMeshes — dispose instances (not the template)
  if (entry.lArm && !entry.lArm.isDisposed()) entry.lArm.dispose();
  if (entry.rArm && !entry.rArm.isDisposed()) entry.rArm.dispose();
  if (entry.shadow && !entry.shadow.isDisposed()) entry.shadow.dispose();
  if (entry.selector && !entry.selector.isDisposed()) entry.selector.dispose();
  if (entry.weapon) disposeWeapon(entry.weapon);
  entry.root.dispose();
}

/**
 * Set HP bar color based on health ratio.
 * @param {BABYLON.GUI.Rectangle} fill - The GUI fill rectangle
 * @param {number} ratio - HP ratio 0..1
 */
export function setHpColor(fill, ratio) {
  if (ratio > 0.6) {
    fill.background = '#00ff00';
  } else if (ratio > 0.3) {
    fill.background = '#ffff00';
  } else {
    fill.background = '#ff0000';
  }
}
