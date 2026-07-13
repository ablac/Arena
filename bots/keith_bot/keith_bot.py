#!/usr/bin/env python3
"""
Anismin's Arena Bot v5 🐬
Pure algorithmic, zero LLM tokens.
Staff AoE kiter. Tracks zone_target_center (where zone shrinks to).
Hunts via hints, intercepts enemy movement, shoves melee threats.
"""
import asyncio
import json
import math
import os
import random
import signal
import sys
import websockets

# ── Config ──────────────────────────────────────────────────────────────
# Never hardcode API keys in source. Set ARENA_API_KEY in your shell/
# environment before running this bot.
API_KEY = os.environ.get("ARENA_API_KEY", "")
SERVER = os.environ.get("ARENA_SERVER", "ws://localhost:8700")
WS_URL = f"{SERVER}/ws/bot?key={API_KEY}"

LOADOUT = {
    "type": "select_loadout",
    "weapon": "staff",
    "stats": {"hp": 6, "speed": 5, "attack": 7, "defense": 2},
    "fallback_behavior": "aggressive",
    "bot_name": "Anismin",
    "avatar_color": "#3498db"
}

# Game constants
STAFF_RANGE = 12.0
SHOVE_RANGE = 3.0   # 2.0 + bot radius
KITE_MIN = 7.0
KITE_MAX = 11.5
RECONNECT_DELAY = 5

# ── Helpers ─────────────────────────────────────────────────────────────
def act(action, tick=0, **kw):
    return {"type": "action", "tick": tick, "action": action, **kw}

def dist(a, b):
    return math.sqrt((a[0]-b[0])**2 + (a[1]-b[1])**2)

def norm(dx, dy):
    mag = math.sqrt(dx*dx + dy*dy)
    if mag < 0.001:
        a = random.uniform(0, 2*math.pi)
        return [math.cos(a), math.sin(a)]
    return [dx/mag, dy/mag]

def clamp(v, lo, hi):
    return max(lo, min(hi, v))

# ── Bot Brain ───────────────────────────────────────────────────────────
class AnisminBot:
    def __init__(self):
        self.kills = 0
        self.deaths = 0
        self.rounds = 0
        self.last_hp = 999
        self.last_pos = [0, 0]
        self.stuck_ticks = 0
        self.shove_cd = 0
        self.grid_width = 100
        self.grid_height = 100
        # Enemy position tracking for interception
        self.enemy_history = {}  # bot_id -> list of last N positions

    def configure_arena(self, connected):
        """Remember the negotiated grid bounds used by every tick position."""
        size = connected.get("grid_size") or []
        if len(size) == 2 and all(isinstance(value, (int, float)) and value > 2 for value in size):
            self.grid_width = int(size[0])
            self.grid_height = int(size[1])

    def _clamp_target(self, target):
        return [
            clamp(target[0], 1, self.grid_width - 2),
            clamp(target[1], 1, self.grid_height - 2),
        ]

    def _track_enemy(self, bot_id, pos):
        """Track enemy positions for movement prediction."""
        if bot_id not in self.enemy_history:
            self.enemy_history[bot_id] = []
        history = self.enemy_history[bot_id]
        history.append(pos[:])
        if len(history) > 10:
            history.pop(0)

    def _predict_position(self, bot_id, ticks_ahead=5):
        """Predict where an enemy will be based on movement history."""
        history = self.enemy_history.get(bot_id, [])
        if len(history) < 3:
            return None
        # Average velocity over last few positions
        vx = (history[-1][0] - history[-3][0]) / 3
        vy = (history[-1][1] - history[-3][1]) / 3
        return [
            history[-1][0] + vx * ticks_ahead,
            history[-1][1] + vy * ticks_ahead
        ]

    def _intercept_point(self, my_pos, enemy_pos, predicted_pos, my_speed=5.5):
        """Calculate intercept point — where to move to cut off a fleeing enemy."""
        if predicted_pos is None:
            return enemy_pos
        # If enemy is moving away, aim ahead of them
        enemy_vel_x = predicted_pos[0] - enemy_pos[0]
        enemy_vel_y = predicted_pos[1] - enemy_pos[1]
        enemy_speed = math.sqrt(enemy_vel_x**2 + enemy_vel_y**2)
        if enemy_speed < 0.5:
            return enemy_pos  # Standing still
        # Lead the target — aim where they'll be
        d_to_enemy = dist(my_pos, enemy_pos)
        time_to_reach = d_to_enemy / my_speed if my_speed > 0 else 10
        intercept = [
            enemy_pos[0] + enemy_vel_x * time_to_reach * 0.5,
            enemy_pos[1] + enemy_vel_y * time_to_reach * 0.5
        ]
        return self._clamp_target(intercept)

    def decide(self, state):
        me = state.get("your_state") or {}
        if not me.get("is_alive", False):
            return None

        tick = state.get("tick", 0)
        hp = me.get("hp", 0)
        max_hp = me.get("max_hp", 160)
        pos = me.get("position", [0, 0])
        weapon_ready = me.get("weapon_ready", False)
        dodge_cd = me.get("dodge_cooldown", 0)
        stun = me.get("stun_ticks", 0)
        in_safe = me.get("in_safe_zone", True)
        zone_center = me.get("zone_center", [self.grid_width / 2, self.grid_height / 2])
        zone_radius = me.get("zone_radius", max(self.grid_width, self.grid_height) / 2)
        zone_edge_dist = me.get("distance_to_zone_edge", 999)
        # WHERE the zone is shrinking TO — this is where we should position
        zone_target = me.get("zone_target_center") or zone_center
        zone_target_radius = me.get("zone_target_radius", zone_radius)
        hits = me.get("hits_received") or []

        # Parse entities
        nearby_raw = state.get("nearby_entities") or []
        alive = [e for e in nearby_raw if e.get("type") == "bot" and e.get("is_alive", False)]
        pickups = [e for e in nearby_raw if e.get("type") == "pickup"]
        hints = state.get("hints") or []
        bot_hints = [h for h in hints if h.get("hint_type") == "bot"]

        # Track all visible enemies for prediction
        for e in alive:
            eid = e.get("id") or e.get("bot_id")
            if eid:
                self._track_enemy(eid, e["position"])

        # Stuck detection
        if dist(pos, self.last_pos) < 0.3:
            self.stuck_ticks += 1
        else:
            self.stuck_ticks = 0
        self.last_pos = pos[:]

        if self.shove_cd > 0:
            self.shove_cd -= 1

        # ── Stunned ──
        if stun > 0:
            return None

        # ── P1: SHOVE melee threats (knockback 15 + 2 tick stun) ──
        if alive and self.shove_cd <= 0:
            melee = [e for e in alive if dist(pos, e["position"]) <= SHOVE_RANGE]
            if melee:
                target = min(melee, key=lambda e: dist(pos, e["position"]))
                self.shove_cd = 15
                return act("shove", tick=tick, target=target.get("id") or target.get("bot_id"))

        # ── P2: Dodge on hit ──
        if hits and dodge_cd <= 0:
            attacker_id = hits[0].get("attacker_id")
            attacker = next((e for e in alive if e.get("id") == attacker_id or e.get("bot_id") == attacker_id), None)
            if attacker:
                return act("dodge", tick=tick, direction=norm(pos[0]-attacker["position"][0], pos[1]-attacker["position"][1]))
            return act("dodge", tick=tick, direction=norm(zone_target[0]-pos[0], zone_target[1]-pos[1]))

        # ── P3: Emergency dodge ──
        if hp < max_hp * 0.12 and dodge_cd <= 0 and alive:
            closest = min(alive, key=lambda e: dist(pos, e["position"]))
            if dist(pos, closest["position"]) < 6:
                return act("dodge", tick=tick, direction=norm(pos[0]-closest["position"][0], pos[1]-closest["position"][1]))

        # ── P4: Zone survival — move toward ZONE TARGET (where it's shrinking to) ──
        if not in_safe or zone_edge_dist < 10:
            return act("move_to", tick=tick, target_position=zone_target)

        # ── P5: Flee when critical ──
        if hp < max_hp * 0.20 and alive:
            avg_x = sum(e["position"][0] for e in alive) / len(alive)
            avg_y = sum(e["position"][1] for e in alive) / len(alive)
            flee = self._clamp_target([
                pos[0] + (pos[0] - avg_x) * 3,
                pos[1] + (pos[1] - avg_y) * 3,
            ])
            return act("move_to", tick=tick, target_position=flee)

        # ── P6: Health pickups when hurt ──
        if hp < max_hp * 0.6 and pickups:
            health = [p for p in pickups if p.get("pickup_type") == "health_pack"]
            if health:
                closest = min(health, key=lambda p: dist(pos, p["position"]))
                d_pk = dist(pos, closest["position"])
                if d_pk <= 2.5:
                    return act("use_item", tick=tick, item_id=closest.get("id") or closest.get("pickup_id"))
                return act("move_to", tick=tick, target_position=closest["position"])

        # ── P7: ATTACK with interception ──
        if weapon_ready and alive:
            in_range = [e for e in alive if dist(pos, e["position"]) <= STAFF_RANGE]
            if in_range:
                # Target lowest HP → stunned → closest
                target = min(in_range, key=lambda e: (e.get("hp", 999), 0 if e.get("is_stunned") else 1, dist(pos, e["position"])))
                eid = target.get("id") or target.get("bot_id")
                # Predict where they're going and aim there
                predicted = self._predict_position(eid, ticks_ahead=3)
                if predicted and dist(pos, predicted) <= STAFF_RANGE:
                    dx, dy = predicted[0] - pos[0], predicted[1] - pos[1]
                else:
                    dx, dy = target["position"][0] - pos[0], target["position"][1] - pos[1]
                return act("attack", tick=tick, target=eid, direction=[dx, dy])

        # ── P8: Kite visible enemies ──
        if alive:
            closest = min(alive, key=lambda e: dist(pos, e["position"]))
            d_to = dist(pos, closest["position"])
            eid = closest.get("id") or closest.get("bot_id")

            if d_to < KITE_MIN:
                return act("move", tick=tick, direction=norm(pos[0]-closest["position"][0], pos[1]-closest["position"][1]))
            elif d_to > KITE_MAX and d_to <= 30:
                # Intercept — move to where they'll be, not where they are
                predicted = self._predict_position(eid, ticks_ahead=8)
                intercept = self._intercept_point(pos, closest["position"], predicted)
                return act("move_to", tick=tick, target_position=intercept)
            elif KITE_MIN <= d_to <= KITE_MAX:
                dx, dy = closest["position"][0]-pos[0], closest["position"][1]-pos[1]
                if (tick // 12) % 2 == 0:
                    return act("move", tick=tick, direction=norm(-dy, dx))
                else:
                    return act("move", tick=tick, direction=norm(dy, -dx))

        # ── P9: Power-up pickups ──
        if pickups:
            priority = {"damage_boost": 0, "speed_boost": 1, "shield_bubble": 2, "health_pack": 3}
            pickups.sort(key=lambda p: (priority.get(p.get("pickup_type"), 9), dist(pos, p["position"])))
            pk = pickups[0]
            d_pk = dist(pos, pk["position"])
            if d_pk <= 2.5:
                return act("use_item", tick=tick, item_id=pk.get("id") or pk.get("pickup_id"))
            return act("move_to", tick=tick, target_position=pk["position"])

        # ── P10: HUNT — use hints + move toward ZONE TARGET ──
        if bot_hints:
            closest_hint = min(bot_hints, key=lambda h: h.get("distance", 9999))
            hdir = closest_hint.get("direction", [0, 0])
            hdist = closest_hint.get("distance", 500)

            # If enemies are far away and zone is shrinking, position at zone target
            # The zone will push everyone together — be there first
            far_threshold = max(24, zone_target_radius * 2)
            closing_zone = zone_target_radius < min(self.grid_width, self.grid_height) * 0.35
            if hdist > far_threshold and closing_zone:
                # Go to zone target center and wait — enemies will come to us
                return act("move_to", tick=tick, target_position=zone_target)

            # Otherwise chase the hint
            target_x = pos[0] + hdir[0] * min(hdist, 20)
            target_y = pos[1] + hdir[1] * min(hdist, 20)
            return act("move_to", tick=tick, target_position=self._clamp_target([target_x, target_y]))

        # ── P11: Unstuck ──
        if self.stuck_ticks > 8:
            self.stuck_ticks = 0
            angle = random.uniform(0, 2*math.pi)
            target = self._clamp_target([pos[0]+math.cos(angle)*4, pos[1]+math.sin(angle)*4])
            return act("move_to", tick=tick, target_position=target)

        # ── P12: Default — go to ZONE TARGET (where zone shrinks to) ──
        # This is the key difference — go where the action WILL be, not map center
        return act("move_to", tick=tick, target_position=zone_target)

    def on_kill(self):
        self.kills += 1
        print(f"[Anismin] 🐬 Kill! K/D: {self.kills}/{self.deaths}")

    def on_death(self):
        self.deaths += 1
        print(f"[Anismin] 💀 Died. K/D: {self.kills}/{self.deaths}")

    def on_round_end(self, data):
        self.rounds += 1
        self.enemy_history.clear()
        self.shove_cd = 0
        self.last_hp = 999
        print(f"[Anismin] 🏁 Round {self.rounds} | K/D: {self.kills}/{self.deaths}")

# ── Main loop ───────────────────────────────────────────────────────────
async def run():
    if not API_KEY:
        print("[Anismin] ERROR: Set ARENA_API_KEY in your environment before running.")
        return

    bot = AnisminBot()
    print("[Anismin] 🐬 v5 — Staff kiter + zone tracking + enemy prediction. Zero tokens.")

    while True:
        try:
            async with websockets.connect(WS_URL, ping_interval=20, ping_timeout=30) as ws:
                print("[Anismin] Connected!")
                lobby_logged = False

                async for raw in ws:
                    msg = json.loads(raw)
                    mt = msg.get("type")

                    if mt == "connected":
                        print(f"[Anismin] Auth'd: {msg.get('bot_id', '?')[:8]}")
                        bot.configure_arena(msg)
                        await ws.send(json.dumps(LOADOUT))

                    elif mt == "select_loadout":
                        await ws.send(json.dumps(LOADOUT))

                    elif mt == "loadout_confirmed":
                        c = msg.get("computed", {})
                        print(f"[Anismin] ✅ {msg.get('weapon')} | HP:{c.get('max_hp')} SPD:{c.get('move_speed')} ATK:x{c.get('attack_mult')} Range:{c.get('attack_range')}")

                    elif mt == "lobby":
                        if not lobby_logged:
                            print(f"[Anismin] Lobby ({msg.get('bots_connected',0)}/{msg.get('bots_needed',2)})")
                            lobby_logged = True

                    elif mt == "round_start":
                        lobby_logged = False
                        print("[Anismin] ⚔️ Round start!")

                    elif mt == "tick":
                        try:
                            a = bot.decide(msg)
                        except Exception as e:
                            print(f"[Anismin] ⚠️ decide error: {e}")
                            a = None
                        if a:
                            await ws.send(json.dumps(a))
                        else:
                            # Send idle to prevent AFK kick (30 tick timeout!)
                            await ws.send(json.dumps({"type": "action", "tick": msg.get("tick", 0), "action": "idle"}))

                    elif mt == "kill":
                        bot.on_kill()
                    elif mt == "death":
                        bot.on_death()
                    elif mt == "round_end":
                        bot.on_round_end(msg)
                    elif mt == "kick":
                        print(f"[Anismin] ❌ Kicked: {msg.get('reason')}")
                        break

        except websockets.ConnectionClosed as e:
            print(f"[Anismin] Disconnected ({e.code}). Retry in {RECONNECT_DELAY}s...")
        except ConnectionRefusedError:
            print(f"[Anismin] Server down. Retry in {RECONNECT_DELAY}s...")
        except Exception as e:
            print(f"[Anismin] Error: {e}. Retry in {RECONNECT_DELAY}s...")

        await asyncio.sleep(RECONNECT_DELAY)

if __name__ == "__main__":
    signal.signal(signal.SIGINT, lambda s, f: sys.exit(0))
    signal.signal(signal.SIGTERM, lambda s, f: sys.exit(0))
    asyncio.run(run())
