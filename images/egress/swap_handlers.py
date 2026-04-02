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
        """Exchange an API key for a JWT.

        Always resolves the raw API key from key_ref in .service-keys.env.
        The enforcer no longer injects credentials — real API keys stay in
        the egress boundary only (ASK Tenet 4: least privilege).
        """
        raw_key = self.resolver.resolve(self.config["key_ref"])
        if not raw_key:
            logger.warning("No key for jwt-exchange key_ref=%s", self.config["key_ref"])
            return self._token  # return stale token if available

        # Cache keyed by key_ref config name (stable across reloads)
        now = time.time()
        if self._token and now < (self._expiry - 60):
            return self._token

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


# JWT state cache: keyed by key_ref (service config name).
_jwt_state_cache: dict = {}


def handle_jwt_exchange(flow: Any, config: dict, resolver: Any,
                        _state_cache: Optional[dict] = None) -> None:
    """Exchange a credential for a JWT and inject it.

    Resolves the raw API key from key_ref in .service-keys.env, exchanges
    it for a JWT, and injects the token into the request. The enforcer
    passes X-Agency-Service through for dispatch — real credentials are
    resolved here in the egress boundary only (ASK Tenet 4).
    """
    if _state_cache is None:
        _state_cache = _jwt_state_cache

    # Cache keyed by key_ref (service config name)
    cache_key = config["key_ref"]

    if cache_key not in _state_cache:
        _state_cache[cache_key] = JWTSwapState(config, resolver)
    state = _state_cache[cache_key]

    token = state.get_token()
    if token:
        header = config.get("inject_header", "Authorization")
        fmt = config.get("inject_format", "Bearer {token}")
        flow.request.headers[header] = fmt.format(token=token)
    else:
        logger.warning("JWT swap: no token for %s, forwarding without auth", config["key_ref"])


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


def handle_body_key_swap(flow: Any, config: dict, resolver: Any) -> None:
    """Replace a scoped placeholder key in the POST body with the real key.

    Used when a CLI tool (like limacharlie) does its own token exchange
    and sends the API key in the POST body. The proxy intercepts the
    request and swaps the scoped placeholder for the real credential
    before forwarding.

    Config fields:
      key_ref: name in .service-keys.env holding the real key
      body_field: POST form field name containing the key (default: "secret")
    """
    real_key = resolver.resolve(config["key_ref"])
    if not real_key:
        logger.warning("No key for body-key-swap key_ref=%s", config["key_ref"])
        return

    body_field = config.get("body_field", "secret")
    content_type = flow.request.headers.get("content-type", "")

    if "application/x-www-form-urlencoded" in content_type:
        # URL-encoded form body — replace the field value
        from urllib.parse import parse_qs, urlencode
        body = flow.request.get_text()
        params = parse_qs(body, keep_blank_values=True)
        if body_field in params:
            params[body_field] = [real_key]
            flow.request.set_text(urlencode(params, doseq=True))
            logger.debug("body-key-swap: replaced %s in form body", body_field)
    elif "application/json" in content_type:
        # JSON body — replace the field value
        import json
        try:
            data = json.loads(flow.request.get_text())
            if body_field in data:
                data[body_field] = real_key
                flow.request.set_text(json.dumps(data))
                logger.debug("body-key-swap: replaced %s in JSON body", body_field)
        except (json.JSONDecodeError, ValueError):
            logger.warning("body-key-swap: failed to parse JSON body")
    else:
        # Raw body — try simple string replacement
        body = flow.request.get_text()
        # Find any scoped key pattern and replace with real key
        import re
        scoped_pattern = r'agency-scoped--[A-Za-z0-9]+'
        if re.search(scoped_pattern, body):
            flow.request.set_text(re.sub(scoped_pattern, real_key, body))
            logger.debug("body-key-swap: replaced scoped key in raw body")


# Handler dispatch registry. Add new types here.
HANDLER_DISPATCH: dict = {
    "api-key": handle_api_key,
    "jwt-exchange": handle_jwt_exchange,
    "github-app": handle_github_app,
    "body-key-swap": handle_body_key_swap,
}
