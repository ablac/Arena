'use strict';

/**
 * Shared utilities for renderer modules.
 * @module renderer/utils
 */

/**
 * Parse hex color string to BABYLON.Color3.
 * @param {string} hex - e.g. "#ff3300"
 * @returns {BABYLON.Color3}
 */
export function parseColor(hex) {
  const BABYLON = window.BABYLON;
  if (!hex || typeof hex !== 'string' || hex.length < 7) {
    return new BABYLON.Color3(0.5, 0.5, 0.5);
  }
  const r = parseInt(hex.slice(1, 3), 16) / 255;
  const g = parseInt(hex.slice(3, 5), 16) / 255;
  const b = parseInt(hex.slice(5, 7), 16) / 255;
  if (isNaN(r) || isNaN(g) || isNaN(b)) {
    return new BABYLON.Color3(0.5, 0.5, 0.5);
  }
  return new BABYLON.Color3(r, g, b);
}

/**
 * Create a StandardMaterial with common settings.
 * @param {string} name
 * @param {BABYLON.Scene} scene
 * @param {BABYLON.Color3} color
 * @param {Object} [opts]
 * @returns {BABYLON.StandardMaterial}
 */
export function makeMat(name, scene, color, opts = {}) {
  const BABYLON = window.BABYLON;
  const mat = new BABYLON.StandardMaterial(name, scene);
  mat.diffuseColor = color.clone();
  if (opts.emissive !== false) {
    mat.emissiveColor = opts.emissiveFactor
      ? color.scale(opts.emissiveFactor)
      : color.scale(0.4);
  }
  if (opts.noLight) mat.disableLighting = true;
  if (opts.alpha != null) mat.alpha = opts.alpha;
  mat.specularColor = opts.specular || BABYLON.Color3.Black();
  mat.backFaceCulling = opts.backFace !== undefined ? opts.backFace : false;
  return mat;
}

/**
 * Create a DynamicTexture text plane (for name labels).
 * @param {string} id
 * @param {string} text
 * @param {BABYLON.Color3} color
 * @param {BABYLON.Scene} scene
 * @param {number} width
 * @param {number} height
 * @returns {{plane: BABYLON.Mesh, mat: BABYLON.StandardMaterial, tex: BABYLON.DynamicTexture}}
 */
export function createTextPlane(id, text, color, scene, width, height) {
  const BABYLON = window.BABYLON;
  const plane = BABYLON.MeshBuilder.CreatePlane(`txt-${id}`, {
    width, height
  }, scene);
  plane.rotation.x = Math.PI / 2;
  plane.position.y = 2;

  const tex = new BABYLON.DynamicTexture(`dtex-${id}`, { width: 256, height: 64 }, scene, false);
  const ctx = tex.getContext();
  ctx.clearRect(0, 0, 256, 64);
  ctx.font = 'bold 28px monospace';
  ctx.textAlign = 'center';
  ctx.textBaseline = 'middle';
  const r = Math.round(color.r * 255);
  const g = Math.round(color.g * 255);
  const b = Math.round(color.b * 255);
  ctx.fillStyle = `rgb(${r},${g},${b})`;
  const display = text.length > 12 ? text.slice(0, 11) + '\u2026' : text;
  ctx.fillText(display, 128, 32);
  tex.update();
  tex.hasAlpha = true;

  const mat = new BABYLON.StandardMaterial(`tmat-${id}`, scene);
  mat.diffuseTexture = tex;
  mat.emissiveTexture = tex;
  mat.disableLighting = true;
  mat.backFaceCulling = false;
  mat.useAlphaFromDiffuseTexture = true;
  mat.hasAlpha = true;
  mat.alpha = 0.95;
  plane.material = mat;

  return { plane, mat, tex };
}
