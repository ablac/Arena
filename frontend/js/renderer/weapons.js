'use strict';

/**
 * Weapon mesh builders — procedural weapon shapes attached to bot bodies.
 * @module renderer/weapons
 */

import { makeMat } from './utils.js';

const B = () => window.BABYLON;

/**
 * Create a weapon mesh for a given weapon type.
 * @param {string} weaponType
 * @param {string} botId
 * @param {BABYLON.Scene} scene
 * @param {BABYLON.Mesh} parent - bot body to attach to
 * @returns {BABYLON.Mesh}
 */
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
  const mat = makeMat(`wmat-${id}`, scene, new (B().Color3)(0.85, 0.85, 0.95), {
    emissiveFactor: 0.6, specular: new (B().Color3)(0.5, 0.5, 0.5)
  });
  blade.material = mat;
  blade._wMat = mat;
  return blade;
}

function bow(id, scene) {
  const arc = B().MeshBuilder.CreateTorus(`wpn-${id}`, {
    diameter: 14, thickness: 0.8, tessellation: 16, arc: 0.5
  }, scene);
  arc.position.set(8, 4, 0);
  arc.rotation.y = Math.PI / 2;
  const mat = makeMat(`wmat-${id}`, scene, new (B().Color3)(0.6, 0.35, 0.1), {
    emissiveFactor: 0.3
  });
  arc.material = mat;
  arc._wMat = mat;
  return arc;
}

function spear(id, scene) {
  const shaft = B().MeshBuilder.CreateCylinder(`wpn-${id}`, {
    height: 26, diameter: 1.0
  }, scene);
  shaft.position.set(8, 6, 0);
  shaft.rotation.z = -0.3;
  const mat = makeMat(`wmat-${id}`, scene, new (B().Color3)(0.5, 0.35, 0.2), {
    emissiveFactor: 0.2
  });
  shaft.material = mat;
  shaft._wMat = mat;
  return shaft;
}

function daggers(id, scene) {
  const root = new (B().TransformNode)(`wpn-${id}`, scene);
  const mkBlade = (name, xOff) => {
    const b = B().MeshBuilder.CreateBox(name, {
      width: 1, height: 8, depth: 0.5
    }, scene);
    b.position.set(xOff, 2, 0);
    b.rotation.z = -0.5;
    b.parent = root;
    return b;
  };
  const b1 = mkBlade(`dg1-${id}`, 7);
  const b2 = mkBlade(`dg2-${id}`, -7);
  const mat = makeMat(`wmat-${id}`, scene, new (B().Color3)(0.9, 0.55, 0.15), {
    emissiveFactor: 0.5
  });
  b1.material = mat;
  b2.material = mat;
  root._wMat = mat;
  root._children = [b1, b2];
  return root;
}

function staff(id, scene) {
  const root = new (B().TransformNode)(`wpn-${id}`, scene);
  const pole = B().MeshBuilder.CreateCylinder(`pole-${id}`, {
    height: 22, diameter: 1.2
  }, scene);
  pole.position.set(8, 6, 0);
  pole.parent = root;
  const poleMat = makeMat(`wpole-${id}`, scene, new (B().Color3)(0.4, 0.25, 0.15), {
    emissiveFactor: 0.2
  });
  pole.material = poleMat;

  const orb = B().MeshBuilder.CreateSphere(`orb-${id}`, { diameter: 4 }, scene);
  orb.position.set(8, 18, 0);
  orb.parent = root;
  const orbMat = makeMat(`worb-${id}`, scene, new (B().Color3)(0.4, 0.15, 0.9), {
    emissiveFactor: 1.2, noLight: true
  });
  orb.material = orbMat;

  root._wMat = poleMat;
  root._children = [pole, orb];
  root._orbMat = orbMat;
  return root;
}

function shield(id, scene) {
  const disc = B().MeshBuilder.CreateDisc(`wpn-${id}`, {
    radius: 7, tessellation: 16
  }, scene);
  disc.position.set(-8, 4, 0);
  disc.rotation.y = Math.PI / 2;
  const mat = makeMat(`wmat-${id}`, scene, new (B().Color3)(0.3, 0.5, 0.85), {
    emissiveFactor: 0.4, specular: new (B().Color3)(0.3, 0.3, 0.4)
  });
  disc.material = mat;
  disc._wMat = mat;
  return disc;
}

/**
 * Dispose a weapon mesh and its materials.
 * @param {BABYLON.Mesh|BABYLON.TransformNode} weapon
 */
export function disposeWeapon(weapon) {
  if (!weapon) return;
  if (weapon._wMat) weapon._wMat.dispose();
  if (weapon._orbMat) weapon._orbMat.dispose();
  if (weapon._children) {
    weapon._children.forEach(c => c.dispose());
  }
  weapon.dispose();
}
