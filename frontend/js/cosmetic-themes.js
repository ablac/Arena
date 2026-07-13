(function attachArenaCosmeticThemes(root) {
  'use strict';

  const KEY_PATTERN = /^arena_set_(00[1-9]|0[1-9][0-9]|[1-9][0-9]{2})_([a-z0-9]+(?:[_-][a-z0-9]+)*)$/;

  const PALETTES = [
    ['#ff7048', '#ffca62', '#5ee7ff', '#1b0b14'],
    ['#25d9c8', '#89f7de', '#ffd166', '#071b20'],
    ['#6aa8ff', '#a58cff', '#7cf7ff', '#0a1024'],
    ['#e86cff', '#ff9bd8', '#6fffe9', '#1b0822'],
    ['#b5e550', '#5ee88d', '#d9ff82', '#10200c'],
    ['#ff536f', '#ff9f68', '#ffe066', '#210b12'],
    ['#7f8cff', '#42d6ff', '#b8c0ff', '#0c1028'],
    ['#f3b34c', '#f7e08b', '#66d9ff', '#211606'],
    ['#57d39b', '#8cf0c4', '#f0ff7a', '#082018'],
    ['#d27aff', '#8e8cff', '#ffb3ef', '#170d26'],
    ['#ff876e', '#ffb36b', '#9df7ff', '#24100b'],
    ['#4fd1ff', '#66f2c2', '#d8f06a', '#061b25'],
  ];
  const SKIN_PATTERNS = ['bands', 'plates', 'chevrons', 'core'];
  const WEAPON_FINISHES = ['ion', 'ember', 'prism', 'void'];
  const ATTACHMENTS = ['halo', 'antenna', 'crown', 'orbitals', 'fins', 'reactor'];
  const THEME_CACHE_LIMIT = 256;
  const themeCache = new Map();
  const LEGACY_SWATCHES = Object.freeze({
    standard: 'linear-gradient(135deg, #6f8094, #283747)',
    neon_grid: 'linear-gradient(135deg, #25d9ff, #7f5cff 62%, #11182b)',
    carbon_armor: 'linear-gradient(135deg, #596675, #111722 60%, #05070b)',
    solar_flare: 'linear-gradient(135deg, #ffba45, #ff5d32 62%, #3a1010)',
    void_edge: 'linear-gradient(135deg, #a16cff, #422075 62%, #0c0718)',
    none: 'linear-gradient(135deg, #465567, #151d28)',
    signal_antenna: 'linear-gradient(135deg, #56dfff, #62768c 62%, #111923)',
    orbital_halo: 'linear-gradient(135deg, #d187ff, #5f73ff 62%, #12132c)',
    ember_sparks: 'linear-gradient(135deg, #46130b, #ff5b2e 52%, #ffd166)',
    frost_shards: 'linear-gradient(135deg, #10284a, #7be7ff 52%, #e5fbff)',
    ion_stream: 'linear-gradient(135deg, #101f63, #4b7cff 48%, #39f5ff)',
    plasma_ribbon: 'linear-gradient(135deg, #32125a, #7957ff 48%, #ff3fd1)',
    void_motes: 'linear-gradient(135deg, #130b2d, #6d43c5 52%, #cc8cff)',
    solar_wake: 'linear-gradient(135deg, #5a1b06, #ff9e2c 50%, #fff4a8)',
    lunar_dust: 'linear-gradient(135deg, #252b42, #aeb9da 52%, #ffffff)',
    comet_tail: 'linear-gradient(135deg, #102c42, #6ee7ff 52%, #ffffff)',
    nebula_pulse: 'linear-gradient(135deg, #24154f, #8f63ff 48%, #ff77cc)',
    storm_arcs: 'linear-gradient(135deg, #102456, #56b7ff 52%, #e8fbff)',
    static_glitch: 'linear-gradient(135deg, #10252b, #00f0b5 48%, #f638dc)',
    pixel_scatter: 'linear-gradient(135deg, #12352b, #57f287 48%, #78a7ff)',
    data_stream: 'linear-gradient(135deg, #0c3028, #39ffb6 52%, #83f7ff)',
    holo_prism: 'linear-gradient(135deg, #16284f, #64e6ff 48%, #ff72d2)',
    toxic_spores: 'linear-gradient(135deg, #21320b, #9bea37 52%, #e3ff75)',
    verdant_leaves: 'linear-gradient(135deg, #103a24, #35c96f 52%, #b8f26d)',
    sand_wake: 'linear-gradient(135deg, #493218, #c99a55 52%, #f0d58d)',
    magma_cinders: 'linear-gradient(135deg, #4a0b06, #ff3d20 52%, #ffbf3f)',
    ocean_spray: 'linear-gradient(135deg, #082e55, #23aef3 52%, #b9fbff)',
    gilded_dust: 'linear-gradient(135deg, #45300b, #dcae36 52%, #fff0a1)',
    rune_sparks: 'linear-gradient(135deg, #2b1759, #9a6cff 48%, #60e9ff)',
    phantom_smoke: 'linear-gradient(135deg, #242033, #766b99 52%, #c8bce8)',
    gear_sparks: 'linear-gradient(135deg, #47250d, #d67b31 52%, #f7df92)',
    bounty_flare: 'linear-gradient(135deg, #4d1909, #ff5a36 48%, #ffca3a)',
    // Original full-body forms mirror their bounded renderer palettes so cards
    // stay recognizable before the lazy 3D preview starts.
    body_giant_chicken: 'linear-gradient(135deg, #7c241e, #f4eee2 48%, #f4bd35)',
    body_highland_cow: 'linear-gradient(135deg, #30170e, #7c4327 48%, #f2d5a4)',
    body_corgi: 'linear-gradient(135deg, #4d2713, #d9853f 48%, #61d7ff)',
    body_tabby_cat: 'linear-gradient(135deg, #332c27, #8d7c6d 48%, #72e0ff)',
    body_red_fox: 'linear-gradient(135deg, #4c1a0d, #d95c2f 48%, #4ad9ff)',
    body_battle_rabbit: 'linear-gradient(135deg, #383641, #c9c7d2 48%, #ff6ea8)',
    body_emperor_penguin: 'linear-gradient(135deg, #080d14, #222b3a 48%, #f2bd35)',
    body_bullfrog: 'linear-gradient(135deg, #163316, #4f9b4f 48%, #f2d85c)',
    body_land_shark: 'linear-gradient(135deg, #122934, #477b98 48%, #ff6a64)',
    body_tyrant_rex: 'linear-gradient(135deg, #1d3216, #5f8f48 48%, #ff8b45)',
    body_human_adventurer: 'linear-gradient(135deg, #172d49, #c58d69 48%, #e7ad4f)',
    body_astronaut: 'linear-gradient(135deg, #273342, #e8eef5 48%, #48d8ff)',
    body_knight: 'linear-gradient(135deg, #18202d, #9aa8b8 48%, #f0c54b)',
    body_wizard: 'linear-gradient(135deg, #1c1234, #4e3a82 48%, #65e5ff)',
    body_skeleton: 'linear-gradient(135deg, #151a24, #dedbd0 48%, #60ebff)',
    body_stone_golem: 'linear-gradient(135deg, #252b2f, #69737b 48%, #ffb34e)',
    body_slime_monarch: 'linear-gradient(135deg, #0e4738, #4bcf8d 48%, #f3d45b)',
    body_spider_drone: 'linear-gradient(135deg, #101421, #657189 48%, #f15bff)',
  });

  function hashKey(value) {
    let hash = 2166136261;
    for (let index = 0; index < value.length; index += 1) {
      hash ^= value.charCodeAt(index);
      hash = Math.imul(hash, 16777619);
    }
    return hash >>> 0;
  }

  function parseAssetKey(value) {
    if (typeof value !== 'string' || value.length > 96) return null;
    const match = KEY_PATTERN.exec(value);
    if (!match) return null;
    return Object.freeze({key: value, number: Number(match[1]), slug: match[2]});
  }

  function freezeTheme(theme) {
    Object.freeze(theme.palette);
    Object.freeze(theme.skin);
    Object.freeze(theme.weapon);
    Object.freeze(theme.attachment);
    return Object.freeze(theme);
  }

  function themeFor(value) {
    const parsed = parseAssetKey(value);
    if (!parsed) return null;
    const cached = themeCache.get(parsed.key);
    if (cached) return cached;
    const seed = (hashKey(parsed.key) ^ Math.imul(parsed.number, 2654435761)) >>> 0;
    const paletteValues = PALETTES[seed % PALETTES.length];
    const theme = freezeTheme({
      key: parsed.key,
      number: parsed.number,
      slug: parsed.slug,
      palette: {
        primary: paletteValues[0],
        secondary: paletteValues[1],
        accent: paletteValues[2],
        dark: paletteValues[3],
      },
      skin: {
        pattern: SKIN_PATTERNS[(seed >>> 4) % SKIN_PATTERNS.length],
        layers: 1 + ((seed >>> 7) % 3),
        angle: (((seed >>> 10) % 7) - 3) * 0.08,
      },
      weapon: {
        finish: WEAPON_FINISHES[(seed >>> 13) % WEAPON_FINISHES.length],
        emissive: 0.48 + ((seed >>> 16) % 28) / 100,
      },
      attachment: {
        kind: ATTACHMENTS[(seed >>> 19) % ATTACHMENTS.length],
        variant: (seed >>> 22) % 4,
      },
    });
    if (themeCache.size >= THEME_CACHE_LIMIT) {
      themeCache.delete(themeCache.keys().next().value);
    }
    themeCache.set(parsed.key, theme);
    return theme;
  }

  function swatchStyle(value) {
    if (Object.hasOwn(LEGACY_SWATCHES, value)) return LEGACY_SWATCHES[value];
    const theme = themeFor(value);
    if (!theme) return '';
    return `linear-gradient(135deg, ${theme.palette.dark}, ${theme.palette.primary} 46%, ${theme.palette.accent})`;
  }

  root.ArenaCosmeticThemes = Object.freeze({parseAssetKey, swatchStyle, themeFor});
})(typeof globalThis !== 'undefined' ? globalThis : window);
