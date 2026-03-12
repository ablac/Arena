'use strict';

/**
 * Bot body mesh construction — humanoid figure from MeshBuilder primitives.
 * Body=cylinder, head=sphere, arms=thin cylinders.
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

/**
 * Create all meshes for a single bot.
 * @param {Object} bot - bot state from server
 * @param {BABYLON.Scene} scene
 * @returns {Object} entry with all meshes and state
 */
export function createBotEntry(bot, scene) {
  const B = window.BABYLON;
  const id = bot.bot_id;
  const color = parseColor(bot.avatar_color);

  // Root transform
  const root = new B.TransformNode(`botRoot-${id}`, scene);

  // Body cylinder
  const body = B.MeshBuilder.CreateCylinder(`body-${id}`, {
    height: BODY_H, diameter: BODY_R * 2, tessellation: 12
  }, scene);
  body.position.y = BODY_H / 2;
  body.parent = root;
  const bodyMat = makeMat(`bmat-${id}`, scene, color, { emissiveFactor: 0.35 });
  body.material = bodyMat;

  // Head sphere
  const head = B.MeshBuilder.CreateSphere(`head-${id}`, {
    diameter: HEAD_R * 2, segments: 10
  }, scene);
  head.position.y = BODY_H + HEAD_R * 0.7;
  head.parent = root;
  const headColor = new B.Color3(
    Math.min(color.r * 1.2, 1), Math.min(color.g * 1.2, 1), Math.min(color.b * 1.2, 1)
  );
  const headMat = makeMat(`hmat-${id}`, scene, headColor, { emissiveFactor: 0.4 });
  head.material = headMat;

  // Arms
  const armMat = makeMat(`amat-${id}`, scene, color.scale(0.8), { emissiveFactor: 0.3 });
  const lArm = B.MeshBuilder.CreateCylinder(`larm-${id}`, {
    height: ARM_H, diameter: ARM_R * 2
  }, scene);
  lArm.position.set(-BODY_R - ARM_R, BODY_H * 0.6, 0);
  lArm.parent = root;
  lArm.material = armMat;

  const rArm = B.MeshBuilder.CreateCylinder(`rarm-${id}`, {
    height: ARM_H, diameter: ARM_R * 2
  }, scene);
  rArm.position.set(BODY_R + ARM_R, BODY_H * 0.6, 0);
  rArm.parent = root;
  rArm.material = armMat;

  // Shadow disc under feet
  const shadow = B.MeshBuilder.CreateDisc(`shd-${id}`, {
    radius: BODY_R * 1.3, tessellation: 16
  }, scene);
  shadow.rotation.x = Math.PI / 2;
  shadow.position.y = 0.1;
  shadow.parent = root;
  const shdMat = makeMat(`smat-${id}`, scene, new B.Color3(0, 0, 0), {
    noLight: true, alpha: 0.3, emissive: false
  });
  shadow.material = shdMat;

  // Weapon
  const weapon = createWeaponMesh(bot.weapon || 'sword', id, scene, root);

  // Name label (floating above)
  const label = createTextPlane(id, bot.name || '???', color, scene, LABEL_W, LABEL_H);
  label.plane.position.y = BODY_H + HEAD_R * 2 + 8;
  label.plane.parent = root;

  // Health bar BG
  const hpBg = B.MeshBuilder.CreatePlane(`hpbg-${id}`, {
    width: HP_BAR_W, height: HP_BAR_H
  }, scene);
  hpBg.rotation.x = Math.PI / 2;
  hpBg.position.y = BODY_H + HEAD_R * 2 + 2;
  hpBg.parent = root;
  const hpBgMat = makeMat(`hpbgm-${id}`, scene, new B.Color3(0.1, 0.1, 0.1), {
    noLight: true, alpha: 0.7, emissive: false
  });
  hpBg.material = hpBgMat;

  // Health bar fill
  const hpBar = B.MeshBuilder.CreatePlane(`hp-${id}`, {
    width: HP_BAR_W, height: HP_BAR_H
  }, scene);
  hpBar.rotation.x = Math.PI / 2;
  hpBar.position.y = BODY_H + HEAD_R * 2 + 2.2;
  hpBar.parent = root;
  const hpMat = makeMat(`hpm-${id}`, scene, new B.Color3(0, 1, 0), {
    noLight: true, emissiveFactor: 1
  });
  hpBar.material = hpMat;

  return {
    root, body, bodyMat, head, headMat, lArm, rArm, armMat,
    shadow, shdMat, weapon, hpBar, hpMat, hpBg, hpBgMat,
    label, anim: new BotAnimState(),
    prevPos: null, currPos: null, isAlive: true, _wasAlive: true,
  };
}

/**
 * Dispose all meshes for a bot entry.
 * @param {Object} entry
 */
export function disposeBotEntry(entry) {
  // Dispose label
  if (entry.label) {
    entry.label.plane.dispose();
    entry.label.mat.dispose();
    entry.label.tex.dispose();
  }
  // Materials
  for (const k of ['bodyMat', 'headMat', 'armMat', 'shdMat', 'hpMat', 'hpBgMat']) {
    if (entry[k]) entry[k].dispose();
  }
  // Weapon
  if (entry.weapon) {
    disposeWeapon(entry.weapon);
  }
  entry.root.dispose();
}

/**
 * Set HP bar color based on ratio.
 * @param {BABYLON.StandardMaterial} mat
 * @param {number} ratio 0..1
 */
export function setHpColor(mat, ratio) {
  const B = window.BABYLON;
  if (ratio > 0.6) {
    mat.diffuseColor = new B.Color3(0, 1, 0);
  } else if (ratio > 0.3) {
    mat.diffuseColor = new B.Color3(1, 1, 0);
  } else {
    mat.diffuseColor = new B.Color3(1, 0, 0);
  }
  mat.emissiveColor = mat.diffuseColor.clone();
}
