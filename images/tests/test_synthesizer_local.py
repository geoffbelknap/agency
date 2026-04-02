"""Tests for local-first synthesizer with admin model + Haiku fallback."""

import json
import os
from unittest.mock import patch, MagicMock

import pytest

from images.knowledge.store import KnowledgeStore
from images.knowledge.synthesizer import LLMSynthesizer


VALID_EXTRACTION = json.dumps({
    "entities": [{"label": "test-svc", "kind": "component", "summary": "a test service"}],
    "relationships": [],
})

VALID_OPENAI_RESPONSE = {
    "choices": [{"message": {"content": VALID_EXTRACTION}}],
}


class TestLocalFirstFlow:
    def test_calls_admin_model_first(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        with patch.dict(os.environ, {"KNOWLEDGE_LOCAL_MODEL_ENABLED": "true"}):
            synth = LLMSynthesizer(store)
        mock_response = MagicMock()
        mock_response.status_code = 200
        mock_response.json.return_value = VALID_OPENAI_RESPONSE
        mock_response.raise_for_status = MagicMock()
        synth._http_admin = MagicMock()
        synth._http_admin.post.return_value = mock_response
        synth.synthesize(
            [{"author": "scout", "content": "test msg", "channel": "general"}],
            ["#general"],
        )
        synth._http_admin.post.assert_called_once()
        nodes = store.find_nodes("test-svc")
        assert len(nodes) == 1
        assert nodes[0]["source_type"] == "local"

    def test_fallback_to_haiku_on_connection_error(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        with patch.dict(os.environ, {"KNOWLEDGE_LOCAL_MODEL_ENABLED": "true", "KNOWLEDGE_SYNTH_FALLBACK": "true"}):
            synth = LLMSynthesizer(store)
        synth._http_admin = MagicMock()
        synth._http_admin.post.side_effect = Exception("connection refused")
        mock_gateway_resp = MagicMock()
        mock_gateway_resp.status_code = 200
        mock_gateway_resp.json.return_value = VALID_OPENAI_RESPONSE
        mock_gateway_resp.raise_for_status = MagicMock()
        synth._http_gateway = MagicMock()
        synth._http_gateway.post.return_value = mock_gateway_resp
        synth.synthesize(
            [{"author": "scout", "content": "test msg", "channel": "general"}],
            ["#general"],
        )
        nodes = store.find_nodes("test-svc")
        assert len(nodes) == 1
        assert nodes[0]["source_type"] == "llm"

    def test_fallback_on_parse_error(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        with patch.dict(os.environ, {"KNOWLEDGE_LOCAL_MODEL_ENABLED": "true", "KNOWLEDGE_SYNTH_FALLBACK": "true"}):
            synth = LLMSynthesizer(store)
        bad_response = MagicMock()
        bad_response.status_code = 200
        bad_response.json.return_value = {
            "choices": [{"message": {"content": "not json at all"}}],
        }
        bad_response.raise_for_status = MagicMock()
        synth._http_admin = MagicMock()
        synth._http_admin.post.return_value = bad_response
        mock_gateway_resp = MagicMock()
        mock_gateway_resp.status_code = 200
        mock_gateway_resp.json.return_value = VALID_OPENAI_RESPONSE
        mock_gateway_resp.raise_for_status = MagicMock()
        synth._http_gateway = MagicMock()
        synth._http_gateway.post.return_value = mock_gateway_resp
        synth.synthesize(
            [{"author": "scout", "content": "test msg", "channel": "general"}],
            ["#general"],
        )
        nodes = store.find_nodes("test-svc")
        assert len(nodes) == 1

    def test_no_fallback_when_both_disabled(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        with patch.dict(os.environ, {"KNOWLEDGE_LOCAL_MODEL_ENABLED": "true", "KNOWLEDGE_SYNTH_FALLBACK": "false"}):
            synth = LLMSynthesizer(store)
        synth._http_admin = MagicMock()
        synth._http_admin.post.side_effect = Exception("connection refused")
        synth._http_gateway = MagicMock()
        synth.synthesize(
            [{"author": "scout", "content": "test msg", "channel": "general"}],
            ["#general"],
        )
        synth._http_gateway.post.assert_not_called()
        nodes = store.find_nodes("test-svc")
        assert len(nodes) == 0

    def test_default_routes_direct_to_gateway_llm(self, tmp_path):
        """Default config (no env vars) routes straight to gateway LLM."""
        store = KnowledgeStore(tmp_path)
        synth = LLMSynthesizer(store)
        assert synth._local_model_enabled is False
        mock_gateway_resp = MagicMock()
        mock_gateway_resp.status_code = 200
        mock_gateway_resp.json.return_value = VALID_OPENAI_RESPONSE
        mock_gateway_resp.raise_for_status = MagicMock()
        synth._http_gateway = MagicMock()
        synth._http_gateway.post.return_value = mock_gateway_resp
        synth.synthesize(
            [{"author": "scout", "content": "test msg", "channel": "general"}],
            ["#general"],
        )
        synth._http_gateway.post.assert_called_once()

    def test_explicit_disable_routes_direct_to_gateway_llm(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        with patch.dict(os.environ, {"KNOWLEDGE_LOCAL_MODEL_ENABLED": "false"}):
            synth = LLMSynthesizer(store)
        mock_gateway_resp = MagicMock()
        mock_gateway_resp.status_code = 200
        mock_gateway_resp.json.return_value = VALID_OPENAI_RESPONSE
        mock_gateway_resp.raise_for_status = MagicMock()
        synth._http_gateway = MagicMock()
        synth._http_gateway.post.return_value = mock_gateway_resp
        synth.synthesize(
            [{"author": "scout", "content": "test msg", "channel": "general"}],
            ["#general"],
        )
        synth._http_gateway.post.assert_called_once()


class TestSynthesisAuditLog:
    def test_audit_log_contains_all_required_fields(self, tmp_path):
        """Audit log entry must contain all 9 fields from the spec's fallback audit schema."""
        store = KnowledgeStore(tmp_path)
        with patch.dict(os.environ, {"KNOWLEDGE_LOCAL_MODEL_ENABLED": "true"}):
            synth = LLMSynthesizer(store)
        mock_response = MagicMock()
        mock_response.status_code = 200
        mock_response.json.return_value = VALID_OPENAI_RESPONSE
        mock_response.raise_for_status = MagicMock()
        synth._http_admin = MagicMock()
        synth._http_admin.post.return_value = mock_response
        with patch.object(synth, "_log_synthesis") as mock_log:
            synth.synthesize(
                [{"author": "scout", "content": "test", "channel": "general"}],
                ["#general"],
            )
            mock_log.assert_called_once()
            log_entry = mock_log.call_args[0][0]
            required_fields = [
                "model_attempted",
                "model_used",
                "fallback_triggered",
                "fallback_reason",
                "entities_extracted",
                "relationships_extracted",
                "source_type",
                "batch_size",
                "duration_ms",
            ]
            for field in required_fields:
                assert field in log_entry, f"Missing audit field: {field}"
            assert isinstance(log_entry["fallback_triggered"], bool)
            assert isinstance(log_entry["entities_extracted"], int)
            assert isinstance(log_entry["relationships_extracted"], int)
            assert isinstance(log_entry["batch_size"], int)
            assert isinstance(log_entry["duration_ms"], int)

    def test_audit_log_records_fallback(self, tmp_path):
        """Audit log correctly records when fallback is triggered."""
        store = KnowledgeStore(tmp_path)
        with patch.dict(os.environ, {"KNOWLEDGE_LOCAL_MODEL_ENABLED": "true", "KNOWLEDGE_SYNTH_FALLBACK": "true"}):
            synth = LLMSynthesizer(store)
        synth._http_admin = MagicMock()
        synth._http_admin.post.side_effect = Exception("connection refused")
        mock_gateway_resp = MagicMock()
        mock_gateway_resp.status_code = 200
        mock_gateway_resp.json.return_value = VALID_OPENAI_RESPONSE
        mock_gateway_resp.raise_for_status = MagicMock()
        synth._http_gateway = MagicMock()
        synth._http_gateway.post.return_value = mock_gateway_resp
        with patch.object(synth, "_log_synthesis") as mock_log:
            synth.synthesize(
                [{"author": "scout", "content": "test", "channel": "general"}],
                ["#general"],
            )
            log_entry = mock_log.call_args[0][0]
            assert log_entry["fallback_triggered"] is True
            assert log_entry["fallback_reason"] == "connection_refused"
            assert log_entry["model_used"] == "claude-haiku"
            assert log_entry["source_type"] == "llm"


class TestLazyModelPull:
    def test_model_not_found_triggers_pull(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        with patch.dict(os.environ, {"KNOWLEDGE_LOCAL_MODEL_ENABLED": "true"}):
            synth = LLMSynthesizer(store)
        # First call: model not found error
        not_found_resp = MagicMock()
        not_found_resp.status_code = 404
        not_found_resp.raise_for_status.side_effect = Exception("model 'qwen2.5:3b' not found")
        not_found_resp.json.return_value = {"error": "model 'qwen2.5:3b' not found"}

        synth._http_admin = MagicMock()
        synth._http_admin.post.return_value = not_found_resp

        # Mock the pull
        with patch.object(synth, "_pull_model", return_value=True) as mock_pull:
            synth._call_admin_model("test prompt")
            mock_pull.assert_called_once()


class TestGraduatedTrust:
    def test_unvalidated_model_dual_runs(self, tmp_path):
        """Before validation, synthesizer runs both models and compares."""
        store = KnowledgeStore(tmp_path)
        with patch.dict(os.environ, {"KNOWLEDGE_LOCAL_MODEL_ENABLED": "true"}):
            synth = LLMSynthesizer(store)
        synth._local_model_validated = False
        synth._validation_batches_remaining = 3

        admin_resp = MagicMock()
        admin_resp.status_code = 200
        admin_resp.json.return_value = VALID_OPENAI_RESPONSE
        admin_resp.raise_for_status = MagicMock()
        synth._http_admin = MagicMock()
        synth._http_admin.post.return_value = admin_resp

        gateway_resp = MagicMock()
        gateway_resp.status_code = 200
        gateway_resp.json.return_value = VALID_OPENAI_RESPONSE
        gateway_resp.raise_for_status = MagicMock()
        synth._http_gateway = MagicMock()
        synth._http_gateway.post.return_value = gateway_resp

        synth.synthesize(
            [{"author": "scout", "content": "test msg", "channel": "general"}],
            ["#general"],
        )

        # Both should be called during validation
        synth._http_admin.post.assert_called_once()
        synth._http_gateway.post.assert_called_once()

    def test_validated_model_skips_dual_run(self, tmp_path):
        """After validation, only admin model is called."""
        store = KnowledgeStore(tmp_path)
        with patch.dict(os.environ, {"KNOWLEDGE_LOCAL_MODEL_ENABLED": "true"}):
            synth = LLMSynthesizer(store)
        synth._local_model_validated = True

        admin_resp = MagicMock()
        admin_resp.status_code = 200
        admin_resp.json.return_value = VALID_OPENAI_RESPONSE
        admin_resp.raise_for_status = MagicMock()
        synth._http_admin = MagicMock()
        synth._http_admin.post.return_value = admin_resp
        synth._http_gateway = MagicMock()

        synth.synthesize(
            [{"author": "scout", "content": "test msg", "channel": "general"}],
            ["#general"],
        )

        synth._http_admin.post.assert_called_once()
        synth._http_gateway.post.assert_not_called()

    def test_bypass_validation_via_env(self, tmp_path):
        """KNOWLEDGE_LOCAL_MODEL_VALIDATED=true skips dual-run."""
        store = KnowledgeStore(tmp_path)
        with patch.dict(os.environ, {"KNOWLEDGE_LOCAL_MODEL_VALIDATED": "true"}):
            synth = LLMSynthesizer(store)
        assert synth._local_model_validated is True

    def test_recall_computation(self, tmp_path):
        store = KnowledgeStore(tmp_path)
        with patch.dict(os.environ, {"KNOWLEDGE_LOCAL_MODEL_ENABLED": "true"}):
            synth = LLMSynthesizer(store)
        local_entities = [
            {"label": "nginx", "kind": "component", "summary": "web"},
            {"label": "redis", "kind": "component", "summary": "cache"},
        ]
        haiku_entities = [
            {"label": "nginx", "kind": "component", "summary": "web server"},
            {"label": "redis", "kind": "component", "summary": "cache store"},
            {"label": "postgres", "kind": "component", "summary": "database"},
        ]
        recall = synth._compute_recall(local_entities, haiku_entities)
        assert abs(recall - 2.0 / 3.0) < 0.01

    def test_validation_passes_at_threshold(self, tmp_path):
        """After N batches with >=70% recall, flag is set and persisted."""
        store = KnowledgeStore(tmp_path)
        with patch.dict(os.environ, {"KNOWLEDGE_LOCAL_MODEL_ENABLED": "true"}):
            synth = LLMSynthesizer(store)
        synth._local_model_validated = False
        synth._validation_batches_remaining = 1  # last batch
        synth._validation_recalls = [0.75, 0.72]  # avg > 0.70

        admin_resp = MagicMock()
        admin_resp.status_code = 200
        admin_resp.json.return_value = VALID_OPENAI_RESPONSE
        admin_resp.raise_for_status = MagicMock()
        synth._http_admin = MagicMock()
        synth._http_admin.post.return_value = admin_resp

        gateway_resp = MagicMock()
        gateway_resp.status_code = 200
        gateway_resp.json.return_value = VALID_OPENAI_RESPONSE
        gateway_resp.raise_for_status = MagicMock()
        synth._http_gateway = MagicMock()
        synth._http_gateway.post.return_value = gateway_resp

        synth.synthesize(
            [{"author": "scout", "content": "test msg", "channel": "general"}],
            ["#general"],
        )
        assert synth._local_model_validated is True

    def test_validation_fails_below_threshold(self, tmp_path):
        """After N batches with <70% recall, flag stays false."""
        store = KnowledgeStore(tmp_path)
        with patch.dict(os.environ, {"KNOWLEDGE_LOCAL_MODEL_ENABLED": "true"}):
            synth = LLMSynthesizer(store)
        synth._local_model_validated = False
        synth._validation_batches_remaining = 1
        synth._validation_recalls = [0.40, 0.50]  # avg < 0.70

        admin_resp = MagicMock()
        admin_resp.status_code = 200
        admin_resp.json.return_value = {
            "choices": [{"message": {"content": json.dumps({
                "entities": [{"label": "nginx", "kind": "component", "summary": "web"}],
                "relationships": [],
            })}}],
        }
        admin_resp.raise_for_status = MagicMock()
        synth._http_admin = MagicMock()
        synth._http_admin.post.return_value = admin_resp

        gateway_resp = MagicMock()
        gateway_resp.status_code = 200
        gateway_resp.json.return_value = {"choices": [{"message": {"content": json.dumps({
            "entities": [
                {"label": "nginx", "kind": "component", "summary": "web"},
                {"label": "redis", "kind": "component", "summary": "cache"},
                {"label": "postgres", "kind": "component", "summary": "db"},
            ],
            "relationships": [],
        })}}]}
        gateway_resp.raise_for_status = MagicMock()
        synth._http_gateway = MagicMock()
        synth._http_gateway.post.return_value = gateway_resp

        synth.synthesize(
            [{"author": "scout", "content": "test msg", "channel": "general"}],
            ["#general"],
        )
        assert synth._local_model_validated is False


class TestGraduatedTrustPersistence:
    def test_state_persisted_to_file(self, tmp_path):
        """Validation state is written to validation_state.json."""
        store = KnowledgeStore(tmp_path)
        with patch.dict(os.environ, {"KNOWLEDGE_LOCAL_MODEL_ENABLED": "true"}):
            synth = LLMSynthesizer(store)
        synth._local_model_validated = True
        synth._save_validation_state()
        state_file = tmp_path / "validation_state.json"
        assert state_file.exists()
        state = json.loads(state_file.read_text())
        assert state["validated"] is True
        assert state["model_name"] == synth._model_name

    def test_state_loaded_on_init(self, tmp_path):
        """Validation state is loaded from file on init."""
        state_file = tmp_path / "validation_state.json"
        state_file.write_text(json.dumps({
            "validated": True,
            "model_name": "qwen2.5:3b",
            "recalls": [0.8, 0.85, 0.9],
        }))
        store = KnowledgeStore(tmp_path)
        with patch.dict(os.environ, {"KNOWLEDGE_LOCAL_MODEL_ENABLED": "true"}):
            synth = LLMSynthesizer(store)
        assert synth._local_model_validated is True

    def test_model_swap_resets_validation(self, tmp_path):
        """Changing model name resets validated flag."""
        state_file = tmp_path / "validation_state.json"
        state_file.write_text(json.dumps({
            "validated": True,
            "model_name": "qwen2.5:3b",
            "recalls": [0.8, 0.85, 0.9],
        }))
        store = KnowledgeStore(tmp_path)
        with patch.dict(os.environ, {"KNOWLEDGE_LOCAL_MODEL_ENABLED": "true", "KNOWLEDGE_LOCAL_MODEL": "llama3.2:1b"}):
            synth = LLMSynthesizer(store)
        assert synth._local_model_validated is False
        assert synth._validation_recalls == []


class TestGraduatedTrustE2E:
    def test_full_validation_lifecycle(self, tmp_path):
        """E2E: 3 dual-run batches → validation passes → subsequent calls skip dual-run."""
        store = KnowledgeStore(tmp_path)
        with patch.dict(os.environ, {"KNOWLEDGE_LOCAL_MODEL_ENABLED": "true"}):
            synth = LLMSynthesizer(store)
        assert synth._local_model_validated is False
        assert synth._validation_batches_remaining == 3

        # Prepare mocks for both models — same entities for high recall
        admin_resp = MagicMock()
        admin_resp.status_code = 200
        admin_resp.json.return_value = VALID_OPENAI_RESPONSE
        admin_resp.raise_for_status = MagicMock()

        gateway_resp = MagicMock()
        gateway_resp.status_code = 200
        gateway_resp.json.return_value = VALID_OPENAI_RESPONSE
        gateway_resp.raise_for_status = MagicMock()

        synth._http_admin = MagicMock()
        synth._http_admin.post.return_value = admin_resp
        synth._http_gateway = MagicMock()
        synth._http_gateway.post.return_value = gateway_resp

        msgs = [{"author": "scout", "content": "test msg", "channel": "general"}]

        # Run 3 batches (validation period)
        for i in range(3):
            synth._http_admin.post.reset_mock()
            synth._http_gateway.post.reset_mock()
            synth.synthesize(msgs, ["#general"])
            # Both models called during validation
            synth._http_admin.post.assert_called_once()
            synth._http_gateway.post.assert_called_once()

        # After 3 batches, validation should pass (100% recall — same entities)
        assert synth._local_model_validated is True

        # Verify state persisted
        state_file = tmp_path / "validation_state.json"
        assert state_file.exists()
        state = json.loads(state_file.read_text())
        assert state["validated"] is True

        # Next call should skip dual-run
        synth._http_admin.post.reset_mock()
        synth._http_gateway.post.reset_mock()
        synth.synthesize(msgs, ["#general"])
        synth._http_admin.post.assert_called_once()
        synth._http_gateway.post.assert_not_called()

    def test_validation_survives_restart(self, tmp_path):
        """E2E: validated state survives synthesizer re-initialization."""
        store = KnowledgeStore(tmp_path)
        # Write validated state
        state_file = tmp_path / "validation_state.json"
        state_file.write_text(json.dumps({
            "validated": True,
            "model_name": "qwen2.5:3b",
            "recalls": [0.85, 0.90, 0.88],
        }))
        # Create new synthesizer — should load state
        with patch.dict(os.environ, {"KNOWLEDGE_LOCAL_MODEL_ENABLED": "true"}):
            synth = LLMSynthesizer(store)
        assert synth._local_model_validated is True
        assert synth._validation_batches_remaining == 0
