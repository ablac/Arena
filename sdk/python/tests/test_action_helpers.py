from arena_sdk.bot import ArenaBot


def test_staff_helpers_use_documented_target_position_field() -> None:
    bot = ArenaBot("test-key")

    # The server rejects an attack action carrying both target and
    # target_position (exactly one aim mode is allowed), so attack() must
    # drop target_id in favor of target_position when both are given.
    assert bot.attack("enemy", [12, 7]) == {
        "action": "attack",
        "target_position": [12, 7],
    }
    assert bot.attack("enemy") == {
        "action": "attack",
        "target": "enemy",
    }
    assert bot.staff_attack([4, 9]) == {
        "action": "attack",
        "target_position": [4, 9],
    }
