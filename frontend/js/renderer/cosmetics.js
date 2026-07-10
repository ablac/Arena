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

function safeAsset(slot, value) {
  return typeof value === 'string' && ALLOWED[slot].has(value) ? value : DEFAULTS[slot];
}

function desiredLoadout(bot) {
  const raw = bot && bot.cosmetics && typeof bot.cosmetics === 'object' ? bot.cosmetics : {};
  return {
    bot_skin: safeAsset('bot_skin', raw.bot_skin),
    weapon_skin: safeAsset('weapon_skin', raw.weapon_skin),
    attachment: safeAsset('attachment', raw.attachment),
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

function buildBotSkin(state, assetKey, entry, bot, scene) {
  if (assetKey === 'standard') return;
  const B = window.BABYLON;
  const color = parseColor(bot.avatar_color);
  const group = createGroup(`cosmetic-skin-${bot.bot_id}`, entry.root, scene);
  state.groups.push(group);

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

function buildAttachment(state, assetKey, entry, bot, scene) {
  if (assetKey === 'none') return;
  const B = window.BABYLON;
  const color = parseColor(bot.avatar_color);
  const group = createGroup(`cosmetic-attachment-${bot.bot_id}`, entry.root, scene);
  state.groups.push(group);

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

function applyWeaponFinish(state, assetKey, entry, bot) {
  if (assetKey === 'standard' || !entry.weapon || typeof entry.weapon.getChildMeshes !== 'function') return;
  const B = window.BABYLON;
  const tint = assetKey === 'solar_flare'
    ? new B.Color3(1, 0.46, 0.06)
    : new B.Color3(0.32, 0.07, 0.58);

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
    clone.emissiveColor = tint.scale(assetKey === 'solar_flare' ? 0.85 : 0.65);
    clone.specularColor = assetKey === 'solar_flare'
      ? new B.Color3(1, 0.86, 0.5)
      : new B.Color3(0.52, 0.2, 0.8);
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
export function applyBotCosmetics(entry, bot, scene) {
  if (!entry || !bot) return;
  const loadout = desiredLoadout(bot);
  const enabled = {
    bot_skin: isEnabled('botCosmetics', 'skins'),
    weapon_skin: isEnabled('botCosmetics', 'weaponFinishes'),
    attachment: isEnabled('botCosmetics', 'attachments'),
  };
  const signature = [
    loadout.bot_skin, enabled.bot_skin,
    loadout.weapon_skin, enabled.weapon_skin,
    loadout.attachment, enabled.attachment,
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
