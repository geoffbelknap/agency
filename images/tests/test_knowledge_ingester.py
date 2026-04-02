"""Tests for the rule-based knowledge ingester."""

import json

from images.knowledge.ingester import RuleIngester
from images.knowledge.store import KnowledgeStore


class TestMessageIngestion:
    def test_ingest_message_creates_agent_node(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        ingester = RuleIngester(store)
        ingester.ingest_message({
            "id": "msg001",
            "channel": "general",
            "author": "scout",
            "content": "Hello team",
            "timestamp": "2026-03-09T10:00:00Z",
            "flags": {},
        })
        agents = store.find_nodes_by_kind("agent")
        assert any(n["label"] == "scout" for n in agents)

    def test_ingest_message_creates_channel_node(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        ingester = RuleIngester(store)
        ingester.ingest_message({
            "id": "msg001",
            "channel": "general",
            "author": "scout",
            "content": "Hello",
            "timestamp": "2026-03-09T10:00:00Z",
            "flags": {},
        })
        channels = store.find_nodes_by_kind("channel")
        assert any(n["label"] == "general" for n in channels)

    def test_ingest_message_creates_member_of_edge(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        ingester = RuleIngester(store)
        ingester.ingest_message({
            "id": "msg001",
            "channel": "general",
            "author": "scout",
            "content": "Hello",
            "timestamp": "2026-03-09T10:00:00Z",
            "flags": {},
        })
        agents = store.find_nodes_by_kind("agent")
        agent_id = agents[0]["id"]
        edges = store.get_edges(agent_id, direction="outgoing", relation="member_of")
        assert len(edges) == 1

    def test_decision_flag_creates_decision_node(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        ingester = RuleIngester(store)
        ingester.ingest_message({
            "id": "msg002",
            "channel": "general",
            "author": "lead",
            "content": "We will use three-tier pricing",
            "timestamp": "2026-03-09T10:00:00Z",
            "flags": {"decision": True},
        })
        decisions = store.find_nodes_by_kind("decision")
        assert len(decisions) == 1
        assert "three-tier pricing" in decisions[0]["summary"]

    def test_reply_creates_replied_to_edge(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        ingester = RuleIngester(store)
        ingester.ingest_message({
            "id": "msg001",
            "channel": "general",
            "author": "scout",
            "content": "What about pricing?",
            "timestamp": "2026-03-09T10:00:00Z",
            "flags": {},
        })
        ingester.ingest_message({
            "id": "msg002",
            "channel": "general",
            "author": "lead",
            "content": "Three tiers",
            "timestamp": "2026-03-09T10:01:00Z",
            "flags": {},
            "reply_to": "msg001",
        })
        edges = store._db.execute(
            "SELECT * FROM edges WHERE relation = 'replied_to'"
        ).fetchall()
        assert len(edges) == 1

    def test_duplicate_agent_not_created(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        ingester = RuleIngester(store)
        for i in range(3):
            ingester.ingest_message({
                "id": f"msg{i}",
                "channel": "general",
                "author": "scout",
                "content": f"Message {i}",
                "timestamp": "2026-03-09T10:00:00Z",
                "flags": {},
            })
        agents = store.find_nodes_by_kind("agent")
        scout_agents = [a for a in agents if a["label"] == "scout"]
        assert len(scout_agents) == 1
