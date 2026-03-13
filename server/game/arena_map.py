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
        """Shrink the safe zone and drift center toward target.

        Called every tick. Shrinks by shrink_percent every shrink_interval_secs.
        Center moves toward the random target proportionally.
        """
        ticks_per_interval = tick_rate * self.shrink_interval_secs
        if ticks_per_interval <= 0:
            return
        intervals_now = tick_count // ticks_per_interval
        intervals_last = self._last_shrink_tick // ticks_per_interval

        if intervals_now > intervals_last:
            self.safe_zone_radius *= 1.0 - self.shrink_percent
            self.safe_zone_radius = max(self.min_radius, self.safe_zone_radius)
            # Drift center toward random target (faster than radius shrinks)
            drift = self.shrink_percent * 1.5
            self.center_x += (self.target_center_x - self.center_x) * drift
            self.center_y += (self.target_center_y - self.center_y) * drift
            self._last_shrink_tick = tick_count

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
