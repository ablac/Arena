#!/usr/bin/env python3
"""
🐬 ANISMIN BOT v2 — Staff AoE Dominator
Zero tokens. Pure math. Maximum carnage.

Strategy: Staff (12 range, AoE 3.0 radius) — perfect for packed arenas
Stats: HP 5 / Speed 4 / Attack 10 / Defense 1 = 150 HP, 5.0 speed, 2.0x atk
Effective DPS at 12 range: 18 * 2.0 / 1.3 * AoE bonus ≈ insane

Weapon reference:
  sword:   25 dmg, 2.5 range, 0.5s cd, cleave (50% to 2 nearby)
  bow:     12 dmg, 15.0 range, 1.4s cd, projectile
  daggers: 12 dmg, 1.5 range, 0.3s cd, 25% double strike
  shield:  15 dmg, 1.8 range, 0.7s cd, 50% passive dmg reduction
  spear:   20 dmg, 3.5 range, 0.7s cd, knockback 2.0
  staff:   18 dmg, 12.0 range, 1.3s cd, AoE radius 3.0
"""

import asyncio, json, logging, math, os, time
from collections import defaultdict
from pathlib import Path
import websockets

logging.basicConfig(level=logging.INFO, format="%(asctime)s [ANISMIN] %(message)s")
log = logging.getLogger("anismin")

API_KEY = os.environ.get("ARENA_API_KEY", "")
SERVER  = os.environ.get("ARENA_SERVER", "wss://arena.angel-serv.com")
WS_URL  = f"{SERVER}/ws/bot?key={API_KEY}"

DATA_DIR = Path(__file__).parent / "data"
DATA_DIR.mkdir(exist_ok=True)
STATS_FILE = DATA_DIR / "match_history.json"

WEAPONS = {
    "sword":   {"dmg": 25, "range": 2.5,  "cd": 0.5},
    "bow":     {"dmg": 12, "range": 15.0, "cd": 1.4},
    "daggers": {"dmg": 12, "range": 1.5,  "cd": 0.3},
    "shield":  {"dmg": 15, "range": 1.8,  "cd": 0.7},
    "spear":   {"dmg": 20, "range": 3.5,  "cd": 0.7},
    "staff":   {"dmg": 18, "range": 12.0, "cd": 1.3},
}
MY_WEAPON = "staff"
MY_RANGE  = WEAPONS[MY_WEAPON]["range"]  # 12.0

def dist(a, b):
    return math.sqrt((a[0]-b[0])**2 + (a[1]-b[1])**2)

def normalize(dx, dy):
    d = math.sqrt(dx*dx + dy*dy)
    return (dx/d, dy/d) if d > 1e-10 else (0.0, 0.0)

def dir_to(me, target):
    return normalize(target[0]-me[0], target[1]-me[1])

def dir_away(me, threat):
    dx, dy = dir_to(me, threat)
    return (-dx, -dy)

def weighted_center(positions):
    if not positions: return None
    cx = sum(p[0] for p in positions) / len(positions)
    cy = sum(p[1] for p in positions) / len(positions)
    return (cx, cy)


class MatchTracker:
    def __init__(self):
        self.data = {"matches":0,"total_kills":0,"total_deaths":0,"enemy_profiles":{}}
        self._load()

    def _load(self):
        if STATS_FILE.exists():
            try:
                with open(STATS_FILE) as f:
                    self.data.update(json.load(f))
                log.info(f"History: {self.data['matches']} matches, "
                         f"K/D {self.data['total_kills']}/{self.data['total_deaths']}")
            except Exception: pass

    def save(self):
        try:
            with open(STATS_FILE,"w") as f: json.dump(self.data, f, indent=2)
        except Exception as e: log.warning(f"save failed: {e}")

    def record_kill(self, vid, vw):
        self.data["total_kills"] += 1
        p = self.data["enemy_profiles"].setdefault(vid, {"weapon":vw,"kills_on_me":0,"my_kills":0})
        p["my_kills"] += 1; p["weapon"] = vw

    def record_death(self, kid, kw):
        self.data["total_deaths"] += 1
        p = self.data["enemy_profiles"].setdefault(kid, {"weapon":kw,"kills_on_me":0,"my_kills":0})
        p["kills_on_me"] += 1; p["weapon"] = kw

    def threat(self, bot_id):
        p = self.data["enemy_profiles"].get(bot_id)
        return p["kills_on_me"] - p["my_kills"]*0.5 if p else 0

    def end_match(self):
        self.data["matches"] += 1; self.save()


class Brain:
    def __init__(self, tracker):
        self.tracker = tracker
        self.tick_count = 0
        self.last_hp = 9999
        self.last_pos = None
        self.stuck_ticks = 0
        self.strafe_dir = 1
        self.combat_ticks_idle = 0
        self.kill_streak = 0

    def decide(self, state, nearby):
        self.tick_count += 1

        pos        = tuple(state.get("position", [0,0]))
        hp         = state.get("hp", 0)
        max_hp     = state.get("max_hp", 150)
        hp_pct     = hp / max(max_hp, 1)
        wready     = state.get("weapon_ready", True)
        dodge_cd   = state.get("dodge_cooldown", 0)
        dodge_rdy  = dodge_cd <= 0
        in_zone    = state.get("in_safe_zone", True)
        zone_ctr   = state.get("zone_center", [1000,1000])
        zone_rad   = state.get("zone_radius", 1000)
        stun       = state.get("stun_ticks", 0)
        effects    = state.get("active_effects", []) or []
        hits       = state.get("hits_received", []) or []

        if not state.get("is_alive", True) or hp <= 0:
            return {"type":"action","action":"idle"}

        if stun > 0:
            return {"type":"action","action":"idle"}

        took_dmg = hp < self.last_hp - 0.5
        self.last_hp = hp

        if self.last_pos and dist(pos, self.last_pos) < 0.5:
            self.stuck_ticks += 1
        else:
            self.stuck_ticks = 0
        self.last_pos = pos

        enemies  = [e for e in nearby if e.get("type")=="bot" and e.get("is_alive",False)]
        pickups  = [e for e in nearby if e.get("type")=="pickup"]

        # ── EMERGENCY DODGE ──
        if dodge_rdy and took_dmg and hp_pct < 0.35:
            return {"type":"action","action":"dodge","direction":list(self._dodge_dir(pos,enemies,zone_ctr))}

        # ── ZONE ──
        if not in_zone:
            dx,dy = dir_to(pos, zone_ctr)
            return {"type":"action","action":"move","direction":[dx,dy]}

        # ── PICKUPS ──
        pickup = self._best_pickup(pos, pickups, hp_pct, enemies)
        if pickup:
            pp = pickup["position"]
            d  = dist(pos, tuple(pp))
            if d <= 2.0:
                return {"type":"action","action":"use_item","item_id":pickup.get("id","")}
            if d <= MY_RANGE and not self._enemy_between(pos, tuple(pp), enemies):
                dx,dy = dir_to(pos, tuple(pp))
                return {"type":"action","action":"move","direction":[dx,dy]}

        # ── COMBAT ──
        if enemies:
            target = self._pick_target(pos, enemies, hp_pct)
            if target:
                return self._fight(pos, target, enemies, hp_pct, wready, dodge_rdy, zone_ctr)

        # ── HUNT ── move to zone center to find fights
        self.combat_ticks_idle += 1
        if self.combat_ticks_idle > 20:
            dx,dy = dir_to(pos, zone_ctr)
            return {"type":"action","action":"move","direction":[dx,dy]}

        return {"type":"action","action":"idle"}

    def _pick_target(self, pos, enemies, hp_pct):
        best, best_score = None, -9e9
        for e in enemies:
            ep  = tuple(e["position"])
            d   = dist(pos, ep)
            ehp = e.get("hp", 100)
            emx = e.get("max_hp", 100)
            ew  = e.get("weapon","sword")
            eid = e.get("bot_id","")

            score = 0
            # In attack range = top priority
            if d <= MY_RANGE:
                score += 100
            # Low HP = kill it
            if ehp / max(emx,1) < 0.3: score += 60
            elif ehp / max(emx,1) < 0.6: score += 30
            # Closer = better
            score -= d * 0.5
            # Historical threat
            score += self.tracker.threat(eid) * 5 if hp_pct > 0.6 else 0

            if score > best_score:
                best, best_score = e, score
        return best

    def _fight(self, pos, target, all_enemies, hp_pct, wready, dodge_rdy, zone_ctr):
        tp  = tuple(target["position"])
        tid = target.get("bot_id","")
        tw  = target.get("weapon","sword")
        d   = dist(pos, tp)

        # ATTACK if in range and ready
        if d <= MY_RANGE and wready:
            log.info(f"🔥 STAFF BLAST {target.get('name','?')} dist={d:.1f}")
            return {"type":"action","action":"attack","target":tid}

        # Kite: stay just inside staff range (10-12), strafe vs melee threats
        close_melee = [e for e in all_enemies if dist(pos,tuple(e["position"]))<4]
        if close_melee and dodge_rdy:
            return {"type":"action","action":"dodge","direction":list(self._dodge_dir(pos,close_melee,zone_ctr))}

        # Move toward target if out of range
        if d > MY_RANGE:
            dx,dy = dir_to(pos, tp)
            # Slight strafe while approaching to dodge
            if d > MY_RANGE * 1.5:
                self.strafe_dir *= -1 if self.tick_count % 8 == 0 else 1
                perp = (-dy * 0.3 * self.strafe_dir, dx * 0.3 * self.strafe_dir)
                dx = dx + perp[0]; dy = dy + perp[1]
                dx,dy = normalize(dx,dy)
            return {"type":"action","action":"move","direction":[dx,dy]}

        # In range but weapon not ready — orbit to avoid melee
        self.strafe_dir *= -1 if self.tick_count % 5 == 0 else 1
        perp = (-((tp[1]-pos[1])/max(d,0.01)), (tp[0]-pos[0])/max(d,0.01))
        dx = perp[0]*self.strafe_dir; dy = perp[1]*self.strafe_dir
        return {"type":"action","action":"move","direction":[dx,dy]}

    def _dodge_dir(self, pos, enemies, zone_ctr):
        if not enemies:
            return dir_to(pos, zone_ctr)
        ec = weighted_center([tuple(e["position"]) for e in enemies])
        away = dir_away(pos, ec)
        d_zone = dist(pos, zone_ctr)
        if d_zone > 300:
            zd = dir_to(pos, zone_ctr)
            return normalize(away[0]*0.5 + zd[0]*0.5, away[1]*0.5 + zd[1]*0.5)
        return away

    def _best_pickup(self, pos, pickups, hp_pct, enemies):
        best, best_s = None, 0
        for p in pickups:
            pp = tuple(p["position"])
            d  = dist(pos, pp)
            pt = p.get("pickup_type","")
            s  = 0
            if pt == "health":         s += (1-hp_pct)*100
            elif pt == "damage_boost": s += 50
            elif pt == "shield_bubble":s += 40 + (1-hp_pct)*20
            elif pt == "speed_boost":  s += 25
            s -= d * 1.5
            for e in enemies:
                if dist(tuple(e["position"]),pp) < d: s -= 10
            if s > best_s:
                best, best_s = p, s
        return best

    def _enemy_between(self, pos, target, enemies):
        dt = dist(pos, target)
        for e in enemies:
            ep = tuple(e["position"])
            if dist(pos,ep) < dt and dist(ep,target) < dt and dist(pos,ep) < 10:
                return True
        return False

    def on_kill(self, vid, vw):
        self.tracker.record_kill(vid, vw)
        self.kill_streak += 1
        log.info(f"🔪 KILL #{self.tracker.data['total_kills']} streak={self.kill_streak}")
        self.combat_ticks_idle = 0

    def on_death(self, kid, kw):
        self.tracker.record_death(kid, kw)
        self.kill_streak = 0
        kd = self.tracker.data
        log.info(f"☠️ Killed by {kw} | K/D {kd['total_kills']}/{kd['total_deaths']}")


async def run():
    if not API_KEY:
        log.error("Set ARENA_API_KEY!"); return

    tracker = MatchTracker()
    brain   = Brain(tracker)

    while True:
        try:
            log.info(f"🐬 Connecting to {SERVER}...")
            async with websockets.connect(WS_URL, ping_interval=20, ping_timeout=30) as ws:
                raw = await ws.recv()
                msg = json.loads(raw)
                if msg.get("type") != "connected":
                    log.error(f"Expected connected, got {msg.get('type')}"); continue

                log.info(f"🐬 Connected as {msg.get('bot_id')}")

                await ws.send(json.dumps({
                    "type": "select_loadout",
                    "weapon": MY_WEAPON,
                    "stats": {"hp":5, "speed":4, "attack":10, "defense":1},
                    "fallback_behavior": "aggressive",
                }))

                # Read loadout response (may be error then confirmed, or just confirmed)
                for _ in range(3):
                    raw = await ws.recv()
                    msg = json.loads(raw)
                    t = msg.get("type")
                    if t == "loadout_confirmed":
                        s = msg.get("computed", {})
                        log.info(f"⚔️ Loadout: {MY_WEAPON} | HP:{s.get('max_hp')} "
                                 f"SPD:{s.get('move_speed')} ATK:{s.get('attack_mult')} "
                                 f"Range:{s.get('attack_range')}")
                        break
                    elif t == "error":
                        log.warning(f"Loadout error: {msg.get('message')}")
                    elif t == "lobby":
                        # Already in game — process below
                        break

                # Main loop
                while True:
                    try:
                        raw = await asyncio.wait_for(ws.recv(), timeout=25)
                    except asyncio.TimeoutError:
                        # No server tick means there is no valid action tick to
                        # echo. The websocket library handles transport pings.
                        continue

                    msg = json.loads(raw)
                    t   = msg.get("type")

                    if t == "tick":
                        state  = msg.get("your_state", {})
                        nearby = msg.get("nearby_entities") or []
                        action = brain.decide(state, nearby)
                        action["tick"] = msg.get("tick_number", msg.get("tick", 0))
                        await ws.send(json.dumps(action))
                        # Periodic status log
                        if brain.tick_count % 50 == 1:
                            enemies = [e for e in nearby if e.get("type")=="bot" and e.get("is_alive")]
                            hp   = state.get("hp",0)
                            pos  = state.get("position",[0,0])
                            near = ""
                            if enemies:
                                e0 = enemies[0]
                                d0 = dist(tuple(pos), tuple(e0.get("position",[0,0])))
                                near = f" nearest={e0.get('name','?')} d={d0:.0f} wready={state.get('weapon_ready')}"
                            log.info(f"T{brain.tick_count} HP:{hp:.0f}/{state.get('max_hp',0):.0f} "
                                     f"n={len(enemies)}{near} act={action.get('action')}")

                    elif t == "kill":
                        brain.on_kill(msg.get("victim_id",""), msg.get("victim_weapon",""))

                    elif t == "death":
                        brain.on_death(msg.get("killed_by",""), msg.get("weapon_used",""))

                    elif t == "round_end":
                        k = msg.get("your_kills",0); d = msg.get("your_deaths",0)
                        dm = msg.get("your_damage",0)
                        log.info(f"🏁 Round K:{k} D:{d} DMG:{dm}")
                        tracker.end_match()

                    elif t == "respawn":
                        brain.last_hp = 9999
                        log.info(f"🔄 Respawn at {msg.get('position')}")

                    elif t == "lobby":
                        bc = msg.get("bots_connected",0)
                        if bc % 50 == 0:
                            log.info(f"🏠 Lobby {bc}/2")

        except websockets.exceptions.ConnectionClosed as e:
            log.warning(f"Disconnected: {e}. Retry in 3s...")
        except Exception as e:
            log.error(f"Error: {e}")
            await asyncio.sleep(3)
            continue
        await asyncio.sleep(3)


if __name__ == "__main__":
    print("🐬 ANISMIN BOT v2 | Staff AoE | HP:5 SPD:4 ATK:10 DEF:1")
    asyncio.run(run())
