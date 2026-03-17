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

const PERIOD_OPTIONS = {
  '1h': 'Last Hour',
  '24h': '24 Hours',
  '7d': '7 Days',
  '30d': '30 Days',
  all_time: 'All Time',
};

/** @type {string} */
let currentSort = 'elo';
/** @type {string} */
let currentPeriod = '1h';
/** @type {number|null} */
let refreshTimer = null;

/**
 * Fetch leaderboard data from the API.
 */
export async function fetchLeaderboard(sort = 'elo', limit = 50, period = 'all_time') {
  const baseUrl = window.location.origin;
  const resp = await fetch(`${baseUrl}/api/v1/leaderboard?sort=${sort}&limit=${limit}&period=${period}`);
  if (!resp.ok) throw new Error(`Leaderboard fetch failed: ${resp.status}`);
  return resp.json();
}

/**
 * Render leaderboard entries into a container.
 */
export function renderLeaderboard(data, tbody, sort) {
  tbody.innerHTML = '';
  const entries = data.entries || data.leaderboard || [];
  if (entries.length === 0) {
    const msg = currentPeriod === 'all_time'
      ? 'No bots qualified yet — get your first kill!'
      : `No data for this time period yet. Rounds need to complete to populate.`;
    tbody.innerHTML = `<tr><td colspan="5" style="text-align:center;color:var(--text-muted);padding:24px">${msg}</td></tr>`;
    return;
  }
  entries.forEach((entry, i) => {
    const rank = entry.rank || i + 1;
    const statValue = getStatValue(entry, sort);
    const kd = entry.deaths > 0 ? (entry.kills / entry.deaths).toFixed(1) : entry.kills + '.0';
    const tr = document.createElement('tr');
    tr.innerHTML = `<td${rank <= 3 ? ` class="rank-${rank}"` : ''}>${getRankDisplay(rank)}</td>
      <td>${escapeHtml(entry.name)}</td>
      <td>${statValue}</td>
      <td>${entry.kills}/${entry.deaths}</td>
      <td>${entry.elo}</td>`;
    if (rank <= 3) tr.className = `rank-${rank}`;
    tbody.appendChild(tr);
  });
}

/**
 * Initialize leaderboard tabs and auto-refresh.
 */
export function initLeaderboard(tabsContainer, tbody) {
  // Sort tabs
  Object.entries(SORT_OPTIONS).forEach(([key, label]) => {
    const btn = document.createElement('button');
    btn.textContent = label;
    btn.dataset.sort = key;
    if (key === currentSort) btn.classList.add('active');
    btn.addEventListener('click', () => switchSort(key, tabsContainer, tbody));
    tabsContainer.appendChild(btn);
  });

  // Period tabs — add a separator then period buttons
  const sep = document.createElement('span');
  sep.textContent = '|';
  sep.style.cssText = 'color:var(--text-muted);margin:0 6px;opacity:0.3;align-self:center';
  tabsContainer.appendChild(sep);

  Object.entries(PERIOD_OPTIONS).forEach(([key, label]) => {
    const btn = document.createElement('button');
    btn.textContent = label;
    btn.dataset.period = key;
    if (key === currentPeriod) btn.classList.add('active');
    btn.style.fontSize = '0.75rem';
    btn.addEventListener('click', () => switchPeriod(key, tabsContainer, tbody));
    tabsContainer.appendChild(btn);
  });

  refreshData(tbody);
  startAutoRefresh(tbody);
}

/** @private */
function switchSort(sort, tabsContainer, tbody) {
  currentSort = sort;
  tabsContainer.querySelectorAll('button[data-sort]').forEach(b => {
    b.classList.toggle('active', b.dataset.sort === sort);
  });
  refreshData(tbody);
}

/** @private */
function switchPeriod(period, tabsContainer, tbody) {
  currentPeriod = period;
  tabsContainer.querySelectorAll('button[data-period]').forEach(b => {
    b.classList.toggle('active', b.dataset.period === period);
  });
  refreshData(tbody);
}

/** @private */
async function refreshData(tbody) {
  try {
    const data = await fetchLeaderboard(currentSort, 50, currentPeriod);
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
    case 'kd_ratio': {
      const kd = entry.deaths > 0 ? entry.kills / entry.deaths : entry.kills;
      return kd.toFixed(2);
    }
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
