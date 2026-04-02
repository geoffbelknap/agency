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
