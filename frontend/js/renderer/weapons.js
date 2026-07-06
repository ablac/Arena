'use strict';

/**
 * Weapon mesh builders — procedural weapon shapes attached to bot bodies.
 * Materials are shared per weapon type for performance.
 * @module renderer/weapons
 */

import { makeMat } from './utils.js';
import { isEnabled } from '../settings.js';

const B = () => window.BABYLON;

/** @type {Map<string, BABYLON.StandardMaterial>} */
const _sharedMats = new Map();

function _mat(key, scene, color, opts) {
  let mat = _sharedMats.get(key);
  // Materials have no `isDisposed` member (the old check was always
  // undefined); a cached material is stale when it belongs to another scene.
  if (!mat || mat.getScene() !== scene) {
    mat = makeMat(key, scene, color, opts);
    mat.freeze();
    _sharedMats.set(key, mat);
  }
  return mat;
}

export function createWeaponMesh(weaponType, botId, scene, parent) {
  const builders = { sword, bow, spear, daggers, staff, shield, grapple };
  const builder = builders[weaponType] || sword;
  const mesh = builder(botId, scene);
  mesh.parent = parent;
  return mesh;
}

function sword(id, scene) {
  const root = new (B().TransformNode)(`wpn-${id}`, scene);
  const bladeMat = _mat('w-sword', scene, new (B().Color3)(0.85, 0.85, 0.95), {
    emissiveFactor: 0.6, specular: new (B().Color3)(0.5, 0.5, 0.5)
  });
  const hiltMat = _mat('w-sword-hilt', scene, new (B().Color3)(0.52, 0.38, 0.18), {
    emissiveFactor: 0.22
  });
  const guardMat = _mat('w-sword-guard', scene, new (B().Color3)(0.95, 0.72, 0.25), {
    emissiveFactor: 0.45
  });

  const blade = B().MeshBuilder.CreateBox(`sw-blade-${id}`, {
    width: 1.2, height: 18, depth: 0.5
  }, scene);
  blade.position.set(8, 4.5, 0);
  blade.parent = root;
  blade.material = bladeMat;

  const tip = B().MeshBuilder.CreateCylinder(`sw-tip-${id}`, {
    height: 4.2, diameterTop: 0.01, diameterBottom: 1.1, tessellation: 4
  }, scene);
  tip.position.set(8, 15.8, 0);
  tip.parent = root;
  tip.material = bladeMat;

  const guard = B().MeshBuilder.CreateBox(`sw-guard-${id}`, {
    width: 5.2, height: 0.9, depth: 0.9
  }, scene);
  guard.position.set(8, -3.2, 0);
  guard.parent = root;
  guard.material = guardMat;

  const grip = B().MeshBuilder.CreateCylinder(`sw-grip-${id}`, {
    height: 6.2, diameter: 0.95, tessellation: 6
  }, scene);
  grip.position.set(8, -6.4, 0);
  grip.parent = root;
  grip.material = hiltMat;

  const pommel = B().MeshBuilder.CreateSphere(`sw-pommel-${id}`, {
    diameter: 1.4, segments: 6
  }, scene);
  pommel.position.set(8, -9.6, 0);
  pommel.parent = root;
  pommel.material = guardMat;

  root.rotation.z = -0.4;
  root._children = [blade, tip, guard, grip, pommel];
  return root;
}

function bow(id, scene) {
  const root = new (B().TransformNode)(`wpn-${id}`, scene);
  const woodMat = _mat('w-bow', scene, new (B().Color3)(0.6, 0.35, 0.1), {
    emissiveFactor: 0.3
  });
  const stringMat = _mat('w-bowstring', scene, new (B().Color3)(0.8, 0.8, 0.75), {
    emissiveFactor: 0.2
  });

  // Bow limb — curved arc using a tube path
  const points = [];
  for (let i = 0; i <= 12; i++) {
    const t = (i / 12) * Math.PI; // 0 to PI (half circle)
    const x = Math.cos(t) * 7;   // curve outward
    const y = Math.sin(t) * 7;   // arc height
    points.push(new (B().Vector3)(x * 0.35, y, 0));
  }
  const limb = B().MeshBuilder.CreateTube(`bow-limb-${id}`, {
    path: points, radius: 0.5, tessellation: 6, cap: B().Mesh.CAP_ALL
  }, scene);
  limb.parent = root;
  limb.position.set(8, 4, 0);
  limb.material = woodMat;

  // Bowstring — straight line between the two limb tips
  const stringPath = [
    points[0].clone(),
    new (B().Vector3)(0, 7, 0),
    points[points.length - 1].clone()
  ];
  const string = B().MeshBuilder.CreateTube(`bow-str-${id}`, {
    path: stringPath, radius: 0.15, tessellation: 4, cap: B().Mesh.CAP_ALL, updatable: true
  }, scene);
  string.parent = root;
  string.position.set(8, 4, 0);
  string.material = stringMat;

  root._bowLimb = limb;
  root._bowString = string;
  root._bowStringBasePath = stringPath.map(p => p.clone());
  root._children = [limb, string];
  return root;
}

function spear(id, scene) {
  const root = new (B().TransformNode)(`wpn-${id}`, scene);
  const shaftMat = _mat('w-spear', scene, new (B().Color3)(0.5, 0.35, 0.2), {
    emissiveFactor: 0.2
  });
  const steelMat = _mat('w-spear-tip', scene, new (B().Color3)(0.86, 0.88, 0.95), {
    emissiveFactor: 0.5
  });
  const accentMat = _mat('w-spear-accent', scene, new (B().Color3)(0.75, 0.2, 0.12), {
    emissiveFactor: 0.4
  });

  const shaft = B().MeshBuilder.CreateCylinder(`sp-shaft-${id}`, {
    height: 26, diameter: 0.9, tessellation: 8
  }, scene);
  shaft.position.set(8, 6, 0);
  shaft.parent = root;
  shaft.material = shaftMat;

  const tip = B().MeshBuilder.CreateCylinder(`sp-tip-${id}`, {
    height: 6.6, diameterTop: 0.01, diameterBottom: 1.6, tessellation: 6
  }, scene);
  tip.position.set(8, 22.2, 0);
  tip.parent = root;
  tip.material = steelMat;

  const wingL = B().MeshBuilder.CreateBox(`sp-wingl-${id}`, {
    width: 2.2, height: 0.35, depth: 0.4
  }, scene);
  wingL.position.set(7.05, 18.6, 0);
  wingL.parent = root;
  wingL.material = accentMat;

  const wingR = wingL.clone(`sp-wingr-${id}`);
  wingR.position.x = 8.95;
  wingR.parent = root;

  const butt = B().MeshBuilder.CreateSphere(`sp-butt-${id}`, {
    diameter: 1.1, segments: 5
  }, scene);
  butt.position.set(8, -7.2, 0);
  butt.parent = root;
  butt.material = steelMat;

  // Blade-apex anchor for the swing trail (sampled while a trail runs).
  // Deliberately NOT in _children: root dispose cascades to it, and it must
  // stay out of the per-attack-end child sweep in resetWeaponPose.
  const trailTip = new (B().TransformNode)(`sp-trailtip-${id}`, scene);
  trailTip.position.set(8, 25.5, 0);
  trailTip.parent = root;
  root._trailTips = [trailTip];

  root.rotation.z = -0.3;
  root._children = [shaft, tip, wingL, wingR, butt];
  return root;
}

function daggers(id, scene) {
  const root = new (B().TransformNode)(`wpn-${id}`, scene);
  const mat = _mat('w-daggers', scene, new (B().Color3)(0.9, 0.55, 0.15), {
    emissiveFactor: 0.5
  });
  const gripMat = _mat('w-daggers-grip', scene, new (B().Color3)(0.3, 0.22, 0.18), {
    emissiveFactor: 0.18
  });
  const _trailTips = [];
  const mkBlade = (name, xOff) => {
    const blade = B().MeshBuilder.CreateBox(`${name}-blade`, {
      width: 0.8, height: 7.6, depth: 0.45
    }, scene);
    blade.position.set(xOff, 3, 0);
    blade.parent = root;
    blade.material = mat;
    const tip = B().MeshBuilder.CreateCylinder(`${name}-tip`, {
      height: 2.4, diameterTop: 0.01, diameterBottom: 0.7, tessellation: 4
    }, scene);
    tip.position.set(xOff, 7.8, 0);
    tip.parent = root;
    tip.material = mat;
    const grip = B().MeshBuilder.CreateCylinder(`${name}-grip`, {
      height: 3.4, diameter: 0.55, tessellation: 5
    }, scene);
    grip.position.set(xOff, -1.8, 0);
    grip.parent = root;
    grip.material = gripMat;
    const guard = B().MeshBuilder.CreateBox(`${name}-guard`, {
      width: 1.7, height: 0.5, depth: 0.45
    }, scene);
    guard.position.set(xOff, 0.1, 0);
    guard.parent = root;
    guard.material = mat;
    const handle = new (B().TransformNode)(`${name}-root`, scene);
    blade.parent = handle; tip.parent = handle; grip.parent = handle; guard.parent = handle;
    handle.position.set(0, 0, 0);
    handle.rotation.z = -0.52 + (xOff < 0 ? 0.1 : -0.1);
    handle.parent = root;
    // Blade-apex anchor: parented to the HANDLE (not root) so it inherits the
    // handle's z-rotation and rides the actual blade line. Not in _children.
    const trailTip = new (B().TransformNode)(`${name}-trailtip`, scene);
    trailTip.position.set(xOff, 9.0, 0);
    trailTip.parent = handle;
    _trailTips.push(trailTip);
    return [blade, tip, grip, guard, handle];
  };
  root._children = [...mkBlade(`dg1-${id}`, 7), ...mkBlade(`dg2-${id}`, -7)];
  root._trailTips = _trailTips;
  return root;
}

function staff(id, scene) {
  const root = new (B().TransformNode)(`wpn-${id}`, scene);
  const pole = B().MeshBuilder.CreateCylinder(`pole-${id}`, {
    height: 22, diameter: 1.2, tessellation: 8
  }, scene);
  pole.position.set(8, 6, 0);
  pole.parent = root;
  pole.material = _mat('w-staff-pole', scene, new (B().Color3)(0.4, 0.25, 0.15), {
    emissiveFactor: 0.2
  });

  const orb = B().MeshBuilder.CreateSphere(`orb-${id}`, { diameter: 4, segments: 6 }, scene);
  orb.position.set(8, 18, 0);
  orb.parent = root;
  orb.material = _mat('w-staff-orb', scene, new (B().Color3)(0.4, 0.15, 0.9), {
    emissiveFactor: 1.2, noLight: true
  });

  const halo = B().MeshBuilder.CreateTorus(`staff-halo-${id}`, {
    diameter: 5.8, thickness: 0.35, tessellation: 20
  }, scene);
  halo.position.set(8, 18, 0);
  // Slight tilt so the idle y-spin below reads as a visible precession
  // (a flat torus spinning around its own axis of symmetry shows nothing).
  halo.rotation.x = Math.PI / 2 - 0.14;
  halo.parent = root;
  halo.material = _mat('w-staff-halo', scene, new (B().Color3)(0.78, 0.5, 1.0), {
    emissiveFactor: 0.95, noLight: true
  });

  // Idle micro-animation: halo precession + orb breathe, so casters look
  // magical even between fights (transform-only; materials are shared and
  // frozen and must never be animated). Babylon Animatables run at zero
  // per-frame JS cost. Randomized speed desynchronizes bots.
  _idleSpin(scene, halo, 0.4 + Math.random() * 0.2);
  _idleBreathe(scene, orb, 1.12, 40, 0.8 + Math.random() * 0.4);

  const prongL = B().MeshBuilder.CreateCylinder(`staff-prongl-${id}`, {
    height: 4.8, diameterTop: 0.08, diameterBottom: 0.55, tessellation: 5
  }, scene);
  prongL.position.set(6.7, 18.8, 0);
  prongL.rotation.z = -0.55;
  prongL.parent = root;
  prongL.material = _mat('w-staff-prong', scene, new (B().Color3)(0.62, 0.62, 0.75), {
    emissiveFactor: 0.32
  });

  const prongR = prongL.clone(`staff-prongr-${id}`);
  prongR.position.x = 9.3;
  prongR.rotation.z = 0.55;
  prongR.parent = root;

  root._children = [pole, orb, halo, prongL, prongR];
  return root;
}

function shield(id, scene) {
  const root = new (B().TransformNode)(`wpn-${id}`, scene);
  const shell = B().MeshBuilder.CreateCylinder(`shield-shell-${id}`, {
    height: 1.2, diameter: 14, tessellation: 18
  }, scene);
  shell.position.set(-8, 4, 0);
  shell.rotation.z = 0.18;
  shell.parent = root;
  shell.material = _mat('w-shield', scene, new (B().Color3)(0.3, 0.5, 0.85), {
    emissiveFactor: 0.4, specular: new (B().Color3)(0.3, 0.3, 0.4)
  });

  const rim = B().MeshBuilder.CreateTorus(`shield-rim-${id}`, {
    diameter: 14.4, thickness: 0.9, tessellation: 20
  }, scene);
  rim.position.set(-8, 4, 0);
  rim.rotation.y = Math.PI / 2;
  rim.parent = root;
  rim.material = _mat('w-shield-rim', scene, new (B().Color3)(0.88, 0.9, 0.96), {
    emissiveFactor: 0.28
  });

  const boss = B().MeshBuilder.CreateSphere(`shield-boss-${id}`, {
    diameter: 3.1, segments: 8
  }, scene);
  boss.position.set(-8, 4, 0.5);
  boss.parent = root;
  boss.material = _mat('w-shield-boss', scene, new (B().Color3)(0.92, 0.95, 1), {
    emissiveFactor: 0.34
  });

  root._children = [shell, rim, boss];
  return root;
}

function grapple(id, scene) {
  const root = new (B().TransformNode)(`wpn-${id}`, scene);
  const steel = _mat('w-grapple-steel', scene, new (B().Color3)(0.72, 0.82, 0.9), {
    emissiveFactor: 0.35, specular: new (B().Color3)(0.4, 0.45, 0.5)
  });
  const cord = _mat('w-grapple-cord', scene, new (B().Color3)(0.25, 0.55, 0.75), {
    emissiveFactor: 0.5, noLight: true
  });

  // Handle
  const handle = B().MeshBuilder.CreateCylinder(`ghandle-${id}`, {
    height: 7, diameter: 1.6, tessellation: 8
  }, scene);
  handle.position.set(7, 2, 0);
  handle.rotation.z = -0.55;
  handle.parent = root;
  handle.material = _mat('w-grapple-handle', scene, new (B().Color3)(0.4, 0.4, 0.45), {
    emissiveFactor: 0.3
  });

  const cable = B().MeshBuilder.CreateTube(`gcable-${id}`, {
    path: [
      new (B().Vector3)(7.5, 5.0, 0),
      new (B().Vector3)(10.2, 6.8, 0),
      new (B().Vector3)(12.4, 8.4, 0)
    ],
    radius: 0.35,
    tessellation: 8
  }, scene);
  cable.parent = root;
  cable.material = cord;

  const hub = B().MeshBuilder.CreateSphere(`ghub-${id}`, {
    diameter: 1.8, segments: 8
  }, scene);
  hub.position.set(12.7, 8.6, 0);
  hub.parent = root;
  hub.material = steel;

  const clawA = B().MeshBuilder.CreateCylinder(`gclaw-a-${id}`, {
    height: 5.2, diameterTop: 0.4, diameterBottom: 1.0, tessellation: 6
  }, scene);
  clawA.position.set(14.2, 10.0, -0.9);
  clawA.rotation.z = -0.8;
  clawA.rotation.x = 0.3;
  clawA.parent = root;
  clawA.material = steel;

  const clawB = B().MeshBuilder.CreateCylinder(`gclaw-b-${id}`, {
    height: 5.2, diameterTop: 0.4, diameterBottom: 1.0, tessellation: 6
  }, scene);
  clawB.position.set(14.2, 10.0, 0.9);
  clawB.rotation.z = -0.8;
  clawB.rotation.x = -0.3;
  clawB.parent = root;
  clawB.material = steel;

  const clawC = B().MeshBuilder.CreateCylinder(`gclaw-c-${id}`, {
    height: 4.6, diameterTop: 0.35, diameterBottom: 0.9, tessellation: 6
  }, scene);
  clawC.position.set(14.7, 8.0, 0);
  clawC.rotation.z = -1.2;
  clawC.parent = root;
  clawC.material = steel;

  const glow = B().MeshBuilder.CreateSphere(`gglow-${id}`, {
    diameter: 1.4, segments: 6
  }, scene);
  glow.position.set(12.7, 8.6, 0);
  glow.parent = root;
  glow.material = _mat('w-grapple-glow', scene, new (B().Color3)(0.2, 0.9, 1.0), {
    emissiveFactor: 0.9, noLight: true
  });
  // Idle breathe on the emitter glow (transform-only, see staff note).
  _idleBreathe(scene, glow, 1.18, 42, 0.8 + Math.random() * 0.4);

  root._children = [handle, cable, hub, clawA, clawB, clawC, glow];
  return root;
}

/**
 * @private Starts an Animatable already paused/resumed to match the current
 * setting, then registers a per-frame check that pauses/resumes it whenever
 * the setting changes - so an in-session toggle takes effect immediately,
 * without a page reload, for every weapon instance (including ones created
 * while the setting was off). Shared by _idleSpin and _idleBreathe.
 *
 * Self-unregisters once `node` is disposed (weapons are created/destroyed
 * per-bot all game long via disposeWeapon(), which stops the Animatable but
 * doesn't know about this closure - leaving it registered would leak one
 * scene-level callback per weapon for the rest of the session).
 */
function _gateIdleAnimatable(scene, node, animatable) {
  let wasEnabled = isEnabled('arenaAmbience', 'idleWeaponAnims');
  if (!wasEnabled) animatable.pause();
  const tick = () => {
    if (node.isDisposed()) {
      scene.unregisterBeforeRender(tick);
      return;
    }
    const on = isEnabled('arenaAmbience', 'idleWeaponAnims');
    if (on === wasEnabled) return;
    wasEnabled = on;
    if (on) animatable.restart();
    else animatable.pause();
  };
  scene.registerBeforeRender(tick);
}

/** @private Looping y-rotation Animatable (visible on tilted/featured meshes). */
function _idleSpin(scene, node, speed) {
  const anim = new (B().Animation)(
    `idle-spin-${node.name}`, 'rotation.y', 30,
    B().Animation.ANIMATIONTYPE_FLOAT, B().Animation.ANIMATIONLOOPMODE_CYCLE
  );
  anim.setKeys([
    { frame: 0, value: 0 },
    { frame: 60, value: Math.PI * 2 },
  ]);
  const animatable = scene.beginDirectAnimation(node, [anim], 0, 60, true, speed);
  _gateIdleAnimatable(scene, node, animatable);
}

/** @private Looping uniform scale breathe between 1 and `peak`. */
function _idleBreathe(scene, node, peak, frames, speed) {
  const V3 = B().Vector3;
  const anim = new (B().Animation)(
    `idle-breathe-${node.name}`, 'scaling', 30,
    B().Animation.ANIMATIONTYPE_VECTOR3, B().Animation.ANIMATIONLOOPMODE_CYCLE
  );
  anim.setKeys([
    { frame: 0, value: new V3(1, 1, 1) },
    { frame: frames / 2, value: new V3(peak, peak, peak) },
    { frame: frames, value: new V3(1, 1, 1) },
  ]);
  const animatable = scene.beginDirectAnimation(node, [anim], 0, frames, true, speed);
  _gateIdleAnimatable(scene, node, animatable);
}

/**
 * Lazily build swing trails for a generic weapon that declares tip anchors.
 * Direct generalization of the swordsman makeSwordTrail: one shared per-bot
 * material, one TrailMesh per tip anchor. Returns null when the weapon has
 * no tip anchors or TrailMesh is unavailable; callers tri-state the result
 * (undefined = never tried, null = unavailable, never retried).
 */
export function makeWeaponTrails(weapon, baseColor, diam) {
  const BB = B();
  if (!weapon || !weapon._trailTips || !BB.TrailMesh) return null;
  const scene = weapon.getScene();
  const mat = new BB.StandardMaterial(`${weapon.name}-trailmat`, scene);
  const base = baseColor || new BB.Color3(0.8, 0.8, 0.9);
  // Steel flash tinted toward the bot color, standard alpha blend (the
  // additive movement-trail look was reverted in #55; do not reintroduce it).
  mat.emissiveColor = new BB.Color3(
    Math.min(1, 0.55 + base.r * 0.45),
    Math.min(1, 0.55 + base.g * 0.45),
    Math.min(1, 0.55 + base.b * 0.45)
  );
  mat.diffuseColor = BB.Color3.Black();
  mat.disableLighting = true;
  mat.backFaceCulling = false;
  mat.alpha = 0.4;
  const trails = [];
  for (let i = 0; i < weapon._trailTips.length; i++) {
    const trail = new BB.TrailMesh(`${weapon.name}-trail${i}`, weapon._trailTips[i], scene, diam, 24, false);
    trail.material = mat;
    trail.isPickable = false;
    trail.alwaysSelectAsActiveMesh = true;
    trail.setEnabled(false);
    trails.push(trail);
  }
  weapon._trailMat = mat;
  return trails;
}

export function disposeWeapon(weapon) {
  if (!weapon) return;
  // Stop idle Animatables before disposing so they don't linger against
  // disposed nodes (mirrors pickups.js dispose discipline).
  if (weapon._children) {
    const scene = typeof weapon.getScene === 'function' ? weapon.getScene() : null;
    if (scene) {
      weapon._children.forEach(c => scene.stopAnimation(c));
    }
  }
  // Swing trails are scene-parented (outside the root cascade) and their
  // material is per-bot, not shared. Dispose them BEFORE the children: the
  // dagger tip anchors are parented to handles inside _children, and a
  // TrailMesh must not outlive its generator.
  if (weapon._trails) {
    weapon._trails.forEach(tr => tr.dispose());
  }
  if (weapon._trailMat) weapon._trailMat.dispose();
  // Don't dispose shared materials
  if (weapon._children) {
    weapon._children.forEach(c => c.dispose());
  }
  weapon.dispose();
}
