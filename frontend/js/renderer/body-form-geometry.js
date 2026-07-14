'use strict';

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
 * apex at `to`, which gives limbs tapered tips for free.
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

function bodyScaleFor(spec, width, height, depth) {
  const scale = {width: 0.88, height: 0.82, depth: 0.94};
  if (spec.family === 'mammal') Object.assign(scale, {width: 1.02, height: 0.78, depth: 1.08});
  if (spec.family === 'avian') Object.assign(scale, {width: 0.92, height: 0.86, depth: 1.06});
  if (spec.family === 'amphibian') Object.assign(scale, {width: 1.12, height: 0.68, depth: 1.08});
  if (spec.family === 'marine') Object.assign(scale, {width: 0.94, height: 0.82, depth: 1.18});
  if (spec.family === 'reptile') Object.assign(scale, {width: 1.02, height: 0.84, depth: 1.12});
  if (spec.family === 'construct') Object.assign(scale, {width: 1.18, height: 0.92, depth: 1.08});
  return [width * scale.width, height * scale.height, depth * scale.depth];
}

function addHumanoidBase(spec, context, materials, meshes) {
  const {scene, id, joints} = context;
  const B = window.BABYLON;
  const m = context.metrics || {};
  const torsoWidth = vector(m.torsoWidth, 7);
  const torsoHeight = vector(m.torsoHeight, 8.15);
  const torsoDepth = vector(m.torsoDepth, 3.75);
  const headWidth = vector(m.headWidth, 4.35);
  const headHeight = vector(m.headHeight, 3.55);
  const headDepth = vector(m.headDepth, 3.65);
  const upperArmLength = vector(m.upperArmLength, torsoHeight * 0.52);
  const forearmLength = vector(m.forearmLength, torsoHeight * 0.45);
  const upperLegLength = vector(m.upperLegLength, 5.25);
  const shinLength = vector(m.shinLength, 4.85);
  const thin = spec.family === 'undead' ? 0.11 : (spec.key === 'tyrant_rex' ? 0.14 : 0.18);
  const limbWidth = torsoWidth * thin;
  const bodyShape = ['human', 'construct', 'undead'].includes(spec.family) ? 'box' : 'sphere';
  const headShape = spec.family === 'human' && spec.key !== 'wizard' ? 'sphere' : 'sphere';
  const body = part(B, meshes, bodyShape, `body-form-${spec.key}-torso-${id}`, scene, joints.body,
    materials.primary, bodyScaleFor(spec, torsoWidth, torsoHeight, torsoDepth),
    [0, 1.08 + torsoHeight * 0.50, 0]);
  const head = part(B, meshes, headShape, `body-form-${spec.key}-head-${id}`, scene, joints.head,
    spec.family === 'human' ? materials.secondary : materials.primary,
    [headWidth * (spec.family === 'amphibian' ? 1.20 : 1), headHeight, headDepth], [0, 0, 0]);

  for (const side of [-1, 1]) {
    const label = side < 0 ? 'left' : 'right';
    part(B, meshes, spec.family === 'construct' ? 'box' : 'cylinder',
      `body-form-${spec.key}-${label}-upper-arm-${id}`, scene, joints[`${label}Arm`], materials.primary,
      [limbWidth, upperArmLength, limbWidth], [0, -upperArmLength * 0.5, 0]);
    part(B, meshes, 'cylinder', `body-form-${spec.key}-${label}-forearm-${id}`, scene,
      joints[`${label}Elbow`], materials.secondary,
      [limbWidth * 0.88, forearmLength, limbWidth * 0.92], [0, -forearmLength * 0.5, 0]);
    part(B, meshes, spec.family === 'construct' ? 'box' : 'cylinder',
      `body-form-${spec.key}-${label}-upper-leg-${id}`, scene, joints[`${label}Leg`], materials.primary,
      [limbWidth * 1.18, upperLegLength, limbWidth * 1.22], [0, -upperLegLength * 0.5, 0]);
    part(B, meshes, 'cylinder', `body-form-${spec.key}-${label}-shin-${id}`, scene,
      joints[`${label}Knee`], materials.secondary,
      [limbWidth, shinLength, limbWidth * 1.05], [0, -shinLength * 0.5, 0]);
    part(B, meshes, 'box', `body-form-${spec.key}-${label}-foot-${id}`, scene,
      joints[`${label}Knee`], materials.accent,
      [limbWidth * 1.45, Math.max(0.62, limbWidth * 0.5), limbWidth * 2.25],
      [0, -shinLength + 0.06, -limbWidth * 0.45]);
  }
  return {body, head, dimensions: {torsoWidth, torsoHeight, torsoDepth, headWidth, headHeight, headDepth}};
}

function addFeatureParts(spec, context, materials, meshes, dimensions) {
  const {scene, id, joints} = context;
  const B = window.BABYLON;
  const {torsoWidth: tw, torsoHeight: th, torsoDepth: td, headWidth: hw, headHeight: hh, headDepth: hd} = dimensions;
  const has = name => spec.features.includes(name);
  const add = (shape, name, parent, material, scaling, position, rotation) =>
    part(B, meshes, shape, `body-form-${spec.key}-${name}-${id}`, scene, parent, material, scaling, position, rotation);

  if (has('beak')) add('cone', 'beak', joints.head, materials.accent,
    [hw * 0.40, hd * 0.62, hw * 0.38], [0, -hh * 0.04, -hd * 0.66], [-Math.PI / 2, 0, 0]);
  if (has('comb')) {
    for (const [index, z] of [-0.35, 0, 0.35].entries()) add('cone', `comb-${index}`, joints.head,
      materials.secondary, [hw * 0.22, hh * (0.42 - index * 0.04), hw * 0.22],
      [0, hh * 0.58, z * hd], [0, 0, 0]);
  }
  if (has('wings')) {
    for (const side of [-1, 1]) add('sphere', `wing-${side}`, side < 0 ? joints.leftArm : joints.rightArm,
      materials.secondary, [tw * 0.22, th * 0.54, td * 0.42], [side * tw * 0.08, -th * 0.24, td * 0.06], [0, 0, side * 0.22]);
  }
  if (has('tailFan')) {
    for (const [index, side] of [-1, 0, 1].entries()) add('box', `tail-feather-${index}`, joints.body,
      index === 1 ? materials.accent : materials.secondary,
      [tw * 0.16, th * 0.44, td * 0.16], [side * tw * 0.16, th * 0.56, td * 0.82],
      [0.40, 0, side * 0.28]);
  }
  if (has('muzzle')) add('sphere', 'muzzle', joints.head, materials.secondary,
    [hw * 0.68, hh * 0.38, hd * 0.48], [0, -hh * 0.16, -hd * 0.48]);
  if (has('horns')) for (const side of [-1, 1]) add('cone', `horn-${side}`, joints.head, materials.accent,
    [hw * 0.25, hh * 0.62, hw * 0.25], [side * hw * 0.46, hh * 0.35, -hd * 0.05], [0, 0, -side * 0.52]);
  if (has('ears') || has('catEars') || has('foxEars') || has('longEars') || has('rabbitEars')) {
    const long = has('longEars') || has('rabbitEars');
    const fox = has('foxEars');
    for (const side of [-1, 1]) add(long ? 'box' : 'cone', `ear-${side}`, joints.head, materials.secondary,
      [hw * (fox ? 0.34 : 0.28), hh * (long ? 1.15 : 0.56), hd * 0.22],
      [side * hw * 0.34, hh * (long ? 0.78 : 0.53), 0], [0, 0, side * (long ? 0.10 : 0.18)]);
  }
  if (has('tail') || has('dinoTail')) add('cone', 'tail', joints.body, materials.primary,
    [tw * 0.32, td * (has('dinoTail') ? 2.25 : 1.45), tw * 0.32],
    [0, th * 0.30, td * 0.90], [Math.PI / 2, 0, 0]);
  if (has('brushTail')) add('sphere', 'brush-tail', joints.body, materials.secondary,
    [tw * 0.40, th * 0.70, td * 0.46], [0, th * 0.36, td * 1.05], [0.58, 0, 0]);
  if (has('tailPom')) add('sphere', 'tail-pom', joints.body, materials.secondary,
    [tw * 0.30, tw * 0.30, tw * 0.30], [0, th * 0.34, td * 0.72]);
  if (has('stripes')) for (let index = 0; index < 3; index += 1) add('torus', `stripe-${index}`, joints.body,
    materials.secondary, [tw * (0.68 + index * 0.05), th * 0.20, td * 0.72],
    [0, th * (0.30 + index * 0.17), 0], [Math.PI / 2, 0, 0]);
  if (has('belly')) add('sphere', 'belly', joints.body, materials.secondary,
    [tw * 0.58, th * 0.58, td * 0.28], [0, th * 0.52, -td * 0.55]);
  if (has('frogEyes')) for (const side of [-1, 1]) add('sphere', `eye-${side}`, joints.head, materials.accent,
    [hw * 0.24, hw * 0.24, hw * 0.24], [side * hw * 0.31, hh * 0.42, -hd * 0.22]);
  if (has('wideMouth')) add('box', 'wide-mouth', joints.head, materials.accent,
    [hw * 0.72, hh * 0.10, hd * 0.14], [0, -hh * 0.24, -hd * 0.52]);
  if (has('webFeet')) for (const side of [-1, 1]) add('sphere', `web-foot-${side}`,
    side < 0 ? joints.leftKnee : joints.rightKnee, materials.accent,
    [tw * 0.28, th * 0.08, td * 0.62], [0, -vector(context.metrics?.shinLength, 4.85), -td * 0.24]);
  if (has('sharkSnout') || has('dinoSnout')) add('box', 'snout', joints.head, materials.secondary,
    [hw * (has('dinoSnout') ? 0.88 : 0.78), hh * 0.42, hd * 0.82], [0, -hh * 0.10, -hd * 0.54]);
  if (has('dorsal')) add('cone', 'dorsal-fin', joints.body, materials.secondary,
    [tw * 0.42, th * 0.70, td * 0.24], [0, th * 0.98, td * 0.18], [0, 0, 0]);
  if (has('tailFin')) add('box', 'tail-fin', joints.body, materials.secondary,
    [tw * 0.18, th * 0.72, td * 0.62], [0, th * 0.34, td * 0.92], [0.18, 0, 0]);
  if (has('teeth')) for (const side of [-1, 1]) add('cone', `tooth-${side}`, joints.head, materials.accent,
    [hw * 0.12, hh * 0.25, hw * 0.12], [side * hw * 0.20, -hh * 0.28, -hd * 0.94], [Math.PI, 0, 0]);
  if (has('brow')) for (const side of [-1, 1]) add('box', `brow-${side}`, joints.head, materials.accent,
    [hw * 0.32, hh * 0.10, hd * 0.12], [side * hw * 0.22, hh * 0.17, -hd * 0.52], [0, 0, -side * 0.18]);
  if (has('claws')) for (const side of [-1, 1]) add('cone', `claw-${side}`, side < 0 ? joints.leftKnee : joints.rightKnee,
    materials.accent, [tw * 0.14, td * 0.48, tw * 0.14], [0, -vector(context.metrics?.shinLength, 4.85), -td * 0.72], [-Math.PI / 2, 0, 0]);
  if (has('hair')) add('sphere', 'hair', joints.head, materials.accent,
    [hw * 1.02, hh * 0.38, hd * 1.02], [0, hh * 0.40, hd * 0.04]);
  if (has('belt')) add('torus', 'belt', joints.body, materials.accent,
    [tw * 0.84, th * 0.13, td * 0.86], [0, th * 0.30, 0], [Math.PI / 2, 0, 0]);
  if (has('boots')) for (const side of [-1, 1]) add('box', `boot-${side}`, side < 0 ? joints.leftKnee : joints.rightKnee,
    materials.accent, [tw * 0.28, th * 0.16, td * 0.72], [0, -vector(context.metrics?.shinLength, 4.85), -td * 0.18]);
  if (has('visor')) add('sphere', 'visor', joints.head, materials.accent,
    [hw * 0.86, hh * 0.62, hd * 0.34], [0, hh * 0.02, -hd * 0.48]);
  if (has('backpack')) add('box', 'backpack', joints.body, materials.secondary,
    [tw * 0.66, th * 0.62, td * 0.48], [0, th * 0.54, td * 0.66]);
  if (has('helmet')) add('sphere', 'helmet', joints.head, materials.primary,
    [hw * 1.15, hh * 1.08, hd * 1.16], [0, hh * 0.05, 0]);
  if (has('plume')) add('box', 'plume', joints.head, materials.accent,
    [hw * 0.18, hh * 0.86, hd * 0.52], [0, hh * 0.78, hd * 0.18], [0.12, 0, 0]);
  if (has('pauldrons')) for (const side of [-1, 1]) add('sphere', `pauldron-${side}`,
    side < 0 ? joints.leftArm : joints.rightArm, materials.secondary,
    [tw * 0.34, th * 0.22, td * 0.78], [side * tw * 0.05, -th * 0.05, 0]);
  if (has('hat')) {
    add('cone', 'hat-crown', joints.head, materials.primary,
      [hw * 0.72, hh * 1.45, hd * 0.72], [0, hh * 0.86, 0], [0, 0, -0.12]);
    add('torus', 'hat-brim', joints.head, materials.secondary,
      [hw * 1.28, hh * 0.16, hd * 1.28], [0, hh * 0.48, 0], [Math.PI / 2, 0, 0]);
  }
  if (has('robe')) add('cone', 'robe', joints.body, materials.primary,
    [tw * 1.10, th * 0.88, td * 1.05], [0, th * 0.40, 0], [Math.PI, 0, 0]);
  if (has('rune')) add('torus', 'rune', joints.body, materials.accent,
    [tw * 0.40, th * 0.26, td * 0.18], [0, th * 0.58, -td * 0.58], [Math.PI / 2, 0, 0]);
  if (has('skull')) for (const side of [-1, 1]) add('sphere', `socket-${side}`, joints.head, materials.accent,
    [hw * 0.19, hh * 0.19, hd * 0.12], [side * hw * 0.22, hh * 0.08, -hd * 0.51]);
  if (has('ribs')) for (let index = 0; index < 4; index += 1) add('torus', `rib-${index}`, joints.body,
    materials.secondary, [tw * (0.64 - index * 0.05), th * 0.18, td * 0.66],
    [0, th * (0.32 + index * 0.14), 0], [Math.PI / 2, 0, 0]);
  if (has('boulders')) for (const side of [-1, 1]) add('sphere', `boulder-${side}`,
    side < 0 ? joints.leftArm : joints.rightArm, materials.secondary,
    [tw * 0.42, tw * 0.42, td * 0.86], [side * tw * 0.04, -th * 0.08, 0]);
  if (has('runes')) add('torus', 'golem-rune', joints.body, materials.accent,
    [tw * 0.42, th * 0.28, td * 0.16], [0, th * 0.56, -td * 0.59], [Math.PI / 2, 0, 0]);
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
  return {body, head};
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
  return {body, head};
}

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
    canonical = addHumanoidBase(spec, normalized, materials, meshes);
    addFeatureParts(spec, normalized, materials, meshes, canonical.dimensions);
  }
  if (meshes.length > spec.nearMeshBudget) {
    for (const mesh of meshes) mesh.dispose();
    for (const material of materials.all) material.dispose();
    throw new Error(`${spec.key} exceeded its ${spec.nearMeshBudget}-mesh budget`);
  }
  return {meshes, materials: materials.all, body: canonical.body, head: canonical.head};
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
