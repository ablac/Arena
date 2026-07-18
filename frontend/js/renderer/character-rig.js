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
import {getCharacterProfile} from './character-roster.js?v=20260714e';
import {ForgeAnimState} from './character-anims.js?v=20260714e';
import {
  applyForgeLightingMode,
  createForgeWeapon,
  disposeForgeWeapon,
  setForgeWeaponLighting,
} from './forge-weapons.js?v=20260718c';
import {bodyFormForAsset} from './body-form-roster.js?v=20260714e';
import {buildBodyFormGeometry, createBodyFormFarProxy} from './body-form-geometry.js?v=20260714e';
import {isEnabled} from '../settings.js';

const _sceneResources = new WeakMap();

/** Lit-mode emissive floor (fraction of diffuse) — see forge-weapons.js. */
const LIT_EMISSIVE_FLOOR = 0.45;

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
  // Far-LOD silhouettes and the pick selector stay deliberately unlit: the
  // proxy's whole job is a guaranteed-readable identity blob at overview
  // zoom, so it must not depend on a light reaching it.
  material.disableLighting = true;
  material.freeze();
  return material;
}

/**
 * Structural chassis material (issue #181): sun/hemi-lit with an emissive
 * floor so team silhouettes keep a readable minimum in the arena's near-black
 * sectors while directional shading adds depth. The pre-lighting flat
 * emissive stays on the material so rendering.characterLighting can flip the
 * legacy self-lit look back live (setForgeChassisLighting below); accent/core
 * materials are per-bot, already lit, and unaffected.
 */
function litChassisMaterial(scene, name, diffuse, unlitEmissive, specular) {
  const B = window.BABYLON;
  const material = new B.StandardMaterial(name, scene);
  material.diffuseColor = diffuse;
  material.specularColor = specular;
  material.backFaceCulling = true;
  material.emissiveColor = unlitEmissive.clone();
  material._forgeLitEmissive = new B.Color3(
    diffuse.r * LIT_EMISSIVE_FLOOR,
    diffuse.g * LIT_EMISSIVE_FLOOR,
    diffuse.b * LIT_EMISSIVE_FLOOR,
  );
  material._forgeUnlitEmissive = unlitEmissive;
  applyForgeLightingMode(material, isEnabled('rendering', 'characterLighting'));
  material.freeze();
  return material;
}

/**
 * Flip this scene's shared chassis + weapon materials between lit-with-floor
 * and the legacy self-lit look. Called from the per-frame settings check in
 * bots.js when rendering.characterLighting changes; a no-op until the scene
 * has built its shared resources (creation reads the setting directly).
 */
export function setForgeChassisLighting(scene, lit) {
  const resources = _sceneResources.get(scene);
  if (resources) {
    for (const material of [resources.graphite, resources.gunmetal]) {
      material.unfreeze();
      applyForgeLightingMode(material, lit);
      material.freeze();
    }
  }
  setForgeWeaponLighting(scene, lit);
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
  const graphite = litChassisMaterial(
    scene,
    'forge-graphite-shared',
    new B.Color3(0.28, 0.36, 0.50),
    new B.Color3(0.18, 0.24, 0.36),
    new B.Color3(0.40, 0.46, 0.54),
  );
  const gunmetal = litChassisMaterial(
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

  const dome = B.MeshBuilder.CreateSphere('forge-dome-template', {diameter: 1, segments: 8}, scene);
  dome.material = graphite;
  dome.isPickable = false;
  dome.setEnabled(false);

  const ring = B.MeshBuilder.CreateTorus('forge-ring-template', {
    diameter: 1, thickness: 0.15, tessellation: 12,
  }, scene);
  ring.material = gunmetal;
  ring.isPickable = false;
  ring.setEnabled(false);

  // One merged humanoid silhouette replaces the articulated body on distant
  // live bots. Clones share this geometry and add one identity material each.
  const low = createFarSilhouetteTemplate(B, scene, farSilhouette);

  resources = {graphite, gunmetal, farSilhouette, selector, box, head, plate, dome, ring, low};
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

function templateInstance(template, name, parent, position, scaling, rotation) {
  const mesh = template.createInstance(name);
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

// Per-class chassis styling consumed once at build time: head/visor/plate
// variants plus one signature flair mesh for classes whose weapon leaves
// mesh-budget headroom (the daggers and grapple weapons already spend four
// meshes, so those two classes style existing parts only).
const CHASSIS_STYLE = Object.freeze({
  sword: Object.freeze({
    // Duelist: hex face forward, tall head crest, banded visor.
    headYaw: Math.PI / 6, gauntlet: 1.18, flair: 'crest', crownY: 1.15,
    visor: Object.freeze({w: 0.70, h: 0.58, y: 0.18}),
    chest: Object.freeze({w: 0.78, h: 0.44, y: 0.60, pitch: -0.12}),
  }),
  bow: Object.freeze({
    // Scout: hooded dome over a wide visor, light plating.
    gauntlet: 1.04, flair: 'hood', crownY: 1.05,
    visor: Object.freeze({w: 0.84, h: 0.44, y: 0.16}),
    chest: Object.freeze({w: 0.56, h: 0.30, y: 0.62, pitch: 0.04}),
  }),
  spear: Object.freeze({
    // Lancer: swept-back crest, steeply raked chest wedge.
    headYaw: Math.PI / 6, gauntlet: 1.14, flair: 'sweptCrest', crownY: 1.2,
    visor: Object.freeze({w: 0.64, h: 0.50, y: 0.20}),
    chest: Object.freeze({w: 0.66, h: 0.42, y: 0.62, pitch: -0.24}),
  }),
  daggers: Object.freeze({
    // Skirmisher: small head, slit visor, diagonal bandolier plate.
    headScale: 0.88, gauntlet: 0.94, crownY: 0.5,
    visor: Object.freeze({w: 0.56, h: 0.28, y: 0.26}),
    chest: Object.freeze({w: 0.46, h: 0.60, y: 0.52, pitch: -0.04, roll: 0.45}),
  }),
  staff: Object.freeze({
    // Arcanist: tall visor, robe skirt, floating halo ring.
    gauntlet: 1.0, flair: 'halo', skirt: true, crownY: 0.75,
    visor: Object.freeze({w: 0.60, h: 0.72, y: 0.10}),
    chest: Object.freeze({w: 0.48, h: 0.64, y: 0.56, pitch: 0}),
  }),
  shield: Object.freeze({
    // Bulwark: neckless head sunk between the shoulders, sloped plating
    // front and back, heavy gauntlets.
    headScaleY: 0.78, headDrop: 1.15, gauntlet: 1.30, flair: 'backplate', crownY: 0.08,
    visor: Object.freeze({w: 0.58, h: 0.32, y: 0.24}),
    chest: Object.freeze({w: 0.92, h: 0.52, y: 0.55, pitch: -0.30}),
  }),
  grapple: Object.freeze({
    // Rigger: off-center mono-optic, chest plate relocated to a back winch.
    gauntlet: 1.24, crownY: 0.6,
    visor: Object.freeze({w: 0.30, h: 0.44, y: 0.20, x: 0.24}),
    chest: Object.freeze({back: true, w: 0.62, h: 0.50}),
  }),
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

  const style = CHASSIS_STYLE[profile.weapon] || CHASSIS_STYLE.sword;
  const torso = boxInstance(resources, `forge-torso-${id}`, bodyJoint,
    [0, 1.08 + torsoHeight / 2, 0], [torsoWidth, torsoHeight, torsoDepth]);
  // Chest plating angles per class; the Rigger carries its plate behind the
  // shoulders as a winch block instead of on the chest.
  const chestPlate = plateInstance(resources, `forge-chest-plate-${id}`, bodyJoint,
    style.chest.back
      ? [0, 1.12 + torsoHeight * 0.55, torsoDepth * 0.66]
      : [0, 1.12 + torsoHeight * (style.chest.y ?? 0.6), -torsoDepth * 0.54],
    [torsoWidth * style.chest.w, torsoHeight * style.chest.h, style.chest.back ? 1.5 : 0.48],
    [style.chest.pitch || 0, 0, style.chest.roll || 0]);
  const pelvis = accentBox(B, `forge-pelvis-${id}`, scene, bodyJoint, bodyMat,
    [0, style.skirt ? 0.1 : 0.45, 0],
    style.skirt
      ? [pelvisWidth * 1.2, 3.6, torsoDepth * 0.95]
      : [pelvisWidth, 2.0, torsoDepth * 0.88]);

  const headJoint = new B.TransformNode(`forge-head-joint-${id}`, scene);
  headJoint.parent = bodyJoint;
  headJoint.position.y = headY;
  const headDrop = style.headDrop || 0;
  const head = headInstance(resources, `forge-head-${id}`, headJoint, [
    headWidth * (style.headScale || 1),
    headHeight * (style.headScaleY ?? style.headScale ?? 1),
    headDepth * (style.headScale || 1),
  ]);
  if (style.headYaw) head.rotation.y = style.headYaw;
  if (headDrop) head.position.y = -headDrop;
  const visor = accentBox(B, `forge-visor-${id}`, scene, headJoint, headMat,
    [(style.visor.x || 0) * headWidth, style.visor.y - headDrop, -headDepth * 0.51],
    [headWidth * style.visor.w, style.visor.h, 0.30]);

  // One signature flair per class where the weapon leaves mesh-budget room.
  const flairMeshes = [];
  switch (style.flair) {
    case 'crest':
      flairMeshes.push(accentBox(B, `forge-crest-${id}`, scene, headJoint, headMat,
        [0, headHeight * 0.62, 0.1], [0.26, headHeight * 0.72, headDepth * 0.95], [0.06, 0, 0]));
      break;
    case 'sweptCrest':
      flairMeshes.push(accentBox(B, `forge-crest-${id}`, scene, headJoint, headMat,
        [0, headHeight * 0.58, 0.55], [0.26, headHeight * 0.95, headDepth * 0.70], [0.55, 0, 0]));
      break;
    case 'hood':
      flairMeshes.push(templateInstance(resources.dome, `forge-hood-${id}`, headJoint,
        [0, 0.42, 0.55], [headWidth * 1.24, headHeight * 1.02, headDepth * 1.18]));
      break;
    case 'halo':
      flairMeshes.push(templateInstance(resources.ring, `forge-halo-${id}`, headJoint,
        [0, headHeight * 0.42, headDepth * 0.72],
        [headWidth * 1.55, headWidth * 1.55, headWidth * 1.55],
        [Math.PI / 2 - 0.15, 0, 0]));
      break;
    case 'backplate':
      flairMeshes.push(plateInstance(resources, `forge-back-slab-${id}`, bodyJoint,
        [0, 1.12 + torsoHeight * 0.52, torsoDepth * 0.60],
        [torsoWidth * 0.95, torsoHeight * 0.62, 0.55], [0.10, 0, 0]));
      break;
    default:
      break;
  }

  // Cosmetic attachment anchor: where head-mounted cosmetics (halos, crowns,
  // antennas) sit for THIS silhouette. Body-form builders reposition it so a
  // halo hovers over a rabbit's ears, a wizard's hat, or a slime's crown
  // rather than at the humanoid default.
  const headTop = new B.TransformNode(`forge-head-top-${id}`, scene);
  headTop.parent = headJoint;
  headTop.position.y = headHeight * (style.crownY ?? 0.55);

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
    // Gauntlet forearms: wider than the upper arm per class weight so the
    // limb tapers outward instead of reading as two equal sticks.
    const gauntlet = armWidth * (style.gauntlet || 1);
    const forearm = boxInstance(resources, `forge-${label}-forearm-${id}`, elbow,
      [0, -forearmLength / 2, -0.08], [gauntlet, forearmLength, gauntlet]);
    const hand = new B.TransformNode(`forge-${label}-hand-${id}`, scene);
    hand.parent = elbow;
    hand.position.y = -forearmLength;

    const armor = armorStyle[label];
    const pauldron = accentBox(B, `forge-${label}-pauldron-${id}`, scene, arm, bodyMat,
      [side * 0.18, 0.14, 0], [armor[0], armor[1], armor[2]], [0, 0, side * armor[3]]);
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
      [0, -shinLength + 0.05, -0.75], [legWidth * 1.12, 0.82, 2.95]);
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
    headTop,
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
  // The roster stance is authored in character semantics (forward-positive);
  // convert it once here into rig-space joint offsets. Torso/head content
  // sits above its joint (forward = negative rig pitch) while limbs hang
  // below theirs (forward = positive); knees are negated at application.
  const stance = profile.stance || {};
  const basePose = {
    bodyY: bodyY - (stance.crouch || 0),
    bodyYaw: stance.bodyYaw || 0,
    headPitch: -(stance.headPitch || 0),
    armLPitch: stance.armL || 0,
    armRPitch: stance.armR || 0,
    armLRoll: stance.armLRoll ?? (profile.weapon === 'bow' ? -0.08 : 0.05),
    armRRoll: stance.armRRoll ?? (profile.weapon === 'shield' ? 0.10 : -0.05),
    elbowLPitch: stance.elbowL ?? 0.10,
    elbowRPitch: stance.elbowR ?? 0.10,
    kneePitch: (stance.knee || 0) + 0.05 + profile.motion.weight * 0.04,
  };

  let renderedBody = torso;
  let renderedHead = head;
  let formShoulderY = null;
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
    // Snap the cosmetic anchors onto the form's actual silhouette so every
    // attachment slot fits every skin.
    const anchors = geometry.anchors || {};
    if (Number.isFinite(anchors.headTopY)) headTop.position.y = anchors.headTopY;
    if (Array.isArray(anchors.backPos)) backMount.position.set(...anchors.backPos);
    if (Number.isFinite(anchors.shoulderY)) formShoulderY = anchors.shoulderY;
    // The skeleton, semantic mounts, Arena core, and weapon stay shared, but
    // the robot shell itself is removed so a full-body skin never overlays or
    // reveals an invisible second character.
    for (const mesh of [torso, chestPlate, pelvis, head, visor, ...flairMeshes, ...limbMeshes]) mesh.dispose();
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
    : [torso, chestPlate, pelvis, head, visor, core, ...flairMeshes, ...limbMeshes, ...weapon._forgeMeshes];
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
    anim: Object.assign(new ForgeAnimState(profile.weapon), {
      formMotion: bodyForm?.motion || null,
    }),
    isForgeCharacter: true,
    isAlive: true,
    _wasAlive: true,
    _lastHp: -1,
    profile,
    joints,
    mounts,
    mountMetrics,
    cosmeticAnchors: Object.freeze({
      shoulderY: Number.isFinite(formShoulderY) ? formShoulderY : -torsoHeight * 0.08,
    }),
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
