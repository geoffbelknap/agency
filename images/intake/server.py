"""Intake service — webhook receiver, routing, work item tracking."""

import argparse
import asyncio
import hashlib
import hmac
import json
import logging
import os
import signal
import socket
import time
from datetime import datetime, timezone
from pathlib import Path
from string import Template
from urllib.parse import parse_qs

import yaml
from aiohttp import web, ClientSession
from aiohttp.abc import AbstractAccessLogger

from typing import Optional, Union

from images.intake.gateway_client import GatewayClient


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
from images.models.connector import ConnectorConfig, ConnectorRelayTarget
from images.intake.router import evaluate_routes, render_template, parse_sla_duration
from images.intake.graph_ingest import evaluate_graph_ingest
from images.intake.correlation import EventBuffer
from images.intake.work_items import WorkItemStore
from images.intake.poller import PollStateStore, hash_blob, hash_items, extract_items, parse_interval, apply_transform
from images.intake.scheduler import ScheduleStateStore, should_fire
from images.intake.channel_watcher import ChannelWatchStateStore, matches_pattern
from images.intake.bridge_state import BridgeStateStore

logger = logging.getLogger("intake")


def _webhook_path_for_connector(connector: ConnectorConfig) -> str:
    """Return the inbound webhook path for a connector."""
    return connector.source.path or f"/webhooks/{connector.name}"


def _collapse_form_data(form_values: dict[str, list[str]]) -> dict:
    """Collapse parse_qs-style values into scalars when single-valued."""
    payload: dict[str, object] = {}
    for key, values in form_values.items():
        if len(values) == 1:
            payload[key] = values[0]
        else:
            payload[key] = values
    return payload


def _parse_webhook_payload(body_bytes: bytes, connector: ConnectorConfig) -> dict:
    """Parse a webhook request body according to the connector's configured body format."""
    body_format = connector.source.body_format or "json"

    if body_format == "json":
        payload = json.loads(body_bytes)
    elif body_format == "form_urlencoded":
        payload = _collapse_form_data(parse_qs(body_bytes.decode("utf-8", errors="replace"), keep_blank_values=True))
    elif body_format == "form_urlencoded_payload_json_field":
        form_data = _collapse_form_data(parse_qs(body_bytes.decode("utf-8", errors="replace"), keep_blank_values=True))
        payload_field = connector.source.payload_field or "payload"
        wrapped_payload = form_data.get(payload_field)
        if not isinstance(wrapped_payload, str) or not wrapped_payload:
            raise ValueError(f"Missing form field: {payload_field}")
        payload = json.loads(wrapped_payload)
    else:
        raise ValueError(f"Unsupported body format: {body_format}")

    if not isinstance(payload, dict):
        raise ValueError("Payload must be a JSON object")

    return payload


def _expand_route_target(target_value: str) -> str:
    """Expand ${ENV} placeholders in route target values."""
    return Template(target_value).safe_substitute(os.environ)


def _build_route_task_text(route, payload: dict) -> str:
    """Build the task text for an agent/team/mission route."""
    if route.brief:
        return render_template(route.brief, payload)
    return json.dumps(payload, default=str, indent=2)


def _build_channel_text(route, payload: dict, source_label: str) -> str:
    """Build the channel message text for a channel route."""
    if route.brief:
        return render_template(route.brief, payload)
    return _format_channel_message(payload, source_label)


def _build_bridge_metadata(connector_name: str, payload: dict) -> dict:
    """Build normalized bridge metadata for chat-oriented connectors."""
    metadata: dict[str, object] = {
        "connector_name": connector_name,
        "source_payload": payload,
    }

    if connector_name != "slack-events":
        return metadata

    event = payload.get("event")
    if not isinstance(event, dict):
        return metadata

    channel_id = event.get("channel")
    message_ts = event.get("ts")
    thread_ts = event.get("thread_ts") or message_ts
    user_id = event.get("user")
    team_id = payload.get("team_id")

    if not isinstance(channel_id, str) or not isinstance(thread_ts, str):
        return metadata

    metadata["bridge"] = {
        "platform": "slack",
        "workspace_id": team_id if isinstance(team_id, str) else None,
        "user_id": user_id if isinstance(user_id, str) else None,
        "channel_id": channel_id,
        "message_ts": message_ts if isinstance(message_ts, str) else None,
        "thread_ts": thread_ts,
        "root_ts": thread_ts,
        "conversation_key": f"slack:{channel_id}:{thread_ts}",
        "conversation_kind": "dm" if channel_id.startswith("D") else "thread",
    }
    metadata["principal"] = {
        "platform": "slack",
        "workspace_id": team_id if isinstance(team_id, str) else None,
        "user_id": user_id if isinstance(user_id, str) else None,
        "channel_id": channel_id,
        "conversation_key": f"slack:{channel_id}:{thread_ts}",
        "is_dm": channel_id.startswith("D"),
    }
    return metadata


def _apply_bridge_state(
    bridge_state: Optional[BridgeStateStore],
    bridge_metadata: dict,
    target_type: str,
    target_name: str,
) -> tuple[dict, str]:
    """Enrich bridge metadata and routing from existing bridge state."""
    if bridge_state is None:
        return bridge_metadata, target_name

    bridge = bridge_metadata.get("bridge")
    if not isinstance(bridge, dict):
        return bridge_metadata, target_name

    conversation_key = bridge.get("conversation_key")
    if not isinstance(conversation_key, str) or not conversation_key:
        return bridge_metadata, target_name

    existing = bridge_state.get_conversation(conversation_key)
    if not existing:
        return bridge_metadata, target_name

    bridge["known"] = True
    if isinstance(existing.get("target_agent"), str) and existing["target_agent"]:
        bridge["target_agent"] = existing["target_agent"]
        if target_type != "channel":
            target_name = existing["target_agent"]

    principal = bridge_metadata.get("principal")
    if isinstance(principal, dict):
        principal["known"] = True

    return bridge_metadata, target_name


def _connector_from_request(request: web.Request) -> tuple[Optional[str], Optional[ConnectorConfig]]:
    """Resolve the target connector from a request path."""
    connectors = request.app["connectors"]
    connector_name = request.match_info.get("connector_name")
    if connector_name:
        connector = connectors.get(connector_name)
        if connector is None:
            return connector_name, None
        if _webhook_path_for_connector(connector) != request.path:
            return connector_name, None
        return connector_name, connector

    connector_paths: dict[str, str] = request.app.get("connector_paths", {})
    resolved_name = connector_paths.get(request.path)
    if not resolved_name:
        return None, None
    return resolved_name, connectors.get(resolved_name)


def _webhook_success_response(connector: ConnectorConfig, delivered: bool) -> web.Response:
    """Build the success response for a webhook connector."""
    status = connector.source.response_status or 202
    body = connector.source.response_body
    content_type = connector.source.response_content_type or "application/json"

    if body is None:
        return web.json_response({"status": "ok", "delivered": delivered}, status=status)

    return web.Response(status=status, text=body, content_type=content_type)


def _load_connectors(connectors_dir: Path) -> dict[str, ConnectorConfig]:
    """Load all connector YAML files from directory.

    Supports both flat layout (name.yaml) and hub-install subdir layout
    (name/connector.yaml).
    """
    connectors: dict[str, ConnectorConfig] = {}
    if not connectors_dir.exists():
        return connectors
    paths = list(connectors_dir.glob("*.yaml")) + list(connectors_dir.glob("*/connector.yaml"))
    for path in paths:
        try:
            data = yaml.safe_load(path.read_text())
            if not isinstance(data, dict) or data.get("kind") != "connector":
                continue
            config = ConnectorConfig.model_validate(data)
            connectors[config.name] = config
        except Exception as e:
            logger.warning(f"Failed to load connector {path}: {e}")
    return connectors


async def _deliver_task(
    gateway: GatewayClient,
    agent_name: str,
    task_content: str,
    work_item_id: str,
    priority: str,
    source: str,
    metadata: Optional[dict] = None,
) -> bool:
    """Deliver task to agent via gateway event bus."""
    await gateway.publish_event(
        source_name="intake",
        event_type="work_item_created",
        data={
            "agent_name": agent_name,
            "task_content": task_content,
            "work_item_id": work_item_id,
            "priority": priority,
            "source": source,
        },
        metadata=metadata,
    )
    return True


async def _deliver_to_channel(
    gateway: GatewayClient,
    channel_name: str,
    content: str,
    source: str,
) -> bool:
    """Post a message to a comms channel via gateway."""
    result = await gateway.post_channel_message(channel_name, content, source)
    return result is not None


def _make_ssl_context():
    """Build an SSL context that trusts the egress (mitmproxy) CA cert."""
    import ssl
    ctx = ssl.create_default_context()
    ca_cert = os.environ.get("EGRESS_CA_CERT")
    if ca_cert and Path(ca_cert).exists():
        ctx.load_verify_locations(ca_cert)
    return ctx


async def _fetch_url(url: str, method: str = "GET", headers: Optional[dict] = None) -> Union[dict, list, None]:
    """Fetch a URL and return parsed JSON.

    Routes through HTTPS_PROXY / HTTP_PROXY env vars when set (intake has
    no direct internet access on the mediation network).
    """
    proxy = os.environ.get("HTTPS_PROXY") if url.startswith("https://") else os.environ.get("HTTP_PROXY")
    ssl_ctx = _make_ssl_context() if url.startswith("https://") else None
    async with ClientSession() as session:
        try:
            async with session.request(method, url, headers=headers, proxy=proxy, ssl=ssl_ctx) as resp:
                if resp.status == 200:
                    return await resp.json()
                logger.warning(f"Poll fetch {url} returned {resp.status}")
                return None
        except Exception as e:
            logger.warning(f"Poll fetch {url} failed: {e}")
            return None


async def _fetch_channel_messages(
    gateway: GatewayClient, channel: str, since: Optional[str] = None
) -> list[dict]:
    """Fetch messages from comms channel via gateway."""
    return await gateway.get_channel_messages(channel, since=since)


async def _execute_relay(relay: ConnectorRelayTarget, payload: dict, connector_name: str) -> bool:
    """Execute a relay action: render body template and POST to a URL directly.

    No agent is spawned. Used for comms→Slack mirroring, webhook forwarding, etc.
    Routes through egress proxy when HTTPS_PROXY / HTTP_PROXY env vars are set.
    """
    def _prune_empty_json(value):
        if isinstance(value, dict):
            pruned = {}
            for k, v in value.items():
                pv = _prune_empty_json(v)
                if pv is None:
                    continue
                pruned[k] = pv
            return pruned
        if isinstance(value, list):
            return [pv for item in value if (pv := _prune_empty_json(item)) is not None]
        if value == "" or value is None:
            return None
        return value

    env = os.environ
    url = Template(relay.url).safe_substitute(env)
    headers = {k: Template(v).safe_substitute(env) for k, v in (relay.headers or {}).items()}
    if "Content-Type" not in headers:
        headers["Content-Type"] = relay.content_type

    # Render body: expand ${ENV} first, then Jinja2 payload fields
    body_str = Template(relay.body).safe_substitute(env)
    body_str = render_template(body_str, payload)

    proxy = os.environ.get("HTTPS_PROXY") if url.startswith("https://") else os.environ.get("HTTP_PROXY")
    ssl_ctx = _make_ssl_context() if url.startswith("https://") else None

    async with ClientSession() as session:
        try:
            request_kwargs = {
                "headers": headers,
                "proxy": proxy,
                "ssl": ssl_ctx,
            }
            if relay.content_type == "application/json":
                try:
                    body_obj = json.loads(body_str)
                    request_kwargs["json"] = _prune_empty_json(body_obj)
                except json.JSONDecodeError:
                    request_kwargs["data"] = body_str
            else:
                request_kwargs["data"] = body_str
            async with session.request(
                relay.method, url, **request_kwargs
            ) as resp:
                if resp.status in (200, 201, 202, 204):
                    return True
                logger.warning(f"Relay {connector_name}: {url} returned {resp.status}: {await resp.text()}")
                return False
        except Exception as e:
            logger.error(f"Relay {connector_name}: {e}")
            return False


async def _route_and_deliver(
    connector_name: str,
    connector_config,
    payload: dict,
    store: WorkItemStore,
    gateway: GatewayClient,
    bridge_state: Optional[BridgeStateStore],
    source_label: str,
    knowledge_url: Optional[str] = None,
    event_buffer: Optional[EventBuffer] = None,
) -> bool:
    """Shared routing and delivery logic for all source types."""
    # Record in event buffer before routing (enables cross-source correlation)
    if event_buffer is not None:
        event_buffer.record(connector_name, payload)

    # Graph ingest runs regardless of routing — knowledge writes are independent
    if knowledge_url and connector_config.graph_ingest:
        try:
            evaluate_graph_ingest(
                connector_config.graph_ingest,
                payload,
                knowledge_url,
                connector_name,
                f"payload:{connector_name}",
                event_buffer=event_buffer,
            )
        except Exception as e:
            logger.warning("graph_ingest failed for %s: %s", connector_name, e)

    result = evaluate_routes(connector_config.routes, payload)
    if result is None:
        # No route match — don't create a work item for graph-ingest-only payloads
        return False

    route_index, route = result

    # Create work item only for routed payloads
    wi = store.create(connector=connector_name, payload=payload)

    # Relay route: make a direct HTTP call, no agent spawned
    if route.relay:
        store.update_status(wi.id, status="routed", route_index=route_index, priority=route.priority)
        ok = await _execute_relay(route.relay, payload, connector_name)
        store.update_status(wi.id, status="relayed" if ok else "relay_failed")
        return ok

    # Determine target type and name from route target dict
    target = route.target
    if "channel" in target:
        target_type = "channel"
        target_name = _expand_route_target(target["channel"])
    elif "mission" in target:
        target_type = "mission"
        target_name = _expand_route_target(target["mission"])
    elif "team" in target:
        target_type = "team"
        target_name = _expand_route_target(target["team"])
    else:
        target_type = "agent"
        target_name = _expand_route_target(target.get("agent"))

    sla_delta = parse_sla_duration(route.sla)
    sla_deadline = (datetime.now(timezone.utc) + sla_delta) if sla_delta else None

    store.update_status(
        wi.id,
        status="routed",
        target_type=target_type,
        target_name=target_name,
        route_index=route_index,
        priority=route.priority,
        sla_deadline=sla_deadline,
    )

    # Build task content from route brief or raw payload
    task_text = _build_route_task_text(route, payload)
    bridge_metadata = _build_bridge_metadata(connector_name, payload)
    bridge_metadata, target_name = _apply_bridge_state(bridge_state, bridge_metadata, target_type, target_name)

    # Channel routes: post formatted summary to comms channel
    if target_type == "channel":
        channel_text = _build_channel_text(route, payload, source_label)
        delivered = await _deliver_to_channel(
            gateway=gateway,
            channel_name=target_name,
            content=channel_text,
            source=source_label,
        )
    else:
        # Agent, team, or mission routes: deliver as agent task
        deliver_to = target_name
        delivered = await _deliver_task(
            gateway=gateway,
            agent_name=deliver_to,
            task_content=task_text,
            work_item_id=wi.id,
            priority=route.priority,
            source=source_label,
            metadata=bridge_metadata,
        )
    if delivered:
        store.update_status(wi.id, status="assigned", task_content=task_text)
        bridge = bridge_metadata.get("bridge")
        if bridge_state is not None and isinstance(bridge, dict):
            conversation_key = bridge.get("conversation_key")
            if isinstance(conversation_key, str) and conversation_key:
                bridge_state.upsert_conversation(
                    conversation_key,
                    platform=str(bridge.get("platform") or ""),
                    workspace_id=bridge.get("workspace_id") if isinstance(bridge.get("workspace_id"), str) else None,
                    channel_id=bridge.get("channel_id") if isinstance(bridge.get("channel_id"), str) else None,
                    root_ts=bridge.get("root_ts") if isinstance(bridge.get("root_ts"), str) else None,
                    thread_ts=bridge.get("thread_ts") if isinstance(bridge.get("thread_ts"), str) else None,
                    conversation_kind=bridge.get("conversation_kind") if isinstance(bridge.get("conversation_kind"), str) else None,
                    user_id=bridge.get("user_id") if isinstance(bridge.get("user_id"), str) else None,
                    target_agent=deliver_to if target_type != "channel" else None,
                    connector_name=connector_name,
                    metadata=bridge_metadata,
                )

    return delivered


def _format_channel_message(payload: dict, source: str) -> str:
    """Render a detection/event payload as a human-readable channel message.

    Extracts common fields from connector payloads and produces a compact
    summary. Falls back to truncated JSON for unknown structures.
    """
    lines = []

    # Detection title — try common field paths
    cat = payload.get("cat", "")
    title = (
        payload.get("detect_mtd", {}).get("description")
        or payload.get("description")
        or cat
        or "Event"
    )
    level = (
        payload.get("detect_mtd", {}).get("level")
        or payload.get("level")
        or payload.get("severity")
        or ""
    )
    level_badge = f" [{level.upper()}]" if level else ""
    lines.append(f"**{title}**{level_badge}")

    # Source / host
    routing = payload.get("detect", {}).get("routing", payload.get("routing", {}))
    hostname = routing.get("hostname") or payload.get("hostname", "")
    ext_ip = routing.get("ext_ip") or payload.get("ext_ip", "")
    if hostname:
        host_parts = [hostname]
        if ext_ip:
            host_parts.append(ext_ip)
        lines.append(f"Host: {' / '.join(host_parts)}")

    # Key evidence fields
    event = payload.get("detect", {}).get("event", payload.get("event", {}))
    if isinstance(event, dict):
        cmd = event.get("COMMAND_LINE") or event.get("command_line", "")
        file_path = event.get("FILE_PATH") or event.get("file_path", "")
        if cmd:
            cmd_display = cmd if len(cmd) <= 120 else cmd[:117] + "..."
            lines.append(f"Command: `{cmd_display}`")
        elif file_path:
            lines.append(f"File: `{file_path}`")

    # Tags / MITRE
    tags = payload.get("rule_tags", payload.get("tags", []))
    if isinstance(tags, list) and tags:
        mitre = [t for t in tags if t.startswith("attack.")]
        if mitre:
            lines.append(f"MITRE: {', '.join(mitre)}")

    # Link
    link = payload.get("link", "")
    if link:
        lines.append(f"[View in console]({link})")

    # Source rule
    rule = payload.get("source_rule", "")
    if rule:
        lines.append(f"Rule: `{rule}`")

    # Timestamp
    ts = payload.get("ts")
    if ts:
        try:
            from datetime import datetime as _dt
            if isinstance(ts, (int, float)):
                ts_val = ts / 1000 if ts > 1e12 else ts
                dt = _dt.utcfromtimestamp(ts_val)
                lines.append(f"Time: {dt.strftime('%Y-%m-%d %H:%M:%S UTC')}")
        except Exception:
            pass

    if len(lines) <= 1:
        # Fallback: compact JSON
        compact = json.dumps(payload, default=str, separators=(",", ":"))
        if len(compact) > 500:
            compact = compact[:497] + "..."
        lines.append(compact)

    return "\n".join(lines)


def _format_url(template: str, item: dict) -> str:
    """Substitute {field} placeholders from item into a URL template.

    Uses safe substitution — missing keys are left as-is.
    ${ENV} placeholders should already be expanded before calling this.
    """
    import string

    class SafeDict(dict):
        def __missing__(self, key):
            return "{" + key + "}"

    return template.format_map(SafeDict(item))


async def _poll_once(
    connector,
    store: WorkItemStore,
    poll_state: PollStateStore,
    gateway: GatewayClient,
    knowledge_url: Optional[str] = None,
    event_buffer: Optional[EventBuffer] = None,
) -> int:
    """Execute one poll tick for a connector. Returns count of new work items."""
    # Expand ${VAR} placeholders in URL and headers using env vars.
    # This lets connectors reference service keys like ${SLACK_BOT_TOKEN}
    # without embedding them in the connector YAML.
    env = os.environ
    url = Template(connector.source.url).safe_substitute(env)

    # Substitute built-in poll variables: {_poll_start}, {_poll_end}
    # These provide unix timestamps for APIs that require time-bounded queries
    # (e.g. LimaCharlie Insight detections API).
    import time as _time
    poll_end = int(_time.time())
    interval_str = connector.source.interval or "2m"
    poll_start = poll_end - parse_interval(interval_str)
    url = url.replace("{_poll_start}", str(poll_start)).replace("{_poll_end}", str(poll_end))
    # Headers passed as-is. Secret credentials are injected by the egress
    # proxy via domain matching (credential-swaps.yaml), not by intake.
    headers = dict(connector.source.headers) if connector.source.headers else None
    data = await _fetch_url(url, method=connector.source.method, headers=headers)
    if data is None:
        poll_state.increment_failure_count(connector.name)
        count = poll_state.get_failure_count(connector.name)
        if count >= 3:
            logger.error(f"Poll {connector.name}: {count} consecutive failures")
        return 0

    poll_state.reset_failure_count(connector.name)
    old_hashes = poll_state.get_hashes(connector.name)

    if connector.source.response_key:
        items = extract_items(data, connector.source.response_key)
        if items is None:
            logger.warning(f"Poll {connector.name}: response_key '{connector.source.response_key}' did not extract a list")
            return 0
        if connector.source.transform:
            items = apply_transform(items, connector.source.transform)
        dedup_key = connector.source.dedup_key
        if dedup_key:
            new_hashes_list = [hash_blob({dedup_key: item.get(dedup_key)}) if isinstance(item, dict) else hash_blob(item) for item in items]
        else:
            new_hashes_list = hash_items(items)
        new_hashes = set(new_hashes_list)
        new_items = [
            item for item, h in zip(items, new_hashes_list) if h not in old_hashes
        ]
        poll_state.set_hashes(connector.name, new_hashes)
    else:
        blob_hash = hash_blob(data)
        new_hashes = {blob_hash}
        new_items = [data] if blob_hash not in old_hashes else []
        poll_state.set_hashes(connector.name, new_hashes)

    created = 0
    for item in new_items:
        payload = item if isinstance(item, dict) else {"data": item}
        await _route_and_deliver(
            connector.name, connector, payload, store, gateway,
            source_label=f"poll:{connector.name}",
            knowledge_url=knowledge_url,
            event_buffer=event_buffer,
        )
        created += 1

    # Follow-up: fetch per-item URLs for nested items (e.g. Slack thread replies)
    follow_up = connector.source.follow_up
    if follow_up and connector.source.response_key:
        # Check ALL items (not just new ones) for follow-up — reply_count can grow on old messages.
        # We deduplicate replies by their own dedup_key in a per-thread namespace.
        all_items = items  # already extracted above
        for parent in all_items:
            if not isinstance(parent, dict):
                continue
            # Only follow up when the condition field is truthy/non-zero
            if follow_up.when and not parent.get(follow_up.when):
                continue

            # Expand env vars first, then item fields
            fu_url_env = Template(follow_up.url).safe_substitute(env)
            fu_url = _format_url(fu_url_env, parent)

            fu_data = await _fetch_url(fu_url, method="GET", headers=headers)
            if fu_data is None:
                continue

            if follow_up.response_key:
                fu_items = extract_items(fu_data, follow_up.response_key)
            else:
                fu_items = fu_data if isinstance(fu_data, list) else None

            if not fu_items:
                continue

            if follow_up.skip_first:
                fu_items = fu_items[1:]

            # Deduplicate follow-up items per parent using a namespaced key.
            # Use the parent's dedup_key value (or hash) as part of the namespace.
            parent_id = str(parent.get(connector.source.dedup_key, hash_blob(parent)))
            fu_namespace = f"{connector.name}:fu:{parent_id}"
            fu_old_hashes = poll_state.get_hashes(fu_namespace)

            fu_dedup_key = follow_up.dedup_key
            if fu_dedup_key:
                fu_hashes_list = [
                    hash_blob({fu_dedup_key: fi.get(fu_dedup_key)}) if isinstance(fi, dict) else hash_blob(fi)
                    for fi in fu_items
                ]
            else:
                fu_hashes_list = hash_items(fu_items)

            fu_new_hashes = set(fu_hashes_list)
            fu_new_items = [fi for fi, h in zip(fu_items, fu_hashes_list) if h not in fu_old_hashes]
            poll_state.set_hashes(fu_namespace, fu_new_hashes)

            for fu_item in fu_new_items:
                fu_payload = fu_item if isinstance(fu_item, dict) else {"data": fu_item}
                await _route_and_deliver(
                    connector.name, connector, fu_payload, store, gateway,
                    source_label=f"poll:{connector.name}:reply",
                    knowledge_url=knowledge_url,
                    event_buffer=event_buffer,
                )
                created += 1

    return created


async def _schedule_once(
    connectors: dict,
    store: WorkItemStore,
    schedule_state: ScheduleStateStore,
    gateway: GatewayClient,
    poll_state: Optional[PollStateStore] = None,
    knowledge_url: Optional[str] = None,
    event_buffer: Optional[EventBuffer] = None,
) -> int:
    """Check all schedule connectors and cron-triggered poll connectors. Returns fire count."""
    fired = 0
    for name, connector in connectors.items():
        if connector.source.type != "schedule":
            continue
        last_fired = schedule_state.get_last_fired(name)
        if not should_fire(connector.source.cron, last_fired):
            continue

        now = datetime.now(timezone.utc)
        payload = {"now": now.isoformat(), "schedule_name": name, "triggered_at": now.isoformat(), "connector": name}
        schedule_state.set_last_fired(name, now)

        await _route_and_deliver(
            name, connector, payload, store, gateway,
            source_label=f"schedule:{name}",
            knowledge_url=knowledge_url,
            event_buffer=event_buffer,
        )
        fired += 1

    # Cron-triggered poll sources
    if poll_state is not None:
        for name, connector in connectors.items():
            if connector.source.type != "poll" or not connector.source.cron:
                continue
            if not should_fire(connector.source.cron, schedule_state.get_last_fired(name)):
                continue
            try:
                created = await _poll_once(connector, store, poll_state, gateway, knowledge_url=knowledge_url, event_buffer=event_buffer)
                schedule_state.set_last_fired(name, datetime.now(timezone.utc))
                if created:
                    logger.info(f"Cron-poll {name}: created {created} work items")
                fired += 1
            except Exception as e:
                logger.error(f"Cron-poll {name} failed: {e}")

    return fired


async def _channel_watch_once(
    connector,
    store: WorkItemStore,
    watch_state: ChannelWatchStateStore,
    gateway: GatewayClient,
    knowledge_url: Optional[str] = None,
    event_buffer: Optional[EventBuffer] = None,
) -> int:
    """Check one channel-watch connector for new matching messages. Returns match count."""
    last_seen = watch_state.get_last_seen(connector.name)
    since = last_seen.isoformat() if last_seen else None
    watched_channel = Template(connector.source.channel).safe_substitute(os.environ)

    messages = await _fetch_channel_messages(gateway, watched_channel, since=since)
    if not messages:
        return 0

    created = 0
    latest_ts = last_seen
    for msg in messages:
        msg_ts = datetime.fromisoformat(msg["timestamp"])
        if last_seen and msg_ts <= last_seen:
            continue
        if latest_ts is None or msg_ts > latest_ts:
            latest_ts = msg_ts
        if not matches_pattern(msg.get("content", ""), connector.source.pattern):
            continue
        payload = {
            "channel": watched_channel,
            "content": msg.get("content", ""),
            "author": msg.get("author", msg.get("sender", "")),
            "message_id": msg.get("id", ""),
            "timestamp": msg["timestamp"],
            "reply_to": msg.get("reply_to"),
            "metadata": msg.get("metadata", {}),
            "flags": msg.get("flags", {}),
        }
        await _route_and_deliver(
            connector.name, connector, payload, store, gateway,
            source_label=f"channel-watch:{connector.name}",
            knowledge_url=knowledge_url,
            event_buffer=event_buffer,
        )
        created += 1

    if latest_ts and latest_ts != last_seen:
        watch_state.set_last_seen(connector.name, latest_ts)
    return created


def _egress_healthy() -> bool:
    """Check if the egress proxy is reachable via TCP connect."""
    proxy = os.environ.get("HTTPS_PROXY", os.environ.get("HTTP_PROXY", ""))
    if not proxy:
        return True  # No proxy configured — skip check
    # Parse host:port from proxy URL (e.g., "http://egress:3128")
    host_port = proxy.split("://", 1)[-1].rstrip("/")
    host, _, port_str = host_port.partition(":")
    port = int(port_str) if port_str else 3128
    try:
        sock = socket.create_connection((host, port), timeout=2)
        sock.close()
        return True
    except (OSError, socket.timeout):
        return False


_egress_warned = False


async def _poll_loop(app: web.Application) -> None:
    """Background task: poll all poll-type connectors on their intervals."""
    global _egress_warned
    poll_state = PollStateStore(app["store"].data_dir)
    last_poll: dict[str, float] = {}

    while True:
        # Gate: skip all polls while egress is unreachable
        if not _egress_healthy():
            if not _egress_warned:
                logger.warning("Egress proxy unreachable — skipping polls until healthy")
                _egress_warned = True
            await asyncio.sleep(10)
            continue
        if _egress_warned:
            logger.info("Egress proxy recovered — resuming polls")
            _egress_warned = False

        try:
            connectors = app["connectors"]
            for name, connector in connectors.items():
                if connector.source.type != "poll":
                    continue
                interval_secs = parse_interval(connector.source.interval)
                if name not in last_poll:
                    last_poll[name] = 0

                now = time.monotonic()
                if now - last_poll[name] < interval_secs:
                    continue

                last_poll[name] = now
                try:
                    created = await _poll_once(connector, app["store"], poll_state, app["gateway"], knowledge_url=app.get("knowledge_url"), event_buffer=app.get("event_buffer"))
                    if created:
                        logger.info(f"Poll {name}: created {created} work items")
                except Exception as e:
                    logger.error(f"Poll {name} error: {e}")
        except Exception as e:
            logger.error(f"Poll loop error: {e}")

        await asyncio.sleep(10)


async def _schedule_loop(app: web.Application) -> None:
    """Background task: check schedule connectors and cron-triggered poll connectors every 60 seconds."""
    schedule_state = ScheduleStateStore(app["store"].data_dir)
    poll_state = PollStateStore(app["store"].data_dir)

    while True:
        if not _egress_healthy():
            await asyncio.sleep(60)
            continue

        try:
            fired = await _schedule_once(app["connectors"], app["store"], schedule_state, app["gateway"], poll_state=poll_state, knowledge_url=app.get("knowledge_url"), event_buffer=app.get("event_buffer"))
            if fired:
                logger.info(f"Schedule: fired {fired} connectors")
        except Exception as e:
            logger.error(f"Schedule loop error: {e}")

        await asyncio.sleep(60)


async def _channel_watch_loop(app: web.Application) -> None:
    """Background task: check channel-watch connectors every 30 seconds."""
    watch_state = ChannelWatchStateStore(app["store"].data_dir)

    while True:
        try:
            connectors = app["connectors"]
            for name, connector in connectors.items():
                if connector.source.type != "channel-watch":
                    continue
                try:
                    created = await _channel_watch_once(connector, app["store"], watch_state, app["gateway"], knowledge_url=app.get("knowledge_url"), event_buffer=app.get("event_buffer"))
                    if created:
                        logger.info(f"Channel-watch {name}: created {created} work items")
                except Exception as e:
                    logger.error(f"Channel-watch {name} error: {e}")
        except Exception as e:
            logger.error(f"Channel-watch loop error: {e}")

        await asyncio.sleep(30)


async def _start_background_tasks(app: web.Application) -> None:
    """Start background source loops."""
    app["_poll_task"] = asyncio.create_task(_poll_loop(app))
    app["_schedule_task"] = asyncio.create_task(_schedule_loop(app))
    app["_channel_watch_task"] = asyncio.create_task(_channel_watch_loop(app))


async def _cleanup_background_tasks(app: web.Application) -> None:
    """Cancel background tasks on shutdown."""
    for key in ("_poll_task", "_schedule_task", "_channel_watch_task"):
        task = app.get(key)
        if task:
            task.cancel()
            try:
                await task
            except asyncio.CancelledError:
                pass


async def handle_health(request: web.Request) -> web.Response:
    connectors = request.app["connectors"]
    return web.json_response({
        "status": "ok",
        "connectors_loaded": len(connectors),
    })


async def handle_stats(request: web.Request) -> web.Response:
    store: WorkItemStore = request.app["store"]
    connector = request.query.get("connector")
    return web.json_response(store.stats(connector=connector))


async def handle_poll_health(request: web.Request) -> web.Response:
    """GET /poll-health — per-connector poll success/failure status."""
    from images.intake.poller import PollStateStore
    poll_state = PollStateStore(request.app["store"].data_dir)
    connectors = request.app.get("connectors", {})

    results = {}
    for name, connector in connectors.items():
        if connector.source.type != "poll":
            continue
        failures = poll_state.get_failure_count(name)
        results[name] = {
            "type": "poll",
            "interval": connector.source.interval,
            "consecutive_failures": failures,
            "status": "failing" if failures >= 3 else "ok",
        }

    return web.json_response({"connectors": results})


async def handle_poll_trigger(request: web.Request) -> web.Response:
    """POST /poll/{connector_name} — trigger an immediate poll for a connector."""
    from images.intake.poller import PollStateStore
    connector_name = request.match_info["connector_name"]
    connectors = request.app.get("connectors", {})
    connector = connectors.get(connector_name)
    if connector is None:
        return web.json_response({"error": f"connector {connector_name!r} not found"}, status=404)
    if connector.source.type != "poll":
        return web.json_response({"error": f"{connector_name} is not a poll connector"}, status=400)

    store: WorkItemStore = request.app["store"]
    poll_state = PollStateStore(store.data_dir)
    gateway = request.app["gateway"]
    knowledge_url = request.app.get("knowledge_url")
    event_buffer = request.app.get("event_buffer")

    try:
        created = await _poll_once(connector, store, poll_state, gateway,
                                   knowledge_url=knowledge_url, event_buffer=event_buffer)
        return web.json_response({"connector": connector_name, "work_items_created": created})
    except Exception as e:
        logger.error(f"Manual poll trigger for {connector_name} failed: {e}")
        return web.json_response({"error": str(e)}, status=500)


async def handle_items(request: web.Request) -> web.Response:
    store: WorkItemStore = request.app["store"]
    connector = request.query.get("connector")
    status = request.query.get("status")
    sla_breached = request.query.get("sla_breached", "").lower() in ("1", "true")
    limit = int(request.query.get("limit", "50"))

    if sla_breached:
        items = store.list_sla_breached()
    else:
        items = store.list_items(connector=connector, status=status, limit=limit)

    return web.json_response([
        {
            "id": item.id,
            "connector": item.connector,
            "status": item.status,
            "target_type": item.target_type,
            "target_name": item.target_name,
            "priority": item.priority,
            "sla_deadline": item.sla_deadline.isoformat() if item.sla_deadline else None,
            "created_at": item.created_at.isoformat() if item.created_at else None,
        }
        for item in items
    ])


def _verify_webhook_auth(request: web.Request, body_bytes: bytes, connector) -> Optional[web.Response]:
    """Verify HMAC-SHA256 webhook signature. Returns an error Response on failure, None on success."""
    auth = connector.source.webhook_auth
    if not auth:
        # No auth configured — check if enforcement is required
        if os.environ.get("AGENCY_INTAKE_REQUIRE_AUTH", "").lower() in ("1", "true", "yes"):
            logger.warning(
                "Webhook rejected: no auth configured for connector '%s' and AGENCY_INTAKE_REQUIRE_AUTH is set",
                connector.name,
            )
            return web.json_response(
                {"error": "Webhook authentication required but not configured for this connector"},
                status=403,
            )
        return None

    secret = os.environ.get(auth.secret_env, "")
    if not secret:
        logger.warning("Webhook auth configured but required secret env var is not set")
        return web.json_response({"error": "Webhook auth misconfigured"}, status=500)

    sig_header = request.headers.get(auth.header, "")

    if auth.timestamp_header:
        ts = request.headers.get(auth.timestamp_header, "")
        try:
            age = abs(time.time() - int(ts))
            if age > 300:
                return web.json_response({"error": "Request timestamp too old"}, status=401)
        except (ValueError, TypeError):
            return web.json_response({"error": "Invalid or missing timestamp header"}, status=401)
        sig_base = f"v0:{ts}:{body_bytes.decode('utf-8', errors='replace')}"
    else:
        sig_base = body_bytes.decode("utf-8", errors="replace")

    computed = auth.prefix + hmac.new(
        secret.encode(), sig_base.encode(), hashlib.sha256
    ).hexdigest()

    if not hmac.compare_digest(computed, sig_header):
        logger.warning(f"Webhook auth failed: signature mismatch")
        return web.json_response({"error": "Invalid signature"}, status=401)

    return None


async def handle_webhook(request: web.Request) -> web.Response:
    connector_name, connector = _connector_from_request(request)
    store: WorkItemStore = request.app["store"]

    # Find connector
    if connector is None:
        if connector_name:
            return web.json_response({"error": f"Unknown connector: {connector_name}"}, status=404)
        return web.json_response({"error": "Unknown webhook path"}, status=404)

    # Read raw body once (needed for HMAC verification before JSON parsing)
    body_bytes = await request.read()

    # Webhook auth verification (HMAC-SHA256, e.g. Slack Events API)
    if connector.source.webhook_auth:
        auth = connector.source.webhook_auth

        # Handle URL verification challenge before signature check
        # (Slack sends the challenge as plain JSON without a valid signature on first contact)
        if auth.challenge_field:
            try:
                pre_body = _parse_webhook_payload(body_bytes, connector)
                if isinstance(pre_body, dict) and auth.challenge_field in pre_body:
                    logger.info(f"Webhook {connector_name}: responding to challenge handshake")
                    return web.json_response({auth.challenge_field: pre_body[auth.challenge_field]})
            except Exception:
                pass

        err = _verify_webhook_auth(request, body_bytes, connector)
        if err:
            return err

    # Parse body according to connector source format
    try:
        payload = _parse_webhook_payload(body_bytes, connector)
    except ValueError as exc:
        return web.json_response({"error": str(exc)}, status=400)
    except Exception:
        return web.json_response({"error": "Invalid request body"}, status=400)

    # Validate against schema (if defined)
    if connector.source.payload_schema:
        required = connector.source.payload_schema.get("required", [])
        for field in required:
            if field not in payload:
                return web.json_response(
                    {"error": f"Missing required field: {field}"}, status=400
                )

    # Check rate limits
    if store.count_per_hour(connector_name) >= connector.rate_limits.max_per_hour:
        logger.warning(f"Rate limit exceeded for {connector_name} (max_per_hour)")
        return web.json_response({"error": "Rate limit exceeded"}, status=429)

    if store.count_concurrent(connector_name) >= connector.rate_limits.max_concurrent:
        logger.warning(f"Rate limit exceeded for {connector_name} (max_concurrent)")
        return web.json_response({"error": "Rate limit exceeded (concurrent)"}, status=429)

    # Route and deliver (handles both agent/team and relay targets)
    gateway = request.app["gateway"]
    bridge_state = request.app.get("bridge_state")
    knowledge_url = request.app.get("knowledge_url")
    event_buffer = request.app.get("event_buffer")
    delivered = await _route_and_deliver(
        connector_name, connector, payload, store, gateway, bridge_state,
        source_label=f"connector:{connector_name}",
        knowledge_url=knowledge_url,
        event_buffer=event_buffer,
    )
    return _webhook_success_response(connector, delivered)



def create_app(
    connectors_dir: Optional[Path] = None,
    data_dir: Optional[Path] = None,
    gateway_url: str = "http://gateway:8200",
    gateway_token: str = "",
) -> web.Application:
    from logging_config import correlation_middleware
    app = web.Application(middlewares=[correlation_middleware()])
    app["connectors_dir"] = connectors_dir or Path("/app/connectors")
    app["connectors"] = _load_connectors(app["connectors_dir"])
    app["connector_paths"] = {
        _webhook_path_for_connector(connector): connector.name
        for connector in app["connectors"].values()
        if connector.source.type == "webhook"
    }
    app["store"] = WorkItemStore(data_dir=data_dir or Path("/app/data"))
    app["gateway"] = GatewayClient(base_url=gateway_url, token=gateway_token)
    app["bridge_state"] = BridgeStateStore(data_dir=data_dir or Path("/app/data"))
    app["knowledge_url"] = os.environ.get("KNOWLEDGE_URL")
    app["event_buffer"] = EventBuffer()

    app.router.add_get("/health", handle_health)
    app.router.add_get("/stats", handle_stats)
    app.router.add_get("/poll-health", handle_poll_health)
    app.router.add_post("/poll/{connector_name}", handle_poll_trigger)
    app.router.add_get("/items", handle_items)
    app.router.add_post("/webhooks/{connector_name}", handle_webhook)
    app.router.add_post("/{tail:.*}", handle_webhook)

    async def _log_intake_shutdown(app: web.Application) -> None:
        logger.info("Intake server shutting down")

    app.on_shutdown.append(_log_intake_shutdown)

    app.on_startup.append(_start_background_tasks)
    app.on_cleanup.append(_cleanup_background_tasks)

    return app


def _setup_sighup_handler(app: web.Application) -> None:
    """Reload connectors on SIGHUP."""
    def reload_handler(signum, frame):
        logger.info("SIGHUP received, reloading connectors...")
        old_names = set(app["connectors"].keys())
        app["connectors"] = _load_connectors(app["connectors_dir"])
        app["connector_paths"] = {
            _webhook_path_for_connector(connector): connector.name
            for connector in app["connectors"].values()
            if connector.source.type == "webhook"
        }
        new_names = set(app["connectors"].keys())
        # Drop event buffers for connectors that were removed
        removed = old_names - new_names
        if removed:
            buf: EventBuffer = app["event_buffer"]
            for name in removed:
                buf.drop(name)
            logger.info(f"Dropped event buffers for removed connectors: {removed}")
        logger.info(f"Reloaded {len(app['connectors'])} connectors")

    signal.signal(signal.SIGHUP, reload_handler)


def main():
    parser = argparse.ArgumentParser(description="Agency intake service")
    parser.add_argument("--port", type=int, default=8080)
    parser.add_argument("--connectors-dir", type=str, default="/app/connectors")
    parser.add_argument("--data-dir", type=str, default="/app/data")
    parser.add_argument(
        "--gateway-url", type=str,
        default=os.environ.get("GATEWAY_URL", "http://gateway:8200"),
    )
    parser.add_argument(
        "--gateway-token", type=str,
        default=os.environ.get("GATEWAY_TOKEN", ""),
    )
    args = parser.parse_args()

    # Logging configured automatically by sitecustomize.py via AGENCY_COMPONENT env var.

    app = create_app(
        connectors_dir=Path(args.connectors_dir),
        data_dir=Path(args.data_dir),
        gateway_url=args.gateway_url,
        gateway_token=args.gateway_token,
    )
    _setup_sighup_handler(app)
    logger.info("Starting intake server on port %d", args.port)
    web.run_app(app, host="0.0.0.0", port=args.port, access_log_class=_HealthFilterAccessLogger)


if __name__ == "__main__":
    main()
