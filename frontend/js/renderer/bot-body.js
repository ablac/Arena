'use strict';

/**
 * Bot body mesh construction — humanoid figure from MeshBuilder primitives.
 * Shared materials for shadow, HP bar BG. Per-bot materials only where colour differs.
 * @module renderer/bot-body
 */

import { parseColor, makeMat, createTextPlane } from './utils.js';
import { createWeaponMesh, disposeWeapon } from './weapons.js';
import { BotAnimState } from './animations.js';

const BODY_H = 12;
const BODY_R = 5;
const HEAD_R = 4;
const ARM_H = 10;
const ARM_R = 1.5;
const HP_BAR_W = 40;
const HP_BAR_H = 4;
const LABEL_W = 80;
const LABEL_H = 25;

/** Shared materials (created once, reused across all bots). */
let _shdMat = null;
let _hpBgMat = null;

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

function _getHpBgMat(scene) {
  if (!_hpBgMat || _hpBgMat.isDisposed) {
    const B = window.BABYLON;
    _hpBgMat = new B.StandardMaterial('hpbgm-shared', scene);
    _hpBgMat.diffuseColor = new B.Color3(0.1, 0.1, 0.1);
    _hpBgMat.specularColor = B.Color3.Black();
    _hpBgMat.emissiveColor = B.Color3.Black();
    _hpBgMat.disableLighting = true;
    _hpBgMat.alpha = 0.7;
    _hpBgMat.backFaceCulling = false;
    _hpBgMat.freeze();
  }
  return _hpBgMat;
}

export function createBotEntry(bot, scene) {
  const B = window.BABYLON;
  const id = bot.bot_id;
  const color = parseColor(bot.avatar_color);

  const root = new B.TransformNode(`botRoot-${id}`, scene);

  // Body cylinder
  const body = B.MeshBuilder.CreateCylinder(`body-${id}`, {
    height: BODY_H, diameter: BODY_R * 2, tessellation: 8
  }, scene);
  body.position.y = BODY_H / 2;
  body.parent = root;
  const bodyMat = makeMat(`bmat-${id}`, scene, color, { emissiveFactor: 0.35 });
  body.material = bodyMat;

  // Head sphere
  const head = B.MeshBuilder.CreateSphere(`head-${id}`, {
    diameter: HEAD_R * 2, segments: 6
  }, scene);
  head.position.y = BODY_H + HEAD_R * 0.7;
  head.parent = root;
  const headColor = new B.Color3(
    Math.min(color.r * 1.2, 1), Math.min(color.g * 1.2, 1), Math.min(color.b * 1.2, 1)
  );
  const headMat = makeMat(`hmat-${id}`, scene, headColor, { emissiveFactor: 0.4 });
  head.material = headMat;

  // Arms — share one material
  const armMat = makeMat(`amat-${id}`, scene, color.scale(0.8), { emissiveFactor: 0.3 });
  const lArm = B.MeshBuilder.CreateCylinder(`larm-${id}`, {
    height: ARM_H, diameter: ARM_R * 2, tessellation: 6
  }, scene);
  lArm.position.set(-BODY_R - ARM_R, BODY_H * 0.6, 0);
  lArm.parent = root;
  lArm.material = armMat;

  const rArm = B.MeshBuilder.CreateCylinder(`rarm-${id}`, {
    height: ARM_H, diameter: ARM_R * 2, tessellation: 6
  }, scene);
  rArm.position.set(BODY_R + ARM_R, BODY_H * 0.6, 0);
  rArm.parent = root;
  rArm.material = armMat;

  // Shadow disc (shared material)
  const shadow = B.MeshBuilder.CreateDisc(`shd-${id}`, {
    radius: BODY_R * 1.3, tessellation: 8
  }, scene);
  shadow.rotation.x = Math.PI / 2;
  shadow.position.y = 0.1;
  shadow.parent = root;
  shadow.material = _getShadowMat(scene);

  // Weapon
  const weapon = createWeaponMesh(bot.weapon || 'sword', id, scene, root);

  // Name label
  const label = createTextPlane(id, bot.name || '???', color, scene, LABEL_W, LABEL_H);
  label.plane.position.y = BODY_H + HEAD_R * 2 + 8;
  label.plane.parent = root;

  // Health bar BG (shared material)
  const hpBg = B.MeshBuilder.CreatePlane(`hpbg-${id}`, {
    width: HP_BAR_W, height: HP_BAR_H
  }, scene);
  hpBg.billboardMode = B.Mesh.BILLBOARDMODE_ALL;
  hpBg.position.y = BODY_H + HEAD_R * 2 + 2;
  hpBg.parent = root;
  hpBg.material = _getHpBgMat(scene);

  // Health bar fill (needs unique mat for per-bot color changes)
  const hpBar = B.MeshBuilder.CreatePlane(`hp-${id}`, {
    width: HP_BAR_W, height: HP_BAR_H
  }, scene);
  hpBar.billboardMode = B.Mesh.BILLBOARDMODE_ALL;
  hpBar.position.y = BODY_H + HEAD_R * 2 + 2.2;
  hpBar.parent = root;
  const hpMat = makeMat(`hpm-${id}`, scene, new B.Color3(0, 1, 0), {
    noLight: true, emissiveFactor: 1
  });
  hpBar.material = hpMat;

  return {
    root, body, bodyMat, head, headMat, lArm, rArm, armMat,
    shadow, weapon, hpBar, hpMat, hpBg,
    label, anim: new BotAnimState(),
    prevPos: null, currPos: null, isAlive: true, _wasAlive: true,
    _lastHp: -1,
  };
}

export function disposeBotEntry(entry) {
  if (entry.label) {
    entry.label.plane.dispose();
    entry.label.mat.dispose();
    entry.label.tex.dispose();
  }
  // Only dispose per-bot materials (not shared ones)
  for (const k of ['bodyMat', 'headMat', 'armMat', 'hpMat']) {
    if (entry[k]) entry[k].dispose();
  }
  if (entry.weapon) disposeWeapon(entry.weapon);
  entry.root.dispose();
}

export function setHpColor(mat, ratio) {
  if (ratio > 0.6) {
    mat.diffuseColor.r = 0; mat.diffuseColor.g = 1; mat.diffuseColor.b = 0;
  } else if (ratio > 0.3) {
    mat.diffuseColor.r = 1; mat.diffuseColor.g = 1; mat.diffuseColor.b = 0;
  } else {
    mat.diffuseColor.r = 1; mat.diffuseColor.g = 0; mat.diffuseColor.b = 0;
  }
  mat.emissiveColor.r = mat.diffuseColor.r;
  mat.emissiveColor.g = mat.diffuseColor.g;
  mat.emissiveColor.b = mat.diffuseColor.b;
}
