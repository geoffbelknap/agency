"""Key resolution abstraction for credential swap.

Today: reads from a flat .service-keys.env file.
Future: swap in a VaultKeyResolver that reads from HashiCorp Vault.
"""

import logging
from pathlib import Path
from typing import Optional, Protocol

logger = logging.getLogger(__name__)


class KeyResolver(Protocol):
    """Interface for resolving key references to real values."""

    def resolve(self, key_ref: str) -> Optional[str]: ...

    def reload(self) -> None: ...


class SocketKeyResolver:
    """Resolve key references from the gateway via Unix socket.

    Connects to the gateway's restricted socket (no auth needed — the
    socket is only accessible to containers that have it bind-mounted).
    """

    def __init__(self, socket_path: str) -> None:
        self._socket_path = socket_path
        self._cache: dict[str, str] = {}

    def resolve(self, key_ref: str) -> Optional[str]:
        if key_ref in self._cache:
            return self._cache[key_ref]
        try:
            import urllib.request
            import urllib.parse
            import json
            import http.client
            import socket

            # Connect via Unix socket using raw http.client
            conn = http.client.HTTPConnection("localhost")
            sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
            sock.settimeout(5)
            sock.connect(self._socket_path)
            conn.sock = sock

            params = urllib.parse.urlencode({"name": key_ref})
            conn.request("GET", f"/api/v1/internal/credentials/resolve?{params}")
            resp = conn.getresponse()
            if resp.status == 200:
                data = json.loads(resp.read())
                self._cache[key_ref] = data.get("value", "")
                return self._cache[key_ref]
            logger.warning(
                "SocketKeyResolver: resolve %s returned %d", key_ref, resp.status
            )
            conn.close()
        except Exception as exc:
            logger.warning("SocketKeyResolver: failed to resolve %s: %s", key_ref, exc)
        return None

    def reload(self) -> None:
        self._cache.clear()


class FileKeyResolver:
    """Resolve key references from a flat env file (KEY=VALUE per line)."""

    def __init__(self, path: str) -> None:
        self._path = path
        self._keys: dict[str, str] = {}
        self._load()

    def _load(self) -> None:
        self._keys = {}
        path = Path(self._path)
        if not path.exists():
            logger.warning("Key store not found: %s", self._path)
            return
        try:
            text = path.read_text()
        except OSError as exc:
            logger.warning("Cannot read key store (%s): %s", self._path, exc)
            return
        for line in text.splitlines():
            line = line.strip()
            if not line or line.startswith("#") or "=" not in line:
                continue
            key, val = line.split("=", 1)
            self._keys[key.strip()] = val.strip()

    def resolve(self, key_ref: str) -> Optional[str]:
        return self._keys.get(key_ref)

    def reload(self) -> None:
        self._load()
