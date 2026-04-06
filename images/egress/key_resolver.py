"""Key resolution abstraction for credential swap.

Credential resolution uses the gateway Unix socket exclusively.

Future: swap in a VaultKeyResolver that reads from HashiCorp Vault.
"""

import logging
from typing import Optional, Protocol

logger = logging.getLogger(__name__)


class KeyResolver(Protocol):
    """Interface for resolving key references to real values."""

    def resolve(self, key_ref: str) -> Optional[str]: ...

    def reload(self) -> None: ...


class SocketKeyResolver:
    """Resolve key references from the gateway.

    Tries TCP first (via GATEWAY_URL + GATEWAY_TOKEN env vars), then
    falls back to Unix socket if configured. TCP works cross-platform;
    Unix socket only works on Linux (Docker Desktop on macOS/Windows
    cannot mount Unix sockets into containers).
    """

    def __init__(self, socket_path: str) -> None:
        import os
        self._socket_path = socket_path
        self._gateway_url = os.environ.get("GATEWAY_URL", "")
        self._gateway_token = os.environ.get("GATEWAY_TOKEN", "")
        self._cache: dict[str, str] = {}

    def resolve(self, key_ref: str) -> Optional[str]:
        if key_ref in self._cache:
            return self._cache[key_ref]

        # Try TCP first (cross-platform). Works with or without token —
        # gateway in dev mode (empty token) allows all requests.
        if self._gateway_url:
            result = self._resolve_tcp(key_ref)
            if result is not None:
                return result

        # Fall back to Unix socket (Linux only)
        if self._socket_path:
            result = self._resolve_socket(key_ref)
            if result is not None:
                return result

        return None

    def _resolve_tcp(self, key_ref: str) -> Optional[str]:
        try:
            import urllib.request
            import urllib.parse
            import json

            params = urllib.parse.urlencode({"name": key_ref})
            url = f"{self._gateway_url}/api/v1/internal/credentials/resolve?{params}"
            req = urllib.request.Request(url)
            if self._gateway_token:
                req.add_header("X-Agency-Token", self._gateway_token)
            with urllib.request.urlopen(req, timeout=5) as resp:
                if resp.status == 200:
                    data = json.loads(resp.read())
                    self._cache[key_ref] = data.get("value", "")
                    return self._cache[key_ref]
            logger.warning("SocketKeyResolver: TCP resolve %s returned %d", key_ref, resp.status)
        except Exception as exc:
            logger.warning("SocketKeyResolver: TCP resolve %s failed: %s", key_ref, exc)
        return None

    def _resolve_socket(self, key_ref: str) -> Optional[str]:
        try:
            import urllib.parse
            import json
            import http.client
            import socket

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
            logger.warning("SocketKeyResolver: socket resolve %s returned %d", key_ref, resp.status)
            conn.close()
        except Exception as exc:
            logger.warning("SocketKeyResolver: socket resolve %s failed: %s", key_ref, exc)
        return None

    def reload(self) -> None:
        self._cache.clear()

