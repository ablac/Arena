'use strict';

/**
 * Leaderboard fetching, rendering, and tab switching.
 * @module leaderboard
 */

const SORT_OPTIONS = {
  elo: 'ELO Rating',
  kills: 'Most Kills',
  kd_ratio: 'K/D Ratio',
  streak: 'Best Streak',
};

const WEAPON_ICONS = {
  sword: '\u2694\uFE0F', bow: '\uD83C\uDFF9', daggers: '\uD83D\uDDE1\uFE0F',
  shield: '\uD83D\uDEE1\uFE0F', spear: '\uD83D\uDD31', staff: '\uD83E\uDE84',
};

/** @type {string} */
let currentSort = 'elo';
/** @type {number|null} */
let refreshTimer = null;

/**
 * Fetch leaderboard data from the API.
 * @param {string} sort - Sort field
 * @param {number} limit - Number of entries
 * @returns {Promise<Object>}
 */
export async function fetchLeaderboard(sort = 'elo', limit = 50) {
  const baseUrl = window.location.origin;
  const resp = await fetch(`${baseUrl}/api/v1/leaderboard?sort=${sort}&limit=${limit}`);
  if (!resp.ok) throw new Error(`Leaderboard fetch failed: ${resp.status}`);
  return resp.json();
}

/**
 * Render leaderboard entries into a container.
 * @param {Object} data - API response with entries array
 * @param {HTMLElement} tbody - Table body element
 * @param {string} sort - Current sort field
 */
export function renderLeaderboard(data, tbody, sort) {
  tbody.innerHTML = '';
  if (!data.entries || data.entries.length === 0) {
    tbody.innerHTML = '<tr><td colspan="5" style="text-align:center;color:var(--text-muted);padding:24px">No bots qualified yet — get your first kill!</td></tr>';
    return;
  }
  data.entries.forEach((entry, i) => {
    const rank = entry.rank || i + 1;
    const rankClass = rank <= 3 ? ` class="rank-${rank}"` : '';
    const statValue = getStatValue(entry, sort);
    const tr = document.createElement('tr');
    tr.innerHTML = `<td${rankClass ? ` class="rank-${rank}"` : ''}>${getRankDisplay(rank)}</td>
      <td>${escapeHtml(entry.name)}</td>
      <td>${statValue}</td>
      <td>${entry.kills}/${entry.deaths}</td>
      <td>${entry.elo}</td>`;
    if (rankClass) tr.className = `rank-${rank}`;
    tbody.appendChild(tr);
  });
}

/**
 * Initialize leaderboard tabs and auto-refresh.
 * @param {HTMLElement} tabsContainer - Tabs container element
 * @param {HTMLElement} tbody - Table body element
 */
export function initLeaderboard(tabsContainer, tbody) {
  Object.entries(SORT_OPTIONS).forEach(([key, label]) => {
    const btn = document.createElement('button');
    btn.textContent = label;
    btn.dataset.sort = key;
    if (key === currentSort) btn.classList.add('active');
    btn.addEventListener('click', () => switchTab(key, tabsContainer, tbody));
    tabsContainer.appendChild(btn);
  });
  refreshData(tbody);
  startAutoRefresh(tbody);
}

/** @private */
function switchTab(sort, tabsContainer, tbody) {
  currentSort = sort;
  tabsContainer.querySelectorAll('button').forEach(b => {
    b.classList.toggle('active', b.dataset.sort === sort);
  });
  refreshData(tbody);
}

/** @private */
async function refreshData(tbody) {
  try {
    const data = await fetchLeaderboard(currentSort);
    renderLeaderboard(data, tbody, currentSort);
  } catch (err) {
    console.error('[Leaderboard] Fetch error:', err);
  }
}

/** @private */
function startAutoRefresh(tbody) {
  if (refreshTimer) clearInterval(refreshTimer);
  refreshTimer = setInterval(() => refreshData(tbody), 30000);
}

/** @private */
function getStatValue(entry, sort) {
  switch (sort) {
    case 'kills': return entry.kills;
    case 'kd_ratio': return (entry.kd_ratio || 0).toFixed(2);
    case 'streak': return entry.best_streak;
    default: return entry.elo;
  }
}

/** @private */
function getRankDisplay(rank) {
  if (rank === 1) return '\uD83E\uDD47 1';
  if (rank === 2) return '\uD83E\uDD48 2';
  if (rank === 3) return '\uD83E\uDD49 3';
  return `#${rank}`;
}

/** @private */
function escapeHtml(str) {
  const div = document.createElement('div');
  div.textContent = str;
  return div.innerHTML;
}
