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
      email_login_enabled: source.email_login_enabled === true,
      oidc_login_enabled: source.oidc_login_enabled === true,
      login_url: cleanText(source.login_url),
      logout_url: cleanText(source.logout_url),
      email_start_url: cleanText(source.email_start_url),
      email_verify_url: cleanText(source.email_verify_url),
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

  function normalizeCatalog(payload) {
    const source = payload && typeof payload === 'object' ? payload : {};
    return {
      checkout_enabled: source.checkout_enabled === true,
      categories: Array.isArray(source.categories) ? source.categories : [],
      items: Array.isArray(source.items) ? source.items : [],
      packs: Array.isArray(source.packs) ? source.packs.filter(pack => pack && typeof pack === 'object') : [],
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
      checkout: '/account/cosmetics/checkout',
      orders: '/account/cosmetics/orders',
      bots: '/account/bots',
      bot: `/account/bots/${encoded}`,
      equip: `/account/bots/${encoded}/cosmetics`,
      assignment: `/account/cosmetic-licenses/${encoded}/assignment`,
    };
    if (!Object.hasOwn(routes, name)) throw new Error(`unknown account route: ${name}`);
    return routes[name];
  }

  function checkoutIntent(rawCatalog, packID) {
    const catalog = normalizeCatalog(rawCatalog);
    const normalizedID = cleanText(packID);
    if (!catalog.checkout_enabled) return {ok: false, reason: 'checkout-disabled'};
    if (!normalizedID) return {ok: false, reason: 'pack-not-found'};
    const pack = catalog.packs.find(entry => cleanText(entry.id) === normalizedID);
    if (!pack) return {ok: false, reason: 'pack-not-found'};
    if (pack.is_purchasable !== true) return {ok: false, reason: 'pack-not-purchasable'};
    return {
      ok: true,
      path: accountRoute('checkout'),
      body: {pack_id: normalizedID, quantity: 1},
    };
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

  function formatPrice(item) {
    if (item?.is_free === true) return 'Free';
    const rawCents = Number(item?.price_cents || 0);
    const cents = Number.isFinite(rawCents) && rawCents >= 0 ? Math.round(rawCents) : 0;
    const currency = cleanText(item?.currency) || 'USD';
    try {
      return new Intl.NumberFormat(undefined, {style: 'currency', currency}).format(cents / 100);
    } catch (_) {
      return `$${(cents / 100).toFixed(2)}`;
    }
  }

  function shopSwatch(pack) {
    const firstItem = Array.isArray(pack?.items) ? pack.items[0] : null;
    const helper = root.ArenaCosmeticThemes;
    if (!helper || typeof helper.swatchStyle !== 'function') return '';
    return helper.swatchStyle(cleanText(firstItem?.asset_key));
  }

  function renderShopPack(pack, catalog, checkoutState) {
    const packID = cleanText(pack.id);
    const pending = checkoutState?.status === 'pending' && checkoutState.packID === packID;
    const purchasable = catalog.checkout_enabled && pack.is_purchasable === true;
    const items = Array.isArray(pack.items) ? pack.items.slice(0, 3) : [];
    const contents = items.length
      ? items.map(item => `<span>${escapeHTML(item.name || item.id || 'Cosmetic')}</span>`).join('')
      : '<span>Three-piece set</span>';
    const swatch = shopSwatch(pack);
    const swatchAttribute = swatch ? ` style="background:${escapeHTML(swatch)}"` : '';
    const action = purchasable
      ? `<button type="button" class="sm cosmetic-shop-buy" data-pack-checkout="${escapeHTML(packID)}"${pending ? ' disabled' : ''}>${pending ? 'Opening checkout...' : `Buy ${escapeHTML(formatPrice(pack))}`}</button>`
      : '<span class="cosmetic-shop-state">Checkout coming soon</span>';
    return `<article class="cosmetic-shop-pack" data-shop-pack="${escapeHTML(packID)}">
      <div class="cosmetic-shop-swatch" aria-hidden="true"${swatchAttribute}></div>
      <div class="cosmetic-shop-pack-copy">
        <div class="cosmetic-kicker">Coordinated set</div>
        <h3>${escapeHTML(pack.name || packID || 'Arena set')}</h3>
        <p>${escapeHTML(pack.description || 'Three presentation-only cosmetics with no gameplay advantage.')}</p>
        <div class="cosmetic-shop-contents">${contents}</div>
      </div>
      <div class="cosmetic-shop-offer"><strong>${escapeHTML(formatPrice(pack))}</strong>${action}</div>
    </article>`;
  }

  function renderSetShop(view) {
    const catalog = view.catalog ? normalizeCatalog(view.catalog) : null;
    const query = cleanText(view.shopQuery);
    const checkoutState = view.checkoutState || {status: 'idle', packID: '', message: ''};
    let feedback = '';
    if (checkoutState.status === 'success') {
      feedback = `<div class="tip" role="status"><b>Checkout returned.</b> Payment is still processing. New licenses appear only after Arena verifies Stripe's signed payment event.</div>`;
    } else if (checkoutState.status === 'cancelled') {
      feedback = `<div class="tip" role="status"><b>Checkout cancelled.</b> Your collection was not changed.</div>`;
    } else if (checkoutState.status === 'error') {
      feedback = `<div class="tip warn" role="alert"><b>Checkout could not start:</b> ${escapeHTML(checkoutState.message || 'Try again in a moment.')}</div>`;
    } else if (checkoutState.status === 'disabled') {
      feedback = `<div class="tip" role="status"><b>Checkout is not open yet.</b> Preview the sets now and return when sales are enabled.</div>`;
    }

    if (view.catalogError) {
      return `<section class="cosmetic-shop" aria-labelledby="cosmetic-shop-title">
        <div class="cosmetic-inventory-head"><div><div class="cosmetic-kicker">Set shop</div><h2 id="cosmetic-shop-title">Cosmetic sets</h2></div></div>
        ${feedback}<div class="tip warn" role="alert"><b>Shop unavailable:</b> ${escapeHTML(view.catalogError)}</div>
      </section>`;
    }
    if (!catalog) {
      return `<section class="cosmetic-shop" aria-labelledby="cosmetic-shop-title">
        <div class="cosmetic-inventory-head"><div><div class="cosmetic-kicker">Set shop</div><h2 id="cosmetic-shop-title">Cosmetic sets</h2></div></div>
        ${feedback}<div class="cosmetic-loading">Loading cosmetic sets...</div>
      </section>`;
    }

    const normalizedQuery = query.toLowerCase();
    const matches = catalog.packs.filter(pack => {
      if (!normalizedQuery) return true;
      const itemText = (Array.isArray(pack.items) ? pack.items : []).flatMap(item => [item.id, item.name, item.description]);
      return [pack.id, pack.name, pack.description, ...itemText]
        .filter(Boolean).join(' ').toLowerCase().includes(normalizedQuery);
    });
    const visible = matches.slice(0, 12);
    const summary = matches.length
      ? `Showing ${visible.length} of ${matches.length} sets`
      : 'No cosmetic sets match';
    const packs = visible.length
      ? visible.map(pack => renderShopPack(pack, catalog, checkoutState)).join('')
      : `<div class="cosmetic-empty cosmetic-empty-inventory">No cosmetic sets match "${escapeHTML(query)}". Try a theme, set number, or item name.</div>`;

    return `<section class="cosmetic-shop" aria-labelledby="cosmetic-shop-title">
      <div class="cosmetic-inventory-head">
        <div><div class="cosmetic-kicker">Set shop</div><h2 id="cosmetic-shop-title">Cosmetic sets</h2></div>
        <span>${escapeHTML(summary)}</span>
      </div>
      <p class="cosmetic-rule">Buying a pack grants one license for every included item. Each purchased item copy can be assigned to one bot at a time; items from the same pack can be assigned to different bots.</p>
      ${feedback}
      <label class="cosmetic-shop-search" for="accountCosmeticSearch">Find a set
        <input type="search" id="accountCosmeticSearch" data-account-shop-search value="${escapeHTML(query)}" placeholder="Search name, number, or item" autocomplete="off">
      </label>
      <div class="cosmetic-shop-grid">${packs}</div>
    </section>`;
  }

  function formatOrderUSD(rawCents) {
    const amount = Number(rawCents);
    const cents = Number.isFinite(amount) && amount >= 0 ? Math.round(amount) : 0;
    try {
      return new Intl.NumberFormat('en-US', {style: 'currency', currency: 'USD'}).format(cents / 100);
    } catch (_) {
      return `$${(cents / 100).toFixed(2)}`;
    }
  }

  function orderStatusMeta(rawStatus) {
    const status = cleanText(rawStatus).toLowerCase() || 'created';
    const labels = {
      created: 'Checkout pending',
      checkout_pending: 'Checkout pending',
      processing: 'Processing',
      paid: 'Paid',
      refund_review: 'Refund review',
      refunded: 'Refunded',
      disputed: 'Disputed',
      expired: 'Expired',
      payment_failed: 'Failed',
      failed: 'Failed',
    };
    const warnings = new Set(['refund_review', 'refunded', 'disputed', 'expired', 'payment_failed', 'failed']);
    return {
      status,
      label: labels[status] || 'Unknown',
      className: status === 'paid' ? ' is-paid' : (warnings.has(status) ? ' is-warning' : ''),
    };
  }

  function orderCreatedTime(rawTime) {
    const raw = cleanText(rawTime);
    const date = new Date(raw);
    if (!raw || Number.isNaN(date.getTime())) return {iso: '', label: 'Time unavailable'};
    return {
      iso: date.toISOString(),
      label: date.toLocaleString(undefined, {
        year: 'numeric', month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit',
      }),
    };
  }

  function renderPurchaseOrder(rawOrder) {
    const order = rawOrder && typeof rawOrder === 'object' ? rawOrder : {};
    const status = orderStatusMeta(order.status);
    const rawQuantity = Number(order.quantity);
    const quantity = Number.isFinite(rawQuantity) && rawQuantity >= 0 ? Math.floor(rawQuantity) : 0;
    const rawFulfilled = Number(order.fulfilled_license_count);
    const fulfilled = Number.isFinite(rawFulfilled) && rawFulfilled >= 0 ? Math.floor(rawFulfilled) : 0;
    const orderID = cleanText(order.id) || 'Unknown order';
    const packName = cleanText(order.pack_name || order.pack_id) || 'Unknown pack';
    const created = orderCreatedTime(order.created_at);
    const createdHTML = created.iso
      ? `<time datetime="${escapeHTML(created.iso)}">${escapeHTML(created.label)}</time>`
      : escapeHTML(created.label);
    return `<article class="cosmetic-purchase" data-purchase-order="${escapeHTML(orderID)}" data-order-status="${escapeHTML(status.status)}">
      <div class="cosmetic-purchase-head">
        <div><div class="cosmetic-kicker">Order <code>${escapeHTML(orderID)}</code></div><h3>${escapeHTML(packName)}</h3></div>
        <span class="cosmetic-purchase-status${status.className}">${escapeHTML(status.label)}</span>
      </div>
      <div class="cosmetic-purchase-facts"><span>Quantity ${quantity}</span><span>${fulfilled} ${fulfilled === 1 ? 'license' : 'licenses'} fulfilled</span>${createdHTML}</div>
      <div class="cosmetic-purchase-money">
        <span>Expected <strong>${escapeHTML(formatOrderUSD(order.expected_subtotal_cents))}</strong></span>
        <span>Received <strong>${escapeHTML(formatOrderUSD(order.amount_received_cents))}</strong></span>
        <span>Refunded <strong>${escapeHTML(formatOrderUSD(order.amount_refunded_cents))}</strong></span>
      </div>
    </article>`;
  }

  function renderRecentPurchases(view) {
    let body = '';
    if (view.ordersError) {
      body = `<div class="tip warn" role="alert"><b>Recent purchases unavailable:</b> ${escapeHTML(view.ordersError)} Owned cosmetics and the shop are unaffected.</div>`;
    } else if (!Array.isArray(view.orders)) {
      body = '<div class="cosmetic-loading" aria-busy="true">Loading recent purchases...</div>';
    } else if (!view.orders.length) {
      body = '<div class="cosmetic-empty cosmetic-empty-inventory">No purchases yet.</div>';
    } else {
      body = `<div class="cosmetic-purchase-list">${view.orders.slice(0, 20).map(renderPurchaseOrder).join('')}</div>`;
    }
    return `<section class="cosmetic-purchases" aria-labelledby="cosmetic-purchases-title">
      <div class="cosmetic-inventory-head">
        <div><div class="cosmetic-kicker">Account ledger</div><h2 id="cosmetic-purchases-title">Recent purchases</h2></div>
        <span>Latest 20</span>
      </div>
      <p class="cosmetic-rule">Statuses come from Arena's signed payment ledger. Returning from checkout does not mark an order paid.</p>
      ${body}
    </section>`;
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
    ${renderSetShop(view)}
    ${renderRecentPurchases(view)}
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
        <p class="cosmetic-rule">Every purchased pack item appears here as its own license. Each license can be assigned to one bot at a time. Items from the same pack may be assigned to different bots. Moving changes which bot may use a license; Equip is a separate, explicit action that can replace that bot's active cosmetic in the same slot.</p>
        ${inventory}
      </section>
    </div>`;
  }

  root.ArenaAccountCosmetics = Object.freeze({
    accountRoute,
    assignmentIntent,
    checkoutIntent,
    escapeHTML,
    normalizeCatalog,
    normalizeSession,
    normalizeSnapshot,
    renderPanel,
    requestHeaders,
    slotLabel,
  });
})(typeof globalThis !== 'undefined' ? globalThis : window);
