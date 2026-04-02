"""Pluggable embedding provider abstraction for the knowledge graph.

Providers:
  - NoOpProvider: no-op, returns empty vectors (default/fallback)
  - OllamaProvider: local model via agency-infra-admin-model
  - OpenAIProvider: text-embedding-3-small/large via egress proxy
  - VoyageProvider: voyage-3-lite/voyage-3 via egress proxy

Remote providers route through HTTPS_PROXY (egress) for credential swap
per ASK Tenets 3 and 4 — no unmediated path to external resources.
"""

import logging
import os
from abc import ABC, abstractmethod
from typing import Optional

import httpx

logger = logging.getLogger(__name__)

DEFAULT_EMBED_KINDS = (
    "Software,ConfigItem,BehaviorPattern,Vulnerability,Finding,"
    "ThreatIndicator,HuntHypothesis"
)


class EmbeddingProvider(ABC):
    """Abstract base for embedding providers."""

    @abstractmethod
    def embed(self, text: str) -> list[float]:
        """Embed a single text string."""
        ...

    @abstractmethod
    def embed_batch(self, texts: list[str]) -> list[list[float]]:
        """Embed a list of text strings."""
        ...

    @property
    @abstractmethod
    def dimensions(self) -> int:
        """Dimensionality of returned vectors."""
        ...

    @property
    @abstractmethod
    def name(self) -> str:
        """Provider identifier."""
        ...


class NoOpProvider(EmbeddingProvider):
    """No-op provider — returns empty vectors. Used as fallback."""

    def embed(self, text: str) -> list[float]:
        return []

    def embed_batch(self, texts: list[str]) -> list[list[float]]:
        return [[] for _ in texts]

    @property
    def dimensions(self) -> int:
        return 0

    @property
    def name(self) -> str:
        return "none"


class OllamaProvider(EmbeddingProvider):
    """Embedding via local Ollama instance (agency-infra-admin-model).

    Uses /api/embed (not /v1/embeddings). Batch input is an array;
    response["embeddings"] is a list of vectors.
    """

    def __init__(
        self,
        model: str,
        endpoint: str = "http://agency-infra-admin-model:11434",
        dimensions: Optional[int] = None,
    ) -> None:
        self._model = model
        self._endpoint = endpoint.rstrip("/")
        self._client = httpx.Client(timeout=30)
        if dimensions is not None:
            self._dimensions = dimensions
        else:
            self._dimensions = self._probe_dimensions()

    def _probe_dimensions(self) -> int:
        """Run a test embed to discover vector size."""
        try:
            result = self.embed("probe")
            return len(result)
        except Exception as exc:
            logger.warning("OllamaProvider: dimension probe failed: %s", exc)
            return 0

    def embed(self, text: str) -> list[float]:
        resp = self._client.post(
            f"{self._endpoint}/api/embed",
            json={"model": self._model, "input": [text]},
        )
        resp.raise_for_status()
        embeddings = resp.json()["embeddings"]
        return embeddings[0] if embeddings else []

    def embed_batch(self, texts: list[str]) -> list[list[float]]:
        if not texts:
            return []
        resp = self._client.post(
            f"{self._endpoint}/api/embed",
            json={"model": self._model, "input": texts},
        )
        resp.raise_for_status()
        return resp.json()["embeddings"]

    @property
    def dimensions(self) -> int:
        return self._dimensions

    @property
    def name(self) -> str:
        return "ollama"


class OpenAIProvider(EmbeddingProvider):
    """Embedding via OpenAI API, routed through the egress proxy.

    Supported models and their dimensions:
      text-embedding-3-small  →  1536
      text-embedding-3-large  →  3072
    """

    _DIMENSIONS = {
        "text-embedding-3-small": 1536,
        "text-embedding-3-large": 3072,
    }

    def __init__(self, model: str = "text-embedding-3-small") -> None:
        self._model = model
        proxy = os.environ.get("HTTPS_PROXY") or os.environ.get("https_proxy")
        self._client = (
            httpx.Client(timeout=30, proxy=proxy)
            if proxy
            else httpx.Client(timeout=30)
        )
        api_key = os.environ.get("OPENAI_API_KEY", "")
        self._headers = {"Authorization": f"Bearer {api_key}"}

    def embed(self, text: str) -> list[float]:
        resp = self._client.post(
            "https://api.openai.com/v1/embeddings",
            headers=self._headers,
            json={"model": self._model, "input": text},
        )
        resp.raise_for_status()
        return resp.json()["data"][0]["embedding"]

    def embed_batch(self, texts: list[str]) -> list[list[float]]:
        if not texts:
            return []
        resp = self._client.post(
            "https://api.openai.com/v1/embeddings",
            headers=self._headers,
            json={"model": self._model, "input": texts},
        )
        resp.raise_for_status()
        data = resp.json()["data"]
        # API returns items sorted by index
        return [item["embedding"] for item in sorted(data, key=lambda x: x["index"])]

    @property
    def dimensions(self) -> int:
        return self._DIMENSIONS.get(self._model, 1536)

    @property
    def name(self) -> str:
        return "openai"


class VoyageProvider(EmbeddingProvider):
    """Embedding via Voyage AI API, routed through the egress proxy.

    Supported models and their dimensions:
      voyage-3-lite  →  512
      voyage-3       →  1024
    """

    _DIMENSIONS = {
        "voyage-3-lite": 512,
        "voyage-3": 1024,
    }

    def __init__(self, model: str = "voyage-3-lite") -> None:
        self._model = model
        proxy = os.environ.get("HTTPS_PROXY") or os.environ.get("https_proxy")
        self._client = (
            httpx.Client(timeout=30, proxy=proxy)
            if proxy
            else httpx.Client(timeout=30)
        )
        api_key = os.environ.get("VOYAGE_API_KEY", "")
        self._headers = {"Authorization": f"Bearer {api_key}"}

    def embed(self, text: str, input_type: str = "document") -> list[float]:
        resp = self._client.post(
            "https://api.voyageai.com/v1/embeddings",
            headers=self._headers,
            json={"model": self._model, "input": [text], "input_type": input_type},
        )
        resp.raise_for_status()
        return resp.json()["data"][0]["embedding"]

    def embed_batch(self, texts: list[str], input_type: str = "document") -> list[list[float]]:
        if not texts:
            return []
        resp = self._client.post(
            "https://api.voyageai.com/v1/embeddings",
            headers=self._headers,
            json={"model": self._model, "input": texts, "input_type": input_type},
        )
        resp.raise_for_status()
        data = resp.json()["data"]
        return [item["embedding"] for item in sorted(data, key=lambda x: x["index"])]

    @property
    def dimensions(self) -> int:
        return self._DIMENSIONS.get(self._model, 512)

    @property
    def name(self) -> str:
        return "voyage"


def create_provider(provider_name: Optional[str] = None) -> EmbeddingProvider:
    """Create an embedding provider by name.

    Falls back to NoOpProvider on unknown names or any exception during
    construction so the knowledge service always starts cleanly.

    Environment variables:
      KNOWLEDGE_EMBED_PROVIDER  — provider name (none/ollama/openai/voyage)
      KNOWLEDGE_EMBED_OLLAMA_MODEL — model name for Ollama
    """
    name = (
        provider_name or os.environ.get("KNOWLEDGE_EMBED_PROVIDER", "ollama")
    ).lower()

    try:
        if name == "none":
            return NoOpProvider()

        if name == "ollama":
            model = os.environ.get("KNOWLEDGE_EMBED_OLLAMA_MODEL", "all-minilm")
            endpoint = os.environ.get(
                "KNOWLEDGE_EMBED_OLLAMA_ENDPOINT",
                "http://agency-infra-admin-model:11434",
            )
            return OllamaProvider(model=model, endpoint=endpoint)

        if name == "openai":
            model = os.environ.get(
                "KNOWLEDGE_EMBED_OPENAI_MODEL", "text-embedding-3-small"
            )
            return OpenAIProvider(model=model)

        if name == "voyage":
            model = os.environ.get(
                "KNOWLEDGE_EMBED_VOYAGE_MODEL", "voyage-3-lite"
            )
            return VoyageProvider(model=model)

        logger.warning(
            "create_provider: unknown provider %r, falling back to NoOp", name
        )
        return NoOpProvider()

    except Exception as exc:
        logger.warning(
            "create_provider: failed to create %r provider (%s), falling back to NoOp",
            name,
            exc,
        )
        return NoOpProvider()


def get_embeddable_kinds() -> set[str]:
    """Return the set of entity kinds that should be embedded.

    Controlled by KNOWLEDGE_EMBED_KINDS env var (comma-separated list of
    kind names). Defaults to DEFAULT_EMBED_KINDS. All names are lowercased
    for case-insensitive comparison.
    """
    raw = os.environ.get("KNOWLEDGE_EMBED_KINDS", DEFAULT_EMBED_KINDS)
    return {k.strip().lower() for k in raw.split(",") if k.strip()}
