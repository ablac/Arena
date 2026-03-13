"""Arena map with safe zone mechanics and obstacle support."""

import math
import random

from server.config import settings
from server.game.obstacles import generate_obstacles
from server.game.state import Obstacle


class ArenaMap:
    """Manages the arena boundaries, shrinking safe zone, and obstacles."""

    def __init__(self) -> None:
        """Initialize arena map from config."""
        self.width: int = settings.game.arena_width
        self.height: int = settings.game.arena_height
        self.center_x: float = float(self.width) / 2
        self.center_y: float = float(self.height) / 2
        self.initial_radius: float = settings.arena_zone.initial_radius
        self.safe_zone_radius: float = self.initial_radius
        self.shrink_percent: float = settings.arena_zone.shrink_percent
        self.shrink_interval_secs: int = settings.arena_zone.shrink_interval_secs
        self.damage_per_tick: int = settings.arena_zone.damage_per_tick
        self.min_radius: float = settings.arena_zone.min_radius
        self._last_shrink_tick: int = 0
        self.target_center_x: float = self.center_x
        self.target_center_y: float = self.center_y
        self.obstacles: list[Obstacle] = []

    def is_in_safe_zone(self, x: float, y: float) -> bool:
        """Check if a position is inside the current safe zone."""
        dx = x - self.center_x
        dy = y - self.center_y
        return (dx * dx + dy * dy) <= self.safe_zone_radius ** 2

    def update_zone(self, tick_count: int, tick_rate: int) -> None:
        """Shrink the safe zone and drift center toward target every tick.

        Waits shrink_delay_secs before starting, then smoothly interpolates
        from initial_radius to min_radius over the remaining round time.
        """
        from server.config import settings as _s
        delay_ticks = tick_rate * _s.arena_zone.shrink_delay_secs
        shrink_ticks = tick_rate * _s.combat.round_duration - delay_ticks
        if shrink_ticks <= 0:
            return
        elapsed = tick_count - self._last_shrink_tick  # ticks since round start
        shrink_elapsed = elapsed - delay_ticks
        if shrink_elapsed <= 0:
            return  # still in the delay period — zone stays full size
        t = min(shrink_elapsed / shrink_ticks, 1.0)  # 0..1 progress through shrink phase
        self.safe_zone_radius = self.initial_radius + (self.min_radius - self.initial_radius) * t
        # Drift center toward random target at the same pace
        self.center_x = float(self.width) / 2 + (self.target_center_x - float(self.width) / 2) * t
        self.center_y = float(self.height) / 2 + (self.target_center_y - float(self.height) / 2) * t

    def get_random_spawn_point(self) -> tuple[float, float]:
        """Generate a random spawn position inside the current safe zone, avoiding obstacles."""
        from server.game.obstacles import collides_with_obstacle
        for _ in range(100):
            angle = random.uniform(0, 2 * math.pi)
            r = self.safe_zone_radius * math.sqrt(random.random()) * 0.8
            x = self.center_x + r * math.cos(angle)
            y = self.center_y + r * math.sin(angle)
            x = max(0.0, min(float(self.width), x))
            y = max(0.0, min(float(self.height), y))
            if self.is_in_safe_zone(x, y) and collides_with_obstacle(x, y, self.obstacles) is None:
                return (x, y)
        return (self.center_x, self.center_y)

    def clamp_position(self, x: float, y: float) -> tuple[float, float]:
        """Clamp a position to arena boundaries."""
        return (
            max(0.0, min(float(self.width), x)),
            max(0.0, min(float(self.height), y)),
        )

    def get_zone_state(self) -> dict:
        """Return zone state for client updates."""
        return {
            "center": (round(self.center_x, 1), round(self.center_y, 1)),
            "radius": round(self.safe_zone_radius, 1),
            "target_center": (round(self.target_center_x, 1), round(self.target_center_y, 1)),
            "target_radius": self.min_radius,
        }

    def reset(self) -> None:
        """Reset zone and obstacles for a new round."""
        self.safe_zone_radius = self.initial_radius
        self._last_shrink_tick = 0
        # Reset center to arena middle
        self.center_x = float(self.width) / 2
        self.center_y = float(self.height) / 2
        # Pick a random final target — must fit min_radius within arena bounds
        margin = self.min_radius
        self.target_center_x = random.uniform(margin, self.width - margin)
        self.target_center_y = random.uniform(margin, self.height - margin)
        self.obstacles = generate_obstacles()

    def get_obstacles_dicts(self) -> list[dict]:
        """Return obstacles as dicts for client updates."""
        return [
            {"x": o.x, "y": o.y, "width": o.width, "height": o.height}
            for o in self.obstacles
        ]
