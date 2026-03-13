'use strict';

/**
 * Weapon mesh builders — procedural weapon shapes attached to bot bodies.
 * Materials are shared per weapon type for performance.
 * @module renderer/weapons
 */

import { makeMat } from './utils.js';

const B = () => window.BABYLON;

/** @type {Map<string, BABYLON.StandardMaterial>} */
const _sharedMats = new Map();

function _mat(key, scene, color, opts) {
  let mat = _sharedMats.get(key);
  if (!mat || mat.isDisposed) {
    mat = makeMat(key, scene, color, opts);
    mat.freeze();
    _sharedMats.set(key, mat);
  }
  return mat;
}

export function createWeaponMesh(weaponType, botId, scene, parent) {
  const builders = { sword, bow, spear, daggers, staff, shield };
  const builder = builders[weaponType] || sword;
  const mesh = builder(botId, scene);
  mesh.parent = parent;
  return mesh;
}

function sword(id, scene) {
  const blade = B().MeshBuilder.CreateBox(`wpn-${id}`, {
    width: 1.5, height: 18, depth: 0.6
  }, scene);
  blade.position.set(8, 2, 0);
  blade.rotation.z = -0.4;
  blade.material = _mat('w-sword', scene, new (B().Color3)(0.85, 0.85, 0.95), {
    emissiveFactor: 0.6, specular: new (B().Color3)(0.5, 0.5, 0.5)
  });
  return blade;
}

function bow(id, scene) {
  const arc = B().MeshBuilder.CreateTorus(`wpn-${id}`, {
    diameter: 14, thickness: 0.8, tessellation: 10, arc: 0.5
  }, scene);
  arc.position.set(8, 4, 0);
  arc.rotation.y = Math.PI / 2;
  arc.material = _mat('w-bow', scene, new (B().Color3)(0.6, 0.35, 0.1), {
    emissiveFactor: 0.3
  });
  return arc;
}

function spear(id, scene) {
  const shaft = B().MeshBuilder.CreateCylinder(`wpn-${id}`, {
    height: 26, diameter: 1.0, tessellation: 8
  }, scene);
  shaft.position.set(8, 6, 0);
  shaft.rotation.z = -0.3;
  shaft.material = _mat('w-spear', scene, new (B().Color3)(0.5, 0.35, 0.2), {
    emissiveFactor: 0.2
  });
  return shaft;
}

function daggers(id, scene) {
  const root = new (B().TransformNode)(`wpn-${id}`, scene);
  const mat = _mat('w-daggers', scene, new (B().Color3)(0.9, 0.55, 0.15), {
    emissiveFactor: 0.5
  });
  const mkBlade = (name, xOff) => {
    const b = B().MeshBuilder.CreateBox(name, {
      width: 1, height: 8, depth: 0.5
    }, scene);
    b.position.set(xOff, 2, 0);
    b.rotation.z = -0.5;
    b.parent = root;
    b.material = mat;
    return b;
  };
  root._children = [mkBlade(`dg1-${id}`, 7), mkBlade(`dg2-${id}`, -7)];
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

  root._children = [pole, orb];
  return root;
}

function shield(id, scene) {
  const disc = B().MeshBuilder.CreateDisc(`wpn-${id}`, {
    radius: 7, tessellation: 10
  }, scene);
  disc.position.set(-8, 4, 0);
  disc.rotation.y = Math.PI / 2;
  disc.material = _mat('w-shield', scene, new (B().Color3)(0.3, 0.5, 0.85), {
    emissiveFactor: 0.4, specular: new (B().Color3)(0.3, 0.3, 0.4)
  });
  return disc;
}

export function disposeWeapon(weapon) {
  if (!weapon) return;
  // Don't dispose shared materials
  if (weapon._children) {
    weapon._children.forEach(c => c.dispose());
  }
  weapon.dispose();
}
