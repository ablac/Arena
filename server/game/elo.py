"""ELO rating calculations for bot matchups."""

from __future__ import annotations

from server.config import settings


def calculate_elo_change(
    killer_elo: int, victim_elo: int
) -> tuple[int, int]:
    """Calculate ELO changes for a kill event.

    Returns (killer_gain, victim_loss) as positive integers.
    """
    k = settings.elo.k_factor
    expected = 1.0 / (1.0 + 10.0 ** ((victim_elo - killer_elo) / 400.0))
    gain = round(k * (1.0 - expected))
    loss = round(k * expected)
    return (max(1, gain), max(1, loss))


def apply_elo_change(
    killer_elo: int, victim_elo: int
) -> tuple[int, int]:
    """Apply ELO changes and return new ratings.

    Respects minimum ELO floor.
    """
    gain, loss = calculate_elo_change(killer_elo, victim_elo)
    new_killer = killer_elo + gain
    new_victim = max(settings.elo.min_elo, victim_elo - loss)
    return (new_killer, new_victim)
