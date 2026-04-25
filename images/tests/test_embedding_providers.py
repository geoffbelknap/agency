"""Tests for the pluggable embedding provider abstraction."""

import json
from pathlib import Path
from unittest.mock import MagicMock, patch


class TestNoOpProvider:
    def test_dimensions_zero(self):
        from images.knowledge.embedding import NoOpProvider

        assert NoOpProvider().dimensions == 0

    def test_embed_returns_empty(self):
        from images.knowledge.embedding import NoOpProvider

        assert NoOpProvider().embed("test") == []

    def test_embed_batch_returns_empty_lists(self):
        from images.knowledge.embedding import NoOpProvider

        assert NoOpProvider().embed_batch(["a", "b"]) == [[], []]

    def test_name(self):
        from images.knowledge.embedding import NoOpProvider

        assert NoOpProvider().name == "none"


class TestCreateProvider:
    def test_none_returns_noop(self):
        from images.knowledge.embedding import create_provider

        assert create_provider("none").name == "none"

    def test_unknown_falls_back_to_noop(self):
        from images.knowledge.embedding import create_provider

        assert create_provider("nonexistent").name == "none"

    @patch("images.knowledge.embedding.httpx")
    def test_ollama_calls_api_embed(self, mock_httpx):
        from images.knowledge.embedding import OllamaProvider

        mock_resp = MagicMock()
        mock_resp.status_code = 200
        mock_resp.json.return_value = {"embeddings": [[0.1, 0.2, 0.3]]}
        mock_client = MagicMock()
        mock_client.post.return_value = mock_resp
        mock_httpx.Client.return_value = mock_client

        p = OllamaProvider(model="test-model", endpoint="http://localhost:11434", dimensions=3)
        result = p.embed("test text")

        assert result == [0.1, 0.2, 0.3]
        call_url = mock_client.post.call_args[0][0]
        assert "/api/embed" in call_url

    def test_env_none_returns_noop(self):
        from images.knowledge.embedding import create_provider

        with patch.dict("os.environ", {"KNOWLEDGE_EMBED_PROVIDER": "none"}):
            assert create_provider().name == "none"

    @patch("images.knowledge.embedding.OpenAIProvider")
    def test_default_provider_is_openai_adapter(self, mock_openai):
        from images.knowledge.embedding import create_provider

        mock_openai.return_value.name = "openai"

        with patch.dict("os.environ", {}, clear=True):
            assert create_provider().name == "openai"
        mock_openai.assert_called_once_with(
            model="text-embedding-3-small",
            endpoint="https://api.openai.com/v1/embeddings",
            api_key_env="OPENAI_API_KEY",
            dimensions=None,
        )

    def test_exception_during_construction_falls_back_to_noop(self):
        from images.knowledge.embedding import create_provider

        with patch(
            "images.knowledge.embedding.OllamaProvider",
            side_effect=RuntimeError("connection refused"),
        ):
            provider = create_provider("ollama")
        assert provider.name == "none"


class TestOllamaProvider:
    @patch("images.knowledge.embedding.httpx")
    def test_embed_batch(self, mock_httpx):
        from images.knowledge.embedding import OllamaProvider

        mock_resp = MagicMock()
        mock_resp.json.return_value = {
            "embeddings": [[0.1, 0.2], [0.3, 0.4]]
        }
        mock_client = MagicMock()
        mock_client.post.return_value = mock_resp
        mock_httpx.Client.return_value = mock_client

        p = OllamaProvider(model="nomic-embed-text", endpoint="http://localhost:11434", dimensions=2)
        result = p.embed_batch(["hello", "world"])

        assert result == [[0.1, 0.2], [0.3, 0.4]]

    @patch("images.knowledge.embedding.httpx")
    def test_uses_api_embed_not_v1(self, mock_httpx):
        from images.knowledge.embedding import OllamaProvider

        mock_resp = MagicMock()
        mock_resp.json.return_value = {"embeddings": [[0.5]]}
        mock_client = MagicMock()
        mock_client.post.return_value = mock_resp
        mock_httpx.Client.return_value = mock_client

        p = OllamaProvider(model="m", endpoint="http://localhost:11434", dimensions=1)
        p.embed("x")

        url = mock_client.post.call_args[0][0]
        assert "/api/embed" in url
        assert "/v1/embeddings" not in url

    @patch("images.knowledge.embedding.httpx")
    def test_name_is_ollama(self, mock_httpx):
        from images.knowledge.embedding import OllamaProvider

        mock_client = MagicMock()
        mock_httpx.Client.return_value = mock_client
        p = OllamaProvider(model="m", endpoint="http://localhost:11434", dimensions=0)
        assert p.name == "ollama"

    @patch("images.knowledge.embedding.httpx")
    def test_dimensions_from_constructor(self, mock_httpx):
        from images.knowledge.embedding import OllamaProvider

        mock_client = MagicMock()
        mock_httpx.Client.return_value = mock_client
        p = OllamaProvider(model="m", endpoint="http://localhost:11434", dimensions=768)
        assert p.dimensions == 768


class TestOpenAIProvider:
    @patch("images.knowledge.embedding.httpx")
    def test_embed(self, mock_httpx):
        from images.knowledge.embedding import OpenAIProvider

        mock_resp = MagicMock()
        mock_resp.json.return_value = {
            "data": [{"index": 0, "embedding": [0.1, 0.2, 0.3]}]
        }
        mock_client = MagicMock()
        mock_client.post.return_value = mock_resp
        mock_httpx.Client.return_value = mock_client

        p = OpenAIProvider(model="text-embedding-3-small")
        result = p.embed("hello")

        assert result == [0.1, 0.2, 0.3]

    @patch("images.knowledge.embedding.httpx")
    def test_dimensions_small(self, mock_httpx):
        from images.knowledge.embedding import OpenAIProvider

        mock_httpx.Client.return_value = MagicMock()
        p = OpenAIProvider(model="text-embedding-3-small")
        assert p.dimensions == 1536

    @patch("images.knowledge.embedding.httpx")
    def test_dimensions_large(self, mock_httpx):
        from images.knowledge.embedding import OpenAIProvider

        mock_httpx.Client.return_value = MagicMock()
        p = OpenAIProvider(model="text-embedding-3-large")
        assert p.dimensions == 3072

    @patch("images.knowledge.embedding.httpx")
    def test_name_is_openai(self, mock_httpx):
        from images.knowledge.embedding import OpenAIProvider

        mock_httpx.Client.return_value = MagicMock()
        assert OpenAIProvider().name == "openai"

    @patch("images.knowledge.embedding.httpx")
    def test_configurable_endpoint_key_env_and_dimensions(self, mock_httpx):
        from images.knowledge.embedding import OpenAIProvider

        mock_resp = MagicMock()
        mock_resp.json.return_value = {
            "data": [{"index": 0, "embedding": [0.1, 0.2]}]
        }
        mock_client = MagicMock()
        mock_client.post.return_value = mock_resp
        mock_httpx.Client.return_value = mock_client

        with patch.dict("os.environ", {"PROVIDER_EMBED_API_KEY": "test-key"}):
            p = OpenAIProvider(
                model="custom-embedding-model",
                endpoint="https://embeddings.example.com/v1/embeddings",
                api_key_env="PROVIDER_EMBED_API_KEY",
                dimensions=2048,
            )
            assert p.embed("hello") == [0.1, 0.2]

        url = mock_client.post.call_args[0][0]
        _, kwargs = mock_client.post.call_args
        assert url == "https://embeddings.example.com/v1/embeddings"
        assert kwargs["headers"] == {"Authorization": "Bearer test-key"}
        assert p.dimensions == 2048

    @patch.dict("os.environ", {"HTTPS_PROXY": "http://egress:8080"})
    @patch("images.knowledge.embedding.httpx")
    def test_uses_proxy_when_set(self, mock_httpx):
        from images.knowledge.embedding import OpenAIProvider

        mock_httpx.Client.return_value = MagicMock()
        OpenAIProvider()
        _, kwargs = mock_httpx.Client.call_args
        assert kwargs.get("proxy") == "http://egress:8080"


class TestVoyageProvider:
    @patch("images.knowledge.embedding.httpx")
    def test_embed(self, mock_httpx):
        from images.knowledge.embedding import VoyageProvider

        mock_resp = MagicMock()
        mock_resp.json.return_value = {
            "data": [{"index": 0, "embedding": [0.9, 0.8]}]
        }
        mock_client = MagicMock()
        mock_client.post.return_value = mock_resp
        mock_httpx.Client.return_value = mock_client

        p = VoyageProvider(model="voyage-3-lite")
        result = p.embed("test")

        assert result == [0.9, 0.8]
        url = mock_client.post.call_args[0][0]
        assert "voyageai.com" in url

    @patch("images.knowledge.embedding.httpx")
    def test_dimensions_lite(self, mock_httpx):
        from images.knowledge.embedding import VoyageProvider

        mock_httpx.Client.return_value = MagicMock()
        assert VoyageProvider(model="voyage-3-lite").dimensions == 512

    @patch("images.knowledge.embedding.httpx")
    def test_dimensions_full(self, mock_httpx):
        from images.knowledge.embedding import VoyageProvider

        mock_httpx.Client.return_value = MagicMock()
        assert VoyageProvider(model="voyage-3").dimensions == 1024

    @patch("images.knowledge.embedding.httpx")
    def test_name_is_voyage(self, mock_httpx):
        from images.knowledge.embedding import VoyageProvider

        mock_httpx.Client.return_value = MagicMock()
        assert VoyageProvider().name == "voyage"

    @patch.dict("os.environ", {"HTTPS_PROXY": "http://egress:8080"})
    @patch("images.knowledge.embedding.httpx")
    def test_uses_proxy_when_set(self, mock_httpx):
        from images.knowledge.embedding import VoyageProvider

        mock_httpx.Client.return_value = MagicMock()
        VoyageProvider()
        _, kwargs = mock_httpx.Client.call_args
        assert kwargs.get("proxy") == "http://egress:8080"


class TestEmbeddableKinds:
    def test_default_kinds(self):
        from images.knowledge.embedding import get_embeddable_kinds

        kinds = get_embeddable_kinds()
        assert "software" in kinds
        assert "vulnerability" in kinds
        assert "ontologycandidate" not in kinds

    def test_custom_from_env(self):
        from images.knowledge.embedding import get_embeddable_kinds

        with patch.dict("os.environ", {"KNOWLEDGE_EMBED_KINDS": "Foo,Bar"}):
            kinds = get_embeddable_kinds()
            assert "foo" in kinds
            assert "bar" in kinds

    def test_default_includes_all_required_kinds(self):
        from images.knowledge.embedding import get_embeddable_kinds

        kinds = get_embeddable_kinds()
        required = {
            "software",
            "configitem",
            "behaviorpattern",
            "vulnerability",
            "finding",
            "threatindicator",
            "hunthypothesis",
        }
        assert required <= kinds

    def test_strips_whitespace(self):
        from images.knowledge.embedding import get_embeddable_kinds

        with patch.dict("os.environ", {"KNOWLEDGE_EMBED_KINDS": " Alpha , Beta "}):
            kinds = get_embeddable_kinds()
            assert "alpha" in kinds
            assert "beta" in kinds

    def test_ignores_empty_entries(self):
        from images.knowledge.embedding import get_embeddable_kinds

        with patch.dict("os.environ", {"KNOWLEDGE_EMBED_KINDS": "Foo,,Bar,"}):
            kinds = get_embeddable_kinds()
            assert "" not in kinds
            assert "foo" in kinds
            assert "bar" in kinds


class TestStoreEmbedding:
    def test_skips_non_embeddable_kind(self, tmp_path):
        """Nodes with non-embeddable kinds don't get embedded."""
        from images.knowledge.store import KnowledgeStore
        from images.knowledge.embedding import NoOpProvider

        store = KnowledgeStore(tmp_path)
        store._embedding_provider = NoOpProvider()
        store._generate_embedding("id-1", "agent", "test-agent", "An agent")
        # No error — agent is not in embeddable_kinds by default

    def test_skips_noop_provider(self, tmp_path):
        from images.knowledge.store import KnowledgeStore
        from images.knowledge.embedding import NoOpProvider

        store = KnowledgeStore(tmp_path)
        store._embedding_provider = NoOpProvider()
        store._embeddable_kinds = {"software"}
        store._generate_embedding("id-1", "software", "nginx", "web server")
        # No error — NoOpProvider has dimensions=0

    def test_embedding_failure_does_not_block_add_node(self, tmp_path):
        from images.knowledge.store import KnowledgeStore

        store = KnowledgeStore(tmp_path)
        mock_provider = MagicMock()
        mock_provider.name = "test"
        mock_provider.dimensions = 3
        mock_provider.embed.side_effect = Exception("API down")
        store._embedding_provider = mock_provider
        store._embeddable_kinds = {"software"}
        node_id = store.add_node("nginx", "software", "web server", {}, "agent")
        assert node_id is not None


class TestHybridRetrieval:
    def test_fts_only_when_no_vectors(self, tmp_path):
        from images.knowledge.store import KnowledgeStore

        store = KnowledgeStore(tmp_path)
        store.add_node("nginx", "software", "web server", {}, "agent", ["ch-1"])
        results = store.find_nodes("nginx")
        assert len(results) >= 1

    def test_excludes_ontology_candidate(self, tmp_path):
        from images.knowledge.store import KnowledgeStore

        store = KnowledgeStore(tmp_path)
        store.add_node("nginx", "software", "web server", {}, "agent", ["ch-1"])
        store.add_node("candidate:software", "OntologyCandidate", "", {"status": "candidate"}, "rule")
        results = store.find_nodes("software")
        kinds = [r["kind"] for r in results]
        assert "OntologyCandidate" not in kinds

    def test_find_similar_returns_empty_when_no_vectors(self, tmp_path):
        from images.knowledge.store import KnowledgeStore

        store = KnowledgeStore(tmp_path)
        node_id = store.add_node("nginx", "software", "web server", {}, "agent")
        results = store.find_similar(node_id)
        assert results == []

    def test_backfill_returns_zero_when_no_provider(self, tmp_path):
        from images.knowledge.store import KnowledgeStore

        store = KnowledgeStore(tmp_path)
        count = store.backfill_embeddings()
        assert count == 0


class TestFindSimilarTool:
    def test_returns_response_from_server(self):
        from images.body.knowledge_tools import _query_graph

        with patch("images.body.knowledge_tools._http") as mock_http:
            mock_resp = MagicMock()
            mock_resp.text = '{"nodes": [], "edges": []}'
            mock_http.get.return_value = mock_resp
            result = _query_graph("http://test", "agent", {"pattern": "find_similar", "id": "abc"})
            assert "nodes" in result

    def test_requires_id(self):
        from images.body.knowledge_tools import _query_graph

        result = _query_graph("http://test", "agent", {"pattern": "find_similar"})
        parsed = json.loads(result)
        assert "error" in parsed
