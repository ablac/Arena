import importlib.util
from pathlib import Path


MODULE_PATH = Path(__file__).resolve().parents[1] / "bots" / "keith_bot" / "keith_bot.py"
SPEC = importlib.util.spec_from_file_location("arena_keith_bot", MODULE_PATH)
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


def tick_state(distance, direction=(1, 0), zone_radius=30):
    return {
        "type": "tick",
        "tick": 1,
        "your_state": {
            "is_alive": True,
            "position": [40, 40],
            "hp": 100,
            "max_hp": 100,
            "weapon_ready": False,
            "dodge_cooldown": 10,
            "in_safe_zone": True,
            "distance_to_zone_edge": 20,
            "zone_center": [50, 40],
            "zone_radius": zone_radius,
            "zone_target_center": [50, 40],
            "zone_target_radius": zone_radius,
        },
        "nearby_entities": [],
        "hints": [{"hint_type": "bot", "distance": distance, "direction": list(direction)}],
    }


bot = MODULE.AnisminBot()
bot.configure_arena({"grid_size": [100, 80]})

action = bot.decide(tick_state(10))
assert action["action"] == "move_to", action
assert action["target_position"] == [50, 40], (
    "a 10-tile hint must reconstruct a 10-tile target, not a 200-world-unit target",
    action,
)

clamped = bot._intercept_point([95, 75], [99, 79], [140, 120])
assert 1 <= clamped[0] <= 98 and 1 <= clamped[1] <= 78, clamped

far_action = bot.decide(tick_state(80, zone_radius=10))
assert far_action["target_position"] == [50, 40], far_action

print("Keith bot consumes grid-unit hints and clamps targets to negotiated grid bounds")
