"""Agency Egress Proxy — mitmproxy addon.

Enforces:
  - Domain denylist (blocked domains get 403)
  - Full HTTPS interception (TLS MITM with generated CA)
  - Request logging with full URLs (structured JSONL)
  - Rate limiting (per-domain and global, sliding window)
  - Size limits (max request/response body)
  - Raw IP blocking (prevents DNS logging bypass)

HTTPS strategy: Full TLS interception.
  - CONNECT requests: domain checked, blocked if listed
  - Non-blocked: mitmproxy intercepts TLS, inspects request/response,
    then re-encrypts to upstream
  - Enables: full URL logging, request/response body inspection,
    size limits on HTTPS traffic, content-based rules
  - Tenet 3 compliance: complete mediation of all agent traffic

Policy loaded from /app/config/policy.yaml (mounted at runtime).
CA certificate generated at first run in /app/certs/ (persisted).
"""

import json
import time
from collections import defaultdict
from datetime import datetime, timezone
from pathlib import Path

import yaml
from mitmproxy import ctx, http


class EgressPolicy:
    """Loads and enforces the egress policy configuration."""

    def __init__(self, policy_path: str = "/app/config/policy.yaml"):
        self.policy_path = policy_path
        self.policy = {}
        self.blocked_domains = set()
        self.load()

    def load(self):
        try:
            with open(self.policy_path) as f:
                self.policy = yaml.safe_load(f) or {}
        except FileNotFoundError:
            ctx.log.error(f"Policy file not found: {self.policy_path} — failing closed (block all)")
            self.policy = {"_deny_all": True}

        self.blocked_domains = set()
        for entry in self.policy.get("blocked_domains", []):
            domain = entry if isinstance(entry, str) else entry.get("domain", "")
            if domain:
                self.blocked_domains.add(domain.lower())

        self._load_external_blocklists()
        ctx.log.info(f"[AGENCY-EGRESS] Total blocked domains: {len(self.blocked_domains)}")

    def _load_external_blocklists(self, blocklist_dir: str = "/app/blocklists"):
        """Load external blocklist files from the blocklist directory."""
        blocklist_path = Path(blocklist_dir)
        if not blocklist_path.is_dir():
            return
        for txt_file in sorted(blocklist_path.glob("*.txt")):
            count = 0
            for line in txt_file.read_text().splitlines():
                domain = line.strip().lower()
                if domain and not domain.startswith("#"):
                    self.blocked_domains.add(domain)
                    count += 1
            if count:
                ctx.log.info(f"[AGENCY-EGRESS] Loaded {count} domains from {txt_file.name}")

    def is_domain_blocked(self, domain: str) -> bool:
        # Fail closed: if policy file was missing, block everything
        if self.policy.get("_deny_all"):
            return True
        domain = domain.lower()
        # Block raw IP addresses
        if domain.replace(".", "").isdigit() or ":" in domain:
            return True
        if domain in self.blocked_domains:
            return True
        # O(depth) domain hierarchy walk instead of O(n) suffix iteration
        parts = domain.split(".")
        for i in range(1, len(parts)):
            parent = ".".join(parts[i:])
            if parent in self.blocked_domains:
                return True
        return False

    @property
    def max_request_body(self) -> int:
        return self.policy.get("size_limits", {}).get("max_request_body_bytes", 1_048_576)

    @property
    def max_response_body(self) -> int:
        return self.policy.get("size_limits", {}).get("max_response_body_bytes", 10_485_760)

    @property
    def global_rate_limit(self) -> int:
        return self.policy.get("rate_limits", {}).get("global_per_minute", 500)

    def domain_rate_limit(self, domain: str) -> int:
        per_domain = self.policy.get("rate_limits", {}).get("per_domain", {})
        return per_domain.get(domain.lower(), self.global_rate_limit)


class RateLimiter:
    """Sliding-window rate limiter."""

    def __init__(self):
        self.windows = defaultdict(list)
        self._last_prune = time.time()
        self._prune_interval = 300  # prune stale entries every 5 minutes

    def check(self, key: str, limit: int, window_seconds: int = 60) -> bool:
        now = time.time()
        cutoff = now - window_seconds
        self.windows[key] = [t for t in self.windows[key] if t > cutoff]
        if len(self.windows[key]) >= limit:
            return False
        self.windows[key].append(now)
        # Periodic pruning of stale keys
        if now - self._last_prune > self._prune_interval:
            self._prune(now, window_seconds)
        return True

    def _prune(self, now: float, window_seconds: int = 60) -> None:
        cutoff = now - window_seconds
        stale = [k for k, v in self.windows.items() if not v or v[-1] < cutoff]
        for k in stale:
            del self.windows[k]
        self._last_prune = now


class RequestLogger:
    """Structured JSONL logging for egress requests.

    Keeps the log file handle open across writes to avoid open/close
    overhead on every request.  Rotates to a new file on date change.
    """

    def __init__(self, log_dir: str = "/app/logs"):
        self.log_dir = Path(log_dir)
        self.log_dir.mkdir(parents=True, exist_ok=True)
        self._current_date: str = ""
        self._file = None

    def _ensure_file(self, date_str: str):
        if date_str != self._current_date or self._file is None:
            if self._file is not None:
                self._file.close()
            log_file = self.log_dir / f"egress-{date_str}.jsonl"
            self._file = open(log_file, "a")
            self._current_date = date_str

    def log(self, event: dict):
        date_str = datetime.now(timezone.utc).strftime("%Y-%m-%d")
        self._ensure_file(date_str)
        self._file.write(json.dumps(event, default=str) + "\n")
        self._file.flush()


class EgressProxyAddon:
    """mitmproxy addon enforcing the Agency egress policy."""

    def __init__(self):
        self.policy = EgressPolicy()
        self.rate_limiter = RateLimiter()
        self.logger = RequestLogger()
        self.request_count = 0
        self.blocked_count = 0

    def _get_domain(self, flow: http.HTTPFlow) -> str:
        return flow.request.pretty_host.lower()

    def _block(self, flow: http.HTTPFlow, reason: str):
        self.blocked_count += 1
        flow.response = http.Response.make(
            403,
            json.dumps({
                "error": "blocked_by_egress_policy",
                "reason": reason,
                "domain": self._get_domain(flow),
            }),
            {"Content-Type": "application/json"},
        )

    def _log_request(self, flow: http.HTTPFlow, action: str, reason: str = ""):
        event = {
            "timestamp": datetime.now(timezone.utc).isoformat(),
            "action": action,
            "method": flow.request.method,
            "domain": self._get_domain(flow),
            "path": flow.request.path,
            "url": flow.request.pretty_url,
            "status": flow.response.status_code if flow.response else None,
            "request_size": len(flow.request.content) if flow.request.content else 0,
            "response_size": len(flow.response.content) if flow.response and flow.response.content else 0,
            "reason": reason,
            "request_number": self.request_count,
        }
        self.logger.log(event)

    def http_connect(self, flow: http.HTTPFlow):
        """CONNECT requests (HTTPS tunnel setup). Block denylisted domains."""
        self.request_count += 1
        domain = self._get_domain(flow)

        if self.policy.is_domain_blocked(domain):
            self._log_request(flow, "DENIED_CONNECT", f"domain blocked: {domain}")
            self._block(flow, f"Domain '{domain}' is blocked by egress policy")
            ctx.log.warn(f"[AGENCY-EGRESS] DENIED CONNECT: {domain}")
            return

        if not self.rate_limiter.check(f"domain:{domain}", self.policy.domain_rate_limit(domain)):
            self._log_request(flow, "RATE_LIMITED_CONNECT", f"rate limit: {domain}")
            self._block(flow, f"Rate limit exceeded for '{domain}'")
            return

        if not self.rate_limiter.check("global", self.policy.global_rate_limit):
            self._log_request(flow, "RATE_LIMITED_CONNECT", "global rate limit")
            self._block(flow, "Global rate limit exceeded")
            return

        self._log_request(flow, "ALLOWED_CONNECT")

    def request(self, flow: http.HTTPFlow):
        """HTTP and HTTPS requests. Enforce policy before forwarding."""
        self.request_count += 1
        domain = self._get_domain(flow)

        if self.policy.is_domain_blocked(domain):
            self._log_request(flow, "DENIED", f"domain blocked: {domain}")
            self._block(flow, f"Domain '{domain}' is blocked by egress policy")
            return

        if not self.rate_limiter.check(f"domain:{domain}", self.policy.domain_rate_limit(domain)):
            self._log_request(flow, "RATE_LIMITED", f"rate limit: {domain}")
            self._block(flow, f"Rate limit exceeded for '{domain}'")
            return

        if not self.rate_limiter.check("global", self.policy.global_rate_limit):
            self._log_request(flow, "RATE_LIMITED", "global rate limit")
            self._block(flow, "Global rate limit exceeded")
            return

        if flow.request.content and len(flow.request.content) > self.policy.max_request_body:
            self._log_request(flow, "DENIED", "request body too large")
            self._block(flow, "Request body exceeds size limit")
            return

        self._log_request(flow, "ALLOWED")

    def response(self, flow: http.HTTPFlow):
        """Enforce response size limits."""
        if flow.response and flow.response.content:
            if len(flow.response.content) > self.policy.max_response_body:
                domain = self._get_domain(flow)
                self._log_request(flow, "TRUNCATED", "response body too large")
                flow.response = http.Response.make(
                    502,
                    json.dumps({"error": "response_too_large", "domain": domain}),
                    {"Content-Type": "application/json"},
                )


try:
    from credential_swap import CredentialSwapAddon
    addons = [EgressProxyAddon(), CredentialSwapAddon()]
except ImportError:
    # credential_swap.py not present — run without credential swapping
    addons = [EgressProxyAddon()]
