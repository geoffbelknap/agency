"""Typed credential swap handlers.

Each handler function takes (flow, config, key_resolver) and injects
credentials into the request. Adding a new auth type = writing a new
handler and registering it in HANDLER_DISPATCH.
"""

import logging
import os
import time
from typing import Any, Optional

import requests

logger = logging.getLogger(__name__)


def handle_api_key(flow: Any, config: dict, resolver: Any) -> None:
    """Inject a static API key as a request header."""
    key = resolver.resolve(config["key_ref"])
    if not key:
        logger.warning("No key found for key_ref=%s, skipping injection", config["key_ref"])
        return
    fmt = config.get("format")
    value = fmt.format(key=key) if fmt else key
    flow.request.headers[config["header"]] = value


class JWTSwapState:
    """Manages JWT token exchange and caching for a single swap entry."""

    def __init__(self, config: dict, resolver: Any) -> None:
        self.config = config
        self.resolver = resolver
        self._token: Optional[str] = None
        self._expiry: float = 0.0

    def get_token(self) -> Optional[str]:
        now = time.time()
        if self._token and now < (self._expiry - 60):
            return self._token

        raw_key = self.resolver.resolve(self.config["key_ref"])
        if not raw_key:
            logger.warning("No key for jwt-exchange key_ref=%s", self.config["key_ref"])
            return self._token  # return stale token if available

        params = {}
        for k, v in self.config.get("token_params", {}).items():
            if v == "${credential}":
                params[k] = raw_key
            elif v.startswith("${") and v.endswith("}"):
                env_name = v[2:-1]
                params[k] = os.environ.get(env_name, "")
            else:
                params[k] = v

        try:
            resp = requests.post(self.config["token_url"], data=params, timeout=10)
            resp.raise_for_status()
            data = resp.json()
            field = self.config.get("token_response_field", "access_token")
            token = data.get(field)
            if token:
                self._token = token
                self._expiry = now + self.config.get("token_ttl_seconds", 3600)
                return self._token
            else:
                logger.warning("JWT exchange: field %s not in response", field)
                return self._token
        except Exception as exc:
            logger.warning("JWT exchange failed: %s", exc)
            return self._token


def handle_jwt_exchange(flow: Any, config: dict, resolver: Any,
                        _state_cache: Optional[dict] = None) -> None:
    """Exchange a credential for a JWT and inject it."""
    if _state_cache is None:
        _state_cache = {}
    key_ref = config["key_ref"]
    if key_ref not in _state_cache:
        _state_cache[key_ref] = JWTSwapState(config, resolver)
    state = _state_cache[key_ref]

    token = state.get_token()
    if token:
        header = config.get("inject_header", "Authorization")
        fmt = config.get("inject_format", "Bearer {token}")
        flow.request.headers[header] = fmt.format(token=token)
    else:
        logger.warning("JWT swap: no token for %s, forwarding without auth", key_ref)


def handle_github_app(flow: Any, config: dict, resolver: Any) -> None:
    """Generate GitHub App installation token and inject it."""
    try:
        import jwt as pyjwt
    except ImportError:
        logger.warning("PyJWT not installed, cannot handle github-app swap")
        return

    cache_key = "_github_app_state"
    if not hasattr(handle_github_app, cache_key):
        app_id = resolver.resolve(config.get("app_id_ref", "GITHUB_APP_ID"))
        installation_id = resolver.resolve(config.get("installation_id_ref", "GITHUB_INSTALLATION_ID"))
        pk_path = config.get("private_key_path", "")

        if not all([app_id, installation_id, pk_path]):
            logger.warning("GitHub App config incomplete")
            return

        try:
            private_key = open(pk_path).read()
        except OSError:
            logger.warning("Cannot read GitHub App private key at %s", pk_path)
            return

        setattr(handle_github_app, cache_key, {
            "app_id": app_id,
            "installation_id": installation_id,
            "private_key": private_key,
            "token": None,
            "expiry": 0.0,
        })

    state = getattr(handle_github_app, cache_key)
    now = time.time()

    if state["token"] and now < (state["expiry"] - 60):
        flow.request.headers["authorization"] = f"token {state['token']}"
        return

    payload = {
        "iat": int(now) - 60,
        "exp": int(now) + 600,
        "iss": state["app_id"],
    }
    jwt_token = pyjwt.encode(payload, state["private_key"], algorithm="RS256")

    try:
        resp = requests.post(
            f"https://api.github.com/app/installations/{state['installation_id']}/access_tokens",
            headers={
                "Authorization": f"Bearer {jwt_token}",
                "Accept": "application/vnd.github+json",
            },
            timeout=10,
        )
        resp.raise_for_status()
        data = resp.json()
        state["token"] = data["token"]
        state["expiry"] = now + 3600
        flow.request.headers["authorization"] = f"token {state['token']}"
    except Exception as exc:
        logger.warning("GitHub App token exchange failed: %s", exc)
        if state["token"]:
            flow.request.headers["authorization"] = f"token {state['token']}"


# Handler dispatch registry. Add new types here.
HANDLER_DISPATCH: dict = {
    "api-key": handle_api_key,
    "jwt-exchange": handle_jwt_exchange,
    "github-app": handle_github_app,
}
