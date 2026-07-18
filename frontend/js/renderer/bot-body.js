'use strict';

/**
 * Forge bot entry construction plus scene-owned HUD/shadow resources.
 * @module renderer/bot-body
 */

import {createForgeCharacter, disposeForgeCharacter} from './character-rig.js?v=20260714e';

const SHADOW_RADIUS = 6.5;

/** Shared scene resources, rebuilt when the Arena scene changes. */
let _shdMat = null;
let _tplShadow = null;

/** Singleton fullscreen GUI texture for all bot HUD elements. */
let _guiTexture = null;

/**
 * Get or create the singleton AdvancedDynamicTexture for bot GUI overlays.
 * @returns {BABYLON.GUI.AdvancedDynamicTexture}
 */
export function getGuiTexture() {
  // Babylon textures have no `isDisposed` member. Validate the cached
  // texture through its owning scene so a rebuilt Arena gets a fresh HUD.
  const guiScene = _guiTexture ? _guiTexture.getScene() : null;
  if (!_guiTexture || !guiScene || guiScene.isDisposed) {
    const GUI = window.BABYLON.GUI;
    _guiTexture = GUI.AdvancedDynamicTexture.CreateFullscreenUI('botUI');
    // The per-bot name labels/HP bars are linked to meshes that move every
    // rendered frame, so this fullscreen ADT re-rasterizes and re-uploads
    // its whole canvas per frame (~8 MB/frame at 1080p). Half render scale
    // quarters that raster+upload cost at the price of slightly softer
    // overlay text. Mitigation, not the fix — the real fix (migrating
    // labels/HP/taunts to world-space billboards) is tracked in issue #166.
    _guiTexture.renderScale = 0.5;
  }
  return _guiTexture;
}

function getShadowMaterial(scene) {
  if (!_shdMat || _shdMat.getScene() !== scene) {
    const B = window.BABYLON;
    _shdMat = new B.StandardMaterial('smat-shared', scene);
    _shdMat.diffuseColor = new B.Color3(0, 0, 0);
    _shdMat.specularColor = B.Color3.Black();
    _shdMat.emissiveColor = B.Color3.Black();
    _shdMat.disableLighting = true;
    _shdMat.alpha = 0.3;
    _shdMat.backFaceCulling = false;
    _shdMat.freeze();
  }
  return _shdMat;
}

/** Get the scene-owned shadow template used by all Forge instances. */
export function _getTplShadow(scene) {
  if (!_tplShadow || _tplShadow.isDisposed() || _tplShadow.getScene() !== scene) {
    const B = window.BABYLON;
    _tplShadow = B.MeshBuilder.CreateDisc('tpl-shadow', {
      radius: SHADOW_RADIUS,
      tessellation: 6,
    }, scene);
    _tplShadow.rotation.x = Math.PI / 2;
    _tplShadow.setEnabled(false);
    _tplShadow.isPickable = false;
    _tplShadow.material = getShadowMaterial(scene);
  }
  return _tplShadow;
}

/** Build the one production character system used by live and Shop scenes. */
export function createBotEntry(bot, scene, options = {}) {
  const presentationOnly = options.presentationOnly === true;
  return createForgeCharacter(bot, scene, {
    ...options,
    guiTexture: options.guiTexture || (presentationOnly ? null : getGuiTexture()),
    shadowTemplate: options.shadowTemplate || _getTplShadow(scene),
  });
}

/** Dispose a production Forge entry without touching scene-owned templates. */
export function disposeBotEntry(entry) {
  disposeForgeCharacter(entry);
}

/** Set HP bar color based on health ratio. */
export function setHpColor(fill, ratio) {
  if (ratio > 0.6) {
    fill.background = '#00ff00';
  } else if (ratio > 0.3) {
    fill.background = '#ffff00';
  } else {
    fill.background = '#ff0000';
  }
}
