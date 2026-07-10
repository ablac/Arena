(function attachArenaAccountCosmetics(root) {
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

  function normalizeSession(payload) {
    const source = payload && typeof payload === 'object' ? payload : {};
    const rawAccount = source.account && typeof source.account === 'object'
      ? source.account
      : source;
    const email = cleanText(rawAccount.email).toLowerCase();
    const emailVerified = rawAccount.email_verified === true || Boolean(cleanText(rawAccount.email_verified_at));
    const authenticated = typeof source.authenticated === 'boolean'
      ? source.authenticated
      : Boolean(rawAccount.id && email);
    return {
      authenticated,
      login_enabled: source.login_enabled === true || authenticated,
      login_url: cleanText(source.login_url),
      logout_url: cleanText(source.logout_url),
      account: {
        id: cleanText(rawAccount.id),
        email,
        email_verified: emailVerified,
        name: cleanText(rawAccount.name || rawAccount.display_name),
      },
    };
  }

  function normalizeBot(raw) {
    const bot = raw && typeof raw === 'object' ? raw : {};
    return {
      id: cleanText(bot.id || bot.bot_id),
      name: cleanText(bot.name || bot.bot_name) || 'Unnamed bot',
      key_prefix: cleanText(bot.key_prefix || bot.api_key_prefix),
      key_is_active: bot.key_is_active !== false && bot.is_active !== false,
      linked_at: cleanText(bot.linked_at),
    };
  }

  function normalizeItem(raw) {
    const item = raw && typeof raw === 'object' ? raw : {};
    return {
      id: cleanText(item.id || item.cosmetic_id),
      name: cleanText(item.name) || 'Unnamed cosmetic',
      description: cleanText(item.description),
      slot: cleanText(item.slot),
      asset_key: cleanText(item.asset_key),
      rarity: cleanText(item.rarity) || 'common',
      is_active: item.is_active !== false,
    };
  }

  function normalizeLicense(raw) {
    const license = raw && typeof raw === 'object' ? raw : {};
    const item = normalizeItem(license.item || license.cosmetic || {
      id: license.cosmetic_id,
      name: license.name,
      description: license.description,
      slot: license.slot,
      asset_key: license.asset_key,
      rarity: license.rarity,
    });
    const assignment = license.assignment && typeof license.assignment === 'object'
      ? license.assignment
      : {};
    return {
      id: cleanText(license.id || license.license_id || license.entitlement_id),
      cosmetic_id: cleanText(license.cosmetic_id || item.id),
      item,
      assigned_bot_id: cleanText(license.assigned_bot_id || assignment.bot_id),
      assigned_bot_name: cleanText(license.assigned_bot_name || assignment.bot_name),
      assigned_at: cleanText(license.assigned_at || assignment.assigned_at),
      equipped: license.equipped === true || license.is_equipped === true,
      equipped_bot_id: cleanText(license.equipped_bot_id),
      status: cleanText(license.status).toLowerCase() || 'active',
    };
  }

  function normalizeSnapshot(payload) {
    const source = payload && typeof payload === 'object' ? payload : {};
    const session = normalizeSession({
      authenticated: true,
      account: source.account || {},
    });
    const bots = Array.isArray(source.bots) ? source.bots.map(normalizeBot).filter(bot => bot.id) : [];
    const licensesSource = Array.isArray(source.licenses)
      ? source.licenses
      : (Array.isArray(source.entitlements) ? source.entitlements : []);
    return {
      account: session.account,
      bots,
      licenses: licensesSource.map(normalizeLicense).filter(license => license.id),
      checkout_enabled: source.checkout_enabled === true,
    };
  }

  function assignmentIntent(snapshot, licenseID, botID) {
    const state = normalizeSnapshot(snapshot);
    if (!state.account.email || state.account.email_verified !== true) {
      return {ok: false, reason: 'verified-email-required'};
    }
    const license = state.licenses.find(entry => entry.id === licenseID);
    if (!license) return {ok: false, reason: 'license-not-found'};
    if (license.status !== 'active') return {ok: false, reason: 'license-inactive'};
    const bot = state.bots.find(entry => entry.id === botID);
    if (!bot) return {ok: false, reason: 'bot-not-linked'};
    if (!bot.key_is_active) return {ok: false, reason: 'bot-key-inactive'};
    if (license.assigned_bot_id === bot.id) {
      return {ok: false, reason: 'already-assigned'};
    }
    return {
      ok: true,
      kind: license.assigned_bot_id ? 'move' : 'assign',
      license_id: license.id,
      bot_id: bot.id,
      previous_bot_id: license.assigned_bot_id || null,
    };
  }

  function slotLabel(slot) {
    const labels = {
      bot_skin: 'Bot skins',
      weapon_skin: 'Weapon designs',
      attachment: 'Attachments',
    };
    return labels[slot] || (slot ? slot.replaceAll('_', ' ') : 'Cosmetics');
  }

  function accountRoute(name, id) {
    const encoded = id ? encodeURIComponent(String(id)) : '';
    const routes = {
      session: '/account/session',
      cosmetics: '/account/cosmetics',
      bots: '/account/bots',
      bot: `/account/bots/${encoded}`,
      equip: `/account/bots/${encoded}/cosmetics`,
      assignment: `/account/cosmetic-licenses/${encoded}/assignment`,
    };
    if (!Object.hasOwn(routes, name)) throw new Error(`unknown account route: ${name}`);
    return routes[name];
  }

  function requestHeaders(method, csrfToken, hasBody) {
    const normalizedMethod = String(method || 'GET').toUpperCase();
    const headers = {Accept: 'application/json'};
    if (hasBody) headers['Content-Type'] = 'application/json';
    if (normalizedMethod !== 'GET' && normalizedMethod !== 'HEAD' && csrfToken) {
      headers['X-CSRF-Token'] = csrfToken;
    }
    return headers;
  }

  function renderBotOption(bot, currentlyAssigned) {
    const disabled = currentlyAssigned || !bot.key_is_active;
    const suffix = currentlyAssigned ? ' (currently assigned)' : (!bot.key_is_active ? ' (key inactive)' : '');
    const identity = bot.key_prefix ? ` - ${bot.key_prefix}...` : ` - ${bot.id.slice(0, 8)}`;
    return `<option value="${escapeHTML(bot.id)}"${disabled ? ' disabled' : ''}>${escapeHTML(bot.name + identity)}${suffix}</option>`;
  }

  function renderLicense(license, snapshot, busyLicenseID) {
    const assignedBot = snapshot.bots.find(bot => bot.id === license.assigned_bot_id);
    const assignedName = assignedBot?.name || license.assigned_bot_name || '';
    const assignedIdentity = assignedBot
      ? `${assignedName} (${assignedBot.key_prefix ? assignedBot.key_prefix + '...' : assignedBot.id.slice(0, 8)})`
      : assignedName;
    const isBusy = busyLicenseID === license.id;
    const activeLicense = license.status === 'active';
    const assignableBots = snapshot.bots.filter(bot => bot.id !== license.assigned_bot_id && bot.key_is_active);
    const options = snapshot.bots.map(bot => renderBotOption(bot, bot.id === license.assigned_bot_id)).join('');
    const assignedCopy = assignedIdentity
      ? `Assigned to <strong>${escapeHTML(assignedIdentity)}</strong>`
      : 'Not assigned to a bot';
    const actionLabel = license.assigned_bot_id ? 'Move to bot' : 'Assign to bot';
    const assignedBotActive = assignedBot?.key_is_active === true;
    const equipped = license.equipped === true && (!license.equipped_bot_id || license.equipped_bot_id === license.assigned_bot_id);
    const equippedCopy = !activeLicense
      ? `License ${escapeHTML(license.status)}; it cannot be assigned or equipped`
      : equipped
      ? `Equipped on <strong>${escapeHTML(assignedIdentity || 'linked bot')}</strong>`
      : (license.assigned_bot_id ? 'Assigned, not equipped' : 'Assign this license before equipping it');
    const statusLabel = activeLicense ? 'Account owned' : license.status.charAt(0).toUpperCase() + license.status.slice(1);

    return `<article class="cosmetic-license" data-license-id="${escapeHTML(license.id)}">
      <div class="cosmetic-license-head">
        <div>
          <div class="cosmetic-kicker">${escapeHTML(slotLabel(license.item.slot))} - ${escapeHTML(license.item.rarity)}</div>
          <h3>${escapeHTML(license.item.name)}</h3>
        </div>
        <span class="ownership-badge${activeLicense ? '' : ' inactive'}">${escapeHTML(statusLabel)}</span>
      </div>
      <p>${escapeHTML(license.item.description || 'Visual customization only. No gameplay advantage.')}</p>
      <div class="cosmetic-assignment${license.assigned_bot_id ? ' assigned' : ''}">${assignedCopy}</div>
      <div class="cosmetic-equip-state${equipped ? ' equipped' : ''}">${equippedCopy}</div>
      <div class="cosmetic-license-actions">
        <select data-license-target="${escapeHTML(license.id)}" aria-label="Bot for ${escapeHTML(license.item.name)}"${activeLicense && assignableBots.length && !isBusy ? '' : ' disabled'}>
          ${snapshot.bots.length ? `<option value="">${license.assigned_bot_id ? 'Choose another bot...' : 'Choose a linked bot...'}</option>${options}` : '<option value="">Link a bot first</option>'}
        </select>
        <button class="sm" data-license-assign="${escapeHTML(license.id)}" disabled>${isBusy ? 'Saving...' : actionLabel}</button>
        ${activeLicense && license.assigned_bot_id && !equipped ? `<button class="sm cosmetic-equip" data-license-equip="${escapeHTML(license.id)}"${isBusy || !license.item.is_active || !assignedBotActive ? ' disabled' : ''}>${isBusy ? 'Saving...' : (assignedBotActive ? 'Equip on bot' : 'Bot key inactive')}</button>` : ''}
        ${activeLicense && license.assigned_bot_id ? `<button class="sm danger" data-license-unassign="${escapeHTML(license.id)}"${isBusy ? ' disabled' : ''}>Remove from bot</button>` : ''}
      </div>
      <div class="cosmetic-license-id">License ${escapeHTML(license.id)}</div>
    </article>`;
  }

  function renderPanel(rawSnapshot, options) {
    const snapshot = normalizeSnapshot(rawSnapshot);
    const view = options && typeof options === 'object' ? options : {};
    const email = snapshot.account.email || 'your verified email';
    const botRows = snapshot.bots.length
      ? snapshot.bots.map(bot => `<li data-linked-bot-id="${escapeHTML(bot.id)}">
          <span><strong>${escapeHTML(bot.name)}</strong>${bot.key_prefix ? `<small>${escapeHTML(bot.key_prefix)}...</small>` : ''}</span>
          <span class="linked-bot-actions"><span class="linked-bot-state${bot.key_is_active ? '' : ' inactive'}">${bot.key_is_active ? 'Linked' : 'Key inactive'}</span><button type="button" class="sm danger" data-bot-unlink="${escapeHTML(bot.id)}">Unlink</button></span>
        </li>`).join('')
      : '<li class="cosmetic-empty">No bots linked yet. Link one by proving its API key below.</li>';

    const groups = new Map();
    snapshot.licenses.forEach(license => {
      const slot = license.item.slot || 'other';
      if (!groups.has(slot)) groups.set(slot, []);
      groups.get(slot).push(license);
    });
    const inventory = groups.size
      ? [...groups.entries()].map(([slot, licenses]) => `<section class="cosmetic-group">
          <h3>${escapeHTML(slotLabel(slot))}</h3>
          <div class="cosmetic-license-grid">${licenses.map(license => renderLicense(license, snapshot, view.busyLicenseID)).join('')}</div>
        </section>`).join('')
      : '<div class="cosmetic-empty cosmetic-empty-inventory">No cosmetic licenses are on this account yet.</div>';
    const activeCount = snapshot.licenses.filter(license => license.status === 'active').length;
    const inactiveCount = snapshot.licenses.length - activeCount;
    const licenseSummary = `${activeCount} active${inactiveCount ? ` / ${inactiveCount} inactive` : ''}`;

    return `<div class="cosmetic-account-summary">
      <div>
        <div class="cosmetic-kicker">Verified owner</div>
        <h2>${escapeHTML(email)}</h2>
        <p>Purchases stay with this account even if a bot API key is rotated, revoked, or lost.</p>
      </div>
      <span class="verified-email-badge">Email verified</span>
    </div>
    ${view.error ? `<div class="tip warn" role="alert"><b>Could not update cosmetics:</b> ${escapeHTML(view.error)}</div>` : ''}
    ${view.notice ? `<div class="tip good" role="status"><b>Saved:</b> ${escapeHTML(view.notice)}</div>` : ''}
    <div class="cosmetic-layout">
      <section class="cosmetic-sidebar">
        <h3>Linked bots</h3>
        <p>Linking proves that you control a bot. It does not transfer cosmetic ownership to the key.</p>
        <ul class="linked-bot-list">${botRows}</ul>
        <form id="linkBotForm" class="link-bot-form">
          <label for="linkBotKey">Link another bot</label>
          <input type="password" id="linkBotKey" name="api_key" placeholder="arena_..." autocomplete="off" spellcheck="false" required>
          <button type="submit"${view.linkBusy ? ' disabled' : ''}>${view.linkBusy ? 'Linking...' : 'Verify & link bot'}</button>
          <small>The key is sent once to prove control, then cleared from this form. The purchase remains with ${escapeHTML(email)}.</small>
        </form>
      </section>
      <section class="cosmetic-inventory">
        <div class="cosmetic-inventory-head">
          <div><div class="cosmetic-kicker">Your collection</div><h2>Cosmetic licenses</h2></div>
          <span>${escapeHTML(licenseSummary)}</span>
        </div>
        <p class="cosmetic-rule">Each license can be assigned to one bot at a time. Moving changes which bot may use it; Equip is a separate, explicit action that can replace that bot's active cosmetic in the same slot.</p>
        ${inventory}
      </section>
    </div>`;
  }

  root.ArenaAccountCosmetics = Object.freeze({
    accountRoute,
    assignmentIntent,
    escapeHTML,
    normalizeSession,
    normalizeSnapshot,
    renderPanel,
    requestHeaders,
    slotLabel,
  });
})(typeof globalThis !== 'undefined' ? globalThis : window);
