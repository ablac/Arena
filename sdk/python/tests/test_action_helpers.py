from arena_sdk.bot import ArenaBot


def test_staff_helpers_use_documented_target_position_field() -> None:
    bot = ArenaBot("test-key")

    assert bot.attack("enemy", [12, 7]) == {
        "action": "attack",
        "target": "enemy",
        "target_position": [12, 7],
    }
    assert bot.staff_attack([4, 9]) == {
        "action": "attack",
        "target_position": [4, 9],
    }
