"""Tests for the core game engine — Phase 3."""

import asyncio
import os

# Set test environment before imports
os.environ.setdefault("ARENA_DB_HOST", "localhost")
os.environ.setdefault("ARENA_DB_PORT", "5433")
os.environ.setdefault("ARENA_DB_PASSWORD", "arena_pass")

import pytest

from server.game.arena_map import ArenaMap
from server.game.combat import process_combat
from server.game.engine import GameEngine
from server.game.fallback_ai import get_fallback_action
from server.game.movement import process_movement
from server.game.spatial import SpatialGrid
from server.game.spawner import check_deaths, process_respawns, spawn_bot
from server.game.state import Action, ActionType, BotState
from server.game.weapons import (
    calculate_damage,
    get_available_weapons,
    get_weapon_config,
    is_in_range,
)


def _make_bot(bot_id: str = "b1", **kwargs) -> BotState:
    """Helper to create a BotState with defaults."""
    defaults = dict(
        bot_id=bot_id, api_key_id="k1", name=f"Bot_{bot_id}",
        position=(100.0, 100.0), hp=100, max_hp=100, speed=5.5,
        attack_multiplier=1.5, defense_reduction=0.1, weapon="sword",
    )
    defaults.update(kwargs)
    return BotState(**defaults)


# --- Spatial Grid Tests ---

class TestSpatialGrid:
    def test_insert_and_query(self):
        grid = SpatialGrid()
        grid.insert("a", 50.0, 50.0)
        result = grid.query_radius(50.0, 50.0, 10.0)
        assert "a" in result

    def test_query_excludes_far(self):
        grid = SpatialGrid()
        grid.insert("a", 50.0, 50.0)
        grid.insert("b", 1500.0, 1500.0)
        result = grid.query_radius(50.0, 50.0, 100.0)
        assert "a" in result
        assert "b" not in result

    def test_update_position(self):
        grid = SpatialGrid()
        grid.insert("a", 50.0, 50.0)
        grid.update("a", 1500.0, 1500.0)
        result = grid.query_radius(50.0, 50.0, 100.0)
        assert "a" not in result
        result2 = grid.query_radius(1500.0, 1500.0, 100.0)
        assert "a" in result2

    def test_remove(self):
        grid = SpatialGrid()
        grid.insert("a", 50.0, 50.0)
        grid.remove("a")
        result = grid.query_radius(50.0, 50.0, 100.0)
        assert "a" not in result

    def test_cell_boundary(self):
        grid = SpatialGrid()
        grid.insert("a", 99.0, 99.0)
        grid.insert("b", 101.0, 101.0)
        result = grid.query_radius(100.0, 100.0, 10.0)
        assert "a" in result
        assert "b" in result


# --- Weapon Tests ---

class TestWeapons:
    def test_available_weapons(self):
        weapons = get_available_weapons()
        assert set(weapons) == {"sword", "bow", "daggers", "shield", "spear", "staff"}

    def test_weapon_config(self):
        sword = get_weapon_config("sword")
        assert sword["damage"] == 25
        assert sword["range"] == 2.0
        assert sword["special"] == "cleave"

    def test_unknown_weapon_defaults_to_sword(self):
        cfg = get_weapon_config("nonexistent")
        assert cfg["damage"] == 25

    def test_calculate_damage(self):
        attacker = _make_bot("a", attack_multiplier=1.5)
        target = _make_bot("t", defense_reduction=0.1, weapon="bow")
        dmg = calculate_damage("sword", attacker, target)
        expected = 25 * 1.5 * (1.0 - 0.1)
        assert abs(dmg - expected) < 0.01

    def test_shield_passive(self):
        attacker = _make_bot("a", attack_multiplier=1.0)
        target = _make_bot("t", defense_reduction=0.0, weapon="shield")
        dmg = calculate_damage("sword", attacker, target)
        assert dmg < 25  # Should be reduced by shield passive

    def test_is_in_range(self):
        a = _make_bot("a", position=(100.0, 100.0))
        b = _make_bot("b", position=(101.0, 100.0))
        assert is_in_range(a, b, "sword")  # range=2.0, dist=1.0
        c = _make_bot("c", position=(200.0, 200.0))
        assert not is_in_range(a, c, "sword")


# --- Arena Map Tests ---

class TestArenaMap:
    def test_safe_zone_center(self):
        arena = ArenaMap()
        assert arena.is_in_safe_zone(1000.0, 1000.0)

    def test_outside_zone(self):
        arena = ArenaMap()
        arena.safe_zone_radius = 10.0
        assert not arena.is_in_safe_zone(0.0, 0.0)

    def test_spawn_in_zone(self):
        arena = ArenaMap()
        for _ in range(10):
            x, y = arena.get_random_spawn_point()
            assert arena.is_in_safe_zone(x, y)

    def test_zone_shrinks(self):
        arena = ArenaMap()
        original = arena.safe_zone_radius
        arena.update_zone(600, 10)  # 60s * 10 ticks = 600 ticks
        assert arena.safe_zone_radius < original

    def test_clamp_position(self):
        arena = ArenaMap()
        assert arena.clamp_position(-5.0, 3000.0) == (0.0, 2000.0)


# --- Fallback AI Tests ---

class TestFallbackAI:
    def test_aggressive_moves_toward(self):
        bot = _make_bot("b1", position=(100.0, 100.0))
        enemy = _make_bot("e1", position=(200.0, 100.0))
        action = get_fallback_action(bot, [enemy], "aggressive")
        assert action.action_type == ActionType.MOVE
        assert action.direction[0] > 0  # Moving right toward enemy

    def test_idle_when_alone(self):
        bot = _make_bot("b1")
        action = get_fallback_action(bot, [], "aggressive")
        assert action.action_type == ActionType.IDLE

    def test_defensive_moves_away(self):
        bot = _make_bot("b1", position=(100.0, 100.0))
        enemy = _make_bot("e1", position=(200.0, 100.0))
        action = get_fallback_action(bot, [enemy], "defensive")
        assert action.action_type == ActionType.MOVE
        assert action.direction[0] < 0  # Moving left, away

    def test_territorial_returns_to_spawn(self):
        bot = _make_bot("b1", position=(500.0, 500.0), spawn_position=(100.0, 100.0))
        action = get_fallback_action(bot, [], "territorial")
        assert action.action_type == ActionType.IDLE or (
            action.action_type == ActionType.MOVE
        )


# --- Engine Integration Tests ---

class TestEngine:
    @pytest.fixture
    def engine(self):
        return GameEngine()

    def test_add_remove_bot(self, engine):
        bot = _make_bot("b1")
        engine.add_bot(bot)
        assert "b1" in engine.bots
        engine.remove_bot("b1")
        assert "b1" not in engine.bots

    @pytest.mark.asyncio
    async def test_tick_increments(self, engine):
        assert engine.tick_count == 0
        await engine.tick()
        assert engine.tick_count == 1

    @pytest.mark.asyncio
    async def test_combat_tick(self, engine):
        b1 = _make_bot("b1", position=(100.0, 100.0), weapon="sword")
        b2 = _make_bot("b2", position=(101.0, 100.0), weapon="bow", hp=100, max_hp=100)
        engine.add_bot(b1)
        engine.add_bot(b2)
        # Override positions after spawning
        b1.position = (100.0, 100.0)
        b2.position = (101.0, 100.0)
        engine.grid.update("b1", 100.0, 100.0)
        engine.grid.update("b2", 101.0, 100.0)
        b1.pending_action = Action(action_type=ActionType.ATTACK, target_id="b2")
        b1.last_action_tick = 1
        b2.last_action_tick = 1
        await engine.tick()
        assert b2.hp < 100

    @pytest.mark.asyncio
    async def test_death_and_respawn(self, engine):
        b1 = _make_bot("b1", position=(100.0, 100.0))
        b2 = _make_bot("b2", position=(101.0, 100.0))
        engine.add_bot(b1)
        engine.add_bot(b2)
        # Override positions and HP after spawn (spawn resets hp to max_hp)
        b1.position = (100.0, 100.0)
        b2.position = (101.0, 100.0)
        b2.hp = 1  # Set low HP after spawn
        engine.grid.update("b1", 100.0, 100.0)
        engine.grid.update("b2", 101.0, 100.0)
        b1.pending_action = Action(action_type=ActionType.ATTACK, target_id="b2")
        b1.last_action_tick = 1
        b2.last_action_tick = 1
        await engine.tick()
        assert not b2.is_alive
        # Run 51 ticks for respawn (5s * 10 tps)
        for _ in range(51):
            await engine.tick()
        assert b2.is_alive
        assert b2.hp == b2.max_hp

    @pytest.mark.asyncio
    async def test_zone_damage(self, engine):
        bot = _make_bot("b1", position=(0.0, 0.0))
        engine.add_bot(bot)
        bot.position = (0.0, 0.0)
        engine.grid.update("b1", 0.0, 0.0)
        engine.arena.safe_zone_radius = 10.0
        bot.last_action_tick = 1
        original_hp = bot.hp
        await engine.tick()
        assert bot.hp < original_hp

    def test_arena_status(self, engine):
        status = engine.get_arena_status()
        assert status["bots_connected"] == 0
        assert status["safe_zone_radius"] == 1000.0

    def test_spectator_state(self, engine):
        state = engine.get_spectator_state()
        assert state["type"] == "arena_state"
        assert "bots" in state
        assert "safe_zone" in state
