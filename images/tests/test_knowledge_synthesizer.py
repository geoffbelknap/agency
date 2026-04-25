"""Tests for LLM synthesis pipeline."""

import json
import time
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
        synth.mode = "batch"
        synth.record_message("msg1")
        assert synth.should_synthesize() is False

    def test_event_mode_synthesizes_after_one_message(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        synth = LLMSynthesizer(store, message_threshold=50)
        synth.record_message("msg1")
        assert synth.should_synthesize() is True


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
            request=httpx.Request("POST", "http://localhost:8200/api/v1/infra/internal/llm"),
        )
        with patch.object(synth._http_gateway, "post", return_value=mock_response) as mock_post:
            result = synth._call_llm("test prompt")
            call_args = mock_post.call_args
            body = call_args[1]["json"]
            assert body["model"] == "fast"
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
            request=httpx.Request("POST", "http://localhost:8200/api/v1/infra/internal/llm"),
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


class TestContentSynthesis:
    def test_add_content_for_synthesis(self, tmp_path):
        """add_content_for_synthesis queues content items."""
        store = KnowledgeStore(tmp_path)
        synth = LLMSynthesizer(store)
        assert not synth.has_pending_content()
        synth.add_content_for_synthesis("Some raw text about infrastructure")
        assert synth.has_pending_content()
        assert len(synth._pending_content) == 1
        assert synth._pending_content[0]["content"] == "Some raw text about infrastructure"
        assert synth._pending_content[0]["scope"] is None

    def test_add_content_with_scope(self, tmp_path):
        """add_content_for_synthesis stores scope metadata."""
        store = KnowledgeStore(tmp_path)
        synth = LLMSynthesizer(store)
        scope = {"source_channels": ["#docs"], "source_type": "document"}
        synth.add_content_for_synthesis("Architecture decisions", scope=scope)
        assert synth._pending_content[0]["scope"] == scope

    def test_has_pending_content_false_when_empty(self, tmp_path):
        """has_pending_content returns False on fresh synthesizer."""
        store = KnowledgeStore(tmp_path)
        synth = LLMSynthesizer(store)
        assert synth.has_pending_content() is False

    def test_should_synthesize_with_pending_content(self, tmp_path):
        """should_synthesize returns True when content is queued."""
        store = KnowledgeStore(tmp_path)
        synth = LLMSynthesizer(store, message_threshold=50)
        assert synth.should_synthesize() is False
        synth.add_content_for_synthesis("Some content to process")
        assert synth.should_synthesize() is True

    def test_should_synthesize_content_respects_min_interval(self, tmp_path):
        """Pending content still respects the minimum interval cooldown."""
        store = KnowledgeStore(tmp_path)
        synth = LLMSynthesizer(store, min_interval_seconds=9999)
        # Simulate a recent synthesis
        synth._last_synthesis = time.monotonic()
        synth.add_content_for_synthesis("Content")
        assert synth.should_synthesize() is False

    def test_synthesize_content_calls_llm_and_applies(self, tmp_path):
        """synthesize_content processes content through LLM extraction."""
        store = KnowledgeStore(tmp_path)
        synth = LLMSynthesizer(store)
        synth.add_content_for_synthesis(
            "The payment service depends on Stripe API",
            scope={"source_channels": ["#docs"], "source_type": "document"},
        )
        llm_response = json.dumps({
            "entities": [
                {"label": "payment service", "kind": "system", "summary": "Handles payments"},
                {"label": "Stripe API", "kind": "service", "summary": "Payment provider"},
            ],
            "relationships": [
                {"source": "payment service", "target": "Stripe API", "relation": "depends_on"},
            ],
        })
        mock_resp = httpx.Response(
            200,
            json={"choices": [{"message": {"content": llm_response}}]},
            request=httpx.Request("POST", "http://localhost:8200/api/v1/infra/internal/llm"),
        )
        with patch.object(synth._http_gateway, "post", return_value=mock_resp):
            synth.synthesize_content()

        assert not synth.has_pending_content()
        nodes = store.find_nodes("payment service")
        assert len(nodes) == 1
        assert nodes[0]["kind"] == "system"

    def test_synthesize_content_clears_pending(self, tmp_path):
        """synthesize_content clears the pending content list."""
        store = KnowledgeStore(tmp_path)
        synth = LLMSynthesizer(store)
        synth.add_content_for_synthesis("Content A")
        synth.add_content_for_synthesis("Content B")
        mock_resp = httpx.Response(
            200,
            json={"choices": [{"message": {"content": '{"entities":[],"relationships":[]}'}}]},
            request=httpx.Request("POST", "http://localhost:8200/api/v1/infra/internal/llm"),
        )
        with patch.object(synth._http_gateway, "post", return_value=mock_resp):
            synth.synthesize_content()
        assert not synth.has_pending_content()


class TestConfigurableThresholds:
    def test_default_thresholds_lowered(self, tmp_path):
        """Default thresholds are lower for low-traffic environments."""
        store = KnowledgeStore(tmp_path)
        synth = LLMSynthesizer(store)
        assert synth.mode == "event"
        assert synth.message_threshold == 1
        assert synth.time_threshold_seconds == 3600  # 1 hour
        assert synth.min_interval_seconds == 60  # 1 minute

    def test_thresholds_from_env(self, tmp_path, monkeypatch):
        """Thresholds can be configured via environment variables."""
        monkeypatch.setenv("AGENCY_SYNTH_MODE", "batch")
        monkeypatch.setenv("AGENCY_SYNTH_MSG_THRESHOLD", "25")
        monkeypatch.setenv("AGENCY_SYNTH_TIME_THRESHOLD_HOURS", "3")
        monkeypatch.setenv("AGENCY_SYNTH_MIN_INTERVAL_SECS", "600")
        store = KnowledgeStore(tmp_path)
        synth = LLMSynthesizer(store)
        assert synth.mode == "batch"
        assert synth.message_threshold == 25
        assert synth.time_threshold_seconds == 10800  # 3 hours
        assert synth.min_interval_seconds == 600
