"""Logging helpers for packaged host-managed infrastructure services."""

import contextvars
import json
import logging
import os
import sys
from datetime import datetime, timezone


correlation_id: contextvars.ContextVar[str] = contextvars.ContextVar(
    "correlation_id", default=""
)


class JSONFormatter(logging.Formatter):
    def __init__(self, component: str, build_id: str) -> None:
        super().__init__()
        self._component = component
        self._build_id = build_id

    def format(self, record: logging.LogRecord) -> str:
        entry: dict = {
            "ts": datetime.fromtimestamp(record.created, tz=timezone.utc).strftime(
                "%Y-%m-%dT%H:%M:%S.%fZ"
            ),
            "level": record.levelname.lower(),
            "component": self._component,
            "build_id": self._build_id,
            "msg": record.getMessage(),
        }

        for key in (
            "agent",
            "duration_ms",
            "method",
            "path",
            "status",
            "remote",
            "bytes",
            "model",
            "service",
        ):
            val = getattr(record, key, None)
            if val is not None:
                entry[key] = val

        cid = correlation_id.get("")
        if cid:
            entry["correlation_id"] = cid

        if record.exc_info and record.exc_info[1]:
            entry["error"] = str(record.exc_info[1])
        elif getattr(record, "error", None):
            entry["error"] = record.error

        return json.dumps(entry, default=str)


class TextFormatter(logging.Formatter):
    def __init__(self, component: str) -> None:
        super().__init__()
        self._component = component

    def format(self, record: logging.LogRecord) -> str:
        ts = datetime.fromtimestamp(record.created, tz=timezone.utc).strftime(
            "%Y-%m-%dT%H:%M:%S.%fZ"
        )
        level = record.levelname.ljust(5)
        parts = [f"{ts} {level} [{self._component}] {record.getMessage()}"]

        for key in ("agent", "correlation_id", "duration_ms", "error"):
            val = getattr(record, key, None)
            if val is not None:
                parts.append(f"{key}={val}")

        cid = correlation_id.get("")
        if cid and not getattr(record, "correlation_id", None):
            parts.append(f"correlation_id={cid}")

        if record.exc_info and record.exc_info[1]:
            parts.append(f"error={record.exc_info[1]}")

        return "  ".join(parts)


def correlation_middleware():
    from aiohttp import web

    @web.middleware
    async def middleware(request: web.Request, handler):
        cid = request.headers.get("X-Correlation-Id", "")
        token = correlation_id.set(cid)
        try:
            return await handler(request)
        finally:
            correlation_id.reset(token)

    return middleware


def setup_logging(component: str, level: int = logging.INFO) -> None:
    build_id = os.environ.get("BUILD_ID", "unknown")
    log_format = os.environ.get("AGENCY_LOG_FORMAT", "json")

    handler = logging.StreamHandler(sys.stdout)
    if log_format == "text":
        handler.setFormatter(TextFormatter(component))
    else:
        handler.setFormatter(JSONFormatter(component, build_id))

    root = logging.getLogger()
    root.handlers.clear()
    root.addHandler(handler)
    root.setLevel(level)
