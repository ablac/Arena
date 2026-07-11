import json

import pytest

from arena_sdk.bot import ArenaBot


class FakeWebSocket:
    def __init__(self, messages):
        self.messages = list(messages)
        self.sent = []

    async def recv(self):
        return json.dumps(self.messages.pop(0))

    async def send(self, payload):
        self.sent.append(json.loads(payload))


class TrackingBot(ArenaBot):
    def __init__(self):
        super().__init__("test-key")
        self.on_tick_calls = 0

    async def on_tick(self, state, nearby, safe_zone):
        self.on_tick_calls += 1
        return self.idle()


@pytest.mark.asyncio
async def test_dead_ticks_do_not_invoke_agent_logic_or_submit_actions():
    bot = TrackingBot()
    socket = FakeWebSocket([
        {
            "type": "tick",
            "tick": 10,
            "your_state": {"is_alive": False, "position": [1, 1]},
            "nearby_entities": [],
        },
        {"type": "kick", "reason": "test complete"},
    ])
    bot._ws = socket

    await bot._game_loop()

    assert bot.on_tick_calls == 0
    assert socket.sent == []
