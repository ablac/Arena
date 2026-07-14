// Main per-tick AI: PickAction and the per-strategy decision trees.
package demobots

import (
	"math"
)

// === Main AI ===

// PickAction decides the bot's action for this tick.
// attackRange is the Chebyshev grid range from the server's loadout_confirmed.
func PickAction(strategy string, msg map[string]interface{}, weapon string, attackRange int, botID string) actionResult {
	ts := parseTick(msg)

	// Per-tick danger set: cells movement must route around (pooled — demo
	// bots pick actions concurrently).
	danger := dangerPool.Get().(*dangerSet)
	defer dangerPool.Put(danger)
	buildDangerSet(danger, &ts, botID)
	ts.Danger = danger

	pos := ts.Position
	hpRatio := ts.HP / ts.MaxHP
	wrange := float64(attackRange)
	if wrange <= 0 {
		wrange = WeaponRanges[weapon]
	}
	canAtk := ts.WeaponReady
	canDodge := ts.DodgeCool <= 0
	visibleEnemies := countVisibleEnemies(ts.Enemies)
	needsDefensiveDisengage := visibleEnemies > 0 &&
		(hpRatio < 0.3 || (hpRatio < 0.45 && wrange <= 2 && hasVisibleRangedThreat(ts.Enemies)))
	near, nearD := closestVisible(pos, ts.Enemies)
	if near == nil {
		near, nearD = closest(pos, ts.Enemies)
	}

	// Stunned — can't act
	if ts.StunTicks > 0 {
		return idle()
	}

	// === SUDDEN DEATH: step off/away from void tiles immediately ===
	// Void tiles are already in the danger set, so normal pathing avoids
	// them; this branch handles standing on (or right next to) one.
	if ts.SuddenDeath && len(ts.VoidTiles) > 0 {
		cell := [2]int{int(math.Round(pos[0])), int(math.Round(pos[1]))}
		minVoid := math.MaxInt32
		for _, vt := range ts.VoidTiles {
			d := intChebyshev(cell, vt)
			if d < minVoid {
				minVoid = d
			}
		}
		if minVoid <= 1 {
			dir := safeStepDir(pos, gridDir(pos, ts.ZoneTargetCenter), ts.Danger)
			if minVoid == 0 && canDodge {
				return dodgeSafe(ts, dir)
			}
			return moveDir(dir)
		}
	}

	// === DANGER: leave the shortest safe way immediately. This covers active
	// hazards, enemy gravity wells, armed mines, and a void tile underfoot.
	cell := [2]int{int(math.Round(pos[0])), int(math.Round(pos[1]))}
	if ts.Danger.hasLethal(cell[0], cell[1]) {
		return escapeDanger(ts, canDodge)
	}
	// Ready pads are soft danger: ordinary routing must not enter their trigger
	// footprint, but a bot already inside it needs a one-cell egress path. If
	// every adjacent pad cell stays blocked, BFS returns zero and the bot idles
	// forever. Do not dodge here; a single step preserves multi-cell entry
	// avoidance while moving along the shortest route out of the footprint.
	if ts.Danger.hasPad(cell[0], cell[1]) {
		return escapeDanger(ts, false)
	}

	// Picking up an adjacent health pack is immediate and cannot be deferred
	// behind mines, grapples, or objective movement. The old ordering could
	// leave a critically wounded bot idling/moving on top of a heal while it
	// spent the tick on an offensive utility action.
	if hpRatio < 0.65 {
		if hp, hpD := nearestHealthPickup(pos, ts.Pickups, ts.HazardZones, ts.HasHazardKey); hp != nil && hpD <= 1 && hp.ID != "" {
			return useItem(hp.ID)
		}
	}

	// During the sudden-death stall penalty, passive play damages everyone and
	// only renewed combat stops the ramp. Healthy demo bots therefore force
	// contact instead of hiding; critically wounded bots keep survival logic.
	if ts.SuddenDeathStall && hpRatio >= 0.3 {
		if near != nil {
			if canAtk && near.HasLOS && nearD <= wrange {
				return finalizeWeaponAction(ts, weapon, wrange, atk(near, weapon))
			}
			return finalizeWeaponAction(ts, weapon, wrange, chaseApproach(ts, near, wrange, weapon))
		}
		for _, hint := range ts.Hints {
			if hint.HintType == "bot" {
				target := [2]float64{pos[0] + hint.Direction[0]*hint.Distance, pos[1] + hint.Direction[1]*hint.Distance}
				return moveTo(pos, target, ts.Danger)
			}
		}
		return moveTo(pos, ts.ZoneCenter, ts.Danger)
	}

	// === CTF OBJECTIVE PLAY: carry, chase carriers, return, steal ===
	if ctf := tryCTFObjective(ts, strategy, near, nearD, wrange, weapon, canAtk, canDodge, botID); ctf != nil {
		return *ctf
	}

	isAggStrat := strategy == "aggressive" || strategy == "berserker" || strategy == "assassin"
	canShv := ts.ShoveCool <= 0

	if ts.DoubleBounty {
		for i := range ts.Enemies {
			target := &ts.Enemies[i]
			if target.Type != "bounty_target" || !target.IsAlive {
				continue
			}
			d := chebyshev(pos, target.Position)
			if canAtk && d <= wrange {
				return finalizeWeaponAction(ts, weapon, wrange, atk(target, weapon))
			}
			if d <= wrange+4 {
				return moveTo(pos, target.Position, ts.Danger)
			}
		}
	}

	if weapon == "shield" {
		if canAtk {
			if target := bestShieldBashTarget(pos, ts.Enemies, wrange); target != nil {
				return finalizeWeaponAction(ts, weapon, wrange, atk(target, weapon))
			}
		}
		if near != nil && nearD <= 1 && canShv && near.DisruptedTicks <= 0 && !near.Stunned {
			return shove(near.ID)
		}
	}

	if weapon == "daggers" && canAtk {
		if target := bestBackstabTarget(pos, ts.Enemies, wrange); target != nil {
			return finalizeWeaponAction(ts, weapon, wrange, atk(target, weapon))
		}
	}

	if weapon == "spear" {
		if canAtk && near != nil && nearD <= wrange && ts.BraceReady {
			return finalizeWeaponAction(ts, weapon, wrange, atk(near, weapon))
		}
		if canAtk && near != nil && near.HasLOS && nearD >= 2 && nearD <= wrange && !ts.BraceReady {
			enemyRange := WeaponRanges[near.Weapon]
			if enemyRange <= 0 {
				enemyRange = 1
			}
			// Spend a safe beat bracing when the opponent cannot answer at
			// the current distance; the next thrust gains the spear's damage
			// and knockback bonus instead of throwing the signature away.
			if !near.CanAttack || nearD > enemyRange {
				return idle()
			}
		}
		if near != nil && nearD > wrange && nearD <= wrange+1 && !ts.BraceReady &&
			(strategy == "territorial" || strategy == "defensive") {
			return idle()
		}
	}

	if weapon == "grapple" && canAtk {
		if target := bestGrappleSlamTarget(pos, ts.Enemies, wrange); target != nil {
			return finalizeWeaponAction(ts, weapon, wrange, atk(target, weapon))
		}
	}

	if weapon == "bow" && canAtk {
		target := bestTarget(&ts, pos, ts.Enemies, wrange)
		if target != nil {
			dist := chebyshev(pos, target.Position)
			if shouldHoldBowCharge(ts, target, dist, wrange) {
				if near != nil && nearD <= 2 && canDodge {
					return dodgeSafe(ts, gridDirAway(ts.Position, near.Position))
				}
				return moveDirSafe(ts, stablePerpDir(gridDir(ts.Position, target.Position), botID))
			}
		}
	}

	// Preserve a grapple charge for survival: escaping pressure must happen
	// before offensive target grapples, otherwise low-HP bots can pull the
	// threat they meant to flee directly onto themselves.
	if near != nil && hpRatio < 0.45 {
		if ga := tryAnchorGrapple(ts, strategy, near, nearD, wrange); ga != nil {
			return *ga
		}
	}

	// A ready hit in range beats optional utility. Emergency health, CTF,
	// weapon setup, and low-HP escape above still retain higher priority.
	if !needsDefensiveDisengage {
		if attack := tryImmediateAttack(ts, weapon, wrange); attack != nil {
			return finalizeWeaponAction(ts, weapon, wrange, *attack)
		}
	}

	// === GRAVITY WELL: Deploy if 3+ enemies nearby ===
	if gw := tryGravityWell(ts, botID); gw != nil {
		setHasGravWell(botID, false)
		return *gw
	}

	// === UNIVERSAL GRAPPLE: Pull high-value targets into kill range ===
	if gp := tryUniversalGrapple(ts, weapon, wrange); gp != nil {
		return *gp
	}

	// === ANCHOR GRAPPLE: use the hook for repositioning, not just target pulls ===
	if ga := tryAnchorGrapple(ts, strategy, near, nearD, wrange); ga != nil {
		return *ga
	}

	// === TELEPORTER ESCAPE: spend the charge only for verified critical safety ===
	if tp := tryTeleporterPressureEscape(ts, strategy, near, nearD); tp != nil {
		return *tp
	}

	// === MINE PLACEMENT ===
	if mine := tryPlaceMineAdvanced(ts, strategy, weapon, near, nearD); mine != nil {
		return *mine
	}

	// === DODGE CHARGED ATTACKS: sidestep ready bow shots and braced spears ===
	// Skipped when we can land our own hit this tick — trading beats juking.
	if canDodge && !(canAtk && near != nil && nearD <= wrange) {
		for i := range ts.Enemies {
			e := &ts.Enemies[i]
			if !e.HasLOS || !e.IsAlive {
				continue
			}
			charged := (e.Weapon == "bow" && e.ChargedShotReady) ||
				(e.Weapon == "spear" && e.BraceReady)
			if !charged {
				continue
			}
			if chebyshev(pos, e.Position) <= WeaponRanges[e.Weapon]+1 {
				return dodgeSafe(ts, perpDir(gridDir(pos, e.Position)))
			}
		}
	}

	// === REACT: Got hit — only kite/defensive dodge; aggressive types fight back ===
	if ts.HitsThisTick > 0 && canDodge && near != nil && nearD <= wrange+3 {
		if !isAggStrat {
			return dodgeSafe(ts, perpDir(gridDir(pos, near.Position)))
		}
		// Aggressive bots: shove the attacker instead of dodging
		if canShv && nearD <= 1 {
			return shove(near.ID)
		}
	}

	// === FLEE: Assassins disengage at 20%, others at 15% ===
	fleeThreshold := 0.15
	if strategy == "assassin" {
		fleeThreshold = 0.20
	}
	if strategy != "berserker" && strategy != "territorial" && hpRatio < fleeThreshold && near != nil && nearD <= 2 {
		// Grab adjacent health pickup if available
		hp, hpD := nearestHealthPickup(pos, ts.Pickups, ts.HazardZones, ts.HasHazardKey)
		if hp != nil && hpD <= 1 {
			return useItem(hp.ID)
		}
		if canDodge {
			return dodgeSafe(ts, gridDirAway(pos, near.Position))
		}
	}

	// === DISENGAGE & BREAK LOS: melee bots under ranged fire at <45% HP,
	// anyone at <30% — duck behind terrain or grab a closer health pack.
	if needsDefensiveDisengage {
		threat := strongestRangedThreat(&ts)
		if threat == nil {
			threat = near
		}
		if threat != nil {
			// A health pickup closer than the threat beats hiding.
			if hp, hpD := nearestHealthPickupWithDanger(pos, ts.Pickups, ts.HazardZones, ts.HasHazardKey, ts.Danger); hp != nil && hpD < chebyshev(pos, threat.Position) {
				if hpD <= 1 {
					return useItem(hp.ID)
				}
				return moveTo(pos, hp.Position, ts.Danger)
			}
			if cover, ok := findLOSBreakCell(pos, threat.Position, ts.Danger); ok {
				return moveTo(pos, cover, ts.Danger)
			}
			return moveDirSafe(ts, gridDirAway(pos, threat.Position))
		}
	}

	// === ZONE SAFETY: Move into safe zone, but attack on the way ===
	if !ts.InZone {
		// Zone edge tactics: shove enemies OUT of zone
		if near != nil && nearD <= 1 && canShv {
			// If enemy is between us and zone edge, shove them further out
			return shove(near.ID)
		}
		if near != nil && nearD <= wrange && canAtk {
			return finalizeWeaponAction(ts, weapon, wrange, atk(near, weapon))
		}
		return moveTo(pos, ts.ZoneCenter, ts.Danger)
	}

	if (ts.FastZone || ts.HazardStorm) && ts.ZoneDist <= 3 && near == nil {
		return moveTo(pos, ts.ZoneTargetCenter, ts.Danger)
	}

	// === ZONE EDGE TACTICS: Shove enemies out of zone ===
	if ts.ZoneDist <= 3 && near != nil && nearD <= 1 && canShv {
		// Check if enemy is between us and zone edge → shove them OUT
		enemyZoneDist := chebyshev(near.Position, ts.ZoneCenter) - ts.ZoneRadius
		if enemyZoneDist > -2 { // enemy near zone edge
			return shove(near.ID)
		}
	}

	// === PROACTIVE ZONE DRIFT: reposition ahead of the shrink when the zone
	// edge is close (or the round is late and we're far from the next zone)
	// and no enemy is within fighting distance. If carved terrain blocks the
	// direct route, start sooner so rings, rooms, islands, and caves do not
	// strand the bot behind a long detour during the shrink.
	if near == nil || nearD > wrange+2 {
		distToZoneTarget := chebyshev(pos, ts.ZoneTargetCenter)
		driftMargin := 4.0
		lateTick := 1200
		if terrain := getTerrain(); terrain != nil {
			start := [2]int{int(math.Round(pos[0])), int(math.Round(pos[1]))}
			goal := [2]int{int(math.Round(ts.ZoneTargetCenter[0])), int(math.Round(ts.ZoneTargetCenter[1]))}
			if terrain.gridLineBlocked(start, goal) {
				driftMargin = 7
				lateTick = 900
			}
		}
		lateRound := ts.RoundTick > lateTick
		if ts.ZoneDist < driftMargin || (lateRound && distToZoneTarget > ts.ZoneTargetRadius) {
			if a := moveTo(pos, ts.ZoneTargetCenter, ts.Danger); a.Action != "idle" {
				return a
			}
		}
	}

	// === OBJECTIVE PLAY: contest or claim the capture pad when the fight allows it ===
	if pad := tryCapturePadObjective(ts, strategy, near, nearD, botID); pad != nil {
		return *pad
	}

	// === SMART PICKUPS ===
	if pickup := trySmartPickup(ts, strategy, weapon); pickup != nil {
		return *pickup
	}

	// === ANTI-BOUNTY AWARENESS (kill_streak >= 3) ===
	// Adjustments are handled within each strategy below

	// === NO ENEMIES VISIBLE: Hunt them down ===
	// (trySmartPickup already ran unconditionally above.)
	if visibleEnemies == 0 {
		for _, h := range ts.Hints {
			if h.HintType == "pickup" {
				target := [2]float64{
					pos[0] + h.Direction[0]*math.Min(h.Distance, 6),
					pos[1] + h.Direction[1]*math.Min(h.Distance, 6),
				}
				if action := moveTo(pos, target, ts.Danger); action.Action != "idle" {
					return action
				}
			}
		}
		for _, h := range ts.Hints {
			if h.HintType == "bot" {
				target := [2]float64{pos[0] + h.Direction[0]*h.Distance, pos[1] + h.Direction[1]*h.Distance}
				if action := moveTo(pos, target, ts.Danger); action.Action != "idle" {
					return action
				}
			}
		}
		p, pd := nearestPickup(pos, ts.Pickups, ts.HazardZones, ts.HasHazardKey)
		if p != nil && pd <= 3 {
			if action := moveTo(pos, p.Position, ts.Danger); action.Action != "idle" {
				return action
			}
		}
		const patrolRadius = 5.0
		if chebyshev(pos, ts.ZoneTargetCenter) > patrolRadius {
			if towardCenter := moveTo(pos, ts.ZoneTargetCenter, ts.Danger); towardCenter.Action != "idle" {
				return towardCenter
			}
		}
		// Reaching the next-zone center is not a reason to stop hunting. A
		// deterministic local patrol keeps isolated survivors moving until a
		// bot hint, pickup, or opponent enters sensor range.
		return guardPatrol(ts, patrolRadius, botID)
	}

	// === COMBAT: Strategy-specific ===
	switch strategy {
	case "aggressive":
		return finalizeWeaponAction(ts, weapon, wrange, aiAggressive(ts, near, nearD, wrange, weapon, canAtk, canDodge, botID))
	case "berserker":
		return finalizeWeaponAction(ts, weapon, wrange, aiBerserker(ts, near, nearD, wrange, weapon, canAtk, canDodge))
	case "kite":
		return finalizeWeaponAction(ts, weapon, wrange, aiKite(ts, near, nearD, wrange, weapon, canAtk, canDodge, botID))
	case "assassin":
		return finalizeWeaponAction(ts, weapon, wrange, aiAssassin(ts, near, nearD, wrange, weapon, canAtk, canDodge, botID))
	case "defensive":
		return finalizeWeaponAction(ts, weapon, wrange, aiDefensive(ts, near, nearD, wrange, weapon, canAtk, canDodge, botID))
	case "territorial":
		return finalizeWeaponAction(ts, weapon, wrange, aiTerritorial(ts, near, nearD, wrange, weapon, canAtk, canDodge, botID))
	default:
		return finalizeWeaponAction(ts, weapon, wrange, aiAggressive(ts, near, nearD, wrange, weapon, canAtk, canDodge, botID))
	}
}

// chaseApproach closes distance on a target. Melee bots approaching a ranged
// enemy zigzag deterministically (tick parity) between the direct BFS step
// and a perpendicular offset, making charged shots harder to line up.
func chaseApproach(ts tickState, target *entity, wrange float64, weapon string) actionResult {
	dist := chebyshev(ts.Position, target.Position)
	if !isMelee(weapon) || dist <= wrange || (target.Weapon != "bow" && target.Weapon != "staff") {
		return moveTo(ts.Position, target.Position, ts.Danger)
	}
	dir := bfsDir(ts.Position, target.Position, ts.Danger)
	if dir[0] == 0 && dir[1] == 0 {
		return idle()
	}
	// Offset perpendicular on alternating tick pairs while still far out.
	if dist > 2 && (ts.Tick/2)%2 == 1 {
		perp := [2]float64{-dir[1], dir[0]} // fixed CW perpendicular — deterministic
		px, py := int(perp[0]), int(perp[1])
		cx, cy := int(math.Round(ts.Position[0])), int(math.Round(ts.Position[1]))
		t := getTerrain()
		if (px != 0 || py != 0) && (t == nil || !t.isMoveBlocked(cx, cy, px, py)) && !ts.Danger.has(cx+px, cy+py) {
			return moveDir(perp)
		}
	}
	return moveDir(dir)
}

// baitPunish handles the adjacent stand-off: our weapon is cooling down while
// the enemy's is ready — dodging (or shoving) beats strafing into the swing.
func baitPunish(ts tickState, near *entity, canDodge bool) *actionResult {
	if near == nil || !near.CanAttack || !isMelee(near.Weapon) {
		return nil
	}
	if canShv := ts.ShoveCool <= 0; canShv {
		a := shove(near.ID)
		return &a
	}
	if canDodge && ts.DodgeCool <= 0 {
		a := dodgeSafe(ts, perpDir(gridDir(ts.Position, near.Position)))
		return &a
	}
	return nil
}

// AGGRESSIVE: Rush enemies, attack on cooldown, shove when close, chase relentlessly.
// Used by Lancers (spear) — knockback into walls for bonus damage.
func aiAggressive(ts tickState, near *entity, nearD, wrange float64, weapon string, canAtk, canDodge bool, botID string) actionResult {
	if near == nil {
		return moveTo(ts.Position, ts.ZoneTargetCenter, ts.Danger)
	}
	canShv := ts.ShoveCool <= 0

	// Attack best target in range
	target := bestTarget(&ts, ts.Position, ts.Enemies, wrange)
	if target != nil && canAtk {
		return atk(target, weapon)
	}

	// Adjacent on cooldown — shove to stun, then burst next tick
	if nearD <= 1 && canShv {
		return shove(near.ID)
	}

	// Close on cooldown — advance to stay in melee
	if nearD <= wrange && !canAtk {
		if nearD > 1 {
			return moveTo(ts.Position, near.Position, ts.Danger)
		}
		// Adjacent with their weapon ready and ours cooling — don't stand in the swing.
		if p := baitPunish(ts, near, canDodge); p != nil {
			return *p
		}
		return moveDirSafe(ts, stablePerpDir(gridDir(ts.Position, near.Position), botID))
	}

	// Chase — zigzag against ranged kiters so shots are harder to line up
	return chaseApproach(ts, near, wrange, weapon)
}

// BERSERKER: Never retreat, dodge INTO enemies, shove constantly, fight to the death.
func aiBerserker(ts tickState, near *entity, nearD, wrange float64, weapon string, canAtk, canDodge bool) actionResult {
	if near == nil {
		return moveTo(ts.Position, ts.ZoneTargetCenter, ts.Danger)
	}
	canShv := ts.ShoveCool <= 0

	if nearD <= wrange && canAtk {
		target := bestTarget(&ts, ts.Position, ts.Enemies, wrange)
		if target != nil {
			return atk(target, weapon)
		}
		return atk(near, weapon)
	}

	if nearD <= 1 && canShv {
		return shove(near.ID)
	}

	if nearD > 1 && nearD <= wrange+3 && canDodge {
		return dodgeSafe(ts, gridDir(ts.Position, near.Position))
	}

	// Chase — zigzag against ranged kiters so shots are harder to line up
	return chaseApproach(ts, near, wrange, weapon)
}

func preferredKiteRange(weapon string, wrange float64) float64 {
	switch weapon {
	case "bow":
		return math.Max(5, wrange-1.5)
	case "staff":
		return math.Max(4, wrange-1.5)
	default:
		return math.Max(3, wrange-1)
	}
}

// KITE: prioritize ranged spacing and staff clusters. Bow users hold a
// longer lane than staff users instead of sharing the old 3.5-tile sweet spot.
func aiKite(ts tickState, near *entity, nearD, wrange float64, weapon string, canAtk, canDodge bool, botID string) actionResult {
	if near == nil {
		return moveTo(ts.Position, ts.ZoneTargetCenter, ts.Danger)
	}
	canShv := ts.ShoveCool <= 0
	isBounty := ts.KillStreak >= 3

	// Delayed staff impacts and bow projectiles are poor point-blank trades.
	// Create separation before committing the ranged attack.
	if (weapon == "bow" || weapon == "staff") && nearD <= 1 {
		if canShv {
			return shove(near.ID)
		}
		if canDodge {
			return dodgeSafe(ts, gridDirAway(ts.Position, near.Position))
		}
		return moveDirSafe(ts, gridDirAway(ts.Position, near.Position))
	}

	// Staff AoE: target cluster center instead of individual enemies
	if weapon == "staff" && canAtk {
		if cast, ok := bestStaffCast(&ts, ts.Position, ts.Enemies, wrange); ok {
			return *cast
		}
	} else if canAtk {
		// Non-staff kite weapons
		target := bestTarget(&ts, ts.Position, ts.Enemies, wrange)
		if target != nil {
			return atk(target, weapon)
		}
		if nearD <= wrange {
			return atk(near, weapon)
		}
	}

	// Enemy at range 1 — shove THEN dodge away
	if nearD <= 1 {
		if canShv {
			return shove(near.ID)
		}
		if canDodge {
			return dodgeSafe(ts, gridDirAway(ts.Position, near.Position))
		}
		return moveDirSafe(ts, gridDirAway(ts.Position, near.Position))
	}

	// Maintain a weapon-specific lane rather than letting bow users drift into
	// the staff's much shorter old sweet spot.
	idealRange := preferredKiteRange(weapon, wrange)
	if isBounty {
		idealRange = wrange - 0.5 // Play extra cautiously when bounty target
	}

	if nearD < idealRange-0.5 && !canAtk {
		// Too close — back off
		if canDodge {
			return dodgeSafe(ts, gridDirAway(ts.Position, near.Position))
		}
		return moveDirSafe(ts, gridDirAway(ts.Position, near.Position))
	}

	// On cooldown at range — strafe to be harder to hit
	if nearD <= wrange && !canAtk {
		return moveDirSafe(ts, stablePerpDir(gridDir(ts.Position, near.Position), botID))
	}

	// Too far — approach to get in range
	if nearD > wrange {
		// Approach cluster if multiple enemies
		clusterCenter, clusterCount := enemyClusterCenter(ts.Enemies, 3)
		if clusterCount >= 2 {
			return moveTo(ts.Position, clusterCenter, ts.Danger)
		}
		return moveTo(ts.Position, near.Position, ts.Danger)
	}

	return moveTo(ts.Position, near.Position, ts.Danger)
}

// ASSASSIN: Hunt weakest target, grapple for gap-close, shove + burst, disengage at 20%.
// Used by Hooks (grapple) and Shredders (daggers).
func aiAssassin(ts tickState, near *entity, nearD, wrange float64, weapon string, canAtk, canDodge bool, botID string) actionResult {
	canShv := ts.ShoveCool <= 0
	isBounty := ts.KillStreak >= 3

	// Find weakest enemy (always hunt the lowest HP target)
	prey, preyD := weakest(ts.Position, ts.Enemies)
	if prey == nil {
		if near != nil {
			prey = near
			preyD = nearD
		} else {
			return moveTo(ts.Position, ts.ZoneTargetCenter, ts.Danger)
		}
	}

	// Bounty target — grab health packs more aggressively
	if isBounty {
		hp, hpD := nearestHealthPickup(ts.Position, ts.Pickups, ts.HazardZones, ts.HasHazardKey)
		if hp != nil && hpD <= 3 && ts.HP/ts.MaxHP < 0.6 {
			if hpD <= 1 {
				return useItem(hp.ID)
			}
			return moveTo(ts.Position, hp.Position, ts.Danger)
		}
	}

	// Grapple weapon: attack at range 3-4 to pull toward target, then shove when adjacent
	if weapon == "grapple" {
		// In grapple range — attack (server handles the pull)
		if preyD <= wrange && canAtk {
			return atk(prey, weapon)
		}
		// Adjacent after grapple — shove for stun, then burst next tick
		if preyD <= 1 && canShv {
			return shove(prey.ID)
		}
	} else if weapon == "daggers" {
		// Daggers: dodge INTO target for gap close, then shove + burst
		if preyD <= wrange && canAtk {
			return atk(prey, weapon)
		}
		if !canAtk && preyD <= 3 && !prey.RearExposed && !isRearArc(ts.Position, *prey) {
			if flank, ok := daggerFlankPosition(ts, prey); ok && chebyshev(ts.Position, flank) > 0 {
				return moveTo(ts.Position, flank, ts.Danger)
			}
		}
		if preyD <= 1 && canShv {
			return shove(prey.ID)
		}
		// Gap close with dodge toward prey
		if preyD > 1 && preyD <= 3 && canDodge {
			return dodgeSafe(ts, gridDir(ts.Position, prey.Position))
		}
	} else {
		// Generic assassin behavior
		if preyD <= wrange && canAtk {
			return atk(prey, weapon)
		}
		if preyD <= 1 && canShv {
			return shove(prey.ID)
		}
	}

	// Close — dodge IN to gap-close for the kill
	if preyD > 1 && preyD <= wrange+2 && canDodge {
		return dodgeSafe(ts, gridDir(ts.Position, prey.Position))
	}

	// Hunt them down
	return moveTo(ts.Position, prey.Position, ts.Danger)
}

var guardPatrolDirections = [...][2]float64{
	{1, 0}, {1, 1}, {0, 1}, {-1, 1},
	{-1, 0}, {-1, -1}, {0, -1}, {1, -1},
}

func guardPatrolStart(botID string) int {
	var hash uint32 = 2166136261
	for i := 0; i < len(botID); i++ {
		hash ^= uint32(botID[i])
		hash *= 16777619
	}
	return int(hash % uint32(len(guardPatrolDirections)))
}

func guardPatrolStepSafe(ts tickState, direction [2]float64, terrain *botTerrain) bool {
	dx, dy := int(fsign(direction[0])), int(fsign(direction[1]))
	if dx == 0 && dy == 0 {
		return false
	}
	cx, cy := int(math.Round(ts.Position[0])), int(math.Round(ts.Position[1]))
	if terrain != nil && terrain.isMoveBlocked(cx, cy, dx, dy) {
		return false
	}
	next := [2]float64{ts.Position[0] + float64(dx), ts.Position[1] + float64(dy)}
	if ts.ZoneTargetRadius > 0 && chebyshev(next, ts.ZoneTargetCenter) > ts.ZoneTargetRadius {
		return false
	}
	if ts.ZoneRadius > 0 && chebyshev(next, ts.ZoneCenter) > ts.ZoneRadius {
		return false
	}
	return !ts.Danger.has(cx+dx, cy+dy)
}

// guardPatrolMove constrains every patrol step, not just its destination.
// The general BFS may choose an equally short diagonal that briefly crosses a
// zone boundary while routing between two in-zone waypoints.
func guardPatrolMove(ts tickState, target [2]float64, terrain *botTerrain) actionResult {
	if chebyshev(ts.Position, target) < 0.5 {
		return idle()
	}
	// Prefer the direct safe step. On an open grid, BFS has many equally short
	// paths and its fixed neighbor order can pick the opposite lateral step,
	// making an otherwise smooth ring patrol visibly reverse at midpoints.
	if direct := gridDir(ts.Position, target); guardPatrolStepSafe(ts, direct, terrain) {
		return moveDir(direct)
	}
	if action := moveTo(ts.Position, target, ts.Danger); action.Direction != nil && guardPatrolStepSafe(ts, *action.Direction, terrain) {
		return action
	}

	bestDistance := math.Inf(1)
	bestDirection := [2]float64{}
	for _, direction := range guardPatrolDirections {
		if !guardPatrolStepSafe(ts, direction, terrain) {
			continue
		}
		next := [2]float64{ts.Position[0] + direction[0], ts.Position[1] + direction[1]}
		if distance := chebyshev(next, target); distance < bestDistance {
			bestDistance = distance
			bestDirection = direction
		}
	}
	return moveDir(bestDirection)
}

// guardPatrol keeps defensive bots moving around their assigned center without
// leaving either the current safe zone or the next territory. Waypoints and
// tie-breaking are stable per bot so guards spread out without random jitter.
func guardPatrolRing(ts tickState, radius float64, botID string, terrain *botTerrain) (actionResult, bool) {
	if radius < 1 {
		return actionResult{}, false
	}

	var waypoints [len(guardPatrolDirections)][2]float64
	var usable [len(guardPatrolDirections)]bool
	for i, direction := range guardPatrolDirections {
		candidate := [2]float64{
			ts.ZoneTargetCenter[0] + direction[0]*radius,
			ts.ZoneTargetCenter[1] + direction[1]*radius,
		}
		waypoints[i] = candidate
		if ts.ZoneTargetRadius > 0 && chebyshev(candidate, ts.ZoneTargetCenter) > ts.ZoneTargetRadius {
			continue
		}
		if ts.ZoneRadius > 0 && chebyshev(candidate, ts.ZoneCenter) > ts.ZoneRadius {
			continue
		}
		cellX, cellY := int(math.Round(candidate[0])), int(math.Round(candidate[1]))
		if terrain != nil && terrain.isBlocked(cellX, cellY) {
			continue
		}
		if ts.Danger.has(cellX, cellY) {
			continue
		}
		usable[i] = true
	}

	start := guardPatrolStart(botID)
	closest := -1
	closestDistance := math.Inf(1)
	for offset := range guardPatrolDirections {
		i := (start + offset) % len(guardPatrolDirections)
		if !usable[i] {
			continue
		}
		distance := chebyshev(ts.Position, waypoints[i])
		if distance < closestDistance {
			closest = i
			closestDistance = distance
		}
	}
	if closest < 0 {
		return actionResult{}, false
	}

	// Always advance clockwise from the nearest sector. Retargeting the nearest
	// waypoint itself at a segment midpoint selects the point just left and
	// creates an endless two-cell reversal.
	first := (closest + 1) % len(guardPatrolDirections)
	for offset := range guardPatrolDirections {
		i := (first + offset) % len(guardPatrolDirections)
		if !usable[i] {
			continue
		}
		if action := guardPatrolMove(ts, waypoints[i], terrain); action.Action != "idle" {
			return action, true
		}
	}
	return actionResult{}, false
}

func guardPatrol(ts tickState, requestedRadius float64, botID string) actionResult {
	terrain := getTerrain()
	radiusLimit := math.Floor(ts.ZoneTargetRadius) - 1
	if radiusLimit < 1 {
		radiusLimit = 1
	}
	maxRadius := int(math.Floor(math.Min(requestedRadius, radiusLimit)))
	for radius := maxRadius; radius >= 1; radius-- {
		if action, ok := guardPatrolRing(ts, float64(radius), botID, terrain); ok {
			return action
		}
	}

	// No complete ring survived nearby walls or dynamic danger. Keep moving on
	// any safe local step instead of idling at the center forever.
	start := guardPatrolStart(botID)
	for offset := range guardPatrolDirections {
		direction := guardPatrolDirections[(start+offset)%len(guardPatrolDirections)]
		if guardPatrolStepSafe(ts, direction, terrain) {
			return moveDir(direction)
		}
	}
	return idle()
}

// DEFENSIVE: Counter-attack focused, shove intruders, patrol ground but fight.
func aiDefensive(ts tickState, near *entity, nearD, wrange float64, weapon string, canAtk, canDodge bool, botID string) actionResult {
	if near == nil {
		d := chebyshev(ts.Position, ts.ZoneTargetCenter)
		if d > 5 {
			return moveTo(ts.Position, ts.ZoneTargetCenter, ts.Danger)
		}
		return guardPatrol(ts, 4, botID)
	}
	canShv := ts.ShoveCool <= 0

	target := bestTarget(&ts, ts.Position, ts.Enemies, wrange)
	if target != nil && canAtk {
		return atk(target, weapon)
	}

	if nearD <= 1 && canShv {
		return shove(near.ID)
	}

	if nearD <= wrange+2 && !canAtk {
		if nearD > 1 {
			return moveTo(ts.Position, near.Position, ts.Danger)
		}
		// Adjacent with their weapon ready and ours cooling — don't stand in the swing.
		if p := baitPunish(ts, near, canDodge); p != nil {
			return *p
		}
		return moveDirSafe(ts, stablePerpDir(gridDir(ts.Position, near.Position), botID))
	}

	if nearD <= wrange+5 {
		return moveTo(ts.Position, near.Position, ts.Danger)
	}

	if chebyshev(ts.Position, ts.ZoneTargetCenter) > 5 {
		return moveTo(ts.Position, ts.ZoneTargetCenter, ts.Danger)
	}
	return guardPatrol(ts, 4, botID)
}

// TERRITORIAL: Hold zone TARGET center, shove EVERY adjacent enemy, place mines at center.
// Used by Juggernauts (shield) — 180 HP + 50% block = unkillable.
func aiTerritorial(ts tickState, near *entity, nearD, wrange float64, weapon string, canAtk, canDodge bool, botID string) actionResult {
	canShv := ts.ShoveCool <= 0
	distToCenter := chebyshev(ts.Position, ts.ZoneTargetCenter)

	if near == nil {
		if distToCenter > 3 {
			return moveTo(ts.Position, ts.ZoneTargetCenter, ts.Danger)
		}
		return guardPatrol(ts, 3, botID)
	}

	// Priority 1: Shove EVERY adjacent enemy on cooldown (free stun + pushes them out)
	if nearD <= 1 && canShv {
		// Find the best shove target — prefer enemies near zone edge
		bestShove := near
		for i := range ts.Enemies {
			e := &ts.Enemies[i]
			if chebyshev(ts.Position, e.Position) <= 1 {
				// Prefer shoving enemies that are near the zone edge
				eDist := chebyshev(e.Position, ts.ZoneCenter) - ts.ZoneRadius
				nDist := chebyshev(bestShove.Position, ts.ZoneCenter) - ts.ZoneRadius
				if eDist > nDist {
					bestShove = e
				}
			}
		}
		return shove(bestShove.ID)
	}

	// Priority 2: Attack anything in range (alternate between shove and attack on different targets)
	target := bestTarget(&ts, ts.Position, ts.Enemies, wrange)
	if target != nil && canAtk {
		return atk(target, weapon)
	}

	// Priority 3: Chase enemies that enter territory, but NEVER more than 5 tiles from center
	if nearD <= wrange+4 && distToCenter <= 5 {
		return moveTo(ts.Position, near.Position, ts.Danger)
	}

	// Priority 4: Return to zone TARGET center (anticipate shrink)
	if distToCenter > 2 {
		return moveTo(ts.Position, ts.ZoneTargetCenter, ts.Danger)
	}

	// At center with no target in range, keep a deterministic guard patrol.
	return guardPatrol(ts, 3, botID)
}
