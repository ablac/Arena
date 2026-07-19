'use strict';

/**
 * Forge bot entry construction plus scene-owned selection/shadow resources.
 * @module renderer/bot-body
 */

import {createForgeCharacter, disposeForgeCharacter} from './character-rig.js?v=20260718n';

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
    // Do NOT set renderScale here as an upload-cost mitigation: without
    // idealWidth/idealHeight the ADT maps control pixel sizes (fontSize,
    // "60px" bars, linkOffsetY) onto the scaled-down texture and stretches
    // it fullscreen, so renderScale 0.5 doubled every label and HP bar on
    // screen (shipped and reverted, 2026-07-18). The per-frame re-upload
    // cost is tracked in issue #166; the fix is the world-space billboard
    // migration (or renderScale paired with ideal dimensions, verified on a
    // live render).
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
    shadowTemplate: options.shadowTemplate || _getTplShadow(scene),
  });
}

/** Dispose a production Forge entry without touching scene-owned templates. */
export function disposeBotEntry(entry) {
  disposeForgeCharacter(entry);
}
