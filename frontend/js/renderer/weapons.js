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
  const builders = { sword, bow, spear, daggers, staff, shield, grapple };
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
    points[points.length - 1].clone()
  ];
  const string = B().MeshBuilder.CreateTube(`bow-str-${id}`, {
    path: stringPath, radius: 0.15, tessellation: 4, cap: B().Mesh.CAP_ALL
  }, scene);
  string.parent = root;
  string.position.set(8, 4, 0);
  string.material = stringMat;

  root._children = [limb, string];
  return root;
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

  root._children = [handle, cable, hub, clawA, clawB, clawC, glow];
  return root;
}

export function disposeWeapon(weapon) {
  if (!weapon) return;
  // Don't dispose shared materials
  if (weapon._children) {
    weapon._children.forEach(c => c.dispose());
  }
  weapon.dispose();
}
