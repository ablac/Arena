"""Tests for API key generation, verification, bot config, and input validation."""

import pytest
from httpx import AsyncClient

from server.security.auth import generate_api_key, verify_api_key
from server.security.input_validator import (
    sanitize_bot_name,
    validate_color,
    validate_stats,
)


class TestKeyGeneration:
    """API key format and crypto tests."""

    def test_key_format(self) -> None:
        """Generated key starts with prefix and has sufficient length."""
        raw, key_hash, prefix = generate_api_key()
        assert raw.startswith("arena_")
        assert len(raw) > 20
        assert prefix == raw[:12]

    def test_hash_verification(self) -> None:
        """Correct key verifies; wrong key does not."""
        raw, key_hash, _ = generate_api_key()
        assert verify_api_key(raw, key_hash) is True
        assert verify_api_key(raw + "x", key_hash) is False
        assert verify_api_key("wrong_key", key_hash) is False

    def test_unique_keys(self) -> None:
        """Each call produces a different key."""
        keys = {generate_api_key()[0] for _ in range(5)}
        assert len(keys) == 5


class TestInputValidation:
    """Input sanitization and validation tests."""

    def test_sanitize_strips_html(self) -> None:
        """HTML tags are removed from bot names."""
        assert sanitize_bot_name("<script>alert(1)</script>Bot") == "alert1Bot"

    def test_sanitize_strips_special_chars(self) -> None:
        """Non-allowed special characters are removed."""
        assert sanitize_bot_name("Bot@#$%Name") == "BotName"

    def test_sanitize_allows_valid_chars(self) -> None:
        """Alphanumeric + spaces + basic punctuation pass through."""
        assert sanitize_bot_name("Cool Bot-v2!") == "Cool Bot-v2!"

    def test_sanitize_truncates(self) -> None:
        """Names exceeding 20 chars are truncated."""
        result = sanitize_bot_name("A" * 50)
        assert len(result) == 20

    def test_sanitize_empty_fallback(self) -> None:
        """Empty/stripped names default to 'Unnamed Bot'."""
        assert sanitize_bot_name("") == "Unnamed Bot"
        assert sanitize_bot_name("<<<>>>") == "Unnamed Bot"

    def test_valid_stats(self) -> None:
        """Stats totaling 20 with values in range pass."""
        assert validate_stats({"hp": 5, "speed": 5, "attack": 5, "defense": 5})

    def test_stats_wrong_total(self) -> None:
        """Stats not totaling 20 are rejected."""
        assert not validate_stats({"hp": 5, "speed": 5, "attack": 5, "defense": 6})

    def test_stats_out_of_range(self) -> None:
        """Individual stats outside 1-10 are rejected."""
        assert not validate_stats({"hp": 0, "speed": 10, "attack": 5, "defense": 5})
        assert not validate_stats({"hp": 11, "speed": 3, "attack": 3, "defense": 3})

    def test_stats_missing_key(self) -> None:
        """Missing stat keys are rejected."""
        assert not validate_stats({"hp": 10, "speed": 5, "attack": 5})

    def test_valid_color(self) -> None:
        """Valid hex colors pass."""
        assert validate_color("#FF00FF")
        assert validate_color("#000000")
        assert validate_color("#aaBBcc")

    def test_invalid_color(self) -> None:
        """Invalid color formats are rejected."""
        assert not validate_color("FF00FF")
        assert not validate_color("#GGG000")
        assert not validate_color("#FFF")
        assert not validate_color("")


@pytest.mark.asyncio
class TestKeyEndpoints:
    """Integration tests for key management endpoints."""

    async def test_generate_returns_key(self, client: AsyncClient) -> None:
        """POST /generate returns a valid key response."""
        resp = await client.post("/api/v1/keys/generate")
        assert resp.status_code == 200
        data = resp.json()
        assert data["api_key"].startswith("arena_")
        assert "bot_id" in data
        assert "cannot be recovered" in data["message"].lower()

    async def test_auth_with_generated_key(self, client: AsyncClient) -> None:
        """A generated key authenticates for bot stats."""
        gen = await client.post("/api/v1/keys/generate")
        key = gen.json()["api_key"]
        resp = await client.get(
            "/api/v1/bot/stats", headers={"X-Arena-Key": key}
        )
        assert resp.status_code == 200
        assert resp.json()["elo"] == 1000

    async def test_invalid_key_rejected(self, client: AsyncClient) -> None:
        """An invalid key returns 401."""
        resp = await client.get(
            "/api/v1/bot/stats", headers={"X-Arena-Key": "arena_fake_key_here"}
        )
        assert resp.status_code == 401

    async def test_revoke_deactivates(self, client: AsyncClient) -> None:
        """Revoking a key prevents future authentication."""
        gen = await client.post("/api/v1/keys/generate")
        key = gen.json()["api_key"]

        revoke = await client.delete(
            "/api/v1/keys/revoke", headers={"X-Arena-Key": key}
        )
        assert revoke.status_code == 200

        after = await client.get(
            "/api/v1/bot/stats", headers={"X-Arena-Key": key}
        )
        assert after.status_code == 401


@pytest.mark.asyncio
class TestBotConfig:
    """Integration tests for bot configuration."""

    async def test_valid_config_update(
        self, client: AsyncClient, api_key: str
    ) -> None:
        """Valid config update succeeds."""
        resp = await client.put(
            "/api/v1/bot/config",
            headers={"X-Arena-Key": api_key},
            json={
                "name": "MyBot",
                "avatar_color": "#FF0000",
                "default_loadout": {
                    "weapon": "bow",
                    "stats": {"hp": 3, "speed": 8, "attack": 6, "defense": 3},
                    "fallback_behavior": "opportunistic",
                },
            },
        )
        assert resp.status_code == 200
        data = resp.json()
        assert data["name"] == "MyBot"
        assert data["default_weapon"] == "bow"
        assert data["default_stats"]["speed"] == 8

    async def test_invalid_stats_total_rejected(
        self, client: AsyncClient, api_key: str
    ) -> None:
        """Stats not totaling 20 are rejected with 422."""
        resp = await client.put(
            "/api/v1/bot/config",
            headers={"X-Arena-Key": api_key},
            json={
                "default_loadout": {
                    "weapon": "sword",
                    "stats": {"hp": 10, "speed": 10, "attack": 10, "defense": 10},
                    "fallback_behavior": "aggressive",
                }
            },
        )
        assert resp.status_code == 422

    async def test_invalid_color_rejected(
        self, client: AsyncClient, api_key: str
    ) -> None:
        """Invalid color format is rejected."""
        resp = await client.put(
            "/api/v1/bot/config",
            headers={"X-Arena-Key": api_key},
            json={"avatar_color": "not-a-color"},
        )
        assert resp.status_code == 422

    async def test_name_sanitized(
        self, client: AsyncClient, api_key: str
    ) -> None:
        """Bot name is sanitized on update."""
        resp = await client.put(
            "/api/v1/bot/config",
            headers={"X-Arena-Key": api_key},
            json={"name": "<b>Evil</b>Bot"},
        )
        assert resp.status_code == 200
        assert "<" not in resp.json()["name"]
