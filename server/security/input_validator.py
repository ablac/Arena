"""Input validation and sanitisation helpers."""

import re

from server.config import settings

# Allowed characters in bot names: alphanumeric, spaces, and basic punctuation.
_NAME_ALLOWED_RE: re.Pattern[str] = re.compile(r"[^a-zA-Z0-9 .\-_!?]")

# Valid hex colour code.
_COLOR_RE: re.Pattern[str] = re.compile(r"^#[0-9a-fA-F]{6}$")

# Required stat keys.
_STAT_KEYS: set[str] = {"hp", "speed", "attack", "defense"}


def sanitize_bot_name(name: str) -> str:
    """Sanitise a user-provided bot name.

    Processing steps:
    1. Strip HTML tags.
    2. Remove characters outside the allowed set.
    3. Strip surrounding whitespace and truncate to 20 characters.
    4. Fall back to ``"Unnamed Bot"`` if the result is empty.

    Args:
        name: The raw bot name string.

    Returns:
        A cleaned name safe for storage and display.
    """
    # Remove HTML tags.
    name = re.sub(r"<[^>]+>", "", name)
    # Remove disallowed characters.
    name = _NAME_ALLOWED_RE.sub("", name)
    # Strip whitespace and enforce length limit.
    name = name.strip()[:20]

    return name if name else "Unnamed Bot"


def validate_stats(stats: dict) -> bool:
    """Validate a bot stat allocation dictionary.

    Rules:
    - Must contain exactly the keys: hp, speed, attack, defense.
    - Each value must be an integer in ``[stat_min, stat_max]``.
    - The sum of all values must equal ``stat_budget``.

    Args:
        stats: Mapping of stat names to integer values.

    Returns:
        True if the stats are valid, False otherwise.
    """
    if set(stats.keys()) != _STAT_KEYS:
        return False

    for value in stats.values():
        if not isinstance(value, int):
            return False
        if value < settings.combat.stat_min or value > settings.combat.stat_max:
            return False

    if sum(stats.values()) != settings.combat.stat_budget:
        return False

    return True


def validate_color(color: str) -> bool:
    """Validate a hex colour string.

    Args:
        color: A string that should match the pattern ``#RRGGBB``.

    Returns:
        True if the colour is a valid 6-digit hex code, False otherwise.
    """
    return bool(_COLOR_RE.match(color))


def validate_action(action: dict) -> dict | None:
    """Validate a game action payload.

    This is a placeholder for the game phase. Currently only checks that
    the action contains a ``"type"`` key.

    Args:
        action: The action dictionary submitted by a bot.

    Returns:
        The action dictionary unchanged if valid, or None if invalid.
    """
    if "type" in action:
        return action
    return None
