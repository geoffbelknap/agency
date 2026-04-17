"""WebSocket listener thread for the body runtime.

Background thread that maintains a WebSocket connection to the comms server,
handles reconnection with exponential backoff, fetches missed messages on
reconnect, and feeds parsed events into a thread-safe queue for the main
loop to consume.
"""

import asyncio
import json
import logging
import queue
import threading
from datetime import datetime, timezone
from pathlib import Path
from typing import Optional

logger = logging.getLogger("body.ws_listener")


def _backoff_delay(attempt: int) -> int:
    """Return reconnection delay in seconds, capped at 30.

    attempt=0 → 1s, attempt=1 → 2s, attempt=2 → 4s, …, attempt≥5 → 30s cap.
    """
    return min(2**attempt, 30)


def read_context_fallback(context_file: Path) -> Optional[dict]:
    """Read session-context.json and return the current_task dict, or None.

    Returns None when the file is missing, empty, or contains invalid JSON,
    or when the parsed value has no ``current_task`` key.
    """
    if context_file is None:
        return None
    try:
        text = context_file.read_text(encoding="utf-8").strip()
        if not text:
            return None
        data = json.loads(text)
        return data.get("current_task")
    except (OSError, FileNotFoundError, json.JSONDecodeError, AttributeError):
        return None


class WSListener:
    """Background thread that keeps a WebSocket connection to the comms server.

    Events arriving on the socket are parsed as JSON and placed into
    ``event_queue`` (a ``queue.Queue``) for the main body loop to consume.

    On reconnect after a disconnection, fetches any messages that arrived
    while the socket was down (using the comms ``since`` parameter) so no
    messages are silently dropped.

    Reconnection uses exponential backoff (capped at 30s).  The thread is a
    daemon thread so it will not block process exit.

    Usage::

        listener = WSListener(
            comms_url="http://enforcer:8081/mediation/comms",
            agent_name="scout",
            event_queue=q,
            context_file=Path("/workspace/session-context.json"),
        )
        listener.start()
        …
        listener.stop()
    """

    def __init__(
        self,
        comms_url: str,
        agent_name: str,
        event_queue: queue.Queue,
        context_file: Optional[Path] = None,
    ) -> None:
        self.comms_url = comms_url
        self.agent_name = agent_name
        self.event_queue = event_queue
        self.context_file = context_file

        self._stop_event = threading.Event()
        self.connected = False
        self._thread: Optional[threading.Thread] = None
        self._last_connected_at: Optional[str] = None  # ISO timestamp of last successful connection

    # ------------------------------------------------------------------
    # Public lifecycle API
    # ------------------------------------------------------------------

    def start(self) -> None:
        """Start the daemon listener thread."""
        self._thread = threading.Thread(target=self._run, daemon=True, name="ws-listener")
        self._thread.start()
        logger.info("WSListener started for agent=%s url=%s", self.agent_name, self.comms_url)

    def stop(self) -> None:
        """Signal the listener to stop and wait for the thread to finish."""
        self._stop_event.set()
        if self._thread is not None:
            self._thread.join(timeout=5)
        logger.info("WSListener stopped for agent=%s", self.agent_name)

    # ------------------------------------------------------------------
    # Internal thread entry point
    # ------------------------------------------------------------------

    def _run(self) -> None:
        """Create a dedicated asyncio event loop and run the connect loop."""
        loop = asyncio.new_event_loop()
        asyncio.set_event_loop(loop)
        try:
            loop.run_until_complete(self._connect_loop())
        except Exception:
            logger.exception("WSListener _run raised unexpectedly")
        finally:
            loop.close()

    # ------------------------------------------------------------------
    # Async connect loop (runs in the listener's own event loop)
    # ------------------------------------------------------------------

    async def _connect_loop(self) -> None:
        """Async loop: connect, read messages, reconnect with backoff."""
        import aiohttp  # imported here so the module is usable without aiohttp at import time

        ws_url = self._http_to_ws(self.comms_url) + f"/ws?agent={self.agent_name}"
        attempt = 0
        disconnect_at: Optional[str] = None  # ISO timestamp when we lost connection

        while not self._stop_event.is_set():
            try:
                async with aiohttp.ClientSession() as session:
                    logger.info("WSListener connecting attempt=%d url=%s", attempt, ws_url)
                    async with session.ws_connect(ws_url) as ws:
                        self.connected = True
                        self._last_connected_at = datetime.now(timezone.utc).isoformat()
                        attempt = 0  # reset on successful connect
                        logger.info("WSListener connected agent=%s", self.agent_name)

                        # On reconnect, surface any missed tasks from context file
                        missed = read_context_fallback(self.context_file)
                        if missed is not None:
                            self.event_queue.put({
                                "type": "task",
                                "source": "context_fallback",
                                "task": missed,
                            })

                        # Catch up on messages missed during disconnection
                        if disconnect_at is not None:
                            await self._fetch_missed_messages(session, disconnect_at)
                            disconnect_at = None

                        # Read messages until disconnected or stopped.
                        # Use receive() with a timeout so we can check the stop
                        # event promptly without waiting for the next message.
                        while not self._stop_event.is_set():
                            try:
                                msg = await asyncio.wait_for(ws.receive(), timeout=0.2)
                            except asyncio.TimeoutError:
                                continue  # No message yet; re-check stop_event

                            if msg.type == aiohttp.WSMsgType.TEXT:
                                try:
                                    event = json.loads(msg.data)
                                    self.event_queue.put(event)
                                except json.JSONDecodeError:
                                    logger.warning(
                                        "WSListener received non-JSON: %r", msg.data[:200]
                                    )
                            elif msg.type in (
                                aiohttp.WSMsgType.ERROR,
                                aiohttp.WSMsgType.CLOSE,
                            ):
                                logger.info(
                                    "WSListener socket closed/error agent=%s type=%s",
                                    self.agent_name,
                                    msg.type,
                                )
                                break

                        if self._stop_event.is_set():
                            await ws.close()
                            return
            except Exception as exc:
                logger.warning(
                    "WSListener connection failed agent=%s attempt=%d: %s",
                    self.agent_name,
                    attempt,
                    exc,
                )
            finally:
                if self.connected:
                    disconnect_at = datetime.now(timezone.utc).isoformat()
                self.connected = False

            if self._stop_event.is_set():
                return

            delay = _backoff_delay(attempt)
            attempt += 1
            logger.info(
                "WSListener reconnecting in %ds (attempt=%d) agent=%s",
                delay,
                attempt,
                self.agent_name,
            )
            # Sleep in small increments so stop_event is checked promptly
            for _ in range(delay * 10):
                if self._stop_event.is_set():
                    return
                await asyncio.sleep(0.1)

    # ------------------------------------------------------------------
    # Reconnect catch-up
    # ------------------------------------------------------------------

    async def _fetch_missed_messages(
        self, session, disconnect_at: str,
    ) -> None:
        """Fetch messages that arrived while the WebSocket was disconnected.

        Queries the comms REST API for each channel the agent is a member of,
        using the ``since`` parameter to get only messages posted after the
        disconnection timestamp. Injects them into the event queue as regular
        message events so the InterruptionController handles them normally.
        """
        try:
            # Get channels this agent is a member of
            url = f"{self.comms_url}/channels?member={self.agent_name}"
            async with session.get(url) as resp:
                if resp.status != 200:
                    logger.warning("Failed to list channels for catch-up: %d", resp.status)
                    return
                channels = await resp.json()

            total_caught_up = 0
            for ch in channels:
                ch_name = ch if isinstance(ch, str) else ch.get("name", "")
                if not ch_name or ch_name.startswith("_"):
                    continue  # skip internal channels

                msgs_url = (
                    f"{self.comms_url}/channels/{ch_name}/messages"
                    f"?since={disconnect_at}&reader={self.agent_name}"
                )
                async with session.get(msgs_url) as resp:
                    if resp.status != 200:
                        continue
                    messages = await resp.json()

                for msg in messages:
                    # Skip messages authored by this agent
                    if msg.get("author") == self.agent_name:
                        continue
                    content = msg.get("content", "")
                    # DM channel traffic is a direct operator-to-agent path even
                    # when the operator does not include an explicit @mention.
                    match = "direct" if ch_name.startswith("dm-") else "ambient"
                    if f"@{self.agent_name}" in content:
                        match = "direct"

                    self.event_queue.put({
                        "v": 1,
                        "type": "message",
                        "channel": ch_name,
                        "match": match,
                        "matched_keywords": [],
                        "message": msg,
                        "source": "reconnect_catchup",
                    })
                    total_caught_up += 1

            if total_caught_up:
                logger.info(
                    "WSListener catch-up: injected %d missed messages since %s",
                    total_caught_up, disconnect_at,
                )
        except Exception as e:
            logger.warning("WSListener catch-up failed: %s", e)

    # ------------------------------------------------------------------
    # Helpers
    # ------------------------------------------------------------------

    @staticmethod
    def _http_to_ws(url: str) -> str:
        """Convert http(s):// URL scheme to ws(s)://."""
        if url.startswith("https://"):
            return "wss://" + url[len("https://"):]
        if url.startswith("http://"):
            return "ws://" + url[len("http://"):]
        return url
