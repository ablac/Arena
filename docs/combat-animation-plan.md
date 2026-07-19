# Historical Combat Animation Plan

> **Status:** Historical implementation snapshot. File inventories, line counts,
> gaps, and suggested sequencing below describe the code at the time this plan
> was written and are not a current roadmap or source of truth. Confirm current
> behavior in the code and track new work in GitHub Issues before implementing
> any item from this document.

TOP 5 COMBAT-ANIMATION ENHANCEMENTS (all procedural Babylon.js, no external assets)

1. UNIVERSAL DAMAGE NUMBERS + HIT FLINCH (highest impact, do first)
What it looks like: every successful hit pops a floating damage number (already built: pooled DynamicTexture billboard rising 10 units over 0.5s, red for damage) at the victim, colored/scaled by magnitude (>=25 dmg: larger font + brief scale punch), and the victim plays a flinch: reuse playImpactReaction (white flash + squash) extended with a 3-5 unit positional nudge away from the attacker and a 0.1s rotation dip. Crits/backstabs get a yellow number.
Trigger source: Phase A (client-only): bots.js already caches entry._lastHp; on hp decrease for an alive bot, call effects.spawnDamageNumber(x, z, delta) and playImpactReaction(botId). Attacker direction approximated from the most recent onAttack/event targeting that bot. Phase B (exact): new server ArenaEvent type "hit" with a new Damage float64 `json:"damage,omitempty"` field on ArenaEvent, emitted in combat.go wherever HP is deducted (owner_id, target_id, position, damage, color).
Files: frontend/js/renderer/bots.js (hp-delta detect, flinch nudge), effects.js (magnitude styling on existing spawnDamageNumber), engine.js (event case). Phase B adds go-arena/internal/game/state.go, arena_events.go, combat.go.
Effort: S (phase A) / M (with server event). Server field: none for phase A; Damage float on ArenaEvent + "hit" event for phase B.

2. KILL-SHOT PUNCTUATION: WEAPON-FLAVORED DEATHS + KILLER ATTRIBUTION
What it looks like: deaths become the show's exclamation points. On kill: 120ms white flash + 1.15x scale pop on the victim, then a weapon-specific death burst (sword/daggers: radial slash flash planes; bow: piercing streak through the body; staff: purple implosion-then-explosion reusing spawnStaffExplosion at 0.6 scale; shield/shove: ground shockwave torus; grapple: upward yank), an expanding shock ring in the KILLER's avatar color, and the existing topple biased to fall AWAY from the killer (pass a topple yaw into the death branch of updateBotAnim/updateSwordsmanAnim instead of the fixed rotation.z). Optional "streak" text over the killer using the pooled damage-number path.
Trigger source: new server ArenaEvent type "kill" (owner_id, target_id, position, from_position=killer pos, color=killer color) plus a new Weapon string `json:"weapon,omitempty"` field; emitted at the death site in combat.go. Client fallback without server work: death already inferred in effects.update, but no attribution, so ship the server event.
Files: effects.js (spawnKillBurst dispatcher reusing existing burst builders), animations.js + swordsman-anims.js (parameterized topple direction, stash on anim state), bots.js (plumb kill dir), engine.js (event case), go-arena state.go + arena_events.go + combat.go.
Effort: M. Server field: "kill" event + Weapon string on ArenaEvent.

3. MELEE SWING ARC RIBBONS (blade trails)
What it looks like: a glowing arc ribbon traces the weapon tip through the swing (classic fighting-game slash arc) in attacker color mixed with weapon hue, fading over ~200ms, double arcs for daggers, a straight lance streak for spear thrusts, readable at any zoom. This replaces the current guess-the-swing readout where the only cue is a small mesh rotation plus the 150ms spawnWeaponStrike billboard.
Trigger source: client-only, driven by existing attack state: while anim.attackTimer is in windup+active phase, sample the tip's world position each frame (getAbsolutePosition of a new tip locator node) into a short ring buffer and update an updatable ribbon exactly like trails.js does (pathArray [tipPositions, hiltPositions], additive blend, vertex-alpha fade by age). Dispose/park when recovery ends.
Files: weapons.js (add invisible tip + hilt locator TransformNodes per weapon, expose as root._tipNode/_hiltNode; swordsman-body.js blade already has a mesh to parent a locator to), new frontend/js/renderer/weapon-trails.js (ribbon pool, borrow trails.js in-place update pattern), animations.js + swordsman-anims.js (expose swing-phase flag), bots.js or engine.js render loop (tick the pool).
Effort: M. Server field: none.

4. WOUNDED LOW-HP STATE (who is about to die)
What it looks like: below 35% HP a bot visibly struggles: persistent ember/smoke particle wisp attached to the body (small, 6-8 particles, bot-tinted), emissive dimmed 30%, idle bob slowed and lowered (wounded slump: targetY - 1.5, slight rotation.x droop), swordsman already switches to alber guard - amplify with a knee-bend offset. Below 15%: red pulse on the body emissive synced to a 1.2Hz sine (heartbeat). Restores instantly on heal/respawn. This gives spectators the narrative cue the GUI HP bar is too small to deliver at arena zoom.
Trigger source: client-only; hp/max_hp already streamed every snapshot (bots.js already computes hpRatio for the HP bar and swordsman stances).
Files: bots.js (state flag on entry + attach/detach particle), animations.js (wounded modifiers in idle branch), swordsman-anims.js (_updateIdle slump), effects.js or bot-body.js (pooled wisp ParticleSystem helper).
Effort: S. Server field: none.

5. HIT-STOP + CAMERA IMPULSE ON BIG BEATS
What it looks like: kills, mine/staff detonations, and grapple slams land with weight: a 60-90ms global animation-rate dip (scene.animationTimeScale or simply scaling the dt fed to updateBotAnim/updateSwordsmanAnim/gameplay.animate to 0.15 for 5 frames, then easing back) plus a 2-4 unit exponential-decay camera offset shake in CameraController (add a shake(intensity) that perturbs the camera target for 250ms; nothing else touches camera position math). Small shake for kills near screen center, bigger for detonations scaled by radius, none for routine hits so it stays punchy.
Trigger source: existing WS events (mine_detonated, staff_detonated, grapple_slam) and the kill signal from item 2 (or the client-inferred death diff until then). All in engine._playArenaEvents.
Files: engine.js (dt scaler + event hooks), camera.js (shake method), no changes to the six inventoried files beyond consuming the scaled dt they already accept.
Effort: S-M. Server field: none (leverages item 2's kill event if present).

SUGGESTED ORDER: 4 then 1(phase A) as a same-day double (both S, both client-only), then 3, then 2, then 5. Items 1B+2 share one small Go PR (two new event builders + two ArenaEvent fields), keeping server churn to a single change.

MECHANISM:
INVENTORY BY FILE

1. animations.js (379 lines, capsule bots i.e. every weapon except sword)
- Per-frame procedural state machine per bot (BotAnimState), priority: death > respawn > dodge > attack > idle/movement. No Babylon Animation class; everything is dt-lerped in updateBotAnim.
- Attacks: WEAPON_ANIMS has 8 configs (sword fallback, bow, daggers, spear, staff, shield, grapple, shove) with windup/active/recovery phase percentages. The "swing" is a rotation.z sweep of the weapon TransformNode plus small position/pitch/yaw offsets, a body lunge (rotation.x) and vertical bob. Specials: bow rebuilds the bowstring tube for draw (updateBowDraw), daggers get a sin-oscillated double slash, staff/shield get child-mesh glow scaling (staff-orb, staff-halo, shield-rim/boss). Duration is overridden by the server cooldown value so anims never outlast cooldowns.
- Death: fixed 0.6s topple (rotation.z to 90deg) + Y squash + alpha fade, identical for every death. Respawn: 0.5s white emissive flare. Dodge: 0.3s squash-stretch + alpha flicker (invuln shimmer). Idle/move: sine bob + directional tilt. Facing: smoothed targetRotY.
- Trigger: entirely INFERRED. bots.js:122-161 compares consecutive 10Hz snapshots: action==='attack' && (cooldown_remaining jumped > 0.35 || action changed to attack with cooldown > 0.05). Dodge/shove likewise from bot.action. No WS event involved for melee.

2. swordsman-anims.js (585 lines, sword bots only)
- Keyframe interpolation engine (smoothstep between pose dicts of "joint.axis": degrees) applied to the articulated rig from swordsman-body.js (torso, arms, lower arms, legs, knees + held longsword group). Handles the Three.js-to-Babylon handedness flip (Y/Z sign negation).
- 4 HEMA guard stances (pflug/vomtag/ochs/alber) selected by HP ratio (stanceForHp: aggressive high guard at full HP down to low guard under 25%). 3 attacks per stance (slash/backhand/thrust) cycled through a fixed 12-combo list for variety. Idle: breathing scale + guard-pose lerp; walk cycle with leg/knee swing and body bob. Death/dodge duplicated from base system.
- CRITICAL GAP flagged in-code (lines 182-189): the attack keyframes are placeholder 5-keyframe _quickSwing/_quickThrust generators; the real 13-keyframe editor animations were never dropped in. Attacks are compressed to 0.5s to match sword cooldown.
- Trigger: same inferred path (triggerSwordsmanAttack from bots.js).

3. effects.js (1097 lines, EffectRenderer)
- Death: update(bots) diffs a prevAlive Set and fires _deathBurst (20 additive particles, bot color) - inferred, no attribution.
- One-shot effects: spawnHitSparks (6 per-weapon particle configs), spawnBowImpact (expanding torus + miss dust), spawnDodgeEffect, spawnShoveEffect (directional blast), spawnWeaponStrike (150ms slash billboard plane + rotating wake disc for sword), spawnShieldBash / spawnSpearBrace / spawnBackstab / spawnGrappleSlam (torus/plane accents), spawnGrappleEffect (chain cylinder + hook, 3-phase appear/pull/fade, pull vs anchor modes), spawnMineExplosion (ring + core + scorch), spawnStaffExplosion, spawnCapturePadPulse, spawnTeleportBurst (burst + ripple + portal shell at both ends).
- spawnDamageNumber (pooled DynamicTexture billboard, MAX 12 concurrent, rise-and-fade) is FULLY IMPLEMENTED AND NEVER CALLED from anywhere - dead code because no damage quantity exists client-side per hit and no caller was wired.
- Triggers are mixed: melee sparks/strike come from the inferred onAttack callback (engine.js:116-124, note bow/staff early-return); everything else comes from server ArenaEvents in engine._playArenaEvents (id-deduped, 256-entry LRU). Victim reaction playImpactReaction (white flash + squash 100ms) fires ONLY for bow_impact/spear_brace/shield_bash/backstab/grapple_slam events with target_id - plain melee hits produce no victim reaction at all.

4. trails.js (180 lines)
- Movement-only ribbons: 20-point position history sampled every 30ms, updatable ribbon updated in place, vertex-alpha fade tail-to-head (max alpha 0.3), floor level y=0.4, bot body color. Always on for alive bots. No relation to combat: constant width regardless of speed, no dodge/sprint emphasis, no weapon trails.

5. weapons.js (392 lines)
- Procedural weapon meshes: sword (blade/tip/guard/grip/pommel), bow (tube arc + UPDATABLE string with base path stored for draw anim), spear, dual daggers, staff (orb + halo + prongs, the only weapon with attack glow), shield (shell/rim/boss), grapple (handle/cable/hub/3 claws/glow). Shared frozen materials keyed per scene. Roots expose _children for glow iteration and _bowString/_bowLimb hooks. No tip-position hook exists for trail sampling yet.

6. bot-body.js (290 lines)
- Capsule bot assembly: 6-tess cylinder body + sphere head with fresnel emissive, 2 STATIC instanced arm cylinders (never animated - all attack motion lives on the weapon mesh), instanced shadow disc, invisible pick cylinder, weapon mesh, GUI nameplate + HP bar (linkWithMesh, color thresholds at 60/30%). Sword bots branch to createSwordsmanEntry. Status tinting in bots.js: is_dodging alpha 0.5, is_stunned red emissive.

TRIGGER ARCHITECTURE SUMMARY
- Inferred from 10Hz state (bots.js): melee/staff/bow swing anims, shove, dodge, deaths, stun/dodge tint, swordsman stances (hp ratio).
- Server ArenaEvents (engine.js): teleport, bow_fired, bow_impact, spear_brace, shield_bash, backstab, grapple_pull/anchor, grapple_slam, mine_detonated, staff_detonated, burn_field_spawned (unused client-side), flag_taken/returned/dropped/captured, capture_pad_captured.
- ArenaEvent struct (state.go:393): id, type, tick, position, from_position, to_position, owner_id, target_id, color, radius, intensity. There is NO damage amount anywhere in the protocol, NO generic hit event for plain melee, and NO kill event with attacker attribution.
- Dead paths: effects.spawnDamageNumber never called; botRenderer.onGrapple callback never assigned in engine.js (grapple visuals ride the WS events instead).

QUALITY GAPS RANKED FOR SPECTATOR WATCHABILITY
(a) Zero damage feedback: you cannot tell a graze from a nuke. (b) Deaths (the highlight moments) are a quiet fade + small burst with no killer/weapon story. (c) Melee swings are small weapon-mesh rotations + a 150ms billboard; unreadable at typical spectator zoom. (d) Victims of ordinary melee show no reaction (no flinch/knockback). (e) No on-body low-HP tension signal (HP bar only; swordsman stances are too subtle). (f) Swordsman attack keyframes are placeholders per the in-file TODO. (g) Capsule-bot arms are static. (h) No camera response to any combat beat.

LOCATIONS:
frontend/js/renderer/animations.js:23-32 (WEAPON_ANIMS per-weapon phase configs)
frontend/js/renderer/animations.js:157-326 (updateBotAnim priority state machine: death>respawn>dodge>attack>idle)
frontend/js/renderer/animations.js:334-379 (triggerAttack/triggerShove/triggerDodge interruption rules)
frontend/js/renderer/swordsman-anims.js:142-179 (4 HEMA guard poses)
frontend/js/renderer/swordsman-anims.js:182-268 (TODO placeholder attack keyframes, _quickSwing/_quickThrust, ATTACK_ANIMS)
frontend/js/renderer/swordsman-anims.js:287-293 (stanceForHp: HP-driven stance selection)
frontend/js/renderer/effects.js:15-22 (HIT_EFFECTS per-weapon spark configs)
frontend/js/renderer/effects.js:221-311 (spawnWeaponStrike slash billboard + sword wake)
frontend/js/renderer/effects.js:449-509 (spawnDamageNumber: pooled, DEFINED BUT NEVER CALLED)
frontend/js/renderer/effects.js:512-530 (_deathBurst: generic 20-particle death)
frontend/js/renderer/effects.js:537-696 (spawnGrappleEffect chain 3-phase)
frontend/js/renderer/bots.js:122-161 (inferred attack detection from action + cooldown_remaining jump)
frontend/js/renderer/bots.js:163-199 (shove/grapple/dodge inference; onGrapple callback is never wired in engine.js = dead path)
frontend/js/renderer/bots.js:315-347 (playImpactReaction: white flash + squash, only for event-driven weapons)
frontend/js/renderer/engine.js:115-134 (onAttack/onDodge/onShove wiring for inferred path)
frontend/js/renderer/engine.js:254-370 (_playArenaEvents: WS event dispatch, id dedupe)
frontend/js/renderer/trails.js:10-171 (movement ribbon system, updatable ribbon + vertex alpha)
frontend/js/renderer/weapons.js:28-34 (createWeaponMesh dispatch; roots expose _children/_bowString)
frontend/js/renderer/bot-body.js:120-252 (capsule bot assembly: static instanced arms, GUI HP bar)
frontend/js/renderer/projectiles.js:9-156 (arrow/bolt keyframe flight + particle trail)
go-arena/internal/game/state.go:393-405 (ArenaEvent struct: no damage field, no kill/hit event type)
go-arena/internal/game/arena_events.go:5-197 (all 12 server event builders)
