import asyncio
import json

import pytest

import arena_sdk.bot as bot_module
from arena_sdk.bot import ArenaBot


class FakeWebSocket:
    def __init__(self, messages=()):
        self.messages = list(messages)
        self.sent = []
        self.closed = False

    async def recv(self):
        if self.messages:
            return json.dumps(self.messages.pop(0))
        await asyncio.Future()

    async def send(self, payload):
        self.sent.append(json.loads(payload))

    async def close(self):
        self.closed = True


def live_tick(tick_number=1):
    return {
        "type": "tick",
        "tick": tick_number,
        "your_state": {"is_alive": True, "position": [1, 1]},
        "nearby_entities": [],
    }


@pytest.mark.asyncio
@pytest.mark.parametrize(
    "result",
    [
        None,
        "move",
        {"direction": [1, 0]},
        {"action": "move", "direction": object()},
        {"action": "move", "direction": [float("nan"), 0]},
        {"action": "move", "direction": [float("inf"), 0]},
        {"action": "move", "direction": [float("-inf"), 0]},
    ],
    ids=[
        "none",
        "non-mapping",
        "missing-action",
        "non-json",
        "nan",
        "positive-infinity",
        "negative-infinity",
    ],
)
async def test_invalid_tick_result_falls_back_to_idle_without_ending_socket(result):
    class InvalidResultBot(ArenaBot):
        async def on_tick(self, state, nearby, safe_zone):
            return result

    bot = InvalidResultBot("test-key")
    socket = FakeWebSocket([
        live_tick(7),
        {"type": "kick", "reason": "test complete"},
    ])
    bot._ws = socket

    await bot._game_loop()

    assert socket.sent == [{"type": "action", "tick": 7, "action": "idle"}]
    assert bot._kicked is True


@pytest.mark.asyncio
async def test_lifecycle_callback_exceptions_do_not_end_socket():
    class ExplodingLifecycleBot(ArenaBot):
        def __init__(self):
            super().__init__("test-key")
            self.callbacks = []

        async def on_tick(self, state, nearby, safe_zone):
            return self.idle()

        async def on_death(self, death_info):
            self.callbacks.append("death")
            raise RuntimeError("death callback failed")

        async def on_respawn(self, respawn_info):
            self.callbacks.append("respawn")
            raise RuntimeError("respawn callback failed")

        async def on_round_end(self, round_info):
            self.callbacks.append("round_end")
            raise RuntimeError("round-end callback failed")

    bot = ExplodingLifecycleBot()
    socket = FakeWebSocket([
        {"type": "death"},
        {"type": "respawn", "position": [2, 3]},
        {"type": "round_end", "round_number": 1},
        live_tick(8),
        {"type": "kick", "reason": "test complete"},
    ])
    bot._ws = socket

    await bot._game_loop()

    assert bot.callbacks == ["death", "respawn", "round_end"]
    assert socket.sent == [{"type": "action", "tick": 8, "action": "idle"}]
    assert bot._kicked is True


@pytest.mark.asyncio
async def test_connect_fetches_map_off_loop_and_invokes_map_init(monkeypatch):
    class MapTrackingBot(ArenaBot):
        def __init__(self):
            super().__init__("test-key")
            self.map_init_args = None

        async def on_map_init(self, terrain, width, height):
            self.map_init_args = (terrain, width, height)

    bot = MapTrackingBot()
    socket = FakeWebSocket([
        {"type": "connected", "bot_id": "bot-1"},
        {"type": "loadout_confirmed"},
    ])
    to_thread_calls = []

    async def fake_connect(url):
        return socket

    def fake_fetch_map():
        bot._terrain = [[".", "."]]
        bot._map_width = 2
        bot._map_height = 1
        return True

    async def fake_to_thread(function, *args, **kwargs):
        to_thread_calls.append(function)
        return function(*args, **kwargs)

    monkeypatch.setattr(bot_module.websockets, "connect", fake_connect)
    monkeypatch.setattr(bot, "fetch_map", fake_fetch_map)
    monkeypatch.setattr(bot_module.asyncio, "to_thread", fake_to_thread)

    await bot.connect()

    assert len(to_thread_calls) == 1
    assert bot.map_init_args == ([[".", "."]], 2, 1)


@pytest.mark.asyncio
async def test_connect_closes_and_clears_socket_when_handshake_fails(monkeypatch):
    bot = ArenaBot("test-key")
    socket = FakeWebSocket([
        {"type": "error", "message": "invalid handshake"},
    ])

    async def fake_connect(url):
        return socket

    monkeypatch.setattr(bot_module.websockets, "connect", fake_connect)

    with pytest.raises(ConnectionError, match="Expected 'connected'"):
        await bot.connect()

    assert socket.closed is True
    assert bot._ws is None


@pytest.mark.asyncio
async def test_connect_cleans_up_after_loadout_rejection(monkeypatch):
    bot = ArenaBot("test-key")
    socket = FakeWebSocket([
        {"type": "connected", "bot_id": "bot-1"},
        {"type": "error", "message": "loadout rejected"},
    ])

    async def fake_connect(url):
        return socket

    monkeypatch.setattr(bot_module.websockets, "connect", fake_connect)

    with pytest.raises(ConnectionError, match="loadout rejected"):
        await bot.connect()

    assert socket.closed is True
    assert bot._ws is None


@pytest.mark.asyncio
async def test_round_start_fetches_map_off_loop(monkeypatch):
    class MapTrackingBot(ArenaBot):
        def __init__(self):
            super().__init__("test-key")
            self.map_init_args = None

        async def on_map_init(self, terrain, width, height):
            self.map_init_args = (terrain, width, height)

    bot = MapTrackingBot()
    socket = FakeWebSocket([
        {"type": "round_start"},
        {"type": "kick", "reason": "test complete"},
    ])
    bot._ws = socket
    to_thread_calls = []

    def fake_fetch_map():
        bot._terrain = [["."]]
        bot._map_width = 1
        bot._map_height = 1
        return True

    async def fake_to_thread(function, *args, **kwargs):
        to_thread_calls.append(function)
        return function(*args, **kwargs)

    monkeypatch.setattr(bot, "fetch_map", fake_fetch_map)
    monkeypatch.setattr(bot_module.asyncio, "to_thread", fake_to_thread)

    await bot._game_loop()

    assert len(to_thread_calls) == 1
    assert bot.map_init_args == ([["."]], 1, 1)


@pytest.mark.asyncio
async def test_run_closes_and_clears_socket_before_retry_wait(monkeypatch):
    class FailingBot(ArenaBot):
        def __init__(self):
            super().__init__("test-key")
            self.socket = FakeWebSocket()

        async def connect(self):
            self._ws = self.socket

        async def _game_loop(self):
            raise RuntimeError("socket failed")

    bot = FailingBot()

    async def stop_at_retry(delay):
        assert bot.socket.closed is True
        assert bot._ws is None
        bot._kicked = True

    monkeypatch.setattr(bot_module.asyncio, "sleep", stop_at_retry)

    await bot.run()


@pytest.mark.asyncio
async def test_run_escalates_backoff_for_repeated_short_sessions(monkeypatch):
    class ShortSessionBot(ArenaBot):
        def __init__(self):
            super().__init__("test-key")
            self.connections = 0

        async def connect(self):
            self.connections += 1
            self._ws = FakeWebSocket()

        async def _game_loop(self):
            raise RuntimeError("short session failed")

    bot = ShortSessionBot()
    retry_delays = []

    async def record_retry(delay):
        retry_delays.append(delay)
        if len(retry_delays) == 3:
            bot._kicked = True

    monkeypatch.setattr(bot_module.asyncio, "sleep", record_retry)

    await bot.run()

    assert retry_delays == [1.0, 2.0, 4.0]
    assert bot.connections == 3


@pytest.mark.asyncio
async def test_run_resets_accumulated_backoff_after_stable_session(monkeypatch):
    class RecoveringBot(ArenaBot):
        def __init__(self):
            super().__init__("test-key")
            self.connections = 0

        async def connect(self):
            self.connections += 1
            if self.connections <= 2:
                raise ConnectionError("handshake failed")
            self._ws = FakeWebSocket()

        async def _game_loop(self):
            raise RuntimeError("stable session ended")

    bot = RecoveringBot()
    retry_delays = []

    async def record_retry(delay):
        retry_delays.append(delay)
        if len(retry_delays) == 3:
            bot._kicked = True

    monkeypatch.setattr(bot_module, "_RECONNECT_BACKOFF_RESET_SECONDS", 0.0)
    monkeypatch.setattr(bot_module.asyncio, "sleep", record_retry)

    await bot.run()

    assert retry_delays == [1.0, 2.0, 1.0]


@pytest.mark.asyncio
async def test_run_cancellation_closes_socket_and_propagates():
    started = asyncio.Event()

    class WaitingBot(ArenaBot):
        def __init__(self):
            super().__init__("test-key")
            self.socket = FakeWebSocket()

        async def connect(self):
            self._ws = self.socket

        async def _game_loop(self):
            started.set()
            await asyncio.Future()

    bot = WaitingBot()
    task = asyncio.create_task(bot.run())
    await started.wait()

    task.cancel()

    with pytest.raises(asyncio.CancelledError):
        await task
    assert bot.socket.closed is True
    assert bot._ws is None
