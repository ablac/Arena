'use strict';

/**
 * Central graphics/animation settings: schema, persistence, and the
 * enabled-check every effect site calls before doing visual work.
 * @module settings
 */

const STORAGE_KEY = 'arenaSettings';

// Every section groups effects a viewer would recognize as "one thing".
// A section's master toggle ANDs with each effect's own toggle - turning
// the master off silences everything in the section without touching the
// individual effect states, so switching it back on restores whatever was
// set before. All defaults are ON: shipping this feature must not change
// the site's current look for anyone who never opens Settings.
export const SETTINGS_SCHEMA = {
  killFlash: {
    label: 'Kill Flash',
    description: 'Red flash when a bot dies.',
    effects: {
      fullScreenFlash: { label: 'Full-screen flash' },
      killFeedGlow: { label: 'Kill feed glow' },
    },
  },
  deathEffects: {
    label: 'Death Effects',
    description: 'What happens to a bot\'s body when it dies.',
    effects: {
      deathFlash: { label: 'Death flash (white pulse)' },
      deathBurst: { label: 'Death burst (particles, ring, light pillar)' },
      corpseFade: { label: 'Corpse fade (translucent after death)' },
      directionalDeath: { label: 'Directional death fall' },
      killCameraNudge: { label: 'Kill camera nudge' },
    },
  },
  taunts: {
    label: 'Bot Taunts',
    description: 'Speech bubbles when a bot emotes.',
    effects: {
      speechBubbles: { label: 'Speech bubbles above bots' },
    },
  },
  hitReactions: {
    label: 'Hit Reactions',
    description: 'Feedback when a bot takes damage but stays alive.',
    effects: {
      impactFlash: { label: 'Impact flash + squash' },
      damageFlinch: { label: 'Damage flinch recoil' },
      floatingDamageNumbers: { label: 'Floating damage numbers' },
      woundedTint: { label: 'Wounded / low-HP red tint' },
      damageVignette: { label: 'Followed-bot damage vignette' },
    },
  },
  weaponImpactVfx: {
    label: 'Weapon & Ability Impact VFX',
    description: 'Strike, hit, and ability-impact effects for every weapon.',
    effects: {
      weaponStrike: { label: 'Weapon strike silhouette' },
      hitSparks: { label: 'Hit sparks' },
      bowImpact: { label: 'Bow impact ring' },
      shieldBash: { label: 'Shield bash ring' },
      spearBrace: { label: 'Spear brace ring' },
      backstab: { label: 'Backstab mark' },
      shoveShockwave: { label: 'Shove shockwave' },
      dodgeAfterimage: { label: 'Dodge afterimage' },
      meleeSwingTrails: { label: 'Melee swing trails' },
      grappleLine: { label: 'Grapple line & hook' },
      grappleSlam: { label: 'Grapple slam ring' },
      mineExplosion: { label: 'Mine explosion' },
      staffExplosion: { label: 'Staff explosion' },
      teleportBurst: { label: 'Teleport burst' },
      groundScorch: { label: 'Ground scorch decals' },
    },
  },
  objectiveIndicators: {
    label: 'Objective Indicators',
    description: 'Bounty, flag, and capture-pad visuals.',
    effects: {
      bountyCrown: { label: 'Bounty crown' },
      flagComet: { label: 'Flag comet trail (CTF)' },
      capturePadPulse: { label: 'Capture pad pulse' },
      minimapPings: { label: 'Minimap bounty & flag pings' },
    },
  },
  gameplayZoneIndicators: {
    label: 'Gameplay Zone Indicators',
    description: 'Danger and pickup zone visuals (functional, not just decorative).',
    effects: {
      safeZoneRing: { label: 'Safe zone ring' },
      minimapZonePulse: { label: 'Minimap zone pulse' },
      hazardZoneEffects: { label: 'Hazard zone electrical effects' },
      burnFieldPulse: { label: 'Burn field pulsing' },
      staffImpactRings: { label: 'Staff impact reveal rings' },
      gravityWellSwirl: { label: 'Gravity well swirl' },
    },
  },
  movementTrails: {
    label: 'Movement Trails',
    description: 'Ribbon trails behind moving bots.',
    effects: {
      botTrails: { label: 'Movement trails' },
      trailBrightness: { label: 'Trail brightness / glow' },
    },
  },
  botCosmetics: {
    label: 'Bot Cosmetics',
    description: 'Player-selected visual customization with no gameplay effect.',
    effects: {
      skins: { label: 'Chassis skins' },
      weaponFinishes: { label: 'Weapon finishes' },
      attachments: { label: 'Bot attachments' },
    },
  },
  rendering: {
    label: 'Rendering',
    description: 'Post-processing (also affects GPU cost).',
    effects: {
      bloom: { label: 'Bloom' },
      glowLayer: { label: 'Neon glow halos' },
      vignette: { label: 'Vignette' },
      fxaa: { label: 'Anti-aliasing (FXAA)' },
      sharpen: { label: 'Sharpen' },
      shadows: { label: 'Obstacle shadows' },
      characterLighting: { label: 'Character lighting & shading' },
      dynamicGrading: { label: 'Dynamic mode grading (sudden death, round win)' },
      // Default-on keeps the current 1x render resolution; unchecking lets
      // HiDPI displays render at native resolution (sharper, more GPU).
      resolutionCap: { label: 'Cap resolution at 1x (performance)' },
    },
  },
  arenaAmbience: {
    label: 'Arena Scene Ambience',
    description: 'Background scene decoration with no gameplay meaning.',
    effects: {
      skybox: { label: 'Procedural skybox (stars & nebulae)' },
      ambientParticles: { label: 'Ambient floating particles' },
      cornerPylons: { label: 'Corner pylon beams & beacons' },
      thrusters: { label: 'Underside thruster jets' },
      spaceObjects: { label: 'Space objects (satellites, comets, UFO)' },
      floorEnergyGlow: { label: 'Floor energy glow motion' },
      mapPalettes: { label: 'Per-map color palettes' },
      contactShadows: { label: 'Obstacle contact shadows (floor)' },
      obstacleDetailing: { label: 'Obstacle rooftop detailing' },
      smoothMapWalls: { label: 'Smooth map boundary walls' },
      depthFog: { label: 'Depth fog (far-edge falloff)' },
      lightShafts: { label: 'Light shafts' },
      edgeWaterfalls: { label: 'Edge energy waterfalls' },
      holoTitle: { label: 'Holographic arena title' },
      idleWeaponAnims: { label: 'Idle weapon animations' },
    },
  },
  siteMotion: {
    label: 'Site Motion',
    description: 'Page-level motion outside the 3D view. Overlaps with your OS reduced-motion setting.',
    effects: {
      roundSweep: { label: 'Round start sweep' },
      liveHeartbeat: { label: 'Live status heartbeat pulse' },
      rankChangeFlash: { label: 'Standings rank-change flash' },
      auroraBackground: { label: 'Aurora background drift' },
      heroChipFloat: { label: 'Hero chip float' },
      orbitSpins: { label: 'Keygen orbit spins' },
      revealOnScroll: { label: 'Reveal-on-scroll' },
    },
  },
};

function buildDefaults() {
  const defaults = {};
  for (const [sectionKey, section] of Object.entries(SETTINGS_SCHEMA)) {
    const effects = {};
    for (const effectKey of Object.keys(section.effects)) effects[effectKey] = true;
    defaults[sectionKey] = { master: true, effects };
  }
  return defaults;
}

const DEFAULT_SETTINGS = buildDefaults();

function loadFromStorage() {
  let stored = null;
  try {
    stored = JSON.parse(localStorage.getItem(STORAGE_KEY));
  } catch {
    stored = null;
  }

  // Deep-merge over defaults so adding a new effect later doesn't crash on
  // an older saved blob that predates it, and so a partially-corrupt blob
  // can't take down a whole section.
  const merged = {};
  for (const [sectionKey, sectionDefaults] of Object.entries(DEFAULT_SETTINGS)) {
    const storedSection = stored && typeof stored === 'object' ? stored[sectionKey] : null;
    const effects = { ...sectionDefaults.effects };
    if (storedSection && typeof storedSection.effects === 'object') {
      for (const effectKey of Object.keys(effects)) {
        if (typeof storedSection.effects[effectKey] === 'boolean') {
          effects[effectKey] = storedSection.effects[effectKey];
        }
      }
    }
    merged[sectionKey] = {
      master: storedSection && typeof storedSection.master === 'boolean' ? storedSection.master : sectionDefaults.master,
      effects,
    };
  }
  return merged;
}

let state = loadFromStorage();
const listeners = new Set();

function persist() {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state));
  } catch {
    // Storage can be unavailable (private browsing, quota) - settings just
    // won't persist across reloads; the in-memory state still works.
  }
}

function notify() {
  for (const listener of listeners) listener(state);
}

/** Subscribe to any settings change. Returns an unsubscribe function. */
export function onSettingsChange(listener) {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

/** Whether a specific effect should currently render. */
export function isEnabled(sectionKey, effectKey) {
  const section = state[sectionKey];
  if (!section) return true;
  return section.master && section.effects[effectKey] !== false;
}

/** Whether a whole section's master switch is on. */
export function isSectionEnabled(sectionKey) {
  return state[sectionKey] ? state[sectionKey].master : true;
}

export function getState() {
  return state;
}

export function setSectionMaster(sectionKey, value) {
  if (!state[sectionKey]) return;
  state[sectionKey].master = !!value;
  persist();
  notify();
}

export function setEffect(sectionKey, effectKey, value) {
  if (!state[sectionKey] || !(effectKey in state[sectionKey].effects)) return;
  state[sectionKey].effects[effectKey] = !!value;
  persist();
  notify();
}

export function resetToDefaults() {
  state = buildDefaults();
  persist();
  notify();
}
