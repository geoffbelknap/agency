"""Unified credential swap addon for the egress proxy.

Replaces four independent credential injection mechanisms with a single
typed handler dispatch system. Configuration from credential-swaps.yaml
(generated) and credential-swaps.local.yaml (operator overrides).

Keys resolved via KeyResolver abstraction (file-based today, Vault-ready).
"""

import logging
import os
import signal
from typing import Any, Optional

import yaml

try:
    from mitmproxy import http
except ImportError:
    pass

try:
    from images.egress.key_resolver import FileKeyResolver, SocketKeyResolver
    from images.egress.swap_handlers import HANDLER_DISPATCH
except ImportError:
    # In container: modules are at /app/ without package structure
    from key_resolver import FileKeyResolver, SocketKeyResolver
    from swap_handlers import HANDLER_DISPATCH

logger = logging.getLogger(__name__)


def _extract_domain(url: str) -> str:
    """Extract domain from a URL, stripping protocol, port, and path."""
    d = url.split("://", 1)[-1]
    d = d.split("/", 1)[0]
    d = d.split(":")[0]
    return d.lower()


class CredentialSwapAddon:
    """Mitmproxy addon that injects credentials into outbound requests.

    Loads swap rules from credential-swaps.yaml (generated) and
    credential-swaps.local.yaml (operator overrides). Dispatches to
    typed handler functions based on the 'type' field.

    Reloads on SIGHUP.
    """

    def __init__(
        self,
        swap_config_path: str = "/app/secrets/credential-swaps.yaml",
        swap_local_path: str = "/app/secrets/credential-swaps.local.yaml",
        service_keys_path: str = "/app/secrets/.service-keys.env",
    ):
        self._swap_config_path = swap_config_path
        self._swap_local_path = swap_local_path

        # Use socket resolver (credential store API via Unix socket) when
        # gateway socket is mounted; fall back to file-based resolver.
        gateway_socket = os.environ.get("GATEWAY_SOCKET", "")
        if gateway_socket and os.path.exists(gateway_socket):
            self._resolver = SocketKeyResolver(gateway_socket)
            logger.info("Using SocketKeyResolver (socket: %s)", gateway_socket)
        else:
            self._resolver = FileKeyResolver(service_keys_path)
            logger.info("Using FileKeyResolver (path: %s)", service_keys_path)

        # domain -> swap config dict
        self._domain_swaps: dict[str, dict] = {}
        # swap name -> swap config dict (for X-Agency-Service lookup)
        self._named_swaps: dict[str, dict] = {}

        self._load()

        # Register SIGHUP handler for hot-reload
        try:
            signal.signal(signal.SIGHUP, lambda *_: self.reload())
        except (OSError, ValueError):
            pass  # Not supported in this environment (e.g., tests)

    def _load(self) -> None:
        """Load swap config and build lookup tables."""
        self._domain_swaps = {}
        self._named_swaps = {}

        swaps = self._load_yaml(self._swap_config_path)
        local_swaps = self._load_yaml(self._swap_local_path)

        # Generated entries first
        for name, config in swaps.items():
            config["_name"] = name
            self._register_swap(name, config)

        # Local overrides on top (wins on domain conflict)
        for name, config in local_swaps.items():
            config["_name"] = name
            self._register_swap(name, config)

        self._resolver.reload()

        logger.info(
            "Credential swap loaded: %d domain rules, %d named rules",
            len(self._domain_swaps),
            len(self._named_swaps),
        )

    def _load_yaml(self, path: str) -> dict:
        """Load swaps dict from a YAML file. Returns empty dict on error."""
        try:
            with open(path) as f:
                data = yaml.safe_load(f) or {}
            return data.get("swaps", {})
        except (OSError, yaml.YAMLError):
            return {}

    def _register_swap(self, name: str, config: dict) -> None:
        """Register a swap entry in both domain and name lookup tables."""
        self._named_swaps[name] = config
        for domain in config.get("domains", []):
            existing = self._domain_swaps.get(domain)
            if existing:
                logger.warning(
                    "Credential swap: domain %s overridden by %s (was %s)",
                    domain, name, existing.get("_name", "unknown"),
                )
            self._domain_swaps[domain] = config

    def reload(self) -> None:
        """Reload config and keys. Called on SIGHUP."""
        old_count = len(self._domain_swaps)
        self._load()
        new_count = len(self._domain_swaps)
        logger.info(
            "Credential swap reloaded: %d domain rules (was %d)",
            new_count, old_count,
        )

    def request(self, flow: "http.HTTPFlow") -> None:
        """Intercept requests and inject credentials."""
        domain = flow.request.pretty_host.lower()

        # Check X-Agency-Service header first (agent service calls).
        # The enforcer validates scope and passes X-Agency-Service through
        # without injecting credentials. Egress resolves the real credential.
        service_name = flow.request.headers.get("x-agency-service", "")
        if service_name:
            del flow.request.headers["x-agency-service"]
            # Consume internal headers — never leak to external APIs
            if "x-agency-agent" in flow.request.headers:
                del flow.request.headers["x-agency-agent"]
            if "x-agency-tool" in flow.request.headers:
                del flow.request.headers["x-agency-tool"]
            config = self._named_swaps.get(service_name)
            if config:
                self._dispatch(flow, config)
            else:
                logger.warning("No swap config for service: %s", service_name)
            return

        # Domain-based matching
        config = self._domain_swaps.get(domain)
        if config is None:
            # Try parent domains
            parts = domain.split(".")
            for i in range(1, len(parts)):
                parent = ".".join(parts[i:])
                config = self._domain_swaps.get(parent)
                if config:
                    break

        if config:
            self._dispatch(flow, config)

    def _dispatch(self, flow: "http.HTTPFlow", config: dict) -> None:
        """Dispatch to the appropriate handler by type."""
        swap_type = config.get("type", "")
        handler = HANDLER_DISPATCH.get(swap_type)
        if handler is None:
            logger.warning("Unknown swap type: %s", swap_type)
            return
        handler(flow, config, self._resolver)
        logger.debug(
            "Credential swap: %s (%s) for %s",
            config.get("_name", "unknown"),
            swap_type,
            flow.request.pretty_url,
        )
