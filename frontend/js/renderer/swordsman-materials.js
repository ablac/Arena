'use strict';

/**
 * Scene-owned shared materials for the articulated swordsman weapon.
 *
 * ArenaEngine rebuilds the Babylon scene when the arena dimensions change.
 * Scene disposal also disposes every material registered with that scene, so
 * module-level material caches must never hand those objects to the next
 * scene.
 * @module renderer/swordsman-materials
 */

import { makeMat } from './utils.js';

/** @type {{blade: BABYLON.StandardMaterial, guard: BABYLON.StandardMaterial, grip: BABYLON.StandardMaterial, pommel: BABYLON.StandardMaterial}|null} */
let cachedSwordMaterials = null;
const MATERIAL_SLOTS = ['blade', 'guard', 'grip', 'pommel'];

function materialIsDisposed(material) {
  if (!material) return true;
  if (material._isDisposed === true || material.isDisposed === true) return true;
  if (typeof material.isDisposed === 'function') return material.isDisposed();
  return false;
}

function materialIsStale(material, scene) {
  if (!material || typeof material.getScene !== 'function') return true;
  const owner = material.getScene();
  return owner !== scene || !owner || owner.isDisposed === true || materialIsDisposed(material);
}

function createSwordMaterials(scene) {
  const B = window.BABYLON;
  const created = [];
  const shared = (name, color, options) => {
    const material = makeMat(name, scene, color, options);
    material.freeze();
    created.push(material);
    return material;
  };

  try {
    return {
      blade: shared('sw-blade', new B.Color3(0.85, 0.85, 0.95), {
        emissiveFactor: 0.5, specular: new B.Color3(0.6, 0.6, 0.6),
      }),
      guard: shared('sw-guard', new B.Color3(0.55, 0.45, 0.25), {
        emissiveFactor: 0.3,
      }),
      grip: shared('sw-grip', new B.Color3(0.3, 0.2, 0.1), {
        emissiveFactor: 0.2,
      }),
      pommel: shared('sw-pommel', new B.Color3(0.6, 0.5, 0.3), {
        emissiveFactor: 0.3,
      }),
    };
  } catch (error) {
    // Do not leave a half-created replacement set registered with a live
    // scene. The old cache remains untouched until all four creations finish.
    for (const material of created) material.dispose();
    throw error;
  }
}

/**
 * Return one complete material set owned by `scene`.
 *
 * Replacing the object only after all four materials are ready keeps the cache
 * atomic: callers can never observe a mixture of previous- and current-scene
 * materials.
 */
export function getSwordsmanMaterials(scene) {
  const stale = !cachedSwordMaterials ||
    MATERIAL_SLOTS.some(slot => materialIsStale(cachedSwordMaterials[slot], scene));

  if (stale) {
    const replacement = createSwordMaterials(scene);
    cachedSwordMaterials = replacement;
  }
  return cachedSwordMaterials;
}
