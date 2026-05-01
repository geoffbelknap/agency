"""Subscription manager for agent expertise declarations.

Supports four-tier expertise profiles (base, standing, learned, task).
In-memory store with SQLite persistence for server restart survival.
Logs all operations for audit (ASK tenet 2).
"""

import json
import logging
import sqlite3
from datetime import datetime, timezone
from pathlib import Path

from typing import Optional
from services.comms.models import ExpertiseDeclaration, ExpertiseTier, InterestDeclaration

logger = logging.getLogger("agency.comms.subscriptions")


class SubscriptionManager:
    def __init__(self, data_dir: Path):
        self._data_dir = data_dir
        self._db_path = data_dir / "subscriptions.db"
        self._log_path = data_dir / "subscriptions.log"
        # New: multi-tier expertise per agent
        self._expertise: dict[str, dict[str, ExpertiseDeclaration]] = {}
        # Agent responsiveness config: {agent: {channel: mode, "default": mode}}
        self._responsiveness: dict[str, dict[str, str]] = {}
        # Legacy: single interest declaration per agent (backward compat)
        self._interests: dict[str, InterestDeclaration] = {}
        self._init_db()
        self._load()

    def _init_db(self) -> None:
        conn = sqlite3.connect(str(self._db_path))
        conn.execute(
            "CREATE TABLE IF NOT EXISTS interests ("
            "  agent_name TEXT PRIMARY KEY,"
            "  declaration TEXT NOT NULL"
            ")"
        )
        conn.execute(
            "CREATE TABLE IF NOT EXISTS expertise ("
            "  agent_name TEXT NOT NULL,"
            "  tier TEXT NOT NULL,"
            "  declaration TEXT NOT NULL,"
            "  PRIMARY KEY (agent_name, tier)"
            ")"
        )
        conn.execute(
            "CREATE TABLE IF NOT EXISTS responsiveness ("
            "  agent_name TEXT PRIMARY KEY,"
            "  config TEXT NOT NULL"
            ")"
        )
        conn.commit()
        conn.close()

    def _load(self) -> None:
        conn = sqlite3.connect(str(self._db_path))
        # Load legacy interests
        rows = conn.execute("SELECT agent_name, declaration FROM interests").fetchall()
        for agent_name, decl_json in rows:
            try:
                self._interests[agent_name] = InterestDeclaration.model_validate_json(decl_json)
            except Exception:
                logger.warning("Failed to load interests for %s", agent_name)
        # Load expertise tiers
        rows = conn.execute("SELECT agent_name, tier, declaration FROM expertise").fetchall()
        for agent_name, tier, decl_json in rows:
            try:
                decl = ExpertiseDeclaration.model_validate_json(decl_json)
                if agent_name not in self._expertise:
                    self._expertise[agent_name] = {}
                self._expertise[agent_name][tier] = decl
            except Exception:
                logger.warning("Failed to load expertise for %s tier %s", agent_name, tier)
        # Load responsiveness configs
        rows = conn.execute("SELECT agent_name, config FROM responsiveness").fetchall()
        for agent_name, config_json in rows:
            try:
                self._responsiveness[agent_name] = json.loads(config_json)
            except Exception:
                logger.warning("Failed to load responsiveness for %s", agent_name)
        conn.close()

    # ── Responsiveness API ───────────────────────────────────────────────

    def register_responsiveness(self, agent_name: str, config: dict[str, str]) -> None:
        """Register channel responsiveness config for an agent.

        config is a dict like {"default": "mention-only", "general": "active", ...}
        """
        self._responsiveness[agent_name] = config
        conn = sqlite3.connect(str(self._db_path))
        conn.execute(
            "INSERT OR REPLACE INTO responsiveness (agent_name, config) VALUES (?, ?)",
            (agent_name, json.dumps(config)),
        )
        conn.commit()
        conn.close()
        self._audit("register_responsiveness", agent_name, config)

    def get_responsiveness(self, agent_name: str) -> dict[str, str]:
        """Get responsiveness config for an agent. Returns empty dict if none registered."""
        return self._responsiveness.get(agent_name, {})

    # ── Expertise API (new) ──────────────────────────────────────────────

    def register_expertise(self, agent_name: str, declaration: ExpertiseDeclaration) -> None:
        """Register or update a tier of expertise for an agent."""
        tier = declaration.tier.value
        if agent_name not in self._expertise:
            self._expertise[agent_name] = {}
        self._expertise[agent_name][tier] = declaration
        conn = sqlite3.connect(str(self._db_path))
        conn.execute(
            "INSERT OR REPLACE INTO expertise (agent_name, tier, declaration) VALUES (?, ?, ?)",
            (agent_name, tier, declaration.model_dump_json()),
        )
        conn.commit()
        conn.close()
        self._audit("register_expertise", agent_name, {"tier": tier, **declaration.model_dump()})

    def clear_expertise(self, agent_name: str, tier: Optional[str] = None) -> None:
        """Clear expertise for an agent. If tier is specified, only clear that tier."""
        if tier:
            if agent_name in self._expertise:
                self._expertise[agent_name].pop(tier, None)
            conn = sqlite3.connect(str(self._db_path))
            conn.execute(
                "DELETE FROM expertise WHERE agent_name = ? AND tier = ?",
                (agent_name, tier),
            )
            conn.commit()
            conn.close()
        else:
            self._expertise.pop(agent_name, None)
            conn = sqlite3.connect(str(self._db_path))
            conn.execute("DELETE FROM expertise WHERE agent_name = ?", (agent_name,))
            conn.commit()
            conn.close()
        self._audit("clear_expertise", agent_name, {"tier": tier or "all"})

    def get_expertise(self, agent_name: str) -> dict[str, ExpertiseDeclaration]:
        """Get all expertise tiers for an agent."""
        return dict(self._expertise.get(agent_name, {}))

    def get_merged_keywords(self, agent_name: str) -> list[str]:
        """Get all keywords across all tiers, merged and deduplicated."""
        tiers = self._expertise.get(agent_name, {})
        keywords = set()
        for decl in tiers.values():
            keywords.update(kw.lower() for kw in decl.keywords)
        # Also include legacy interests
        legacy = self._interests.get(agent_name)
        if legacy:
            keywords.update(kw.lower() for kw in legacy.keywords)
        return sorted(keywords)

    def all_agents_with_expertise(self) -> dict[str, dict[str, ExpertiseDeclaration]]:
        """Return all agents and their expertise profiles."""
        return dict(self._expertise)

    # ── Legacy API (backward compat) ─────────────────────────────────────

    def register(self, agent_name: str, declaration: InterestDeclaration) -> None:
        self._interests[agent_name] = declaration
        conn = sqlite3.connect(str(self._db_path))
        conn.execute(
            "INSERT OR REPLACE INTO interests (agent_name, declaration) VALUES (?, ?)",
            (agent_name, declaration.model_dump_json()),
        )
        conn.commit()
        conn.close()
        self._audit("register", agent_name, declaration.model_dump())

    def clear(self, agent_name: str) -> None:
        self._interests.pop(agent_name, None)
        conn = sqlite3.connect(str(self._db_path))
        conn.execute("DELETE FROM interests WHERE agent_name = ?", (agent_name,))
        conn.commit()
        conn.close()
        self._audit("clear", agent_name, {})

    def get(self, agent_name: str) -> Optional[InterestDeclaration]:
        return self._interests.get(agent_name)

    def all_agents_with_interests(self) -> dict[str, InterestDeclaration]:
        return dict(self._interests)

    # ── Audit ────────────────────────────────────────────────────────────

    def _audit(self, action: str, agent_name: str, data: dict) -> None:
        entry = {
            "timestamp": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
            "action": action,
            "agent_name": agent_name,
            "data": data,
        }
        with open(self._log_path, "a") as f:
            f.write(json.dumps(entry) + "\n")
