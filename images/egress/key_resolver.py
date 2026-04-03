"""Key resolution abstraction for credential swap.

Resolvers try the gateway Unix socket first (zero-auth, Linux hosts),
then fall back to HTTP via host.docker.internal (macOS Docker Desktop
where Unix sockets can't cross the VM boundary).

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
            logger.warning(
                "SocketKeyResolver: resolve %s returned %d", key_ref, resp.status
            )
            conn.close()
        except Exception as exc:
            logger.warning("SocketKeyResolver: failed to resolve %s: %s", key_ref, exc)
        return None

    def reload(self) -> None:
        self._cache.clear()


class HTTPKeyResolver:
    """Resolve key references from the gateway via HTTP (TCP).

    Used when Unix sockets are unavailable (e.g. macOS Docker Desktop).
    Requires GATEWAY_URL and GATEWAY_TOKEN environment variables.
    Hits the authenticated /api/v1/internal/credentials/resolve endpoint.
    """

    def __init__(self, gateway_url: str, token: str) -> None:
        self._gateway_url = gateway_url.rstrip("/")
        self._token = token
        self._cache: dict[str, str] = {}

    def resolve(self, key_ref: str) -> Optional[str]:
        if key_ref in self._cache:
            return self._cache[key_ref]
        try:
            import urllib.request
            import urllib.parse
            import json

            params = urllib.parse.urlencode({"name": key_ref})
            url = f"{self._gateway_url}/api/v1/internal/credentials/resolve?{params}"
            req = urllib.request.Request(url)
            req.add_header("Authorization", f"Bearer {self._token}")
            with urllib.request.urlopen(req, timeout=5) as resp:
                data = json.loads(resp.read())
                self._cache[key_ref] = data.get("value", "")
                return self._cache[key_ref]
        except Exception as exc:
            logger.warning("HTTPKeyResolver: failed to resolve %s: %s", key_ref, exc)
        return None

    def reload(self) -> None:
        self._cache.clear()
