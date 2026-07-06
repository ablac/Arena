'use strict';

/**
 * Swordsman animation system — keyframe-driven HEMA sword animations.
 *
 * Port of the Three.js CharacterAnimator to work with Babylon.js TransformNodes.
 * The keyframe interpolation is pure math (no engine dependency). Poses are
 * applied to the joint hierarchy built by swordsman-body.js.
 *
 * Supports:
 *   - 4 HEMA guard stances (Pflug, Vom Tag, Ochs, Alber)
 *   - 3 attack types per stance (slash, backhand, thrust)
 *   - Idle breathing + arm sway
 *   - Walk cycle (leg swing, knee bend, body bob)
 *   - Death/respawn/dodge (delegated to base anim system)
 *
 * Attack timing is compressed to match the Arena's 0.5s sword cooldown.
 *
 * @module renderer/swordsman-anims
 */

// Circular with swordsman-body.js (it imports SwordsmanAnimState from here);
// safe because both sides only use the other's export at runtime, never at
// module top level. The ?v= MUST match every other import of that module.
import { makeSwordTrail } from './swordsman-body.js?v=20260707a';
import { isEnabled } from '../settings.js';

const DEG = Math.PI / 180;
const ATTACK_DURATION = 0.50; // Match server sword cooldown (0.5s)

// ─── Smoothstep helper (same as editor) ─────────────────────────────────────
function smoothstep(t) {
  return t * t * (3 - 2 * t);
}

// ─── Exponential lerp (same as base animations.js) ──────────────────────────
function elerp(current, target, rate, dt) {
  return current + (target - current) * (1 - Math.exp(-rate * dt));
}

// ─── Keyframe interpolation (direct port from CharacterAnimator.ts) ─────────

// A given `keyframes` array (ATTACK_ANIMS[stance][attack].kf) is a static,
// never-mutated reference shared by every bot playing that attack, so the
// union of pose keys for each adjacent (kf0, kf1) bracket is cached per
// array instead of rebuilt via Set+spread every frame per attacking bot.
const _bracketKeysCache = new WeakMap();
function _getBracketKeys(keyframes, i, kf0, kf1) {
  let perArray = _bracketKeysCache.get(keyframes);
  if (!perArray) {
    perArray = [];
    _bracketKeysCache.set(keyframes, perArray);
  }
  let keys = perArray[i];
  if (!keys) {
    keys = Array.from(new Set([...Object.keys(kf0.pose), ...Object.keys(kf1.pose)]));
    perArray[i] = keys;
  }
  return keys;
}

/**
 * Interpolate a pose from a keyframe array at normalized time p (0..1).
 * Keyframes are sorted by t. Returns a dict of "partName.axis" -> degrees.
 *
 * @param {number} p - Normalized progress (0..1)
 * @param {Array<{t: number, pose: Object}>} keyframes - Sorted keyframe array
 * @returns {Object} Interpolated pose dict (keys like "rightArm.x", values in degrees)
 */
function interpolateKeyframes(p, keyframes) {
  if (!keyframes || keyframes.length === 0) return {};
  if (keyframes.length === 1) return { ...keyframes[0].pose };

  // Clamp
  if (p <= keyframes[0].t) return { ...keyframes[0].pose };
  if (p >= keyframes[keyframes.length - 1].t) return { ...keyframes[keyframes.length - 1].pose };

  // Find bracketing keyframes
  let i = 0;
  for (; i < keyframes.length - 1; i++) {
    if (p >= keyframes[i].t && p <= keyframes[i + 1].t) break;
  }

  const kf0 = keyframes[i];
  const kf1 = keyframes[i + 1];
  const span = kf1.t - kf0.t;
  const localT = span > 0 ? (p - kf0.t) / span : 0;
  const s = smoothstep(localT);

  // Interpolate all keys present in either keyframe
  const result = {};
  const allKeys = _getBracketKeys(keyframes, i, kf0, kf1);
  for (const key of allKeys) {
    const v0 = kf0.pose[key] ?? 0;
    const v1 = kf1.pose[key] ?? 0;
    result[key] = v0 + (v1 - v0) * s;
  }
  return result;
}

// ─── Apply a pose dict to the joint hierarchy ───────────────────────────────
/**
 * Apply interpolated pose values to Babylon.js TransformNodes.
 *
 * @param {Object} pose - Dict of "partName.axis" -> degrees
 * @param {Object} joints - Joint references from swordsman-body.js
 * @returns {number} body.y offset in degrees (for body bob)
 */
function applyPose(pose, joints) {
  // Babylon.js is left-handed (Z away from camera).
  // Three.js is right-handed (Z toward camera).
  // For rotation.y and rotation.z we negate to compensate.
  const Y_SIGN = -1;
  const Z_SIGN = -1;

  let bodyY = 0;

  for (const [key, deg] of Object.entries(pose)) {
    const dot = key.indexOf('.');
    if (dot < 0) continue;
    const partName = key.slice(0, dot);
    const axis = key.slice(dot + 1);

    // body.y is a position offset, not a rotation
    if (partName === 'body' && axis === 'y') {
      bodyY = deg; // in degrees but used as a scaled offset
      continue;
    }

    const node = joints[partName];
    if (!node) continue;

    const rad = deg * DEG;
    switch (axis) {
      case 'x': node.rotation.x = rad; break;
      case 'y': node.rotation.y = rad * Y_SIGN; break;
      case 'z': node.rotation.z = rad * Z_SIGN; break;
    }
  }

  return bodyY;
}

// ─── Guard stance poses (static target poses for each stance) ───────────────
// Values in degrees. These are the resting poses the character lerps to
// when in a given stance (between attacks).

const GUARD_POSES = {
  pflug: {
    'rightArm.x': -45, 'rightArm.y': 0, 'rightArm.z': -20,
    'rightLowerArm.x': -60,
    'leftArm.x': -30, 'leftArm.y': 0, 'leftArm.z': 20,
    'leftLowerArm.x': -40,
    'body.y': 0,
    'rightLeg.x': -5, 'rightLowerLeg.x': 10,
    'leftLeg.x': 5, 'leftLowerLeg.x': 5,
  },
  vomtag: {
    'rightArm.x': -160, 'rightArm.y': 0, 'rightArm.z': -15,
    'rightLowerArm.x': -30,
    'leftArm.x': -40, 'leftArm.y': 0, 'leftArm.z': 15,
    'leftLowerArm.x': -20,
    'body.y': 0,
    'rightLeg.x': -8, 'rightLowerLeg.x': 15,
    'leftLeg.x': 8, 'leftLowerLeg.x': 5,
  },
  ochs: {
    'rightArm.x': -120, 'rightArm.y': 30, 'rightArm.z': -30,
    'rightLowerArm.x': -90,
    'leftArm.x': -35, 'leftArm.y': 0, 'leftArm.z': 15,
    'leftLowerArm.x': -30,
    'body.y': 0,
    'rightLeg.x': -5, 'rightLowerLeg.x': 10,
    'leftLeg.x': 5, 'leftLowerLeg.x': 5,
  },
  alber: {
    'rightArm.x': 20, 'rightArm.y': 0, 'rightArm.z': -15,
    'rightLowerArm.x': -10,
    'leftArm.x': 10, 'leftArm.y': 0, 'leftArm.z': 15,
    'leftLowerArm.x': -20,
    'body.y': 0,
    'rightLeg.x': -10, 'rightLowerLeg.x': 20,
    'leftLeg.x': 10, 'leftLowerLeg.x': 5,
  },
};

// ─── Keyframe data ──────────────────────────────────────────────────────────
// TODO: Replace these placeholder keyframes with the actual data from your
// animation editor (PFLUG_SLASH_KF, VOMTAG_BACKHAND_KF, etc.).
//
// Format: Array of { t: 0..1, pose: { "partName.axis": degrees, ... } }
//
// The placeholders below create simple swinging motions that look reasonable
// from the Arena's top-down camera. Replace them with your 13-keyframe
// animations for full fidelity.

// Rich 8-beat generators (2026-07-05): full-body strike choreography built
// for close-up viewing. Guard-relative so all 12 stance variants keep their
// flavor. Beats: guard -> anticipation (torso twist away, elbow coil, blade
// lag, crouch load, eyes on target) -> windup HOLD (a near-duplicate
// keyframe = readable pause, free from the smoothstep easing) -> launch ->
// CONTACT (arm swept through, wrist-whip overshoot on the sword) -> contact
// HOLD (built-in hit-stop) -> follow-through -> near-guard (the idle guard
// elerp absorbs the last few degrees). Arrays are built once at module load
// and never mutated (the bracket-keys WeakMap cache depends on that).
// Channels: head.y tracks the target; head.x/head.z/body-pitch are reserved
// for the additive hit-recoil layer and never appear in keyframe data.
function _quickSwing(guardPose, swingAxis, swingRange) {
  const s = Math.sign(swingRange) || 1;
  const g = (k) => guardPose[k] || 0;
  const rest = { ...guardPose };

  const anticip = { ...guardPose };
  anticip['torso.y'] = g('torso.y') - 28 * s;
  anticip['rightArm.x'] = g('rightArm.x') - Math.abs(swingRange) * 0.55;
  anticip['rightArm.' + swingAxis] = g('rightArm.' + swingAxis) - swingRange * 0.45;
  anticip['rightLowerArm.x'] = -70;
  anticip['sword.z'] = 20 * s;
  anticip['head.y'] = g('head.y') + 24 * s;
  anticip['leftArm.x'] = g('leftArm.x') + 30;
  anticip['body.y'] = -3;

  const hold = { ...anticip };
  hold['body.y'] = -3.5;
  hold['rightArm.x'] = anticip['rightArm.x'] - 3;

  const launch = { ...guardPose };
  launch['torso.y'] = g('torso.y') + 10 * s;
  launch['rightArm.x'] = g('rightArm.x') + Math.abs(swingRange) * 0.2;
  launch['rightArm.' + swingAxis] = g('rightArm.' + swingAxis) + swingRange * 0.1;
  launch['rightLowerArm.x'] = -35;
  launch['head.y'] = g('head.y') + 10 * s;
  launch['body.y'] = 0;

  const contact = { ...guardPose };
  contact['torso.y'] = g('torso.y') + 34 * s;
  contact['rightArm.x'] = g('rightArm.x') + Math.abs(swingRange) * 0.85;
  contact['rightArm.' + swingAxis] = g('rightArm.' + swingAxis) + swingRange * 0.7;
  contact['rightLowerArm.x'] = -10;
  contact['sword.z'] = -14 * s;
  contact['leftArm.x'] = g('leftArm.x') - 15;
  contact['body.y'] = 2;

  const chold = { ...contact };
  chold['sword.z'] = -8 * s;
  chold['body.y'] = 1.6;

  const follow = { ...guardPose };
  follow['torso.y'] = g('torso.y') + 44 * s;
  follow['rightArm.x'] = g('rightArm.x') + Math.abs(swingRange) * 0.45;
  follow['rightArm.' + swingAxis] = g('rightArm.' + swingAxis) + swingRange * 0.35;
  follow['sword.z'] = 0;
  follow['body.y'] = -1;

  return [
    { t: 0.00, pose: rest },
    { t: 0.18, pose: anticip },
    { t: 0.30, pose: hold },
    { t: 0.44, pose: launch },
    { t: 0.52, pose: contact },
    { t: 0.62, pose: chold },
    { t: 0.80, pose: follow },
    { t: 1.00, pose: rest },
  ];
}

function _quickThrust(guardPose) {
  const g = (k) => guardPose[k] || 0;
  const rest = { ...guardPose };

  const coil = { ...guardPose };
  coil['torso.y'] = g('torso.y') - 14;
  coil['rightArm.x'] = g('rightArm.x') - 35;
  coil['rightLowerArm.x'] = -55;
  coil['leftArm.x'] = g('leftArm.x') + 25;
  coil['body.y'] = -2;

  const hold = { ...coil };
  hold['body.y'] = -2.5;
  hold['rightLowerArm.x'] = -58;

  const launch = { ...guardPose };
  launch['rightArm.x'] = g('rightArm.x') + 25;
  launch['rightLowerArm.x'] = -30;
  launch['body.y'] = 0.5;

  const contact = { ...guardPose };
  contact['torso.y'] = g('torso.y') + 6;
  contact['rightArm.x'] = g('rightArm.x') + 62;
  contact['rightLowerArm.x'] = -4;
  contact['rightLeg.x'] = -24;   // lunge step
  contact['leftLeg.x'] = 20;
  contact['body.y'] = 3;

  const chold = { ...contact };
  chold['body.y'] = 2.6;
  chold['rightArm.x'] = contact['rightArm.x'] - 3;

  const recover = { ...guardPose };
  recover['rightArm.x'] = g('rightArm.x') + 20;
  recover['rightLeg.x'] = -8;
  recover['leftLeg.x'] = 7;
  recover['body.y'] = 1;

  return [
    { t: 0.00, pose: rest },
    { t: 0.18, pose: coil },
    { t: 0.30, pose: hold },
    { t: 0.44, pose: launch },
    { t: 0.52, pose: contact },
    { t: 0.62, pose: chold },
    { t: 0.80, pose: recover },
    { t: 1.00, pose: rest },
  ];
}

// ─── Attack keyframe lookup ─────────────────────────────────────────────────
// Each entry: { keyframes: [...], duration: seconds }
// duration is the original animation time; playback is compressed to ATTACK_DURATION.

const ATTACK_ANIMS = {
  pflug: {
    slash:    { kf: _quickSwing(GUARD_POSES.pflug, 'z', 80, 0.7),    dur: 0.6 },
    backhand: { kf: _quickSwing(GUARD_POSES.pflug, 'z', -70, 0.7),   dur: 0.6 },
    thrust:   { kf: _quickThrust(GUARD_POSES.pflug),                  dur: 0.5 },
  },
  vomtag: {
    slash:    { kf: _quickSwing(GUARD_POSES.vomtag, 'z', 90, 0.7),   dur: 0.6 },
    backhand: { kf: _quickSwing(GUARD_POSES.vomtag, 'z', -80, 0.7),  dur: 0.6 },
    thrust:   { kf: _quickThrust(GUARD_POSES.vomtag),                 dur: 0.5 },
  },
  ochs: {
    slash:    { kf: _quickSwing(GUARD_POSES.ochs, 'y', 70, 0.7),     dur: 0.6 },
    backhand: { kf: _quickSwing(GUARD_POSES.ochs, 'y', -60, 0.7),    dur: 0.6 },
    thrust:   { kf: _quickThrust(GUARD_POSES.ochs),                   dur: 0.5 },
  },
  alber: {
    slash:    { kf: _quickSwing(GUARD_POSES.alber, 'z', 100, 0.7),   dur: 0.6 },
    backhand: { kf: _quickSwing(GUARD_POSES.alber, 'z', -90, 0.7),   dur: 0.6 },
    thrust:   { kf: _quickThrust(GUARD_POSES.alber),                  dur: 0.5 },
  },
};

// ─── Attack combo sequence ──────────────────────────────────────────────────
// Cycles through stance+attack combos for visual variety.
const ATTACK_COMBOS = [
  { stance: 'pflug',  attack: 'slash' },
  { stance: 'vomtag', attack: 'backhand' },
  { stance: 'ochs',   attack: 'backhand' },
  { stance: 'pflug',  attack: 'backhand' },
  { stance: 'vomtag', attack: 'slash' },
  { stance: 'alber',  attack: 'backhand' },
  { stance: 'pflug',  attack: 'thrust' },
  { stance: 'ochs',   attack: 'slash' },
  { stance: 'alber',  attack: 'slash' },
  { stance: 'vomtag', attack: 'thrust' },
  { stance: 'alber',  attack: 'thrust' },
  { stance: 'ochs',   attack: 'thrust' },
];

// ─── Stance selection by HP ratio ───────────────────────────────────────────
function stanceForHp(hpRatio) {
  if (hpRatio > 0.75) return 'vomtag';   // aggressive high guard
  if (hpRatio > 0.50) return 'pflug';     // balanced middle guard
  if (hpRatio > 0.25) return 'ochs';      // defensive high guard
  return 'alber';                          // low defensive guard
}

// ─── Swordsman animation state ──────────────────────────────────────────────

export class SwordsmanAnimState {
  constructor() {
    this.time = Math.random() * 10; // stagger
    this.prevX = 0;
    this.prevZ = 0;
    this.isMoving = false;
    this.moveAngle = 0;

    // Stance
    this.stance = 'pflug';
    this.targetStance = 'pflug';
    this.stanceBlend = 1; // 0..1, 1 = fully in current stance

    // Attack
    this.attackTimer = -1;
    this.attackKeyframes = null;
    this.attackComboIndex = 0;
    this.attackDuration = ATTACK_DURATION;
    this._trailOn = false;
    this.deathYaw = 0;

    // Base anim compat
    this.deathTimer = -1;
    this.respawnTimer = -1;
    this.dodgeTimer = -1;
    this.dodgeAngle = 0;
    this.targetRotY = null;

    // Smoothed values for idle
    this.smoothBodyY = 0;
    this.breathPhase = Math.random() * Math.PI * 2;

    // Hit recoil (additive layer over attack/idle; never cancels a swing).
    // Owns head.x, head.z and body pitch absolutely - keyframe data never
    // touches those channels.
    this.hitTimer = -1;
    this.hitYaw = 0;
    this.hitAmp = 0;

    // Track HP for stance selection
    this._lastHpRatio = 1;
  }
}

/**
 * Trigger a directional hit recoil: head snap + torso lean away from the
 * attacker, amplitude from the damage-scaled flinch. Additive - the current
 * swing keeps playing underneath.
 */
export function triggerSwordsmanHit(anim, yaw, amp) {
  if (anim.deathTimer >= 0) return;
  anim.hitTimer = 0;
  anim.hitYaw = yaw || 0;
  anim.hitAmp = Math.min(1, amp || 0.5);
}

/**
 * Apply the recoil AFTER the base pose has been written for this frame
 * (applyPose writes rotations absolutely). Owns its channels: writes exact
 * zeros when the recoil expires so they always rest clean.
 */
function _applyHitRecoil(entry, anim, dt) {
  const joints = entry.joints;
  if (anim.hitTimer < 0) return;
  anim.hitTimer += dt;
  const t = anim.hitTimer / 0.25;
  if (t >= 1) {
    anim.hitTimer = -1;
    joints.head.rotation.x = 0;
    joints.head.rotation.z = 0;
    joints.body.rotation.x = 0;
    return;
  }
  const k = (1 - t) * anim.hitAmp;
  const rel = anim.hitYaw - entry.root.rotation.y;
  joints.head.rotation.x = -0.55 * k * Math.cos(rel);
  joints.head.rotation.z = 0.4 * k * Math.sin(rel);
  joints.body.rotation.x = -0.28 * k * Math.cos(rel);
}

// ─── Main update function ───────────────────────────────────────────────────

/**
 * Update swordsman animation for one frame.
 * Called from the interpolate loop in bots.js via the animation branch.
 *
 * @param {Object} entry - Bot entry from createSwordsmanEntry
 * @param {number} dt - Frame delta in seconds
 */
export function updateSwordsmanAnim(entry, dt) {
  const anim = entry.anim;
  const joints = entry.joints;
  const root = entry.root;
  const bodyMat = entry.bodyMat;

  anim.time += dt;

  // Detect movement
  const dx = root.position.x - anim.prevX;
  const dz = root.position.z - anim.prevZ;
  const speed = Math.sqrt(dx * dx + dz * dz);
  anim.isMoving = speed > 0.5;
  if (anim.isMoving) {
    anim.moveAngle = Math.atan2(dx, dz);
  }
  anim.prevX = root.position.x;
  anim.prevZ = root.position.z;

  // ── Death (never interrupted) ──
  // Keep this choreography in sync with the generic death block in
  // animations.js (same beats, mirrored per-rig node math).
  if (!entry.isAlive) {
    // A death mid-swing must not leave a frozen blade trail hanging.
    if (anim._trailOn && entry._trail) {
      entry._trail.stop();
      entry._trail.setEnabled(false);
      anim._trailOn = false;
    }
    if (anim.deathTimer < 0) {
      anim.deathTimer = 0;
      // Capture the fall bearing once, on the first dead frame: toward the
      // killer when the hit stamp is fresh, else backward from facing.
      const fresh = entry._hitFromT && (performance.now() - entry._hitFromT) < 1200;
      anim.deathYaw = fresh
        ? Math.atan2(entry._hitFromX - root.position.x, entry._hitFromZ - root.position.z)
        : root.rotation.y + Math.PI;
      // Clear any dodge squash frozen by the interrupt before the fall.
      joints.body.scaling.set(1, 1, 1);
    }
    const S_DEATH = 13;
    const BODY_BASE = 0.75 * S_DEATH;
    if (isEnabled('deathEffects', 'directionalDeath')) {
      anim.deathTimer = Math.min(anim.deathTimer + dt, 0.9);
      const tn = anim.deathTimer / 0.9;
      // Fall AWAY from the killer. Euler writes only; never set
      // rotationQuaternion on these nodes.
      const rel = anim.deathYaw - root.rotation.y;
      let topple;
      if (tn < 0.20) {
        const q = tn / 0.20;
        topple = 0.12 * q;
        joints.body.position.y = BODY_BASE - 0.12 * S_DEATH * q;
        joints.body.scaling.y = 1 - 0.06 * q;
      } else if (tn < 0.62) {
        const q = (tn - 0.20) / 0.42;
        topple = 0.12 + q * q * 0.98;
        joints.body.position.y = BODY_BASE - 0.12 * S_DEATH - 0.35 * S_DEATH * q * q;
        joints.body.scaling.y = 0.94 - 0.09 * q;
      } else {
        const q = Math.min(1, (tn - 0.62) / 0.1);
        topple = 1.10 - 0.10 * q;
        joints.body.position.y = BODY_BASE - 0.47 * S_DEATH;
        joints.body.scaling.y = 0.85;
      }
      const amount = topple * (Math.PI / 2);
      joints.body.rotation.x = -Math.cos(rel) * amount;
      joints.body.rotation.z = Math.sin(rel) * amount;
      // Body hits the ground before dissolving; fade rides the settle window.
      if (bodyMat) {
        const fade = tn <= 0.62 ? 1 : 1 - (tn - 0.62) / 0.38;
        const a = isEnabled('deathEffects', 'corpseFade') ? Math.max(0, fade) : 1;
        bodyMat.alpha = a;
        // The head has its own material; fade it with the body so the
        // now-visible corpse does not leave a floating head.
        if (entry.headMat) entry.headMat.alpha = a;
      }
    } else {
      // Legacy fixed-axis topple (directionalDeath toggled off).
      anim.deathTimer = Math.min(anim.deathTimer + dt, 0.6);
      const t = anim.deathTimer / 0.6;
      joints.body.rotation.z = t * (Math.PI / 2);
      joints.body.scaling.y = Math.max(0.1, 1 - t * 0.8);
      if (bodyMat) bodyMat.alpha = isEnabled('deathEffects', 'corpseFade') ? 1 - t : 1;
    }
    return;
  }

  // ── Respawn recovery ──
  if (anim.deathTimer >= 0) {
    anim.deathTimer = -1;
    anim.respawnTimer = 0;
    anim.dodgeTimer = -1;
    anim.attackTimer = -1;
    anim.attackKeyframes = null;
    anim.hitTimer = -1;
    joints.body.rotation.z = 0;
    joints.body.rotation.x = 0;
    // Dodge writes scaling.x/z too; a dodge interrupted by death used to
    // leave them at up to 1.2 forever. Restore the full vector.
    joints.body.scaling.set(1, 1, 1);
    joints.head.rotation.x = 0;
    joints.head.rotation.z = 0;
    joints.body.position.y = 0.75 * 13; // standing base; idle bob re-owns it
    if (bodyMat) bodyMat.alpha = 1;
    if (entry.headMat) entry.headMat.alpha = 1;
    anim._trailOn = false;
  }

  // ── Residue self-heal ──
  // The death topple and the dodge squash are the only writers of these
  // channels, and the reset above is the only cleaner; the idle path never
  // touches them. If the reset is bypassed (anim state and rig falling out
  // of step, observed live under rAF throttling), a live bot keeps a
  // permanent 45-degree roll. Converge to rest on every alive frame outside
  // dodge so no residue can outlive a second.
  if (anim.dodgeTimer < 0) {
    const bodyNode = joints.body;
    if (bodyNode.rotation.z !== 0) {
      bodyNode.rotation.z = Math.abs(bodyNode.rotation.z) < 0.01
        ? 0 : elerp(bodyNode.rotation.z, 0, 6, dt);
    }
    if (bodyNode.scaling.y !== 1) {
      bodyNode.scaling.y = Math.abs(bodyNode.scaling.y - 1) < 0.01
        ? 1 : elerp(bodyNode.scaling.y, 1, 6, dt);
    }
    if (bodyNode.scaling.x !== 1) {
      bodyNode.scaling.x = Math.abs(bodyNode.scaling.x - 1) < 0.01
        ? 1 : elerp(bodyNode.scaling.x, 1, 6, dt);
    }
    if (bodyNode.scaling.z !== 1) {
      bodyNode.scaling.z = Math.abs(bodyNode.scaling.z - 1) < 0.01
        ? 1 : elerp(bodyNode.scaling.z, 1, 6, dt);
    }
    // Hit-recoil channels (head pitch/roll, body pitch). The recoil writes
    // them absolutely while hitTimer runs and zeroes them on expiry, but a
    // lost hitTimer would strand them the same way the death roll was
    // stranded. Heal them whenever no recoil owns the channels.
    if (anim.hitTimer < 0) {
      const headNode = joints.head;
      if (headNode.rotation.x !== 0) {
        headNode.rotation.x = Math.abs(headNode.rotation.x) < 0.01
          ? 0 : elerp(headNode.rotation.x, 0, 6, dt);
      }
      if (headNode.rotation.z !== 0) {
        headNode.rotation.z = Math.abs(headNode.rotation.z) < 0.01
          ? 0 : elerp(headNode.rotation.z, 0, 6, dt);
      }
      if (bodyNode.rotation.x !== 0) {
        bodyNode.rotation.x = Math.abs(bodyNode.rotation.x) < 0.01
          ? 0 : elerp(bodyNode.rotation.x, 0, 6, dt);
      }
    }
  }

  // One-shot residue rule for the blade trail: a dodge cancels the attack
  // (attackTimer = -1) without a trail hook, which stranded the ribbon
  // through dodge and idle. Force it off whenever no attack owns it.
  if (anim._trailOn && anim.attackTimer < 0 && entry._trail) {
    entry._trail.stop();
    entry._trail.setEnabled(false);
    anim._trailOn = false;
  }

  if (anim.respawnTimer >= 0) {
    anim.respawnTimer += dt;
    const rt = Math.min(anim.respawnTimer / 0.5, 1);
    const glow = (1 - rt) * 0.8;
    if (bodyMat && bodyMat.emissiveColor) {
      // Store original emissive on first frame, restore when done
      if (!anim._origEmissive) {
        anim._origEmissive = { r: bodyMat.emissiveColor.r, g: bodyMat.emissiveColor.g, b: bodyMat.emissiveColor.b };
      }
      bodyMat.emissiveColor.r = Math.min(anim._origEmissive.r + glow, 1);
      bodyMat.emissiveColor.g = Math.min(anim._origEmissive.g + glow, 1);
      bodyMat.emissiveColor.b = Math.min(anim._origEmissive.b + glow, 1);
    }
    if (anim.respawnTimer > 0.5) {
      anim.respawnTimer = -1;
      anim._origEmissive = null;
    }
  }

  // Smooth facing toward target
  if (anim.targetRotY !== null) {
    root.rotation.y = elerp(root.rotation.y, anim.targetRotY, 8, dt);
    // Clear once close enough to avoid drift
    if (Math.abs(root.rotation.y - anim.targetRotY) < 0.01) {
      anim.targetRotY = null;
    }
  }

  // ── Dodge (only interrupted by death) ──
  if (anim.dodgeTimer >= 0) {
    anim.dodgeTimer += dt;
    const t = Math.min(anim.dodgeTimer / 0.3, 1);
    const wave = Math.sin(t * Math.PI);
    joints.body.scaling.y = 1 - wave * 0.3;
    joints.body.scaling.x = 1 + wave * 0.2;
    joints.body.scaling.z = 1 + wave * 0.2;
    if (bodyMat) {
      bodyMat.alpha = 0.5 + Math.sin(t * Math.PI * 4) * 0.3;
    }
    if (anim.dodgeTimer > 0.3) {
      anim.dodgeTimer = -1;
      joints.body.scaling.set(1, 1, 1);
      if (bodyMat) bodyMat.alpha = 1;
    }
    return;
  }

  // ── Attack animation ──
  if (anim.attackTimer >= 0 && anim.attackKeyframes) {
    anim.attackTimer += dt;
    const duration = anim.attackDuration > 0 ? anim.attackDuration : ATTACK_DURATION;
    const progress = Math.min(anim.attackTimer / duration, 1);

    const pose = interpolateKeyframes(progress, anim.attackKeyframes);
    const bodyYOffset = applyPose(pose, joints);

    // Body bob during attack
    const S = 13; // scale factor
    joints.body.position.y = elerp(
      joints.body.position.y,
      0.75 * S + bodyYOffset * 0.5 * S,
      12, dt
    );

    // Blade swing trail, visible only through the strike arc. Lazily built
    // on the first swing (undefined = never tried, null = unavailable) and
    // reused; the dirty check keeps this to one comparison per frame.
    const inSwing = progress > 0.30 && progress < 0.85 &&
      entry._trail !== null &&
      isEnabled('weaponImpactVfx', 'meleeSwingTrails');
    if (inSwing !== anim._trailOn) {
      if (inSwing) {
        if (entry._trail === undefined) entry._trail = makeSwordTrail(entry);
        if (entry._trail) {
          if (entry._trail.reset) entry._trail.reset();
          entry._trail.setEnabled(true);
          entry._trail.start();
        }
      } else if (entry._trail) {
        entry._trail.stop();
        entry._trail.setEnabled(false);
      }
      anim._trailOn = !!entry._trail && inSwing;
    }

    if (progress >= 1) {
      anim.attackTimer = -1;
      anim.attackKeyframes = null;
      if (anim._trailOn && entry._trail) {
        entry._trail.stop();
        entry._trail.setEnabled(false);
        anim._trailOn = false;
      }
    }
    // Recoil rides on top of the swing (applyPose wrote absolutely above).
    _applyHitRecoil(entry, anim, dt);
    return;
  } else if (anim.attackTimer >= 0) {
    // A timer with no keyframes has nothing to play, and it would gate every
    // future attack trigger forever (progress reads as 0, below the 0.7
    // interrupt threshold). The generic triggerShove sets exactly this state
    // on swordsman entries. Clear it and fall through to idle.
    anim.attackTimer = -1;
  }

  // ── Idle / movement ──
  _updateIdle(anim, joints, dt);
  _applyHitRecoil(entry, anim, dt);
}

// ─── Idle animation (breathing, guard pose, walk cycle) ─────────────────────

// GUARD_POSES entries are static per stance, so the parsed
// {partName, axis, targetRad} triples are cached once per stance instead of
// being re-derived (Object.entries + string split) every frame per bot.
const _guardPoseEntriesCache = {};
function _getGuardPoseEntries(stance) {
  let entries = _guardPoseEntriesCache[stance];
  if (entries) return entries;

  const guardPose = GUARD_POSES[stance] || GUARD_POSES.pflug;
  entries = [];
  for (const [key, targetDeg] of Object.entries(guardPose)) {
    if (key === 'body.y') continue;
    const dot = key.indexOf('.');
    const partName = key.slice(0, dot);
    const axis = key.slice(dot + 1);

    const Y_SIGN = -1;
    const Z_SIGN = -1;
    let sign = 1;
    if (axis === 'y') sign = Y_SIGN;
    if (axis === 'z') sign = Z_SIGN;

    entries.push({ partName, axis, targetRad: targetDeg * DEG * sign });
  }
  _guardPoseEntriesCache[stance] = entries;
  return entries;
}

function _updateIdle(anim, joints, dt) {
  const S = 13;
  anim.breathPhase += dt * 2.0;

  // Breathing: subtle torso scale + arm sway
  const breathAmt = Math.sin(anim.breathPhase) * 0.015;
  joints.torso.scaling.y = 1 + breathAmt;

  // Guard pose: lerp all joints toward current stance
  const entries = _getGuardPoseEntries(anim.stance);
  for (let i = 0; i < entries.length; i++) {
    const { partName, axis, targetRad } = entries[i];
    const node = joints[partName];
    if (!node) continue;
    node.rotation[axis] = elerp(node.rotation[axis], targetRad, 4, dt);
  }

  // Walk cycle
  if (anim.isMoving) {
    const walkSpeed = 8;
    const legSwing = Math.sin(anim.time * walkSpeed) * 25 * DEG;
    const kneeSwing = Math.max(0, Math.sin(anim.time * walkSpeed)) * 20 * DEG;

    joints.leftLeg.rotation.x = elerp(joints.leftLeg.rotation.x, legSwing, 10, dt);
    joints.rightLeg.rotation.x = elerp(joints.rightLeg.rotation.x, -legSwing, 10, dt);
    joints.leftLowerLeg.rotation.x = elerp(joints.leftLowerLeg.rotation.x, kneeSwing, 10, dt);
    joints.rightLowerLeg.rotation.x = elerp(joints.rightLowerLeg.rotation.x, kneeSwing, 10, dt);

    // Body bob
    const bob = Math.sin(anim.time * walkSpeed * 2) * 1.5;
    joints.body.position.y = elerp(joints.body.position.y, 0.75 * S + bob, 12, dt);
  } else {
    // Idle bob
    const bob = Math.sin(anim.time * 2.5) * 0.8;
    joints.body.position.y = elerp(joints.body.position.y, 0.75 * S + bob, 8, dt);
  }
}

// ─── Trigger functions (called from bots.js) ────────────────────────────────

/**
 * Trigger a swordsman attack. Picks the next combo in the cycle,
 * transitions to the appropriate stance, and starts the keyframe animation.
 *
 * @param {SwordsmanAnimState} anim
 */
export function triggerSwordsmanAttack(anim, durationOverride) {
  // Match triggerSwordsmanDodge: while a respawn reset is pending, starting
  // an attack is pointless (the reset clobbers it on the next frame).
  if (anim.deathTimer >= 0) return;
  if (anim.dodgeTimer >= 0) return;
  if (anim.attackTimer >= 0) {
    // Allow interrupt only in last 30% of attack (recovery)
    const duration = anim.attackDuration > 0 ? anim.attackDuration : ATTACK_DURATION;
    if (anim.attackTimer / duration < 0.7) return;
  }

  const combo = ATTACK_COMBOS[anim.attackComboIndex % ATTACK_COMBOS.length];
  anim.attackComboIndex++;

  // Switch stance
  anim.stance = combo.stance;

  // Get keyframes
  const animData = ATTACK_ANIMS[combo.stance]?.[combo.attack];
  if (!animData) return;

  anim.attackTimer = 0;
  anim.attackDuration = Math.max(0.16, Number(durationOverride) || ATTACK_DURATION);
  anim.attackKeyframes = animData.kf;
}

/**
 * Trigger dodge (same as base system).
 * @param {SwordsmanAnimState} anim
 * @param {number} angle
 */
export function triggerSwordsmanDodge(anim, angle) {
  if (anim.deathTimer >= 0) return;
  if (anim.dodgeTimer >= 0) return;
  anim.dodgeTimer = 0;
  anim.dodgeAngle = angle || 0;
  anim.attackTimer = -1;
  anim.attackKeyframes = null;
}

/**
 * Update stance based on HP ratio.
 * Call this when HP changes to get dynamic stance transitions.
 *
 * @param {SwordsmanAnimState} anim
 * @param {number} hpRatio - 0..1
 */
export function updateSwordsmanStance(anim, hpRatio) {
  if (Math.abs(hpRatio - anim._lastHpRatio) < 0.05) return;
  anim._lastHpRatio = hpRatio;
  // Only change stance when not attacking
  if (anim.attackTimer < 0) {
    anim.stance = stanceForHp(hpRatio);
  }
}
