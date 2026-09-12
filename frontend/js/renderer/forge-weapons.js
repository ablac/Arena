'use strict';

/**
 * Mechanically articulated weapons for the shared Forge character rig.
 *
 * Weapon materials are owned by the scene and reused by every bot. The only
 * per-bot material used here is the caller's mutable accent material, keeping
 * status flashes and cosmetic finish swaps compatible with BotRenderer.
 * @module renderer/forge-weapons
 */

import { isEnabled } from '../settings.js';
import {applyForgeSurface, syncForgeSurface} from './forge-surfaces.js?v=20260907p';
import {beveledBox, profileHull} from './mech-geometry.js';

const _sceneResources = new WeakMap();

/**
 * Lit-mode emissive floor as a fraction of the material's diffuse. Weapon
 * silhouettes keep the same dark-sector visibility guarantee as the shared
 * chassis: even a face no light reaches renders at floor brightness, while
 * sun/hemi shading models the lit faces on top of it.
 */
const LIT_EMISSIVE_FLOOR = 0.12;

/**
 * Apply lit or legacy self-lit mode to a shared Forge material. Both emissive
 * variants ride on the material so the rendering.characterLighting toggle can
 * flip live without rebuilding any rig. Callers own unfreeze()/freeze().
 */
export function applyForgeLightingMode(material, lit) {
  material.disableLighting = !lit;
  if (material._forgeLitDiffuse) {
    material.diffuseColor.copyFrom(lit ? material._forgeLitDiffuse : material._forgeUnlitDiffuse);
  }
  syncForgeSurface(material);
  material.emissiveColor.copyFrom(
    lit ? material._forgeLitEmissive : material._forgeUnlitEmissive);
}

function sharedMaterial(scene, name, diffuse, unlitEmissive, specular) {
  const B = window.BABYLON;
  const material = new B.StandardMaterial(name, scene);
  material.diffuseColor = diffuse.clone();
  material._forgeUnlitDiffuse = diffuse;
  material._forgeLitDiffuse = name.endsWith('steel') ? new B.Color3(0.56, 0.59, 0.62)
    : name.endsWith('cable') ? new B.Color3(0.09, 0.12, 0.15) : new B.Color3(0.20, 0.25, 0.30);
  const litDiffuse = material._forgeLitDiffuse;
  material.specularColor = specular;
  material.backFaceCulling = true;
  // Sun/hemi-lit with an emissive floor (issue #181): the floor keeps the
  // silhouette readable in the arena's near-black sectors, while directional
  // shading adds depth. The pre-lighting flat emissive stays on the material
  // so the characterLighting toggle can restore the legacy self-lit look.
  material.emissiveColor = unlitEmissive.clone();
  material._forgeLitEmissive = new B.Color3(
    litDiffuse.r * LIT_EMISSIVE_FLOOR,
    litDiffuse.g * LIT_EMISSIVE_FLOOR,
    litDiffuse.b * LIT_EMISSIVE_FLOOR,
  );
  material._forgeUnlitEmissive = unlitEmissive;
  applyForgeSurface(material, scene, name.endsWith('steel') ? 'steel'
    : name.endsWith('cable') ? 'graphite' : 'gunmetal');
  applyForgeLightingMode(material, isEnabled('rendering', 'characterLighting'));
  material.freeze();
  return material;
}

/** Flip this scene's shared weapon materials between lit and self-lit. */
export function setForgeWeaponLighting(scene, lit) {
  const resources = _sceneResources.get(scene);
  if (!resources) return;
  for (const material of [resources.steel, resources.dark, resources.cable]) {
    material.unfreeze();
    applyForgeLightingMode(material, lit);
    material.freeze();
  }
}

function getResources(scene) {
  let resources = _sceneResources.get(scene);
  if (resources) return resources;

  const B = window.BABYLON;
  resources = {
    steel: sharedMaterial(
      scene,
      'forge-weapon-steel',
      new B.Color3(0.42, 0.52, 0.68),
      new B.Color3(0.20, 0.27, 0.38),
      new B.Color3(0.62, 0.68, 0.76),
    ),
    dark: sharedMaterial(
      scene,
      'forge-weapon-dark',
      new B.Color3(0.25, 0.33, 0.48),
      new B.Color3(0.16, 0.22, 0.33),
      new B.Color3(0.22, 0.26, 0.32),
    ),
    cable: sharedMaterial(
      scene,
      'forge-weapon-cable',
      new B.Color3(0.28, 0.38, 0.56),
      new B.Color3(0.18, 0.25, 0.38),
      new B.Color3(0.16, 0.20, 0.24),
    ),
  };
  _sceneResources.set(scene, resources);
  return resources;
}

function finish(mesh, parent, material) {
  mesh.parent = parent;
  mesh.material = material;
  mesh.isPickable = false;
  return mesh;
}

function tipAnchor(root, name, position) {
  const B = window.BABYLON;
  const tip = new B.TransformNode(name, root.getScene());
  tip.parent = root;
  tip.position.copyFrom(position);
  root._trailTips.push(tip);
  return tip;
}

// All dimensions are local to the existing grip and trail-tip contracts. Hulls
// carry the silhouette; small fittings only explain how each tool is assembled.
function hull(name, rings, parent, material, scene) {
  return finish(profileHull(name, rings, scene), parent, material);
}

function block(name, width, height, depth, parent, material, scene, bevel = 0.12) {
  return finish(beveledBox(name, {width, height, depth, bevel}, scene), parent, material);
}

function cylinder(name, height, diameter, parent, material, scene, top = diameter) {
  return finish(window.BABYLON.MeshBuilder.CreateCylinder(name, {
    height, diameterBottom: diameter, diameterTop: top, tessellation: 10,
  }, scene), parent, material);
}

function tube(name, points, radius, parent, material, scene) {
  const B = window.BABYLON;
  return finish(B.MeshBuilder.CreateTube(name, {
    path: points.map(p => new B.Vector3(...p)), radius, tessellation: 6, cap: B.Mesh.CAP_ALL,
  }, scene), parent, material);
}

function buildSword(root, id, scene, materials, accentMaterial) {
  const B = window.BABYLON;
  // Broad ricasso, straight cutting planes and a long spear point. The shallow
  // bevel leaves a central ridge instead of the old four-sided conical blade.
  const blade = hull(`forge-sword-blade-${id}`, [
    {y: 0.55, width: 1.12, depth: 0.38, bevel: 0.17},
    {y: 2.0, width: 1.62, depth: 0.38, bevel: 0.18},
    {y: 8.8, width: 1.28, depth: 0.28, bevel: 0.13},
    {y: 10.35, width: 0.90, depth: 0.20, bevel: 0.09},
    {y: 12.05, width: 0.025, depth: 0.025, bevel: 0.01},
  ], root, materials.steel, scene);
  const fuller = hull(`forge-sword-fuller-${id}`, [
    {y: 1.8, width: 0.15, depth: 0.04},
    {y: 8.8, width: 0.13, depth: 0.04},
    {y: 10.1, width: 0.02, depth: 0.02},
  ], root, materials.dark, scene);
  fuller.position.z = -0.185;
  const guard = hull(`forge-sword-guard-${id}`, [
    {y: -0.03, width: 1.25, depth: 0.92, bevel: 0.2},
    {y: 0.18, width: 4.0, depth: 0.72, bevel: 0.2},
    {y: 0.65, width: 3.3, depth: 0.58, bevel: 0.18},
  ], root, accentMaterial, scene);
  const grip = cylinder(`forge-sword-grip-${id}`, 2.75, 0.66, root, materials.dark, scene);
  grip.position.y = -1.45;
  const pommel = hull(`forge-sword-pommel-${id}`, [
    {y: -3.30, width: 0.48, depth: 0.5},
    {y: -3.0, width: 1.06, depth: 0.76, bevel: 0.2},
    {y: -2.65, width: 0.66, depth: 0.66, bevel: 0.18},
  ], root, materials.steel, scene);
  const meshes = [blade, fuller, guard, grip, pommel];
  for (let i = 0; i < 4; i += 1) {
    const band = cylinder(`forge-sword-wrap-${id}-${i}`, 0.10, 0.73, root, materials.cable, scene);
    band.position.y = -0.6 - i * 0.56;
    meshes.push(band);
  }
  tipAnchor(root, `forge-sword-tip-${id}`, new B.Vector3(0, 12.05, 0));
  return meshes;
}

function buildBow(root, id, scene, materials, accentMaterial) {
  const B = window.BABYLON;
  const meshes = [];
  const riser = hull(`forge-bow-riser-${id}`, [
    {y: -2.25, width: 0.7, depth: 0.9, z: -0.5},
    {y: -1.05, width: 0.85, depth: 1.1, z: -0.2},
    {y: 0.6, width: 0.62, depth: 0.8},
    {y: 2.25, width: 0.7, depth: 0.9, z: -0.5},
  ], root, materials.dark, scene);
  riser.position.x = -0.7;
  meshes.push(riser);
  // Laminated recurve limbs bend toward the shooter at their tips. Both
  // strings terminate on those tips and meet the arrow nock at the same point.
  for (const side of [-1, 1]) {
    const limb = hull(`forge-bow-limb-${id}-${side}`, [
      {y: 0, width: 0.74, depth: 0.48, z: -0.5},
      {y: 2.3, width: 0.62, depth: 0.36, z: -0.1},
      {y: 4.65, width: 0.44, depth: 0.28, z: 1.4},
      {y: 6.15, width: 0.30, depth: 0.25, z: 1.7},
    ], root, accentMaterial, scene);
    limb.position.set(-0.7, side * 2.25, 0);
    // A rotation mirrors the lower limb without reversing mesh winding.
    if (side < 0) limb.rotation.z = Math.PI;
    const cam = cylinder(`forge-bow-cam-${id}-${side}`, 0.58, 1.12, root, materials.steel, scene);
    cam.rotation.z = Math.PI / 2;
    cam.position.set(-0.7, side * 8.1, 1.7);
    const tendon = tube(`forge-bow-string-${id}-${side}`, [
      [-0.7, side * 8.4, 1.7], [-0.7, 0, 2.85],
    ], 0.065, root, materials.cable, scene);
    meshes.push(limb, cam, tendon);
  }
  const arrow = cylinder(`forge-bow-arrow-${id}`, 9.0, 0.18, root, materials.steel, scene);
  arrow.rotation.x = -Math.PI / 2;
  arrow.position.set(-0.7, 0, -1.65);
  const head = hull(`forge-bow-arrowhead-${id}`, [
    {y: 0, width: 0.62, depth: 0.18, bevel: 0.08},
    {y: 1.5, width: 0.02, depth: 0.02},
  ], root, materials.steel, scene);
  head.rotation.x = -Math.PI / 2;
  head.position.set(-0.7, 0, -6.15);
  const vane = block(`forge-bow-fletching-${id}`, 0.66, 0.08, 0.85, root, accentMaterial, scene, 0.03);
  vane.position.set(-0.7, 0, 2.10);
  meshes.push(arrow, head, vane);
  tipAnchor(root, `forge-bow-tip-${id}`, new B.Vector3(-0.7, 0, -7.65));
  return meshes;
}

function buildSpear(root, id, scene, materials, accentMaterial) {
  const B = window.BABYLON;
  const shaft = cylinder(`forge-spear-shaft-${id}`, 19.5, 0.57, root, materials.dark, scene);
  shaft.position.y = 5.5;
  const tip = hull(`forge-spear-tip-${id}`, [
    {y: 15.1, width: 0.56, depth: 0.5, bevel: 0.2},
    {y: 16.3, width: 1.65, depth: 0.40, bevel: 0.19},
    {y: 17.6, width: 1.20, depth: 0.30, bevel: 0.14},
    {y: 20.05, width: 0.025, depth: 0.025, bevel: 0.01},
  ], root, materials.steel, scene);
  const collar = cylinder(`forge-spear-collar-${id}`, 1.2, 0.95, root, accentMaterial, scene, 0.72);
  collar.position.y = 14.75;
  const heel = cylinder(`forge-spear-heel-${id}`, 0.7, 0.75, root, materials.steel, scene, 0.56);
  heel.position.y = -4.05;
  const meshes = [shaft, tip, collar, heel];
  for (const y of [-1.6, 2.5, 8.9, 12.9]) {
    const band = cylinder(`forge-spear-coupler-${id}-${y}`, 0.42, 0.78, root, materials.steel, scene);
    band.position.y = y;
    meshes.push(band);
  }
  tipAnchor(root, `forge-spear-trail-tip-${id}`, new B.Vector3(0, 20.05, 0));
  return meshes;
}

function buildDaggers(roots, id, scene, materials, accentMaterial) {
  const B = window.BABYLON;
  const meshes = [];
  for (const [index, side] of [-1, 1].entries()) {
    const root = roots[index];
    const blade = hull(`forge-dagger-blade-${id}-${side}`, [
      {y: -5.8, width: 0.025, depth: 0.025, x: side * 0.34},
      {y: -4.2, width: 0.92, depth: 0.22, bevel: 0.1, x: side * 0.18},
      {y: -1.4, width: 1.12, depth: 0.32, bevel: 0.15},
      {y: -0.35, width: 0.7, depth: 0.34, bevel: 0.15},
    ], root, materials.steel, scene);
    const guard = block(`forge-dagger-guard-${id}-${side}`, 1.9, 0.38, 0.75, root, accentMaterial, scene);
    guard.position.y = -0.25;
    const grip = cylinder(`forge-dagger-grip-${id}-${side}`, 2.15, 0.65, root, materials.dark, scene);
    grip.position.y = 0.98;
    const pommel = cylinder(`forge-dagger-pommel-${id}-${side}`, 0.35, 0.88, root, materials.steel, scene);
    pommel.position.y = 2.16;
    const spine = hull(`forge-dagger-spine-${id}-${side}`, [
      {y: -4.3, width: 0.03, depth: 0.04},
      {y: -1.1, width: 0.14, depth: 0.04},
    ], root, materials.dark, scene);
    spine.position.z = -0.155;
    tipAnchor(root, `forge-dagger-tip-${id}-${side}`, new B.Vector3(side * 0.34, -5.8, 0));
    meshes.push(blade, guard, grip, pommel, spine);
  }
  return meshes;
}

function buildStaff(root, id, scene, materials, accentMaterial) {
  const B = window.BABYLON;
  const shaft = cylinder(`forge-staff-shaft-${id}`, 19, 0.68, root, materials.dark, scene);
  shaft.position.y = 5.2;
  const focus = hull(`forge-staff-focus-${id}`, [
    {y: 14.0, width: 0.35, depth: 0.35},
    {y: 15.3, width: 1.75, depth: 1.75, bevel: 0.4},
    {y: 16.4, width: 1.55, depth: 1.55, bevel: 0.4},
    {y: 17.4, width: 0.25, depth: 0.25},
  ], root, accentMaterial, scene);
  const socket = cylinder(`forge-staff-socket-${id}`, 1.1, 1.9, root, materials.steel, scene, 1.2);
  socket.position.y = 13.9;
  const halo = finish(B.MeshBuilder.CreateTorus(`forge-staff-halo-${id}`, {
    diameter: 4.5, thickness: 0.28, tessellation: 16,
  }, scene), root, materials.steel);
  halo.position.y = 15.7;
  halo.rotation.x = Math.PI / 2 - 0.22;
  const meshes = [shaft, focus, socket, halo];
  for (const side of [-1, 1]) {
    const cage = hull(`forge-staff-reactor-cage-${id}-${side}`, [
      {y: 13.75, width: 0.45, depth: 0.65, x: side * 0.65},
      {y: 15.3, width: 0.42, depth: 0.6, x: side * 2.05},
      {y: 17.4, width: 0.35, depth: 0.45, x: side * 1.7},
      {y: 18.0, width: 0.15, depth: 0.3, x: side * 0.85},
    ], root, materials.dark, scene);
    meshes.push(cage);
  }
  for (const y of [-3.8, 0, 4, 8, 11.5]) {
    const collar = cylinder(`forge-staff-coupler-${id}-${y}`, 0.38, 0.93, root, materials.steel, scene);
    collar.position.y = y;
    meshes.push(collar);
  }
  return meshes;
}

function buildShield(root, id, scene, materials, accentMaterial) {
  // A kite shield uses broad shoulders and a tapered lower impact shoe. Three
  // fitted hulls make its rolled rim, structural backing and colored face.
  const rings = (inset, depth) => [
    {y: -2.6 + inset, width: 0.8, depth, bevel: Math.min(depth * 0.45, 0.22)},
    {y: -0.7, width: 6.5 - inset, depth, bevel: Math.min(depth * 0.45, 0.22)},
    {y: 3.8, width: 8.6 - inset, depth, bevel: Math.min(depth * 0.45, 0.22)},
    {y: 5.9 - inset, width: 5.5 - inset, depth, bevel: Math.min(depth * 0.45, 0.22)},
  ];
  const rim = hull(`forge-shield-rim-${id}`, rings(0, 0.78), root, materials.steel, scene);
  const shell = hull(`forge-shield-shell-${id}`, rings(0.55, 0.35), root, materials.dark, scene);
  shell.position.z = -0.42;
  const face = hull(`forge-shield-face-${id}`, [
    {y: -1.5, width: 0.65, depth: 0.28},
    {y: 0.2, width: 3.5, depth: 0.35},
    {y: 3.8, width: 4.7, depth: 0.45},
    {y: 5.0, width: 3.5, depth: 0.32},
  ], root, accentMaterial, scene);
  face.position.z = -0.68;
  const boss = hull(`forge-shield-core-${id}`, [
    {y: 0.6, width: 0.65, depth: 0.4},
    {y: 1.7, width: 1.9, depth: 0.85, bevel: 0.35},
    {y: 2.8, width: 0.65, depth: 0.4},
  ], root, materials.steel, scene);
  boss.position.z = -1.02;
  const brace = block(`forge-shield-brace-${id}`, 3.5, 0.65, 0.65, root, materials.dark, scene);
  brace.position.set(0, 1.7, 0.75);
  return [rim, shell, face, boss, brace];
}

function buildGrapple(root, id, scene, materials, accentMaterial) {
  const B = window.BABYLON;
  const launcher = hull(`forge-grapple-launcher-${id}`, [
    {y: -1.65, width: 1.7, depth: 4.4, bevel: 0.35},
    {y: -0.85, width: 2.7, depth: 5.4, bevel: 0.4},
    {y: 0.95, width: 2.5, depth: 5.0, bevel: 0.4},
    {y: 1.65, width: 1.7, depth: 3.7, bevel: 0.3},
  ], root, materials.dark, scene);
  launcher.position.z = -1;
  const spool = cylinder(`forge-grapple-spool-${id}`, 3.05, 2.45, root, materials.cable, scene);
  spool.rotation.z = Math.PI / 2;
  spool.position.set(0, 0.1, -0.1);
  const meshes = [launcher, spool];
  for (const side of [-1, 1]) {
    const flange = cylinder(`forge-grapple-flange-${id}-${side}`, 0.25, 2.9, root, accentMaterial, scene);
    flange.rotation.z = Math.PI / 2;
    flange.position.set(side * 1.58, 0.1, -0.1);
    const rail = block(`forge-grapple-rail-${id}-${side}`, 0.34, 0.45, 4.6, root, materials.steel, scene);
    rail.position.set(side * 0.8, 0.85, -2.2);
    meshes.push(flange, rail);
  }
  const cable = tube(`forge-grapple-cable-${id}`, [
    [0, 0.2, -3.5], [0.2, 0.05, -5.8], [0, 0.15, -8.2],
  ], 0.13, root, materials.cable, scene);
  const claw = cylinder(`forge-grapple-claw-${id}`, 2.95, 0.66, root, materials.steel, scene, 0.07);
  claw.rotation.x = -Math.PI / 2;
  claw.position.set(0, 0.15, -9.675);
  meshes.push(cable, claw);
  // Open flukes, rather than a solid cone, explain the hook's catching action.
  for (const side of [-1, 1]) {
    const fluke = hull(`forge-grapple-fluke-${id}-${side}`, [
      {y: 0, width: 0.35, depth: 0.4},
      {y: 1.3, width: 0.42, depth: 0.45, x: side * 1.15},
      {y: 2.1, width: 0.25, depth: 0.3, x: side * 1.1},
      {y: 2.7, width: 0.035, depth: 0.035, x: side * 0.55},
    ], root, materials.steel, scene);
    fluke.rotation.x = -Math.PI / 2;
    fluke.position.set(0, 0.15, -8.2);
    meshes.push(fluke);
  }
  tipAnchor(root, `forge-grapple-tip-${id}`, new B.Vector3(0, 0.15, -11.15));
  return meshes;
}

const BUILDERS = Object.freeze({
  sword: buildSword,
  bow: buildBow,
  spear: buildSpear,
  daggers: buildDaggers,
  staff: buildStaff,
  shield: buildShield,
  grapple: buildGrapple,
});

const REST_ROTATION = Object.freeze({
  sword: {x: 0.04, y: 0, z: -0.34},
  bow: {x: 0, y: -0.10, z: -0.08},
  spear: {x: 0.08, y: 0, z: -0.44},
  daggers: {x: 0, y: 0, z: 0},
  staff: {x: 0.04, y: 0, z: 0.10},
  // -x pitch keeps the shield face vertical against the raised-forearm carry.
  shield: {x: -0.45, y: 0.14, z: -0.12},
  grapple: {x: -0.08, y: 0, z: 0},
});

/**
 * Construct one allowlisted Forge weapon and attach it to its semantic mount.
 * The returned TransformNode exposes normal getChildMeshes() for cosmetics.
 */
export function createForgeWeapon(profile, id, scene, mounts, accentMaterial, dimensions = {}) {
  const B = window.BABYLON;
  const type = BUILDERS[profile?.weapon] ? profile.weapon : 'sword';
  const root = new B.TransformNode(`forge-weapon-${type}-${id}`, scene);
  root._trailTips = [];

  const hand = profile?.weaponPose?.hand;
  root.parent = hand === 'left' ? mounts.handL : hand === 'both' ? mounts.chest : mounts.handR;

  const poseNodes = hand === 'both'
    ? [-1, 1].map((side) => {
        const node = new B.TransformNode(
          `forge-weapon-${type}-${side < 0 ? 'left' : 'right'}-${id}`,
          scene,
        );
        node.parent = side < 0 ? mounts.handL : mounts.handR;
        node._forgePoseSign = side;
        node._trailTips = [];
        return node;
      })
    : [root];
  root._forgePoseNodes = poseNodes;

  if (hand !== 'both') {
    root.position.set(
      (profile.weaponPose.restX || 0) * 2,
      (profile.weaponPose.restY || 0) * 2,
      (profile.weaponPose.restZ || 0) * 2,
    );
  }
  const rest = REST_ROTATION[type];
  root.rotation.set(rest.x, rest.y, rest.z);

  const materials = getResources(scene);
  const span = Math.max(3.0, Number(dimensions.handSpan) || 4.0);
  root._forgeMeshes = type === 'daggers'
    ? buildDaggers(poseNodes, id, scene, materials, accentMaterial)
    : BUILDERS[type](root, id, scene, materials, accentMaterial, span);
  if (type === 'daggers') {
    root._trailTips = poseNodes.flatMap(node => node._trailTips);
  }
  root._visibleMeshCount = root._forgeMeshes.length;
  return root;
}

/** Dispose nodes only; scene-owned and caller-owned materials remain owned. */
export function disposeForgeWeapon(weapon) {
  if (!weapon) return;
  for (const node of weapon._forgePoseNodes || []) {
    if (node !== weapon && typeof node.dispose === 'function' && !node.isDisposed()) {
      node.dispose();
    }
  }
  if (typeof weapon.dispose === 'function' && !weapon.isDisposed()) {
    weapon.dispose();
  }
}
