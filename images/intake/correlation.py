"""In-memory windowed event buffer for cross-source correlation."""
import threading
import time
from collections import deque
from typing import Any, Optional


class EventBuffer:
    """Rolling buffer of recent events per connector. Thread-safe."""

    def __init__(self, default_window: int = 60):
        self._buffers: dict[str, deque] = {}
        self._locks: dict[str, threading.Lock] = {}
        self._default_window = default_window

    def _get_lock(self, connector_name: str) -> threading.Lock:
        if connector_name not in self._locks:
            self._locks[connector_name] = threading.Lock()
        return self._locks[connector_name]

    def record(self, connector_name: str, payload: dict) -> None:
        lock = self._get_lock(connector_name)
        with lock:
            if connector_name not in self._buffers:
                self._buffers[connector_name] = deque()
            buf = self._buffers[connector_name]
            now = time.time()
            cutoff = now - self._default_window
            while buf and buf[0][0] < cutoff:
                buf.popleft()
            buf.append((now, payload))

    def lookup(self, connector_name: str, field: str, value: Any, window_seconds: int) -> Optional[dict]:
        lock = self._get_lock(connector_name)
        with lock:
            buf = self._buffers.get(connector_name)
            if not buf:
                return None
            cutoff = time.time() - window_seconds
            for ts, payload in reversed(buf):
                if ts < cutoff:
                    break
                if payload.get(field) == value:
                    return payload
            return None

    def drop(self, connector_name: str) -> None:
        lock = self._get_lock(connector_name)
        with lock:
            self._buffers.pop(connector_name, None)
