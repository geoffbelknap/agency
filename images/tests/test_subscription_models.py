"""Tests for subscription and WebSocket event models."""

import pytest
from images.models.subscriptions import (
    InterestDeclaration,
    MatchClassification,
    WSEvent,
    CommsPolicy,
    InterruptionRule,
)


class TestInterestDeclaration:
    def test_valid_declaration(self):
        d = InterestDeclaration(
            task_id="task-abc",
            description="Investigating payments latency",
            keywords=["payments", "latency", "p99"],
            knowledge_filter={"kinds": ["finding"], "topics": ["payments"]},
        )
        assert d.task_id == "task-abc"
        assert len(d.keywords) == 3

    def test_max_20_keywords(self):
        with pytest.raises(ValueError, match="at most 20"):
            InterestDeclaration(
                task_id="t1",
                description="test",
                keywords=[f"kw{i}" for i in range(21)],
            )

    def test_min_3_char_keywords_filtered(self):
        d = InterestDeclaration(
            task_id="t1",
            description="test",
            keywords=["ab", "payments", "ok", "latency"],
        )
        assert d.keywords == ["payments", "latency"]

    def test_max_10_knowledge_filter_entries(self):
        with pytest.raises(ValueError, match="at most 10"):
            InterestDeclaration(
                task_id="t1",
                description="test",
                keywords=["payments"],
                knowledge_filter={
                    "kinds": [f"k{i}" for i in range(6)],
                    "topics": [f"t{i}" for i in range(5)],
                },
            )

    def test_empty_keywords_allowed(self):
        d = InterestDeclaration(
            task_id="t1", description="test", keywords=[]
        )
        assert d.keywords == []


class TestMatchClassification:
    def test_values(self):
        assert MatchClassification.DIRECT == "direct"
        assert MatchClassification.INTEREST_MATCH == "interest_match"
        assert MatchClassification.AMBIENT == "ambient"


class TestWSEvent:
    def test_message_event(self):
        e = WSEvent(
            v=1,
            type="message",
            channel="team-platform",
            match="interest_match",
            matched_keywords=["payments"],
            message={"id": "abc", "content": "test"},
        )
        assert e.type == "message"
        assert e.v == 1

    def test_task_event(self):
        e = WSEvent(
            v=1,
            type="task",
            task={"task_id": "t1", "content": "do something"},
        )
        assert e.type == "task"

    def test_system_event(self):
        e = WSEvent(v=1, type="system", event="constraint_update")
        assert e.type == "system"


class TestCommsPolicy:
    def test_default_policy(self):
        p = CommsPolicy()
        assert p.max_interrupts_per_task == 3
        assert p.cooldown_seconds == 60
        assert len(p.rules) > 0

    def test_custom_rules(self):
        p = CommsPolicy(
            rules=[
                InterruptionRule(match="direct", action="interrupt"),
                InterruptionRule(match="ambient", action="queue"),
            ],
            max_interrupts_per_task=5,
        )
        assert len(p.rules) == 2
        assert p.max_interrupts_per_task == 5

    def test_circuit_breaker_defaults(self):
        p = CommsPolicy()
        assert p.circuit_breaker_min_action_rate == 0.2
        assert p.circuit_breaker_window_size == 20
