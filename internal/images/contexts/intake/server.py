"""Intake service — webhook receiver, routing, work item tracking."""

import argparse
import asyncio
import hashlib
import hmac
import logging
import os
import signal
import time
from datetime import datetime, timezone
from pathlib import Path
from string import Template

import yaml
from aiohttp import web, ClientSession

from agency_core.models.connector import ConnectorConfig, ConnectorRelayTarget
from agency_core.images.intake.router import evaluate_routes, render_template, parse_sla_duration
from agency_core.images.intake.work_items import WorkItemStore
from agency_core.images.intake.poller import PollStateStore, hash_blob, hash_items, extract_items, parse_interval
from agency_core.images.intake.scheduler import ScheduleStateStore, should_fire
from agency_core.images.intake.channel_watcher import ChannelWatchStateStore, matches_pattern

logger = logging.getLogger("intake")


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
    comms_url: str,
    agent_name: str,
    task_content: str,
    work_item_id: str,
    priority: str,
    source: str,
) -> bool:
    """Deliver task to agent via comms service."""
    async with ClientSession() as session:
        try:
            async with session.post(
                f"{comms_url}/tasks/deliver",
                json={
                    "agent_name": agent_name,
                    "task_content": task_content,
                    "work_item_id": work_item_id,
                    "priority": priority,
                    "source": source,
                },
            ) as resp:
                return resp.status == 200
        except Exception as e:
            logger.error(f"Task delivery failed: {e}")
            return False


async def _deliver_channel_message(
    comms_url: str,
    channel_name: str,
    task_content: str,
) -> bool:
    """Deliver connector output directly into a channel."""
    gateway_url = os.environ.get("GATEWAY_URL", "").rstrip("/")
    gateway_token = os.environ.get("GATEWAY_TOKEN", "")
    target_url = f"{comms_url}/channels/{channel_name}/messages"
    headers = {}
    expected_status = 201
    if gateway_url and gateway_token:
        target_url = f"{gateway_url}/api/v1/comms/channels/{channel_name}/messages"
        headers["Authorization"] = f"Bearer {gateway_token}"
        expected_status = 200

    async with ClientSession() as session:
        try:
            async with session.post(
                target_url,
                json={
                    "author": "_operator",
                    "content": task_content,
                },
                headers=headers,
            ) as resp:
                if resp.status == expected_status:
                    return True
                logger.warning(f"channel message failed: {resp.status} {(await resp.text())[:400]}")
                return False
        except Exception as e:
            logger.error(f"Channel delivery failed: {e}")
            return False


def _make_ssl_context():
    """Build an SSL context that trusts the egress (mitmproxy) CA cert."""
    import ssl
    ctx = ssl.create_default_context()
    ca_cert = os.environ.get("EGRESS_CA_CERT")
    if ca_cert and Path(ca_cert).exists():
        ctx.load_verify_locations(ca_cert)
    return ctx


async def _fetch_url(url: str, method: str = "GET", headers: dict | None = None) -> dict | list | None:
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
    comms_url: str, channel: str, since: str | None = None
) -> list[dict]:
    """Fetch messages from comms service."""
    async with ClientSession() as session:
        try:
            params = {"limit": "100"}
            if since:
                params["since"] = since
            async with session.get(
                f"{comms_url}/channels/{channel}/messages", params=params
            ) as resp:
                if resp.status == 200:
                    return await resp.json()
                logger.warning(f"Channel fetch {channel} returned {resp.status}")
                return []
        except Exception as e:
            logger.warning(f"Channel fetch {channel} failed: {e}")
            return []


async def _execute_relay(relay: ConnectorRelayTarget, payload: dict, connector_name: str) -> bool:
    """Execute a relay action: render body template and POST to a URL directly.

    No agent is spawned. Used for comms→Slack mirroring, webhook forwarding, etc.
    Routes through egress proxy when HTTPS_PROXY / HTTP_PROXY env vars are set.
    """
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
            async with session.request(
                relay.method, url, data=body_str, headers=headers, proxy=proxy, ssl=ssl_ctx
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
    comms_url: str,
    source_label: str,
) -> bool:
    """Shared routing and delivery logic for all source types."""
    wi = store.create(connector=connector_name, payload=payload)
    result = evaluate_routes(connector_config.routes, payload)
    if result is None:
        store.update_status(wi.id, status="unrouted")
        return False

    route_index, route = result

    # Relay route: make a direct HTTP call, no agent spawned
    if route.relay:
        store.update_status(wi.id, status="routed", route_index=route_index, priority=route.priority)
        ok = await _execute_relay(route.relay, payload, connector_name)
        store.update_status(wi.id, status="relayed" if ok else "relay_failed")
        return ok

    # Channel/agent/team route
    target_name = route.target.get("channel") or route.target.get("team") or route.target.get("agent")
    if not target_name:
        store.update_status(wi.id, status="unrouted")
        return False
    sla_delta = parse_sla_duration(route.sla)
    sla_deadline = (datetime.now(timezone.utc) + sla_delta) if sla_delta else None
    if "channel" in route.target:
        target_type = "channel"
    elif "team" in route.target:
        target_type = "team"
    else:
        target_type = "agent"

    store.update_status(
        wi.id,
        status="routed",
        target_type=target_type,
        target_name=target_name,
        route_index=route_index,
        priority=route.priority,
        sla_deadline=sla_deadline,
    )

    # Build task content from payload summary
    task_text = json.dumps(payload, default=str, indent=2)
    if target_type == "channel":
        delivered = await _deliver_channel_message(
            comms_url=comms_url,
            channel_name=target_name,
            task_content=task_text,
        )
    else:
        delivered = await _deliver_task(
            comms_url=comms_url,
            agent_name=target_name,
            task_content=task_text,
            work_item_id=wi.id,
            priority=route.priority,
            source=source_label,
        )
    if delivered:
        store.update_status(wi.id, status="assigned", task_content=task_text)
    return delivered


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
    comms_url: str,
) -> int:
    """Execute one poll tick for a connector. Returns count of new work items."""
    # Expand ${VAR} placeholders in URL and headers using env vars.
    # This lets connectors reference service keys like ${SLACK_BOT_TOKEN}
    # without embedding them in the connector YAML.
    env = os.environ
    url = Template(connector.source.url).safe_substitute(env)
    headers = (
        {k: Template(v).safe_substitute(env) for k, v in connector.source.headers.items()}
        if connector.source.headers
        else None
    )
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
            connector.name, connector, payload, store, comms_url,
            source_label=f"poll:{connector.name}",
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
                    connector.name, connector, fu_payload, store, comms_url,
                    source_label=f"poll:{connector.name}:reply",
                )
                created += 1

    return created


async def _schedule_once(
    connectors: dict,
    store: WorkItemStore,
    schedule_state: ScheduleStateStore,
    comms_url: str,
) -> int:
    """Check all schedule connectors and fire matching ones. Returns fire count."""
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
            name, connector, payload, store, comms_url,
            source_label=f"schedule:{name}",
        )
        fired += 1
    return fired


async def _channel_watch_once(
    connector,
    store: WorkItemStore,
    watch_state: ChannelWatchStateStore,
    comms_url: str,
) -> int:
    """Check one channel-watch connector for new matching messages. Returns match count."""
    last_seen = watch_state.get_last_seen(connector.name)
    since = last_seen.isoformat() if last_seen else None

    messages = await _fetch_channel_messages(comms_url, connector.source.channel, since=since)
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
            "channel": connector.source.channel,
            "content": msg.get("content", ""),
            "sender": msg.get("sender", ""),
            "message_id": msg.get("id", ""),
            "timestamp": msg["timestamp"],
        }
        await _route_and_deliver(
            connector.name, connector, payload, store, comms_url,
            source_label=f"channel-watch:{connector.name}",
        )
        created += 1

    if latest_ts and latest_ts != last_seen:
        watch_state.set_last_seen(connector.name, latest_ts)
    return created


async def _poll_loop(app: web.Application) -> None:
    """Background task: poll all poll-type connectors on their intervals."""
    poll_state = PollStateStore(app["store"].data_dir)
    last_poll: dict[str, float] = {}

    while True:
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
                    created = await _poll_once(connector, app["store"], poll_state, app["comms_url"])
                    if created:
                        logger.info(f"Poll {name}: created {created} work items")
                except Exception as e:
                    logger.error(f"Poll {name} error: {e}")
        except Exception as e:
            logger.error(f"Poll loop error: {e}")

        await asyncio.sleep(10)


async def _schedule_loop(app: web.Application) -> None:
    """Background task: check schedule connectors every 60 seconds."""
    schedule_state = ScheduleStateStore(app["store"].data_dir)

    while True:
        try:
            fired = await _schedule_once(app["connectors"], app["store"], schedule_state, app["comms_url"])
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
                    created = await _channel_watch_once(connector, app["store"], watch_state, app["comms_url"])
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


def _verify_webhook_auth(request: web.Request, body_bytes: bytes, connector) -> web.Response | None:
    """Verify HMAC-SHA256 webhook signature. Returns an error Response on failure, None on success."""
    auth = connector.source.webhook_auth
    if not auth:
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
    connector_name = request.match_info["connector_name"]
    connectors = request.app["connectors"]
    store: WorkItemStore = request.app["store"]

    # Find connector
    if connector_name not in connectors:
        return web.json_response({"error": f"Unknown connector: {connector_name}"}, status=404)

    connector = connectors[connector_name]

    # Read raw body once (needed for HMAC verification before JSON parsing)
    body_bytes = await request.read()

    # Webhook auth verification (HMAC-SHA256, e.g. Slack Events API)
    if connector.source.webhook_auth:
        auth = connector.source.webhook_auth

        # Handle URL verification challenge before signature check
        # (Slack sends the challenge as plain JSON without a valid signature on first contact)
        if auth.challenge_field:
            try:
                import json as _json
                pre_body = _json.loads(body_bytes)
                if isinstance(pre_body, dict) and auth.challenge_field in pre_body:
                    logger.info(f"Webhook {connector_name}: responding to challenge handshake")
                    return web.json_response({auth.challenge_field: pre_body[auth.challenge_field]})
            except Exception:
                pass

        err = _verify_webhook_auth(request, body_bytes, connector)
        if err:
            return err

    # Parse JSON body
    try:
        import json as _json
        payload = _json.loads(body_bytes)
    except Exception:
        return web.json_response({"error": "Invalid JSON"}, status=400)

    if not isinstance(payload, dict):
        return web.json_response({"error": "Payload must be a JSON object"}, status=400)

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
    comms_url = request.app["comms_url"]
    delivered = await _route_and_deliver(
        connector_name, connector, payload, store, comms_url,
        source_label=f"connector:{connector_name}",
    )
    return web.json_response({"status": "ok", "delivered": delivered}, status=202)



def create_app(
    connectors_dir: Path | None = None,
    data_dir: Path | None = None,
    comms_url: str = "http://comms:18091",
) -> web.Application:
    app = web.Application()
    app["connectors_dir"] = connectors_dir or Path("/app/connectors")
    app["connectors"] = _load_connectors(app["connectors_dir"])
    app["store"] = WorkItemStore(data_dir=data_dir or Path("/app/data"))
    app["comms_url"] = comms_url

    app.router.add_get("/health", handle_health)
    app.router.add_post("/webhooks/{connector_name}", handle_webhook)

    app.on_startup.append(_start_background_tasks)
    app.on_cleanup.append(_cleanup_background_tasks)

    return app


def _setup_sighup_handler(app: web.Application) -> None:
    """Reload connectors on SIGHUP."""
    def reload_handler(signum, frame):
        logger.info("SIGHUP received, reloading connectors...")
        app["connectors"] = _load_connectors(app["connectors_dir"])
        logger.info(f"Reloaded {len(app['connectors'])} connectors")

    signal.signal(signal.SIGHUP, reload_handler)


def main():
    parser = argparse.ArgumentParser(description="Agency intake service")
    parser.add_argument("--port", type=int, default=8080)
    parser.add_argument("--connectors-dir", type=str, default="/app/connectors")
    parser.add_argument("--data-dir", type=str, default="/app/data")
    parser.add_argument("--comms-url", type=str, default="http://comms:18091")
    args = parser.parse_args()

    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s")

    app = create_app(
        connectors_dir=Path(args.connectors_dir),
        data_dir=Path(args.data_dir),
        comms_url=args.comms_url,
    )
    _setup_sighup_handler(app)
    web.run_app(app, host="0.0.0.0", port=args.port)


if __name__ == "__main__":
    main()
