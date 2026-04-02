"""XPIA scanner for knowledge-sourced content before system prompt injection.

ASK Tenet 17: Instructions only come from verified principals. External
entities produce data, not instructions. Knowledge graph content flows
from potentially unverified sources (channel messages, web fetches) through
the synthesizer into agent system prompts. This scanner detects injection
patterns in that content before it reaches the prompt.

The enforcer scans tool-role messages in the LLM proxy path. This module
covers the gap: knowledge → system prompt injection, which bypasses the
enforcer's tool-message scanner.

Patterns mirror the enforcer's mcp_scanner.go for consistency.
"""

import logging
import re

logger = logging.getLogger(__name__)

# Same patterns as enforcer mcp_scanner.go — keep in sync.
_PATTERNS = [
    (re.compile(r"(?:^|\n)\s*(?:system|assistant)\s*:", re.IGNORECASE), "role impersonation"),
    (re.compile(r"ignore\s+(?:previous|above|all)\s+instructions", re.IGNORECASE), "instruction override"),
    (re.compile(r"disregard\s+(?:previous|above|your)\s+(?:instructions|constraints|rules)", re.IGNORECASE), "instruction override"),
    (re.compile(r"you\s+are\s+now\s+(?:a|an|the)\b", re.IGNORECASE), "identity override"),
    (re.compile(r"new\s+instructions?\s*:", re.IGNORECASE), "instruction injection"),
    (re.compile(r"<\s*(?:system|prompt|instruction)\s*>", re.IGNORECASE), "tag injection"),
    (re.compile(r"forget\s+(?:everything|all|your)\s+(?:previous|prior)", re.IGNORECASE), "memory wipe"),
    (re.compile(r"act\s+as\s+if\s+you\s+(?:have\s+no|don't\s+have)\s+constraints", re.IGNORECASE), "constraint bypass"),
    (re.compile(r"(?:do\s+not|don't)\s+follow\s+(?:your|the|any)\s+(?:rules|constraints|instructions)", re.IGNORECASE), "constraint bypass"),
]


def scan_text(text: str) -> list[str]:
    """Scan text for XPIA injection patterns. Returns list of flag descriptions."""
    if len(text) < 10:
        return []
    flags = []
    seen = set()
    for pattern, desc in _PATTERNS:
        if desc not in seen and pattern.search(text):
            flags.append(desc)
            seen.add(desc)
    return flags


def sanitize_knowledge_section(section: str, source: str) -> str:
    """Scan a knowledge-sourced prompt section and strip flagged content.

    Args:
        section: The markdown text to include in the system prompt
        source: Label for logging (e.g. "procedural_memory", "org_context")

    Returns:
        The section unchanged if clean, or a redacted placeholder if flagged.
    """
    flags = scan_text(section)
    if not flags:
        return section

    flag_str = ", ".join(flags)
    logger.warning(
        "XPIA: knowledge content from %s flagged for injection patterns: %s — redacted from system prompt",
        source, flag_str,
    )
    return f"[Knowledge section from {source} redacted: injection patterns detected ({flag_str})]"
