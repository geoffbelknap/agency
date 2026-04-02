"""Tests for LLM synthesis pipeline."""

import json
from unittest.mock import MagicMock, patch

import httpx

from images.knowledge.store import KnowledgeStore
from images.knowledge.synthesizer import LLMSynthesizer


class TestSynthesisPrompt:
    def test_builds_prompt_with_existing_labels(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        store.add_node(label="pricing", kind="concept", summary="")
        synth = LLMSynthesizer(store)
        prompt = synth._build_extraction_prompt([
            {"author": "scout", "content": "The pricing model needs work", "channel": "general"},
        ])
        assert "pricing" in prompt
        assert "scout" in prompt

    def test_parses_llm_response(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        synth = LLMSynthesizer(store)
        llm_output = {
            "entities": [
                {"label": "API rate limiting", "kind": "feature", "summary": "Limits API calls per tier"},
            ],
            "relationships": [
                {"source": "scout", "target": "API rate limiting", "relation": "proposed"},
            ],
        }
        synth._apply_extraction(llm_output, source_channels=["#general"])
        nodes = store.find_nodes("rate limiting")
        assert len(nodes) == 1
        assert nodes[0]["kind"] == "feature"

    def test_should_synthesize_by_count(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        synth = LLMSynthesizer(store, message_threshold=5)
        for i in range(5):
            synth.record_message(f"msg{i}")
        assert synth.should_synthesize() is True

    def test_should_not_synthesize_below_threshold(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        synth = LLMSynthesizer(store, message_threshold=50)
        synth.record_message("msg1")
        assert synth.should_synthesize() is False


class TestGatewayLLMFormat:
    def test_llm_fallback_uses_gateway_endpoint(self, tmp_path):
        """LLM fallback calls gateway internal LLM endpoint."""
        store = KnowledgeStore(tmp_path)
        synth = LLMSynthesizer(store)
        mock_response = httpx.Response(
            200,
            json={
                "choices": [{"message": {"content": '{"entities":[],"relationships":[]}'}}],
            },
            request=httpx.Request("POST", "http://localhost:8200/api/v1/internal/llm"),
        )
        with patch.object(synth._http_gateway, "post", return_value=mock_response) as mock_post:
            result = synth._call_llm("test prompt")
            call_args = mock_post.call_args
            body = call_args[1]["json"]
            assert body["model"] == "claude-haiku"
            assert body["messages"] == [{"role": "user", "content": "test prompt"}]
            assert body["max_tokens"] == 4096

    def test_llm_fallback_extracts_text_from_openai_response(self, tmp_path):
        """Response parsing handles OpenAI-compatible format from gateway."""
        store = KnowledgeStore(tmp_path)
        synth = LLMSynthesizer(store)
        mock_response = httpx.Response(
            200,
            json={
                "choices": [{"message": {"content": "extracted text"}}],
            },
            request=httpx.Request("POST", "http://localhost:8200/api/v1/internal/llm"),
        )
        with patch.object(synth._http_gateway, "post", return_value=mock_response):
            result = synth._call_llm("test prompt")
            assert result == "extracted text"


class TestServerIntegration:
    def test_create_app_with_ingestion(self, tmp_path):
        """create_app creates synthesizer without enforcer_url."""
        from images.knowledge.server import create_app
        app = create_app(data_dir=tmp_path, enable_ingestion=True)
        synth = app["synthesizer"]
        assert isinstance(synth, LLMSynthesizer)
        assert not hasattr(synth, "enforcer_url")


class TestConfigurableThresholds:
    def test_default_thresholds_lowered(self, tmp_path):
        """Default thresholds are lower for low-traffic environments."""
        store = KnowledgeStore(tmp_path)
        synth = LLMSynthesizer(store)
        assert synth.message_threshold == 10
        assert synth.time_threshold_seconds == 3600  # 1 hour
        assert synth.min_interval_seconds == 300  # 5 minutes

    def test_thresholds_from_env(self, tmp_path, monkeypatch):
        """Thresholds can be configured via environment variables."""
        monkeypatch.setenv("AGENCY_SYNTH_MSG_THRESHOLD", "25")
        monkeypatch.setenv("AGENCY_SYNTH_TIME_THRESHOLD_HOURS", "3")
        monkeypatch.setenv("AGENCY_SYNTH_MIN_INTERVAL_SECS", "600")
        store = KnowledgeStore(tmp_path)
        synth = LLMSynthesizer(store)
        assert synth.message_threshold == 25
        assert synth.time_threshold_seconds == 10800  # 3 hours
        assert synth.min_interval_seconds == 600
