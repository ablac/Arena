'use strict';

/**
 * Small world-space HUD planes for bot names, health, and taunts.
 *
 * Unlike Babylon fullscreen GUI controls linked to moving meshes, these
 * planes do not invalidate and upload a viewport-sized texture every frame.
 * Name textures are drawn once, taunts only when their text changes, and HP
 * changes are represented by scaling a plain colored quad.
 * @module renderer/world-hud
 */

const BASE_CAMERA_RADIUS = 800;
const HUD_RENDERING_GROUP = 3;
const HP_WIDTH = 38;
const _sceneResources = new WeakMap();

export function worldHudScaleForRadius(radius) {
  const value = Number(radius);
  if (!Number.isFinite(value) || value <= 0) return 1;
  return Math.max(0.15, Math.min(2.25, value / BASE_CAMERA_RADIUS));
}

export function healthBandForRatio(ratio) {
  if (ratio > 0.6) return 'healthy';
  if (ratio > 0.3) return 'wounded';
  return 'critical';
}

function makeFlatMaterial(B, name, scene, color, alpha = 1) {
  const material = new B.StandardMaterial(name, scene);
  material.diffuseColor = color;
  material.emissiveColor = color;
  material.specularColor = B.Color3.Black();
  material.disableLighting = true;
  material.disableDepthWrite = true;
  material.backFaceCulling = false;
  material.alpha = alpha;
  material.freeze();
  return material;
}

function getSceneResources(scene) {
  let resources = _sceneResources.get(scene);
  if (resources) return resources;
  const B = window.BABYLON;
  resources = {
    background: makeFlatMaterial(B, 'world-hud-hp-bg', scene, new B.Color3(0.025, 0.035, 0.05), 0.88),
    healthy: makeFlatMaterial(B, 'world-hud-hp-ok', scene, new B.Color3(0.08, 1, 0.28)),
    wounded: makeFlatMaterial(B, 'world-hud-hp-warn', scene, new B.Color3(1, 0.78, 0.08)),
    critical: makeFlatMaterial(B, 'world-hud-hp-critical', scene, new B.Color3(1, 0.12, 0.08)),
  };
  _sceneResources.set(scene, resources);

  // Render the HUD after the arena and clear depth for this small overlay
  // group. The planes stay readable like the old fullscreen GUI, while their
  // picking remains disabled and they never modify the arena depth buffer.
  if (typeof scene.setRenderingAutoClearDepthStencil === 'function') {
    scene.setRenderingAutoClearDepthStencil(HUD_RENDERING_GROUP, true, true, false);
  }
  return resources;
}

function configurePlane(mesh) {
  mesh.renderingGroupId = HUD_RENDERING_GROUP;
  mesh.isPickable = false;
  mesh.alwaysSelectAsActiveMesh = true;
  return mesh;
}

function drawLabelTexture(B, scene, name, text, kind = 'name') {
  const size = kind === 'taunt' ? {width: 512, height: 96} : {width: 256, height: 64};
  const texture = new B.DynamicTexture(name, size, scene, false);
  texture.hasAlpha = true;
  const ctx = texture.getContext();
  ctx.clearRect(0, 0, size.width, size.height);
  ctx.textAlign = 'center';
  ctx.textBaseline = 'middle';
  ctx.font = kind === 'taunt' ? '600 34px monospace' : '700 34px monospace';
  if (kind === 'taunt') {
    ctx.fillStyle = 'rgba(8, 12, 20, 0.9)';
    ctx.strokeStyle = 'rgba(255, 215, 90, 0.95)';
    ctx.lineWidth = 3;
    ctx.fillRect(3, 3, size.width - 6, size.height - 6);
    ctx.strokeRect(3, 3, size.width - 6, size.height - 6);
    ctx.fillStyle = '#ffd75a';
  } else {
    ctx.shadowColor = 'rgba(0, 0, 0, 0.95)';
    ctx.shadowBlur = 8;
    ctx.lineWidth = 7;
    ctx.strokeStyle = 'rgba(0, 0, 0, 0.9)';
    ctx.strokeText(text, size.width / 2, size.height / 2);
    ctx.fillStyle = '#ffffff';
  }
  ctx.fillText(text, size.width / 2, size.height / 2);
  texture.update(false);
  return texture;
}

function drawSelectionTexture(texture, lines) {
  const ctx = texture.getContext();
  const size = texture.getSize();
  ctx.clearRect(0, 0, size.width, size.height);
  ctx.fillStyle = 'rgba(8, 12, 20, 0.92)';
  ctx.strokeStyle = 'rgba(138, 223, 255, 0.95)';
  ctx.lineWidth = 4;
  ctx.fillRect(4, 4, size.width - 8, size.height - 8);
  ctx.strokeRect(4, 4, size.width - 8, size.height - 8);
  ctx.textAlign = 'left';
  ctx.textBaseline = 'middle';
  ctx.font = '600 30px monospace';
  for (let index = 0; index < lines.length; index += 1) {
    ctx.fillStyle = index === 0 ? '#8adfff' : '#ffffff';
    ctx.fillText(lines[index], 24, 34 + index * 44, size.width - 48);
  }
  texture.update(false);
}

function makeTextureMaterial(B, scene, name, texture) {
  const material = new B.StandardMaterial(name, scene);
  material.diffuseColor = B.Color3.White();
  material.emissiveColor = B.Color3.White();
  material.specularColor = B.Color3.Black();
  material.diffuseTexture = texture;
  material.emissiveTexture = texture;
  material.useAlphaFromDiffuseTexture = true;
  material.transparencyMode = B.Material.MATERIAL_ALPHABLEND;
  material.disableLighting = true;
  material.disableDepthWrite = true;
  material.backFaceCulling = false;
  return material;
}

export function createWorldBotHud(bot, id, root, scene) {
  const B = window.BABYLON;
  // Lightweight renderer unit doubles intentionally omit texture/plane
  // constructors. Production Babylon always provides both; keeping this
  // boundary nullable lets character logic remain independently testable.
  if (typeof B?.DynamicTexture !== 'function' || typeof B?.MeshBuilder?.CreatePlane !== 'function') {
    return null;
  }
  const resources = getSceneResources(scene);
  const anchor = new B.TransformNode(`world-hud-${id}`, scene);
  anchor.parent = root;
  anchor.position.y = 25;
  anchor.billboardMode = B.TransformNode.BILLBOARDMODE_ALL ?? B.Mesh.BILLBOARDMODE_ALL;

  const fullName = String(bot?.name || '???');
  const displayName = fullName.length > 12 ? `${fullName.slice(0, 11)}\u2026` : fullName;
  const nameTexture = drawLabelTexture(B, scene, `world-hud-name-tex-${id}`, displayName);
  const nameMaterial = makeTextureMaterial(B, scene, `world-hud-name-mat-${id}`, nameTexture);
  const nameLabel = configurePlane(B.MeshBuilder.CreatePlane(`world-hud-name-${id}`, {
    width: Math.max(34, Math.min(72, displayName.length * 6.4)),
    height: 14,
  }, scene));
  nameLabel.parent = anchor;
  nameLabel.position.y = 14;
  // TransformNode billboarding presents textured child planes vertically
  // inverted in this ArcRotate convention. Flip local Y once; the symmetric
  // HP quads do not need the extra transform.
  nameLabel.scaling.y = -1;
  nameLabel.material = nameMaterial;

  const hpContainer = configurePlane(B.MeshBuilder.CreatePlane(`world-hud-hp-bg-${id}`, {
    width: 40,
    height: 3.6,
  }, scene));
  hpContainer.parent = anchor;
  hpContainer.position.y = 5;
  hpContainer.material = resources.background;

  const hpFill = configurePlane(B.MeshBuilder.CreatePlane(`world-hud-hp-fill-${id}`, {
    width: HP_WIDTH,
    height: 2.4,
  }, scene));
  hpFill.parent = anchor;
  hpFill.position.y = 5;
  hpFill.position.z = -0.04;
  hpFill.material = resources.healthy;

  const hud = {
    root: anchor,
    nameLabel,
    nameTexture,
    nameMaterial,
    hpContainer,
    hpFill,
    resources,
    tauntBubble: null,
    tauntTexture: null,
    tauntMaterial: null,
    selectionCard: null,
    selectionTexture: null,
    selectionMaterial: null,
    selectionText: '',
  };
  updateWorldBotHudHealth(hud, 1);
  return hud;
}

export function setWorldBotHudVisible(hud, visible) {
  if (hud?.root) hud.root.setEnabled(Boolean(visible));
}

export function updateWorldBotHudHealth(hud, ratio) {
  if (!hud?.hpFill) return;
  const clamped = Math.max(0.01, Math.min(1, Number(ratio) || 0));
  hud.hpFill.scaling.x = clamped;
  hud.hpFill.position.x = -(HP_WIDTH * (1 - clamped)) / 2;
  hud.hpFill.material = hud.resources[healthBandForRatio(clamped)];
}

export function scaleWorldBotHud(hud, cameraRadius) {
  if (hud?.root) hud.root.scaling.setAll(worldHudScaleForRadius(cameraRadius));
}

export function showWorldTaunt(hud, text) {
  if (!hud?.root) return;
  const B = window.BABYLON;
  const scene = hud.root.getScene();
  hideWorldTaunt(hud, true);
  const clean = String(text).slice(0, 28);
  const texture = drawLabelTexture(B, scene, `${hud.root.name}-taunt-tex`, clean, 'taunt');
  const material = makeTextureMaterial(B, scene, `${hud.root.name}-taunt-mat`, texture);
  const plane = configurePlane(B.MeshBuilder.CreatePlane(`${hud.root.name}-taunt`, {
    width: Math.max(56, Math.min(120, clean.length * 5)),
    height: 20,
  }, scene));
  plane.parent = hud.root;
  plane.position.y = 32;
  plane.scaling.y = -1;
  plane.material = material;
  hud.tauntBubble = plane;
  hud.tauntTexture = texture;
  hud.tauntMaterial = material;
}

export function hideWorldTaunt(hud, dispose = false) {
  if (!hud?.tauntBubble) return;
  if (!dispose) {
    hud.tauntBubble.isVisible = false;
    return;
  }
  hud.tauntBubble.dispose();
  hud.tauntMaterial?.dispose(false, false);
  hud.tauntTexture?.dispose();
  hud.tauntBubble = null;
  hud.tauntMaterial = null;
  hud.tauntTexture = null;
}

/** Show selected-bot details without allocating a fullscreen GUI texture. */
export function showWorldSelection(hud, lines) {
  if (!hud?.root) return;
  const B = window.BABYLON;
  const scene = hud.root.getScene();
  const cleanLines = (lines || []).slice(0, 5).map(line => String(line).slice(0, 34));
  const text = cleanLines.join('\n');
  if (!hud.selectionCard) {
    const texture = new B.DynamicTexture(`${hud.root.name}-selection-tex`, {
      width: 512,
      height: 256,
    }, scene, false);
    texture.hasAlpha = true;
    const material = makeTextureMaterial(B, scene, `${hud.root.name}-selection-mat`, texture);
    const plane = configurePlane(B.MeshBuilder.CreatePlane(`${hud.root.name}-selection`, {
      width: 76,
      height: 38,
    }, scene));
    plane.parent = hud.root;
    plane.position.y = 54;
    plane.scaling.y = -1;
    plane.material = material;
    hud.selectionCard = plane;
    hud.selectionTexture = texture;
    hud.selectionMaterial = material;
  }
  if (text !== hud.selectionText) {
    drawSelectionTexture(hud.selectionTexture, cleanLines);
    hud.selectionText = text;
  }
  hud.selectionCard.isVisible = true;
}

export function hideWorldSelection(hud) {
  if (hud?.selectionCard) hud.selectionCard.isVisible = false;
}

export function disposeWorldBotHud(hud) {
  if (!hud) return;
  hideWorldTaunt(hud, true);
  hud.hpFill?.dispose();
  hud.hpContainer?.dispose();
  hud.nameLabel?.dispose();
  hud.nameMaterial?.dispose(false, false);
  hud.nameTexture?.dispose();
  hud.selectionCard?.dispose();
  hud.selectionMaterial?.dispose(false, false);
  hud.selectionTexture?.dispose();
  hud.root?.dispose();
}
