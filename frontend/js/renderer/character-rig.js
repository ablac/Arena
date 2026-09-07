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

import {beveledBox, profileHull} from './mech-geometry.js';
import {parseColor, makeMat} from './utils.js';
import {applyForgeSurface} from './forge-surfaces.js';
import {getCharacterProfile} from './character-roster.js?v=20260714e';
import {ForgeAnimState} from './character-anims.js?v=20260907r';
import {
  applyForgeLightingMode,
  createForgeWeapon,
  disposeForgeWeapon,
  setForgeWeaponLighting,
} from './forge-weapons.js?v=20260907r';
import {bodyFormForAsset} from './body-form-roster.js?v=20260714e';
import {buildBodyFormGeometry, createBodyFormFarProxy} from './body-form-geometry.js?v=20260714e';
import {createWorldBotHud, disposeWorldBotHud} from './world-hud.js?v=20260718o';
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
  Object.freeze({role: 'torso', shape: 'hull', position: [0, 0.57, 0], scaling: [0.56, 0.48, 0.32]}),
  Object.freeze({role: 'head', shape: 'helmet', position: [0, 0.88, 0], scaling: [0.30, 0.26, 0.29]}),
  Object.freeze({role: 'arm-left', shape: 'armor', position: [-0.39, 0.56, 0], scaling: [0.15, 0.43, 0.19]}),
  Object.freeze({role: 'arm-right', shape: 'armor', position: [0.39, 0.56, 0], scaling: [0.15, 0.43, 0.19]}),
  Object.freeze({role: 'leg-left', shape: 'armor', position: [-0.18, 0.185, 0], scaling: [0.19, 0.37, 0.22]}),
  Object.freeze({role: 'leg-right', shape: 'armor', position: [0.18, 0.185, 0], scaling: [0.19, 0.37, 0.22]}),
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
  applyForgeSurface(material, scene, name.includes('graphite') ? 'graphite' : 'gunmetal');
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
    const mesh = part.shape === 'hull'
      ? profileHull(`forge-low-${part.role}`, hullRings([
        [-0.5, 0.55, 0.72], [0.20, 1.0, 1.0], [0.5, 0.70, 0.78],
      ]), scene)
      : armorBlock(`forge-low-${part.role}`, scene, 1, 1, 1, part.shape === 'helmet' ? 0.25 : 0.14);
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

  const low = createFarSilhouetteTemplate(B, scene, farSilhouette);
  // Shape templates are scene-owned. Instances batch the fixed metal parts;
  // colored clones share geometry but retain each bot's two status materials.
  resources = {graphite, gunmetal, farSilhouette, selector, low, shapes: new Map()};
  _sceneResources.set(scene, resources);
  return resources;
}

function setTransform(node, position, scaling, rotation) {
  if (position) node.position.set(position[0], position[1], position[2]);
  if (scaling) node.scaling.set(scaling[0], scaling[1], scaling[2]);
  if (rotation) node.rotation.set(rotation[0], rotation[1], rotation[2]);
  return node;
}

function mergeParts(B, name, parts) {
  const mesh = B.Mesh.MergeMeshes(parts, true, true, undefined, false, false);
  if (!mesh) throw new Error(`Unable to assemble chassis part ${name}`);
  mesh.name = name;
  return mesh;
}

function metalPart(resources, key, name, parent, material, build, position, scaling, rotation) {
  let source = resources.shapes.get(key);
  if (!source) {
    source = build(`forge-template-${key}`);
    source.material = material === resources.graphite ? resources.graphite : resources.gunmetal;
    source.isPickable = false;
    source.setEnabled(false);
    resources.shapes.set(key, source);
  }
  const sharedMetal = material === resources.graphite || material === resources.gunmetal;
  const mesh = sharedMetal ? source.createInstance(name) : source.clone(name);
  mesh.parent = parent;
  if (!sharedMetal) mesh.material = material;
  mesh.isPickable = false;
  mesh.setEnabled(true);
  return setTransform(mesh, position, scaling, rotation);
}

function hullRings(rows) {
  return rows.map(([y, width, depth, z = 0]) => ({y, width, depth, z, bevel: Math.min(width, depth) * 0.18}));
}

function armorBlock(name, scene, width = 1, height = 1, depth = 1, bevel = 0.12) {
  return beveledBox(name, {width, height, depth, bevel}, scene);
}

// The socket and shaft are baked together once. The visible cylindrical hinge
// is centered exactly on the animated joint, with clearance before its armor.
function actuator(B, name, scene, width, length, depth) {
  const socket = B.MeshBuilder.CreateCylinder(`${name}-socket`, {
    height: width * 0.92, diameter: width * 0.85, tessellation: 12,
  }, scene);
  socket.rotation.z = Math.PI / 2;
  const shaft = profileHull(`${name}-shaft`, hullRings([
    [-0.94, 0.49, 0.54], [-0.78, 0.76, 0.72], [-0.22, 0.68, 0.68], [-0.12, 0.43, 0.46],
  ]), scene);
  shaft.scaling.set(width, length, depth);
  return mergeParts(B, name, [socket, shaft]);
}

function limbShell(B, name, scene, rows, width, length, depth) {
  const armor = profileHull(`${name}-armor`, hullRings(rows), scene);
  armor.scaling.set(width, length, depth);
  const hinge = B.MeshBuilder.CreateCylinder(`${name}-hinge`, {
    height: width * 0.84, diameter: width * 0.62, tessellation: 12,
  }, scene);
  hinge.rotation.z = Math.PI / 2;
  return mergeParts(B, name, [armor, hinge]);
}

function helmet(B, name, scene, style) {
  // Open face bay: opaque brow, side cheeks and jaw surround a physically
  // recessed sensor. The dark rear housing never covers that sensor plane.
  const back = profileHull(`${name}-shell`, hullRings([
    [-0.48, 0.54, 0.54, 0.15], [-0.22, 0.94, 0.72, 0.14],
    [0.28, 1.0, 0.75, 0.14], [0.50, style.helmetTop, 0.54, 0.16],
  ]), scene);
  const parts = [back];
  for (const side of [-1, 1]) {
    const cheek = armorBlock(`${name}-cheek`, scene, 0.20, 0.62, 0.36, 0.045);
    cheek.position.set(side * 0.38, -0.06, -0.30);
    cheek.rotation.z = side * -0.12;
    parts.push(cheek);
  }
  const brow = armorBlock(`${name}-brow`, scene, 0.90, 0.18, 0.34, 0.06);
  brow.position.set(0, 0.30, -0.29);
  const jaw = armorBlock(`${name}-jaw`, scene, 0.64, 0.18, 0.32, 0.055);
  jaw.position.set(0, -0.34, -0.27);
  parts.push(brow, jaw);
  return mergeParts(B, name, parts);
}

function chestArmor(B, name, scene, style) {
  const parts = [-1, 1].map(side => {
    const plate = profileHull(`${name}-pectoral`, hullRings([
      [-0.43, 0.23, 0.18, 0.04], [-0.12, 0.44, 0.28, -0.02],
      [0.30, 0.46, 0.23], [0.48, 0.30, 0.15, 0.05],
    ]), scene);
    plate.position.x = side * 0.255;
    plate.rotation.z = side * style.chestSweep;
    return plate;
  });
  return mergeParts(B, name, parts);
}

function shoulderArmor(B, name, scene) {
  const cap = profileHull(`${name}-cap`, hullRings([
    [-0.32, 0.88, 0.82], [0.05, 1, 1], [0.36, 0.76, 0.78], [0.48, 0.42, 0.54],
  ]), scene);
  const lower = armorBlock(`${name}-overlap`, scene, 0.80, 0.28, 0.86, 0.07);
  lower.position.set(0, -0.40, 0.02);
  return mergeParts(B, name, [cap, lower]);
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

// Seven different load-bearing silhouettes, not accessories on one box body.
// Torso rings run waist → rib cage → shoulder deck → neck. Dimensions remain
// in the roster so weapons, cosmetics and full-body replacements share metrics.
const CHASSIS_STYLE = Object.freeze({
  sword: {waist: 0.49, chest: 1.02, neck: 0.43, helmetTop: 0.62, headScaleY: 1,
    chestSweep: 0.12, shoulderL: [2.35, 2.0, 3.2, 0.12], shoulderR: [3.3, 2.75, 3.7, -0.16],
    gauntlet: 1.32, crest: 'keel', visor: [0.57, 0.12], feet: 1.05},
  bow: {waist: 0.48, chest: 0.88, neck: 0.38, helmetTop: 0.90, headScaleY: 0.84,
    chestSweep: -0.15, shoulderL: [1.9, 1.2, 3.4, 0.30], shoulderR: [1.6, 1.2, 2.6, -0.15],
    gauntlet: 1.02, crest: 'fins', visor: [0.64, 0.10], feet: 0.93},
  spear: {waist: 0.50, chest: 0.91, neck: 0.40, helmetTop: 0.36, headScaleY: 1.16,
    chestSweep: 0.25, shoulderL: [2.0, 3.1, 2.6, 0.06], shoulderR: [2.4, 3.7, 2.9, -0.10],
    gauntlet: 1.15, crest: 'lance', visor: [0.43, 0.10], feet: 1.08},
  daggers: {waist: 0.44, chest: 1.03, neck: 0.32, helmetTop: 0.52, headScaleY: 0.72,
    chestSweep: 0.36, shoulderL: [2.3, 1.15, 3.5, 0.52], shoulderR: [2.3, 1.15, 3.5, -0.52],
    gauntlet: 1.08, crest: 'blades', visor: [0.57, 0.09], feet: 1.0},
  staff: {waist: 0.38, chest: 0.74, neck: 0.35, helmetTop: 0.50, headScaleY: 1.23,
    chestSweep: -0.20, shoulderL: [1.65, 3.2, 2.3, -0.12], shoulderR: [1.65, 3.2, 2.3, 0.12],
    gauntlet: 1.12, crest: 'mantle', visor: [0.16, 0.36], feet: 0.96},
  shield: {waist: 0.75, chest: 1.08, neck: 0.70, helmetTop: 0.90, headScaleY: 0.66, headDrop: 1.2,
    chestSweep: 0.08, shoulderL: [4.0, 3.1, 4.3, 0.06], shoulderR: [3.6, 2.9, 4.0, -0.06],
    gauntlet: 1.68, crest: 'bastion', visor: [0.59, 0.11], feet: 1.28},
  grapple: {waist: 0.58, chest: 0.94, neck: 0.46, helmetTop: 0.72, headScaleY: 0.91,
    chestSweep: -0.08, shoulderL: [1.9, 1.7, 2.8, 0.20], shoulderR: [3.8, 2.6, 4.2, -0.27],
    gauntlet: 1.42, crest: 'winch', visor: [0.22, 0.24], feet: 1.13},
});

/**
 * Build one Forge combat chassis.
 *
 * @param {Object} bot trusted snapshot fields (bot_id, avatar_color, weapon, name)
 * @param {BABYLON.Scene} scene
 * @param {{presentationOnly?: boolean, shadowTemplate?: Object}} options
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
  const torsoWidth = 7.6 * p.shoulders;
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
  const armWidth = 1.64 * (0.82 + p.shoulders * 0.18);
  const hipX = Math.max(1.15, pelvisWidth * 0.34);
  const legWidth = 2.05 * (0.82 + p.hips * 0.18);
  const headWidth = 3.65 * p.head;
  const headHeight = 3.25 * p.head;
  const headDepth = 3.18 * p.head;
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
  const part = (key, name, parent, material, build, pos, scale, rot) =>
    metalPart(resources, key, `${name}-${id}`, parent, material, build, pos, scale, rot);
  const torso = part(`torso-${profile.weapon}`, 'forge-torso', bodyJoint, resources.graphite,
    name => profileHull(name, hullRings([
      [0, style.waist, 0.66], [0.18, style.waist * 1.12, 0.82],
      [0.64, style.chest, 1.0, -0.04], [0.86, style.chest * 0.96, 0.90],
      [1, style.neck, 0.61, 0.06],
    ]), scene), [0, 1.08, 0], [torsoWidth, torsoHeight, torsoDepth]);
  const chestPlate = part(`chest-${profile.weapon}`, 'forge-chest-plate', bodyJoint, bodyMat,
    name => chestArmor(B, name, scene, style),
    [0, 1.08 + torsoHeight * 0.59, -torsoDepth * 0.51],
    [torsoWidth * 0.90, torsoHeight * 0.72, torsoDepth * 0.70]);
  const hips = new B.TransformNode(`forge-hips-${id}`, scene);
  hips.parent = bodyJoint;
  const pelvis = part('pelvis', 'forge-pelvis', hips, resources.gunmetal,
    name => profileHull(name, hullRings([
      [-0.55, 0.66, 0.66], [-0.20, 1, 0.86], [0.28, 0.94, 0.82], [0.5, 0.64, 0.64],
    ]), scene), [0, 0.30, 0], [pelvisWidth, 2.1, torsoDepth]);

  const headJoint = new B.TransformNode(`forge-head-joint-${id}`, scene);
  headJoint.parent = bodyJoint;
  headJoint.position.y = headY;
  const headDrop = style.headDrop || 0;
  const head = part(`head-${profile.weapon}`, 'forge-head', headJoint, resources.graphite,
    name => helmet(B, name, scene, style), [0, -headDrop, 0],
    [headWidth, headHeight * style.headScaleY, headDepth]);
  const visor = part('sensor', 'forge-visor', headJoint, headMat,
    name => armorBlock(name, scene, 1, 1, 1, 0.1),
    [profile.weapon === 'grapple' ? headWidth * 0.12 : 0, -headDrop, -headDepth * 0.30],
    [headWidth * style.visor[0], headHeight * style.visor[1], 0.16]);
  const flairMeshes = [];
  const neck = part('neck-bearing', 'forge-neck', bodyJoint, resources.gunmetal,
    name => B.MeshBuilder.CreateCylinder(name, {height: 1, diameter: 1, tessellation: 12}, scene),
    [0, headY - headHeight * 0.54 - headDrop * 0.25, 0],
    [headWidth * 0.38, headHeight * 0.52, headWidth * 0.38]);
  flairMeshes.push(neck);

  // Signature structures are structural, solid profiles. They share geometry
  // per class, and remain separate from all replaceable cosmetic mount slots.
  const signature = (key, parent, material, rows, pos, scale, rot) => {
    const mesh = part(`signature-${profile.weapon}-${key}`, `forge-${key}`, parent, material,
      name => profileHull(name, hullRings(rows), scene), pos, scale, rot);
    flairMeshes.push(mesh);
    return mesh;
  };
  if (style.crest === 'keel' || style.crest === 'lance') {
    signature('helmet-keel', headJoint, resources.gunmetal,
      [[0, 0.18, 0.72], [0.45, 0.16, 0.50, 0.10], [1, 0.06, 0.17, 0.32]],
      [0, headHeight * style.headScaleY * 0.40, 0.05],
      [headWidth, style.crest === 'lance' ? 3.4 : 2.2, headDepth]);
  } else if (style.crest === 'winch') {
    const spool = part('winch-spool', 'forge-winch', bodyJoint, resources.gunmetal, name => {
      const drum = B.MeshBuilder.CreateCylinder(`${name}-drum`, {height: 2.2, diameter: 2.0, tessellation: 16}, scene);
      drum.rotation.z = Math.PI / 2;
      const ends = [-1, 1].map(side => {
        const end = B.MeshBuilder.CreateCylinder(`${name}-end`, {height: 0.22, diameter: 2.8, tessellation: 12}, scene);
        end.rotation.z = Math.PI / 2;
        end.position.x = side * 1.15;
        return end;
      });
      return mergeParts(B, name, [drum, ...ends]);
    }, [0.7, torsoHeight * 0.74, torsoDepth * 0.72]);
    flairMeshes.push(spool);
  } else {
    for (const side of [-1, 1]) {
      const tall = style.crest === 'mantle';
      const heavy = style.crest === 'bastion';
      signature(`dorsal-${side < 0 ? 'left' : 'right'}`, bodyJoint,
        heavy ? resources.gunmetal : resources.graphite,
        [[0, 0.42, 0.65], [0.25, 0.72, 0.84], [0.78, 0.52, 0.64, 0.08], [1, 0.16, 0.28, 0.22]],
        [side * torsoWidth * (tall ? 0.37 : 0.42), torsoHeight * (tall ? 0.50 : 0.48), torsoDepth * 0.47],
        [heavy ? 2.5 : 1.3, tall ? torsoHeight * 1.06 : heavy ? 5.3 : 4.0, 2.0],
        [style.crest === 'blades' ? 0.38 : 0.14, 0, side * (tall ? -0.12 : -0.30)]);
    }
  }

  const headTop = new B.TransformNode(`forge-head-top-${id}`, scene);
  headTop.parent = headJoint;
  headTop.position.y = headHeight * style.headScaleY * 0.5 - headDrop
    + (style.crest === 'lance' ? 3.3 : style.crest === 'keel' ? 2.1 : 0.3);

  const core = B.MeshBuilder.CreateCylinder(`forge-core-mesh-${id}`, {
    height: 0.26, diameter: Math.max(1.35, torsoWidth * 0.19), tessellation: 16,
  }, scene);
  core.parent = bodyJoint;
  core.position.set(0, 1.15 + torsoHeight * 0.53, -torsoDepth * 0.59);
  core.rotation.x = Math.PI / 2;
  core.material = headMat;
  core.isPickable = false;

  const limbMeshes = [];
  const arms = {};
  for (const side of [-1, 1]) {
    const label = side < 0 ? 'left' : 'right';
    const arm = new B.TransformNode(`forge-${label}-arm-${id}`, scene);
    arm.parent = bodyJoint;
    arm.position.set(side * shoulderX, shoulderY, 0);
    const upper = part(`arm-actuator-${profile.weapon}`, `forge-${label}-upper-arm`, arm, resources.graphite,
      name => actuator(B, name, scene, armWidth, upperArmLength, armWidth));
    const elbow = new B.TransformNode(`forge-${label}-elbow-${id}`, scene);
    elbow.parent = arm;
    elbow.position.y = -upperArmLength;
    const gauntlet = armWidth * style.gauntlet;
    const forearm = part(`forearm-shell-${profile.weapon}`, `forge-${label}-forearm`, elbow, resources.gunmetal,
      name => limbShell(B, name, scene, [
        [-0.96, 0.65, 0.70], [-0.70, 0.98, 0.97, -0.04], [-0.27, 0.91, 0.85], [-0.10, 0.53, 0.54],
      ], gauntlet, forearmLength, gauntlet));
    const hand = new B.TransformNode(`forge-${label}-hand-${id}`, scene);
    hand.parent = elbow;
    hand.position.y = -forearmLength;
    const fist = part('fist', `forge-${label}-fist`, hand, resources.graphite,
      name => armorBlock(name, scene, 1, 1, 1, 0.16), [0, -0.16, -0.08], [armWidth * 0.8, 0.95, armWidth * 0.92]);
    const armor = side < 0 ? style.shoulderL : style.shoulderR;
    const pauldron = part('pauldron', `forge-${label}-pauldron`, arm, bodyMat,
      name => shoulderArmor(B, name, scene), [side * 0.30, 0.40, 0], armor.slice(0, 3), [0, 0, armor[3]]);
    limbMeshes.push(upper, forearm, fist, pauldron);
    arms[label] = {arm, elbow, hand};
  }

  const legs = {};
  for (const side of [-1, 1]) {
    const label = side < 0 ? 'left' : 'right';
    const leg = new B.TransformNode(`forge-${label}-leg-${id}`, scene);
    leg.parent = hips;
    leg.position.x = side * hipX;
    const upper = part(`leg-actuator-${profile.weapon}`, `forge-${label}-upper-leg`, leg, resources.graphite,
      name => actuator(B, name, scene, legWidth, upperLegLength, legWidth * 1.12));
    const knee = new B.TransformNode(`forge-${label}-knee-${id}`, scene);
    knee.parent = leg;
    knee.position.y = -upperLegLength;
    const shin = part(`shin-shell-${profile.weapon}`, `forge-${label}-shin`, knee, resources.gunmetal,
      name => limbShell(B, name, scene, [
        [-0.94, 0.62, 0.62, 0.05], [-0.74, 0.73, 0.78], [-0.23, 1.03, 1.03, -0.05], [-0.07, 0.71, 0.73],
      ], legWidth, shinLength, legWidth));
    const foot = new B.TransformNode(`forge-${label}-ankle-${id}`, scene);
    foot.parent = knee;
    foot.position.y = -shinLength;
    const boot = part('foot', `forge-${label}-foot`, foot, resources.graphite,
      name => profileHull(name, hullRings([
        [-0.41, 0.92, 0.96, -0.18], [-0.22, 1.0, 1.0, -0.18],
        [0.22, 0.84, 0.83, -0.12], [0.62, 0.58, 0.48, 0.03],
      ]), scene), [0, 0.04, -0.42], [legWidth * 1.34 * style.feet, 1, 3.7 * style.feet]);
    limbMeshes.push(upper, shin, boot);
    legs[label] = {leg, knee, foot};
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

  const hud = presentationOnly ? null : createWorldBotHud(bot, id, root, scene);
  const joints = {
    body: bodyJoint,
    hips,
    torso,
    head: headJoint,
    leftArm: arms.left.arm,
    leftElbow: arms.left.elbow,
    rightArm: arms.right.arm,
    rightElbow: arms.right.elbow,
    leftLeg: legs.left.leg,
    leftKnee: legs.left.knee,
    leftFoot: legs.left.foot,
    rightLeg: legs.right.leg,
    rightKnee: legs.right.knee,
    rightFoot: legs.right.foot,
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
    hipYaw: 0,
    footLPitch: 0,
    footRPitch: 0,
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
    worldHud: hud,
    hpContainer: hud?.hpContainer || null,
    hpFill: hud?.hpFill || null,
    nameLabel: hud?.nameLabel || null,
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
  if (entry.worldHud) disposeWorldBotHud(entry.worldHud);
  if (entry.weapon) disposeForgeWeapon(entry.weapon);
  if (entry.selector && !entry.selector.isDisposed()) entry.selector.dispose();
  if (entry.shadow && !entry.shadow.isDisposed()) entry.shadow.dispose();
  if (entry.root && !entry.root.isDisposed()) entry.root.dispose();
  for (const material of entry._forgeMaterials || []) {
    if (material) material.dispose();
  }
}
