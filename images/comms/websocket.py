"""WebSocket endpoint, connection registry, and message fan-out.

Agents connect via ``GET /ws?agent=<name>`` and receive a JSON stream of
push events (new messages, knowledge updates, task deliveries).  The
connection registry tracks live sockets so ``fan_out_message`` can push
to all interested channel members in real time.
"""

import json
import logging
from typing import Optional, Any

from aiohttp import WSMsgType, web

from images.comms.matcher import Matcher
from images.comms.store import MessageStore
from images.comms.subscriptions import SubscriptionManager

logger = logging.getLogger("agency.comms.ws")


class ConnectionRegistry:
    """In-memory map of agent_name -> WebSocketResponse."""

    def __init__(self) -> None:
        self._connections: dict[str, web.WebSocketResponse] = {}

    def add(self, agent_name: str, ws: web.WebSocketResponse) -> None:
        old = self._connections.get(agent_name)
        if old and not old.closed:
            import asyncio
            asyncio.ensure_future(old.close())
        self._connections[agent_name] = ws

    def remove(self, agent_name: str) -> None:
        self._connections.pop(agent_name, None)

    def get(self, agent_name: str) -> Optional[web.WebSocketResponse]:
        return self._connections.get(agent_name)

    def connected_agents(self) -> list[str]:
        return list(self._connections.keys())


async def push_to_agent(
    registry: ConnectionRegistry,
    agent_name: str,
    event: dict[str, Any],
) -> bool:
    """Push a JSON event dict to a connected agent. Returns True on success."""
    ws = registry.get(agent_name)
    if ws is None or ws.closed:
        return False
    try:
        await ws.send_json(event)
        return True
    except Exception:
        logger.debug("Failed to push to %s", agent_name)
        return False


async def fan_out_message(
    app: web.Application,
    channel_name: str,
    message: dict[str, Any],
    author: str,
) -> None:
    """Fan out a posted message to all connected channel members.

    Skips the author.  Uses the Matcher for interest classification and
    the SubscriptionManager for per-agent interest declarations.
    """
    store: MessageStore = app["store"]
    registry: ConnectionRegistry = app["ws_registry"]
    matcher: Optional[Matcher] = app.get("matcher")
    sub_manager: Optional[SubscriptionManager] = app.get("sub_manager")

    # Look up channel members
    try:
        ch = store.get_channel(channel_name)
    except ValueError:
        return

    content = message.get("content", "")
    is_knowledge = channel_name == "_knowledge-updates"

    # System observers (gateway, operator) receive all messages regardless
    # of channel membership — they need full visibility for relay/UI.
    system_observers = {"_gateway", "_operator"}
    recipients = set(ch.members)
    for observer in system_observers:
        if registry.get(observer) is not None:
            recipients.add(observer)

    for member in recipients:
        if member == author:
            continue

        # System observers always get all events (no filtering)
        is_system = member in system_observers

        is_direct_channel = ch.type == "direct" or channel_name.startswith("dm-")

        # Determine classification
        classification = "ambient"
        matched_keywords: list[str] = []

        # Direct-message channels should always reach their participants as direct
        # operator-to-agent work, even without an explicit @mention.
        if is_direct_channel and not is_system:
            classification = "direct"

        # Check @mention first (always classified as "direct")
        is_mentioned = f"@{member}" in content

        if is_mentioned:
            classification = "direct"
        elif classification == "ambient" and matcher and sub_manager:
            # Check merged expertise keywords (new) + legacy interests
            merged_kws = sub_manager.get_merged_keywords(member)
            interests = sub_manager.get(member)
            if merged_kws:
                # Simple keyword matching against merged expertise
                content_lower = content.lower()
                for kw in merged_kws:
                    if kw in content_lower:
                        classification = "interest_match"
                        matched_keywords.append(kw)
            if classification == "ambient" and interests:
                if is_knowledge:
                    result = matcher.classify_knowledge(
                        member,
                        content,
                        message.get("flags", {}),
                        interests,
                    )
                    if result is None and not is_system:
                        continue
                    if result:
                        classification = result.classification
                        matched_keywords = result.matched_keywords
                else:
                    result = matcher.classify(member, content, interests)
                    classification = result.classification
                    matched_keywords = result.matched_keywords

        # Apply channel responsiveness filtering (skip for system observers)
        if not is_system:
            responsiveness = sub_manager.get_responsiveness(member) if sub_manager else {}
            channel_mode = responsiveness.get(channel_name, responsiveness.get("default", "mention-only"))
            if channel_mode == "silent":
                continue
            if channel_mode == "mention-only" and classification != "direct":
                continue
            # "active" mode: deliver direct + interest_match (drop ambient)

        # Generate summary
        summary = content
        if matcher:
            summary = matcher.generate_summary(content)

        event = {
            "v": 1,
            "type": "message",
            "channel": channel_name,
            "match": classification,
            "matched_keywords": matched_keywords,
            "message": {
                **message,
                "summary": summary,
            },
        }

        await push_to_agent(registry, member, event)


async def handle_websocket(request: web.Request) -> web.WebSocketResponse:
    """WebSocket handler: ``GET /ws?agent=<name>``.

    On connect the server sends an ``ack`` event with the agent's channel
    list and unread counts.  The connection then stays open for server-push
    events.  The client may send ``{"type": "ack", "event_id": ...}`` but
    those are currently informational only (no re-delivery queue yet).
    """
    agent_name = request.query.get("agent")
    if not agent_name:
        return web.Response(text="agent query parameter required", status=400)

    ws = web.WebSocketResponse()
    await ws.prepare(request)

    registry: ConnectionRegistry = request.app["ws_registry"]
    store: MessageStore = request.app["store"]

    # Register connection
    registry.add(agent_name, ws)
    logger.info("WS connected: %s", agent_name)

    try:
        # Send ack with channels + unreads
        channels = store.list_channels(member=agent_name)
        unreads = store.get_unreads(agent_name)
        ack = {
            "v": 1,
            "type": "ack",
            "data": {
                "agent": agent_name,
                "channels": [c.name for c in channels],
                "unreads": unreads,
            },
        }
        await ws.send_json(ack)

        # Listen for client messages (acks, etc.)
        async for msg in ws:
            if msg.type == WSMsgType.TEXT:
                try:
                    data = json.loads(msg.data)
                    if data.get("type") == "ack":
                        logger.debug("Client ack from %s: %s", agent_name, data.get("event_id"))
                except (json.JSONDecodeError, AttributeError):
                    pass
            elif msg.type in (WSMsgType.ERROR, WSMsgType.CLOSE):
                break
    finally:
        registry.remove(agent_name)
        logger.info("WS disconnected: %s", agent_name)

    return ws


async def handle_connected(request: web.Request) -> web.Response:
    agent_name = request.match_info["agent_name"]
    registry: ConnectionRegistry = request.app["ws_registry"]
    ws = registry.get(agent_name)
    return web.json_response({
        "agent": agent_name,
        "connected": bool(ws is not None and not ws.closed),
    })


def setup_websocket(app: web.Application) -> None:
    """Register the WebSocket route and initialize the connection registry."""
    registry = ConnectionRegistry()
    app["ws_registry"] = registry
    app.router.add_get("/ws/connected/{agent_name}", handle_connected)
    app.router.add_get("/ws", handle_websocket)
