'use strict';

import {isEnabled, onSettingsChange} from '../settings.js';

// Small scene-owned, seamless surface maps. Neutral values preserve avatar
// colors and status flashes; the directional lighting supplies the relief.
const surfaces = new WeakMap();
const SIZE = 128;
const sceneMaterials = new WeakMap();

export function syncForgeSurface(material) {
  const texture = isEnabled('rendering', 'surfaceDetail') ? material._forgeSurfaceTexture : null;
  material.diffuseTexture = texture || null;
  material.emissiveTexture = texture || null;
}


export function forgeSurface(scene, finish) {
  const B = window.BABYLON;
  if (typeof B.DynamicTexture !== 'function') return null;
  let cache = surfaces.get(scene);
  if (!cache) { cache = new Map(); surfaces.set(scene, cache); }
  if (cache.has(finish)) return cache.get(finish);
  const texture = new B.DynamicTexture(`forge-surface-${finish}`, SIZE, scene, true);
  const ctx = texture.getContext();
  // Canvas is unavailable in some headless renderer hosts.
  if (typeof ctx?.createImageData !== 'function') {
    texture.dispose();
    cache.set(finish, null);
    return null;
  }
  const pixels = ctx.createImageData(SIZE, SIZE);
  for (let y = 0; y < SIZE; y++) {
    for (let x = 0; x < SIZE; x++) {
      const hash = ((x * 73 + y * 151 + x * y * 3) & 31) / 31;
      const grain = Math.sin(y * Math.PI / 2) * 5;
      const weave = ((Math.floor(x / 4) + Math.floor(y / 4)) & 1) ? 5 : -5;
      const value = finish === 'graphite'
        ? 226 + weave + hash * 8
        : finish === 'steel' ? 240 + grain + hash * 6 : 232 + grain + hash * 8;
      const i = (y * SIZE + x) * 4;
      pixels.data[i] = pixels.data[i + 1] = pixels.data[i + 2] = value;
      pixels.data[i + 3] = 255;
    }
  }
  ctx.putImageData(pixels, 0, 0);
  texture.wrapU = texture.wrapV = B.Texture?.WRAP_ADDRESSMODE ?? 1;
  texture.anisotropicFilteringLevel = 4;
  texture.update(false);
  cache.set(finish, texture);
  return texture;
}

export function applyForgeSurface(material, scene, finish) {
  material._forgeSurfaceTexture = forgeSurface(scene, finish);
  material.specularPower = finish === 'steel' ? 96 : finish === 'gunmetal' ? 48 : 24;
  syncForgeSurface(material);
  let materials = sceneMaterials.get(scene);
  if (!materials) {
    materials = new Set();
    sceneMaterials.set(scene, materials);
    if (scene.onDisposeObservable?.addOnce) {
      const unsubscribe = onSettingsChange(() => {
        for (const item of materials) {
          item.unfreeze();
          syncForgeSurface(item);
          item.freeze();
        }
      });
      scene.onDisposeObservable.addOnce(() => { unsubscribe(); materials.clear(); });
    }
  }
  materials.add(material);
}
