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
