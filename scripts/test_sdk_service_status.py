import asyncio
import json
import sys
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / "sdk" / "python"))

from arena_sdk import bot as bot_module  # noqa: E402
from arena_sdk.bot import ArenaBot  # noqa: E402


class StatusBot(ArenaBot):
    def __init__(self):
        super().__init__("arena_test", "ws://127.0.0.1:1/ws/bot")
        self.statuses = []

    async def on_tick(self, state, nearby, safe_zone):
        return self.idle()

    async def on_service_status(self, status):
        self.statuses.append(status)


class FakeSocket:
    def __init__(self):
        self.messages = iter([
            {"type": "connected", "bot_id": "test-bot", "service_status": {"type": "service_status", "revision": 1, "maintenance": None}},
            {"type": "service_status", "revision": 2, "maintenance": {"message": "Updating", "retry_after_seconds": 60}},
            {"type": "loadout_confirmed", "weapon": "sword"},
        ])
        self.sent = []

    async def recv(self):
        return json.dumps(next(self.messages))

    async def send(self, payload):
        self.sent.append(json.loads(payload))


async def main():
    bot = StatusBot()
    start = asyncio.get_running_loop().time()
    await bot._handle_service_status({
        "type": "service_status", "revision": 4,
        "maintenance": {"message": "Updating", "retry_after_seconds": 60},
    })
    assert bot._service_status["revision"] == 4
    assert len(bot.statuses) == 1
    assert bot._maintenance_retry_until >= start + 59

    await bot._handle_service_status({
        "type": "service_status", "revision": 4, "server_time": "later",
        "maintenance": {"message": "Restarting", "phase": "restarting", "retry_after_seconds": 30},
    })
    assert len(bot.statuses) == 2, "same-revision semantic changes must be delivered"
    await bot._handle_service_status({
        "type": "service_status", "revision": 4, "server_time": "even-later",
        "maintenance": {"message": "Restarting", "phase": "restarting", "retry_after_seconds": 30},
    })
    assert len(bot.statuses) == 2, "server_time-only changes must be deduplicated"

    await bot._handle_service_status({"type": "service_status", "revision": 3, "maintenance": None})
    assert bot._service_status["revision"] == 4
    assert len(bot.statuses) == 2

    await bot._handle_service_status({"type": "service_status", "revision": 4, "maintenance": None})
    assert bot._maintenance_retry_until == 0
    assert len(bot.statuses) == 3

    socket = FakeSocket()
    original_connect = bot_module.websockets.connect
    bot_module.websockets.connect = lambda _url: asyncio.sleep(0, result=socket)
    bot.fetch_map = lambda: False
    try:
        await bot.connect()
    finally:
        bot_module.websockets.connect = original_connect
    assert bot._bot_id == "test-bot"
    assert bot._service_status["revision"] == 4, "older handshake frames must not replace newer status"
    assert socket.sent and socket.sent[0]["type"] == "select_loadout"
    print("Python SDK service-status callback, handshake, and reconnect delay pass")


asyncio.run(main())
