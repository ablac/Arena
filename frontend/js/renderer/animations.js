'use strict';

/**
 * Bot animations — idle bob, movement tilt, per-weapon attack, dodge, death/respawn.
 * Uses per-frame updates (not Babylon Animation class) for simplicity.
 * @module renderer/animations
 */

import { isEnabled } from '../settings.js';
import { makeWeaponTrails } from './weapons.js?v=20260707c';

const IDLE_BOB_SPEED = 2.5;
const IDLE_BOB_AMOUNT = 1.2;
const MOVE_BOB_SPEED = 6;
const MOVE_BOB_AMOUNT = 2.0;
const MOVE_TILT = 0.15;

/**
 * Per-weapon animation configs. Durations slightly undershoot server cooldowns
 * so animations finish before the next attack is possible.
 *
 * Phase percentages split the animation into windup → active → recovery.
 * restZ/windupZ/swingZ control weapon mesh rotation.z through each phase.
 * lunge/bob control body tilt and vertical offset during the strike.
 */
// armRaise (radians) swings the shoulder pivots through windup/strike so the
// body participates in the attack, not just the weapon mesh. armSide picks
// which arm carries the weapon ('L' for shield, mounted at negative x).
// contactHold (fraction of the recovery phase) freezes the strike pose for a
// beat before returning, which sells the impact at close zoom.
const WEAPON_ANIMS = {
  sword:   { duration: 0.42, windupPct: 0.18, activePct: 0.48, restZ: -0.4, windupZ: -1.35, swingZ: 2.25, lunge: 0.28, bob: 2.4, weaponX: 0.75, weaponY: 0.65, pitch: 0.18, yaw: 0.10, glow: 0.0, armRaise: 1.5, contactHold: 0.12 },
  bow:     { duration: 0.78, windupPct: 0.34, activePct: 0.12, restZ: 0.02, windupZ: 0.42, swingZ: -0.62, lunge: -0.08, bob: 0.7, weaponX: -0.25, weaponY: 0.85, pitch: -0.12, yaw: -0.08, glow: 0.0, armRaise: 0.5, contactHold: 0 },
  daggers: { duration: 0.24, windupPct: 0.14, activePct: 0.56, restZ: 0.05, windupZ: -0.48, swingZ: 1.75, lunge: 0.34, bob: 1.8, weaponX: 0.55, weaponY: 0.45, pitch: 0.12, yaw: 0.16, glow: 0.0, armRaise: 1.2, contactHold: 0.07, trail: 0.45, trailTail: 0.82 },
  spear:   { duration: 0.56, windupPct: 0.24, activePct: 0.42, restZ: -0.26, windupZ: -0.92, swingZ: 0.62, lunge: 0.38, bob: 3.1, weaponX: 1.6, weaponY: 0.32, pitch: -0.08, yaw: 0.12, glow: 0.0, armRaise: 1.3, contactHold: 0.12, trail: 0.62 },
  staff:   { duration: 0.96, windupPct: 0.42, activePct: 0.26, restZ: 0.05, windupZ: 0.72, swingZ: -0.36, lunge: 0.16, bob: 4.2, weaponX: 0.28, weaponY: 1.1, pitch: -0.12, yaw: 0.18, glow: 1.0, armRaise: 0.9, contactHold: 0.05 },
  shield:  { duration: 0.64, windupPct: 0.24, activePct: 0.34, restZ: 0.08, windupZ: 0.52, swingZ: -1.02, lunge: 0.32, bob: 2.2, weaponX: -0.95, weaponY: 0.28, pitch: 0.08, yaw: -0.18, glow: 0.25, armRaise: 1.0, contactHold: 0.12, armSide: 'L' },
  grapple: { duration: 0.48, windupPct: 0.18, activePct: 0.36, restZ: -0.10, windupZ: -0.58, swingZ: 0.94, lunge: 0.26, bob: 1.8, weaponX: 0.9, weaponY: 0.28, pitch: -0.04, yaw: 0.2, glow: 0.2, armRaise: 1.1, contactHold: 0.07 },
  shove:   { duration: 0.35, windupPct: 0.15, activePct: 0.50, restZ: 0, windupZ: -0.2, swingZ: 0.3, lunge: 0.4, bob: 3, weaponX: 0.45, weaponY: 0.15, pitch: 0.0, yaw: 0.0, glow: 0.0, armRaise: 0.9, contactHold: 0.09 },
};

const DODGE_DURATION = 0.3;

// Directional death choreography: total time, phase boundaries, and the
// standing base height the fall sinks from (idle/attack target 10 + bob).
const DEATH_TOTAL = 0.9;
const DEATH_STAGGER_END = 0.20;
const DEATH_FALL_END = 0.62;
const BODY_BASE_Y = 10;

/**
 * Exponential smoothing helper.
 * @param {number} current
 * @param {number} target
 * @param {number} rate - higher = faster convergence
 * @param {number} dt - frame delta in seconds
 * @returns {number}
 */
function lerp(current, target, rate, dt) {
  return current + (target - current) * (1 - Math.exp(-rate * dt));
}

/**
 * Per-frame animation state for a single bot.
 */
export class BotAnimState {
  constructor() {
    this.time = Math.random() * 10; // stagger bots
    this.prevX = 0;
    this.prevZ = 0;
    this.isMoving = false;
    this.moveAngle = 0;
    this.deathTimer = -1;  // -1 = not dying
    this.respawnTimer = -1;
    this.attackTimer = -1;
    this.attackType = null;      // weapon name: 'sword', 'bow', etc.
    this.attackDuration = 0.5;   // set per weapon from WEAPON_ANIMS
    this.dodgeTimer = -1;
    this.dodgeAngle = 0;
    // Smoothed values
    this.smoothY = 10;
    this.smoothRotX = 0;
    this.smoothRotZ = 0;
    this.targetRotY = null; // set externally when facing target
    // Shoulder-pivot channels (radians), smoothed so attack and locomotion
    // hand off without snapping. walkPhase accumulates by distance traveled
    // so the arm counter-swing gait matches ground covered at any speed.
    this.smoothArmL = 0;
    this.smoothArmR = 0;
    this.walkPhase = 0;
    this._trailOn = false;
    this.deathYaw = 0;
  }
}

/**
 * Time from attack start to the visual moment of contact, so hit effects and
 * victim reactions can land when the swing connects instead of at swing start.
 * Mirrors the duration fallback in triggerAttack.
 */
export function meleeContactDelay(weapon, durationOverride) {
  const cfg = WEAPON_ANIMS[weapon] || WEAPON_ANIMS.sword;
  const duration = Math.max(0.12, Number(durationOverride) || cfg.duration);
  return (cfg.windupPct + cfg.activePct * 0.5) * duration;
}

function updateBowDraw(weapon, drawT = 0) {
  if (!weapon || !weapon._bowString || !weapon._bowStringBasePath) return;
  const B = window.BABYLON;
  const base = weapon._bowStringBasePath;
  const pull = Math.max(0, Math.min(1, drawT)) * 3.2;
  // Rebuilding the tube geometry is only needed when the draw amount actually
  // changed — idle bots called this every frame at drawT=0.
  if (weapon._bowLastPull === pull) return;
  weapon._bowLastPull = pull;
  const path = [
    base[0].clone(),
    new B.Vector3(base[1].x - pull, base[1].y, base[1].z),
    base[2].clone(),
  ];
  B.MeshBuilder.CreateTube(null, {
    path,
    radius: 0.15,
    tessellation: 4,
    cap: B.Mesh.CAP_ALL,
    instance: weapon._bowString,
  });
  if (weapon._bowLimb) {
    const s = 1 + Math.max(0, Math.min(1, drawT)) * 0.03;
    weapon._bowLimb.scaling.set(s, 1, s);
  }
}

function applyWeaponPose(weapon, cfg, swingT = 0, glowT = 0) {
  if (!weapon) return;
  weapon.position.x = (cfg.weaponX || 0) * swingT;
  weapon.position.y = (cfg.weaponY || 0) * swingT;
  weapon.rotation.x = (cfg.pitch || 0) * swingT;
  weapon.rotation.y = (cfg.yaw || 0) * swingT;
  weapon.scaling.set(1, 1, 1);

  if (weapon._children?.length) {
    for (const child of weapon._children) {
      const mat = child?.material;
      if (!mat || !mat.emissiveColor) continue;
      if (child.name?.includes('staff-orb')) {
        child.scaling.set(1 + glowT * 0.16, 1 + glowT * 0.16, 1 + glowT * 0.16);
        mat.emissiveColor.set(0.4 + glowT * 0.45, 0.15 + glowT * 0.18, 0.9 + glowT * 0.05);
      } else if (child.name?.includes('staff-halo')) {
        child.scaling.set(1 + glowT * 0.14, 1 + glowT * 0.14, 1 + glowT * 0.14);
        child.rotation.z += glowT * 0.035;
        mat.alpha = 1;
        mat.emissiveColor.set(0.78 + glowT * 0.15, 0.5 + glowT * 0.18, 1.0);
      } else if (child.name?.includes('shield-rim')) {
        child.scaling.set(1 + glowT * 0.05, 1 + glowT * 0.05, 1 + glowT * 0.05);
      } else if (child.name?.includes('shield-boss')) {
        child.scaling.set(1 + glowT * 0.08, 1 + glowT * 0.08, 1 + glowT * 0.08);
      }
    }
  }
}

function resetWeaponPose(weapon) {
  if (!weapon) return;
  weapon.position.x = 0;
  weapon.position.y = 0;
  weapon.rotation.x = 0;
  weapon.rotation.y = 0;
  weapon.scaling.set(1, 1, 1);

  if (weapon._children?.length) {
    for (const child of weapon._children) {
      if (!child?.scaling) continue;
      child.scaling.set(1, 1, 1);
    }
  }
}

// Swing-trail state transitions. Trails are built lazily on first use and
// tri-stated per weapon (_trails undefined = never tried, null = unavailable,
// never retried). Only runs on state changes, so steady state costs nothing.
function _setWeaponTrail(anim, weapon, on, bodyMat, diam) {
  if (on) {
    if (weapon._trails === undefined) {
      weapon._trails = makeWeaponTrails(weapon, bodyMat ? bodyMat.diffuseColor : null, diam);
    }
    if (weapon._trails) {
      for (let i = 0; i < weapon._trails.length; i++) {
        const tr = weapon._trails[i];
        if (tr.reset) tr.reset();
        tr.setEnabled(true);
        tr.start();
      }
    }
    anim._trailOn = !!weapon._trails;
  } else {
    if (weapon._trails) {
      for (let i = 0; i < weapon._trails.length; i++) {
        weapon._trails[i].stop();
        weapon._trails[i].setEnabled(false);
      }
    }
    anim._trailOn = false;
  }
}


/**
 * Update animation state and apply transforms.
 * Priority order: death > respawn > dodge > attack > idle/movement.
 * @param {BotAnimState} anim
 * @param {BABYLON.TransformNode} body - root transform node
 * @param {BABYLON.Mesh|null} weapon - weapon mesh
 * @param {number} x - current x
 * @param {number} z - current z
 * @param {boolean} isAlive
 * @param {number} dt - frame delta in seconds
 * @param {BABYLON.StandardMaterial|null} bodyMat - body mesh material for alpha/emissive
 * @param {Object|null} entry - bot entry carrying lShoulder/rShoulder pivots
 */
export function updateBotAnim(anim, body, weapon, x, z, isAlive, dt, bodyMat, entry) {
  anim.time += dt;

  // Detect movement
  const dx = x - anim.prevX;
  const dz = z - anim.prevZ;
  const speed = Math.sqrt(dx * dx + dz * dz);
  anim.isMoving = speed > 0.5;
  if (anim.isMoving) {
    anim.moveAngle = Math.atan2(dx, dz);
  }
  anim.prevX = x;
  anim.prevZ = z;

  // --- Priority 1: Death animation (NEVER interrupted) ---
  if (!isAlive) {
    // A death mid-swing must not leave a frozen weapon trail running (this
    // branch early-returns before the attack machine and its residue guard).
    if (anim._trailOn && weapon) _setWeaponTrail(anim, weapon, false);
    if (anim.deathTimer < 0) {
      anim.deathTimer = 0;
      // Capture the fall bearing once, on the first dead frame: toward the
      // killer when the hit stamp is fresh (bots.js stamps it at the death
      // transition), else fall straight backward from current facing.
      const fresh = entry && entry._hitFromT && (performance.now() - entry._hitFromT) < 1200;
      anim.deathYaw = fresh
        ? Math.atan2(entry._hitFromX - x, entry._hitFromZ - z)
        : body.rotation.y + Math.PI;
      // Clear any dodge squash frozen by the interrupt before the fall.
      body.scaling.set(1, 1, 1);
    }
    if (isEnabled('deathEffects', 'directionalDeath')) {
      anim.deathTimer = Math.min(anim.deathTimer + dt, DEATH_TOTAL);
      const tn = anim.deathTimer / DEATH_TOTAL;
      // Fall AWAY from the killer: the bearing relative to facing drives
      // pitch and roll together (sign convention matches the flinch layer).
      // Euler writes only; never set rotationQuaternion on this node.
      const rel = anim.deathYaw - body.rotation.y;
      let topple;
      if (tn < DEATH_STAGGER_END) {
        // Stagger: knees buckle, slight sink, barely tipping yet.
        const q = tn / DEATH_STAGGER_END;
        topple = 0.12 * q;
        body.position.y = BODY_BASE_Y - 1.5 * q;
        body.scaling.y = 1 - 0.06 * q;
      } else if (tn < DEATH_FALL_END) {
        // Fall: gravity-like acceleration, overshooting past flat.
        const q = (tn - DEATH_STAGGER_END) / (DEATH_FALL_END - DEATH_STAGGER_END);
        topple = 0.12 + q * q * 0.98;
        body.position.y = BODY_BASE_Y - 1.5 - 4.5 * q * q;
        body.scaling.y = 0.94 - 0.09 * q;
      } else {
        // Settle: a small bounce back to flat, then hold.
        const q = Math.min(1, (tn - DEATH_FALL_END) / 0.1);
        topple = 1.10 - 0.10 * q;
        body.position.y = BODY_BASE_Y - 6.0;
        body.scaling.y = 0.85;
      }
      const amount = topple * (Math.PI / 2);
      body.rotation.x = -Math.cos(rel) * amount;
      body.rotation.z = Math.sin(rel) * amount;
      // The body visibly hits the ground BEFORE dissolving: alpha holds
      // through the fall, then fades across the settle window.
      if (bodyMat) {
        const fade = tn <= DEATH_FALL_END ? 1 : 1 - (tn - DEATH_FALL_END) / (1 - DEATH_FALL_END);
        const a = isEnabled('deathEffects', 'corpseFade') ? Math.max(0, fade) : 1;
        bodyMat.alpha = a;
        // The head has its own material; fading only the body leaves a
        // floating head on the now-visible corpse.
        if (entry && entry.headMat) entry.headMat.alpha = a;
      }
    } else {
      // Legacy fixed-axis topple (directionalDeath toggled off).
      anim.deathTimer = Math.min(anim.deathTimer + dt, 0.6);
      const t = anim.deathTimer / 0.6;
      body.rotation.z = t * (Math.PI / 2);
      body.scaling.y = Math.max(0.1, 1 - t * 0.8);
      if (bodyMat) bodyMat.alpha = isEnabled('deathEffects', 'corpseFade') ? 1 - t : 1;
    }
    return;
  }

  // --- Priority 2: Respawn recovery (NEVER interrupted) ---
  if (anim.deathTimer >= 0) {
    anim.deathTimer = -1;
    anim.respawnTimer = 0;
    anim.dodgeTimer = -1;
    anim.attackTimer = -1;
    anim.attackType = null;
    body.rotation.z = 0;
    body.rotation.x = 0;
    body.position.y = BODY_BASE_Y;
    // Zero the smoothed channels too, or the next idle frame overwrites the
    // reset with stale pre-death values for the first half second.
    anim.smoothRotX = 0;
    anim.smoothRotZ = 0;
    anim.smoothY = BODY_BASE_Y;
    // Dodge writes scaling.x/z too; restore the full vector.
    body.scaling.set(1, 1, 1);
    if (bodyMat) bodyMat.alpha = 1;
    if (entry && entry.headMat) entry.headMat.alpha = 1;
  }
  if (anim.respawnTimer >= 0) {
    anim.respawnTimer += dt;
    const rt = Math.min(anim.respawnTimer / 0.5, 1);
    const glow = (1 - rt) * 0.8;
    if (bodyMat && bodyMat.emissiveColor) {
      bodyMat.emissiveColor.r = Math.min(bodyMat.emissiveColor.r + glow, 1);
      bodyMat.emissiveColor.g = Math.min(bodyMat.emissiveColor.g + glow, 1);
      bodyMat.emissiveColor.b = Math.min(bodyMat.emissiveColor.b + glow, 1);
    }
    if (anim.respawnTimer > 0.5) anim.respawnTimer = -1;
  }

  // --- Self-heal scale residue ---
  // rotation.z and alpha already have alive-path owners here (the idle block
  // writes body.rotation.z absolutely every frame, and _updateStatusEffects
  // stomps alpha every server tick), but nothing restores scaling outside
  // the dodge branch itself, so a missed respawn reset would freeze the
  // death squash forever. Ease scaling back to rest on every alive frame
  // the dodge does not own it.
  if (anim.dodgeTimer < 0) {
    const sc = body.scaling;
    if (sc.x !== 1 || sc.y !== 1 || sc.z !== 1) {
      sc.x = lerp(sc.x, 1, 6, dt);
      sc.y = lerp(sc.y, 1, 6, dt);
      sc.z = lerp(sc.z, 1, 6, dt);
      if (Math.abs(sc.x - 1) < 0.001 && Math.abs(sc.y - 1) < 0.001 &&
          Math.abs(sc.z - 1) < 0.001) sc.set(1, 1, 1);
    }
  }

  // One-shot residue rule: a swing trail must never outlive its attack.
  // Dodge cancels attacks without a trail hook, so force it off whenever no
  // attack owns it. Two scalar compares per bot per frame at steady state.
  if (anim._trailOn && anim.attackTimer < 0 && weapon) {
    _setWeaponTrail(anim, weapon, false);
  }

  // Smooth rotation toward target when set
  if (anim.targetRotY !== null) {
    body.rotation.y = lerp(body.rotation.y, anim.targetRotY, 8, dt);
    // Clear once close enough to prevent jitter
    const diff = Math.abs(body.rotation.y - anim.targetRotY);
    if (diff < 0.05) {
      anim.targetRotY = null;
    }
  }

  // --- Priority 3: Dodge dash (only interrupted by death) ---
  if (anim.dodgeTimer >= 0) {
    anim.dodgeTimer += dt;
    const t = Math.min(anim.dodgeTimer / DODGE_DURATION, 1);
    const wave = Math.sin(t * Math.PI);
    // Squish body + rapid transparency flicker for invuln shimmer
    body.scaling.y = 1 - wave * 0.3;
    body.scaling.x = 1 + wave * 0.2;
    body.scaling.z = 1 + wave * 0.2;
    if (bodyMat) {
      bodyMat.alpha = 0.5 + Math.sin(t * Math.PI * 4) * 0.3;
    }
    if (anim.dodgeTimer > DODGE_DURATION) {
      anim.dodgeTimer = -1;
      body.scaling.set(1, 1, 1);
      if (bodyMat) bodyMat.alpha = 1;
    }
    return;
  }

  // --- Priority 4–6: Weapon-specific attack ---
  if (anim.attackTimer >= 0) {
    anim.attackTimer += dt;
    const cfg = WEAPON_ANIMS[anim.attackType] || WEAPON_ANIMS.sword;
    const duration = anim.attackDuration > 0 ? anim.attackDuration : cfg.duration;
    const progress = Math.min(anim.attackTimer / duration, 1);

    const windupEnd = cfg.windupPct;
    const activeEnd = windupEnd + cfg.activePct;

    let weaponZ, targetRotX, targetY, bowDraw = 0, weaponPoseT = 0, glowT = 0;
    let armSwing = 0; // weapon-arm shoulder target this frame (radians)

    if (progress < windupEnd) {
      // Windup: anticipation — weapon pulls back, body braces
      const t = progress / windupEnd;
      const ease = t * t; // accelerating ease-in
      weaponZ = cfg.restZ + (cfg.windupZ - cfg.restZ) * ease;
      targetRotX = -cfg.lunge * 0.3 * ease;
      targetY = 10;
      weaponPoseT = ease * 0.75;
      glowT = ease * (cfg.glow || 0);
      armSwing = -(cfg.armRaise || 0) * 0.6 * ease; // cock the arm back
      if (anim.attackType === 'bow') bowDraw = ease;
    } else if (progress < activeEnd) {
      // Active: the strike — weapon sweeps through, body lunges
      const t = (progress - windupEnd) / cfg.activePct;
      const ease = Math.sin(t * Math.PI / 2); // decelerating ease-out
      weaponZ = cfg.windupZ + (cfg.swingZ - cfg.windupZ) * ease;
      targetRotX = cfg.lunge * ease;
      targetY = 10 + cfg.bob * ease;
      weaponPoseT = 0.75 + ease * 0.55;
      glowT = (0.45 + ease * 0.55) * (cfg.glow || 0);
      armSwing = (cfg.armRaise || 0) * (-0.6 + 1.6 * ease); // sweep through
      if (anim.attackType === 'bow') bowDraw = 1 - ease * 0.92;
    } else {
      // Recovery: follow-through — weapon returns, body settles.
      // contactHold freezes the strike pose for a beat before the return,
      // selling the moment of impact at close zoom.
      let t = (progress - activeEnd) / (1 - activeEnd);
      const hold = cfg.contactHold || 0;
      t = hold > 0 ? Math.max(0, (t - hold) / (1 - hold)) : t;
      const ease = t * t; // accelerating ease-in for smooth return
      weaponZ = cfg.swingZ + (cfg.restZ - cfg.swingZ) * ease;
      targetRotX = cfg.lunge * (1 - ease);
      targetY = 10 + cfg.bob * (1 - ease);
      weaponPoseT = Math.max(0, 1 - ease);
      glowT = Math.max(0, 1 - ease * 1.15) * (cfg.glow || 0);
      armSwing = (cfg.armRaise || 0) * (1 - ease);
      if (anim.attackType === 'bow') bowDraw = Math.max(0, 0.08 - ease * 0.08);
    }

    // Daggers special: rapid oscillating double-slash during active phase
    if (anim.attackType === 'daggers' && progress >= windupEnd && progress < activeEnd) {
      const t = (progress - windupEnd) / cfg.activePct;
      // 2 cycles (was 3): at ~8 sampled frames the 3-cycle weave aliased into
      // a jagged flicker; 2 reads as a clean twin-arc weave.
      weaponZ = cfg.windupZ + Math.sin(t * Math.PI * 2) * 1.5;
    }

    if (weapon) {
      weapon.rotation.z = weaponZ;
      applyWeaponPose(weapon, cfg, weaponPoseT, glowT);
    }
    if (anim.attackType === 'bow') updateBowDraw(weapon, bowDraw);

    // Melee swing trail: level-based desired state (window = the active
    // phase), dirty-checked so transitions are the only work. A frame that
    // skips the whole window simply never turns it on. isEnabled goes last
    // in the chain as the rarest-changing check.
    if (weapon) {
      const wantTrail = !!cfg.trail && !!weapon._trailTips &&
        weapon._trails !== null &&
        progress >= windupEnd && progress < (cfg.trailTail || activeEnd) &&
        isEnabled('weaponImpactVfx', 'meleeSwingTrails');
      if (wantTrail !== anim._trailOn) {
        _setWeaponTrail(anim, weapon, wantTrail, bodyMat, cfg.trail);
      }
    }
    anim.smoothRotX = lerp(anim.smoothRotX, targetRotX, 8, dt);
    anim.smoothY = lerp(anim.smoothY, targetY, 12, dt);
    body.rotation.x = anim.smoothRotX;
    body.position.y = anim.smoothY;

    // Shoulder swing: weapon arm follows the strike, off arm counters at a
    // third amplitude. Smoothed so the handoff back to locomotion never snaps.
    {
      const left = cfg.armSide === 'L';
      const tR = left ? -armSwing * 0.35 : armSwing;
      const tL = left ? armSwing : -armSwing * 0.35;
      anim.smoothArmR = lerp(anim.smoothArmR, tR, 14, dt);
      anim.smoothArmL = lerp(anim.smoothArmL, tL, 14, dt);
      if (entry && entry.rShoulder) entry.rShoulder.rotation.x = anim.smoothArmR;
      if (entry && entry.lShoulder) entry.lShoulder.rotation.x = anim.smoothArmL;
    }

    if (anim.attackTimer > duration) {
      anim.attackTimer = -1;
      anim.attackType = null;
      updateBowDraw(weapon, 0);
      resetWeaponPose(weapon);
      // Let smoothing handle return — no hard snap
    }
    return;
  }

  // --- Idle / movement bob (smoothed) ---
  let targetY, targetRotX, targetRotZ;
  let armL = 0, armR = 0;
  if (anim.isMoving) {
    const bob = Math.sin(anim.time * MOVE_BOB_SPEED) * MOVE_BOB_AMOUNT;
    targetY = 10 + bob;
    targetRotZ = Math.sin(anim.moveAngle) * MOVE_TILT;
    targetRotX = Math.cos(anim.moveAngle) * MOVE_TILT;
    // Locomotion micro-motion: arm counter-swing paced by distance traveled
    // (speed is this frame's displacement, so the gait tracks ground covered)
    // plus a slight forward lean into the run. All scalar math.
    anim.walkPhase += speed * 0.06;
    const f = Math.min(1, speed / 1.2);
    armL = Math.sin(anim.walkPhase) * 0.45 * f;
    armR = -Math.sin(anim.walkPhase) * 0.45 * f;
    targetRotX += 0.1 * f;
  } else {
    // Wounded bots idle with a slower bob (below 35% HP, woundLevel set in bots.js).
    const bobSpeed = anim.woundLevel >= 1 ? IDLE_BOB_SPEED * 0.5 : IDLE_BOB_SPEED;
    const bob = Math.sin(anim.time * bobSpeed) * IDLE_BOB_AMOUNT;
    targetY = 10 + bob;
    targetRotZ = 0;
    targetRotX = 0;
  }
  // Wounded slump: sink lower and droop forward, whether idle or moving. Smoothed
  // through the same lerp below, so it eases in as HP falls and out on a heal.
  if (anim.woundLevel >= 1) {
    targetY -= 1.5;
    targetRotX += 0.12;
  }
  anim.smoothY = lerp(anim.smoothY, targetY, 12, dt);
  anim.smoothRotX = lerp(anim.smoothRotX, targetRotX, 8, dt);
  anim.smoothRotZ = lerp(anim.smoothRotZ, targetRotZ, 8, dt);
  body.position.y = anim.smoothY;
  body.rotation.x = anim.smoothRotX;
  body.rotation.z = anim.smoothRotZ;
  anim.smoothArmL = lerp(anim.smoothArmL, armL, 10, dt);
  anim.smoothArmR = lerp(anim.smoothArmR, armR, 10, dt);
  if (entry && entry.lShoulder) entry.lShoulder.rotation.x = anim.smoothArmL;
  if (entry && entry.rShoulder) entry.rShoulder.rotation.x = anim.smoothArmR;
  updateBowDraw(weapon, 0);
  // Heal a swing z-rotation stranded by a dodge-cancel or death (attacks write
  // weapon.rotation.z absolutely; resetWeaponPose only covers x/y). Eases to
  // the builder's rest angle, the same value the next windup assumes.
  if (weapon && weapon._restZ !== undefined && weapon.rotation.z !== weapon._restZ) {
    weapon.rotation.z = lerp(weapon.rotation.z, weapon._restZ, 8, dt);
    if (Math.abs(weapon.rotation.z - weapon._restZ) < 0.005) weapon.rotation.z = weapon._restZ;
  }
  resetWeaponPose(weapon);
}

/**
 * Trigger a weapon-specific attack animation.
 * Respects interruption rules: won't interrupt dodge or active attack phase.
 * @param {BotAnimState} anim
 * @param {string} [weapon='sword'] - weapon type name
 */
export function triggerAttack(anim, weapon, durationOverride) {
  if (anim.dodgeTimer >= 0) return; // dodge can't be interrupted by attack
  // If already attacking, only interrupt during recovery phase
  if (anim.attackTimer >= 0) {
    const curCfg = WEAPON_ANIMS[anim.attackType] || WEAPON_ANIMS.sword;
    const activeDuration = anim.attackDuration > 0 ? anim.attackDuration : curCfg.duration;
    const progress = anim.attackTimer / activeDuration;
    if (progress < curCfg.windupPct + curCfg.activePct) return; // in windup or active
  }
  const cfg = WEAPON_ANIMS[weapon] || WEAPON_ANIMS.sword;
  anim.attackTimer = 0;
  anim.attackType = weapon || 'sword';
  anim.attackDuration = Math.max(0.12, Number(durationOverride) || cfg.duration);
}

/**
 * Trigger a dodge dash animation (0.3s shimmer + squish).
 * Can interrupt windup/recovery attacks but not active strike phase.
 * @param {BotAnimState} anim
 * @param {number} [angle=0] - dodge direction angle
 */
/**
 * Trigger a shove animation (shoulder-check push).
 * Uses the attack animation system with shove-specific config.
 * @param {BotAnimState} anim
 */
export function triggerShove(anim) {
  if (anim.dodgeTimer >= 0) return;
  if (anim.attackTimer >= 0) {
    const curCfg = WEAPON_ANIMS[anim.attackType] || WEAPON_ANIMS.sword;
    const progress = anim.attackTimer / curCfg.duration;
    if (progress < curCfg.windupPct + curCfg.activePct) return;
  }
  anim.attackTimer = 0;
  anim.attackType = 'shove';
  anim.attackDuration = WEAPON_ANIMS.shove.duration;
}

export function triggerDodge(anim, angle) {
  if (anim.deathTimer >= 0) return;
  if (anim.dodgeTimer >= 0) return; // already dodging
  anim.dodgeTimer = 0;
  anim.dodgeAngle = angle || 0;
  anim.attackTimer = -1;
  anim.attackType = null;
}
