"""Agency comms HTTP server.

Lightweight aiohttp server exposing channel and message management
for agent communication. Runs on the mediation network as an
infrastructure sidecar.

Endpoints:
    GET  /health                                 - Health check
    POST /channels                               - Create channel
    GET  /channels?member=X                      - List channels (optionally filtered)
    POST /channels/{name}/join                   - Join a channel
    POST /channels/{name}/messages               - Post a message
    GET  /channels/{name}/messages               - Read messages
    PUT  /channels/{name}/messages/{message_id}  - Edit a message
    DELETE /channels/{name}/messages/{message_id} - Delete a message
    GET  /unreads/{participant}                   - Get unread counts
    POST /channels/{name}/mark-read              - Mark channel as read
    GET  /search?q=X&channel=Y&author=Z          - Search messages
    GET  /ws?agent=X                             - WebSocket push connection
    POST /subscriptions/{agent_name}/interests   - Register task interests for an agent
    DELETE /subscriptions/{agent_name}/interests - Clear task interests for an agent
    POST /tasks/{agent_name}                     - Deliver a task (file + WebSocket push)
"""

import argparse
import json
import logging
import os
from datetime import datetime, timedelta, timezone
from pathlib import Path
from uuid import uuid4

import httpx
from aiohttp import web
from aiohttp.abc import AbstractAccessLogger

from typing import Optional


class _HealthFilterAccessLogger(AbstractAccessLogger):
    """Access logger that suppresses /health requests (Docker healthcheck noise)."""

    def log(self, request, response, time):
        if request.path == "/health":
            return
        self.logger.info(
            '%s "%s %s" %s %.3fs',
            request.remote, request.method, request.path_qs,
            response.status, time,
        )
from images.comms.matcher import Matcher
from images.comms.store import MessageStore
from images.comms.subscriptions import SubscriptionManager
from images.comms.websocket import fan_out_message, push_to_agent, setup_websocket
from images.models.comms import ChannelState, ChannelType
from images.models.subscriptions import InterestDeclaration

logger = logging.getLogger("agency.comms")


async def _close_websocket_connections(app: web.Application) -> None:
    """Close all active WebSocket connections on shutdown."""
    registry = app.get("ws_registry")
    if not registry:
        return
    for agent_name in registry.connected_agents():
        ws = registry.get(agent_name)
        if ws and not ws.closed:
            await ws.close()
    logger.info("Closed WebSocket connections")


async def _log_shutdown(app: web.Application) -> None:
    logger.info("Comms server shutting down")


def create_app(data_dir: Optional[Path] = None, agents_dir: Optional[Path] = None) -> web.Application:
    app = web.Application()
    resolved_data_dir = data_dir or Path("/app/data")
    app["store"] = MessageStore(resolved_data_dir)
    app["agents_dir"] = agents_dir or Path("/app/agents")
    app["matcher"] = Matcher(resolved_data_dir)
    app["sub_manager"] = SubscriptionManager(resolved_data_dir)
    setup_websocket(app)
    app.router.add_get("/health", handle_health)
    app.router.add_post("/channels", handle_create_channel)
    app.router.add_get("/channels", handle_list_channels)
    app.router.add_post("/channels/{name}/archive", handle_archive_channel)
    app.router.add_post("/channels/{name}/grant-access", handle_grant_access)
    app.router.add_post("/channels/{name}/join", handle_join_channel)
    app.router.add_post("/channels/{name}/leave", handle_leave_channel)
    app.router.add_post("/participants/{participant}/leave-all", handle_leave_all_channels)
    app.router.add_post("/channels/{name}/messages", handle_post_message)
    app.router.add_get("/channels/{name}/messages", handle_read_messages)
    app.router.add_put("/channels/{name}/messages/{message_id}", handle_edit_message)
    app.router.add_delete("/channels/{name}/messages/{message_id}", handle_delete_message)
    app.router.add_post("/channels/{name}/messages/{message_id}/reactions", handle_add_reaction)
    app.router.add_delete("/channels/{name}/messages/{message_id}/reactions/{emoji}", handle_remove_reaction)
    app.router.add_get("/unreads/{participant}", handle_get_unreads)
    app.router.add_post("/channels/{name}/mark-read", handle_mark_read)
    app.router.add_post("/cursors/{participant}/reset", handle_reset_cursors)
    app.router.add_get("/search", handle_search)
    app.router.add_post("/tasks/deliver", handle_deliver_task)
    app.router.add_post("/subscriptions/{agent_name}/interests", handle_register_interests)
    app.router.add_delete("/subscriptions/{agent_name}/interests", handle_clear_interests)
    app.router.add_post("/subscriptions/{agent_name}/expertise", handle_register_expertise)
    app.router.add_get("/subscriptions/{agent_name}/expertise", handle_get_expertise)
    app.router.add_delete("/subscriptions/{agent_name}/expertise", handle_clear_expertise)
    app.router.add_post("/subscriptions/{agent_name}/responsiveness", handle_register_responsiveness)
    app.router.add_post("/tasks/{agent_name}", handle_deliver_task_v2)
    app.router.add_post("/signals", handle_signal)

    app.on_shutdown.append(_close_websocket_connections)
    app.on_shutdown.append(_log_shutdown)

    mode = os.environ.get("COMMS_MODE", "primary")
    app["mode"] = mode
    if mode == "cache":
        upstream = os.environ.get("COMMS_UPSTREAM", "")
        app["upstream_url"] = upstream
        # Use a mutable container so handlers can update state without aiohttp deprecation warning
        app["upstream_state"] = {"ok": False}  # start pessimistic
        app.on_startup.append(_start_upstream_client)
        app.on_cleanup.append(_stop_upstream_client)

    return app


async def _start_upstream_client(app):
    app["http"] = httpx.AsyncClient(timeout=httpx.Timeout(2.0, connect=2.0))


async def _stop_upstream_client(app):
    client = app.get("http")
    if client:
        await client.aclose()


async def handle_health(request: web.Request) -> web.Response:
    mode = request.app.get("mode", "primary")
    resp = {"status": "ok", "mode": mode}
    if mode == "cache":
        state = request.app.get("upstream_state", {})
        resp["upstream_ok"] = state.get("ok", False)
    return web.json_response(resp)


async def handle_create_channel(request: web.Request) -> web.Response:
    store: MessageStore = request.app["store"]

    if request.app.get("mode") == "cache":
        return await _cache_create_channel(request, store)

    try:
        body = await request.json()
    except Exception:
        return web.json_response({"error": "invalid JSON body"}, status=400)
    try:
        ch = store.create_channel(
            name=body["name"],
            type=ChannelType(body["type"]),
            created_by=body.get("created_by", "_platform"),
            topic=body.get("topic", ""),
            members=body.get("members"),
            visibility=body.get("visibility", "open"),
        )
        return web.json_response(ch.model_dump(mode="json"), status=201)
    except ValueError as e:
        return web.json_response({"error": str(e)}, status=409)


async def handle_list_channels(request: web.Request) -> web.Response:
    store: MessageStore = request.app["store"]
    if request.app.get("mode") == "cache":
        return await _cache_list_channels(request, store)
    member = request.query.get("member")
    state = request.query.get("state", "active")
    channels = store.list_channels(member=member, state=state)
    return web.json_response([c.model_dump(mode="json") for c in channels])


async def handle_join_channel(request: web.Request) -> web.Response:
    store: MessageStore = request.app["store"]
    name = request.match_info["name"]
    try:
        body = await request.json()
    except Exception:
        return web.json_response({"error": "invalid JSON body"}, status=400)

    if request.app.get("mode") == "cache":
        return await _cache_join_channel(request, store, name, body)

    try:
        store.join_channel(name, body["participant"])
        return web.json_response({"status": "joined"})
    except ValueError as e:
        error_msg = str(e)
        if "private" in error_msg.lower():
            return web.json_response({"error": error_msg}, status=403)
        return web.json_response({"error": error_msg}, status=404)


async def handle_leave_channel(request: web.Request) -> web.Response:
    store: MessageStore = request.app["store"]
    name = request.match_info["name"]
    try:
        body = await request.json()
    except Exception:
        return web.json_response({"error": "invalid JSON body"}, status=400)
    try:
        store.leave_channel(name, body["participant"])
        return web.json_response({"status": "left"})
    except ValueError as e:
        return web.json_response({"error": str(e)}, status=404)


async def handle_leave_all_channels(request: web.Request) -> web.Response:
    store: MessageStore = request.app["store"]
    participant = request.match_info["participant"]
    store.leave_all_channels(participant)
    return web.json_response({"status": "left_all"})


async def handle_post_message(request: web.Request) -> web.Response:
    store: MessageStore = request.app["store"]
    channel = request.match_info["name"]
    try:
        body = await request.json()
    except Exception:
        return web.json_response({"error": "invalid JSON body"}, status=400)

    # Validate X-Agency-Cache-Relay header if present
    relay_host = request.headers.get("X-Agency-Cache-Relay")
    if relay_host:
        hosts_registry = request.app.get("hosts_registry")
        if hosts_registry is not None and relay_host not in hosts_registry:
            return web.json_response(
                {"error": f"Unknown relay host: {relay_host!r}"},
                status=403,
            )

    # Enforce platform-write visibility
    try:
        ch = store.get_channel(channel)
        if ch.state == ChannelState.ARCHIVED:
            return web.json_response(
                {"error": "Channel is archived (read-only)"},
                status=403,
            )
        if ch.visibility == "platform-write":
            if request.headers.get("X-Agency-Platform") != "true":
                return web.json_response(
                    {"error": "platform-write channel requires X-Agency-Platform header"},
                    status=403,
                )
    except ValueError:
        return web.json_response({"error": f"Channel {channel!r} not found"}, status=404)

    if request.app.get("mode") == "cache":
        return await _cache_post_message(request, store, channel, body)

    try:
        msg = store.post_message(
            channel=channel,
            author=body["author"],
            content=body["content"],
            reply_to=body.get("reply_to"),
            flags=body.get("flags"),
            metadata=body.get("metadata"),
        )
        msg_dict = msg.model_dump(mode="json")

        # Fan out to connected WebSocket clients
        if request.app.get("ws_registry"):
            await fan_out_message(request.app, channel, msg_dict, body["author"])

        return web.json_response(msg_dict, status=201)
    except ValueError as e:
        error_msg = str(e)
        if "not a member" in error_msg.lower():
            return web.json_response({"error": error_msg}, status=403)
        return web.json_response({"error": error_msg}, status=404)


async def handle_read_messages(request: web.Request) -> web.Response:
    store: MessageStore = request.app["store"]
    channel = request.match_info["name"]
    since = request.query.get("since")
    limit = int(request.query.get("limit", "50"))
    reader = request.query.get("reader")

    since_dt = datetime.fromisoformat(since) if since else None

    if request.app.get("mode") == "cache":
        return await _cache_read_messages(request, store, channel, since, since_dt, limit, reader)

    try:
        msgs = store.read_messages(channel, since=since_dt, limit=limit, reader=reader)
        return web.json_response([m.model_dump(mode="json") for m in msgs])
    except ValueError as e:
        error_msg = str(e)
        if "not a member" in error_msg.lower():
            return web.json_response({"error": error_msg}, status=403)
        return web.json_response({"error": error_msg}, status=404)


async def handle_edit_message(request: web.Request) -> web.Response:
    store: MessageStore = request.app["store"]
    channel = request.match_info["name"]
    message_id = request.match_info["message_id"]
    try:
        body = await request.json()
    except Exception:
        return web.json_response({"error": "invalid JSON body"}, status=400)

    content = body.get("content")
    author = body.get("author")
    if not content or not author:
        return web.json_response({"error": "content and author required"}, status=400)

    try:
        msg = store.edit_message(channel, message_id, content, author)
        return web.json_response(msg.model_dump(mode="json"))
    except ValueError as e:
        return web.json_response({"error": str(e)}, status=404)
    except PermissionError as e:
        return web.json_response({"error": str(e)}, status=403)


async def handle_delete_message(request: web.Request) -> web.Response:
    store: MessageStore = request.app["store"]
    channel = request.match_info["name"]
    message_id = request.match_info["message_id"]

    # Accept author from body or query param
    author = request.query.get("author")
    if not author:
        try:
            body = await request.json()
            author = body.get("author")
        except Exception:
            pass

    if not author:
        return web.json_response({"error": "author required"}, status=400)

    try:
        store.delete_message(channel, message_id, author)
        return web.json_response({"ok": True})
    except ValueError as e:
        return web.json_response({"error": str(e)}, status=404)
    except PermissionError as e:
        return web.json_response({"error": str(e)}, status=403)


async def handle_add_reaction(request: web.Request) -> web.Response:
    store: MessageStore = request.app["store"]
    channel = request.match_info["name"]
    message_id = request.match_info["message_id"]
    try:
        body = await request.json()
    except Exception:
        return web.json_response({"error": "invalid JSON body"}, status=400)

    emoji = body.get("emoji")
    author = body.get("author")
    if not emoji or not author:
        return web.json_response({"error": "emoji and author required"}, status=400)

    try:
        msg = store.add_reaction(channel, message_id, emoji, author)
        return web.json_response(msg.model_dump(mode="json"))
    except ValueError as e:
        return web.json_response({"error": str(e)}, status=404)


async def handle_remove_reaction(request: web.Request) -> web.Response:
    store: MessageStore = request.app["store"]
    channel = request.match_info["name"]
    message_id = request.match_info["message_id"]
    emoji = request.match_info["emoji"]
    author = request.query.get("author")

    if not author:
        return web.json_response({"error": "author query param required"}, status=400)

    try:
        store.remove_reaction(channel, message_id, emoji, author)
        return web.json_response({"ok": True})
    except ValueError as e:
        return web.json_response({"error": str(e)}, status=404)


# -- Cache mode helpers --


async def _cache_create_channel(request: web.Request, store: MessageStore) -> web.Response:
    """Cache mode: forward channel creation to upstream."""
    http = request.app["http"]
    upstream = request.app["upstream_url"]
    host_name = os.environ.get("AGENCY_HOST_NAME", "unknown")
    try:
        body = await request.json()
    except Exception:
        return web.json_response({"error": "invalid JSON body"}, status=400)

    try:
        resp = await http.post(
            f"{upstream}/channels",
            json=body,
            headers={"X-Agency-Cache-Relay": host_name},
        )
        if resp.status_code in (200, 201):
            ch_data = resp.json()
            # Cache locally
            try:
                store.create_channel(
                    name=ch_data["name"],
                    type=ChannelType(ch_data["type"]),
                    created_by=ch_data["created_by"],
                    topic=ch_data.get("topic", ""),
                    members=ch_data.get("members"),
                    visibility=ch_data.get("visibility", "open"),
                )
            except ValueError:
                pass  # Already cached
            request.app["upstream_state"]["ok"] = True
            return web.json_response(ch_data, status=201)
        return web.json_response(
            {"error": f"Upstream error: {resp.status_code}"},
            status=resp.status_code,
        )
    except Exception:
        request.app["upstream_state"]["ok"] = False
        return web.json_response(
            {"error": "Manager unreachable — cannot create channels in cache mode"},
            status=503,
        )


def _cache_messages(store: MessageStore, channel: str, messages: list) -> None:
    """Cache upstream messages in local store (dedup by ID)."""
    try:
        existing_ids = set()
        jsonl_path = store._channels_dir / f"{channel}.jsonl"
        if jsonl_path.exists():
            for line in jsonl_path.read_text().strip().splitlines():
                if line.strip():
                    raw = json.loads(line)
                    existing_ids.add(raw.get("id"))
        with open(jsonl_path, "a") as f:
            for msg in messages:
                if msg.get("id") not in existing_ids:
                    f.write(json.dumps(msg) + "\n")
                    existing_ids.add(msg.get("id"))
    except Exception:
        pass  # Cache failure is not fatal


async def _cache_post_message(request: web.Request, store: MessageStore, channel: str, body: dict) -> web.Response:
    """Cache mode: try upstream, buffer on failure."""
    state = request.app.get("upstream_state", {})
    http = request.app["http"]
    upstream = request.app["upstream_url"]
    host_name = os.environ.get("AGENCY_HOST_NAME", "unknown")

    try:
        resp = await http.post(
            f"{upstream}/channels/{channel}/messages",
            json={
                "author": body["author"],
                "content": body["content"],
                "reply_to": body.get("reply_to"),
                "flags": body.get("flags"),
            },
            headers={"X-Agency-Cache-Relay": host_name},
        )
        if resp.status_code == 201:
            server_msg = resp.json()
            _cache_messages(store, channel, [server_msg])
            state["ok"] = True
            return web.json_response(server_msg, status=201)
    except Exception:
        state["ok"] = False

    # Upstream unavailable — buffer locally
    entry = store.buffer_message(
        channel=channel,
        author=body["author"],
        content=body["content"],
        reply_to=body.get("reply_to"),
        flags=body.get("flags"),
    )
    return web.json_response(entry, status=201)


async def _try_buffer_drain(app: web.Application, store: MessageStore, channel: str) -> None:
    """Try to drain the write buffer for this channel if upstream is available."""
    state = app.get("upstream_state", {})
    if not state.get("ok", False):
        return
    http = app["http"]
    upstream = app["upstream_url"]
    host_name = os.environ.get("AGENCY_HOST_NAME", "unknown")
    entries = store.read_buffer(channel)
    if not entries:
        return

    drained = []
    rejected = []
    earliest_ts = entries[0].get("timestamp", "")
    latest_ts = entries[-1].get("timestamp", "")

    for entry in entries:
        try:
            resp = await http.post(
                f"{upstream}/channels/{channel}/messages",
                json={
                    "author": entry["author"],
                    "content": entry["content"],
                    "reply_to": entry.get("reply_to"),
                    "flags": entry.get("flags"),
                },
                headers={"X-Agency-Cache-Relay": host_name},
            )
            if resp.status_code == 201:
                server_msg = resp.json()
                store.add_id_remap(entry["id"], server_msg.get("id", entry["id"]))
                _cache_messages(store, channel, [server_msg])
                store.remove_buffer_entry(channel, entry["id"])
                drained.append(entry["id"])
            elif resp.status_code == 403:
                logger.warning(
                    "Buffer drain rejected: channel=%s author=%s",
                    channel, entry["author"],
                )
                rejected.append({
                    "agent": entry["author"],
                    "channel": channel,
                    "reason": resp.text[:200],
                })
                store.remove_buffer_entry(channel, entry["id"])
            else:
                break
        except Exception:
            state["ok"] = False
            break

    # Emit audit events
    audit_log = app.get("audit_log")
    if rejected and audit_log:
        for r in rejected:
            audit_log.record("buffer_drain_rejected", r)
    if drained and audit_log:
        audit_log.record("buffer_drained", {
            "channel": channel,
            "count": len(drained),
            "earliest": earliest_ts,
            "latest": latest_ts,
            "rejected_count": len(rejected),
        })
    if drained:
        logger.info("Drained %d buffered messages for channel %s", len(drained), channel)


async def _cache_read_messages(
    request: web.Request,
    store: MessageStore,
    channel: str,
    since_str: Optional[str],
    since_dt,
    limit: int,
    reader: Optional[str],
) -> web.Response:
    """Cache mode: try upstream, fall back to local cache."""
    state = request.app.get("upstream_state", {})
    http = request.app["http"]
    upstream = request.app["upstream_url"]
    try:
        params = {"limit": str(limit)}
        if since_str:
            params["since"] = since_str
        if reader:
            params["reader"] = reader
        host_name = os.environ.get("AGENCY_HOST_NAME", "unknown")
        resp = await http.get(
            f"{upstream}/channels/{channel}/messages",
            params=params,
            headers={"X-Agency-Cache-Relay": host_name},
        )
        if resp.status_code == 200:
            messages = resp.json()
            _cache_messages(store, channel, messages)
            state["ok"] = True
            await _try_buffer_drain(request.app, store, channel)
            return web.json_response(messages)
    except Exception:
        state["ok"] = False

    # Serve from local cache
    try:
        msgs = store.read_messages(channel, since=since_dt, limit=limit, reader=reader)
        return web.json_response([m.model_dump(mode="json") for m in msgs])
    except ValueError:
        return web.json_response([], status=200)


async def _cache_list_channels(request: web.Request, store: MessageStore) -> web.Response:
    """Cache mode: try upstream, fall back to local."""
    state = request.app.get("upstream_state", {})
    http = request.app["http"]
    upstream = request.app["upstream_url"]
    member = request.query.get("member")
    host_name = os.environ.get("AGENCY_HOST_NAME", "unknown")
    try:
        params = {}
        if member:
            params["member"] = member
        resp = await http.get(
            f"{upstream}/channels",
            params=params,
            headers={"X-Agency-Cache-Relay": host_name},
        )
        if resp.status_code == 200:
            state["ok"] = True
            return web.json_response(resp.json())
    except Exception:
        state["ok"] = False
    # Fall back to local cache
    channels = store.list_channels(member=member)
    return web.json_response([c.model_dump(mode="json") for c in channels])


async def _cache_join_channel(request: web.Request, store: MessageStore, name: str, body: dict) -> web.Response:
    """Cache mode: forward join to upstream. Fail 503 if down."""
    http = request.app["http"]
    upstream = request.app["upstream_url"]
    host_name = os.environ.get("AGENCY_HOST_NAME", "unknown")
    try:
        resp = await http.post(
            f"{upstream}/channels/{name}/join",
            json=body,
            headers={"X-Agency-Cache-Relay": host_name},
        )
        if resp.status_code == 200:
            try:
                store.join_channel(name, body["participant"])
            except ValueError:
                pass
            request.app["upstream_state"]["ok"] = True
            return web.json_response(resp.json())
        return web.json_response(
            {"error": f"Upstream error: {resp.status_code}"},
            status=resp.status_code,
        )
    except Exception:
        request.app["upstream_state"]["ok"] = False
        return web.json_response(
            {"error": "Manager unreachable — cannot join channels in cache mode"},
            status=503,
        )


async def handle_archive_channel(request: web.Request) -> web.Response:
    store: MessageStore = request.app["store"]
    name = request.match_info["name"]
    if request.headers.get("X-Agency-Platform") != "true":
        return web.json_response(
            {"error": "Archive requires X-Agency-Platform header"},
            status=403,
        )
    try:
        body = await request.json()
    except Exception:
        body = {}
    archived_by = body.get("archived_by", "operator")
    try:
        ch = store.archive_channel(name, archived_by)
        return web.json_response(ch.model_dump(mode="json"))
    except ValueError as e:
        error_msg = str(e)
        if "not found" in error_msg.lower():
            return web.json_response({"error": error_msg}, status=404)
        return web.json_response({"error": error_msg}, status=409)


async def handle_grant_access(request: web.Request) -> web.Response:
    store: MessageStore = request.app["store"]
    name = request.match_info["name"]
    if request.headers.get("X-Agency-Platform") != "true":
        return web.json_response(
            {"error": "Grant access requires X-Agency-Platform header"},
            status=403,
        )
    try:
        body = await request.json()
    except Exception:
        return web.json_response({"error": "invalid JSON body"}, status=400)
    agent = body.get("agent")
    if not agent:
        return web.json_response({"error": "agent field required"}, status=400)
    try:
        ch = store.grant_channel_access(name, agent)
        return web.json_response(ch.model_dump(mode="json"))
    except ValueError as e:
        return web.json_response({"error": str(e)}, status=404)


async def handle_get_unreads(request: web.Request) -> web.Response:
    # Unreads are local-only: read cursors are per-host, no sync needed.
    store: MessageStore = request.app["store"]
    participant = request.match_info["participant"]
    unreads = store.get_unreads(participant)
    return web.json_response(unreads)


async def handle_mark_read(request: web.Request) -> web.Response:
    store: MessageStore = request.app["store"]
    channel = request.match_info["name"]
    try:
        body = await request.json()
    except Exception:
        return web.json_response({"error": "invalid JSON body"}, status=400)
    store.mark_read(channel, body["participant"])
    return web.json_response({"status": "marked"})


async def handle_reset_cursors(request: web.Request) -> web.Response:
    """Roll back all channel cursors for a participant to a lookback window.

    POST /cursors/{participant}/reset
    Body (optional): {"lookback_seconds": 600}  — default 600 (10 minutes)

    Only moves cursors backward. Used by the body runtime on session start
    so messages posted just before a restart are not silently missed.
    """
    store: MessageStore = request.app["store"]
    participant = request.match_info["participant"]
    try:
        body = await request.json()
        lookback = int(body.get("lookback_seconds", 600))
    except Exception:
        lookback = 600
    before = datetime.now(timezone.utc) - timedelta(seconds=lookback)
    store.reset_cursors(participant, before)
    return web.json_response({"status": "reset", "before": before.isoformat()})


async def handle_search(request: web.Request) -> web.Response:
    store: MessageStore = request.app["store"]

    if request.app.get("mode") == "cache":
        return await _cache_search(request, store)

    query = request.query.get("q")
    if not query:
        return web.json_response({"error": "q parameter required"}, status=400)
    channel = request.query.get("channel")
    author = request.query.get("author")
    participant = request.query.get("participant")
    results = store.search_messages(
        query, channel=channel, author=author, participant=participant,
    )
    return web.json_response([m.model_dump(mode="json") for m in results])


async def _cache_search(request: web.Request, store: MessageStore) -> web.Response:
    """Cache mode: try upstream search, fall back to local FTS."""
    http = request.app["http"]
    upstream = request.app["upstream_url"]
    query = request.query.get("q")
    if not query:
        return web.json_response({"error": "q parameter required"}, status=400)

    try:
        resp = await http.get(f"{upstream}/search", params=dict(request.query))
        if resp.status_code == 200:
            request.app["upstream_state"]["ok"] = True
            return web.json_response(resp.json())
    except Exception:
        request.app["upstream_state"]["ok"] = False

    # Fall back to local FTS
    channel = request.query.get("channel")
    author = request.query.get("author")
    participant = request.query.get("participant")
    results = store.search_messages(query, channel=channel, author=author, participant=participant)
    return web.json_response([m.model_dump(mode="json") for m in results])


async def handle_deliver_task(request: web.Request) -> web.Response:
    """Deliver a task to an agent by writing to its session context file.

    This is the trusted intermediary endpoint — intake (external-facing)
    calls this on the internal mediation network to deliver tasks to agents.
    """
    try:
        body = await request.json()
    except Exception:
        return web.json_response({"error": "Invalid JSON"}, status=400)

    agent_name = body.get("agent_name")
    task_content = body.get("task_content")
    work_item_id = body.get("work_item_id", "")
    priority = body.get("priority", "normal")
    source = body.get("source", "")

    if not agent_name or not task_content:
        return web.json_response({"error": "agent_name and task_content required"}, status=400)

    agents_dir: Path = request.app["agents_dir"]
    state_dir = agents_dir / agent_name / "state"
    context_file = state_dir / "session-context.json"

    if not state_dir.exists():
        return web.json_response({"error": f"Agent '{agent_name}' not found"}, status=404)

    # Build task
    task_id = f"task-{datetime.now(timezone.utc).strftime('%Y%m%d')}-{uuid4().hex[:6]}"
    task = {
        "type": "task",
        "task_id": task_id,
        "content": task_content,
        "timestamp": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
        "operator": source,
        "work_item_id": work_item_id,
        "priority": priority,
        "stopping_conditions": [
            "sensitive_domain_detected",
            "unexpected_finding",
            "ambiguous_case",
        ],
    }

    # Propagate event_id from metadata if present (event-triggered activation)
    metadata = body.get("metadata", {})
    if metadata.get("event_id"):
        task["event_id"] = metadata["event_id"]

    # Update context file
    try:
        if context_file.exists():
            context = json.loads(context_file.read_text())
        else:
            context = {"session_id": "intake-delivery"}
        context["current_task"] = task
        context_file.write_text(json.dumps(context, indent=2))
    except Exception as e:
        return web.json_response({"error": f"Failed to write task: {e}"}, status=500)

    return web.json_response({"delivered": True, "task_id": task_id})


async def handle_register_interests(request: web.Request) -> web.Response:
    """Register task interests for an agent.

    POST /subscriptions/{agent_name}/interests
    Body: {"task_id": "...", "description": "...", "keywords": [...], "knowledge_filter": {...}}
    """
    agent_name = request.match_info["agent_name"]
    try:
        body = await request.json()
    except Exception:
        return web.json_response({"error": "invalid JSON body"}, status=400)

    try:
        declaration = InterestDeclaration.model_validate(body)
    except Exception as e:
        return web.json_response({"error": str(e)}, status=400)

    sub_manager: SubscriptionManager = request.app["sub_manager"]
    sub_manager.register(agent_name, declaration)
    return web.json_response({"registered": True})


async def handle_clear_interests(request: web.Request) -> web.Response:
    """Clear task interests for an agent.

    DELETE /subscriptions/{agent_name}/interests
    """
    agent_name = request.match_info["agent_name"]
    sub_manager: SubscriptionManager = request.app["sub_manager"]
    sub_manager.clear(agent_name)
    return web.json_response({"cleared": True})


async def handle_register_expertise(request: web.Request) -> web.Response:
    """Register expertise for an agent.

    POST /subscriptions/{agent_name}/expertise
    Body: {"tier": "base|standing|learned|task", "description": "...", "keywords": [...]}
    """
    agent_name = request.match_info["agent_name"]
    try:
        body = await request.json()
    except Exception:
        return web.json_response({"error": "invalid JSON body"}, status=400)

    from images.models.subscriptions import ExpertiseDeclaration
    try:
        declaration = ExpertiseDeclaration.model_validate(body)
    except Exception as e:
        return web.json_response({"error": str(e)}, status=400)

    sub_manager: SubscriptionManager = request.app["sub_manager"]
    sub_manager.register_expertise(agent_name, declaration)
    return web.json_response({"registered": True, "tier": declaration.tier.value})


async def handle_get_expertise(request: web.Request) -> web.Response:
    """Get merged expertise profile for an agent.

    GET /subscriptions/{agent_name}/expertise
    """
    agent_name = request.match_info["agent_name"]
    sub_manager: SubscriptionManager = request.app["sub_manager"]
    tiers = sub_manager.get_expertise(agent_name)
    merged_keywords = sub_manager.get_merged_keywords(agent_name)
    result = {
        "agent": agent_name,
        "keywords": merged_keywords,
        "tiers": {
            tier: decl.model_dump() for tier, decl in tiers.items()
        },
    }
    return web.json_response(result)


async def handle_clear_expertise(request: web.Request) -> web.Response:
    """Clear expertise for an agent.

    DELETE /subscriptions/{agent_name}/expertise?tier=task
    If tier is specified, only that tier is cleared. Otherwise all tiers are cleared.
    """
    agent_name = request.match_info["agent_name"]
    tier = request.query.get("tier")
    sub_manager: SubscriptionManager = request.app["sub_manager"]
    sub_manager.clear_expertise(agent_name, tier)
    return web.json_response({"cleared": True, "tier": tier or "all"})


async def handle_register_responsiveness(request: web.Request) -> web.Response:
    """Register channel responsiveness config for an agent.

    POST /subscriptions/{agent_name}/responsiveness
    Body: {"config": {"default": "mention-only", "general": "active", ...}}
    """
    agent_name = request.match_info["agent_name"]
    try:
        body = await request.json()
    except Exception:
        return web.json_response({"error": "invalid JSON body"}, status=400)

    config = body.get("config", body)  # Accept both {"config": {...}} and flat dict
    if not isinstance(config, dict):
        return web.json_response({"error": "config must be a dict"}, status=400)

    sub_manager: SubscriptionManager = request.app["sub_manager"]
    sub_manager.register_responsiveness(agent_name, config)
    return web.json_response({"registered": True, "agent": agent_name})


async def handle_deliver_task_v2(request: web.Request) -> web.Response:
    """Deliver a task to an agent via HTTP, with WebSocket push if connected.

    POST /tasks/{agent_name}
    Body: {"task_content": "...", "source": "...", "work_item_id": "...", "priority": "..."}

    Writes to session-context.json for durability AND pushes over WebSocket
    if the agent is currently connected.  Returns 404 if agent state dir
    doesn't exist.
    """
    agent_name = request.match_info["agent_name"]
    try:
        body = await request.json()
    except Exception:
        return web.json_response({"error": "invalid JSON body"}, status=400)

    task_content = body.get("task_content")
    if not task_content:
        return web.json_response({"error": "task_content required"}, status=400)

    work_item_id = body.get("work_item_id", "")
    priority = body.get("priority", "normal")
    source = body.get("source", "")

    agents_dir: Path = request.app["agents_dir"]
    state_dir = agents_dir / agent_name / "state"
    context_file = state_dir / "session-context.json"

    if not state_dir.exists():
        return web.json_response({"error": f"Agent '{agent_name}' not found"}, status=404)

    # Build task — same format as handle_deliver_task
    task_id = f"task-{datetime.now(timezone.utc).strftime('%Y%m%d')}-{uuid4().hex[:6]}"
    task = {
        "type": "task",
        "task_id": task_id,
        "content": task_content,
        "timestamp": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
        "operator": source,
        "work_item_id": work_item_id,
        "priority": priority,
        "stopping_conditions": [
            "sensitive_domain_detected",
            "unexpected_finding",
            "ambiguous_case",
        ],
    }

    # Propagate event_id from metadata if present (event-triggered activation)
    metadata = body.get("metadata", {})
    if metadata.get("event_id"):
        task["event_id"] = metadata["event_id"]

    # Write to context file for durability
    try:
        if context_file.exists():
            context = json.loads(context_file.read_text())
        else:
            context = {"session_id": "intake-delivery"}
        context["current_task"] = task
        context_file.write_text(json.dumps(context, indent=2))
    except Exception as e:
        return web.json_response({"error": f"Failed to write task: {e}"}, status=500)

    # Push over WebSocket if agent is connected
    pushed = False
    ws_registry = request.app.get("ws_registry")
    if ws_registry is not None:
        event = {
            "v": 1,
            "type": "task",
            "task": task,
        }
        pushed = await push_to_agent(ws_registry, agent_name, event)

    return web.json_response({"delivered": True, "task_id": task_id, "pushed": pushed})


async def handle_signal(request: web.Request) -> web.Response:
    """Broadcast an agent signal to system observers (gateway, operator).

    Body: {"agent": "name", "signal_type": "processing", "data": {...}}

    Signals are not channel messages — they flow to system observers only
    (the gateway relay and operator connections) for real-time UI updates
    like typing indicators and error notifications.
    """
    try:
        body = await request.json()
    except Exception:
        return web.json_response({"error": "invalid JSON"}, status=400)

    agent = body.get("agent", "")
    signal_type = body.get("signal_type", "")
    data = body.get("data", {})

    if not agent or not signal_type:
        return web.json_response({"error": "agent and signal_type required"}, status=400)

    registry = request.app["ws_registry"]

    event = {
        "type": f"agent_signal_{signal_type}",
        "agent": agent,
        "signal_type": signal_type,
        "data": data,
    }

    # Push to system observers only — not channel members
    for observer in ("_gateway", "_operator"):
        await push_to_agent(registry, observer, event)

    return web.json_response({"status": "ok"})


def main():
    parser = argparse.ArgumentParser(description="Agency comms server")
    parser.add_argument("--port", type=int, default=8080)
    parser.add_argument("--data-dir", type=str, default="/app/data")
    parser.add_argument("--agents-dir", type=str, default="/app/agents")
    args = parser.parse_args()

    # Logging configured automatically by sitecustomize.py via AGENCY_COMPONENT env var.
    app = create_app(data_dir=Path(args.data_dir), agents_dir=Path(args.agents_dir))
    logger.info("Starting comms server on port %d", args.port)
    web.run_app(app, host="0.0.0.0", port=args.port, print=None, access_log_class=_HealthFilterAccessLogger)
    logger.info("Comms server stopped")


if __name__ == "__main__":
    main()
