'use strict';

/**
 * Presentation-only bot cosmetics. Asset keys are mapped to fixed procedural
 * meshes/materials here; server-supplied strings are never treated as URLs,
 * code, stats, or arbitrary model paths.
 * @module renderer/cosmetics
 */

import { isEnabled } from '../settings.js';
import { makeMat, parseColor } from './utils.js';
import { applyForgeSurface, inheritForgeSurface } from './forge-surfaces.js';
import {beveledBox, profileHull} from './mech-geometry.js';
import {bodyFormForAsset} from './body-form-roster.js?v=20260714e';

const ALLOWED = {
  bot_skin: new Set(['standard', 'neon_grid', 'carbon_armor']),
  weapon_skin: new Set(['standard', 'solar_flare', 'void_edge']),
  attachment: new Set(['none', 'signal_antenna', 'orbital_halo']),
};

const DEFAULTS = {
  bot_skin: 'standard',
  weapon_skin: 'standard',
  attachment: 'none',
};

/** Resolve a legacy or procedural asset key without accepting URLs or paths. */
export function resolveCosmeticAsset(slot, value) {
  const fallback = DEFAULTS[slot] || 'standard';
  const bodyForm = slot === 'bot_skin' ? bodyFormForAsset(value) : null;
  if (bodyForm) return {kind: 'body-form', key: bodyForm.assetKey, bodyForm, theme: null};
  if (typeof value === 'string' && ALLOWED[slot]?.has(value)) {
    return {kind: 'legacy', key: value, theme: null};
  }
  const helper = typeof window !== 'undefined' ? window.ArenaCosmeticThemes : null;
  const theme = helper && typeof helper.themeFor === 'function' ? helper.themeFor(value) : null;
  if (theme) return {kind: 'procedural', key: theme.key, theme};
  return {kind: 'legacy', key: fallback, theme: null};
}

function desiredLoadout(bot) {
  const raw = bot && bot.cosmetics && typeof bot.cosmetics === 'object' ? bot.cosmetics : {};
  return {
    bot_skin: resolveCosmeticAsset('bot_skin', raw.bot_skin),
    weapon_skin: resolveCosmeticAsset('weapon_skin', raw.weapon_skin),
    attachment: resolveCosmeticAsset('attachment', raw.attachment),
  };
}

function createGroup(name, parent, scene) {
  const group = new window.BABYLON.TransformNode(name, scene);
  group.parent = parent;
  return group;
}

function finishMesh(mesh, group, material) {
  mesh.parent = group;
  mesh.material = material;
  mesh.isPickable = false;
  return mesh;
}

function cosmeticMaterial(state, name, scene, color, options = {}) {
  const material = makeMat(name, scene, color, options);
  material.freeze();
  state.materials.push(material);
  return material;
}

function isForgeEntry(entry) {
  return entry?.isForgeCharacter === true && entry.mounts && entry.mountMetrics;
}

function forgeMetric(entry, key, fallback) {
  const value = Number(entry.mountMetrics?.[key]);
  return Number.isFinite(value) && value > 0 ? value : fallback;
}

/**
 * Group under the per-silhouette head-top anchor (falls back to the raw head
 * mount for rigs that predate anchors). Attachment geometry is authored
 * relative to this anchor, so halos and crowns sit above a rabbit's ears, a
 * wizard's hat, or a slime's crown without per-form special cases here.
 */
function headAnchorGroup(state, entry, name, scene) {
  const anchor = entry.mounts?.headTop || entry.mounts?.head
    || entry.mounts?.cosmeticRoot || entry.root;
  const group = createGroup(name, anchor, scene);
  state.groups.push(group);
  return group;
}

function forgeGroup(state, entry, mount, name, scene) {
  const semanticMounts = {
    chest: entry.mounts?.chest,
    head: entry.mounts?.head,
    back: entry.mounts?.back,
    shoulderL: entry.mounts?.shoulderL,
    shoulderR: entry.mounts?.shoulderR,
  };
  const group = createGroup(
    name,
    semanticMounts[mount] || entry.mounts?.cosmeticRoot || entry.root,
    scene,
  );
  state.groups.push(group);
  return group;
}

// Hardware collections share a small vocabulary of machined shells, inset
// emitters and exposed graphite joints. Theme IDs remain the catalog authority.
function collectionMaterials(state, asset, bot, scene, slot) {
  const palette = asset.theme?.palette || {
    primary: asset.key === 'carbon_armor' ? '#34414d' : (bot.avatar_color || '#69bde0'),
    secondary: '#c5d5df', accent: bot.avatar_color || '#75d9ee', dark: '#17222c',
  };
  const material = (role, color, finish, emissiveFactor) => {
    const mat = cosmeticMaterial(state, `cosmetic-${slot}-${role}-${bot.bot_id}`, scene,
      parseColor(color), {emissiveFactor, specular: parseColor(palette.secondary)});
    mat.unfreeze();
    applyForgeSurface(mat, scene, finish);
    mat.freeze();
    return mat;
  };
  return {
    shell: material('shell', palette.primary, 'gunmetal', 0.08),
    edge: material('edge', palette.secondary, 'steel', 0.025),
    joint: material('joint', palette.dark || '#17222c', 'graphite', 0.035),
    light: material('light', palette.accent, 'steel', 0.72),
  };
}

function panel(name, group, scene, material, size, position, roll = 0) {
  const mesh = beveledBox(name, {
    width: size[0], height: size[1], depth: size[2],
    bevel: Math.min(...size) * 0.23,
  }, scene);
  mesh.position.set(...position);
  mesh.rotation.z = roll;
  return finishMesh(mesh, group, material);
}

function bladePanel(name, group, scene, material, width, height, depth, position, roll = 0) {
  const mesh = profileHull(name, [
    {y: -height * 0.5, width: width * 0.65, depth, bevel: depth * 0.15},
    {y: height * 0.22, width, depth, bevel: depth * 0.2},
    {y: height * 0.5, width: width * 0.24, depth: depth * 0.55, bevel: depth * 0.1},
  ], scene);
  mesh.position.set(...position);
  mesh.rotation.z = roll;
  return finishMesh(mesh, group, material);
}

// Legacy entries lack semantic anchors. Keep a single explicitly placed
// adapter instead of maintaining an unrelated second cosmetic renderer.
function collectionMount(state, entry, mount, name, scene) {
  if (isForgeEntry(entry)) return mount === 'headTop'
    ? headAnchorGroup(state, entry, name, scene)
    : forgeGroup(state, entry, mount, name, scene);
  const group = createGroup(name, entry.mounts?.cosmeticRoot || entry.root, scene);
  const positions = {headTop: [0, 20.5, 0], back: [0, 10.3, 4.6],
    shoulderL: [-6.2, 12.4, 0], shoulderR: [6.2, 12.4, 0], chest: [0, 0, 0]};
  group.position.set(...positions[mount]);
  state.groups.push(group);
  return group;
}

function buildBotSkin(state, asset, entry, bot, scene) {
  if (asset.key === 'standard' || asset.kind === 'body-form') return;
  const mats = collectionMaterials(state, asset, bot, scene, 'skin');
  const skin = asset.theme?.skin || {pattern: asset.key === 'carbon_armor' ? 'plates' : 'bands', layers: 2, angle: 0};
  const layers = Math.min(3, Math.max(1, Math.floor(Number(skin.layers) || 1)));
  const width = forgeMetric(entry, 'torsoWidth', 8.4);
  const height = forgeMetric(entry, 'torsoHeight', 10.5);
  const depth = forgeMetric(entry, 'torsoDepth', 7.5);
  const group = collectionMount(state, entry, 'chest', `cosmetic-skin-${bot.bot_id}`, scene);
  const y = 1.12 + height * 0.55;
  const z = -depth * 0.5 - 0.38;
  const piece = (name, mat, size, position, roll = 0) =>
    panel(`cosmetic-set-${name}-${bot.bot_id}`, group, scene, mat, size, position, roll);

  if (skin.pattern === 'plates') {
    // Split cuirass: center gap preserves the chassis identity and core.
    for (const side of [-1, 1]) {
      bladePanel(`cosmetic-set-cuirass-${bot.bot_id}-${side}`, group, scene, mats.shell,
        width * (0.3 + layers * 0.015), height * (0.49 + layers * 0.025), 0.64, [side * width * 0.25, y, z], side * -0.12);
      piece(`cuirass-seam-${side}`, mats.edge, [width * 0.24, 0.13, 0.1],
        [side * width * 0.26, y + height * 0.17, z - 0.34], side * -0.12);
      const shoulder = collectionMount(state, entry, side < 0 ? 'shoulderL' : 'shoulderR',
        `cosmetic-skin-${bot.bot_id}-shoulder-${side}`, scene);
      panel(`cosmetic-set-pauldron-${bot.bot_id}-${side}`, shoulder, scene, mats.joint,
        [width * 0.34, height * 0.14, depth * 1.08], [side * 0.12, -0.32, 0], side * 0.12);
      panel(`cosmetic-set-pauldron-cap-${bot.bot_id}-${side}`, shoulder, scene, mats.shell,
        [width * 0.36, height * 0.1, depth * 0.84], [side * 0.15, height * 0.025, 0], side * 0.12);
    }
    return;
  }
  if (skin.pattern === 'bands') {
    // Laminated radiator apron with recessed luminous channels.
    piece('radiator-frame', mats.joint, [width * 0.7, height * 0.6, 0.34], [0, y, z]);
    for (let i = 0; i < layers + 1; i++) {
      const py = y + (i - layers * 0.5) * height * 0.14;
      piece(`radiator-louver-${i}`, mats.shell, [width * (0.64 - i * 0.045), height * 0.105, 0.46], [0, py, z - 0.16]);
      piece(`radiator-channel-${i}`, mats.light, [width * 0.4, 0.09, 0.08], [0, py - height * 0.038, z - 0.41]);
    }
    return;
  }
  if (skin.pattern === 'chevrons') {
    // Swept harness with keyed coupler and independent upper chest blades.
    for (const side of [-1, 1]) {
      const roll = side * (0.38 + Math.abs(skin.angle || 0));
      piece(`harness-${side}`, mats.joint, [width * 0.2, height * 0.66, 0.34], [side * width * 0.22, y, z], roll);
      bladePanel(`cosmetic-set-harness-shell-${bot.bot_id}-${side}`, group, scene, mats.shell,
        width * 0.17, height * 0.58, 0.4, [side * width * 0.22, y + 0.14, z - 0.24], roll);
      piece(`harness-strip-${side}`, mats.light, [0.13, height * 0.31, 0.1], [side * width * 0.27, y + height * 0.09, z - 0.48], roll);
    }
    piece('harness-coupler', mats.edge, [width * 0.23, height * 0.13, 0.66], [0, y - height * 0.23, z - 0.14]);
    return;
  }
  // Contained power cell: dark socket, ceramic shroud and inset octagonal lens.
  piece('power-socket', mats.joint, [width * 0.55, height * 0.55, 0.46], [0, y, z]);
  piece('power-shroud', mats.edge, [width * 0.44, height * 0.44, 0.56], [0, y, z - 0.21], Math.PI / 4);
  piece('power-cell', mats.shell, [width * 0.32, height * 0.32, 0.64], [0, y, z - 0.46], Math.PI / 4);
  piece('power-lens', mats.light, [width * 0.16, height * 0.16, 0.12], [0, y, z - 0.84], Math.PI / 4);
  for (const side of [-1, 1]) piece(`power-contact-${side}`, mats.shell,
    [width * 0.14, height * (0.24 + layers * 0.05), 0.5], [side * width * 0.36, y, z]);
}

function buildAttachment(state, asset, entry, bot, scene) {
  if (asset.key === 'none') return;
  const mats = collectionMaterials(state, asset, bot, scene, 'attachment');
  const attachment = asset.theme?.attachment || {kind: asset.key === 'signal_antenna' ? 'antenna' : 'halo', variant: 0};
  const variant = Math.max(0, Math.min(3, Math.floor(Number(attachment.variant) || 0)));
  const hw = forgeMetric(entry, 'headWidth', 4.8);
  const hh = forgeMetric(entry, 'headHeight', 3.5);
  const tw = forgeMetric(entry, 'torsoWidth', 8);
  const th = forgeMetric(entry, 'torsoHeight', 9);
  const td = forgeMetric(entry, 'torsoDepth', 4);
  const mount = ['fins', 'reactor'].includes(attachment.kind) ? 'back' : 'headTop';
  const group = collectionMount(state, entry, mount, `cosmetic-attachment-${bot.bot_id}`, scene);
  const piece = (name, mat, size, position, roll = 0) =>
    panel(`cosmetic-set-${name}-${bot.bot_id}`, group, scene, mat, size, position, roll);
  const blade = (name, mat, w, h, d, position, roll = 0) =>
    bladePanel(`cosmetic-set-${name}-${bot.bot_id}`, group, scene, mat, w, h, d, position, roll);

  if (attachment.kind === 'halo') {
    // A segmented sensor crown, physically supported by rear risers.
    for (const side of [-1, 1]) piece(`halo-riser-${side}`, mats.joint,
      [hw * 0.1, hh * 0.52, hw * 0.14], [side * hw * 0.38, hh * 0.2, hw * 0.23]);
    const count = variant === 1 ? 8 : 6;
    const radius = hw * (variant === 2 ? 0.65 : 0.74);
    for (let i = 0; i < count; i++) {
      const angle = i * Math.PI * 2 / count;
      const panelMesh = piece(`halo-segment-${i}`, i % 3 === 0 ? mats.edge : mats.shell,
        [hw * 0.45, 0.34, hw * 0.16], [Math.sin(angle) * radius, hh * 0.48 + (variant === 3 ? Math.cos(angle) * 0.45 : 0), Math.cos(angle) * radius]);
      panelMesh.rotation.y = angle;
    }
    piece('halo-sensor', mats.light, [hw * 0.35, 0.12, 0.12], [0, hh * 0.48, -radius - hw * 0.09]);
    return;
  }
  if (attachment.kind === 'antenna') {
    // Recon arrays: single scanner, twin stalks, dish or swept broadcast blade.
    piece('scanner-foot', mats.joint, [hw * 0.56, 0.32, hw * 0.36], [0, 0.12, 0]);
    const sides = variant === 1 ? [-1, 1] : [0];
    for (const side of sides) {
      const x = side * hw * 0.27;
      const height = hh * (variant === 3 ? 1.05 : 0.8);
      blade(`scanner-mast-${side}`, mats.shell, hw * 0.14, height, hw * 0.16, [x, height * 0.5 + 0.2, 0], side * -0.12);
      piece(`scanner-head-${side}`, mats.edge, [hw * (variant === 2 ? 0.66 : 0.35), hh * 0.18, hw * 0.3], [x, height + 0.2, 0]);
      piece(`scanner-lens-${side}`, mats.light, [hw * 0.2, hh * 0.065, 0.1], [x, height + 0.2, -hw * 0.16]);
    }
    if (variant === 3) blade('scanner-sail', mats.edge, hw * 0.45, hh * 0.6, hw * 0.09, [hw * 0.2, hh * 0.65, 0.1], -0.25);
    return;
  }
  if (attachment.kind === 'crown') {
    piece('crest-foundation', mats.joint, [hw * 1.02, 0.32, hw * 0.48], [0, 0.13, 0]);
    const count = variant === 1 ? 5 : 3;
    for (let i = 0; i < count; i++) {
      const offset = i - (count - 1) / 2;
      const h = hh * (0.62 - Math.abs(offset) * 0.13 + (variant === 3 && offset === 0 ? 0.35 : 0));
      blade(`crest-vane-${i}`, offset === 0 ? mats.edge : mats.shell,
        hw * (variant === 2 ? 0.27 : 0.2), h, hw * 0.22, [offset * hw * 0.23, h * 0.5 + 0.24, 0], -offset * 0.12);
    }
    piece('crest-signal', mats.light, [hw * 0.48, 0.12, 0.1], [0, 0.32, -hw * 0.25]);
    return;
  }
  if (attachment.kind === 'orbitals') {
    // Gimballed navigation yoke. Open center leaves the face legible.
    const span = hw * (variant === 2 ? 0.88 : 0.72);
    for (const side of [-1, 1]) {
      piece(`nav-strut-${side}`, mats.joint, [hw * 0.14, hh * 0.7, hw * 0.2], [side * span, hh * 0.1, hw * 0.22], side * 0.18);
      piece(`nav-pod-${side}`, mats.shell, [hw * 0.35, hh * (variant === 3 ? 0.58 : 0.35), hw * 0.5], [side * span, hh * 0.43, 0]);
      piece(`nav-optic-${side}`, mats.light, [hw * 0.16, hh * 0.1, 0.12], [side * span, hh * 0.43, -hw * 0.27]);
    }
    piece('nav-bridge', mats.edge, [span * 2, hh * 0.12, hw * 0.18], [0, hh * 0.52, hw * 0.25]);
    if (variant === 1) piece('nav-upper-array', mats.shell, [hw * 0.74, hh * 0.22, hw * 0.4], [0, hh * 0.8, hw * 0.25]);
    return;
  }
  piece('pack-interface', mats.joint, [tw * 0.54, th * 0.51, td * 0.3], [0, 0, td * 0.16]);
  if (attachment.kind === 'fins') {
    // Folded heat-exchanger wings on articulated shoulder mounts, with a
    // central backpack spine on the independent back anchor.
    const shoulderY = Number.isFinite(entry.cosmeticAnchors?.shoulderY)
      ? entry.cosmeticAnchors.shoulderY : -th * 0.08;
    for (const side of [-1, 1]) {
      const shoulder = collectionMount(state, entry, side < 0 ? 'shoulderL' : 'shoulderR',
        `cosmetic-attachment-${bot.bot_id}-shoulder-${side}`, scene);
      const count = variant === 1 ? 2 : 1;
      for (let i = 0; i < count; i++) {
        const h = th * (variant === 2 ? 0.76 : 0.57);
        const x = side * tw * (0.08 + i * 0.16);
        bladePanel(`cosmetic-set-radiator-wing-${bot.bot_id}-${side}-${i}`, shoulder, scene, mats.shell,
          tw * 0.25, h, td * 0.22, [x, shoulderY, td * (0.33 + i * 0.1)],
          side * (variant === 3 ? -0.7 : -0.28));
        panel(`cosmetic-set-radiator-wing-inlay-${bot.bot_id}-${side}-${i}`, shoulder, scene, mats.edge,
          [tw * 0.11, h * 0.58, 0.12], [x, shoulderY, td * (0.46 + i * 0.1)],
          side * (variant === 3 ? -0.7 : -0.28));
      }
    }
    piece('radiator-pack-light', mats.light, [tw * 0.22, 0.14, 0.12], [0, th * 0.14, td * 0.34]);
    return;
  }
  // Rear equipment packs: containment vault, twin drives, radiator or heavy cell.
  const twin = variant === 1;
  for (const side of twin ? [-1, 1] : [0]) {
    const x = side * tw * 0.19;
    blade(`reactor-casing-${side}`, mats.shell, tw * (twin ? 0.3 : 0.48), th * (variant === 3 ? 0.67 : 0.49), td * 0.53,
      [x, -th * 0.025, td * 0.47]);
    piece(`reactor-window-${side}`, mats.joint, [tw * (twin ? 0.16 : 0.29), th * 0.28, 0.16], [x, 0, td * 0.77]);
    piece(`reactor-charge-${side}`, mats.light, [tw * 0.065, th * 0.2, 0.08], [x, 0, td * 0.88]);
    piece(`reactor-foot-${side}`, mats.edge, [tw * (twin ? 0.23 : 0.37), th * 0.09, td * 0.36], [x, -th * 0.27, td * 0.45]);
  }
  if (variant === 2) for (const side of [-1, 1]) blade(`reactor-cooling-${side}`, mats.edge,
    tw * 0.13, th * 0.63, td * 0.32, [side * tw * 0.36, 0, td * 0.45], side * -0.12);
}

function applyWeaponFinish(state, asset, entry, bot, scene) {
  const assetKey = asset.key;
  if (assetKey === 'standard' || !entry.weapon) return;
  const weaponMeshes = Array.isArray(entry.weapon._forgeMeshes)
    ? entry.weapon._forgeMeshes
    : typeof entry.weapon.getChildMeshes === 'function' ? entry.weapon.getChildMeshes(false) : [];
  if (weaponMeshes.length === 0) return;
  const B = window.BABYLON;
  const tint = asset.kind === 'procedural'
    ? parseColor(asset.theme.palette.primary)
    : assetKey === 'solar_flare'
      ? new B.Color3(1, 0.46, 0.06)
      : new B.Color3(0.32, 0.07, 0.58);
  const glow = asset.kind === 'procedural'
    ? parseColor(asset.theme.palette.accent)
    : tint;
  const specular = asset.kind === 'procedural'
    ? parseColor(asset.theme.palette.secondary)
    : assetKey === 'solar_flare'
      ? new B.Color3(1, 0.86, 0.5)
      : new B.Color3(0.52, 0.2, 0.8);
  const emissiveFactor = asset.kind === 'procedural' ? Math.min(0.28, asset.theme.weapon.emissive * 0.32) : 0.2;

  for (const mesh of weaponMeshes) {
    const original = mesh.material;
    if (!original || typeof original.clone !== 'function') continue;
    const clone = original.clone(`cosmetic-weapon-${assetKey}-${bot.bot_id}-${mesh.name}`);
    if (!clone || !clone.diffuseColor || !clone.emissiveColor) {
      if (clone) clone.dispose();
      continue;
    }
    if (typeof clone.unfreeze === 'function') clone.unfreeze();
    inheritForgeSurface(clone, original, scene);
    // Preserve dark mechanical joints and bright cutting edges under a finish.
    const base = original.diffuseColor;
    const brightness = base ? (base.r + base.g + base.b) / 3 : 0.5;
    clone.diffuseColor = tint.scale(0.38 + Math.min(0.62, brightness));
    clone.emissiveColor = glow.scale(emissiveFactor * (brightness < 0.16 ? 0.18 : 1));
    clone.specularColor = specular.clone();
    clone.freeze();
    mesh.material = clone;
    state.weaponSwaps.push({ mesh, original, clone });
  }
  buildWeaponGarnish(state, asset, entry, bot, scene, glow);
}

const GARNISH_BUILDERS = Object.freeze({
  ion(B, scene, name) {
    // Split induction fork: negative space keeps the weapon tip visible.
    return [-1, 1].map(side => {
      const fin = beveledBox(`${name}-induction-${side}`, {width: 0.22, height: 1.12, depth: 0.48, bevel: 0.06}, scene);
      fin.position.set(side * 0.62, -0.25, 0);
      fin.rotation.z = side * -0.16;
      return fin;
    });
  },
  ember(B, scene, name) {
    const vent = profileHull(`${name}-thermal-vane`, [
      {y: -0.8, width: 0.62, depth: 0.3, bevel: 0.07},
      {y: 0.1, width: 0.92, depth: 0.36, bevel: 0.07},
      {y: 0.7, width: 0.2, depth: 0.16, bevel: 0.03},
    ], scene);
    vent.position.z = 0.38;
    return [vent];
  },
  prism(B, scene, name) {
    return [-1, 1].map(side => {
      const crystal = profileHull(`${name}-ceramic-${side}`, [
        {y: -0.7, width: 0.18, depth: 0.2, bevel: 0.03},
        {y: 0.15, width: 0.4, depth: 0.36, bevel: 0.07},
        {y: 0.65, width: 0.08, depth: 0.1, bevel: 0.02},
      ], scene);
      crystal.position.x = side * 0.56;
      crystal.rotation.z = side * -0.3;
      return crystal;
    });
  },
  void(B, scene, name) {
    const collar = beveledBox(`${name}-containment`, {width: 1.25, height: 0.28, depth: 0.6, bevel: 0.09}, scene);
    collar.position.y = -0.45;
    const keel = beveledBox(`${name}-keel`, {width: 0.24, height: 0.8, depth: 0.2, bevel: 0.05}, scene);
    keel.position.set(0, -0.5, 0.44);
    return [collar, keel];
  },
});

/**
 * Small finish-specific garnish at each weapon tip so paid finishes read as
 * distinct hardware with a restrained luminous edge. Tips ride the weapon pose nodes, so the
 * garnish follows every swing/thrust for free.
 */
function buildWeaponGarnish(state, asset, entry, bot, scene, glowColor) {
  const tips = Array.isArray(entry.weapon?._trailTips)
    ? entry.weapon._trailTips.filter(tip => tip && typeof tip.getScene === 'function')
    : [];
  if (!tips.length || !scene) return;
  const finish = asset.kind === 'procedural'
    ? asset.theme.weapon.finish
    : asset.key === 'solar_flare' ? 'ember' : 'void';
  const builder = GARNISH_BUILDERS[finish] || GARNISH_BUILDERS.ion;
  const glow = cosmeticMaterial(state, `cosmetic-weapon-garnish-${bot.bot_id}`, scene, glowColor, {
    emissiveFactor: 0.55, specular: glowColor,
  });
  for (const [index, tip] of tips.slice(0, 2).entries()) {
    const group = createGroup(`cosmetic-weapon-garnish-${bot.bot_id}-${index}`, tip, scene);
    state.groups.push(group);
    for (const mesh of builder(window.BABYLON, scene, `cosmetic-garnish-${bot.bot_id}-${index}`)) {
      finishMesh(mesh, group, glow);
    }
  }
}

/** Remove cosmetic nodes and restore shared weapon materials. */
export function disposeBotCosmetics(entry) {
  const state = entry && entry._cosmeticState;
  if (!state) return;

  for (const swap of state.weaponSwaps) {
    if (swap.mesh && !swap.mesh.isDisposed() && swap.mesh.material === swap.clone) {
      swap.mesh.material = swap.original;
    }
    swap.clone.dispose();
  }
  for (const group of state.groups) {
    if (group && !group.isDisposed()) group.dispose();
  }
  for (const material of state.materials) material.dispose();
  entry._cosmeticState = null;
  entry._cosmeticSignature = '';
}

/** Apply or live-refresh a bot's allowlisted cosmetic loadout. */
export function applyBotCosmetics(entry, bot, scene, options = {}) {
  if (!entry || !bot) return;
  const loadout = desiredLoadout(bot);
  const enabled = options.forceEnabled === true
    ? {bot_skin: true, weapon_skin: true, attachment: true}
    : {
        bot_skin: isEnabled('botCosmetics', 'skins'),
        weapon_skin: isEnabled('botCosmetics', 'weaponFinishes'),
        attachment: isEnabled('botCosmetics', 'attachments'),
      };
  const signature = [
    loadout.bot_skin.key, enabled.bot_skin,
    loadout.weapon_skin.key, enabled.weapon_skin,
    loadout.attachment.key, enabled.attachment,
  ].join('|');
  if (entry._cosmeticSignature === signature) return;

  disposeBotCosmetics(entry);
  const state = { groups: [], materials: [], weaponSwaps: [] };
  entry._cosmeticState = state;
  entry._cosmeticSignature = signature;

  if (enabled.bot_skin) buildBotSkin(state, loadout.bot_skin, entry, bot, scene);
  if (enabled.weapon_skin) applyWeaponFinish(state, loadout.weapon_skin, entry, bot, scene);
  if (enabled.attachment) buildAttachment(state, loadout.attachment, entry, bot, scene);
  if (typeof entry.setLOD === 'function') entry.setLOD();
}
