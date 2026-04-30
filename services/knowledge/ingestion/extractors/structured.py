"""Structured data extractor — regex-based entity extraction from tool output.

Fallback extractor that handles any text/* content.  Pulls out
recognizable entities (IPs, CVEs, URLs, emails) and annotates HTTP
status codes.  Always sets ``needs_synthesis=True`` because tool
outputs need semantic analysis by the LLM.
"""

from __future__ import annotations

import ipaddress
import re
from typing import Optional

from services.knowledge.ingestion.base import BaseExtractor, ExtractionResult

# ---------------------------------------------------------------------------
# Compiled patterns
# ---------------------------------------------------------------------------

_RE_IPV4 = re.compile(r"\b(\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3})\b")
_RE_CVE = re.compile(r"(CVE-\d{4}-\d{4,})")
_RE_URL = re.compile(r"(https?://[^\s<>\"')\]]+)")
_RE_EMAIL = re.compile(r"([a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,})")
_RE_HTTP_STATUS = re.compile(
    r"HTTP\s+(\d{3})|status\s+(\d{3})|\b([45]\d{2})\s+\w+",
    re.IGNORECASE,
)

# RFC 1918 + link-local + loopback ranges for private detection
_PRIVATE_NETS = (
    ipaddress.IPv4Network("10.0.0.0/8"),
    ipaddress.IPv4Network("172.16.0.0/12"),
    ipaddress.IPv4Network("192.168.0.0/16"),
    ipaddress.IPv4Network("127.0.0.0/8"),
    ipaddress.IPv4Network("169.254.0.0/16"),
)


def _is_valid_ipv4(addr: str) -> bool:
    """Return True if every octet is 0-255."""
    try:
        ipaddress.IPv4Address(addr)
        return True
    except ValueError:
        return False


def _ip_visibility(addr: str) -> str:
    """Return ``'private'`` or ``'public'``."""
    ip = ipaddress.IPv4Address(addr)
    for net in _PRIVATE_NETS:
        if ip in net:
            return "private"
    return "public"


class StructuredExtractor(BaseExtractor):
    """Regex-based entity extractor for arbitrary text content.

    Acts as the fallback extractor — ``can_handle`` returns ``True``
    for any ``text/*`` MIME type.
    """

    @property
    def name(self) -> str:  # noqa: D401
        return "structured"

    def can_handle(self, content_type: str, filename: str = "") -> bool:
        return content_type.startswith("text/")

    def extract(
        self,
        content: str,
        filename: str = "",
        metadata: Optional[dict] = None,
    ) -> ExtractionResult:
        nodes: list[dict] = []
        seen: set[str] = set()
        meta = dict(metadata) if metadata else {}

        # -- IPv4 addresses ---------------------------------------------------
        for m in _RE_IPV4.finditer(content):
            addr = m.group(1)
            if addr in seen or not _is_valid_ipv4(addr):
                continue
            seen.add(addr)
            nodes.append(
                {
                    "label": addr,
                    "kind": "indicator",
                    "summary": f"IPv4 address ({_ip_visibility(addr)})",
                    "properties": {"type": "ipv4", "visibility": _ip_visibility(addr)},
                }
            )

        # -- CVE IDs ----------------------------------------------------------
        for m in _RE_CVE.finditer(content):
            cve = m.group(1)
            if cve in seen:
                continue
            seen.add(cve)
            nodes.append(
                {
                    "label": cve,
                    "kind": "vulnerability",
                    "summary": f"CVE identifier {cve}",
                    "properties": {"type": "cve"},
                }
            )

        # -- URLs -------------------------------------------------------------
        for m in _RE_URL.finditer(content):
            url = m.group(1)
            if url in seen:
                continue
            seen.add(url)
            nodes.append(
                {
                    "label": url,
                    "kind": "url",
                    "summary": f"URL: {url}",
                    "properties": {"type": "url"},
                }
            )

        # -- Email addresses --------------------------------------------------
        for m in _RE_EMAIL.finditer(content):
            email = m.group(1)
            if email in seen:
                continue
            seen.add(email)
            nodes.append(
                {
                    "label": email,
                    "kind": "contact",
                    "summary": f"Email address {email}",
                    "properties": {"type": "email"},
                }
            )

        # -- HTTP status codes (metadata annotation) -------------------------
        status_codes: list[str] = []
        seen_codes: set[str] = set()
        for m in _RE_HTTP_STATUS.finditer(content):
            code = m.group(1) or m.group(2) or m.group(3)
            if code and code not in seen_codes:
                seen_codes.add(code)
                status_codes.append(code)
        if status_codes:
            meta["http_status_codes"] = status_codes

        return ExtractionResult(
            source_type="structured",
            content_type="text/plain",
            nodes=nodes,
            edges=[],
            raw_content=content,
            needs_synthesis=True,
            metadata=meta,
        )
