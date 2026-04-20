import json
import pytest
from unittest.mock import patch, MagicMock
from memory_retrieval import (
    fetch_procedural_memory,
    fetch_episodic_memory,
    fetch_conversation_memory,
    handle_recall_episodes,
)

MOCK_PROCEDURES = [
    {"attributes": {
        "timestamp": "2026-03-27T10:00:00Z",
        "duration_minutes": 4,
        "outcome": "success",
        "approach": "1. Read ticket\n2. Checked dependencies\n3. Assigned P2",
        "lessons": ["Check deploy log first"],
    }},
    {"attributes": {
        "timestamp": "2026-03-26T14:00:00Z",
        "duration_minutes": 6,
        "outcome": "success",
        "approach": "1. Read ticket\n2. Queried knowledge graph",
        "lessons": [],
    }},
]

MOCK_EPISODES = [
    {"attributes": {
        "timestamp": "2026-03-27T10:00:00Z",
        "duration_minutes": 12,
        "outcome": "success",
        "summary": "Triaged INC-4567: production API latency spike.",
        "notable_events": ["Same deploy caused INC-4501"],
        "operational_tone": "notable",
        "tags": ["production-incident"],
    }},
]

MOCK_FAILED = [
    {"attributes": {
        "timestamp": "2026-03-25T09:00:00Z",
        "outcome": "failed",
        "approach": "Tried direct API call without rate limit check",
        "lessons": ["Always check rate limits"],
    }},
]


@patch("memory_retrieval._query_knowledge")
def test_procedural_memory_formatting(mock_query):
    mock_query.return_value = MOCK_PROCEDURES
    result = fetch_procedural_memory("http://fake", "mission-123", max_retrieved=5)
    assert "## Relevant Past Procedures" in result
    assert "2026-03-27" in result
    assert "4 min" in result
    assert "Read ticket" in result
    assert "Check deploy log first" in result
    assert "adapt to the current situation" in result

@patch("memory_retrieval._query_knowledge")
def test_procedural_memory_empty(mock_query):
    mock_query.return_value = []
    result = fetch_procedural_memory("http://fake", "mission-123")
    assert result == ""

@patch("memory_retrieval._query_knowledge")
def test_procedural_memory_no_mission(mock_query):
    result = fetch_procedural_memory("http://fake", "", max_retrieved=5)
    assert result == ""
    mock_query.assert_not_called()

@patch("memory_retrieval._query_knowledge")
def test_procedural_memory_with_failures(mock_query):
    mock_query.side_effect = [MOCK_PROCEDURES, MOCK_FAILED]
    result = fetch_procedural_memory("http://fake", "m-1", include_failures=True)
    assert "Approaches That Did NOT Work" in result
    assert "rate limit" in result.lower()

@patch("memory_retrieval._query_knowledge")
def test_episodic_memory_formatting(mock_query):
    mock_query.return_value = MOCK_EPISODES
    result = fetch_episodic_memory("http://fake", "henrybot", "mission-123")
    assert "## Recent Episodes" in result
    assert "2026-03-27" in result
    assert "INC-4567" in result
    assert "Same deploy caused INC-4501" in result

@patch("memory_retrieval._query_knowledge")
def test_episodic_memory_empty(mock_query):
    mock_query.return_value = []
    result = fetch_episodic_memory("http://fake", "henrybot", "mission-123")
    assert result == ""

@patch("memory_retrieval._query_knowledge")
def test_episodic_memory_no_mission(mock_query):
    result = fetch_episodic_memory("http://fake", "henrybot", "")
    assert result == ""

@patch("memory_retrieval._query_knowledge")
def test_conversation_memory_classifies_graph_results(mock_query):
    mock_query.return_value = [
        {
            "id": "n1",
            "label": "operator-sec-preference",
            "kind": "preference",
            "summary": "Operator prefers primary SEC filings.",
            "properties": {"memory_type": "semantic"},
        },
        {
            "id": "n2",
            "label": "sec-filing-workflow",
            "kind": "procedure",
            "summary": "Use SEC company submissions before web search.",
            "properties": {},
        },
    ]

    result = fetch_conversation_memory("http://fake", "jarvis", "PLTR SEC filing")

    assert "Relevant Long-Term Memory" in result
    assert "<!-- source: conversation_memory -->" in result
    assert "source_node_ids: n1,n2" in result
    assert "(semantic) operator-sec-preference" in result
    assert "(procedural) sec-filing-workflow" in result

@patch("memory_retrieval._query_knowledge")
def test_conversation_memory_skips_pending_proposals(mock_query):
    mock_query.return_value = [
        {
            "id": "n1",
            "label": "memory-proposal:jarvis:t1:1",
            "kind": "memory_proposal",
            "summary": "Pending preference.",
            "properties": {"memory_type": "semantic", "status": "pending_review"},
        },
    ]

    result = fetch_conversation_memory("http://fake", "jarvis", "PLTR SEC filing")

    assert result == ""

@patch("memory_retrieval._query_knowledge")
def test_conversation_memory_empty_without_query(mock_query):
    result = fetch_conversation_memory("http://fake", "jarvis", "")
    assert result == ""
    mock_query.assert_not_called()

@patch("memory_retrieval._query_knowledge")
def test_recall_episodes_returns_json(mock_query):
    mock_query.return_value = MOCK_EPISODES
    result = json.loads(handle_recall_episodes("http://fake", "henrybot", "latency spike"))
    assert result["count"] == 1
    assert result["episodes"][0]["summary"] == "Triaged INC-4567: production API latency spike."

@patch("memory_retrieval._query_knowledge")
def test_recall_episodes_with_filters(mock_query):
    mock_query.return_value = []
    result = json.loads(handle_recall_episodes(
        "http://fake", "henrybot", "query",
        from_date="2026-03-01", tag="incident", outcome="success"
    ))
    assert result["count"] == 0
    # Verify the query included filters
    call_args = mock_query.call_args
    query_str = call_args[0][1]
    assert "from:2026-03-01" in query_str
    assert "tag:incident" in query_str

@patch("memory_retrieval._query_knowledge")
def test_recall_episodes_error_handling(mock_query):
    mock_query.side_effect = Exception("connection refused")
    result = json.loads(handle_recall_episodes("http://fake", "henrybot", "test"))
    assert "error" in result
    assert result["count"] == 0

@patch("memory_retrieval._query_knowledge")
def test_procedural_memory_error_returns_empty(mock_query):
    mock_query.side_effect = Exception("timeout")
    result = fetch_procedural_memory("http://fake", "m-1")
    assert result == ""
