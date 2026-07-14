'use strict';

/**
 * Presentation-only bot cosmetics. Asset keys are mapped to fixed procedural
 * meshes/materials here; server-supplied strings are never treated as URLs,
 * code, stats, or arbitrary model paths.
 * @module renderer/cosmetics
 */

import { isEnabled } from '../settings.js';
import { makeMat, parseColor } from './utils.js';
import {bodyFormForAsset} from './body-form-roster.js?v=20260714d';

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

function buildForgeProceduralSkin(state, asset, entry, bot, scene) {
  const B = window.BABYLON;
  const {palette, skin} = asset.theme;
  const primary = cosmeticMaterial(state, `cosmetic-set-skin-primary-${bot.bot_id}`, scene, parseColor(palette.primary), {
    emissiveFactor: 0.5, specular: parseColor(palette.secondary),
  });
  const accent = cosmeticMaterial(state, `cosmetic-set-skin-accent-${bot.bot_id}`, scene, parseColor(palette.accent), {
    emissiveFactor: 0.9, noLight: true,
  });
  const layers = Math.min(3, Math.max(1, Number(skin.layers) || 1));
  const torsoWidth = forgeMetric(entry, 'torsoWidth', 7);
  const torsoHeight = forgeMetric(entry, 'torsoHeight', 8.15);
  const torsoDepth = forgeMetric(entry, 'torsoDepth', 3.75);

  if (skin.pattern === 'bands') {
    const group = forgeGroup(state, entry, 'chest', `cosmetic-skin-${bot.bot_id}`, scene);
    for (let index = 0; index < layers; index += 1) {
      const ring = B.MeshBuilder.CreateTorus(`cosmetic-set-band-${bot.bot_id}-${index}`, {
        diameter: torsoWidth * (0.82 + index * 0.08),
        thickness: Math.max(0.22, torsoWidth * 0.045), tessellation: 16,
      }, scene);
      ring.position.y = 1.12 + torsoHeight * (0.27 + index * 0.22);
      ring.rotation.z = skin.angle;
      finishMesh(ring, group, index === layers - 1 ? accent : primary);
    }
    return;
  }

  if (skin.pattern === 'plates') {
    for (const side of [-1, 1]) {
      const mount = side < 0 ? 'shoulderL' : 'shoulderR';
      const group = forgeGroup(state, entry, mount, `cosmetic-skin-${bot.bot_id}-${mount}`, scene);
      const plate = B.MeshBuilder.CreateBox(`cosmetic-set-plate-${bot.bot_id}-${side}`, {
        width: Math.max(2.35, torsoWidth * (0.29 + layers * 0.025)),
        height: Math.max(1.25, torsoHeight * (0.14 + layers * 0.012)),
        depth: torsoDepth * 1.06,
      }, scene);
      plate.position.set(side * 0.2, -0.42, 0);
      plate.rotation.z = side * (0.14 + Math.abs(skin.angle));
      finishMesh(plate, group, side > 0 ? accent : primary);
    }
    return;
  }

  const group = forgeGroup(state, entry, 'chest', `cosmetic-skin-${bot.bot_id}`, scene);
  const chestY = 1.12 + torsoHeight * 0.54;
  const chestZ = -(torsoDepth * 0.5 + 0.32);
  if (skin.pattern === 'chevrons') {
    for (const side of [-1, 1]) {
      const chevron = B.MeshBuilder.CreateBox(`cosmetic-set-chevron-${bot.bot_id}-${side}`, {
        width: Math.max(0.72, torsoWidth * 0.13),
        height: torsoHeight * (0.48 + layers * 0.045),
        depth: 0.48,
      }, scene);
      chevron.position.set(side * torsoWidth * 0.23, chestY, chestZ);
      chevron.rotation.z = side * (0.48 + skin.angle);
      finishMesh(chevron, group, side > 0 ? accent : primary);
    }
    return;
  }

  const coreDiameter = Math.max(1.65, torsoWidth * (0.23 + layers * 0.025));
  const core = B.MeshBuilder.CreateSphere(`cosmetic-set-core-${bot.bot_id}`, {
    diameter: coreDiameter, segments: 8,
  }, scene);
  core.position.set(0, chestY, chestZ);
  finishMesh(core, group, accent);
  const bezel = B.MeshBuilder.CreateTorus(`cosmetic-set-core-bezel-${bot.bot_id}`, {
    diameter: coreDiameter * 1.45, thickness: Math.max(0.22, coreDiameter * 0.11), tessellation: 16,
  }, scene);
  bezel.position.set(0, chestY, chestZ - 0.04);
  bezel.rotation.x = Math.PI / 2;
  finishMesh(bezel, group, primary);
}

function buildForgeBotSkin(state, asset, entry, bot, scene) {
  const B = window.BABYLON;
  const assetKey = asset.key;
  if (asset.kind === 'procedural') {
    buildForgeProceduralSkin(state, asset, entry, bot, scene);
    return;
  }

  const torsoWidth = forgeMetric(entry, 'torsoWidth', 7);
  const torsoHeight = forgeMetric(entry, 'torsoHeight', 8.15);
  const torsoDepth = forgeMetric(entry, 'torsoDepth', 3.75);
  if (assetKey === 'neon_grid') {
    const group = forgeGroup(state, entry, 'chest', `cosmetic-skin-${bot.bot_id}`, scene);
    const neon = cosmeticMaterial(state, `cosmetic-neon-${bot.bot_id}`, scene, parseColor(bot.avatar_color), {
      emissiveFactor: 1, noLight: true,
    });
    for (const [index, fraction] of [0.28, 0.72].entries()) {
      const ring = B.MeshBuilder.CreateTorus(`cosmetic-neon-ring-${bot.bot_id}-${index}`, {
        diameter: torsoWidth * (index === 0 ? 0.96 : 0.82),
        thickness: Math.max(0.25, torsoWidth * 0.05), tessellation: 18,
      }, scene);
      ring.position.y = 1.12 + torsoHeight * fraction;
      finishMesh(ring, group, neon);
    }
    return;
  }

  if (assetKey === 'carbon_armor') {
    const carbon = cosmeticMaterial(state, `cosmetic-carbon-${bot.bot_id}`, scene,
      new B.Color3(0.045, 0.055, 0.075), {
        emissiveFactor: 0.08, specular: new B.Color3(0.42, 0.48, 0.55),
      });
    const chestGroup = forgeGroup(state, entry, 'chest', `cosmetic-skin-${bot.bot_id}-chest`, scene);
    const chestDepth = Math.max(0.74, torsoDepth * 0.28);
    const chest = B.MeshBuilder.CreateBox(`cosmetic-carbon-chest-${bot.bot_id}`, {
      width: torsoWidth * 0.88, height: torsoHeight * 0.58, depth: chestDepth,
    }, scene);
    chest.position.set(0, 1.12 + torsoHeight * 0.55, -(torsoDepth * 0.5 + chestDepth * 0.38));
    finishMesh(chest, chestGroup, carbon);
    for (const side of [-1, 1]) {
      const mount = side < 0 ? 'shoulderL' : 'shoulderR';
      const shoulderGroup = forgeGroup(state, entry, mount, `cosmetic-skin-${bot.bot_id}-${mount}`, scene);
      const plate = B.MeshBuilder.CreateBox(`cosmetic-carbon-shoulder-${bot.bot_id}-${side}`, {
        width: Math.max(2.5, torsoWidth * 0.36),
        height: Math.max(1.35, torsoHeight * 0.18), depth: torsoDepth * 1.1,
      }, scene);
      plate.position.set(side * 0.18, -0.42, 0);
      plate.rotation.z = side * 0.2;
      finishMesh(plate, shoulderGroup, carbon);
    }
  }
}

function buildProceduralSkin(state, asset, group, bot, scene) {
  const B = window.BABYLON;
  const {palette, skin} = asset.theme;
  const primary = cosmeticMaterial(state, `cosmetic-set-skin-primary-${bot.bot_id}`, scene, parseColor(palette.primary), {
    emissiveFactor: 0.5, specular: parseColor(palette.secondary),
  });
  const accent = cosmeticMaterial(state, `cosmetic-set-skin-accent-${bot.bot_id}`, scene, parseColor(palette.accent), {
    emissiveFactor: 0.9, noLight: true,
  });
  const layers = Math.min(3, Math.max(1, Number(skin.layers) || 1));

  if (skin.pattern === 'bands') {
    for (let index = 0; index < layers; index += 1) {
      const ring = B.MeshBuilder.CreateTorus(`cosmetic-set-band-${bot.bot_id}-${index}`, {
        diameter: 8.8 + index * 1.1, thickness: 0.34, tessellation: 16,
      }, scene);
      ring.position.y = 5.4 + index * 3.8;
      ring.rotation.z = skin.angle;
      finishMesh(ring, group, index === layers - 1 ? accent : primary);
    }
    return;
  }

  if (skin.pattern === 'plates') {
    for (const side of [-1, 1]) {
      const plate = B.MeshBuilder.CreateBox(`cosmetic-set-plate-${bot.bot_id}-${side}`, {
        width: 3.1 + layers * 0.35, height: 1.5 + layers * 0.2, depth: 4.1,
      }, scene);
      plate.position.set(side * 6.25, 12.3, 0);
      plate.rotation.z = side * (0.14 + Math.abs(skin.angle));
      finishMesh(plate, group, side > 0 ? accent : primary);
    }
    return;
  }

  if (skin.pattern === 'chevrons') {
    for (const side of [-1, 1]) {
      const chevron = B.MeshBuilder.CreateBox(`cosmetic-set-chevron-${bot.bot_id}-${side}`, {
        width: 1.1, height: 5.8 + layers * 0.7, depth: 1.05,
      }, scene);
      chevron.position.set(side * 3.25, 8.8, -4.5);
      chevron.rotation.z = side * (0.48 + skin.angle);
      finishMesh(chevron, group, side > 0 ? accent : primary);
    }
    return;
  }

  const core = B.MeshBuilder.CreateSphere(`cosmetic-set-core-${bot.bot_id}`, {
    diameter: 2.4 + layers * 0.38, segments: 8,
  }, scene);
  core.position.set(0, 9.1, -4.9);
  finishMesh(core, group, accent);
  const bezel = B.MeshBuilder.CreateTorus(`cosmetic-set-core-bezel-${bot.bot_id}`, {
    diameter: 4.2 + layers * 0.25, thickness: 0.34, tessellation: 16,
  }, scene);
  bezel.position.set(0, 9.1, -4.95);
  bezel.rotation.x = Math.PI / 2;
  finishMesh(bezel, group, primary);
}

function buildBotSkin(state, asset, entry, bot, scene) {
  const assetKey = asset.key;
  // Full-body geometry is constructed on the shared articulated skeleton and
  // owns its far proxy. It is intentionally not an overlay cosmetic group.
  if (assetKey === 'standard' || asset.kind === 'body-form') return;
  if (isForgeEntry(entry)) {
    buildForgeBotSkin(state, asset, entry, bot, scene);
    return;
  }
  const B = window.BABYLON;
  const color = parseColor(bot.avatar_color);
  const group = createGroup(
    `cosmetic-skin-${bot.bot_id}`,
    entry.mounts?.cosmeticRoot || entry.root,
    scene,
  );
  state.groups.push(group);

  if (asset.kind === 'procedural') {
    buildProceduralSkin(state, asset, group, bot, scene);
    return;
  }

  if (assetKey === 'neon_grid') {
    const neon = cosmeticMaterial(state, `cosmetic-neon-${bot.bot_id}`, scene, color, {
      emissiveFactor: 1, noLight: true,
    });
    for (const [index, y] of [5.2, 10.4].entries()) {
      const ring = B.MeshBuilder.CreateTorus(`cosmetic-neon-ring-${bot.bot_id}-${index}`, {
        diameter: index === 0 ? 10.8 : 8.6, thickness: 0.42, tessellation: 18,
      }, scene);
      ring.position.y = y;
      finishMesh(ring, group, neon);
    }
    return;
  }

  if (assetKey === 'carbon_armor') {
    const carbon = cosmeticMaterial(state, `cosmetic-carbon-${bot.bot_id}`, scene,
      new B.Color3(0.045, 0.055, 0.075), {
        emissiveFactor: 0.08, specular: new B.Color3(0.42, 0.48, 0.55),
      });
    const chest = B.MeshBuilder.CreateBox(`cosmetic-carbon-chest-${bot.bot_id}`, {
      width: 8.4, height: 5.2, depth: 1.4,
    }, scene);
    chest.position.set(0, 8.2, -4.3);
    finishMesh(chest, group, carbon);
    for (const side of [-1, 1]) {
      const plate = B.MeshBuilder.CreateBox(`cosmetic-carbon-shoulder-${bot.bot_id}-${side}`, {
        width: 3.4, height: 1.8, depth: 4.6,
      }, scene);
      plate.position.set(side * 6.4, 12.4, 0);
      plate.rotation.z = side * 0.2;
      finishMesh(plate, group, carbon);
    }
  }
}

function buildProceduralAttachment(state, asset, group, bot, scene) {
  const B = window.BABYLON;
  const {palette, attachment} = asset.theme;
  const primary = cosmeticMaterial(state, `cosmetic-set-attachment-primary-${bot.bot_id}`, scene, parseColor(palette.primary), {
    emissiveFactor: 0.55, specular: parseColor(palette.secondary),
  });
  const accent = cosmeticMaterial(state, `cosmetic-set-attachment-accent-${bot.bot_id}`, scene, parseColor(palette.accent), {
    emissiveFactor: 1, noLight: true, alpha: 0.92,
  });
  const variant = Math.max(0, Math.min(3, Number(attachment.variant) || 0));

  if (attachment.kind === 'halo') {
    const halo = B.MeshBuilder.CreateTorus(`cosmetic-set-halo-${bot.bot_id}`, {
      diameter: 9.2 + variant * 0.65, thickness: 0.4, tessellation: 18,
    }, scene);
    halo.position.y = 23.2 + variant * 0.25;
    halo.rotation.x = variant * 0.08;
    finishMesh(halo, group, accent);
    return;
  }

  if (attachment.kind === 'antenna') {
    const mast = B.MeshBuilder.CreateCylinder(`cosmetic-set-mast-${bot.bot_id}`, {
      height: 4.8 + variant * 0.45, diameter: 0.5, tessellation: 8,
    }, scene);
    mast.position.y = 21.5;
    finishMesh(mast, group, primary);
    const beacon = B.MeshBuilder.CreateSphere(`cosmetic-set-beacon-${bot.bot_id}`, {
      diameter: 1.5 + variant * 0.18, segments: 8,
    }, scene);
    beacon.position.y = 24.2 + variant * 0.35;
    finishMesh(beacon, group, accent);
    return;
  }

  if (attachment.kind === 'crown') {
    for (const offset of [-1, 0, 1]) {
      const point = B.MeshBuilder.CreateCylinder(`cosmetic-set-crown-${bot.bot_id}-${offset}`, {
        height: 2.6 + (offset === 0 ? 0.8 : 0) + variant * 0.2,
        diameterTop: 0, diameterBottom: 1.25, tessellation: 6,
      }, scene);
      point.position.set(offset * 1.55, 22.4 + (offset === 0 ? 0.35 : 0), 0);
      finishMesh(point, group, offset === 0 ? accent : primary);
    }
    return;
  }

  if (attachment.kind === 'orbitals') {
    for (const axis of [0, 1]) {
      const orbit = B.MeshBuilder.CreateTorus(`cosmetic-set-orbit-${bot.bot_id}-${axis}`, {
        diameter: 9 + axis * 1.5 + variant * 0.3, thickness: 0.3, tessellation: 16,
      }, scene);
      orbit.position.y = 20.6;
      orbit.rotation.x = axis ? Math.PI / 2.8 : Math.PI / 2;
      orbit.rotation.z = axis ? 0.45 : -0.35;
      finishMesh(orbit, group, axis ? accent : primary);
    }
    return;
  }

  if (attachment.kind === 'fins') {
    for (const side of [-1, 1]) {
      const fin = B.MeshBuilder.CreateBox(`cosmetic-set-fin-${bot.bot_id}-${side}`, {
        width: 1.1, height: 4.4 + variant * 0.35, depth: 3.2,
      }, scene);
      fin.position.set(side * 7.1, 12.7, 1.2);
      fin.rotation.z = side * 0.42;
      finishMesh(fin, group, side > 0 ? accent : primary);
    }
    return;
  }

  const reactor = B.MeshBuilder.CreateSphere(`cosmetic-set-reactor-${bot.bot_id}`, {
    diameter: 2.6 + variant * 0.22, segments: 8,
  }, scene);
  reactor.position.set(0, 10.3, 4.65);
  finishMesh(reactor, group, accent);
  const reactorGuard = B.MeshBuilder.CreateTorus(`cosmetic-set-reactor-guard-${bot.bot_id}`, {
    diameter: 4.4 + variant * 0.2, thickness: 0.34, tessellation: 14,
  }, scene);
  reactorGuard.position.set(0, 10.3, 4.7);
  reactorGuard.rotation.x = Math.PI / 2;
  finishMesh(reactorGuard, group, primary);
}

function buildForgeProceduralAttachment(state, asset, entry, bot, scene) {
  const B = window.BABYLON;
  const {palette, attachment} = asset.theme;
  const primary = cosmeticMaterial(state, `cosmetic-set-attachment-primary-${bot.bot_id}`, scene, parseColor(palette.primary), {
    emissiveFactor: 0.55, specular: parseColor(palette.secondary),
  });
  const accent = cosmeticMaterial(state, `cosmetic-set-attachment-accent-${bot.bot_id}`, scene, parseColor(palette.accent), {
    emissiveFactor: 1, noLight: true, alpha: 0.92,
  });
  // Variants change STRUCTURE, not just size, so two sets that share a kind
  // still read as different purchases at spectator zoom.
  const variant = Math.max(0, Math.min(3, Number(attachment.variant) || 0));
  const headWidth = forgeMetric(entry, 'headWidth', 4.35);
  const headHeight = forgeMetric(entry, 'headHeight', 3.55);
  const headDepth = forgeMetric(entry, 'headDepth', 3.65);
  const headSpan = Math.max(headWidth, headDepth);
  const torsoWidth = forgeMetric(entry, 'torsoWidth', 7);
  const torsoHeight = forgeMetric(entry, 'torsoHeight', 8.15);
  const torsoDepth = forgeMetric(entry, 'torsoDepth', 3.75);

  if (attachment.kind === 'halo') {
    const group = headAnchorGroup(state, entry, `cosmetic-attachment-${bot.bot_id}`, scene);
    const ringCount = variant === 1 || variant === 2 ? 2 : 1;
    for (let index = 0; index < ringCount; index += 1) {
      const halo = B.MeshBuilder.CreateTorus(`cosmetic-set-halo-${bot.bot_id}-${index}`, {
        diameter: headSpan * (1.82 - index * 0.5),
        thickness: Math.max(0.28, headSpan * 0.09), tessellation: 18,
      }, scene);
      halo.position.y = 0.7 + index * 0.85;
      if (variant === 2) halo.rotation.x = (index ? -1 : 1) * 0.35;
      finishMesh(halo, group, index ? primary : accent);
    }
    if (variant === 3) {
      for (const offset of [-1, 0, 1]) {
        const stud = B.MeshBuilder.CreateSphere(`cosmetic-set-halo-stud-${bot.bot_id}-${offset}`, {
          diameter: headSpan * 0.22, segments: 8,
        }, scene);
        stud.position.set(offset * headSpan * 0.55, 1.6 + Math.abs(offset) * 0.3, 0);
        finishMesh(stud, group, accent);
      }
    }
    return;
  }

  if (attachment.kind === 'antenna') {
    const group = headAnchorGroup(state, entry, `cosmetic-attachment-${bot.bot_id}`, scene);
    const mastHeight = headHeight * 1.15;
    const masts = variant === 1 ? [-1, 1] : [0];
    for (const side of masts) {
      const mast = B.MeshBuilder.CreateCylinder(`cosmetic-set-mast-${bot.bot_id}-${side}`, {
        height: mastHeight, diameter: Math.max(0.34, headWidth * 0.11), tessellation: 8,
      }, scene);
      mast.position.set(side * headWidth * 0.32, mastHeight * 0.5, 0);
      mast.rotation.z = side * 0.3;
      finishMesh(mast, group, primary);
      const beacon = B.MeshBuilder.CreateSphere(`cosmetic-set-beacon-${bot.bot_id}-${side}`, {
        diameter: headWidth * 0.32, segments: 8,
      }, scene);
      beacon.position.set(side * (headWidth * 0.32 + mastHeight * 0.29), mastHeight * (side ? 0.96 : 1), 0);
      finishMesh(beacon, group, accent);
    }
    if (variant === 2) {
      const dish = B.MeshBuilder.CreateCylinder(`cosmetic-set-dish-${bot.bot_id}`, {
        height: 0.3, diameterTop: headWidth * 0.95, diameterBottom: headWidth * 0.45, tessellation: 12,
      }, scene);
      dish.position.y = mastHeight * 0.72;
      dish.rotation.x = -0.5;
      finishMesh(dish, group, primary);
    }
    if (variant === 3) {
      for (let index = 0; index < 3; index += 1) {
        const coil = B.MeshBuilder.CreateTorus(`cosmetic-set-coil-${bot.bot_id}-${index}`, {
          diameter: headWidth * (0.55 - index * 0.12), thickness: 0.14, tessellation: 12,
        }, scene);
        coil.position.y = mastHeight * (0.35 + index * 0.22);
        finishMesh(coil, group, accent);
      }
    }
    return;
  }

  if (attachment.kind === 'crown') {
    const group = headAnchorGroup(state, entry, `cosmetic-attachment-${bot.bot_id}`, scene);
    const offsets = variant === 1 ? [-2, -1, 0, 1, 2] : [-1, 0, 1];
    for (const offset of offsets) {
      const central = offset === 0;
      const height = headHeight * (0.5 + (central ? (variant === 3 ? 0.75 : 0.2) : 0));
      const point = B.MeshBuilder.CreateCylinder(`cosmetic-set-crown-${bot.bot_id}-${offset}`, {
        height, diameterTop: 0, diameterBottom: headWidth * (variant === 1 ? 0.2 : 0.27), tessellation: 6,
      }, scene);
      point.position.set(offset * headWidth * (variant === 1 ? 0.22 : 0.34), height * 0.5, 0);
      finishMesh(point, group, central ? accent : primary);
    }
    if (variant === 2) {
      const band = B.MeshBuilder.CreateTorus(`cosmetic-set-crown-band-${bot.bot_id}`, {
        diameter: headWidth * 1.1, thickness: 0.22, tessellation: 14,
      }, scene);
      band.position.y = 0.1;
      finishMesh(band, group, accent);
    }
    if (variant === 3) {
      for (const side of [-1, 1]) {
        const gem = B.MeshBuilder.CreateSphere(`cosmetic-set-crown-gem-${bot.bot_id}-${side}`, {
          diameter: headWidth * 0.24, segments: 8,
        }, scene);
        gem.position.set(side * headWidth * 0.5, headHeight * 0.55, 0);
        finishMesh(gem, group, accent);
      }
    }
    return;
  }

  if (attachment.kind === 'orbitals') {
    const group = headAnchorGroup(state, entry, `cosmetic-attachment-${bot.bot_id}`, scene);
    const rings = variant === 1 ? 3 : 2;
    for (let axis = 0; axis < rings; axis += 1) {
      const orbit = B.MeshBuilder.CreateTorus(`cosmetic-set-orbit-${bot.bot_id}-${axis}`, {
        diameter: headSpan * (1.7 + axis * 0.28),
        thickness: Math.max(0.2, headSpan * 0.065), tessellation: 16,
      }, scene);
      orbit.position.y = -headHeight * 0.35;
      if (variant === 3) {
        orbit.rotation.x = 0;
        orbit.rotation.z = axis ? Math.PI / 2 : 0;
        orbit.rotation.y = axis * 0.6;
      } else {
        orbit.rotation.x = axis === 0 ? Math.PI / 2 : Math.PI / 2.8;
        orbit.rotation.z = axis * 0.45 - 0.35;
      }
      finishMesh(orbit, group, axis ? accent : primary);
    }
    if (variant === 2) {
      for (let index = 0; index < 3; index += 1) {
        const moon = B.MeshBuilder.CreateSphere(`cosmetic-set-orbit-moon-${bot.bot_id}-${index}`, {
          diameter: headSpan * 0.2, segments: 8,
        }, scene);
        const angle = index * (Math.PI * 2 / 3);
        moon.position.set(Math.cos(angle) * headSpan * 0.85, -headHeight * 0.35, Math.sin(angle) * headSpan * 0.85);
        finishMesh(moon, group, accent);
      }
    }
    return;
  }

  if (attachment.kind === 'fins') {
    const shoulderY = Number.isFinite(entry.cosmeticAnchors?.shoulderY)
      ? entry.cosmeticAnchors.shoulderY
      : -torsoHeight * 0.08;
    for (const side of [-1, 1]) {
      const mount = side < 0 ? 'shoulderL' : 'shoulderR';
      const group = forgeGroup(state, entry, mount, `cosmetic-attachment-${bot.bot_id}-${mount}`, scene);
      const stack = variant === 1 ? 2 : 1;
      for (let index = 0; index < stack; index += 1) {
        const tall = variant === 2;
        const fin = B.MeshBuilder.CreateBox(`cosmetic-set-fin-${bot.bot_id}-${side}-${index}`, {
          width: Math.max(0.72, torsoWidth * 0.11),
          height: torsoHeight * (tall ? 0.62 : 0.38),
          depth: Math.max(2.0, torsoDepth * (tall ? 0.5 : 0.82)),
        }, scene);
        fin.position.set(side * torsoWidth * (0.08 + index * 0.06), shoulderY + index * torsoHeight * 0.16, torsoDepth * 0.18);
        fin.rotation.z = side * (variant === 3 ? 0.85 : 0.42);
        finishMesh(fin, group, side > 0 ? accent : primary);
      }
      if (variant === 3) {
        const light = B.MeshBuilder.CreateSphere(`cosmetic-set-fin-light-${bot.bot_id}-${side}`, {
          diameter: Math.max(0.6, torsoWidth * 0.09), segments: 8,
        }, scene);
        light.position.set(side * torsoWidth * 0.3, shoulderY + torsoHeight * 0.3, torsoDepth * 0.18);
        finishMesh(light, group, accent);
      }
    }
    return;
  }

  const group = forgeGroup(state, entry, 'back', `cosmetic-attachment-${bot.bot_id}`, scene);
  const reactorDiameter = Math.max(1.65, torsoWidth * 0.27);
  if (variant === 1) {
    // Twin thruster nozzles instead of the single orb.
    for (const side of [-1, 1]) {
      const nozzle = B.MeshBuilder.CreateCylinder(`cosmetic-set-reactor-nozzle-${bot.bot_id}-${side}`, {
        height: reactorDiameter * 1.5, diameterTop: reactorDiameter * 0.75,
        diameterBottom: reactorDiameter * 0.45, tessellation: 10,
      }, scene);
      nozzle.position.set(side * reactorDiameter * 0.6, -reactorDiameter * 0.2, torsoDepth * 0.14);
      nozzle.rotation.x = -0.35;
      finishMesh(nozzle, group, primary);
      const flame = B.MeshBuilder.CreateSphere(`cosmetic-set-reactor-${bot.bot_id}-${side}`, {
        diameter: reactorDiameter * 0.55, segments: 8,
      }, scene);
      flame.position.set(side * reactorDiameter * 0.6, -reactorDiameter * 0.75, torsoDepth * 0.3);
      finishMesh(flame, group, accent);
    }
    return;
  }
  const reactor = B.MeshBuilder.CreateSphere(`cosmetic-set-reactor-${bot.bot_id}`, {
    diameter: reactorDiameter * (variant === 3 ? 1.25 : 1), segments: 8,
  }, scene);
  reactor.position.set(0, 0, torsoDepth * 0.08);
  finishMesh(reactor, group, accent);
  const guards = variant === 3 ? 2 : 1;
  for (let index = 0; index < guards; index += 1) {
    const reactorGuard = B.MeshBuilder.CreateTorus(`cosmetic-set-reactor-guard-${bot.bot_id}-${index}`, {
      diameter: reactorDiameter * 1.5, thickness: Math.max(0.22, reactorDiameter * 0.11), tessellation: 14,
    }, scene);
    reactorGuard.position.set(0, 0, torsoDepth * 0.1);
    reactorGuard.rotation.x = index ? 0 : Math.PI / 2;
    finishMesh(reactorGuard, group, primary);
  }
  if (variant === 2) {
    for (const side of [-1, 1]) {
      const exhaust = B.MeshBuilder.CreateBox(`cosmetic-set-reactor-fin-${bot.bot_id}-${side}`, {
        width: reactorDiameter * 0.18, height: reactorDiameter * 1.1, depth: reactorDiameter * 0.6,
      }, scene);
      exhaust.position.set(side * reactorDiameter * 0.95, 0, torsoDepth * 0.1);
      exhaust.rotation.z = side * 0.4;
      finishMesh(exhaust, group, primary);
    }
  }
}

function buildForgeAttachment(state, asset, entry, bot, scene) {
  const B = window.BABYLON;
  if (asset.kind === 'procedural') {
    buildForgeProceduralAttachment(state, asset, entry, bot, scene);
    return;
  }

  const headWidth = forgeMetric(entry, 'headWidth', 4.35);
  const headHeight = forgeMetric(entry, 'headHeight', 3.55);
  if (asset.key === 'signal_antenna') {
    const group = headAnchorGroup(state, entry, `cosmetic-attachment-${bot.bot_id}`, scene);
    const metal = cosmeticMaterial(state, `cosmetic-antenna-metal-${bot.bot_id}`, scene,
      new B.Color3(0.45, 0.5, 0.58), { emissiveFactor: 0.12, specular: B.Color3.White() });
    const glow = cosmeticMaterial(state, `cosmetic-antenna-glow-${bot.bot_id}`, scene, parseColor(bot.avatar_color), {
      emissiveFactor: 1, noLight: true,
    });
    const mastHeight = headHeight * 1.35;
    const mast = B.MeshBuilder.CreateCylinder(`cosmetic-antenna-mast-${bot.bot_id}`, {
      height: mastHeight, diameter: Math.max(0.38, headWidth * 0.12), tessellation: 8,
    }, scene);
    mast.position.y = mastHeight * 0.5;
    finishMesh(mast, group, metal);
    const beacon = B.MeshBuilder.CreateSphere(`cosmetic-antenna-beacon-${bot.bot_id}`, {
      diameter: headWidth * 0.4, segments: 8,
    }, scene);
    beacon.position.y = mastHeight;
    finishMesh(beacon, group, glow);
    return;
  }

  if (asset.key === 'orbital_halo') {
    const group = headAnchorGroup(state, entry, `cosmetic-attachment-${bot.bot_id}`, scene);
    const halo = cosmeticMaterial(state, `cosmetic-halo-${bot.bot_id}`, scene, parseColor(bot.avatar_color), {
      emissiveFactor: 1, noLight: true, alpha: 0.9,
    });
    const ring = B.MeshBuilder.CreateTorus(`cosmetic-halo-ring-${bot.bot_id}`, {
      diameter: headWidth * 2.05, thickness: Math.max(0.34, headWidth * 0.11), tessellation: 24,
    }, scene);
    ring.position.y = 0.75;
    finishMesh(ring, group, halo);
  }
}

function buildAttachment(state, asset, entry, bot, scene) {
  const assetKey = asset.key;
  if (assetKey === 'none') return;
  if (isForgeEntry(entry)) {
    buildForgeAttachment(state, asset, entry, bot, scene);
    return;
  }
  const B = window.BABYLON;
  const color = parseColor(bot.avatar_color);
  const group = createGroup(
    `cosmetic-attachment-${bot.bot_id}`,
    entry.mounts?.cosmeticRoot || entry.root,
    scene,
  );
  state.groups.push(group);

  if (asset.kind === 'procedural') {
    buildProceduralAttachment(state, asset, group, bot, scene);
    return;
  }

  if (assetKey === 'signal_antenna') {
    const metal = cosmeticMaterial(state, `cosmetic-antenna-metal-${bot.bot_id}`, scene,
      new B.Color3(0.45, 0.5, 0.58), { emissiveFactor: 0.12, specular: B.Color3.White() });
    const glow = cosmeticMaterial(state, `cosmetic-antenna-glow-${bot.bot_id}`, scene, color, {
      emissiveFactor: 1, noLight: true,
    });
    const mast = B.MeshBuilder.CreateCylinder(`cosmetic-antenna-mast-${bot.bot_id}`, {
      height: 5.4, diameter: 0.55, tessellation: 8,
    }, scene);
    mast.position.y = 21.4;
    finishMesh(mast, group, metal);
    const beacon = B.MeshBuilder.CreateSphere(`cosmetic-antenna-beacon-${bot.bot_id}`, {
      diameter: 1.8, segments: 8,
    }, scene);
    beacon.position.y = 24.4;
    finishMesh(beacon, group, glow);
    return;
  }

  if (assetKey === 'orbital_halo') {
    const halo = cosmeticMaterial(state, `cosmetic-halo-${bot.bot_id}`, scene, color, {
      emissiveFactor: 1, noLight: true, alpha: 0.9,
    });
    const ring = B.MeshBuilder.CreateTorus(`cosmetic-halo-ring-${bot.bot_id}`, {
      diameter: 10.5, thickness: 0.55, tessellation: 24,
    }, scene);
    ring.position.y = 23.2;
    finishMesh(ring, group, halo);
  }
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
  const emissiveFactor = asset.kind === 'procedural' ? asset.theme.weapon.emissive : (assetKey === 'solar_flare' ? 0.85 : 0.65);

  for (const mesh of weaponMeshes) {
    const original = mesh.material;
    if (!original || typeof original.clone !== 'function') continue;
    const clone = original.clone(`cosmetic-weapon-${assetKey}-${bot.bot_id}-${mesh.name}`);
    if (!clone || !clone.diffuseColor || !clone.emissiveColor) {
      if (clone) clone.dispose();
      continue;
    }
    if (typeof clone.unfreeze === 'function') clone.unfreeze();
    clone.diffuseColor = tint.clone();
    clone.emissiveColor = glow.scale(emissiveFactor);
    clone.specularColor = specular.clone();
    clone.freeze();
    mesh.material = clone;
    state.weaponSwaps.push({ mesh, original, clone });
  }
  buildWeaponGarnish(state, asset, entry, bot, scene, glow);
}

const GARNISH_BUILDERS = Object.freeze({
  ion(B, scene, name) {
    const ring = B.MeshBuilder.CreateTorus(`${name}-ring`, {diameter: 1.9, thickness: 0.24, tessellation: 14}, scene);
    ring.rotation.x = Math.PI / 2;
    return [ring];
  },
  ember(B, scene, name) {
    const flame = B.MeshBuilder.CreateCylinder(`${name}-flame`, {
      height: 2.4, diameterTop: 0, diameterBottom: 1.15, tessellation: 8,
    }, scene);
    flame.position.y = 0.9;
    return [flame];
  },
  prism(B, scene, name) {
    const shards = [];
    for (let index = 0; index < 3; index += 1) {
      const shard = B.MeshBuilder.CreateBox(`${name}-shard-${index}`, {width: 0.4, height: 1.2, depth: 0.4}, scene);
      const angle = index * (Math.PI * 2 / 3);
      shard.position.set(Math.cos(angle) * 1.0, 0.3, Math.sin(angle) * 1.0);
      shard.rotation.set(0.5, angle, 0.4);
      shards.push(shard);
    }
    return shards;
  },
  void(B, scene, name) {
    const orb = B.MeshBuilder.CreateSphere(`${name}-orb`, {diameter: 1.35, segments: 8}, scene);
    const ring = B.MeshBuilder.CreateTorus(`${name}-halo`, {diameter: 2.2, thickness: 0.16, tessellation: 14}, scene);
    ring.rotation.x = Math.PI / 3;
    return [orb, ring];
  },
});

/**
 * Small finish-specific garnish at each weapon tip so paid finishes read as
 * distinct hardware, not just a tint. Tips ride the weapon pose nodes, so the
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
    emissiveFactor: 1, noLight: true, alpha: 0.92,
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
