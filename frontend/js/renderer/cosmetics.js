'use strict';

/**
 * Presentation-only bot cosmetics. Asset keys are mapped to fixed procedural
 * meshes/materials here; server-supplied strings are never treated as URLs,
 * code, stats, or arbitrary model paths.
 * @module renderer/cosmetics
 */

import { isEnabled } from '../settings.js';
import { makeMat, parseColor } from './utils.js';

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
  mesh.alwaysSelectAsActiveMesh = true;
  return mesh;
}

function cosmeticMaterial(state, name, scene, color, options = {}) {
  const material = makeMat(name, scene, color, options);
  material.freeze();
  state.materials.push(material);
  return material;
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
  if (assetKey === 'standard') return;
  const B = window.BABYLON;
  const color = parseColor(bot.avatar_color);
  const group = createGroup(`cosmetic-skin-${bot.bot_id}`, entry.root, scene);
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

function buildAttachment(state, asset, entry, bot, scene) {
  const assetKey = asset.key;
  if (assetKey === 'none') return;
  const B = window.BABYLON;
  const color = parseColor(bot.avatar_color);
  const group = createGroup(`cosmetic-attachment-${bot.bot_id}`, entry.root, scene);
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

function applyWeaponFinish(state, asset, entry, bot) {
  const assetKey = asset.key;
  if (assetKey === 'standard' || !entry.weapon || typeof entry.weapon.getChildMeshes !== 'function') return;
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

  for (const mesh of entry.weapon.getChildMeshes(false)) {
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
  if (enabled.weapon_skin) applyWeaponFinish(state, loadout.weapon_skin, entry, bot);
  if (enabled.attachment) buildAttachment(state, loadout.attachment, entry, bot, scene);
}
