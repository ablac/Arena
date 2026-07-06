'use strict';

/**
 * Renders the Settings overlay: a master Reduced Motion switch plus one
 * expandable section per SETTINGS_SCHEMA entry, each with its own master
 * toggle and individual effect toggles.
 * @module settings-panel
 */

import {
  SETTINGS_SCHEMA,
  getState,
  isSectionEnabled,
  setSectionMaster,
  setEffect,
  onSettingsChange,
  resetToDefaults,
} from './settings.js';

// These two sections are exactly what the site's existing
// `prefers-reduced-motion` CSS media query already silences - the master
// switch here is a manual, per-user equivalent of that OS-level signal.
const REDUCED_MOTION_SECTIONS = ['killFlash', 'siteMotion'];

export function initSettingsPanel() {
  const container = document.getElementById('settings-sections');
  const reducedMotionCheckbox = document.getElementById('settings-reduced-motion');
  const resetBtn = document.getElementById('settings-reset');
  if (!container) return;

  container.innerHTML = Object.entries(SETTINGS_SCHEMA)
    .map(([sectionKey, section]) => renderSection(sectionKey, section))
    .join('');

  // A master checkbox lives inside <summary> so it sits next to the section
  // title, but a native <details> toggles open/closed on any click that
  // bubbles up through its <summary> - stop it here so flipping the master
  // switch doesn't also expand/collapse the section.
  container.querySelectorAll('.settings-master-checkbox').forEach((checkbox) => {
    checkbox.addEventListener('click', (e) => e.stopPropagation());
  });

  container.addEventListener('change', (e) => {
    const input = e.target;
    if (!(input instanceof HTMLInputElement) || input.type !== 'checkbox') return;
    const { section, effect } = input.dataset;
    if (!section) return;
    if (effect) setEffect(section, effect, input.checked);
    else setSectionMaster(section, input.checked);
  });

  reducedMotionCheckbox?.addEventListener('change', (e) => {
    const on = e.target.checked;
    REDUCED_MOTION_SECTIONS.forEach((key) => setSectionMaster(key, !on));
  });

  resetBtn?.addEventListener('click', () => resetToDefaults());

  syncAllInputs();
  onSettingsChange(syncAllInputs);

  function syncAllInputs() {
    const state = getState();
    container.querySelectorAll('input[type="checkbox"]').forEach((input) => {
      const { section, effect } = input.dataset;
      if (!section || !state[section]) return;
      input.checked = effect ? state[section].effects[effect] !== false : !!state[section].master;
    });
    if (reducedMotionCheckbox) {
      reducedMotionCheckbox.checked = REDUCED_MOTION_SECTIONS.every((key) => !isSectionEnabled(key));
    }
  }
}

function renderSection(sectionKey, section) {
  const effectRows = Object.entries(section.effects)
    .map(([effectKey, effect]) => `
      <label class="settings-toggle-row settings-toggle-effect">
        <span>${escapeHtml(effect.label)}</span>
        <input type="checkbox" data-section="${sectionKey}" data-effect="${effectKey}" />
      </label>
    `)
    .join('');

  return `
    <details class="card settings-section">
      <summary class="settings-section-summary">
        <span class="settings-section-title">${escapeHtml(section.label)}</span>
        <input type="checkbox" class="settings-master-checkbox" data-section="${sectionKey}" title="Turn all of ${escapeHtml(section.label)} on/off" />
      </summary>
      <p class="settings-section-desc">${escapeHtml(section.description)}</p>
      <div class="settings-section-effects">${effectRows}</div>
    </details>
  `;
}

function escapeHtml(value) {
  const div = document.createElement('div');
  div.textContent = value;
  return div.innerHTML;
}
