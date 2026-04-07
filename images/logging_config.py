"""Agency unified logging configuration.

Provides structured JSON logging for all agency containers. Configured
automatically via sitecustomize.py — no manual setup required.

Manual usage (if needed):
    from logging_config import setup_logging
    setup_logging("my-component")
"""

import contextvars
import json
import logging
import os
import sys
from datetime import datetime, timezone

# Correlation ID propagated across async contexts (set by aiohttp middleware)
correlation_id: contextvars.ContextVar[str] = contextvars.ContextVar(
    "correlation_id", default=""
)


class JSONFormatter(logging.Formatter):
    """Structured JSON formatter matching the agency log schema.

    Required fields: ts, level, component, build_id, msg
    Optional fields: agent, correlation_id, error, duration_ms, plus extras
    """

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

        # Standard optional fields from extra={...} or record attributes
        for key in ("agent", "duration_ms", "method", "path", "status",
                     "remote", "bytes", "model", "service"):
            val = getattr(record, key, None)
            if val is not None:
                entry[key] = val

        # Correlation ID from contextvar (set by middleware)
        cid = correlation_id.get("")
        if cid:
            entry["correlation_id"] = cid

        # Error from exception info or explicit extra
        if record.exc_info and record.exc_info[1]:
            entry["error"] = str(record.exc_info[1])
        elif getattr(record, "error", None):
            entry["error"] = record.error

        return json.dumps(entry, default=str)


class TextFormatter(logging.Formatter):
    """Human-readable formatter for local development."""

    def __init__(self, component: str) -> None:
        super().__init__()
        self._component = component

    def format(self, record: logging.LogRecord) -> str:
        ts = datetime.fromtimestamp(record.created, tz=timezone.utc).strftime(
            "%Y-%m-%dT%H:%M:%S.%fZ"
        )
        level = record.levelname.ljust(5)
        msg = record.getMessage()
        parts = [f"{ts} {level} [{self._component}] {msg}"]

        # Append structured fields as key=value
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


def setup_logging(component: str, level: int = logging.INFO) -> None:
    """Configure the root logger with the agency structured format.

    Args:
        component: Container/process name (e.g., "knowledge", "body")
        level: Logging level (default: INFO)
    """
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
