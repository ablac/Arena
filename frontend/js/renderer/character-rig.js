'use strict';

/**
 * Shared articulated body for the seven Forge combat chassis.
 *
 * Geometry is deliberately economical: a graphite instanced skeleton carries
 * two mutable avatar-color materials and weapon-specific silhouette accents.
 * TransformNodes provide articulation and semantic cosmetic mounts without
 * adding render submissions.
 * @module renderer/character-rig
 */

import {parseColor, makeMat} from './utils.js';
import {getCharacterProfile} from './character-roster.js?v=20260712c';
import {ForgeAnimState} from './character-anims.js?v=20260712c';
import {createForgeWeapon, disposeForgeWeapon} from './forge-weapons.js?v=20260713b';
import {bodyFormForAsset} from './body-form-roster.js?v=20260713b';
import {buildBodyFormGeometry, createBodyFormFarProxy} from './body-form-geometry.js?v=20260713b';

const _sceneResources = new WeakMap();

// Separate enter/exit distances prevent rapid camera movement near the
// boundary from flipping an entire crowd between detail levels each frame.
export const FORGE_FAR_LOD_ENTER_DISTANCE = 1320;
export const FORGE_FAR_LOD_EXIT_DISTANCE = 1140;

// A distant bot still needs to read as a character at a glance. These six
// normalized pieces are merged once per scene, then share geometry across live
// bots. A tiny per-bot material keeps avatar identity without restoring the
// articulated body's much larger draw/update cost.
export const FORGE_FAR_LOD_PROXY_PARTS = Object.freeze([
  Object.freeze({role: 'torso', shape: 'box', position: [0, 0.57, 0], scaling: [0.56, 0.48, 0.32]}),
  Object.freeze({role: 'head', shape: 'sphere', position: [0, 0.88, 0], scaling: [0.30, 0.26, 0.29]}),
  Object.freeze({role: 'arm-left', shape: 'box', position: [-0.39, 0.56, 0], scaling: [0.15, 0.43, 0.19]}),
  Object.freeze({role: 'arm-right', shape: 'box', position: [0.39, 0.56, 0], scaling: [0.15, 0.43, 0.19]}),
  Object.freeze({role: 'leg-left', shape: 'box', position: [-0.18, 0.185, 0], scaling: [0.19, 0.37, 0.22]}),
  Object.freeze({role: 'leg-right', shape: 'box', position: [0.18, 0.185, 0], scaling: [0.19, 0.37, 0.22]}),
]);

function sharedMaterial(scene, name, diffuse, emissive, specular) {
  const B = window.BABYLON;
  const material = new B.StandardMaterial(name, scene);
  material.diffuseColor = diffuse;
  material.emissiveColor = emissive;
  material.specularColor = specular;
  material.backFaceCulling = true;
  // Shared chassis pieces must keep a readable floor in the arena's near-black
  // sectors. Accent/core materials still react to status and cosmetics, while
  // these scene-owned structural materials deliberately do not depend on a
  // light reaching every articulated limb.
  material.disableLighting = true;
  material.freeze();
  return material;
}

function createFarSilhouetteTemplate(B, scene, material) {
  const parts = FORGE_FAR_LOD_PROXY_PARTS.map(part => {
    const mesh = part.shape === 'sphere'
      ? B.MeshBuilder.CreateSphere(`forge-low-${part.role}`, {diameter: 1, segments: 8}, scene)
      : B.MeshBuilder.CreateBox(`forge-low-${part.role}`, {size: 1}, scene);
    mesh.position.set(part.position[0], part.position[1], part.position[2]);
    mesh.scaling.set(part.scaling[0], part.scaling[1], part.scaling[2]);
    mesh.material = material;
    mesh.isPickable = false;
    return mesh;
  });
  let merged;
  if (typeof B.Mesh?.MergeMeshes === 'function') {
    merged = B.Mesh.MergeMeshes(parts, true, true, undefined, false, true);
  } else {
    // Lightweight test/render shims may not expose Babylon's static merger.
    // Retain a solid box proxy instead of making character creation fail.
    merged = parts.shift();
    for (const part of parts) part.dispose();
    merged.position.set(0, 0, 0);
    merged.scaling.set(1, 1, 1);
  }
  if (!merged) throw new Error('Unable to build the Forge far-detail silhouette');
  merged.name = 'forge-low-template';
  merged.material = material;
  merged.isPickable = false;
  merged.setEnabled(false);
  return merged;
}

function readableFarColor(B, color) {
  const maxChannel = Math.max(color.r, color.g, color.b, 0.001);
  const saturationScale = maxChannel < 0.82 ? 0.82 / maxChannel : 1;
  let red = Math.min(1, color.r * saturationScale);
  let green = Math.min(1, color.g * saturationScale);
  let blue = Math.min(1, color.b * saturationScale);
  const luminance = 0.2126 * red + 0.7152 * green + 0.0722 * blue;
  const targetLuminance = 0.56;
  if (luminance < targetLuminance) {
    const whiteMix = (targetLuminance - luminance) / Math.max(0.001, 1 - luminance);
    red += (1 - red) * whiteMix;
    green += (1 - green) * whiteMix;
    blue += (1 - blue) * whiteMix;
  }
  return new B.Color3(red, green, blue);
}

function getResources(scene) {
  let resources = _sceneResources.get(scene);
  if (resources) return resources;

  const B = window.BABYLON;
  const graphite = sharedMaterial(
    scene,
    'forge-graphite-shared',
    new B.Color3(0.28, 0.36, 0.50),
    new B.Color3(0.18, 0.24, 0.36),
    new B.Color3(0.40, 0.46, 0.54),
  );
  const gunmetal = sharedMaterial(
    scene,
    'forge-gunmetal-shared',
    new B.Color3(0.34, 0.44, 0.60),
    new B.Color3(0.22, 0.30, 0.43),
    new B.Color3(0.52, 0.59, 0.68),
  );
  const farSilhouette = sharedMaterial(
    scene,
    'forge-far-silhouette-shared',
    new B.Color3(0.38, 0.66, 1.00),
    new B.Color3(0.38, 0.66, 1.00),
    new B.Color3(0.58, 0.68, 0.82),
  );
  const selector = sharedMaterial(
    scene,
    'forge-selector-shared',
    B.Color3.Black(),
    B.Color3.Black(),
    B.Color3.Black(),
  );
  selector.alpha = 0.001;
  selector.disableLighting = true;
  selector.unfreeze();
  selector.alpha = 0.001;
  selector.freeze();

  const box = B.MeshBuilder.CreateBox('forge-box-template', {size: 1}, scene);
  box.material = graphite;
  box.isPickable = false;
  box.setEnabled(false);

  const head = B.MeshBuilder.CreateCylinder('forge-head-template', {
    height: 1, diameter: 1, tessellation: 6,
  }, scene);
  head.material = graphite;
  head.isPickable = false;
  head.setEnabled(false);

  const plate = B.MeshBuilder.CreateBox('forge-plate-template', {size: 1}, scene);
  plate.material = gunmetal;
  plate.isPickable = false;
  plate.setEnabled(false);

  // One merged humanoid silhouette replaces the articulated body on distant
  // live bots. Clones share this geometry and add one identity material each.
  const low = createFarSilhouetteTemplate(B, scene, farSilhouette);

  resources = {graphite, gunmetal, farSilhouette, selector, box, head, plate, low};
  _sceneResources.set(scene, resources);
  return resources;
}

function setTransform(node, position, scaling, rotation) {
  if (position) node.position.set(position[0], position[1], position[2]);
  if (scaling) node.scaling.set(scaling[0], scaling[1], scaling[2]);
  if (rotation) node.rotation.set(rotation[0], rotation[1], rotation[2]);
  return node;
}

function boxInstance(resources, name, parent, position, scaling) {
  const mesh = resources.box.createInstance(name);
  mesh.parent = parent;
  mesh.isPickable = false;
  return setTransform(mesh, position, scaling);
}

function headInstance(resources, name, parent, scaling) {
  const mesh = resources.head.createInstance(name);
  mesh.parent = parent;
  mesh.isPickable = false;
  return setTransform(mesh, null, scaling);
}

function plateInstance(resources, name, parent, position, scaling, rotation) {
  const mesh = resources.plate.createInstance(name);
  mesh.parent = parent;
  mesh.isPickable = false;
  return setTransform(mesh, position, scaling, rotation);
}

function accentBox(B, name, scene, parent, material, position, scaling, rotation) {
  const mesh = B.MeshBuilder.CreateBox(name, {size: 1}, scene);
  mesh.parent = parent;
  mesh.material = material;
  mesh.isPickable = false;
  return setTransform(mesh, position, scaling, rotation);
}

function setNodeEnabled(node, enabled) {
  if (node && typeof node.setEnabled === 'function') node.setEnabled(enabled);
}

/** Apply a Forge entry's detail level, including cosmetics refreshed later. */
export function setForgeCharacterLOD(entry, far) {
  const useFar = !!(
    entry?.isForgeCharacter &&
    !entry.presentationOnly &&
    entry.lowDetail &&
    far === true
  );
  if (!entry) return false;

  entry._forgeFarLOD = useFar;
  const farMeshes = new Set(entry._forgeFarMeshes || []);
  for (const mesh of entry._forgeMeshes || []) setNodeEnabled(mesh, !useFar || farMeshes.has(mesh));
  setNodeEnabled(entry.selector, !useFar);
  setNodeEnabled(entry.lowDetail, useFar);
  for (const group of entry._cosmeticState?.groups || []) setNodeEnabled(group, !useFar);
  return useFar;
}

/** Select a live Forge entry's LOD from zoom, with an optional crowd-cap override. */
export function updateForgeCharacterLOD(entry, camera, forceFar = false) {
  if (!entry) return false;
  let useFar = false;
  if (!entry.isForgeCharacter || entry.presentationOnly || !entry.lowDetail) {
    if (entry._forgeFarLOD === useFar) return useFar;
    return setForgeCharacterLOD(entry, useFar);
  }
  if (forceFar === true) {
    useFar = true;
    if (entry._forgeFarLOD === useFar) return useFar;
    return setForgeCharacterLOD(entry, useFar);
  }

  const cameraRadius = Number(camera?.radius);
  const cameraPosition = camera?.globalPosition || camera?.position;
  const rootPosition = typeof entry.root?.getAbsolutePosition === 'function'
    ? entry.root.getAbsolutePosition()
    : entry.root?.position;
  if ((!Number.isFinite(cameraRadius) || cameraRadius <= 0) && (!cameraPosition || !rootPosition)) {
    if (entry._forgeFarLOD === useFar) return useFar;
    return setForgeCharacterLOD(entry, useFar);
  }

  const boundary = entry._forgeFarLOD
    ? FORGE_FAR_LOD_EXIT_DISTANCE
    : FORGE_FAR_LOD_ENTER_DISTANCE;
  if (Number.isFinite(cameraRadius) && cameraRadius > 0) {
    // ArcRotate radius is the spectator's actual zoom level. Using per-bot
    // camera distance made edge-of-map characters turn into proxies while a
    // same-sized bot near the camera target retained its authored rig.
    useFar = cameraRadius > boundary;
  } else {
    const dx = cameraPosition.x - rootPosition.x;
    const dy = cameraPosition.y - rootPosition.y;
    const dz = cameraPosition.z - rootPosition.z;
    useFar = dx * dx + dy * dy + dz * dz > boundary * boundary;
  }
  if (entry._forgeFarLOD === useFar) return useFar;
  return setForgeCharacterLOD(entry, useFar);
}

const ARMOR_STYLE = Object.freeze({
  sword: Object.freeze({left: [2.0, 1.05, 2.6, -0.08], right: [3.15, 1.45, 2.9, -0.25]}),
  bow: Object.freeze({left: [1.55, 0.68, 2.0, 0.28], right: [1.55, 0.68, 2.0, 0.28]}),
  spear: Object.freeze({left: [1.55, 1.55, 2.2, 0.18], right: [2.05, 1.75, 2.4, -0.18]}),
  daggers: Object.freeze({left: [1.35, 0.58, 2.6, 0.42], right: [1.35, 0.58, 2.6, 0.42]}),
  staff: Object.freeze({left: [1.30, 2.05, 1.8, -0.04], right: [1.30, 2.05, 1.8, -0.04]}),
  shield: Object.freeze({left: [3.65, 1.85, 3.1, 0.04], right: [3.15, 1.60, 3.0, -0.04]}),
  grapple: Object.freeze({left: [1.55, 0.90, 2.1, 0.22], right: [3.30, 1.35, 3.2, -0.32]}),
});

function createHUD(bot, id, root, guiTexture) {
  if (!guiTexture) return {nameLabel: null, hpContainer: null, hpFill: null};
  const GUI = window.BABYLON.GUI;
  const nameLabel = new GUI.TextBlock(`forge-label-${id}`);
  const displayName = bot.name || '???';
  nameLabel.text = displayName.length > 12 ? `${displayName.slice(0, 11)}\u2026` : displayName;
  nameLabel.color = 'white';
  nameLabel.fontSize = 14;
  nameLabel.fontFamily = 'monospace';
  nameLabel.fontWeight = 'bold';
  nameLabel.resizeToFit = true;
  guiTexture.addControl(nameLabel);
  nameLabel.linkWithMesh(root);
  nameLabel.linkOffsetY = -54;

  const hpContainer = new GUI.Rectangle(`forge-hp-background-${id}`);
  hpContainer.width = '60px';
  hpContainer.height = '8px';
  hpContainer.background = '#1a1a1a';
  hpContainer.thickness = 0;
  hpContainer.alpha = 0.85;
  guiTexture.addControl(hpContainer);
  hpContainer.linkWithMesh(root);
  hpContainer.linkOffsetY = -41;

  const hpFill = new GUI.Rectangle(`forge-hp-${id}`);
  hpFill.width = 1;
  hpFill.height = 1;
  hpFill.background = '#00ff00';
  hpFill.thickness = 0;
  hpFill.horizontalAlignment = GUI.Control.HORIZONTAL_ALIGNMENT_LEFT;
  hpContainer.addControl(hpFill);
  return {nameLabel, hpContainer, hpFill};
}

/**
 * Build one Forge combat chassis.
 *
 * @param {Object} bot trusted snapshot fields (bot_id, avatar_color, weapon, name)
 * @param {BABYLON.Scene} scene
 * @param {{presentationOnly?: boolean, guiTexture?: Object, shadowTemplate?: Object}} options
 */
export function createForgeCharacter(bot, scene, options = {}) {
  const B = window.BABYLON;
  const profile = getCharacterProfile(bot.weapon);
  const bodyForm = bodyFormForAsset(bot?.cosmetics?.bot_skin);
  const id = bot.bot_id;
  const presentationOnly = options.presentationOnly === true;
  const resources = getResources(scene);
  const color = parseColor(bot.avatar_color);
  const headColor = new B.Color3(
    Math.min(1, color.r * 1.22 + 0.03),
    Math.min(1, color.g * 1.22 + 0.03),
    Math.min(1, color.b * 1.22 + 0.03),
  );

  // Mutable per-bot materials: BotRenderer owns status tint/alpha animation.
  const bodyMat = makeMat(`forge-accent-${id}`, scene, color, {
    emissiveFactor: 0.48,
    specular: new B.Color3(0.34, 0.38, 0.44),
  });
  const headMat = makeMat(`forge-core-${id}`, scene, headColor, {
    emissiveFactor: 0.82,
    specular: new B.Color3(0.42, 0.48, 0.56),
  });
  bodyMat.backFaceCulling = true;
  headMat.backFaceCulling = true;
  bodyMat._forgeRestEmissive = bodyMat.emissiveColor.clone();
  headMat._forgeRestEmissive = headMat.emissiveColor.clone();

  const root = new B.TransformNode(`forge-root-${id}`, scene);
  // Forge's authored face (visor, chest core, toes, and weapon presentation)
  // points down local -Z, while Babylon movement yaw treats +Z as forward.
  // Correct that coordinate mismatch once below the gameplay/interpolation
  // root so Shop orbits, live movement, attacks, cosmetics, and every future
  // body form all share the same canonical facing convention.
  const modelRoot = new B.TransformNode(`forge-model-root-${id}`, scene);
  modelRoot.parent = root;
  modelRoot.rotation.y = Math.PI;
  const cosmeticRoot = new B.TransformNode(`forge-cosmetic-root-${id}`, scene);
  cosmeticRoot.parent = modelRoot;

  const p = profile.proportions;
  const torsoWidth = 7.0 * p.shoulders;
  const torsoHeight = 8.15 * p.torso;
  const torsoDepth = 3.75 * (0.92 + p.torso * 0.08);
  const pelvisWidth = 4.6 * p.hips;
  const upperLegLength = 5.25 * p.leg;
  const shinLength = 4.85 * p.leg;
  const bodyY = upperLegLength + shinLength + 0.46;
  const shoulderY = 1.12 + torsoHeight * 0.78;
  const shoulderX = torsoWidth * 0.52;
  const upperArmLength = 4.25 * (0.82 + p.torso * 0.18);
  const forearmLength = 3.65 * (0.84 + p.torso * 0.16);
  const armWidth = 1.28 * (0.82 + p.shoulders * 0.18);
  const hipX = Math.max(1.15, pelvisWidth * 0.34);
  const legWidth = 1.72 * (0.82 + p.hips * 0.18);
  const headWidth = 4.35 * p.head;
  const headHeight = 3.55 * p.head;
  const headDepth = 3.65 * p.head;
  const headY = 1.12 + torsoHeight + headHeight * 0.56;
  const mountMetrics = Object.freeze({
    bodyY,
    torsoWidth,
    torsoHeight,
    torsoDepth,
    pelvisWidth,
    shoulderX,
    shoulderY,
    upperArmLength,
    forearmLength,
    armWidth,
    upperLegLength,
    shinLength,
    legWidth,
    headWidth,
    headHeight,
    headDepth,
    headY,
  });

  const bodyJoint = new B.TransformNode(`forge-body-joint-${id}`, scene);
  bodyJoint.parent = modelRoot;
  bodyJoint.position.y = bodyY;
  // Preserve the legacy cosmetic builders' root-space coordinates while
  // making their geometry inherit the articulated chassis bob/lean.
  cosmeticRoot.parent = bodyJoint;
  cosmeticRoot.position.y = -bodyY;

  const torso = boxInstance(resources, `forge-torso-${id}`, bodyJoint,
    [0, 1.08 + torsoHeight / 2, 0], [torsoWidth, torsoHeight, torsoDepth]);
  const chestPlate = plateInstance(resources, `forge-chest-plate-${id}`, bodyJoint,
    [0, 1.12 + torsoHeight * 0.57, -torsoDepth * 0.52],
    [torsoWidth * 0.68, torsoHeight * 0.33, 0.38],
    [-0.08, 0, 0]);
  const pelvis = accentBox(B, `forge-pelvis-${id}`, scene, bodyJoint, bodyMat,
    [0, 0.45, 0], [pelvisWidth, 1.75, torsoDepth * 0.84]);

  const headJoint = new B.TransformNode(`forge-head-joint-${id}`, scene);
  headJoint.parent = bodyJoint;
  headJoint.position.y = headY;
  const head = headInstance(resources, `forge-head-${id}`, headJoint,
    [headWidth, headHeight, headDepth]);
  const visor = accentBox(B, `forge-visor-${id}`, scene, headJoint, headMat,
    [0, 0.18, -headDepth * 0.51], [headWidth * 0.70, 0.58, 0.30]);

  const core = B.MeshBuilder.CreateCylinder(`forge-core-mesh-${id}`, {
    height: 0.48, diameter: Math.max(1.6, torsoWidth * 0.25), tessellation: 8,
  }, scene);
  core.parent = bodyJoint;
  core.position.set(0, 1.15 + torsoHeight * 0.56, -torsoDepth * 0.52);
  core.rotation.x = Math.PI / 2;
  core.material = headMat;
  core.isPickable = false;

  const armorStyle = ARMOR_STYLE[profile.weapon] || ARMOR_STYLE.sword;
  const limbMeshes = [];
  const arms = {};
  for (const side of [-1, 1]) {
    const label = side < 0 ? 'left' : 'right';
    const arm = new B.TransformNode(`forge-${label}-arm-${id}`, scene);
    arm.parent = bodyJoint;
    arm.position.set(side * shoulderX, shoulderY, 0);

    const upper = boxInstance(resources, `forge-${label}-upper-arm-${id}`, arm,
      [0, -upperArmLength / 2, 0], [armWidth, upperArmLength, armWidth * 1.05]);
    const elbow = new B.TransformNode(`forge-${label}-elbow-${id}`, scene);
    elbow.parent = arm;
    elbow.position.y = -upperArmLength;
    const forearm = boxInstance(resources, `forge-${label}-forearm-${id}`, elbow,
      [0, -forearmLength / 2, -0.08], [armWidth * 0.88, forearmLength, armWidth]);
    const hand = new B.TransformNode(`forge-${label}-hand-${id}`, scene);
    hand.parent = elbow;
    hand.position.y = -forearmLength;

    const style = armorStyle[label];
    const pauldron = accentBox(B, `forge-${label}-pauldron-${id}`, scene, arm, bodyMat,
      [side * 0.18, -0.42, 0], [style[0], style[1], style[2]], [0, 0, side * style[3]]);
    limbMeshes.push(upper, forearm, pauldron);
    arms[label] = {arm, elbow, hand};
  }

  const legs = {};
  for (const side of [-1, 1]) {
    const label = side < 0 ? 'left' : 'right';
    const leg = new B.TransformNode(`forge-${label}-leg-${id}`, scene);
    leg.parent = bodyJoint;
    leg.position.x = side * hipX;
    const upper = boxInstance(resources, `forge-${label}-upper-leg-${id}`, leg,
      [0, -upperLegLength / 2, 0], [legWidth, upperLegLength, legWidth * 1.15]);

    const knee = new B.TransformNode(`forge-${label}-knee-${id}`, scene);
    knee.parent = leg;
    knee.position.y = -upperLegLength;
    const shin = boxInstance(resources, `forge-${label}-shin-${id}`, knee,
      [0, -shinLength / 2, 0], [legWidth * 0.86, shinLength, legWidth]);
    const foot = boxInstance(resources, `forge-${label}-foot-${id}`, knee,
      [0, -shinLength + 0.05, -0.68], [legWidth * 1.06, 0.86, 2.75]);
    limbMeshes.push(upper, shin, foot);
    legs[label] = {leg, knee};
  }

  const backMount = new B.TransformNode(`forge-back-mount-${id}`, scene);
  backMount.parent = bodyJoint;
  backMount.position.set(0, 1.12 + torsoHeight * 0.58, torsoDepth * 0.52);

  const mounts = {
    head: headJoint,
    chest: bodyJoint,
    back: backMount,
    shoulderL: arms.left.arm,
    shoulderR: arms.right.arm,
    handL: arms.left.hand,
    handR: arms.right.hand,
    weapon: null,
    core,
    cosmeticRoot,
  };
  const weapon = createForgeWeapon(profile, id, scene, mounts, headMat, {
    handSpan: shoulderX,
  });
  mounts.weapon = weapon;

  const weaponPoseNodes = weapon._forgePoseNodes || [weapon];
  const weaponBases = weaponPoseNodes.map(node => ({
    x: node.position.x,
    y: node.position.y,
    z: node.position.z,
    rx: node.rotation.x,
    ry: node.rotation.y,
    rz: node.rotation.z,
    sign: node._forgePoseSign || 1,
  }));
  const weaponBase = weaponBases[0];

  let shadow = null;
  if (options.shadowTemplate && typeof options.shadowTemplate.createInstance === 'function') {
    shadow = options.shadowTemplate.createInstance(`forge-shadow-${id}`);
    shadow.parent = root;
    shadow.position.y = 0.1;
    shadow.scaling.setAll(Math.max(0.80, torsoWidth / 7.0));
    shadow.isPickable = false;
  }

  let selector = null;
  let lowDetail = null;
  let lowDetailMat = null;
  if (!presentationOnly) {
    selector = B.MeshBuilder.CreateCylinder(`forge-selector-${id}`, {
      height: bodyY + headY + headHeight + 2,
      diameter: Math.max(24, torsoWidth + 15),
      tessellation: 8,
    }, scene);
    selector.parent = root;
    selector.position.y = (bodyY + headY) / 2;
    selector.material = resources.selector;
    selector.visibility = 0.01;
    selector.isPickable = true;
    selector.metadata = {botId: id};

    const lowHeight = bodyY + headY + headHeight * 0.64;
    if (bodyForm) {
      lowDetail = createBodyFormFarProxy(bodyForm, scene, modelRoot, {
        width: Math.max(torsoWidth, pelvisWidth, headWidth) * 1.48,
        height: lowHeight * 1.05,
        depth: Math.max(torsoDepth, headDepth) * 1.55,
      });
    } else {
      const lowColor = readableFarColor(B, color);
      lowDetailMat = makeMat(`forge-low-identity-${id}`, scene, lowColor, {
        emissiveFactor: 1,
        noLight: true,
        specular: new B.Color3(0.20, 0.24, 0.30),
        backFace: true,
      });
      lowDetailMat.backFaceCulling = true;
      lowDetailMat.freeze();
      // A clone keeps the single merged scene geometry while allowing each bot
      // to retain its own readable avatar color at extreme spectator zoom.
      lowDetail = resources.low.clone(`forge-low-${id}`);
      lowDetail.parent = modelRoot;
      lowDetail.material = lowDetailMat;
      lowDetail.position.y = 0;
      lowDetail.scaling.set(
        Math.max(torsoWidth, pelvisWidth, headWidth) * 1.34 / 0.9,
        lowHeight * 1.05,
        Math.max(torsoDepth, headDepth) * 1.30 / 0.32,
      );
    }
    lowDetail.isPickable = true;
    lowDetail.metadata = {botId: id};
    lowDetail.setEnabled(false);
  }

  torso.isPickable = !presentationOnly;
  torso.metadata = {botId: id};
  head.isPickable = !presentationOnly;
  head.metadata = {botId: id};

  const hud = presentationOnly
    ? {nameLabel: null, hpContainer: null, hpFill: null}
    : createHUD(bot, id, root, options.guiTexture);
  const joints = {
    body: bodyJoint,
    torso,
    head: headJoint,
    leftArm: arms.left.arm,
    leftElbow: arms.left.elbow,
    rightArm: arms.right.arm,
    rightElbow: arms.right.elbow,
    leftLeg: legs.left.leg,
    leftKnee: legs.left.knee,
    rightLeg: legs.right.leg,
    rightKnee: legs.right.knee,
    core,
  };
  const basePose = {
    bodyY,
    armLRoll: profile.weapon === 'bow' ? -0.08 : 0.05,
    armRRoll: profile.weapon === 'shield' ? 0.10 : -0.05,
    elbowLPitch: profile.weapon === 'shield' ? -0.36 : -0.10,
    elbowRPitch: profile.weapon === 'grapple' ? -0.28 : -0.10,
    kneePitch: 0.05 + profile.motion.weight * 0.04,
  };

  let renderedBody = torso;
  let renderedHead = head;
  let renderedBodyMat = bodyMat;
  let renderedHeadMat = headMat;
  let bodyFormMaterials = [];
  let bodyFormMeshes = [];
  if (bodyForm) {
    const geometry = buildBodyFormGeometry(bodyForm, {
      scene, id, joints, metrics: mountMetrics,
    });
    bodyFormMeshes = geometry.meshes;
    bodyFormMaterials = geometry.materials;
    renderedBody = geometry.body;
    renderedHead = geometry.head;
    renderedBodyMat = geometry.materials[0];
    renderedHeadMat = geometry.materials[1];
    joints.torso = renderedBody;
    // The skeleton, semantic mounts, Arena core, and weapon stay shared, but
    // the robot shell itself is removed so a full-body skin never overlays or
    // reveals an invisible second character.
    for (const mesh of [torso, chestPlate, pelvis, head, visor, ...limbMeshes]) mesh.dispose();
    // The standard-shell accent material no longer has a mesh after the shell
    // is removed. Release it immediately instead of retaining one dead mutable
    // material for every full-body character in a large crowd.
    bodyMat.dispose();
  }

  renderedBody.isPickable = !presentationOnly;
  renderedBody.metadata = {botId: id};
  renderedHead.isPickable = !presentationOnly;
  renderedHead.metadata = {botId: id};

  const visibleMeshes = bodyForm
    ? [core, ...bodyFormMeshes, ...weapon._forgeMeshes]
    : [torso, chestPlate, pelvis, head, visor, core, ...limbMeshes, ...weapon._forgeMeshes];
  // Status feedback owns this exact per-bot list. Body forms retain the
  // avatar-colored core/weapon accent plus all three form materials; generic
  // far-proxy materials remain excluded so overview silhouettes stay readable.
  const statusMaterials = bodyForm
    ? [headMat, ...bodyFormMaterials]
    : [bodyMat, headMat];

  return {
    root,
    modelRoot,
    body: renderedBody,
    bodyMat: renderedBodyMat,
    head: renderedHead,
    headMat: renderedHeadMat,
    lArm: arms.left.arm,
    rArm: arms.right.arm,
    lShoulder: arms.left.arm,
    rShoulder: arms.right.arm,
    shadow,
    selector,
    weapon,
    hpContainer: hud.hpContainer,
    hpFill: hud.hpFill,
    nameLabel: hud.nameLabel,
    pickMeshes: presentationOnly ? [] : [selector, renderedBody, renderedHead, lowDetail].filter(Boolean),
    anim: new ForgeAnimState(profile.weapon),
    isForgeCharacter: true,
    isAlive: true,
    _wasAlive: true,
    _lastHp: -1,
    profile,
    joints,
    mounts,
    mountMetrics,
    basePose,
    weaponBase,
    weaponPoseNodes,
    weaponBases,
    _forgeMaterials: bodyForm
      ? [headMat, lowDetailMat, ...bodyFormMaterials].filter(Boolean)
      : [bodyMat, headMat, lowDetailMat].filter(Boolean),
    _forgeStatusMaterials: statusMaterials,
    _forgeMeshes: visibleMeshes,
    // A form-specific far proxy already communicates the complete character
    // silhouette. Hiding its weapon/core at crowd scale bounds submissions;
    // standard chassis keep the established distant weapon marker.
    _forgeFarMeshes: bodyForm ? [] : [...weapon._forgeMeshes],
    _visibleMeshCount: visibleMeshes.length,
    presentationOnly,
    bodyFormKey: bodyForm?.key || 'standard',
    lowDetail,
    _forgeFarLOD: false,
    setLOD(far = this._forgeFarLOD) {
      return setForgeCharacterLOD(this, far);
    },
  };
}

/** Dispose per-bot nodes/materials while leaving scene-owned templates intact. */
export function disposeForgeCharacter(entry) {
  if (!entry) return;
  if (entry.hpFill) entry.hpFill.dispose();
  if (entry.hpContainer) entry.hpContainer.dispose();
  if (entry.nameLabel) entry.nameLabel.dispose();
  if (entry.weapon) disposeForgeWeapon(entry.weapon);
  if (entry.selector && !entry.selector.isDisposed()) entry.selector.dispose();
  if (entry.shadow && !entry.shadow.isDisposed()) entry.shadow.dispose();
  if (entry.root && !entry.root.isDisposed()) entry.root.dispose();
  for (const material of entry._forgeMaterials || []) {
    if (material) material.dispose();
  }
}
