"""Matching engine for real-time comms push.

Classifies messages against agent interest declarations. Returns match
classification (direct, interest_match, ambient) used by the interruption
controller to decide action.

v1 uses @mention detection + FTS5 keyword matching.
v2 will add sqlite-vec semantic matching (API unchanged).
"""

import re
import sqlite3
from dataclasses import dataclass, field
from pathlib import Path


@dataclass
class MatchResult:
    classification: str | None  # "direct", "interest_match", "ambient", or None (no match)
    matched_keywords: list[str] = field(default_factory=list)


# Control characters to strip from summaries (keep printable + space)
_CONTROL_CHAR_RE = re.compile(r"[\x00-\x1f\x7f-\x9f]")


class Matcher:
    def __init__(self, data_dir: Path):
        self._data_dir = data_dir

    def classify(
        self,
        agent_name: str,
        message_content: str,
        interests: "InterestDeclaration",
    ) -> MatchResult:
        # 1. Direct: @mention
        if f"@{agent_name}" in message_content:
            return MatchResult(classification="direct")

        # 2. Interest match via keyword FTS5
        if interests.keywords:
            matched = self._fts_match(message_content, interests.keywords)
            if matched:
                return MatchResult(
                    classification="interest_match", matched_keywords=matched
                )

        # 3. Ambient
        return MatchResult(classification="ambient")

    def classify_knowledge(
        self,
        agent_name: str,
        node_summary: str,
        metadata: dict,
        interests: "InterestDeclaration",
    ) -> MatchResult:
        kf = interests.knowledge_filter

        # 1. Structural match on kind
        node_kind = metadata.get("kind", "")
        if node_kind and node_kind in kf.get("kinds", []):
            return MatchResult(classification="interest_match", matched_keywords=[node_kind])

        # 2. Structural match on topic
        node_topic = metadata.get("topic", "")
        if node_topic and node_topic in kf.get("topics", []):
            return MatchResult(classification="interest_match", matched_keywords=[node_topic])

        # 3. FTS match on summary
        if interests.keywords:
            matched = self._fts_match(node_summary, interests.keywords)
            if matched:
                return MatchResult(classification="interest_match", matched_keywords=matched)

        # 4. No match — not forwarded
        return None

    def _fts_match(self, content: str, keywords: list[str]) -> list[str]:
        """Check if content matches any keywords via FTS5. Returns matched keywords.

        Uses a per-call in-memory SQLite database to avoid concurrency issues
        when multiple async handlers call this simultaneously.
        """
        matched = []
        conn = sqlite3.connect(":memory:")
        try:
            conn.execute(
                "CREATE VIRTUAL TABLE match_tmp "
                "USING fts5(content, tokenize='unicode61')"
            )
            conn.execute("INSERT INTO match_tmp(content) VALUES (?)", (content,))

            for kw in keywords:
                if " " in kw:
                    query = f'"{kw}"'
                else:
                    query = kw
                try:
                    row = conn.execute(
                        "SELECT count(*) FROM match_tmp WHERE match_tmp MATCH ?",
                        (query,),
                    ).fetchone()
                    if row and row[0] > 0:
                        matched.append(kw)
                except sqlite3.OperationalError:
                    continue
        finally:
            conn.close()
        return matched

    def generate_summary(self, content: str) -> str:
        """Generate a sanitized, truncated summary for interrupt injection."""
        cleaned = _CONTROL_CHAR_RE.sub("", content)
        cleaned = cleaned.replace("\n", " ").replace("\r", " ")
        cleaned = " ".join(cleaned.split())
        if len(cleaned) > 200:
            cleaned = cleaned[:197] + "..."
        return cleaned
