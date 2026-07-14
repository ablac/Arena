'use strict';

/**
 * Bespoke near-detail geometry for every purchasable full-body form.
 *
 * Each form gets its own builder on the shared Forge joints: chunky mascot
 * masses stacked with intent instead of one generic ellipsoid-with-parts
 * base. All meshes clone five shared primitive templates, parent directly to
 * animated joints, and stay within the form's declared mesh budget.
 *
 * Placement space is local to `joints.body`, which rides a full leg-length
 * above the floor: hips at y=0, shoulders near +7.5, the head joint near
 * +11.3, and the ground at -bodyY. Grounded forms (slime, spider) author
 * geometry DOWN from the joint; biped mascots hang legs from the leg/knee
 * joints so the shared gait animates them.
 * @module renderer/body-form-geometry
 */

import {makeMat, parseColor} from './utils.js';

const _nearResources = new WeakMap();
const _farResources = new WeakMap();

function vector(value, fallback) {
  return Number.isFinite(Number(value)) && Number(value) > 0 ? Number(value) : fallback;
}

function makeMaterials(spec, scene, id) {
  const primary = makeMat(`body-form-${spec.key}-primary-${id}`, scene, parseColor(spec.primary), {
    emissiveFactor: 0.18,
    specular: parseColor(spec.secondary),
  });
  const secondary = makeMat(`body-form-${spec.key}-secondary-${id}`, scene, parseColor(spec.secondary), {
    emissiveFactor: 0.22,
    specular: parseColor(spec.accent),
  });
  const accent = makeMat(`body-form-${spec.key}-accent-${id}`, scene, parseColor(spec.accent), {
    emissiveFactor: 0.82,
    noLight: true,
  });
  // Every visible form material stays mutable because dodge, stun, impact,
  // and death feedback must affect the complete silhouette, including feet,
  // horns, combs, runes, and other accent geometry.
  for (const material of [primary, secondary, accent]) material.backFaceCulling = true;
  for (const material of [primary, secondary, accent]) {
    material._forgeRestEmissive = material.emissiveColor.clone();
  }
  return {primary, secondary, accent, all: [primary, secondary, accent]};
}

function createPrimitive(B, shape, name, scene) {
  switch (shape) {
    case 'sphere':
      return B.MeshBuilder.CreateSphere(name, {diameter: 1, segments: 8}, scene);
    case 'cylinder':
      return B.MeshBuilder.CreateCylinder(name, {height: 1, diameter: 1, tessellation: 8}, scene);
    case 'cone':
      return B.MeshBuilder.CreateCylinder(name, {
        height: 1, diameterTop: 0, diameterBottom: 1, tessellation: 8,
      }, scene);
    case 'torus':
      return B.MeshBuilder.CreateTorus(name, {diameter: 1, thickness: 0.17, tessellation: 12}, scene);
    default:
      return B.MeshBuilder.CreateBox(name, {size: 1}, scene);
  }
}

function nearPrimitiveTemplate(B, shape, scene) {
  let resources = _nearResources.get(scene);
  if (!resources) {
    resources = new Map();
    _nearResources.set(scene, resources);
  }
  if (resources.has(shape)) return resources.get(shape);
  const template = createPrimitive(B, shape, `body-form-near-template-${shape}`, scene);
  template.isPickable = false;
  template.setEnabled(false);
  resources.set(shape, template);
  return template;
}

function part(B, meshes, shape, name, scene, parent, material, scaling, position, rotation) {
  const template = nearPrimitiveTemplate(B, shape, scene);
  const mesh = template.clone(name);
  if (!mesh) throw new Error(`Unable to clone the shared ${shape} body-form primitive`);
  // Babylon clones inherit the disabled template state; near-detail clones
  // are live character parts and must opt back into the active mesh list.
  mesh.setEnabled(true);
  mesh.parent = parent;
  mesh.material = material;
  mesh.isPickable = false;
  mesh.scaling.set(scaling[0], scaling[1], scaling[2]);
  if (position) mesh.position.set(position[0], position[1], position[2]);
  if (rotation) mesh.rotation.set(rotation[0], rotation[1], rotation[2]);
  meshes.push(mesh);
  return mesh;
}

/**
 * Place one Y-aligned primitive so it spans `from` -> `to` in the parent's
 * local space (Babylon YXZ Euler: pitch from +Y, then yaw). Cones point their
 * apex at `to`, which gives limbs and tails tapered tips for free.
 */
function limbSegment(B, meshes, shape, name, scene, parent, material, from, to, thickness) {
  const dx = to[0] - from[0];
  const dy = to[1] - from[1];
  const dz = to[2] - from[2];
  const length = Math.max(0.01, Math.hypot(dx, dy, dz));
  return part(B, meshes, shape, name, scene, parent, material,
    [thickness, length, thickness],
    [(from[0] + to[0]) / 2, (from[1] + to[1]) / 2, (from[2] + to[2]) / 2],
    [Math.acos(Math.max(-1, Math.min(1, dy / length))), Math.atan2(dx, dz), 0]);
}

/** Standard build context shared by every form builder. */
function buildContext(spec, context) {
  const m = context.metrics || {};
  return {
    B: window.BABYLON,
    scene: context.scene,
    id: context.id,
    joints: context.joints,
    key: spec.key,
    tw: vector(m.torsoWidth, 7),
    th: vector(m.torsoHeight, 8.15),
    td: vector(m.torsoDepth, 3.75),
    hw: vector(m.headWidth, 4.35),
    hh: vector(m.headHeight, 3.55),
    hd: vector(m.headDepth, 3.65),
    ua: vector(m.upperArmLength, 4.25),
    fa: vector(m.forearmLength, 3.65),
    aw: vector(m.armWidth, 1.28),
    ul: vector(m.upperLegLength, 5.25),
    sl: vector(m.shinLength, 4.85),
    lw: vector(m.legWidth, 1.72),
    bodyY: vector(m.bodyY, 10.56),
    headY: vector(m.headY, 11.26),
  };
}

const FOOT_STYLES = Object.freeze({
  block: {shape: 'box', scale: [1.5, 0.42, 2.4], z: 0.50},
  boot: {shape: 'box', scale: [1.7, 0.62, 2.5], z: 0.55},
  paw: {shape: 'sphere', scale: [1.7, 0.95, 2.1], z: 0.45},
  bigPaw: {shape: 'sphere', scale: [1.9, 1.0, 3.1], z: 0.85},
  talon: {shape: 'box', scale: [2.1, 0.30, 2.9], z: 0.70},
  hoof: {shape: 'box', scale: [1.5, 0.58, 1.8], z: 0.30},
  web: {shape: 'sphere', scale: [2.6, 0.34, 3.1], z: 0.80},
  stone: {shape: 'box', scale: [2.0, 0.68, 2.6], z: 0.50},
});

/** Six meshes: thigh + shin + styled foot per side, riding the shared gait. */
function bipedLegs(c, meshes, {thick = 1, foot = 'block', legMat, shinMat, footMat, legShape = 'cylinder'}) {
  const lw = c.lw * thick;
  const style = FOOT_STYLES[foot] || FOOT_STYLES.block;
  for (const side of [-1, 1]) {
    const label = side < 0 ? 'left' : 'right';
    part(c.B, meshes, legShape, `body-form-${c.key}-${label}-thigh-${c.id}`, c.scene,
      c.joints[`${label}Leg`], legMat, [lw, c.ul, lw * 1.1], [0, -c.ul * 0.5, 0]);
    part(c.B, meshes, 'cylinder', `body-form-${c.key}-${label}-shin-${c.id}`, c.scene,
      c.joints[`${label}Knee`], shinMat, [lw * 0.85, c.sl, lw * 0.9], [0, -c.sl * 0.5, 0]);
    part(c.B, meshes, style.shape, `body-form-${c.key}-${label}-foot-${c.id}`, c.scene,
      c.joints[`${label}Knee`], footMat,
      [lw * style.scale[0], lw * style.scale[1], lw * style.scale[2]],
      [0, -c.sl + 0.05, -lw * style.z]);
  }
}

/** Four meshes: upper arm + forearm per side. */
function bipedArms(c, meshes, {thick = 1, upperMat, foreMat, armShape = 'cylinder'}) {
  const aw = c.aw * thick;
  for (const side of [-1, 1]) {
    const label = side < 0 ? 'left' : 'right';
    part(c.B, meshes, armShape, `body-form-${c.key}-${label}-upper-arm-${c.id}`, c.scene,
      c.joints[`${label}Arm`], upperMat, [aw, c.ua, aw * 1.05], [0, -c.ua * 0.5, 0]);
    part(c.B, meshes, 'cylinder', `body-form-${c.key}-${label}-forearm-${c.id}`, c.scene,
      c.joints[`${label}Elbow`], foreMat, [aw * 0.85, c.fa, aw * 0.9], [0, -c.fa * 0.5, 0]);
  }
}

// === Bespoke form builders =================================================
// Counted mesh budgets are asserted per-form by buildBodyFormGeometry.

function buildChicken(spec, c, mats, meshes) {
  // Plump low egg body over thin talon legs, wings on the arm joints so the
  // flutter overlay flaps them, stacked comb and a wattle under the beak.
  const body = part(c.B, meshes, 'sphere', `body-form-${spec.key}-body-${c.id}`, c.scene,
    c.joints.body, mats.primary, [c.tw * 1.42, c.th * 1.0, c.td * 1.65], [0, 2.1, 0.2]);
  part(c.B, meshes, 'sphere', `body-form-${spec.key}-breast-${c.id}`, c.scene,
    c.joints.body, mats.secondary, [c.tw * 1.0, c.th * 0.72, c.td * 1.0], [0, 1.2, -c.td * 0.55]);
  part(c.B, meshes, 'cylinder', `body-form-${spec.key}-neck-${c.id}`, c.scene,
    c.joints.head, mats.primary, [c.hw * 0.34, c.hh * 1.5, c.hw * 0.34], [0, -c.hh * 0.85, 0]);
  const head = part(c.B, meshes, 'sphere', `body-form-${spec.key}-head-${c.id}`, c.scene,
    c.joints.head, mats.primary, [c.hw * 0.95, c.hh * 1.0, c.hd * 0.95], [0, 0, 0]);
  part(c.B, meshes, 'cone', `body-form-${spec.key}-beak-${c.id}`, c.scene,
    c.joints.head, mats.accent, [c.hw * 0.34, c.hd * 0.62, c.hw * 0.30],
    [0, -c.hh * 0.06, -c.hd * 0.62], [-Math.PI / 2, 0, 0]);
  part(c.B, meshes, 'sphere', `body-form-${spec.key}-wattle-${c.id}`, c.scene,
    c.joints.head, mats.secondary, [c.hw * 0.20, c.hh * 0.34, c.hw * 0.18], [0, -c.hh * 0.42, -c.hd * 0.42]);
  for (const [index, z] of [-0.30, 0, 0.30].entries()) {
    part(c.B, meshes, 'cone', `body-form-${spec.key}-comb-${index}-${c.id}`, c.scene,
      c.joints.head, mats.secondary, [c.hw * 0.20, c.hh * (0.44 - index * 0.05), c.hw * 0.26],
      [0, c.hh * 0.52, z * c.hd], [z * 0.9, 0, 0]);
  }
  for (const side of [-1, 1]) {
    part(c.B, meshes, 'sphere', `body-form-${spec.key}-wing-${side}-${c.id}`, c.scene,
      side < 0 ? c.joints.leftArm : c.joints.rightArm, mats.primary,
      [c.tw * 0.18, c.th * 0.66, c.td * 1.0], [side * c.tw * 0.05, -c.th * 0.30, 0.1], [0, 0, side * 0.14]);
  }
  for (const [index, x] of [-0.5, 0, 0.5].entries()) {
    part(c.B, meshes, 'box', `body-form-${spec.key}-tail-${index}-${c.id}`, c.scene,
      c.joints.body, index === 1 ? mats.accent : mats.secondary,
      [c.tw * 0.16, c.th * 0.55, c.td * 0.18],
      [x * c.tw * 0.30, 3.6, c.td * 1.05], [0.55, 0, x * 0.5]);
  }
  bipedLegs(c, meshes, {thick: 0.55, foot: 'talon', legMat: mats.accent, shinMat: mats.accent, footMat: mats.accent});
  return {body, head, anchors: {headTopY: c.hh * 0.95, shoulderY: -c.th * 0.30, backPos: [0, 4.6, c.td * 0.55]}};
}

function buildPenguin(spec, c, mats, meshes) {
  // Upright bowling-pin body, huge white belly, flippers, waddle feet.
  const body = part(c.B, meshes, 'sphere', `body-form-${spec.key}-body-${c.id}`, c.scene,
    c.joints.body, mats.primary, [c.tw * 1.30, c.th * 1.45, c.td * 1.55], [0, 3.4, 0]);
  part(c.B, meshes, 'sphere', `body-form-${spec.key}-belly-${c.id}`, c.scene,
    c.joints.body, mats.secondary, [c.tw * 1.02, c.th * 1.18, c.td * 1.0], [0, 2.9, -c.td * 0.52]);
  const head = part(c.B, meshes, 'sphere', `body-form-${spec.key}-head-${c.id}`, c.scene,
    c.joints.head, mats.primary, [c.hw * 0.98, c.hh * 1.05, c.hd * 0.98], [0, -c.hh * 0.18, 0]);
  for (const side of [-1, 1]) {
    part(c.B, meshes, 'sphere', `body-form-${spec.key}-cheek-${side}-${c.id}`, c.scene,
      c.joints.head, mats.secondary, [c.hw * 0.30, c.hh * 0.40, c.hd * 0.24],
      [side * c.hw * 0.26, -c.hh * 0.22, -c.hd * 0.36]);
  }
  part(c.B, meshes, 'cone', `body-form-${spec.key}-beak-${c.id}`, c.scene,
    c.joints.head, mats.accent, [c.hw * 0.26, c.hd * 0.70, c.hw * 0.22],
    [0, -c.hh * 0.16, -c.hd * 0.60], [-Math.PI / 2, 0, 0]);
  for (const side of [-1, 1]) {
    part(c.B, meshes, 'sphere', `body-form-${spec.key}-flipper-${side}-${c.id}`, c.scene,
      side < 0 ? c.joints.leftArm : c.joints.rightArm, mats.primary,
      [c.tw * 0.14, c.th * 0.85, c.td * 0.62], [side * c.tw * 0.10, -c.th * 0.38, 0], [0, 0, side * 0.30]);
  }
  bipedLegs(c, meshes, {thick: 0.5, foot: 'talon', legMat: mats.primary, shinMat: mats.primary, footMat: mats.accent});
  return {body, head, anchors: {headTopY: c.hh * 0.55, shoulderY: -c.th * 0.38, backPos: [0, 4.6, c.td * 0.85]}};
}

function buildCow(spec, c, mats, meshes) {
  // Broad shaggy box body, eye fringe, wide muzzle, uplifted horns, rope
  // tail with a tuft, hoofed legs.
  const body = part(c.B, meshes, 'box', `body-form-${spec.key}-body-${c.id}`, c.scene,
    c.joints.body, mats.primary, [c.tw * 1.45, c.th * 0.95, c.td * 1.75], [0, 2.3, 0]);
  part(c.B, meshes, 'box', `body-form-${spec.key}-chest-shag-${c.id}`, c.scene,
    c.joints.body, mats.secondary, [c.tw * 1.15, c.th * 0.62, c.td * 0.6], [0, 1.2, -c.td * 0.72]);
  const head = part(c.B, meshes, 'box', `body-form-${spec.key}-head-${c.id}`, c.scene,
    c.joints.head, mats.primary, [c.hw * 1.05, c.hh * 1.0, c.hd * 1.0], [0, -c.hh * 0.25, 0]);
  part(c.B, meshes, 'box', `body-form-${spec.key}-fringe-${c.id}`, c.scene,
    c.joints.head, mats.secondary, [c.hw * 1.12, c.hh * 0.42, c.hd * 0.5], [0, c.hh * 0.14, -c.hd * 0.34]);
  part(c.B, meshes, 'box', `body-form-${spec.key}-muzzle-${c.id}`, c.scene,
    c.joints.head, mats.accent, [c.hw * 0.72, c.hh * 0.48, c.hd * 0.42], [0, -c.hh * 0.52, -c.hd * 0.48]);
  for (const side of [-1, 1]) {
    part(c.B, meshes, 'cone', `body-form-${spec.key}-horn-${side}-${c.id}`, c.scene,
      c.joints.head, mats.accent, [c.hw * 0.20, c.hh * 0.85, c.hw * 0.20],
      [side * c.hw * 0.62, c.hh * 0.10, 0], [0, 0, -side * 1.15]);
    part(c.B, meshes, 'sphere', `body-form-${spec.key}-ear-${side}-${c.id}`, c.scene,
      c.joints.head, mats.secondary, [c.hw * 0.28, c.hh * 0.22, c.hd * 0.16],
      [side * c.hw * 0.55, -c.hh * 0.12, -c.hd * 0.05]);
  }
  limbSegment(c.B, meshes, 'cylinder', `body-form-${spec.key}-tail-${c.id}`, c.scene,
    c.joints.body, mats.primary, [0, 4.0, c.td * 0.85], [0, 0.6, c.td * 1.15], c.tw * 0.07);
  part(c.B, meshes, 'sphere', `body-form-${spec.key}-tail-tuft-${c.id}`, c.scene,
    c.joints.body, mats.secondary, [c.tw * 0.16, c.tw * 0.20, c.tw * 0.16], [0, 0.2, c.td * 1.18]);
  bipedLegs(c, meshes, {thick: 1.0, foot: 'hoof', legMat: mats.primary, shinMat: mats.primary, footMat: mats.secondary});
  bipedArms(c, meshes, {thick: 0.85, upperMat: mats.primary, foreMat: mats.primary});
  return {body, head, anchors: {headTopY: c.hh * 0.75, backPos: [0, 4.2, c.td * 0.9]}};
}

function buildCorgi(spec, c, mats, meshes) {
  // Long low loaf body, cream chest, oversized head with tall ears and a
  // tiny nose, stubby paw legs.
  const body = part(c.B, meshes, 'sphere', `body-form-${spec.key}-body-${c.id}`, c.scene,
    c.joints.body, mats.primary, [c.tw * 1.10, c.th * 0.72, c.td * 2.15], [0, 1.3, 0.3]);
  part(c.B, meshes, 'sphere', `body-form-${spec.key}-chest-${c.id}`, c.scene,
    c.joints.body, mats.secondary, [c.tw * 0.85, c.th * 0.60, c.td * 1.0], [0, 0.9, -c.td * 0.68]);
  const head = part(c.B, meshes, 'sphere', `body-form-${spec.key}-head-${c.id}`, c.scene,
    c.joints.head, mats.primary, [c.hw * 1.18, c.hh * 1.12, c.hd * 1.05], [0, -c.hh * 0.15, 0]);
  part(c.B, meshes, 'sphere', `body-form-${spec.key}-muzzle-${c.id}`, c.scene,
    c.joints.head, mats.secondary, [c.hw * 0.52, c.hh * 0.40, c.hd * 0.48], [0, -c.hh * 0.38, -c.hd * 0.46]);
  part(c.B, meshes, 'sphere', `body-form-${spec.key}-nose-${c.id}`, c.scene,
    c.joints.head, mats.accent, [c.hw * 0.14, c.hh * 0.12, c.hd * 0.10], [0, -c.hh * 0.28, -c.hd * 0.68]);
  for (const side of [-1, 1]) {
    part(c.B, meshes, 'cone', `body-form-${spec.key}-ear-${side}-${c.id}`, c.scene,
      c.joints.head, mats.primary, [c.hw * 0.34, c.hh * 0.95, c.hd * 0.26],
      [side * c.hw * 0.38, c.hh * 0.62, 0], [0, 0, side * 0.16]);
  }
  part(c.B, meshes, 'sphere', `body-form-${spec.key}-tail-pom-${c.id}`, c.scene,
    c.joints.body, mats.secondary, [c.tw * 0.26, c.tw * 0.26, c.tw * 0.26], [0, 1.9, c.td * 1.25]);
  bipedLegs(c, meshes, {thick: 0.8, foot: 'paw', legMat: mats.primary, shinMat: mats.secondary, footMat: mats.secondary});
  bipedArms(c, meshes, {thick: 0.7, upperMat: mats.primary, foreMat: mats.secondary});
  if (c.joints.core?.position) c.joints.core.position.set(0, 3.6, -c.td * 0.9);
  return {body, head, anchors: {headTopY: c.hh * 1.25, backPos: [0, 2.6, c.td * 0.9]}};
}

function buildCat(spec, c, mats, meshes) {
  // Sleek chest-forward body with belly stripes, pointed ears, and a long
  // raised two-segment tail.
  const body = part(c.B, meshes, 'sphere', `body-form-${spec.key}-body-${c.id}`, c.scene,
    c.joints.body, mats.primary, [c.tw * 1.0, c.th * 1.05, c.td * 1.4], [0, 2.3, 0]);
  part(c.B, meshes, 'sphere', `body-form-${spec.key}-chest-${c.id}`, c.scene,
    c.joints.body, mats.secondary, [c.tw * 0.72, c.th * 0.78, c.td * 0.8], [0, 1.8, -c.td * 0.5]);
  const head = part(c.B, meshes, 'sphere', `body-form-${spec.key}-head-${c.id}`, c.scene,
    c.joints.head, mats.primary, [c.hw * 1.05, c.hh * 0.98, c.hd * 0.95], [0, -c.hh * 0.12, 0]);
  part(c.B, meshes, 'sphere', `body-form-${spec.key}-muzzle-${c.id}`, c.scene,
    c.joints.head, mats.secondary, [c.hw * 0.44, c.hh * 0.30, c.hd * 0.36], [0, -c.hh * 0.34, -c.hd * 0.44]);
  for (const side of [-1, 1]) {
    part(c.B, meshes, 'cone', `body-form-${spec.key}-ear-${side}-${c.id}`, c.scene,
      c.joints.head, mats.primary, [c.hw * 0.30, c.hh * 0.52, c.hd * 0.20],
      [side * c.hw * 0.36, c.hh * 0.52, 0], [0, 0, side * 0.22]);
  }
  for (let index = 0; index < 2; index += 1) {
    part(c.B, meshes, 'torus', `body-form-${spec.key}-stripe-${index}-${c.id}`, c.scene,
      c.joints.body, mats.secondary, [c.tw * (0.74 + index * 0.06), c.th * 0.22, c.td * 1.05],
      [0, 1.6 + index * 1.5, 0], [Math.PI / 2, 0, 0]);
  }
  limbSegment(c.B, meshes, 'cylinder', `body-form-${spec.key}-tail-a-${c.id}`, c.scene,
    c.joints.body, mats.primary, [0, 1.6, c.td * 0.65], [0, 4.4, c.td * 1.5], c.tw * 0.09);
  limbSegment(c.B, meshes, 'cone', `body-form-${spec.key}-tail-b-${c.id}`, c.scene,
    c.joints.body, mats.secondary, [0, 4.4, c.td * 1.5], [0, 7.2, c.td * 1.75], c.tw * 0.09);
  bipedLegs(c, meshes, {thick: 0.72, foot: 'paw', legMat: mats.primary, shinMat: mats.primary, footMat: mats.secondary});
  bipedArms(c, meshes, {thick: 0.65, upperMat: mats.primary, foreMat: mats.secondary});
  return {body, head, anchors: {headTopY: c.hh * 0.95, backPos: [0, 3.6, c.td * 0.5]}};
}

function buildFox(spec, c, mats, meshes) {
  // Cream ruff, sharp cone snout, tall ears, and the signature fat brush
  // tail with a pale tip.
  const body = part(c.B, meshes, 'sphere', `body-form-${spec.key}-body-${c.id}`, c.scene,
    c.joints.body, mats.primary, [c.tw * 1.0, c.th * 1.02, c.td * 1.45], [0, 2.2, 0]);
  part(c.B, meshes, 'sphere', `body-form-${spec.key}-ruff-${c.id}`, c.scene,
    c.joints.body, mats.secondary, [c.tw * 0.88, c.th * 0.85, c.td * 0.85], [0, 2.6, -c.td * 0.42]);
  const head = part(c.B, meshes, 'sphere', `body-form-${spec.key}-head-${c.id}`, c.scene,
    c.joints.head, mats.primary, [c.hw * 1.05, c.hh * 0.95, c.hd * 1.0], [0, -c.hh * 0.12, 0]);
  part(c.B, meshes, 'cone', `body-form-${spec.key}-snout-${c.id}`, c.scene,
    c.joints.head, mats.secondary, [c.hw * 0.42, c.hd * 0.85, c.hh * 0.40],
    [0, -c.hh * 0.30, -c.hd * 0.62], [-Math.PI / 2, 0, 0]);
  for (const side of [-1, 1]) {
    part(c.B, meshes, 'cone', `body-form-${spec.key}-ear-${side}-${c.id}`, c.scene,
      c.joints.head, mats.primary, [c.hw * 0.36, c.hh * 0.72, c.hd * 0.22],
      [side * c.hw * 0.38, c.hh * 0.56, 0], [0, 0, side * 0.20]);
  }
  limbSegment(c.B, meshes, 'cylinder', `body-form-${spec.key}-tail-a-${c.id}`, c.scene,
    c.joints.body, mats.primary, [0, 1.8, c.td * 0.7], [0, 3.6, c.td * 2.2], c.tw * 0.34);
  limbSegment(c.B, meshes, 'cone', `body-form-${spec.key}-tail-tip-${c.id}`, c.scene,
    c.joints.body, mats.secondary, [0, 3.6, c.td * 2.2], [0, 4.9, c.td * 3.0], c.tw * 0.30);
  bipedLegs(c, meshes, {thick: 0.70, foot: 'paw', legMat: mats.primary, shinMat: mats.primary, footMat: mats.secondary});
  bipedArms(c, meshes, {thick: 0.62, upperMat: mats.primary, foreMat: mats.secondary});
  return {body, head, anchors: {headTopY: c.hh * 1.10, backPos: [0, 3.8, c.td * 0.5]}};
}

function buildRabbit(spec, c, mats, meshes) {
  // Upright egg body, huge ears, cheek muzzle, pom tail, and oversized
  // launcher feet for the hop gait.
  const body = part(c.B, meshes, 'sphere', `body-form-${spec.key}-body-${c.id}`, c.scene,
    c.joints.body, mats.primary, [c.tw * 1.08, c.th * 1.12, c.td * 1.3], [0, 2.6, 0]);
  part(c.B, meshes, 'sphere', `body-form-${spec.key}-belly-${c.id}`, c.scene,
    c.joints.body, mats.secondary, [c.tw * 0.80, c.th * 0.85, c.td * 0.85], [0, 2.1, -c.td * 0.45]);
  const head = part(c.B, meshes, 'sphere', `body-form-${spec.key}-head-${c.id}`, c.scene,
    c.joints.head, mats.primary, [c.hw * 1.08, c.hh * 1.05, c.hd * 0.95], [0, -c.hh * 0.12, 0]);
  part(c.B, meshes, 'sphere', `body-form-${spec.key}-muzzle-${c.id}`, c.scene,
    c.joints.head, mats.secondary, [c.hw * 0.46, c.hh * 0.34, c.hd * 0.36], [0, -c.hh * 0.36, -c.hd * 0.42]);
  for (const side of [-1, 1]) {
    part(c.B, meshes, 'cone', `body-form-${spec.key}-ear-${side}-${c.id}`, c.scene,
      c.joints.head, mats.primary, [c.hw * 0.30, c.hh * 1.55, c.hd * 0.24],
      [side * c.hw * 0.26, c.hh * 0.95, 0], [0, 0, side * 0.10]);
  }
  part(c.B, meshes, 'sphere', `body-form-${spec.key}-tail-pom-${c.id}`, c.scene,
    c.joints.body, mats.secondary, [c.tw * 0.30, c.tw * 0.30, c.tw * 0.30], [0, 1.7, c.td * 0.95]);
  bipedLegs(c, meshes, {thick: 0.85, foot: 'bigPaw', legMat: mats.primary, shinMat: mats.primary, footMat: mats.secondary});
  bipedArms(c, meshes, {thick: 0.62, upperMat: mats.primary, foreMat: mats.secondary});
  return {body, head, anchors: {headTopY: c.hh * 1.85, backPos: [0, 2.8, c.td * 0.7]}};
}

function buildFrog(spec, c, mats, meshes) {
  // Squat wide body that swallows the hips, pale throat, eyes on top of the
  // head, and webbed feet; the head sits low so the whole thing reads crouched.
  const body = part(c.B, meshes, 'sphere', `body-form-${spec.key}-body-${c.id}`, c.scene,
    c.joints.body, mats.primary, [c.tw * 1.55, c.th * 0.85, c.td * 1.7], [0, 1.2, 0]);
  part(c.B, meshes, 'sphere', `body-form-${spec.key}-throat-${c.id}`, c.scene,
    c.joints.body, mats.secondary, [c.tw * 1.05, c.th * 0.60, c.td * 0.9], [0, 0.6, -c.td * 0.62]);
  const head = part(c.B, meshes, 'sphere', `body-form-${spec.key}-head-${c.id}`, c.scene,
    c.joints.head, mats.primary, [c.hw * 1.55, c.hh * 0.85, c.hd * 1.25], [0, -c.hh * 0.95, -c.hd * 0.15]);
  for (const side of [-1, 1]) {
    part(c.B, meshes, 'sphere', `body-form-${spec.key}-eye-${side}-${c.id}`, c.scene,
      c.joints.head, mats.accent, [c.hw * 0.34, c.hw * 0.34, c.hw * 0.30],
      [side * c.hw * 0.48, -c.hh * 0.48, -c.hd * 0.25]);
  }
  part(c.B, meshes, 'box', `body-form-${spec.key}-mouth-${c.id}`, c.scene,
    c.joints.head, mats.secondary, [c.hw * 1.15, c.hh * 0.10, c.hd * 0.30],
    [0, -c.hh * 1.12, -c.hd * 0.62]);
  bipedLegs(c, meshes, {thick: 1.35, foot: 'web', legMat: mats.primary, shinMat: mats.primary, footMat: mats.secondary});
  bipedArms(c, meshes, {thick: 0.72, upperMat: mats.primary, foreMat: mats.secondary});
  if (c.joints.core?.position) c.joints.core.position.set(0, 2.4, -c.td * 1.0);
  return {body, head, anchors: {headTopY: -c.hh * 0.25, shoulderY: -c.th * 0.25, backPos: [0, 2.0, c.td * 0.8]}};
}

function buildShark(spec, c, mats, meshes) {
  // Torpedo body with a pale belly, dorsal fin, pectoral fins on the arm
  // joints, a raised two-segment tail with a vertical fluke, and teeth.
  const body = part(c.B, meshes, 'sphere', `body-form-${spec.key}-body-${c.id}`, c.scene,
    c.joints.body, mats.primary, [c.tw * 1.12, c.th * 1.1, c.td * 1.85], [0, 2.6, 0.3]);
  part(c.B, meshes, 'sphere', `body-form-${spec.key}-belly-${c.id}`, c.scene,
    c.joints.body, mats.secondary, [c.tw * 0.72, c.th * 0.72, c.td * 1.05], [0, 1.5, -c.td * 0.30]);
  const head = part(c.B, meshes, 'sphere', `body-form-${spec.key}-head-${c.id}`, c.scene,
    c.joints.head, mats.primary, [c.hw * 1.30, c.hh * 1.0, c.hd * 1.45], [0, -c.hh * 0.15, -c.hd * 0.1]);
  part(c.B, meshes, 'box', `body-form-${spec.key}-snout-${c.id}`, c.scene,
    c.joints.head, mats.secondary, [c.hw * 0.78, c.hh * 0.38, c.hd * 0.72], [0, -c.hh * 0.32, -c.hd * 0.62]);
  for (const side of [-1, 1]) {
    part(c.B, meshes, 'cone', `body-form-${spec.key}-tooth-${side}-${c.id}`, c.scene,
      c.joints.head, mats.accent, [c.hw * 0.11, c.hh * 0.22, c.hw * 0.11],
      [side * c.hw * 0.22, -c.hh * 0.52, -c.hd * 0.78], [Math.PI, 0, 0]);
  }
  part(c.B, meshes, 'cone', `body-form-${spec.key}-dorsal-${c.id}`, c.scene,
    c.joints.body, mats.primary, [c.tw * 0.14, c.th * 0.85, c.td * 0.85], [0, 6.4, 0.6], [0.35, 0, 0]);
  for (const side of [-1, 1]) {
    part(c.B, meshes, 'sphere', `body-form-${spec.key}-pectoral-${side}-${c.id}`, c.scene,
      side < 0 ? c.joints.leftArm : c.joints.rightArm, mats.primary,
      [c.tw * 0.12, c.th * 0.72, c.td * 0.6], [side * c.tw * 0.06, -c.th * 0.30, 0], [0, 0, side * 0.55]);
  }
  limbSegment(c.B, meshes, 'cylinder', `body-form-${spec.key}-tail-a-${c.id}`, c.scene,
    c.joints.body, mats.primary, [0, 2.6, c.td * 1.35], [0, 4.2, c.td * 2.4], c.tw * 0.24);
  part(c.B, meshes, 'box', `body-form-${spec.key}-fluke-${c.id}`, c.scene,
    c.joints.body, mats.secondary, [c.tw * 0.12, c.th * 0.72, c.td * 0.55], [0, 4.6, c.td * 2.6], [0.3, 0, 0]);
  bipedLegs(c, meshes, {thick: 0.85, foot: 'block', legMat: mats.primary, shinMat: mats.primary, footMat: mats.secondary});
  return {body, head, anchors: {headTopY: c.hh * 0.45, backPos: [0, 2.2, c.td * 1.0]}};
}

function buildRex(spec, c, mats, meshes) {
  // Heavy forward mass, huge head with a separate jaw and teeth, thick tail
  // curving down behind, comically small two-piece arms, clawed feet.
  const body = part(c.B, meshes, 'sphere', `body-form-${spec.key}-body-${c.id}`, c.scene,
    c.joints.body, mats.primary, [c.tw * 1.28, c.th * 1.05, c.td * 1.6], [0, 2.4, 0.2]);
  const head = part(c.B, meshes, 'sphere', `body-form-${spec.key}-head-${c.id}`, c.scene,
    c.joints.head, mats.primary, [c.hw * 1.35, c.hh * 1.25, c.hd * 1.3], [0, -c.hh * 0.05, 0]);
  part(c.B, meshes, 'box', `body-form-${spec.key}-snout-${c.id}`, c.scene,
    c.joints.head, mats.primary, [c.hw * 0.92, c.hh * 0.55, c.hd * 0.95], [0, -c.hh * 0.18, -c.hd * 0.75]);
  part(c.B, meshes, 'box', `body-form-${spec.key}-jaw-${c.id}`, c.scene,
    c.joints.head, mats.secondary, [c.hw * 0.80, c.hh * 0.32, c.hd * 0.80], [0, -c.hh * 0.60, -c.hd * 0.62]);
  part(c.B, meshes, 'box', `body-form-${spec.key}-brow-${c.id}`, c.scene,
    c.joints.head, mats.secondary, [c.hw * 1.05, c.hh * 0.18, c.hd * 0.35], [0, c.hh * 0.28, -c.hd * 0.48]);
  for (const side of [-1, 1]) {
    part(c.B, meshes, 'cone', `body-form-${spec.key}-tooth-${side}-${c.id}`, c.scene,
      c.joints.head, mats.accent, [c.hw * 0.12, c.hh * 0.26, c.hw * 0.12],
      [side * c.hw * 0.28, -c.hh * 0.42, -c.hd * 1.05], [Math.PI, 0, 0]);
  }
  limbSegment(c.B, meshes, 'cylinder', `body-form-${spec.key}-tail-a-${c.id}`, c.scene,
    c.joints.body, mats.primary, [0, 2.2, c.td * 1.1], [0, 1.0, c.td * 2.5], c.tw * 0.42);
  limbSegment(c.B, meshes, 'cone', `body-form-${spec.key}-tail-b-${c.id}`, c.scene,
    c.joints.body, mats.secondary, [0, 1.0, c.td * 2.5], [0, -0.8, c.td * 3.9], c.tw * 0.34);
  for (const side of [-1, 1]) {
    const arm = side < 0 ? c.joints.leftArm : c.joints.rightArm;
    part(c.B, meshes, 'cylinder', `body-form-${spec.key}-arm-${side}-${c.id}`, c.scene,
      arm, mats.primary, [c.aw * 0.75, c.ua * 0.5, c.aw * 0.75], [0, -c.ua * 0.25, -0.2], [-0.4, 0, 0]);
    part(c.B, meshes, 'sphere', `body-form-${spec.key}-claw-hand-${side}-${c.id}`, c.scene,
      arm, mats.secondary, [c.aw * 0.85, c.aw * 0.7, c.aw * 0.9], [0, -c.ua * 0.52, -1.1]);
  }
  bipedLegs(c, meshes, {thick: 1.25, foot: 'talon', legMat: mats.primary, shinMat: mats.primary, footMat: mats.secondary});
  return {body, head, anchors: {headTopY: c.hh * 0.85, backPos: [0, 3.6, c.td * 0.7]}};
}

function buildAdventurer(spec, c, mats, meshes) {
  // Tunic over trousers, hair cap with a fringe, belt, satchel, boots.
  const body = part(c.B, meshes, 'box', `body-form-${spec.key}-torso-${c.id}`, c.scene,
    c.joints.body, mats.secondary, [c.tw * 1.02, c.th * 0.92, c.td * 1.02], [0, 1.08 + c.th * 0.5, 0]);
  part(c.B, meshes, 'box', `body-form-${spec.key}-pelvis-${c.id}`, c.scene,
    c.joints.body, mats.primary, [c.tw * 0.72, c.th * 0.28, c.td * 0.92], [0, 0.5, 0]);
  const head = part(c.B, meshes, 'sphere', `body-form-${spec.key}-head-${c.id}`, c.scene,
    c.joints.head, mats.primary, [c.hw * 1.0, c.hh * 1.05, c.hd * 0.98], [0, 0, 0]);
  part(c.B, meshes, 'sphere', `body-form-${spec.key}-hair-${c.id}`, c.scene,
    c.joints.head, mats.accent, [c.hw * 1.06, c.hh * 0.52, c.hd * 1.04], [0, c.hh * 0.34, c.hd * 0.06]);
  part(c.B, meshes, 'box', `body-form-${spec.key}-fringe-${c.id}`, c.scene,
    c.joints.head, mats.accent, [c.hw * 0.9, c.hh * 0.20, c.hd * 0.25], [0, c.hh * 0.26, -c.hd * 0.44]);
  part(c.B, meshes, 'torus', `body-form-${spec.key}-belt-${c.id}`, c.scene,
    c.joints.body, mats.accent, [c.tw * 0.95, c.th * 0.16, c.td * 1.05], [0, 1.15, 0], [Math.PI / 2, 0, 0]);
  part(c.B, meshes, 'box', `body-form-${spec.key}-satchel-${c.id}`, c.scene,
    c.joints.body, mats.primary, [c.tw * 0.34, c.th * 0.30, c.td * 0.45], [c.tw * 0.48, 1.6, c.td * 0.35]);
  bipedLegs(c, meshes, {thick: 0.9, foot: 'boot', legMat: mats.primary, shinMat: mats.primary, footMat: mats.accent});
  bipedArms(c, meshes, {thick: 0.85, upperMat: mats.secondary, foreMat: mats.primary});
  return {body, head, anchors: {headTopY: c.hh * 0.80}};
}

function buildAstronaut(spec, c, mats, meshes) {
  // Puffy white suit, glowing visor dome, chest panel, life-support pack
  // with an antenna, moon boots.
  const body = part(c.B, meshes, 'sphere', `body-form-${spec.key}-torso-${c.id}`, c.scene,
    c.joints.body, mats.primary, [c.tw * 1.15, c.th * 1.0, c.td * 1.25], [0, 1.08 + c.th * 0.48, 0]);
  part(c.B, meshes, 'box', `body-form-${spec.key}-chest-panel-${c.id}`, c.scene,
    c.joints.body, mats.accent, [c.tw * 0.42, c.th * 0.26, 0.4], [0, 1.1 + c.th * 0.58, -c.td * 0.60]);
  part(c.B, meshes, 'box', `body-form-${spec.key}-pelvis-${c.id}`, c.scene,
    c.joints.body, mats.secondary, [c.tw * 0.78, c.th * 0.26, c.td * 0.95], [0, 0.5, 0]);
  const head = part(c.B, meshes, 'sphere', `body-form-${spec.key}-helmet-${c.id}`, c.scene,
    c.joints.head, mats.primary, [c.hw * 1.22, c.hh * 1.25, c.hd * 1.22], [0, 0.1, 0]);
  part(c.B, meshes, 'sphere', `body-form-${spec.key}-visor-${c.id}`, c.scene,
    c.joints.head, mats.accent, [c.hw * 0.85, c.hh * 0.68, c.hd * 0.40], [0, 0, -c.hd * 0.48]);
  part(c.B, meshes, 'box', `body-form-${spec.key}-pack-${c.id}`, c.scene,
    c.joints.body, mats.secondary, [c.tw * 0.72, c.th * 0.62, c.td * 0.55], [0, 1.1 + c.th * 0.52, c.td * 0.72]);
  part(c.B, meshes, 'cylinder', `body-form-${spec.key}-antenna-${c.id}`, c.scene,
    c.joints.body, mats.accent, [0.16, c.th * 0.45, 0.16], [c.tw * 0.28, 1.1 + c.th * 0.92, c.td * 0.72]);
  bipedLegs(c, meshes, {thick: 1.05, foot: 'boot', legMat: mats.primary, shinMat: mats.primary, footMat: mats.secondary});
  bipedArms(c, meshes, {thick: 1.0, upperMat: mats.primary, foreMat: mats.secondary});
  return {body, head, anchors: {headTopY: c.hh * 0.95}};
}

function buildKnight(spec, c, mats, meshes) {
  // Plate cuirass with a front wedge, round helm with a glowing visor slit,
  // plume, ball pauldrons, armored boots.
  const body = part(c.B, meshes, 'box', `body-form-${spec.key}-torso-${c.id}`, c.scene,
    c.joints.body, mats.primary, [c.tw * 1.08, c.th * 0.95, c.td * 1.05], [0, 1.08 + c.th * 0.5, 0]);
  part(c.B, meshes, 'box', `body-form-${spec.key}-cuirass-${c.id}`, c.scene,
    c.joints.body, mats.secondary, [c.tw * 0.78, c.th * 0.55, 0.55], [0, 1.1 + c.th * 0.55, -c.td * 0.55], [-0.06, 0, 0]);
  part(c.B, meshes, 'box', `body-form-${spec.key}-fauld-${c.id}`, c.scene,
    c.joints.body, mats.secondary, [c.tw * 0.82, c.th * 0.28, c.td * 0.98], [0, 0.5, 0]);
  const head = part(c.B, meshes, 'sphere', `body-form-${spec.key}-helm-${c.id}`, c.scene,
    c.joints.head, mats.primary, [c.hw * 1.15, c.hh * 1.18, c.hd * 1.12], [0, 0.05, 0]);
  part(c.B, meshes, 'box', `body-form-${spec.key}-visor-slit-${c.id}`, c.scene,
    c.joints.head, mats.accent, [c.hw * 0.72, c.hh * 0.14, 0.3], [0, 0.1, -c.hd * 0.52]);
  part(c.B, meshes, 'box', `body-form-${spec.key}-plume-${c.id}`, c.scene,
    c.joints.head, mats.accent, [c.hw * 0.16, c.hh * 0.85, c.hd * 0.60], [0, c.hh * 0.72, c.hd * 0.12], [0.15, 0, 0]);
  for (const side of [-1, 1]) {
    part(c.B, meshes, 'sphere', `body-form-${spec.key}-pauldron-${side}-${c.id}`, c.scene,
      side < 0 ? c.joints.leftArm : c.joints.rightArm, mats.secondary,
      [c.tw * 0.36, c.th * 0.26, c.td * 0.80], [side * c.tw * 0.05, -c.th * 0.04, 0]);
  }
  bipedLegs(c, meshes, {thick: 1.0, foot: 'boot', legMat: mats.primary, shinMat: mats.secondary, footMat: mats.primary});
  bipedArms(c, meshes, {thick: 0.95, upperMat: mats.primary, foreMat: mats.secondary});
  return {body, head, anchors: {headTopY: c.hh * 1.30}};
}

function buildWizard(spec, c, mats, meshes) {
  // Floor-swept robe cone, sash, beard, tall crooked hat with a brim, and a
  // glowing rune on the chest.
  const body = part(c.B, meshes, 'cone', `body-form-${spec.key}-robe-${c.id}`, c.scene,
    c.joints.body, mats.primary, [c.tw * 1.75, c.th * 1.6, c.td * 2.6], [0, 1.08 + c.th * 0.28, 0]);
  part(c.B, meshes, 'box', `body-form-${spec.key}-sash-${c.id}`, c.scene,
    c.joints.body, mats.secondary, [c.tw * 0.85, c.th * 0.20, c.td * 1.1], [0, 1.1 + c.th * 0.42, 0], [0, 0, 0.1]);
  part(c.B, meshes, 'torus', `body-form-${spec.key}-rune-${c.id}`, c.scene,
    c.joints.body, mats.accent, [c.tw * 0.40, c.th * 0.26, c.td * 0.20], [0, 1.1 + c.th * 0.62, -c.td * 0.62], [Math.PI / 2, 0, 0]);
  const head = part(c.B, meshes, 'sphere', `body-form-${spec.key}-head-${c.id}`, c.scene,
    c.joints.head, mats.secondary, [c.hw * 0.98, c.hh * 1.0, c.hd * 0.95], [0, 0, 0]);
  part(c.B, meshes, 'cone', `body-form-${spec.key}-beard-${c.id}`, c.scene,
    c.joints.head, mats.secondary, [c.hw * 0.55, c.hh * 0.95, c.hd * 0.40],
    [0, -c.hh * 0.72, -c.hd * 0.28], [Math.PI, 0, 0]);
  part(c.B, meshes, 'cone', `body-form-${spec.key}-hat-crown-${c.id}`, c.scene,
    c.joints.head, mats.primary, [c.hw * 0.80, c.hh * 1.55, c.hd * 0.80], [0, c.hh * 0.95, 0], [0, 0, -0.14]);
  part(c.B, meshes, 'torus', `body-form-${spec.key}-hat-brim-${c.id}`, c.scene,
    c.joints.head, mats.primary, [c.hw * 1.35, c.hh * 0.16, c.hd * 1.35], [0, c.hh * 0.42, 0], [Math.PI / 2, 0, 0]);
  bipedLegs(c, meshes, {thick: 0.8, foot: 'boot', legMat: mats.primary, shinMat: mats.primary, footMat: mats.secondary});
  bipedArms(c, meshes, {thick: 0.9, upperMat: mats.primary, foreMat: mats.secondary});
  if (c.joints.core?.position) c.joints.core.position.set(0, 1.1 + c.th * 0.62, -c.td * 0.62);
  return {body, head, anchors: {headTopY: c.hh * 1.85}};
}

function buildSkeleton(spec, c, mats, meshes) {
  // Dark void torso behind stacked ribs over a bone pelvis, a skull with
  // glowing sockets and a separate jaw, and thin bone limbs.
  const body = part(c.B, meshes, 'box', `body-form-${spec.key}-void-${c.id}`, c.scene,
    c.joints.body, mats.secondary, [c.tw * 0.62, c.th * 0.85, c.td * 0.55], [0, 1.08 + c.th * 0.5, 0]);
  part(c.B, meshes, 'cylinder', `body-form-${spec.key}-spine-${c.id}`, c.scene,
    c.joints.body, mats.primary, [0.5, c.th * 0.95, 0.5], [0, 1.08 + c.th * 0.5, c.td * 0.25]);
  for (let index = 0; index < 3; index += 1) {
    part(c.B, meshes, 'torus', `body-form-${spec.key}-rib-${index}-${c.id}`, c.scene,
      c.joints.body, mats.primary, [c.tw * (0.85 - index * 0.10), c.th * 0.22, c.td * 1.0],
      [0, 1.1 + c.th * (0.72 - index * 0.16), 0], [Math.PI / 2, 0, 0]);
  }
  part(c.B, meshes, 'box', `body-form-${spec.key}-pelvis-${c.id}`, c.scene,
    c.joints.body, mats.primary, [c.tw * 0.68, c.th * 0.20, c.td * 0.75], [0, 0.45, 0]);
  const head = part(c.B, meshes, 'sphere', `body-form-${spec.key}-skull-${c.id}`, c.scene,
    c.joints.head, mats.primary, [c.hw * 1.05, c.hh * 1.05, c.hd * 1.0], [0, 0.1, 0]);
  part(c.B, meshes, 'box', `body-form-${spec.key}-jaw-${c.id}`, c.scene,
    c.joints.head, mats.primary, [c.hw * 0.62, c.hh * 0.28, c.hd * 0.50], [0, -c.hh * 0.52, -c.hd * 0.12]);
  for (const side of [-1, 1]) {
    part(c.B, meshes, 'sphere', `body-form-${spec.key}-socket-${side}-${c.id}`, c.scene,
      c.joints.head, mats.accent, [c.hw * 0.22, c.hh * 0.24, c.hd * 0.12],
      [side * c.hw * 0.22, 0.12, -c.hd * 0.46]);
  }
  bipedLegs(c, meshes, {thick: 0.45, foot: 'block', legMat: mats.primary, shinMat: mats.primary, footMat: mats.primary});
  bipedArms(c, meshes, {thick: 0.42, upperMat: mats.primary, foreMat: mats.primary});
  return {body, head, anchors: {headTopY: c.hh * 0.75}};
}

function buildGolem(spec, c, mats, meshes) {
  // Massive slab torso with a sunken head, boulder shoulders, oversized rock
  // fists, glowing rune, and stone feet.
  const body = part(c.B, meshes, 'box', `body-form-${spec.key}-torso-${c.id}`, c.scene,
    c.joints.body, mats.primary, [c.tw * 1.42, c.th * 1.05, c.td * 1.35], [0, 1.08 + c.th * 0.48, 0]);
  part(c.B, meshes, 'box', `body-form-${spec.key}-pelvis-${c.id}`, c.scene,
    c.joints.body, mats.secondary, [c.tw * 0.92, c.th * 0.30, c.td * 1.05], [0, 0.5, 0]);
  part(c.B, meshes, 'torus', `body-form-${spec.key}-rune-${c.id}`, c.scene,
    c.joints.body, mats.accent, [c.tw * 0.45, c.th * 0.30, c.td * 0.18], [0, 1.1 + c.th * 0.58, -c.td * 0.70], [Math.PI / 2, 0, 0]);
  const head = part(c.B, meshes, 'box', `body-form-${spec.key}-head-${c.id}`, c.scene,
    c.joints.head, mats.secondary, [c.hw * 0.85, c.hh * 0.72, c.hd * 0.85], [0, -c.hh * 0.30, 0]);
  part(c.B, meshes, 'box', `body-form-${spec.key}-brow-${c.id}`, c.scene,
    c.joints.head, mats.primary, [c.hw * 0.95, c.hh * 0.22, c.hd * 0.45], [0, -c.hh * 0.08, -c.hd * 0.30]);
  for (const side of [-1, 1]) {
    const arm = side < 0 ? c.joints.leftArm : c.joints.rightArm;
    const elbow = side < 0 ? c.joints.leftElbow : c.joints.rightElbow;
    part(c.B, meshes, 'sphere', `body-form-${spec.key}-boulder-${side}-${c.id}`, c.scene,
      arm, mats.primary, [c.tw * 0.52, c.tw * 0.48, c.td * 1.0], [side * c.tw * 0.06, -c.th * 0.02, 0]);
    part(c.B, meshes, 'sphere', `body-form-${spec.key}-fist-${side}-${c.id}`, c.scene,
      elbow, mats.secondary, [c.aw * 1.9, c.aw * 1.9, c.aw * 2.0], [0, -c.fa * 0.95, 0]);
  }
  bipedLegs(c, meshes, {thick: 1.35, foot: 'stone', legMat: mats.primary, shinMat: mats.secondary, footMat: mats.secondary, legShape: 'box'});
  bipedArms(c, meshes, {thick: 1.3, upperMat: mats.primary, foreMat: mats.secondary, armShape: 'box'});
  return {body, head, anchors: {headTopY: c.hh * 0.35, shoulderY: -c.th * 0.05}};
}

function buildSlime(spec, context, materials, meshes) {
  const {scene, id, joints} = context;
  const B = window.BABYLON;
  const m = context.metrics || {};
  const tw = vector(m.torsoWidth, 7), th = vector(m.torsoHeight, 8.15), td = vector(m.torsoDepth, 3.75);
  const bodyY = vector(m.bodyY, 10.56);
  const headY = vector(m.headY, 11.26);
  // The chassis joint rides a full leg-length above the floor; the monarch is
  // a grounded gel stack, so every piece is authored down from that joint.
  const floor = -bodyY;
  const body = part(B, meshes, 'sphere', `body-form-${spec.key}-blob-${id}`, scene, joints.body, materials.primary,
    [tw * 1.05, th * 1.05, td * 1.50], [0, floor + th * 0.72, 0]);
  part(B, meshes, 'sphere', `body-form-${spec.key}-skirt-${id}`, scene, joints.body, materials.primary,
    [tw * 1.45, th * 0.55, td * 2.05], [0, floor + th * 0.16, 0]);
  for (const side of [-1, 1]) part(B, meshes, 'sphere', `body-form-${spec.key}-drip-${side}-${id}`,
    scene, joints.body, materials.secondary,
    [tw * 0.22, tw * 0.18, tw * 0.22], [side * tw * 0.62, floor + th * 0.10, -side * td * 0.55]);
  // Face and crown ride the head joint (offset back down onto the mass) so
  // head-look and hit whiplash still read on a body with no neck.
  const head = part(B, meshes, 'sphere', `body-form-${spec.key}-face-${id}`, scene, joints.head, materials.secondary,
    [tw * 0.62, th * 0.40, td * 0.70], [0, floor + th * 0.84 - headY, -td * 0.55]);
  for (const side of [-1, 1]) part(B, meshes, 'sphere', `body-form-${spec.key}-eye-${side}-${id}`,
    scene, joints.head, materials.accent,
    [tw * 0.12, tw * 0.14, tw * 0.10], [side * tw * 0.17, floor + th * 0.95 - headY, -td * 0.78]);
  for (const [index, side] of [-1, 0, 1].entries()) part(B, meshes, 'cone', `body-form-${spec.key}-crown-${index}-${id}`,
    scene, joints.head, materials.accent, [tw * 0.20, th * (0.34 + (index === 1 ? 0.14 : 0)), tw * 0.20],
    [side * tw * 0.22, floor + th * 1.32 - headY, 0]);
  // Pseudopods rise out of the crest and wave with the shared arm gait.
  for (const side of [-1, 1]) part(B, meshes, 'sphere', `body-form-${spec.key}-arm-${side}-${id}`, scene,
    side < 0 ? joints.leftArm : joints.rightArm, materials.primary,
    [tw * 0.30, th * 0.55, td * 0.45], [side * 0.4, -th * 0.95, -td * 0.30]);
  part(B, meshes, 'sphere', `body-form-${spec.key}-core-${id}`, scene, joints.body, materials.accent,
    [tw * 0.32, tw * 0.32, tw * 0.24], [0, floor + th * 0.80, -td * 0.85]);
  // Sink the shared Arena status core into the gel crest instead of leaving
  // it hovering at the missing humanoid chest height.
  if (joints.core?.position) joints.core.position.set(0, floor + th * 1.02, -td * 0.35);
  return {body, head, anchors: {headTopY: floor + th * 1.62 - headY, shoulderY: -th * 0.95, backPos: [0, floor + th * 0.9, td * 0.9]}};
}

function buildDrone(spec, context, materials, meshes) {
  const {scene, id, joints} = context;
  const B = window.BABYLON;
  const m = context.metrics || {};
  const tw = vector(m.torsoWidth, 7), th = vector(m.torsoHeight, 8.15), td = vector(m.torsoDepth, 3.75);
  const bodyY = vector(m.bodyY, 10.56);
  const headY = vector(m.headY, 11.26);
  // Carapace hangs below the chassis joint; eight two-segment legs arch up
  // from its rim and plant their tapered tips on the actual floor.
  const standY = -bodyY * 0.18;
  const groundY = -bodyY + 0.15;
  const body = part(B, meshes, 'sphere', `body-form-${spec.key}-carapace-${id}`, scene, joints.body, materials.primary,
    [tw * 1.30, th * 0.42, td * 1.60], [0, standY, -td * 0.10]);
  part(B, meshes, 'sphere', `body-form-${spec.key}-abdomen-${id}`, scene, joints.body, materials.secondary,
    [tw * 0.95, th * 0.40, td * 1.15], [0, standY + 0.4, td * 0.95]);
  const head = part(B, meshes, 'sphere', `body-form-${spec.key}-sensor-head-${id}`, scene, joints.head, materials.secondary,
    [tw * 0.52, th * 0.26, td * 0.85], [0, standY - headY + 0.3, -td * 1.05]);
  part(B, meshes, 'sphere', `body-form-${spec.key}-optic-${id}`, scene, joints.head, materials.accent,
    [tw * 0.20, tw * 0.20, tw * 0.16], [0, standY - headY + 0.45, -td * 1.45]);
  for (const side of [-1, 1]) {
    for (let index = 0; index < 4; index += 1) {
      const spread = index - 1.5;
      const hip = [side * tw * 0.58, standY + 0.5, spread * td * 0.50 - td * 0.10];
      const knee = [side * tw * 1.30, standY + 3.4, spread * td * 1.00 - td * 0.10];
      const foot = [side * tw * 1.80, groundY, spread * td * 1.45 - td * 0.10];
      limbSegment(B, meshes, 'cylinder', `body-form-${spec.key}-femur-${side}-${index}-${id}`,
        scene, joints.body, index % 2 ? materials.secondary : materials.primary,
        hip, knee, tw * 0.11);
      limbSegment(B, meshes, 'cone', `body-form-${spec.key}-tibia-${side}-${index}-${id}`,
        scene, joints.body, materials.secondary, knee, foot, tw * 0.10);
    }
  }
  // Sink the shared Arena status core onto the carapace so it reads as a
  // power light instead of hovering where the humanoid chest used to be.
  if (joints.core?.position) joints.core.position.set(0, standY + th * 0.10, -td * 0.60);
  return {body, head, anchors: {headTopY: standY - headY + 2.6, shoulderY: standY - vector(m.shoulderY, 7.47) + 1.0, backPos: [0, standY + 1.6, td * 0.8]}};
}

const FORM_BUILDERS = Object.freeze({
  giant_chicken: buildChicken,
  emperor_penguin: buildPenguin,
  highland_cow: buildCow,
  corgi: buildCorgi,
  tabby_cat: buildCat,
  red_fox: buildFox,
  battle_rabbit: buildRabbit,
  bullfrog: buildFrog,
  land_shark: buildShark,
  tyrant_rex: buildRex,
  human_adventurer: buildAdventurer,
  astronaut: buildAstronaut,
  knight: buildKnight,
  wizard: buildWizard,
  skeleton: buildSkeleton,
  stone_golem: buildGolem,
});

/** Build one bounded, articulated near-detail body on the shared Forge joints. */
export function buildBodyFormGeometry(spec, context) {
  if (!spec || !context?.scene || !context?.joints) throw new TypeError('A body form, scene, and Forge joints are required');
  const id = String(context.id || spec.key).slice(0, 96);
  const normalized = {...context, id};
  const materials = makeMaterials(spec, context.scene, id);
  const meshes = [];
  let canonical;
  if (spec.family === 'slime') canonical = buildSlime(spec, normalized, materials, meshes);
  else if (spec.family === 'drone') canonical = buildDrone(spec, normalized, materials, meshes);
  else {
    const builder = FORM_BUILDERS[spec.key] || buildAdventurer;
    canonical = builder(spec, buildContext(spec, normalized), materials, meshes);
  }
  if (meshes.length > spec.nearMeshBudget) {
    for (const mesh of meshes) mesh.dispose();
    for (const material of materials.all) material.dispose();
    throw new Error(`${spec.key} exceeded its ${spec.nearMeshBudget}-mesh budget`);
  }
  return {meshes, materials: materials.all, body: canonical.body, head: canonical.head, anchors: canonical.anchors || null};
}

function farSignatureParts(spec) {
  const parts = [
    {shape: 'sphere', position: [0, 0.55, 0], scaling: [0.52, 0.48, 0.34]},
    {shape: 'sphere', position: [0, 0.88, -0.03], scaling: [0.30, 0.25, 0.29]},
  ];
  if (spec.family === 'drone') {
    parts[0].scaling = [0.58, 0.24, 0.48];
    for (const side of [-1, 1]) for (const z of [-0.24, 0.24]) parts.push({shape: 'box', position: [side * 0.52, 0.42, z], scaling: [0.52, 0.08, 0.08], rotation: [0, 0, side * 0.48]});
  } else if (spec.family === 'slime') {
    parts[0].scaling = [0.62, 0.52, 0.48];
    parts.push({shape: 'cone', position: [0, 1.08, 0], scaling: [0.24, 0.28, 0.24]});
  } else if (spec.features.includes('rabbitEars') || spec.features.includes('longEars')) {
    for (const side of [-1, 1]) parts.push({shape: 'box', position: [side * 0.17, 1.15, 0], scaling: [0.12, 0.48, 0.12]});
  } else if (spec.features.includes('horns')) {
    for (const side of [-1, 1]) parts.push({shape: 'cone', position: [side * 0.30, 1.04, 0], scaling: [0.18, 0.30, 0.18], rotation: [0, 0, -side * 0.48]});
  } else if (spec.features.includes('beak')) {
    parts.push({shape: 'cone', position: [0, 0.87, -0.33], scaling: [0.18, 0.34, 0.18], rotation: [-Math.PI / 2, 0, 0]});
  } else if (spec.features.includes('dorsal')) {
    parts.push({shape: 'cone', position: [0, 0.96, 0.10], scaling: [0.25, 0.42, 0.12]});
  } else if (spec.features.includes('hat')) {
    parts.push({shape: 'cone', position: [0, 1.22, 0], scaling: [0.38, 0.72, 0.38]});
  } else if (spec.features.includes('tail') || spec.features.includes('dinoTail') || spec.features.includes('brushTail')) {
    parts.push({shape: 'cone', position: [0, 0.52, 0.48], scaling: [0.18, 0.52, 0.18], rotation: [Math.PI / 2, 0, 0]});
  }
  return parts;
}

function farTemplate(spec, scene) {
  let resources = _farResources.get(scene);
  if (!resources) {
    resources = new Map();
    _farResources.set(scene, resources);
  }
  if (resources.has(spec.key)) return resources.get(spec.key);

  const B = window.BABYLON;
  const material = makeMat(`body-form-far-${spec.key}`, scene, parseColor(spec.primary), {
    emissiveFactor: 0.72, noLight: true,
  });
  material.backFaceCulling = true;
  material.freeze();
  const pieces = farSignatureParts(spec).map((recipe, index) => {
    const mesh = recipe.shape === 'sphere'
      ? B.MeshBuilder.CreateSphere(`body-form-far-${spec.key}-${index}`, {diameter: 1, segments: 8}, scene)
      : recipe.shape === 'cone'
        ? B.MeshBuilder.CreateCylinder(`body-form-far-${spec.key}-${index}`, {height: 1, diameterTop: 0, diameterBottom: 1, tessellation: 8}, scene)
        : B.MeshBuilder.CreateBox(`body-form-far-${spec.key}-${index}`, {size: 1}, scene);
    mesh.position.set(...recipe.position);
    mesh.scaling.set(...recipe.scaling);
    if (recipe.rotation) mesh.rotation.set(...recipe.rotation);
    mesh.material = material;
    mesh.isPickable = false;
    return mesh;
  });
  let template;
  if (typeof B.Mesh?.MergeMeshes === 'function') {
    template = B.Mesh.MergeMeshes(pieces, true, true, undefined, false, true);
  } else {
    template = pieces.shift();
    for (const piece of pieces) piece.dispose();
    template.position.set(0, 0, 0);
    template.scaling.set(1, 1, 1);
  }
  if (!template) throw new Error(`Unable to build ${spec.key} far proxy`);
  template.name = `body-form-far-template-${spec.key}`;
  template.material = material;
  template.isPickable = false;
  template.setEnabled(false);
  resources.set(spec.key, template);
  return template;
}

/** Create one shared-geometry, form-specific far proxy (never the blue generic bot). */
export function createBodyFormFarProxy(spec, scene, parent, dimensions = {}) {
  const template = farTemplate(spec, scene);
  const proxy = template.clone(`body-form-far-${spec.key}`);
  proxy.parent = parent;
  proxy.material = template.material;
  proxy.position.set(0, 0, 0);
  proxy.scaling.set(
    vector(dimensions.width, 10),
    vector(dimensions.height, 24),
    vector(dimensions.depth, 6),
  );
  proxy.isPickable = true;
  proxy.setEnabled(false);
  return proxy;
}
