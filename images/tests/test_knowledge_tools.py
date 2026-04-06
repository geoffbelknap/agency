"""Tests for knowledge query tools in body runtime."""

import json
from unittest.mock import MagicMock, patch


class TestQueryKnowledge:
    def test_returns_results(self):
        from images.body.knowledge_tools import _query_knowledge
        mock_resp = MagicMock()
        mock_resp.text = json.dumps({
            "query": "pricing",
            "results": [{"label": "pricing", "summary": "Three tiers"}],
        })
        mock_resp.json.return_value = json.loads(mock_resp.text)
        with patch("images.body.knowledge_tools._http") as mock_http:
            mock_http.post.return_value = mock_resp
            result = _query_knowledge("http://knowledge:18092", "scout", {"query": "pricing"})
        parsed = json.loads(result)
        assert "results" in parsed

    def test_returns_error_on_failure(self):
        from images.body.knowledge_tools import _query_knowledge
        with patch("images.body.knowledge_tools._http") as mock_http:
            mock_http.post.side_effect = Exception("connection refused")
            result = _query_knowledge("http://knowledge:18092", "scout", {"query": "test"})
        parsed = json.loads(result)
        assert "error" in parsed


class TestWhoKnows:
    def test_returns_agents(self):
        from images.body.knowledge_tools import _who_knows_about
        mock_resp = MagicMock()
        mock_resp.text = json.dumps({
            "topic": "pricing",
            "agents": [{"label": "scout", "relevance": 2.0}],
        })
        with patch("images.body.knowledge_tools._http") as mock_http:
            mock_http.get.return_value = mock_resp
            result = _who_knows_about("http://knowledge:18092", "scout", {"topic": "pricing"})
        assert "scout" in result


class TestRegistration:
    def test_registers_seven_tools(self):
        from images.body.knowledge_tools import register_knowledge_tools
        registry = MagicMock()
        register_knowledge_tools(registry, "http://knowledge:18092", "scout")
        assert registry.register_tool.call_count == 7
        names = [call.kwargs["name"] for call in registry.register_tool.call_args_list]
        assert "contribute_knowledge" in names
        assert "query_knowledge" in names
        assert "who_knows_about" in names
        assert "what_changed_since" in names
        assert "get_context" in names
        assert "query_graph" in names
        assert "save_insight" in names
