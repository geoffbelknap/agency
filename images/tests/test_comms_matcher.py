"""Tests for the comms matching engine."""

import pytest
from images.comms.matcher import Matcher
from images.models.subscriptions import InterestDeclaration


@pytest.fixture
def matcher(tmp_path):
    return Matcher(data_dir=tmp_path)


@pytest.fixture
def interests():
    return InterestDeclaration(
        task_id="t1",
        description="Investigating payments latency",
        keywords=["payments", "latency", "timeout"],
        knowledge_filter={"kinds": ["finding", "incident"], "topics": ["payments"]},
    )


class TestDirectMatch:
    def test_at_mention_is_direct(self, matcher, interests):
        result = matcher.classify(
            agent_name="agent-bob",
            message_content="Hey @agent-bob check this out",
            interests=interests,
        )
        assert result.classification == "direct"

    def test_at_mention_case_sensitive(self, matcher, interests):
        result = matcher.classify(
            agent_name="agent-bob",
            message_content="Hey @Agent-Bob check this",
            interests=interests,
        )
        assert result.classification != "direct"


class TestInterestMatch:
    def test_keyword_match(self, matcher, interests):
        result = matcher.classify(
            agent_name="agent-bob",
            message_content="The payments gateway is experiencing high latency",
            interests=interests,
        )
        assert result.classification == "interest_match"
        assert "payments" in result.matched_keywords or "latency" in result.matched_keywords

    def test_no_keyword_match(self, matcher, interests):
        result = matcher.classify(
            agent_name="agent-bob",
            message_content="Updated the README with new docs",
            interests=interests,
        )
        assert result.classification == "ambient"


class TestKnowledgeMatch:
    def test_structural_kind_match(self, matcher, interests):
        result = matcher.classify_knowledge(
            agent_name="agent-bob",
            node_summary="API latency spike detected",
            metadata={"kind": "finding", "topic": "api-gateway"},
            interests=interests,
        )
        assert result.classification == "interest_match"

    def test_structural_topic_match(self, matcher, interests):
        result = matcher.classify_knowledge(
            agent_name="agent-bob",
            node_summary="New deployment completed",
            metadata={"kind": "event", "topic": "payments"},
            interests=interests,
        )
        assert result.classification == "interest_match"

    def test_fts_match_on_summary(self, matcher, interests):
        result = matcher.classify_knowledge(
            agent_name="agent-bob",
            node_summary="timeout errors increased in payments service",
            metadata={"kind": "metric", "topic": "monitoring"},
            interests=interests,
        )
        assert result.classification == "interest_match"

    def test_no_match(self, matcher, interests):
        result = matcher.classify_knowledge(
            agent_name="agent-bob",
            node_summary="New team member onboarded",
            metadata={"kind": "event", "topic": "hr"},
            interests=interests,
        )
        assert result is None  # not forwarded


class TestNoInterests:
    def test_no_interests_all_ambient(self, matcher):
        empty = InterestDeclaration(task_id="t1", description="test")
        result = matcher.classify(
            agent_name="agent-bob",
            message_content="The payments service is down",
            interests=empty,
        )
        assert result.classification == "ambient"

    def test_no_interests_mention_still_direct(self, matcher):
        empty = InterestDeclaration(task_id="t1", description="test")
        result = matcher.classify(
            agent_name="agent-bob",
            message_content="@agent-bob are you there?",
            interests=empty,
        )
        assert result.classification == "direct"


class TestSummaryGeneration:
    def test_summary_truncated_to_200_chars(self, matcher):
        long_content = "x" * 500
        summary = matcher.generate_summary(long_content)
        assert len(summary) <= 200

    def test_summary_strips_control_chars(self, matcher):
        content = "Hello\x00\x01\x02World\nNewline ok"
        summary = matcher.generate_summary(content)
        assert "\x00" not in summary
        assert "\x01" not in summary
        assert "\n" not in summary

    def test_summary_preserves_printable(self, matcher):
        content = "Normal message about payments"
        summary = matcher.generate_summary(content)
        assert summary == content
