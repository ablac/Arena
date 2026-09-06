(function attachArenaAccountProfile(root) {
  'use strict';

  function escapeHTML(value) {
    return String(value ?? '')
      .replaceAll('&', '&amp;')
      .replaceAll('<', '&lt;')
      .replaceAll('>', '&gt;')
      .replaceAll('"', '&quot;')
      .replaceAll("'", '&#39;');
  }

  function cleanText(value) {
    return typeof value === 'string' ? value.trim() : '';
  }

  const DEFAULT_AVATAR_COLOR = '#5edfff';
  const BIO_MAX_LENGTH = 280;
  const USERNAME_SETUP_URL = 'https://accounts.angel-serv.com/portal/account/details';
  const HEX_COLOR_PATTERN = /^#[0-9a-fA-F]{6}$/;

  // Presets drawn from colors already used elsewhere in the dashboard (accent,
  // green, gold, orange, red, purple, the default bot avatar color, and a
  // neutral near-white) so a fresh profile's swatch options feel native to
  // the rest of the UI rather than an arbitrary new palette.
  const AVATAR_COLOR_PRESETS = Object.freeze([
    '#47d7ff', '#58e58f', '#ffce54', '#ff8a3d', '#ff7b72', '#c5a6ff', '#5edfff', '#ecf4ff',
  ]);

  function safeHexColor(value, fallback) {
    const cleaned = cleanText(value);
    return HEX_COLOR_PATTERN.test(cleaned) ? cleaned.toLowerCase() : fallback;
  }

  function normalizeProfileBot(raw) {
    const bot = raw && typeof raw === 'object' ? raw : {};
    const elo = Number(bot.elo);
    const kills = Number(bot.kills);
    const deaths = Number(bot.deaths);
    const roundWins = Number(bot.round_wins);
    return {
      bot_id: cleanText(bot.bot_id),
      name: cleanText(bot.name) || 'Unnamed bot',
      avatar_color: cleanText(bot.avatar_color) || DEFAULT_AVATAR_COLOR,
      default_weapon: cleanText(bot.default_weapon) || 'sword',
      elo: Number.isFinite(elo) ? Math.round(elo) : 0,
      kills: Number.isFinite(kills) && kills >= 0 ? Math.floor(kills) : 0,
      deaths: Number.isFinite(deaths) && deaths >= 0 ? Math.floor(deaths) : 0,
      round_wins: Number.isFinite(roundWins) && roundWins >= 0 ? Math.floor(roundWins) : 0,
    };
  }

  // GET /api/v1/profile/{account_id} response shape (also the shape returned
  // by a successful PATCH /api/v1/account/profile).
  function normalizeProfile(payload) {
    const source = payload && typeof payload === 'object' ? payload : {};
    const bots = Array.isArray(source.bots)
      ? source.bots.map(normalizeProfileBot).filter(bot => bot.bot_id)
      : [];
    return {
      account_id: cleanText(source.account_id),
      public_username: typeof source.public_username === 'string' && /^[a-z0-9_]{3,24}$/.test(source.public_username) ? source.public_username : null,
      bio: typeof source.bio === 'string' ? source.bio : '',
      avatar_color: cleanText(source.avatar_color) || DEFAULT_AVATAR_COLOR,
      joined_at: cleanText(source.joined_at),
      shows_bots: source.shows_bots === true,
      bots,
    };
  }

  function accountProfileRoute(name, id) {
    const encoded = id ? encodeURIComponent(String(id)) : '';
    const routes = {
      profile: `/profile/${encoded}`,
      update: '/account/profile',
    };
    if (!Object.hasOwn(routes, name)) throw new Error(`unknown account profile route: ${name}`);
    return routes[name];
  }

  function previewChatHandle(profile) {
    return profile.public_username || 'Username unavailable';
  }

  function weaponLabel(weapon) {
    const value = cleanText(weapon);
    return value ? value.charAt(0).toUpperCase() + value.slice(1) : 'Sword';
  }

  function renderProfileBotCard(bot) {
    return `<article class="profile-bot-card">
      <strong>${escapeHTML(bot.name)}</strong>
      <div class="pill-row">
        <span class="pill">${escapeHTML(weaponLabel(bot.default_weapon))}</span>
        <span class="pill">Elo ${escapeHTML(String(bot.elo))}</span>
        <span class="pill">${escapeHTML(String(bot.kills))}K / ${escapeHTML(String(bot.deaths))}D</span>
      </div>
    </article>`;
  }

  function joinedLabel(rawTime) {
    const raw = cleanText(rawTime);
    const date = new Date(raw);
    if (!raw || Number.isNaN(date.getTime())) return '';
    return date.toLocaleDateString(undefined, {year: 'numeric', month: 'short', day: 'numeric'});
  }

  function renderProfilePreview(profile) {
    const joined = joinedLabel(profile.joined_at);
    const bots = !profile.shows_bots
      ? '<p class="cosmetic-empty">Bots are hidden from your public profile.</p>'
      : profile.bots.length
        ? `<div class="profile-bot-grid">${profile.bots.map(renderProfileBotCard).join('')}</div>`
        : '<p class="cosmetic-empty">No linked bots to show yet.</p>';
    return `<section class="profile-preview" aria-labelledby="profile-preview-title">
      <div class="cosmetic-kicker">Public preview</div>
      <h3 id="profile-preview-title">How others see you</h3>
      <div class="profile-preview-head">
        <span class="profile-preview-avatar" style="background:${escapeHTML(profile.avatar_color)}" aria-hidden="true"></span>
        <div>
          <strong>${escapeHTML(previewChatHandle(profile))}</strong>
          <div class="profile-chat-handle-preview">Chat handle: <strong>${escapeHTML(previewChatHandle(profile))}</strong></div>
          ${joined ? `<div class="profile-chat-handle-preview">Joined ${escapeHTML(joined)}</div>` : ''}
        </div>
      </div>
      <p>${profile.bio ? escapeHTML(profile.bio) : '<em>No bio yet.</em>'}</p>
      ${bots}
    </section>`;
  }

  function renderPanel(profile) {
    const remaining = BIO_MAX_LENGTH - profile.bio.length;
    const selectedColor = safeHexColor(profile.avatar_color, DEFAULT_AVATAR_COLOR);
    const swatches = AVATAR_COLOR_PRESETS.map(hex => `<button type="button" class="profile-color-swatch${hex === selectedColor ? ' is-selected' : ''}" style="background:${escapeHTML(hex)}" data-profile-swatch="${escapeHTML(hex)}" aria-label="Use avatar color ${escapeHTML(hex)}"></button>`).join('');

    return `<form id="profileForm" class="account-profile-form" novalidate>
      <div class="profile-field">
        <span class="profile-field-label">Public username</span>
        <strong>${escapeHTML(previewChatHandle(profile))}</strong>
        <p>Your username is shared across Angel apps. <a href="${USERNAME_SETUP_URL}" target="_blank" rel="noopener noreferrer">${profile.public_username ? 'Manage username' : 'Choose a username'} in Angel Accounts</a>.</p>
        <p>After saving in Accounts, refresh here to sign in again and bring your username into Arena.</p>
        <button type="button" data-profile-refresh-username>Refresh username</button>
      </div>
      <div class="profile-field">
        <label for="profileBioInput">Bio</label>
        <textarea id="profileBioInput" name="bio" maxlength="${BIO_MAX_LENGTH}" placeholder="Tell other pilots about yourself">${escapeHTML(profile.bio)}</textarea>
        <div class="profile-bio-counter" id="profileBioCounter">${escapeHTML(String(remaining))} characters remaining</div>
      </div>
      <div class="profile-field">
        <span class="profile-field-label" id="profileAvatarColorLabel">Avatar color</span>
        <div class="profile-color-row" role="group" aria-labelledby="profileAvatarColorLabel">
          ${swatches}
          <input type="color" id="profileAvatarColorInput" name="avatar_color" value="${escapeHTML(selectedColor)}" aria-label="Custom avatar color">
        </div>
      </div>
      <div class="profile-field">
        <label class="profile-toggle"><input type="checkbox" id="profileShowBotsInput" name="show_bots_public"${profile.shows_bots ? ' checked' : ''}> Show my bots on my public profile</label>
      </div>
      <div class="profile-form-actions">
        <button type="submit" id="profileSaveBtn">Save profile</button>
        <span class="account-email-status" id="profileFormStatus" role="status" aria-live="polite"></span>
      </div>
    </form>
    ${renderProfilePreview(profile)}`;
  }

  root.ArenaAccountProfile = Object.freeze({
    accountProfileRoute,
    BIO_MAX_LENGTH,
    escapeHTML,
    normalizeProfile,
    previewChatHandle,
    renderPanel,
  });
})(typeof globalThis !== 'undefined' ? globalThis : window);
