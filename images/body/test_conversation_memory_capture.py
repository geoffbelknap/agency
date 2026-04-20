import json
from types import SimpleNamespace


def test_parse_conversation_memory_response_filters_invalid_items():
    from post_task import parse_conversation_memory_response

    result = parse_conversation_memory_response(json.dumps({
        "memories": [
            {
                "memory_type": "semantic",
                "summary": "Operator prefers primary SEC filings.",
                "reason": "Explicitly asked for SEC filing.",
                "confidence": "medium",
                "entities": ["PLTR", "SEC"],
                "evidence_message_ids": ["m1"],
            },
            {"memory_type": "unknown", "summary": "skip"},
            {"memory_type": "episodic", "summary": ""},
        ]
    }))

    assert result == [{
        "memory_type": "semantic",
        "summary": "Operator prefers primary SEC filings.",
        "reason": "Explicitly asked for SEC filing.",
        "confidence": "medium",
        "entities": ["PLTR", "SEC"],
        "evidence_message_ids": ["m1"],
    }]


def test_capture_conversation_memory_proposals_writes_pending_nodes():
    from body import Body

    captured = {}

    class Client:
        def post(self, url, json=None, timeout=None):
            captured["url"] = url
            captured["json"] = json
            captured["timeout"] = timeout
            return SimpleNamespace(status_code=200)

    body = Body.__new__(Body)
    body.agent_name = "jarvis"
    body._knowledge_url = "http://knowledge"
    body._http_client = Client()
    body._task_metadata = {
        "channel": "dm-jarvis",
        "author": "_operator",
        "message_id": "m2",
        "match_type": "direct",
        "recent_message_ids": ["m1", "m2"],
    }
    body._messages = [
        {"role": "system", "content": "hidden"},
        {"role": "user", "content": "Recent conversation in this channel:\n_operator: PLTR SEC filing"},
        {"role": "assistant", "content": "The most recent filing is ..."},
    ]
    body._call_llm_for_capture = lambda messages: json.dumps({
        "memories": [{
            "memory_type": "procedural",
            "summary": "For SEC filing requests, use primary SEC filing sources first.",
            "reason": "The conversation centered on finding the most recent SEC filing.",
            "confidence": "medium",
            "entities": ["SEC", "PLTR"],
            "evidence_message_ids": ["m1", "m2"],
        }]
    })

    body._capture_conversation_memory_proposals("idle-reply-123")

    assert captured["url"] == "http://knowledge/ingest/nodes"
    node = captured["json"]["nodes"][0]
    assert node["kind"] == "memory_proposal"
    assert node["source_channels"] == ["dm-jarvis"]
    assert node["properties"]["status"] == "pending_review"
    assert node["properties"]["memory_type"] == "procedural"
    assert node["properties"]["evidence_message_ids"] == ["m1", "m2"]


def test_capture_conversation_memory_proposals_handles_explicit_operator_memory():
    from body import Body

    captured = {}

    class Client:
        def post(self, url, json=None, timeout=None):
            captured["url"] = url
            captured["json"] = json
            captured["timeout"] = timeout
            return SimpleNamespace(status_code=200)

    body = Body.__new__(Body)
    body.agent_name = "jarvis"
    body._knowledge_url = "http://knowledge"
    body._http_client = Client()
    body._task_metadata = {
        "channel": "dm-jarvis",
        "author": "_operator",
        "message_id": "m2",
        "match_type": "direct",
        "latest_message": (
            "For future work, remember this operator preference: when I ask about "
            "SEC filings, use SEC EDGAR primary filings before summaries or "
            "secondary writeups."
        ),
        "recent_message_ids": ["m1", "m2"],
    }
    body._messages = [
        {"role": "user", "content": "For future work, remember this operator preference: use SEC EDGAR first."},
        {"role": "assistant", "content": "Acknowledged."},
    ]
    body._call_llm_for_capture = lambda messages: '{"memories":[]}'

    body._capture_conversation_memory_proposals("idle-reply-123")

    assert captured["url"] == "http://knowledge/ingest/nodes"
    node = captured["json"]["nodes"][0]
    assert node["kind"] == "memory_proposal"
    assert node["properties"]["status"] == "pending_review"
    assert node["properties"]["memory_type"] == "procedural"
    assert node["properties"]["confidence"] == "high"
    assert "SEC EDGAR primary filings" in node["summary"]
    assert node["properties"]["entities"] == ["SEC", "EDGAR"]


def test_capture_conversation_memory_proposals_ignores_non_direct_tasks():
    from body import Body

    class Client:
        def post(self, *args, **kwargs):
            raise AssertionError("should not write proposals")

    body = Body.__new__(Body)
    body.agent_name = "jarvis"
    body._knowledge_url = "http://knowledge"
    body._http_client = Client()
    body._task_metadata = {"channel": "general", "match_type": "interest_match"}
    body._messages = [{"role": "user", "content": "hello"}]
    body._call_llm_for_capture = lambda messages: "{}"

    body._capture_conversation_memory_proposals("idle-reply-123")


def test_capture_conversation_memory_proposals_can_be_disabled(monkeypatch):
    from body import Body

    class Client:
        def post(self, *args, **kwargs):
            raise AssertionError("should not write proposals")

    monkeypatch.setenv("AGENCY_CONVERSATION_MEMORY_CAPTURE", "false")
    body = Body.__new__(Body)
    body.agent_name = "jarvis"
    body._knowledge_url = "http://knowledge"
    body._http_client = Client()
    body._task_metadata = {"channel": "dm-jarvis", "match_type": "direct"}
    body._messages = [{"role": "user", "content": "remember this"}]
    body._call_llm_for_capture = lambda messages: "{}"

    body._capture_conversation_memory_proposals("idle-reply-123")
